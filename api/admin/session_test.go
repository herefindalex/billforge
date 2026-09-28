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
	"golang.org/x/crypto/bcrypt"
)

func TestBusinessClockChangeDoesNotChangeSessionDeadline(t *testing.T) {
	l, err := lab.Open(filepath.Join(t.TempDir(), "commerce.db"), filepath.Join(t.TempDir(), "provider.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	ctx := context.Background()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC)
	const token = "clock-independent-session"
	s := &Server{lab: l, username: "admin", sessions: map[[32]byte]session{
		hashToken(token): {csrf: "csrf", expires: now.Add(8 * time.Hour), idleUntil: now.Add(30 * time.Minute)},
	}, now: func() time.Time { return now }}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "session-clock-change", "C46", "", json.RawMessage(`{"mode":"fixed","value_utc":"2040-01-01T00:00:00Z"}`), "")
	if err != nil {
		t.Fatal(err)
	}
	if command, err = l.AdminExecuteCommand(ctx, command.ID); err != nil || command.Status != "succeeded" {
		t.Fatalf("change business clock: %+v %v", command, err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/admin/api/session", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
	w := httptest.NewRecorder()
	s.currentSession(w, request)
	if w.Code != http.StatusOK {
		t.Fatalf("business clock change expired session: %d %s", w.Code, w.Body.String())
	}
	now = now.Add(31 * time.Minute)
	w = httptest.NewRecorder()
	s.currentSession(w, request)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("wall-clock idle deadline ignored: %d %s", w.Code, w.Body.String())
	}
}

func TestSessionIdleAndAbsoluteExpiry(t *testing.T) {
	now := time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC)
	token := "test-session-token"
	s := &Server{sessions: map[[32]byte]session{hashToken(token): {csrf: "csrf", expires: now.Add(8 * time.Hour), idleUntil: now.Add(30 * time.Minute)}}, now: func() time.Time { return now }}
	r := httptest.NewRequest("GET", "http://127.0.0.1:8080/admin/api/overview", nil)
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
	if _, ok := s.lookupSession(r); !ok {
		t.Fatal("new session rejected")
	}
	now = now.Add(29 * time.Minute)
	if _, ok := s.lookupSession(r); !ok {
		t.Fatal("active session rejected")
	}
	now = now.Add(31 * time.Minute)
	if _, ok := s.lookupSession(r); ok {
		t.Fatal("idle session remained valid")
	}
	s.sessions[hashToken(token)] = session{csrf: "csrf", expires: now.Add(time.Minute), idleUntil: now.Add(time.Hour)}
	now = now.Add(2 * time.Minute)
	if _, ok := s.lookupSession(r); ok {
		t.Fatal("absolute expiry ignored")
	}
}

func TestSessionHTTPExpiresOnIdleAndAbsoluteDeadline(t *testing.T) {
	dir := t.TempDir()
	l, err := lab.Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	now := time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC)
	h, err := newWithClock(l, Config{Username: "admin", Password: "test-password-long-enough"}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	login := func() *http.Cookie {
		t.Helper()
		csrf := request(h, http.MethodGet, "/admin/api/session/csrf", "")
		if csrf.Code != http.StatusOK {
			t.Fatalf("login nonce status = %d", csrf.Code)
		}
		var nonce struct {
			Token string `json:"csrf_token"`
		}
		if err := json.Unmarshal(csrf.Body.Bytes(), &nonce); err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/admin/api/session", strings.NewReader(`{"username":"admin","password":"test-password-long-enough"}`))
		r.Header.Set("Origin", "http://127.0.0.1:8080")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", nonce.Token)
		r.AddCookie(csrf.Result().Cookies()[0])
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("login status = %d: %s", w.Code, w.Body.String())
		}
		for _, cookie := range w.Result().Cookies() {
			if cookie.Name == sessionCookie {
				return cookie
			}
		}
		t.Fatal("login did not issue a session cookie")
		return nil
	}
	check := func(cookie *http.Cookie, want int) {
		t.Helper()
		if got := request(h, http.MethodGet, "/admin/api/overview", "", cookie).Code; got != want {
			t.Fatalf("at %s: overview status = %d, want %d", now.Format(time.RFC3339), got, want)
		}
	}
	first := login()
	check(first, http.StatusOK)
	now = now.Add(29 * time.Minute)
	check(first, http.StatusOK)
	now = now.Add(31 * time.Minute)
	check(first, http.StatusUnauthorized)

	second := login()
	started := now
	for elapsed := 29 * time.Minute; elapsed < sessionLifetime; elapsed += 29 * time.Minute {
		now = started.Add(elapsed)
		check(second, http.StatusOK)
	}
	now = started.Add(sessionLifetime)
	check(second, http.StatusUnauthorized)
}

