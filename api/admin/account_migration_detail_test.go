package admin

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"billforge/lab"
)

func TestAccountMigrationDetailAndReadinessGuardInputs(t *testing.T) {
	h := newTestHandler(t)
	for _, path := range []string{"/admin/api/account-migrations/example", "/admin/api/account-migrations/example/readiness?max_quote_p95_millis=5000&max_unknown_payments=0&max_open_discrepancies=0", "/admin/api/account-migrations/example/entitlements/subscription", "/admin/api/account-migrations/example/shadows", "/admin/api/account-migrations/example/provenance"} {
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
	checks := []struct {
		path   string
		handle http.HandlerFunc
		want   int
	}{
		{"/admin/api/account-migrations/missing", s.accountMigrationDetail, http.StatusNotFound},
		{"/admin/api/account-migrations/missing/readiness", s.accountMigrationReadiness, http.StatusBadRequest},
		{"/admin/api/account-migrations/missing/readiness?max_quote_p95_millis=0&max_unknown_payments=0&max_open_discrepancies=0", s.accountMigrationReadiness, http.StatusBadRequest},
		{"/admin/api/account-migrations/missing/readiness?max_quote_p95_millis=5000&max_unknown_payments=0&max_open_discrepancies=0", s.accountMigrationReadiness, http.StatusNotFound},
		{"/admin/api/account-migrations/missing/entitlements/subscription", s.accountMigrationEntitlement, http.StatusNotFound},
		{"/admin/api/account-migrations/missing/shadows", s.accountMigrationShadows, http.StatusNotFound},
		{"/admin/api/account-migrations/missing/provenance", s.accountMigrationProvenance, http.StatusNotFound},
		{"/admin/api/account-migrations/missing/shadows?cursor=invalid", s.accountMigrationShadows, http.StatusBadRequest},
		{"/admin/api/account-migrations/missing/provenance?cursor=***", s.accountMigrationProvenance, http.StatusBadRequest},
	}
	for _, check := range checks {
		r := httptest.NewRequest(http.MethodGet, check.path, nil)
		r.SetPathValue("id", "missing")
		r.SetPathValue("subscriptionId", "subscription")
		w := httptest.NewRecorder()
		check.handle(w, r)
		if w.Code != check.want {
			t.Fatalf("%s status = %d, want %d: %s", check.path, w.Code, check.want, w.Body.String())
		}
	}
}
