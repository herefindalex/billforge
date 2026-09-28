package admin

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"billforge/lab"
)

func TestInvoiceDetailSessionGuardAndNotFound(t *testing.T) {
	h := newTestHandler(t)
	if got := request(h, http.MethodGet, "/admin/api/invoices/example", "").Code; got != http.StatusUnauthorized {
		t.Fatalf("unauthenticated invoice status = %d", got)
	}
	dir := t.TempDir()
	l, err := lab.Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	s := &Server{lab: l}
	r := httptest.NewRequest(http.MethodGet, "/admin/api/invoices/missing", nil)
	r.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	s.invoiceDetail(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing invoice status = %d: %s", w.Code, w.Body.String())
	}
}
