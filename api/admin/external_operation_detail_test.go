package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"billforge/lab"
)

type externalOperationResponse struct {
	Operation struct {
		ID           string `json:"id"`
		Status       string `json:"status"`
		OutboxStatus string `json:"outbox_status"`
		AmountMinor  string `json:"amount_minor"`
		Currency     string `json:"currency"`
	} `json:"operation"`
	ObservedAt string `json:"observed_at"`
}

func TestExternalOperationDetailsExposeCurrentStateWithoutProviderKeys(t *testing.T) {
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
	quote, err := l.CreateQuote(ctx, "external-detail-customer", "basic")
	if err != nil {
		t.Fatal(err)
	}
	paid, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "external-detail-checkout")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{lab: l}
	read := func(id, path string, handler func(http.ResponseWriter, *http.Request)) externalOperationResponse {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.SetPathValue("id", id)
		w := httptest.NewRecorder()
		handler(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", path, w.Code, w.Body.String())
		}
		var body externalOperationResponse
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.ObservedAt == "" || body.Operation.ID != id || body.Operation.Currency != "USD" {
			t.Fatalf("%s state=%+v", path, body)
		}
		if strings.Contains(w.Body.String(), "provider_key") {
			t.Fatalf("%s exposed provider key", path)
		}
		return body
	}
	paymentPath := "/admin/api/payments/" + paid.OperationID
	if got := read(paid.OperationID, paymentPath, s.paymentDetail).Operation; got.Status != "created" || got.OutboxStatus != "pending" || got.AmountMinor != "2000" {
		t.Fatalf("new payment=%+v", got)
	}
	if _, err := l.DispatchCapture(ctx, paid.OperationID, ""); err != nil {
		t.Fatal(err)
	}
	if got := read(paid.OperationID, paymentPath, s.paymentDetail).Operation; got.Status != "succeeded" || got.OutboxStatus != "done" {
		t.Fatalf("dispatched payment=%+v", got)
	}
	correction, err := l.PostReduction(ctx, paid.InvoiceID, 1000, "service credit", "external-detail-reduction")
	if err != nil || len(correction.GrantIDs) != 1 {
		t.Fatalf("credit grant=%+v err=%v", correction, err)
	}
	refundID, err := l.ReserveRefund(ctx, correction.GrantIDs[0], 400, "external-detail-refund")
	if err != nil {
		t.Fatal(err)
	}
	refundPath := "/admin/api/refunds/" + refundID
	if got := read(refundID, refundPath, s.refundDetail).Operation; got.Status != "created" || got.OutboxStatus != "pending" || got.AmountMinor != "400" {
		t.Fatalf("new refund=%+v", got)
	}
	if _, err := l.DispatchRefund(ctx, refundID, ""); err != nil {
		t.Fatal(err)
	}
	if got := read(refundID, refundPath, s.refundDetail).Operation; got.Status != "succeeded" || got.OutboxStatus != "done" {
		t.Fatalf("dispatched refund=%+v", got)
	}
}

func TestExternalOperationDetailsRejectMissingSessionAndUnknownID(t *testing.T) {
	h := newTestHandler(t)
	for _, path := range []string{"/admin/api/payments/missing", "/admin/api/refunds/missing"} {
		if got := request(h, http.MethodGet, path, "").Code; got != http.StatusUnauthorized {
			t.Fatalf("%s without session=%d", path, got)
		}
	}
	dir := t.TempDir()
	l, err := lab.Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	s := &Server{lab: l}
	for _, tt := range []struct {
		path    string
		handler func(http.ResponseWriter, *http.Request)
	}{
		{"/admin/api/payments/missing", s.paymentDetail},
		{"/admin/api/refunds/missing", s.refundDetail},
	} {
		r := httptest.NewRequest(http.MethodGet, tt.path, nil)
		r.SetPathValue("id", "missing")
		w := httptest.NewRecorder()
		tt.handler(w, r)
		if w.Code != http.StatusNotFound {
			t.Fatalf("%s missing=%d: %s", tt.path, w.Code, w.Body.String())
		}
	}
}
