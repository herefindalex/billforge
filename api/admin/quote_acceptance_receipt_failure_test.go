package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"billforge/lab"
)

func TestQuoteAcceptanceReceiptFailureRecoversOriginalHTTPCommand(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	commercePath := filepath.Join(dir, "commerce.db")
	providerPath := filepath.Join(dir, "provider.db")
	l, err := lab.Open(commercePath, providerPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	quote, err := l.CreateQuote(ctx, "accept-http-receipt-fault", "basic")
	if err != nil {
		t.Fatal(err)
	}
	intent, err := json.Marshal(map[string]string{"fingerprint": quote.Fingerprint})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C02", quote.ID, intent)
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := sql.Open("sqlite3", commercePath)
	if err != nil {
		t.Fatal(err)
	}
	defer inspection.Close()
	provider, err := sql.Open("sqlite3", providerPath)
	if err != nil {
		t.Fatal(err)
	}
	defer provider.Close()
	if _, err := inspection.Exec(`CREATE TRIGGER fail_accept_admin_receipt BEFORE INSERT ON admin_command_receipts BEGIN SELECT RAISE(ABORT,'injected receipt failure'); END`); err != nil {
		t.Fatal(err)
	}

	const token = "accept-receipt-fault-session"
	const csrf = "accept-receipt-fault-csrf"
	s := &Server{
		lab: l,
		sessions: map[[32]byte]session{hashToken(token): {
			csrf: csrf, expires: time.Now().Add(time.Hour), idleUntil: time.Now().Add(time.Hour),
			capabilities: []string{"read", "subscription.manage"},
		}},
		now: time.Now,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /admin/api/commands", s.protectedWrite(s.submitCommand))
	post := func(targetID, previewID string, payload json.RawMessage) *httptest.ResponseRecorder {
		t.Helper()
		body, err := json.Marshal(map[string]any{
			"action_id": "C02", "target_id": targetID, "preview_id": previewID,
			"payload": payload,
		})
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/admin/api/commands", strings.NewReader(string(body)))
		r.Header.Set("Origin", "http://127.0.0.1:8080")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		r.Header.Set("Idempotency-Key", "accept-http-receipt-key")
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}

	first := post(quote.ID, preview.ID, intent)
	var pending struct {
		CommandID string `json:"command_id"`
		Error     struct {
			Code      string `json:"code"`
			Retryable bool   `json:"retryable"`
		} `json:"error"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &pending); err != nil || first.Code != http.StatusServiceUnavailable || pending.Error.Code != "COMMAND_PENDING_RETRY" || !pending.Error.Retryable || pending.CommandID == "" {
		t.Fatalf("receipt failure status=%d body=%s decode=%v", first.Code, first.Body.String(), err)
	}
	checkFacts := func(want int64) {
		t.Helper()
		for _, item := range []struct {
			name  string
			query string
		}{
			{"subscriptions", `SELECT COUNT(*) FROM subscriptions WHERE quote_id=?`},
			{"invoices", `SELECT COUNT(*) FROM invoices WHERE subscription_id IN (SELECT id FROM subscriptions WHERE quote_id=?)`},
			{"billing_periods", `SELECT COUNT(*) FROM billing_periods WHERE subscription_id IN (SELECT id FROM subscriptions WHERE quote_id=?)`},
			{"payment_operations", `SELECT COUNT(*) FROM payment_operations WHERE invoice_id IN (SELECT i.id FROM invoices i JOIN subscriptions s ON s.id=i.subscription_id WHERE s.quote_id=?)`},
			{"capture_outbox", `SELECT COUNT(*) FROM outbox WHERE kind='capture' AND object_id IN (SELECT p.id FROM payment_operations p JOIN invoices i ON i.id=p.invoice_id JOIN subscriptions s ON s.id=i.subscription_id WHERE s.quote_id=?)`},
			{"receipts", `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`},
		} {
			arg := any(quote.ID)
			if item.name == "receipts" {
				arg = pending.CommandID
			}
			var got int64
			if err := inspection.QueryRow(item.query, arg).Scan(&got); err != nil || got != want {
				t.Fatalf("%s count=%d want=%d err=%v", item.name, got, want, err)
			}
		}
		var commands, captures int64
		if err := inspection.QueryRow(`SELECT COUNT(*) FROM admin_commands WHERE idempotency_key='accept-http-receipt-key'`).Scan(&commands); err != nil || commands != 1 {
			t.Fatalf("commands=%d want=1 err=%v", commands, err)
		}
		if err := provider.QueryRow(`SELECT COUNT(*) FROM captures`).Scan(&captures); err != nil || captures != 0 {
			t.Fatalf("provider captures=%d want=0 err=%v", captures, err)
		}
		if want == 0 {
			return
		}
		var subscriptionID, invoiceID, operationID, invoiceCurrency, operationCurrency, operationStatus, outboxStatus string
		var invoiceTotal, operationAmount, lineTotal int64
		if err := inspection.QueryRow(`SELECT s.id,i.id,i.total_minor,i.currency,p.id,p.amount_minor,p.currency,p.status,b.status
			FROM subscriptions s JOIN invoices i ON i.subscription_id=s.id
			JOIN payment_operations p ON p.invoice_id=i.id
			JOIN outbox b ON b.object_id=p.id AND b.kind='capture'
			WHERE s.quote_id=?`, quote.ID).Scan(&subscriptionID, &invoiceID, &invoiceTotal, &invoiceCurrency, &operationID, &operationAmount, &operationCurrency, &operationStatus, &outboxStatus); err != nil {
			t.Fatal(err)
		}
		if err := inspection.QueryRow(`SELECT COALESCE(SUM(amount_minor),0) FROM invoice_lines WHERE invoice_id=?`, invoiceID).Scan(&lineTotal); err != nil {
			t.Fatal(err)
		}
		if invoiceTotal != quote.AmountMinor || operationAmount != quote.AmountMinor || lineTotal != quote.AmountMinor || invoiceCurrency != quote.Currency || operationCurrency != quote.Currency || operationStatus != "created" || outboxStatus != "pending" {
			t.Fatalf("quote=%d %s invoice=%d %s lines=%d operation=%d %s status=%s outbox=%s", quote.AmountMinor, quote.Currency, invoiceTotal, invoiceCurrency, lineTotal, operationAmount, operationCurrency, operationStatus, outboxStatus)
		}
		var refsJSON string
		if err := inspection.QueryRow(`SELECT result_refs_json FROM admin_command_receipts WHERE command_id=?`, pending.CommandID).Scan(&refsJSON); err != nil {
			t.Fatal(err)
		}
		var refs map[string]string
		if err := json.Unmarshal([]byte(refsJSON), &refs); err != nil {
			t.Fatal(err)
		}
		if refs["subscription_id"] != subscriptionID || refs["invoice_id"] != invoiceID || refs["operation_id"] != operationID || refs["amount_minor"] != strconv.FormatInt(quote.AmountMinor, 10) || refs["currency"] != quote.Currency {
			t.Fatalf("receipt refs=%v differ from committed subscription, invoice and payment", refs)
		}
	}
	checkFacts(0)
	if _, err := inspection.Exec(`DROP TRIGGER fail_accept_admin_receipt`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		recovered := post(quote.ID, preview.ID, intent)
		var command struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		}
		if err := json.Unmarshal(recovered.Body.Bytes(), &command); err != nil || recovered.Code != http.StatusOK || command.ID != pending.CommandID || command.Status != "succeeded" {
			t.Fatalf("replay %d status=%d body=%s decode=%v", i, recovered.Code, recovered.Body.String(), err)
		}
		checkFacts(1)
	}
	otherQuote, err := l.CreateQuote(ctx, "accept-http-receipt-other", "basic")
	if err != nil {
		t.Fatal(err)
	}
	otherIntent, err := json.Marshal(map[string]string{"fingerprint": otherQuote.Fingerprint})
	if err != nil {
		t.Fatal(err)
	}
	otherPreview, err := l.AdminCreatePreview(ctx, "local-admin", "C02", otherQuote.ID, otherIntent)
	if err != nil {
		t.Fatal(err)
	}
	conflict := post(otherQuote.ID, otherPreview.ID, otherIntent)
	if conflict.Code != http.StatusConflict || !strings.Contains(conflict.Body.String(), `"code":"IDEMPOTENCY_CONFLICT"`) {
		t.Fatalf("same key with another valid quote status=%d body=%s", conflict.Code, conflict.Body.String())
	}
	var otherSubscriptions int64
	if err := inspection.QueryRow(`SELECT COUNT(*) FROM subscriptions WHERE quote_id=?`, otherQuote.ID).Scan(&otherSubscriptions); err != nil || otherSubscriptions != 0 {
		t.Fatalf("other quote subscriptions=%d want=0 err=%v", otherSubscriptions, err)
	}
	checkFacts(1)
}
