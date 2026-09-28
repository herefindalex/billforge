package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"billforge/lab"
)

func TestAdminPagedReadsRejectUnknownAndRepeatedQueryParameters(t *testing.T) {
	dir := t.TempDir()
	l, err := lab.Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	if err := l.InitAdmin(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := &Server{lab: l}
	checks := []struct {
		name   string
		handle http.HandlerFunc
		path   string
	}{
		{"customers", s.listCustomers, "/admin/api/customers"},
		{"commands", s.listCommands, "/admin/api/commands"},
		{"periods", s.subscriptionPeriods, "/admin/api/subscriptions/missing/periods"},
		{"timeline", s.subscriptionTimeline, "/admin/api/subscriptions/missing/timeline"},
		{"shadows", s.accountMigrationShadows, "/admin/api/account-migrations/missing/shadows"},
		{"provenance", s.accountMigrationProvenance, "/admin/api/account-migrations/missing/provenance"},
		{"run findings", s.reconciliationRunDetail, "/admin/api/reconciliation-runs/missing"},
	}
	for _, check := range checks {
		for _, query := range []string{"unknown=value", "limit=1&limit=2", "cursor=one&cursor=two", "unknown=%zz"} {
			t.Run(check.name+"/"+query, func(t *testing.T) {
				r := httptest.NewRequest(http.MethodGet, check.path+"?"+query, nil)
				r.SetPathValue("id", "missing")
				w := httptest.NewRecorder()
				check.handle(w, r)
				if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), `"code":"INVALID_FILTER"`) {
					t.Fatalf("%s returned %d: %s", r.URL, w.Code, w.Body.String())
				}
			})
		}
	}
}

func TestAdminReadinessRejectsUnknownAndRepeatedThresholds(t *testing.T) {
	s := &Server{}
	for _, query := range []string{
		"max_quote_p95_millis=5000&max_unknown_payments=0&max_open_discrepancies=0&unknown=value",
		"max_quote_p95_millis=5000&max_quote_p95_millis=10000&max_unknown_payments=0&max_open_discrepancies=0",
	} {
		r := httptest.NewRequest(http.MethodGet, "/admin/api/account-migrations/missing/readiness?"+query, nil)
		w := httptest.NewRecorder()
		s.accountMigrationReadiness(w, r)
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), `"code":"INVALID_THRESHOLDS"`) {
			t.Fatalf("%s returned %d: %s", r.URL, w.Code, w.Body.String())
		}
	}
}
