package admin

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"billforge/lab"
)

func TestDiscrepancyDetailSessionGuardAndNotFound(t *testing.T) {
	h := newTestHandler(t)
	if got := request(h, http.MethodGet, "/admin/api/discrepancies/example", "").Code; got != http.StatusUnauthorized {
		t.Fatalf("unauthenticated discrepancy status = %d", got)
	}
	dir := t.TempDir()
	l, err := lab.Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	s := &Server{lab: l}
	r := httptest.NewRequest(http.MethodGet, "/admin/api/discrepancies/missing", nil)
	r.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	s.discrepancyDetail(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing discrepancy status = %d: %s", w.Code, w.Body.String())
	}
}
