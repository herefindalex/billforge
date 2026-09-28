package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"billforge/lab"
)

func TestReadOnlySessionCanVerifyHeldReceiptWithoutStartingOperation(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	l, err := lab.Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	quote, err := l.CreateQuote(ctx, "held-http", "basic")
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "held-http-checkout")
	if err != nil {
		t.Fatal(err)
	}
	held, _, err := l.AdminSubmitCommand(ctx, "local-admin", "held-http-control-001", "C47", accepted.OperationID, json.RawMessage(`{"status":"definitively_failed"}`), "")
	if err != nil {
		t.Fatal(err)
	}
	if err := l.AdminResumeWithPolicy(ctx, func(string) bool { return false }); err != nil {
		t.Fatal(err)
	}
	ordinary, _, err := l.AdminSubmitCommand(ctx, "local-admin", "held-http-local-001", "C01", "", json.RawMessage(`{"customer_id":"held-local","plan_id":"basic","seats":"0"}`), "")
	if err != nil {
		t.Fatal(err)
	}
	waiting, _, err := l.AdminSubmitCommand(ctx, "local-admin", "held-http-reconcile-001", "C10", accepted.OperationID, json.RawMessage(`{}`), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.AdminExecuteCommand(ctx, waiting.ID); err != nil {
		t.Fatal(err)
	}

	const token = "read-only-held-session"
	s := &Server{
		lab: l, now: time.Now,
		sessions: map[[32]byte]session{hashToken(token): {csrf: "csrf", expires: time.Now().Add(time.Hour), idleUntil: time.Now().Add(time.Hour), capabilities: []string{"read"}}},
	}
	request := func(id string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/admin/api/commands/"+id+"/resume", strings.NewReader(`{}`))
		r.SetPathValue("id", id)
		r.Header.Set("Origin", "http://127.0.0.1:8080")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", "csrf")
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
		w := httptest.NewRecorder()
		s.protectedWrite(s.resumeCommand)(w, r)
		return w
	}
	if w := request(held.ID); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "PERMISSION_REVOKED_REVIEW") {
		t.Fatalf("held receipt check: %d %s", w.Code, w.Body.String())
	}
	if w := request(ordinary.ID); w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "PERMISSION_DENIED") {
		t.Fatalf("ordinary command bypassed capability: %d %s", w.Code, w.Body.String())
	}
	if w := request(waiting.ID); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "waiting_verification") {
		t.Fatalf("read-only verification of existing payment: %d %s", w.Code, w.Body.String())
	}
	result, err := l.AdminCommand(ctx, held.ID)
	if err != nil || result.Status != "accepted" || result.ErrorCode != "PERMISSION_REVOKED_REVIEW" {
		t.Fatalf("held command was changed without receipt: %+v, %v", result, err)
	}
}
