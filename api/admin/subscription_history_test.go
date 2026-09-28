package admin

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"billforge/lab"
)

func TestSubscriptionHistoryReadErrorsAndSessionGuard(t *testing.T) {
	h := newTestHandler(t)
	for _, path := range []string{
		"/admin/api/subscriptions/example/periods",
		"/admin/api/subscriptions/example/timeline",
		"/admin/api/subscriptions/example/entitlement",
	} {
		if got := request(h, http.MethodGet, path, "").Code; got != http.StatusUnauthorized {
			t.Fatalf("unauthenticated %s = %d", path, got)
		}
	}
	dir := t.TempDir()
	l, err := lab.Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	s := &Server{lab: l}
	missingTimeCursor := base64.RawURLEncoding.EncodeToString([]byte(`{"kind":"entitlement_updated","reference":""}`))
	emptyReferenceCursor := base64.RawURLEncoding.EncodeToString([]byte(`{"at_nano":1,"kind":"entitlement_updated","reference":""}`))
	checks := []struct {
		path   string
		handle http.HandlerFunc
		status int
		code   string
	}{
		{"/admin/api/subscriptions/missing/periods", s.subscriptionPeriods, http.StatusNotFound, "NOT_FOUND"},
		{"/admin/api/subscriptions/missing/timeline", s.subscriptionTimeline, http.StatusNotFound, "NOT_FOUND"},
		{"/admin/api/subscriptions/missing/entitlement", s.subscriptionEntitlement, http.StatusNotFound, "NOT_FOUND"},
		{"/admin/api/subscriptions/missing/periods?cursor=invalid", s.subscriptionPeriods, http.StatusBadRequest, "INVALID_CURSOR"},
		{"/admin/api/subscriptions/missing/timeline?cursor=invalid", s.subscriptionTimeline, http.StatusBadRequest, "INVALID_CURSOR"},
		{"/admin/api/subscriptions/missing/timeline?cursor=" + missingTimeCursor, s.subscriptionTimeline, http.StatusBadRequest, "INVALID_CURSOR"},
		{"/admin/api/subscriptions/missing/timeline?cursor=" + emptyReferenceCursor, s.subscriptionTimeline, http.StatusNotFound, "NOT_FOUND"},
		{"/admin/api/subscriptions/missing/periods?limit=101", s.subscriptionPeriods, http.StatusBadRequest, "INVALID_LIMIT"},
	}
	for _, check := range checks {
		r := httptest.NewRequest(http.MethodGet, check.path, nil)
		r.SetPathValue("id", "missing")
		w := httptest.NewRecorder()
		check.handle(w, r)
		if w.Code != check.status {
			t.Fatalf("%s status = %d, want %d: %s", check.path, w.Code, check.status, w.Body.String())
		}
		var body struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.Error.Code != check.code {
			t.Fatalf("%s error code = %q, want %q", check.path, body.Error.Code, check.code)
		}
	}
}
