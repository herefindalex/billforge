package admin

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"billforge/lab"
	"golang.org/x/crypto/bcrypt"
)

const (
	sessionCookie   = "billforge_admin_session"
	preAuthCookie   = "billforge_admin_login"
	sessionLifetime = 8 * time.Hour
	sessionIdle     = 30 * time.Minute
	preAuthLifetime = 10 * time.Minute
)

var capabilities = []string{
	"read", "subscription.manage", "finance.adjust", "catalog.publish",
	"migration.manage", "reconciliation.repair", "operations.run",
	"usage.manage", "contract.manage", "lab.control",
}

type Config struct {
	Username string
	Password string
	// Nil uses the default full administrator policy.
	Capabilities []string
}

type session struct {
	csrf         string
	expires      time.Time
	idleUntil    time.Time
	capabilities []string
}

type loginAttempt struct {
	count int
	until time.Time
}

type Server struct {
	lab              *lab.Lab
	username         string
	passwordHash     []byte
	capabilities     []string
	mu               sync.Mutex
	bcryptMu         sync.Mutex
	sessions         map[[32]byte]session
	attempts         map[string]loginAttempt
	loginWindowStart time.Time
	loginCount       int
	preAuth          map[[32]byte]time.Time
	now              func() time.Time
}

func New(l *lab.Lab, cfg Config) (http.Handler, error) {
	return newWithClock(l, cfg, time.Now)
}

