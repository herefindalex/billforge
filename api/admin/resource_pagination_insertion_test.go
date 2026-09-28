package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"billforge/lab"
)

func TestResourcePagesIncludeLaterInsertsWithoutRepeatingEarlierRows(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	l, err := lab.Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	const token = "resource-insertion-session"
	s := &Server{
		lab: l,
		sessions: map[[32]byte]session{
			hashToken(token): {capabilities: []string{"read"}, expires: time.Now().Add(time.Hour), idleUntil: time.Now().Add(time.Hour)},
		},
		now: time.Now,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/api/{resource}", s.protected(s.listResource))
	mux.HandleFunc("GET /admin/api/customers", s.protected(s.listCustomers))
	type page struct {
		Items      []map[string]any `json:"items"`
		Total      int              `json:"total"`
		NextCursor string           `json:"next_cursor"`
	}
	get := func(resource string, query url.Values) page {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/admin/api/"+resource+"?"+query.Encode(), nil)
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s?%s status=%d body=%s", resource, query.Encode(), w.Code, w.Body.String())
		}
		var result page
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	create := func(customer, key string) (string, string, string, string, string, string) {
		t.Helper()
		quote, err := l.CreateQuote(ctx, customer, "basic")
		if err != nil {
			t.Fatal(err)
		}
		accepted, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, key)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := l.DispatchCapture(ctx, accepted.OperationID, ""); err != nil {
			t.Fatal(err)
		}
		correction, err := l.PostReduction(ctx, accepted.InvoiceID, 1000, "pagination credit", key+"-reduction")
		if err != nil || len(correction.GrantIDs) != 1 {
			t.Fatalf("credit correction=%+v error=%v", correction, err)
		}
		grantID := correction.GrantIDs[0]
		refundID, err := l.ReserveRefund(ctx, grantID, 500, key+"-refund")
		if err != nil {
			t.Fatal(err)
		}
		return quote.ID, accepted.SubscriptionID, accepted.InvoiceID, accepted.OperationID, grantID, refundID
	}
	firstQuote, firstSub, firstInvoice, firstPayment, firstCredit, firstRefund := create("page-a", "page-first")
	_, _, otherInvoice, otherPayment, otherCredit, otherRefund := create("page-b", "page-other")
	secondQuote, secondSub, secondInvoice, secondPayment, secondCredit, secondRefund := create("page-a", "page-second")
	pageQuery := func(resource, cursor string) url.Values {
		query := url.Values{"limit": {"1"}}
		if resource == "quotes" || resource == "subscriptions" {
			query.Set("customer_id", "page-a")
		}
		if cursor != "" {
			query.Set("cursor", cursor)
		}
		return query
	}
	firstPages := map[string]page{}
	for _, resource := range []string{"quotes", "subscriptions", "invoices", "payments", "credits", "refunds"} {
		first := get(resource, pageQuery(resource, ""))
		wantTotal := 3
		if resource == "quotes" || resource == "subscriptions" {
			wantTotal = 2
		}
		if first.Total != wantTotal || len(first.Items) != 1 || first.NextCursor == "" {
			t.Fatalf("%s first page %+v", resource, first)
		}
		firstPages[resource] = first
	}
	thirdQuote, thirdSub, thirdInvoice, thirdPayment, thirdCredit, thirdRefund := create("page-a", "page-third")
	_, _, laterInvoice, laterPayment, laterCredit, laterRefund := create("page-b", "page-other-later")
	for _, tc := range []struct {
		resource string
		ids      []string
	}{
		{"quotes", []string{firstQuote, secondQuote, thirdQuote}},
		{"subscriptions", []string{firstSub, secondSub, thirdSub}},
		{"invoices", []string{firstInvoice, otherInvoice, secondInvoice, thirdInvoice, laterInvoice}},
		{"payments", []string{firstPayment, otherPayment, secondPayment, thirdPayment, laterPayment}},
		{"credits", []string{firstCredit, otherCredit, secondCredit, thirdCredit, laterCredit}},
		{"refunds", []string{firstRefund, otherRefund, secondRefund, thirdRefund, laterRefund}},
	} {
		first := firstPages[tc.resource]
		got := []string{first.Items[0]["ID"].(string)}
		cursor := first.NextCursor
		for cursor != "" {
			current := get(tc.resource, pageQuery(tc.resource, cursor))
			if current.Total != len(tc.ids) || len(current.Items) != 1 {
				t.Fatalf("%s page after insert %+v", tc.resource, current)
			}
			got = append(got, current.Items[0]["ID"].(string))
			cursor = current.NextCursor
			if len(got) > len(tc.ids) {
				t.Fatalf("%s cursor repeated rows: %v", tc.resource, got)
			}
		}
		if !reflect.DeepEqual(got, tc.ids) {
			t.Fatalf("%s pages got %v, want %v", tc.resource, got, tc.ids)
		}
	}
}

func TestCustomerPagesKeepIDCursorWhenNamesAreInsertedOnEitherSide(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	l, err := lab.Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	const token = "customer-insertion-session"
	s := &Server{
		lab: l,
		sessions: map[[32]byte]session{
			hashToken(token): {capabilities: []string{"read"}, expires: time.Now().Add(time.Hour), idleUntil: time.Now().Add(time.Hour)},
		},
		now: time.Now,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/api/customers", s.protected(s.listCustomers))
	create := func(customer string) {
		t.Helper()
		if _, err := l.CreateQuote(ctx, customer, "basic"); err != nil {
			t.Fatal(err)
		}
	}
	get := func(cursor string) (string, int, string) {
		t.Helper()
		query := url.Values{"limit": {"1"}}
		if cursor != "" {
			query.Set("cursor", cursor)
		}
		r := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/admin/api/customers?"+query.Encode(), nil)
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("customer page status=%d body=%s", w.Code, w.Body.String())
		}
		var page struct {
			Items      []struct{ ID string } `json:"items"`
			Total      int                   `json:"total"`
			NextCursor string                `json:"next_cursor"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 1 {
			t.Fatalf("customer page items=%+v", page.Items)
		}
		return page.Items[0].ID, page.Total, page.NextCursor
	}
	create("page-1")
	create("page-3")
	id, total, cursor := get("")
	if id != "page-1" || total != 2 || cursor == "" {
		t.Fatalf("first customer page id=%s total=%d cursor=%q", id, total, cursor)
	}
	create("page-0")
	create("page-2")
	id, total, cursor = get(cursor)
	if id != "page-2" || total != 4 || cursor == "" {
		t.Fatalf("second customer page id=%s total=%d cursor=%q", id, total, cursor)
	}
	id, total, cursor = get(cursor)
	if id != "page-3" || total != 4 || cursor != "" {
		t.Fatalf("last customer page id=%s total=%d cursor=%q", id, total, cursor)
	}
}