func TestLoginRateLimitsAndPreAuthRotation(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("test-password-long-enough"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	newServer := func() *Server {
		return &Server{
			username: "admin", passwordHash: hash,
			sessions: make(map[[32]byte]session), attempts: make(map[string]loginAttempt),
			preAuth: map[[32]byte]time.Time{hashToken("login-nonce"): now.Add(preAuthLifetime)},
			now:     func() time.Time { return now },
		}
	}
	login := func(s *Server, username, password string) *httptest.ResponseRecorder {
		t.Helper()
		body, err := json.Marshal(map[string]string{"username": username, "password": password})
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/admin/api/session", strings.NewReader(string(body)))
		r.RemoteAddr = "127.0.0.1:54321"
		r.Header.Set("Origin", "http://127.0.0.1:8080")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", "login-nonce")
		r.AddCookie(&http.Cookie{Name: preAuthCookie, Value: "login-nonce"})
		w := httptest.NewRecorder()
		s.login(w, r)
		return w
	}

	s := newServer()
	var wrongBody string
	for i := 0; i < 5; i++ {
		w := login(s, "admin", "wrong-password")
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("failed login %d: %d %s", i+1, w.Code, w.Body.String())
		}
		wrongBody = w.Body.String()
	}
	if w := login(s, "admin", "test-password-long-enough"); w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") == "" {
		t.Fatalf("sixth per-user attempt bypassed limit: %d %s", w.Code, w.Body.String())
	}
	if w := login(s, "unknown", "test-password-long-enough"); w.Code != http.StatusUnauthorized || w.Body.String() != wrongBody {
		t.Fatalf("unknown user disclosed a different failure: %d %s", w.Code, w.Body.String())
	}
	now = now.Add(15*time.Minute + time.Second)
	s.preAuth[hashToken("login-nonce")] = now.Add(preAuthLifetime)
	if w := login(s, "admin", "test-password-long-enough"); w.Code != http.StatusOK {
		t.Fatalf("login did not recover after limit window: %d %s", w.Code, w.Body.String())
	}
	if w := login(s, "admin", "test-password-long-enough"); w.Code != http.StatusForbidden {
		t.Fatalf("successful login did not rotate pre-auth nonce: %d %s", w.Code, w.Body.String())
	}

	now = time.Now().UTC().Truncate(time.Second)
	s = newServer()
	for i := 0; i < 20; i++ {
		w := login(s, "unknown-"+string(rune('a'+i)), "wrong-password")
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("global attempt %d: %d %s", i+1, w.Code, w.Body.String())
		}
	}
	if w := login(s, "admin", "test-password-long-enough"); w.Code != http.StatusTooManyRequests {
		t.Fatalf("global limit bypassed: %d %s", w.Code, w.Body.String())
	}
	now = now.Add(time.Minute + time.Second)
	if w := login(s, "admin", "test-password-long-enough"); w.Code != http.StatusOK {
		t.Fatalf("global limit did not reset: %d %s", w.Code, w.Body.String())
	}
}

