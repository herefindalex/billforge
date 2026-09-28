package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"billforge/lab"
)

func TestFilteredRefundPagesIncludeLaterInsertWithoutCrossingGrants(t *testing.T) {
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

	createGrant := func(customer, key string) string {
		t.Helper()
		quote, err := l.CreateQuote(ctx, customer, "basic")
		if err != nil {
			t.Fatal(err)
		}
		accepted, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, key+"-checkout")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := l.DispatchCapture(ctx, accepted.OperationID, ""); err != nil {
			t.Fatal(err)
		}
		correction, err := l.PostReduction(ctx, accepted.InvoiceID, 1000, "filtered refund pagination", key+"-reduction")
		if err != nil || len(correction.GrantIDs) != 1 {
			t.Fatalf("credit correction=%+v error=%v", correction, err)
		}
		return correction.GrantIDs[0]
	}
	reserve := func(grantID, key string) string {
		t.Helper()
		id, err := l.ReserveRefund(ctx, grantID, 200, key)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}

	grantID := createGrant("filtered-refunds-a", "refund-page-a")
	otherGrantID := createGrant("filtered-refunds-b", "refund-page-b")
	firstID := reserve(grantID, "refund-page-first")
	reserve(otherGrantID, "refund-page-other-first")
	secondID := reserve(grantID, "refund-page-second")

	const token = "filtered-refund-page-session"
	s := &Server{
		lab: l,
		sessions: map[[32]byte]session{
			hashToken(token): {capabilities: []string{"read"}, expires: time.Now().Add(time.Hour), idleUntil: time.Now().Add(time.Hour)},
		},
		now: time.Now,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/api/{resource}", s.protected(s.listResource))
	type page struct {
		Items      []map[string]any `json:"items"`
		Total      int              `json:"total"`
		NextCursor string           `json:"next_cursor"`
	}
	get := func(query url.Values) (*httptest.ResponseRecorder, page) {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/admin/api/refunds?"+query.Encode(), nil)
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		var result page
		if w.Code == http.StatusOK {
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
		}
		return w, result
	}
	query := url.Values{"limit": {"1"}, "grant_id": {grantID}}
	w, first := get(query)
	if w.Code != http.StatusOK || first.Total != 2 || len(first.Items) != 1 || first.Items[0]["ID"] != firstID || first.NextCursor == "" {
		t.Fatalf("first filtered page status=%d page=%+v body=%s", w.Code, first, w.Body.String())
	}

	thirdID := reserve(grantID, "refund-page-third")
	reserve(otherGrantID, "refund-page-other-second")

	wrongGrant := url.Values{"limit": {"1"}, "grant_id": {otherGrantID}, "cursor": {first.NextCursor}}
	w, _ = get(wrongGrant)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), `"code":"INVALID_CURSOR"`) {
		t.Fatalf("cursor reused for another grant status=%d body=%s", w.Code, w.Body.String())
	}

	got := []string{firstID}
	cursor := first.NextCursor
	for cursor != "" {
		query.Set("cursor", cursor)
		w, current := get(query)
		if w.Code != http.StatusOK || current.Total != 3 || len(current.Items) != 1 {
			t.Fatalf("later filtered page status=%d page=%+v body=%s", w.Code, current, w.Body.String())
		}
		if current.Items[0]["GrantID"] != grantID {
			t.Fatalf("refund crossed grant filter: %+v", current.Items[0])
		}
		got = append(got, current.Items[0]["ID"].(string))
		cursor = current.NextCursor
		if len(got) > 3 {
			t.Fatalf("cursor repeated rows: %v", got)
		}
	}
	if !reflect.DeepEqual(got, []string{firstID, secondID, thirdID}) {
		t.Fatalf("filtered pages got %v, want %v", got, []string{firstID, secondID, thirdID})
	}
}
