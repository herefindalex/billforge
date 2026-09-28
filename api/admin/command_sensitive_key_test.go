package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"billforge/lab"
)

func TestCommandReadsNeverExposeIdempotencyKey(t *testing.T) {
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
	const key = "command-sensitive-key"
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", key, "C01", "", json.RawMessage(`{"customer_id":"command-key-customer","plan_id":"basic","seats":"0"}`), "")
	if err != nil {
		t.Fatal(err)
	}

	const readToken = "command-key-read-session"
	const financeToken = "command-key-finance-session"
	const subscriptionToken = "command-key-subscription-session"
	s := &Server{
		lab: l,
		sessions: map[[32]byte]session{
			hashToken(readToken):         {capabilities: []string{"read"}, expires: time.Now().Add(time.Hour), idleUntil: time.Now().Add(time.Hour)},
			hashToken(financeToken):      {capabilities: []string{"read", "finance.adjust"}, expires: time.Now().Add(time.Hour), idleUntil: time.Now().Add(time.Hour)},
			hashToken(subscriptionToken): {capabilities: []string{"read", "subscription.manage"}, expires: time.Now().Add(time.Hour), idleUntil: time.Now().Add(time.Hour)},
		},
		now: time.Now,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/api/commands", s.protected(s.listCommands))
	mux.HandleFunc("GET /admin/api/commands/{id}", s.protected(s.getCommand))
	get := func(path, token string) map[string]any {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080"+path, nil)
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s status=%d body=%s", path, w.Code, w.Body.String())
		}
		var result map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	for _, path := range []string{"/admin/api/commands", "/admin/api/commands/" + command.ID} {
		for _, token := range []string{readToken, financeToken, subscriptionToken} {
			result := get(path, token)
			if items, ok := result["items"].([]any); ok {
				if len(items) != 1 {
					t.Fatalf("GET %s items=%d", path, len(items))
				}
				result = items[0].(map[string]any)
			}
			if result["id"] != command.ID {
				t.Fatalf("GET %s changed command identity: %+v", path, result)
			}
			if _, visible := result["idempotency_key"]; visible {
				t.Fatalf("command key visible at %s: %+v", path, result)
			}
		}
	}
	stored, err := l.AdminCommand(ctx, command.ID)
	if err != nil || stored.IdempotencyKey != key {
		t.Fatalf("read redaction changed stored command key: %+v error=%v", stored, err)
	}
}