func newWithClock(l *lab.Lab, cfg Config, now func() time.Time) (http.Handler, error) {
	if l == nil {
		return nil, errors.New("admin lab required")
	}
	if !utf8.ValidString(cfg.Username) || utf8.RuneCountInString(cfg.Username) == 0 || utf8.RuneCountInString(cfg.Username) > 64 || strings.TrimSpace(cfg.Username) != cfg.Username || strings.IndexFunc(cfg.Username, unicode.IsControl) >= 0 {
		return nil, errors.New("admin username must be 1–64 non-control characters")
	}
	if !utf8.ValidString(cfg.Password) || len(cfg.Password) < 12 || len(cfg.Password) > 72 || strings.TrimSpace(cfg.Password) != cfg.Password {
		return nil, errors.New("admin password must be 12–72 UTF-8 bytes")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(cfg.Password), 12)
	if err != nil {
		return nil, err
	}
	allowed := cfg.Capabilities
	if allowed == nil {
		allowed = capabilities
	}
	known := make(map[string]bool, len(capabilities))
	for _, capability := range capabilities {
		known[capability] = true
	}
	selected := make(map[string]bool, len(allowed))
	for _, capability := range allowed {
		if !known[capability] || selected[capability] {
			return nil, errors.New("admin capabilities contain an unknown or duplicate value")
		}
		selected[capability] = true
	}
	if !selected["read"] {
		return nil, errors.New("admin capabilities must include read")
	}
	if err := l.InitAdmin(context.Background()); err != nil {
		return nil, err
	}
	if err := l.AdminResumeWithPolicy(context.Background(), func(actionID string) bool {
		return selected[actionCapability(actionID)]
	}); err != nil {
		return nil, err
	}
	s := &Server{
		lab: l, username: cfg.Username, passwordHash: hash, capabilities: append([]string(nil), allowed...),
		sessions: make(map[[32]byte]session), attempts: make(map[string]loginAttempt),
		preAuth: make(map[[32]byte]time.Time), now: now,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/api/session/csrf", s.csrf)
	mux.HandleFunc("POST /admin/api/session", s.login)
	mux.HandleFunc("GET /admin/api/session", s.currentSession)
	mux.HandleFunc("DELETE /admin/api/session", s.logout)
	mux.HandleFunc("GET /admin/api/overview", s.protected(s.overview))
	mux.HandleFunc("GET /admin/api/lab/clock", s.protected(s.labClock))
	mux.HandleFunc("GET /admin/api/lab/faults", s.protected(s.labFaults))
	mux.HandleFunc("GET /admin/api/{resource}", s.protected(s.listResource))
	mux.HandleFunc("GET /admin/api/customers", s.protected(s.listCustomers))
	mux.HandleFunc("GET /admin/api/customers/{id}", s.protected(s.customerDetail))
	mux.HandleFunc("GET /admin/api/quotes/{id}", s.protected(s.quoteDetail))
	mux.HandleFunc("GET /admin/api/subscriptions/{id}", s.protected(s.subscriptionDetail))
	mux.HandleFunc("GET /admin/api/subscriptions/{id}/periods", s.protected(s.subscriptionPeriods))
	mux.HandleFunc("GET /admin/api/subscriptions/{id}/timeline", s.protected(s.subscriptionTimeline))
	mux.HandleFunc("GET /admin/api/subscriptions/{id}/entitlement", s.protected(s.subscriptionEntitlement))
	mux.HandleFunc("GET /admin/api/invoices/{id}", s.protected(s.invoiceDetail))
	mux.HandleFunc("GET /admin/api/credits/{id}", s.protected(s.creditDetail))
	mux.HandleFunc("GET /admin/api/payments/{id}", s.protected(s.paymentDetail))
	mux.HandleFunc("GET /admin/api/refunds/{id}", s.protected(s.refundDetail))
	mux.HandleFunc("GET /admin/api/prices/{id}", s.protected(s.priceDetail))
	mux.HandleFunc("GET /admin/api/contracts/{id}", s.protected(s.contractDetail))
	mux.HandleFunc("GET /admin/api/usage-periods/{id}/{index}", s.protected(s.usagePeriodDetail))
	mux.HandleFunc("GET /admin/api/usage-periods/{id}/{index}/ratings", s.protected(s.usagePeriodRatings))
	mux.HandleFunc("GET /admin/api/invoices/{id}/history/{kind}", s.protected(s.invoiceHistory))
	mux.HandleFunc("GET /admin/api/discrepancies/{id}", s.protected(s.discrepancyDetail))
	mux.HandleFunc("GET /admin/api/reconciliation-runs/{id}", s.protected(s.reconciliationRunDetail))
	mux.HandleFunc("GET /admin/api/account-migrations/{id}", s.protected(s.accountMigrationDetail))
	mux.HandleFunc("GET /admin/api/account-migrations/{id}/readiness", s.protected(s.accountMigrationReadiness))
	mux.HandleFunc("GET /admin/api/account-migrations/{id}/entitlements/{subscriptionId}", s.protected(s.accountMigrationEntitlement))
	mux.HandleFunc("GET /admin/api/account-migrations/{id}/shadows", s.protected(s.accountMigrationShadows))
	mux.HandleFunc("GET /admin/api/account-migrations/{id}/provenance", s.protected(s.accountMigrationProvenance))
	mux.HandleFunc("GET /admin/api/price-migrations/{id}", s.protected(s.migrationDetail))
	mux.HandleFunc("POST /admin/api/commands", s.protectedWrite(s.submitCommand))
	mux.HandleFunc("GET /admin/api/commands", s.protected(s.listCommands))
	mux.HandleFunc("GET /admin/api/commands/{id}", s.protected(s.getCommand))
	mux.HandleFunc("GET /admin/api/jobs/{id}", s.protected(s.getJob))
	mux.HandleFunc("GET /admin/api/jobs", s.protected(s.listJobs))
	mux.HandleFunc("POST /admin/api/commands/{id}/resume", s.protectedWrite(s.resumeCommand))
	mux.HandleFunc("POST /admin/api/previews", s.protectedWrite(s.createPreview))
	mux.HandleFunc("GET /admin/api/previews/{id}", s.protected(s.getPreview))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := randomToken()
		if err != nil {
			apiError(w, http.StatusInternalServerError, "REQUEST_ID_UNAVAILABLE", "Could not create request reference")
			return
		}
		id = "req_" + id
		w.Header().Set("X-Request-ID", id)
		s.serveAdminRoute(mux, w, r.WithContext(lab.WithAdminRequestID(r.Context(), id)))
	}), nil
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func hashToken(token string) [32]byte { return sha256.Sum256([]byte(token)) }

