package admin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"billforge/lab"
)

func TestInvoiceHistorySessionCursorAndReadContracts(t *testing.T) {
	h := newTestHandler(t)
	if got := request(h, http.MethodGet, "/admin/api/invoices/example/history/corrections", "").Code; got != http.StatusUnauthorized {
		t.Fatalf("unauthenticated history status = %d", got)
	}
	dir := t.TempDir()
	l, err := lab.Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	ctx := context.Background()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	quote, err := l.CreateQuote(ctx, "invoice-history-api", "basic")
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "invoice-history-api-accept")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{lab: l}
	call := func(id, kind, query string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "/admin/api/invoices/"+id+"/history/"+kind+query, nil)
		r.SetPathValue("id", id)
		r.SetPathValue("kind", kind)
		w := httptest.NewRecorder()
		s.invoiceHistory(w, r)
		return w
	}
	if w := call(accepted.InvoiceID, "corrections", ""); w.Code != http.StatusOK {
		t.Fatalf("empty history status = %d: %s", w.Code, w.Body.String())
	} else {
		var page struct {
			Items      []json.RawMessage `json:"items"`
			NextCursor string            `json:"next_cursor"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || len(page.Items) != 0 || page.NextCursor != "" {
			t.Fatalf("empty history page = %+v %v", page, err)
		}
	}
	if got := call("missing", "corrections", "").Code; got != http.StatusNotFound {
		t.Fatalf("missing invoice status = %d", got)
	}
	if got := call(accepted.InvoiceID, "unknown", "").Code; got != http.StatusNotFound {
		t.Fatalf("unknown kind status = %d", got)
	}
	if got := call(accepted.InvoiceID, "corrections", "?limit=101").Code; got != http.StatusBadRequest {
		t.Fatalf("oversized limit status = %d", got)
	}
	if got := call(accepted.InvoiceID, "corrections", "?cursor=bad!").Code; got != http.StatusBadRequest {
		t.Fatalf("invalid cursor status = %d", got)
	}
	foreign := base64.RawURLEncoding.EncodeToString([]byte(`{"invoice_id":"other","kind":"corrections","at_nano":1,"id":"corr_1"}`))
	if got := call(accepted.InvoiceID, "corrections", "?cursor="+foreign).Code; got != http.StatusBadRequest {
		t.Fatalf("cross-invoice cursor status = %d", got)
	}
	if got := call(accepted.InvoiceID, "refunds", "?cursor="+foreign).Code; got != http.StatusBadRequest {
		t.Fatalf("cross-kind cursor status = %d", got)
	}
}
