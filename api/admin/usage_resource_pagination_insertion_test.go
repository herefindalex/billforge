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

func TestFilteredUsageEventPagesIncludeLaterInsertWithoutDuplicate(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	l, err := lab.Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), func() time.Time { return at })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}

	paidSubscription := func(customer string) string {
		t.Helper()
		quote, err := l.CreateQuoteWithSeats(ctx, customer, "pro", 5)
		if err != nil {
			t.Fatal(err)
		}
		accepted, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, customer+"-accept")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := l.DispatchCapture(ctx, accepted.OperationID, ""); err != nil {
			t.Fatal(err)
		}
		return accepted.SubscriptionID
	}
	firstSub := paidSubscription("usage-page-first")
	otherSub := paidSubscription("usage-page-other")
	record := func(subID, eventID string) {
		t.Helper()
		event, err := l.RecordUsage(ctx, "worker", eventID, subID, "tasks", 1, at)
		if err != nil {
			t.Fatal(err)
		}
		if event.PeriodIndex != 0 {
			t.Fatalf("%s period index=%d", eventID, event.PeriodIndex)
		}
	}
	record(firstSub, "usage-first")
	record(otherSub, "usage-other")
	record(firstSub, "usage-second")

	const token = "usage-page-session"
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
	get := func(cursor string) page {
		t.Helper()
		query := url.Values{"limit": {"1"}, "subscription_id": {firstSub}, "period_index": {"0"}, "source": {"worker"}}
		if cursor != "" {
			query.Set("cursor", cursor)
		}
		request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/admin/api/usage-events?"+query.Encode(), nil)
		request.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("usage page status=%d body=%s", response.Code, response.Body.String())
		}
		var result page
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}

	first := get("")
	if first.Total != 2 || len(first.Items) != 1 || first.Items[0]["EventID"] != "usage-first" || first.NextCursor == "" {
		t.Fatalf("first usage page %+v", first)
	}
	record(firstSub, "usage-third")
	record(otherSub, "usage-other-later")
	got := []string{first.Items[0]["EventID"].(string)}
	cursor := first.NextCursor
	for cursor != "" {
		next := get(cursor)
		if next.Total != 3 || len(next.Items) != 1 {
			t.Fatalf("usage page after insert %+v", next)
		}
		got = append(got, next.Items[0]["EventID"].(string))
		cursor = next.NextCursor
		if len(got) > 3 {
			t.Fatalf("usage cursor repeated rows: %v", got)
		}
	}
	want := []string{"usage-first", "usage-second", "usage-third"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("usage pages got %v, want %v", got, want)
	}
}