func writeJSON(w http.ResponseWriter, code int, v any) {
	if body, ok := v.(map[string]any); ok && body["error"] != nil {
		if id := w.Header().Get("X-Request-ID"); id != "" {
			body["request_id"] = id
		}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func apiError(w http.ResponseWriter, code int, name, message string) {
	writeJSON(w, code, map[string]any{"error": map[string]any{"code": name, "message": message}})
}

func cookie(name, value string, expires time.Time, r *http.Request) *http.Cookie {
	return &http.Cookie{
		Name: name, Value: value, Path: "/admin", HttpOnly: true,
		SameSite: http.SameSiteStrictMode, Secure: r.TLS != nil,
		Expires: expires, MaxAge: max(0, int(time.Until(expires).Seconds())),
	}
}

func (s *Server) csrf(w http.ResponseWriter, r *http.Request) {
	if current, ok := s.lookupSession(r); ok {
		writeJSON(w, http.StatusOK, map[string]string{"csrf_token": current.csrf})
		return
	}
	token, err := randomToken()
	if err != nil {
		apiError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Could not create login token")
		return
	}
	now := s.now()
	s.mu.Lock()
	var oldestKey [32]byte
	oldest := now.Add(preAuthLifetime)
	for key, expiry := range s.preAuth {
		if !now.Before(expiry) {
			delete(s.preAuth, key)
		} else if expiry.Before(oldest) {
			oldestKey, oldest = key, expiry
		}
	}
	if len(s.preAuth) >= 1024 {
		delete(s.preAuth, oldestKey)
	}
	s.preAuth[hashToken(token)] = now.Add(preAuthLifetime)
	s.mu.Unlock()
	http.SetCookie(w, cookie(preAuthCookie, token, now.Add(preAuthLifetime), r))
	writeJSON(w, http.StatusOK, map[string]string{"csrf_token": token})
}

func sameOrigin(r *http.Request) bool {
	value := r.Header.Get("Origin")
	if value == "" {
		return false
	}
	u, err := url.Parse(value)
	if err != nil || u.Host != r.Host || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		return false
	}
	if r.TLS != nil {
		return u.Scheme == "https"
	}
	return u.Scheme == "http"
}

func jsonContentType(r *http.Request) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return err == nil && mediaType == "application/json"
}

func clientKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func (s *Server) preAuthValid(r *http.Request) bool {
	c, err := r.Cookie(preAuthCookie)
	if err != nil || c.Value == "" {
		return false
	}
	header := r.Header.Get("X-CSRF-Token")
	if subtle.ConstantTimeCompare([]byte(c.Value), []byte(header)) != 1 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	expires, ok := s.preAuth[hashToken(c.Value)]
	return ok && s.now().Before(expires)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) || !s.preAuthValid(r) {
		apiError(w, http.StatusForbidden, "CSRF_INVALID", "Login token or origin is invalid")
		return
	}
	if !jsonContentType(r) {
		apiError(w, http.StatusUnsupportedMediaType, "CONTENT_TYPE_REQUIRED", "Use application/json")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := dec.Decode(&input); err != nil {
		apiError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid login request")
		return
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		apiError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid login request")
		return
	}
	key := clientKey(r) + "\x00" + input.Username
	now := s.now()
	s.mu.Lock()
	attempt := s.attempts[key]
	if s.loginWindowStart.IsZero() || !now.Before(s.loginWindowStart.Add(time.Minute)) {
		s.loginWindowStart, s.loginCount = now, 0
	}
	limited := (attempt.count >= 5 && now.Before(attempt.until)) || s.loginCount >= 20
	if !limited {
		s.loginCount++
	}
	s.mu.Unlock()
	if limited {
		w.Header().Set("Retry-After", "60")
		apiError(w, http.StatusTooManyRequests, "LOGIN_RATE_LIMITED", "Too many login attempts")
		return
	}
	s.bcryptMu.Lock()
	userOK := subtle.ConstantTimeCompare([]byte(input.Username), []byte(s.username)) == 1
	passOK := bcrypt.CompareHashAndPassword(s.passwordHash, []byte(input.Password)) == nil
	s.bcryptMu.Unlock()
	if !userOK || !passOK {
		s.mu.Lock()
		attempt = s.attempts[key]
		if !now.Before(attempt.until) {
			attempt.count = 0
			attempt.until = now.Add(15 * time.Minute)
		}
		attempt.count++
		s.attempts[key] = attempt
		s.mu.Unlock()
		apiError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "Invalid username or password")
		return
	}
	token, err := randomToken()
	if err != nil {
		apiError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Could not create session")
		return
	}
	csrf, err := randomToken()
	if err != nil {
		apiError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Could not create session")
		return
	}
	expires := s.now().Add(sessionLifetime)
	s.mu.Lock()
	s.sessions[hashToken(token)] = session{csrf: csrf, expires: expires, idleUntil: s.now().Add(sessionIdle), capabilities: append([]string(nil), s.capabilities...)}
	delete(s.attempts, key)
	if c, err := r.Cookie(preAuthCookie); err == nil {
		delete(s.preAuth, hashToken(c.Value))
	}
	s.mu.Unlock()
	http.SetCookie(w, cookie(sessionCookie, token, expires, r))
	http.SetCookie(w, &http.Cookie{Name: preAuthCookie, Path: "/admin", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: r.TLS != nil})
	writeJSON(w, http.StatusOK, map[string]any{"username": s.username, "actor_id": "local-admin", "capabilities": s.capabilities, "csrf_token": csrf})
}

