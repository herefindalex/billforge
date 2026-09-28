package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"billforge/lab"
)

func TestResourceListFiltersAndCursorScope(t *testing.T) {
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
	for _, customerID := range []string{"filtered-a", "filtered-b", "filtered-a"} {
		if _, err := l.CreateQuote(ctx, customerID, "basic"); err != nil {
			t.Fatal(err)
		}
	}
	const token = "resource-filter-session"
	s := &Server{
		lab: l,
		sessions: map[[32]byte]session{
			hashToken(token): {capabilities: []string{"read"}, expires: time.Now().Add(time.Hour), idleUntil: time.Now().Add(time.Hour)},
		},
		now: time.Now,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/api/{resource}", s.protected(s.listResource))
	get := func(path string, authenticated bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/admin/api/"+path, nil)
		if authenticated {
			r.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	if w := get("quotes?customer_id=filtered-a", false); w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated filtered list = %d", w.Code)
	}
	if w := get("subscriptions?created_from=2100-01-01T00:00:00Z&created_before=2200-01-01T00:00:00Z", true); w.Code != http.StatusOK {
		t.Fatalf("valid UTC range returned %d: %s", w.Code, w.Body.String())
	}
	var first struct {
		Items      []map[string]any `json:"items"`
		NextCursor string           `json:"next_cursor"`
		Total      int              `json:"total"`
	}
	w := get("quotes?limit=1&customer_id=filtered-a", true)
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &first) != nil || first.Total != 2 || len(first.Items) != 1 || first.NextCursor == "" {
		t.Fatalf("first filtered page status=%d body=%s", w.Code, w.Body.String())
	}
	var second struct {
		Items []map[string]any `json:"items"`
		Total int              `json:"total"`
	}
	w = get("quotes?limit=1&customer_id=filtered-a&cursor="+url.QueryEscape(first.NextCursor), true)
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &second) != nil || second.Total != 2 || len(second.Items) != 1 || second.Items[0]["ID"] == first.Items[0]["ID"] {
		t.Fatalf("second filtered page status=%d body=%s", w.Code, w.Body.String())
	}
	for _, tc := range []struct {
		path, code string
		status     int
	}{
		{"quotes?limit=1&customer_id=filtered-b&cursor=" + url.QueryEscape(first.NextCursor), "INVALID_CURSOR", http.StatusBadRequest},
		{"quotes?limit=1&cursor=" + url.QueryEscape(first.NextCursor), "INVALID_CURSOR", http.StatusBadRequest},
		{"payments?limit=1&customer_id=filtered-a&cursor=" + url.QueryEscape(first.NextCursor), "INVALID_CURSOR", http.StatusBadRequest},
		{"quotes?status=active", "INVALID_FILTER", http.StatusBadRequest},
		{"quotes?unexpected=value", "INVALID_FILTER", http.StatusBadRequest},
		{"quotes?customer_id=filtered-a&customer_id=filtered-b", "INVALID_FILTER", http.StatusBadRequest},
		{"quotes?customer_id=%zz", "INVALID_FILTER", http.StatusBadRequest},
		{"subscriptions?created_from=invalid", "INVALID_FILTER", http.StatusBadRequest},
		{"subscriptions?created_from=2100-01-01T00:00:00Z&created_before=2100-01-01T00:00:00Z", "INVALID_FILTER", http.StatusBadRequest},
		{"quotes?created_from=2026-01-01T00:00:00Z", "INVALID_FILTER", http.StatusBadRequest},
		{"quotes?limit=101", "INVALID_LIMIT", http.StatusBadRequest},
		{"quotes?cursor=invalid", "INVALID_CURSOR", http.StatusBadRequest},
		{"missing-resource", "NOT_FOUND", http.StatusNotFound},
	} {
		w := get(tc.path, true)
		if w.Code != tc.status || !strings.Contains(w.Body.String(), `"code":"`+tc.code+`"`) {
			t.Fatalf("%s status=%d body=%s; want %d %s", tc.path, w.Code, w.Body.String(), tc.status, tc.code)
		}
	}
}

func TestResourceListHTTPKeepsMissingProjectionAndPeriodAsNull(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	l, err := lab.Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	quote, err := l.CreateQuote(ctx, "missing-projection-http", "basic")
	if err != nil {
		t.Fatal(err)
	}
	paid, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "missing-projection-purchase")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.DispatchNext(ctx, ""); err != nil {
		t.Fatal(err)
	}
	now = time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)
	change, err := l.RequestImmediateProUpgrade(ctx, paid.SubscriptionID, 5, 1, "missing-period-http")
	if err != nil {
		t.Fatal(err)
	}
	const token = "missing-source-list-session"
	s := &Server{
		lab: l,
		sessions: map[[32]byte]session{
			hashToken(token): {capabilities: []string{"read"}, expires: time.Now().Add(time.Hour), idleUntil: time.Now().Add(time.Hour)},
		},
		now: time.Now,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/api/{resource}", s.protected(s.listResource))
	get := func(resource string) []map[string]any {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/admin/api/"+resource, nil)
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		var response struct {
			Items []map[string]any `json:"items"`
		}
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &response) != nil {
			t.Fatalf("%s status=%d body=%s", resource, w.Code, w.Body.String())
		}
		return response.Items
	}
	subscriptions := get("subscriptions")
	if len(subscriptions) != 1 || subscriptions[0]["ID"] != paid.SubscriptionID || subscriptions[0]["EntitlementStatus"] != nil || subscriptions[0]["EntitlementReason"] != nil {
		t.Fatalf("missing entitlement projection serialized as known: %+v", subscriptions)
	}
	var original, supplement map[string]any
	for _, row := range get("invoices") {
		switch row["ID"] {
		case paid.InvoiceID:
			original = row
		case change.InvoiceID:
			supplement = row
		}
	}
	if original == nil || original["PeriodIndex"] != "0" || supplement == nil || supplement["PeriodIndex"] != nil {
		t.Fatalf("period source lost zero/null distinction: original=%+v supplement=%+v", original, supplement)
	}
}

func TestContractResourceListReturnsEffectiveBoundsAsUTC(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	l, err := lab.Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), func() time.Time { return from })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	_, err = l.PublishContract(ctx, lab.ContractSpec{
		ID: "contract-time-http", CustomerID: "contract-time-customer", Version: 1,
		BasePriceVersionID: "pro-v1", FixedMinor: 4000, SeatMinor: 700,
		EffectiveFrom: from, EffectiveTo: to,
	})
	if err != nil {
		t.Fatal(err)
	}
	const token = "contract-time-list-session"
	s := &Server{
		lab: l,
		sessions: map[[32]byte]session{hashToken(token): {
			capabilities: []string{"read"}, expires: time.Now().Add(time.Hour), idleUntil: time.Now().Add(time.Hour),
		}},
		now: time.Now,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/api/{resource}", s.protected(s.listResource))
	r := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/admin/api/contracts?id_prefix=contract-time-http", nil)
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	var response struct {
		Items []map[string]any `json:"items"`
	}
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &response) != nil || len(response.Items) != 1 {
		t.Fatalf("contract list status=%d body=%s", w.Code, w.Body.String())
	}
	if response.Items[0]["EffectiveFrom"] != from.Format(time.RFC3339Nano) || response.Items[0]["EffectiveTo"] != to.Format(time.RFC3339Nano) {
		t.Fatalf("contract effective bounds not UTC strings: %+v", response.Items[0])
	}
}
