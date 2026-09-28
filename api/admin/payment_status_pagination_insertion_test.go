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

func TestPaymentStatusFilterKeepsCursorAfterLaterInsert(t *testing.T) {
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
	createPayment := func(key string, capture bool) string {
		t.Helper()
		quote, err := l.CreateQuote(ctx, key, "basic")
		if err != nil {
			t.Fatal(err)
		}
		accepted, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, key+"-checkout")
		if err != nil {
			t.Fatal(err)
		}
		if capture {
			if _, err := l.DispatchCapture(ctx, accepted.OperationID, ""); err != nil {
				t.Fatal(err)
			}
		}
		return accepted.OperationID
	}

	firstID := createPayment("payment-status-first", true)
	createPayment("payment-status-created-before", false)
	secondID := createPayment("payment-status-second", true)

	const token = "payment-status-page-session"
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
		r := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/admin/api/payments?"+query.Encode(), nil)
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
	query := url.Values{"limit": {"1"}, "status": {"succeeded"}}
	w, first := get(query)
	if w.Code != http.StatusOK || first.Total != 2 || len(first.Items) != 1 || first.Items[0]["ID"] != firstID || first.NextCursor == "" {
		t.Fatalf("first succeeded page status=%d page=%+v body=%s", w.Code, first, w.Body.String())
	}

	thirdID := createPayment("payment-status-third", true)
	createPayment("payment-status-created-after", false)
	wrongStatus := url.Values{"limit": {"1"}, "status": {"created"}, "cursor": {first.NextCursor}}
	w, _ = get(wrongStatus)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), `"code":"INVALID_CURSOR"`) {
		t.Fatalf("cursor reused for another status status=%d body=%s", w.Code, w.Body.String())
	}

	got := []string{firstID}
	cursor := first.NextCursor
	for cursor != "" {
		query.Set("cursor", cursor)
		w, current := get(query)
		if w.Code != http.StatusOK || current.Total != 3 || len(current.Items) != 1 {
			t.Fatalf("later succeeded page status=%d page=%+v body=%s", w.Code, current, w.Body.String())
		}
		if current.Items[0]["Status"] != "succeeded" {
			t.Fatalf("payment crossed status filter: %+v", current.Items[0])
		}
		got = append(got, current.Items[0]["ID"].(string))
		cursor = current.NextCursor
		if len(got) > 3 {
			t.Fatalf("cursor repeated rows: %v", got)
		}
	}
	if !reflect.DeepEqual(got, []string{firstID, secondID, thirdID}) {
		t.Fatalf("succeeded pages got %v, want %v", got, []string{firstID, secondID, thirdID})
	}
}