func newTestHandler(t *testing.T) http.Handler {
	t.Helper()
	dir := t.TempDir()
	l, err := lab.Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	h, err := New(l, Config{Username: "admin", Password: "test-password-long-enough"})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func request(h http.Handler, method, path, body string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://127.0.0.1:8080"+path, strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:54321"
	for _, c := range cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestAdminSessionAndCSRF(t *testing.T) {
	h := newTestHandler(t)
	if got := request(h, "GET", "/admin/api/overview", "").Code; got != http.StatusUnauthorized {
		t.Fatalf("unauthenticated overview = %d", got)
	}
	for _, path := range []string{"/admin/api/customers", "/admin/api/customers/example", "/admin/api/subscriptions/example"} {
		if got := request(h, "GET", path, "").Code; got != http.StatusUnauthorized {
			t.Fatalf("unauthenticated %s = %d", path, got)
		}
	}
	csrfResponse := request(h, "GET", "/admin/api/session/csrf", "")
	if csrfResponse.Code != http.StatusOK {
		t.Fatalf("csrf = %d", csrfResponse.Code)
	}
	var nonce struct {
		Token string `json:"csrf_token"`
	}
	if err := json.Unmarshal(csrfResponse.Body.Bytes(), &nonce); err != nil {
		t.Fatal(err)
	}
	preCookie := csrfResponse.Result().Cookies()[0]
	login := func(origin, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "http://127.0.0.1:8080/admin/api/session", strings.NewReader(`{"username":"admin","password":"test-password-long-enough"}`))
		r.Header.Set("Origin", origin)
		r.Header.Set("X-CSRF-Token", token)
		r.Header.Set("Content-Type", "application/json")
		r.AddCookie(preCookie)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if got := login("http://evil.example", nonce.Token).Code; got != http.StatusForbidden {
		t.Fatalf("cross-origin login = %d", got)
	}
	if got := login("http://127.0.0.1:8080", "wrong").Code; got != http.StatusForbidden {
		t.Fatalf("invalid csrf login = %d", got)
	}
	response := login("http://127.0.0.1:8080", nonce.Token)
	if response.Code != http.StatusOK {
		t.Fatalf("login = %d: %s", response.Code, response.Body.String())
	}
	var signedIn struct {
		CSRF string `json:"csrf_token"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &signedIn); err != nil {
		t.Fatal(err)
	}
	var sessionCookie *http.Cookie
	for _, c := range response.Result().Cookies() {
		if c.Name == "billforge_admin_session" {
			sessionCookie = c
		}
	}
	if sessionCookie == nil || !sessionCookie.HttpOnly || sessionCookie.SameSite != http.SameSiteStrictMode {
		t.Fatal("session cookie protections missing")
	}
	if got := request(h, "GET", "/admin/api/overview", "", sessionCookie).Code; got != http.StatusOK {
		t.Fatalf("authenticated overview = %d", got)
	}
	wrongType := httptest.NewRequest("POST", "http://127.0.0.1:8080/admin/api/commands", strings.NewReader(`{}`))
	wrongType.AddCookie(sessionCookie)
	wrongType.Header.Set("Origin", "http://127.0.0.1:8080")
	wrongType.Header.Set("X-CSRF-Token", signedIn.CSRF)
	wrongTypeResult := httptest.NewRecorder()
	h.ServeHTTP(wrongTypeResult, wrongType)
	if wrongTypeResult.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("non-JSON command = %d", wrongTypeResult.Code)
	}
	first := request(h, "GET", "/admin/api/prices?limit=1", "", sessionCookie)
	if first.Code != http.StatusOK {
		t.Fatalf("first prices page = %d: %s", first.Code, first.Body.String())
	}
	var prices struct {
		Items []map[string]any `json:"items"`
		Next  string           `json:"next_cursor"`
		Total int              `json:"total"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &prices); err != nil {
		t.Fatal(err)
	}
	if prices.Total != 2 || len(prices.Items) != 1 || prices.Next == "" {
		t.Fatalf("unexpected first page: %+v", prices)
	}
	if _, ok := prices.Items[0]["FixedMinor"].(string); !ok {
		t.Fatalf("money must be a decimal string: %+v", prices.Items[0])
	}
	second := request(h, "GET", "/admin/api/prices?limit=1&cursor="+prices.Next, "", sessionCookie)
	if second.Code != http.StatusOK {
		t.Fatalf("second prices page = %d: %s", second.Code, second.Body.String())
	}
	if err := json.Unmarshal(second.Body.Bytes(), &prices); err != nil {
		t.Fatal(err)
	}
	if len(prices.Items) != 1 || prices.Next != "" {
		t.Fatalf("unexpected second page: %+v", prices)
	}
	postCommand := func(key, token, customer string) *httptest.ResponseRecorder {
		body := `{"action_id":"C01","payload":{"customer_id":"` + customer + `","plan_id":"basic","seats":"0"}}`
		r := httptest.NewRequest("POST", "http://127.0.0.1:8080/admin/api/commands", strings.NewReader(body))
		r.AddCookie(sessionCookie)
		r.Header.Set("Origin", "http://127.0.0.1:8080")
		r.Header.Set("X-CSRF-Token", token)
		r.Header.Set("Idempotency-Key", key)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if got := postCommand("create-quote-001", "wrong", "cust-1").Code; got != http.StatusForbidden {
		t.Fatalf("command without csrf = %d", got)
	}
	created := postCommand("create-quote-001", signedIn.CSRF, "cust-1")
	if created.Code != http.StatusAccepted {
		t.Fatalf("command = %d: %s", created.Code, created.Body.String())
	}
	var firstCommand lab.AdminCommand
	if err := json.Unmarshal(created.Body.Bytes(), &firstCommand); err != nil {
		t.Fatal(err)
	}
	if firstCommand.Status != "succeeded" || firstCommand.ID == "" {
		t.Fatalf("command result: %+v", firstCommand)
	}
	if got := postCommand("create-quote-001", signedIn.CSRF, "cust-1").Code; got != http.StatusOK {
		t.Fatalf("replay = %d", got)
	}
	if got := postCommand("create-quote-001", signedIn.CSRF, "cust-2").Code; got != http.StatusConflict {
		t.Fatalf("changed intent = %d", got)
	}
	var quoteRefs map[string]string
	if err := json.Unmarshal(firstCommand.ResultRefs, &quoteRefs); err != nil {
		t.Fatal(err)
	}
	previewInput, err := json.Marshal(map[string]any{"action_id": "C02", "target_id": quoteRefs["quote_id"], "payload": map[string]string{"fingerprint": quoteRefs["fingerprint"]}})
	if err != nil {
		t.Fatal(err)
	}
	previewRequest := httptest.NewRequest("POST", "http://127.0.0.1:8080/admin/api/previews", strings.NewReader(string(previewInput)))
	previewRequest.AddCookie(sessionCookie)
	previewRequest.Header.Set("Origin", "http://127.0.0.1:8080")
	previewRequest.Header.Set("X-CSRF-Token", signedIn.CSRF)
	previewRequest.Header.Set("Content-Type", "application/json")
	previewResponse := httptest.NewRecorder()
	h.ServeHTTP(previewResponse, previewRequest)
	if previewResponse.Code != http.StatusOK {
		t.Fatalf("accept preview = %d: %s", previewResponse.Code, previewResponse.Body.String())
	}
	var preview lab.AdminPreview
	if err := json.Unmarshal(previewResponse.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	acceptInput, err := json.Marshal(map[string]any{"action_id": "C02", "target_id": quoteRefs["quote_id"], "preview_id": preview.ID, "payload": map[string]string{"fingerprint": quoteRefs["fingerprint"]}})
	if err != nil {
		t.Fatal(err)
	}
	acceptRequest := httptest.NewRequest("POST", "http://127.0.0.1:8080/admin/api/commands", strings.NewReader(string(acceptInput)))
	acceptRequest.AddCookie(sessionCookie)
	acceptRequest.Header.Set("Origin", "http://127.0.0.1:8080")
	acceptRequest.Header.Set("X-CSRF-Token", signedIn.CSRF)
	acceptRequest.Header.Set("Idempotency-Key", "accept-quote-001")
	acceptRequest.Header.Set("Content-Type", "application/json")
	acceptResponse := httptest.NewRecorder()
	h.ServeHTTP(acceptResponse, acceptRequest)
	if acceptResponse.Code != http.StatusAccepted {
		t.Fatalf("accept command = %d: %s", acceptResponse.Code, acceptResponse.Body.String())
	}
	var acceptedCommand lab.AdminCommand
	if err := json.Unmarshal(acceptResponse.Body.Bytes(), &acceptedCommand); err != nil {
		t.Fatal(err)
	}
	if acceptedCommand.Status != "succeeded" {
		t.Fatalf("accept status: %+v", acceptedCommand)
	}
	if got := request(h, "DELETE", "/admin/api/session", "", sessionCookie).Code; got != http.StatusForbidden {
		t.Fatalf("logout without csrf = %d", got)
	}
	r := httptest.NewRequest("DELETE", "http://127.0.0.1:8080/admin/api/session", nil)
	r.AddCookie(sessionCookie)
	r.Header.Set("Origin", "http://127.0.0.1:8080")
	r.Header.Set("X-CSRF-Token", signedIn.CSRF)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("logout = %d", w.Code)
	}
	if got := request(h, "GET", "/admin/api/overview", "", sessionCookie).Code; got != http.StatusUnauthorized {
		t.Fatalf("old session after logout = %d", got)
	}
}

func TestAdminCredentialsRejected(t *testing.T) {
	dir := t.TempDir()
	l, err := lab.Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	for _, cfg := range []Config{{Username: "", Password: "long-enough-password"}, {Username: "admin", Password: "short"}} {
		if _, err := New(l, cfg); err == nil {
			t.Fatalf("accepted invalid config: username length %d, password length %d", len(cfg.Username), len(cfg.Password))
		}
	}
}
