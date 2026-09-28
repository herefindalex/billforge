package admin

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"billforge/lab"
)

// Every action must pass through the same session, CSRF and capability gates,
// regardless of whether the action needs a preview or has a valid payload.
func TestEveryActionHTTPAdmissionGuards(t *testing.T) {
	const token = "read-only-action-matrix-session"
	const csrf = "action-matrix-csrf"
	productionRouter := newTestHandler(t)
	s := &Server{
		sessions: map[[32]byte]session{
			hashToken(token): {
				csrf:         csrf,
				expires:      time.Now().Add(time.Hour),
				idleUntil:    time.Now().Add(time.Hour),
				capabilities: []string{"read"},
			},
		},
		now: time.Now,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /admin/api/commands", s.protectedWrite(s.submitCommand))
	mux.HandleFunc("POST /admin/api/previews", s.protectedWrite(s.createPreview))

	for i := 1; i <= 49; i++ {
		actionID := fmt.Sprintf("C%02d", i)
		t.Run(actionID, func(t *testing.T) {
			for _, path := range []string{"/admin/api/commands", "/admin/api/previews"} {
				body := fmt.Sprintf(`{"action_id":%q,"payload":{}}`, actionID)
				request := func(withSession, withCSRF bool) *httptest.ResponseRecorder {
					r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080"+path, strings.NewReader(body))
					r.Header.Set("Origin", "http://127.0.0.1:8080")
					r.Header.Set("Content-Type", "application/json")
					if withSession {
						r.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
					}
					if withCSRF {
						r.Header.Set("X-CSRF-Token", csrf)
					}
					w := httptest.NewRecorder()
					if withSession {
						mux.ServeHTTP(w, r)
					} else {
						productionRouter.ServeHTTP(w, r)
					}
					return w
				}
				for _, test := range []struct {
					name        string
					withSession bool
					withCSRF    bool
					want        int
					code        string
				}{
					{"session", false, false, http.StatusUnauthorized, "SESSION_REQUIRED"},
					{"csrf", true, false, http.StatusForbidden, "CSRF_INVALID"},
					{"capability", true, true, http.StatusForbidden, "PERMISSION_DENIED"},
				} {
					t.Run(path+"/"+test.name, func(t *testing.T) {
						w := request(test.withSession, test.withCSRF)
						if w.Code != test.want || !strings.Contains(w.Body.String(), `"code":"`+test.code+`"`) {
							t.Fatalf("status=%d body=%s; want %d %s", w.Code, w.Body.String(), test.want, test.code)
						}
					})
				}
			}
		})
	}
}

func TestEveryActionRejectsUnknownPayloadBeforeAdmission(t *testing.T) {
	dir := t.TempDir()
	l, err := lab.Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	if err := l.InitAdmin(context.Background()); err != nil {
		t.Fatal(err)
	}

	const token = "full-capability-action-matrix-session"
	const csrf = "action-matrix-csrf"
	s := &Server{
		lab: l,
		sessions: map[[32]byte]session{
			hashToken(token): {
				csrf:         csrf,
				expires:      time.Now().Add(time.Hour),
				idleUntil:    time.Now().Add(time.Hour),
				capabilities: []string{"subscription.manage", "contract.manage", "finance.adjust", "catalog.publish", "migration.manage", "usage.manage", "reconciliation.repair", "operations.run", "lab.control"},
			},
		},
		now: time.Now,
	}
	handler := s.protectedWrite(s.submitCommand)
	for i := 1; i <= 49; i++ {
		actionID := fmt.Sprintf("C%02d", i)
		t.Run(actionID, func(t *testing.T) {
			body := fmt.Sprintf(`{"action_id":%q,"target_id":"missing","payload":{"unexpected_field":true}}`, actionID)
			r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/admin/api/commands", strings.NewReader(body))
			r.Header.Set("Origin", "http://127.0.0.1:8080")
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-CSRF-Token", csrf)
			r.Header.Set("Idempotency-Key", "unknown-payload-"+actionID)
			r.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
			w := httptest.NewRecorder()
			handler(w, r)
			if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), `"code":"INVALID_COMMAND"`) {
				t.Fatalf("status=%d body=%s; want 422 INVALID_COMMAND", w.Code, w.Body.String())
			}
		})
	}
	commands, _, err := l.AdminCommandsPage(context.Background(), 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(commands) != 0 {
		t.Fatalf("invalid payload admitted %d command(s)", len(commands))
	}
}
