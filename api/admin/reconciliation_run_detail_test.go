package admin

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"billforge/lab"
)

func TestReconciliationRunDetailSessionGuardAndReadErrors(t *testing.T) {
	h := newTestHandler(t)
	if got := request(h, http.MethodGet, "/admin/api/reconciliation-runs/example", "").Code; got != http.StatusUnauthorized {
		t.Fatalf("unauthenticated run status = %d", got)
	}
	dir := t.TempDir()
	l, err := lab.Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	s := &Server{lab: l}
	for _, check := range []struct {
		path string
		want int
	}{
		{"/admin/api/reconciliation-runs/missing", http.StatusNotFound},
		{"/admin/api/reconciliation-runs/missing?cursor=invalid", http.StatusBadRequest},
		{"/admin/api/reconciliation-runs/missing?limit=101", http.StatusBadRequest},
	} {
		r := httptest.NewRequest(http.MethodGet, check.path, nil)
		r.SetPathValue("id", "missing")
		w := httptest.NewRecorder()
		s.reconciliationRunDetail(w, r)
		if w.Code != check.want {
			t.Fatalf("%s status = %d, want %d: %s", check.path, w.Code, check.want, w.Body.String())
		}
	}
}