func (s *Server) lookupSession(r *http.Request) (session, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return session{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.sessions[hashToken(c.Value)]
	if ok && (!s.now().Before(item.expires) || !s.now().Before(item.idleUntil)) {
		delete(s.sessions, hashToken(c.Value))
		return session{}, false
	}
	if ok {
		item.idleUntil = s.now().Add(sessionIdle)
		s.sessions[hashToken(c.Value)] = item
	}
	return item, ok
}

func (s *Server) currentSession(w http.ResponseWriter, r *http.Request) {
	item, ok := s.lookupSession(r)
	if !ok {
		apiError(w, http.StatusUnauthorized, "SESSION_REQUIRED", "Sign in to continue")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"username": s.username, "actor_id": "local-admin", "capabilities": item.capabilities, "csrf_token": item.csrf})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	item, ok := s.lookupSession(r)
	if !ok {
		apiError(w, http.StatusUnauthorized, "SESSION_REQUIRED", "Sign in to continue")
		return
	}
	if !sameOrigin(r) || subtle.ConstantTimeCompare([]byte(item.csrf), []byte(r.Header.Get("X-CSRF-Token"))) != 1 {
		apiError(w, http.StatusForbidden, "CSRF_INVALID", "Session token or origin is invalid")
		return
	}
	if c, err := r.Cookie(sessionCookie); err == nil {
		s.mu.Lock()
		delete(s.sessions, hashToken(c.Value))
		s.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: "/admin", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: r.TLS != nil})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) protected(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.lookupSession(r); !ok {
			apiError(w, http.StatusUnauthorized, "SESSION_REQUIRED", "Sign in to continue")
			return
		}
		next(w, r)
	}
}

func (s *Server) protectedWrite(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		item, ok := s.lookupSession(r)
		if !ok {
			apiError(w, http.StatusUnauthorized, "SESSION_REQUIRED", "Sign in to continue")
			return
		}
		if !sameOrigin(r) || subtle.ConstantTimeCompare([]byte(item.csrf), []byte(r.Header.Get("X-CSRF-Token"))) != 1 {
			apiError(w, http.StatusForbidden, "CSRF_INVALID", "Session token or origin is invalid")
			return
		}
		if !jsonContentType(r) {
			apiError(w, http.StatusUnsupportedMediaType, "CONTENT_TYPE_REQUIRED", "Use application/json")
			return
		}
		next(w, r)
	}
}
