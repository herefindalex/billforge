package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"billforge/lab"
)

func TestCreditApplyReceiptFailureRecoversOriginalHTTPCommand(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	commercePath := filepath.Join(dir, "commerce.db")
	providerPath := filepath.Join(dir, "provider.db")
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	l, err := lab.Open(commercePath, providerPath, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	const customerID = "credit-apply-http-receipt-fault"
	firstQuote, err := l.CreateQuote(ctx, customerID, "basic")
	if err != nil {
		t.Fatal(err)
	}
	paid, err := l.AcceptQuote(ctx, firstQuote.ID, firstQuote.Fingerprint, "credit-apply-http-paid")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.DispatchCapture(ctx, paid.OperationID, ""); err != nil {
		t.Fatal(err)
	}
	correction, err := l.PostReduction(ctx, paid.InvoiceID, 1000, "service credit", "credit-apply-http-reduction")
	if err != nil || len(correction.GrantIDs) != 1 {
		t.Fatalf("correction=%+v err=%v", correction, err)
	}
	grantID := correction.GrantIDs[0]
	now = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	renewals, err := l.RunRenewals(ctx)
	if err != nil || len(renewals) != 1 {
		t.Fatalf("renewals=%+v err=%v", renewals, err)
	}
	target := renewals[0]
	targetOriginal := firstQuote.AmountMinor
	intent, err := json.Marshal(map[string]string{"invoice_id": target.InvoiceID, "amount_minor": "500"})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C12", grantID, intent)
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
	if _, err := inspection.Exec(`CREATE TRIGGER fail_credit_apply_receipt BEFORE INSERT ON admin_command_receipts BEGIN SELECT RAISE(ABORT,'injected receipt failure'); END`); err != nil {
		t.Fatal(err)
	}

	const token = "credit-apply-receipt-session"
	const csrf = "credit-apply-receipt-csrf"
	s := &Server{
		lab: l,
		sessions: map[[32]byte]session{hashToken(token): {
			csrf: csrf, expires: time.Now().Add(time.Hour), idleUntil: time.Now().Add(time.Hour),
			capabilities: []string{"read", "finance.adjust"},
		}},
		now: time.Now,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /admin/api/commands", s.protectedWrite(s.submitCommand))
	post := func() *httptest.ResponseRecorder {
		t.Helper()
		body, err := json.Marshal(map[string]any{
			"action_id": "C12", "target_id": grantID, "preview_id": preview.ID,
			"payload": json.RawMessage(intent),
		})
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/admin/api/commands", strings.NewReader(string(body)))
		r.Header.Set("Origin", "http://127.0.0.1:8080")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		r.Header.Set("Idempotency-Key", "credit-apply-http-receipt-key")
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}

	first := post()
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
	checkFacts := func(applied, available, outstanding int64, oldStatus, oldOutbox string, replacementCount, receiptCount int64) {
		t.Helper()
		credit, err := l.CreditBalance(ctx, grantID)
		if err != nil {
			t.Fatal(err)
		}
		balance, err := l.Balance(ctx, target.InvoiceID)
		if err != nil {
			t.Fatal(err)
		}
		if credit.GrantedMinor != 1000 || credit.AppliedMinor != applied || credit.AvailableMinor != available || credit.ReservedMinor != 0 || credit.RefundedMinor != 0 || balance.OriginalMinor != targetOriginal || balance.CreditAppliedMinor != applied || balance.OutstandingMinor != outstanding {
			t.Fatalf("credit=%+v invoice=%+v", credit, balance)
		}
		var operationStatus, outboxStatus string
		if err := inspection.QueryRow(`SELECT p.status,b.status FROM payment_operations p JOIN outbox b ON b.object_id=p.id AND b.kind='capture' WHERE p.id=?`, target.OperationID).Scan(&operationStatus, &outboxStatus); err != nil || operationStatus != oldStatus || outboxStatus != oldOutbox {
			t.Fatalf("original operation=%s outbox=%s want=%s/%s err=%v", operationStatus, outboxStatus, oldStatus, oldOutbox, err)
		}
		for _, item := range []struct {
			name  string
			query string
			arg   any
			want  int64
		}{
			{"replacement_operations", `SELECT COUNT(*) FROM payment_operations WHERE invoice_id=? AND id<>?`, nil, replacementCount},
			{"receipts", `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, pending.CommandID, receiptCount},
			{"commands", `SELECT COUNT(*) FROM admin_commands WHERE idempotency_key=?`, "credit-apply-http-receipt-key", 1},
		} {
			var got int64
			var err error
			switch item.name {
			case "replacement_operations":
				err = inspection.QueryRow(item.query, target.InvoiceID, target.OperationID).Scan(&got)
			default:
				err = inspection.QueryRow(item.query, item.arg).Scan(&got)
			}
			if err != nil || got != item.want {
				t.Fatalf("%s count=%d want=%d err=%v", item.name, got, item.want, err)
			}
		}
		var applicationCount, applicationAmount int64
		if err := inspection.QueryRow(`SELECT COUNT(*),COALESCE(SUM(amount_minor),0) FROM credit_applications WHERE grant_id=? AND invoice_id=?`, grantID, target.InvoiceID).Scan(&applicationCount, &applicationAmount); err != nil || applicationCount != receiptCount || applicationAmount != applied {
			t.Fatalf("credit applications count=%d amount=%d want=%d/%d err=%v", applicationCount, applicationAmount, receiptCount, applied, err)
		}
		if receiptCount == 1 {
			var refsJSON string
			if err := inspection.QueryRow(`SELECT result_refs_json FROM admin_command_receipts WHERE command_id=?`, pending.CommandID).Scan(&refsJSON); err != nil {
				t.Fatal(err)
			}
			var refs map[string]string
			if err := json.Unmarshal([]byte(refsJSON), &refs); err != nil {
				t.Fatal(err)
			}
			if refs["application_id"] == "" || refs["grant_id"] != grantID || refs["invoice_id"] != target.InvoiceID || refs["amount_minor"] != "500" || refs["currency"] != credit.Currency {
				t.Fatalf("receipt refs=%v differ from credit application", refs)
			}
		}
		var captures int64
		if err := provider.QueryRow(`SELECT COUNT(*) FROM captures`).Scan(&captures); err != nil || captures != 1 {
			t.Fatalf("provider captures=%d want=1 err=%v", captures, err)
		}
	}
	checkFacts(0, 1000, targetOriginal, "created", "pending", 0, 0)
	if _, err := inspection.Exec(`DROP TRIGGER fail_credit_apply_receipt`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		recovered := post()
		var command struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		}
		if err := json.Unmarshal(recovered.Body.Bytes(), &command); err != nil || recovered.Code != http.StatusOK || command.ID != pending.CommandID || command.Status != "succeeded" {
			t.Fatalf("replay %d status=%d body=%s decode=%v", i, recovered.Code, recovered.Body.String(), err)
		}
		checkFacts(500, 500, targetOriginal-500, "cancelled", "done", 0, 1)
	}
	t.Run("committed credit survives a lost HTTP response", func(t *testing.T) {
		secondIntent, err := json.Marshal(map[string]string{"invoice_id": target.InvoiceID, "amount_minor": "250"})
		if err != nil {
			t.Fatal(err)
		}
		secondPreview, err := l.AdminCreatePreview(ctx, "local-admin", "C12", grantID, secondIntent)
		if err != nil {
			t.Fatal(err)
		}
		body, err := json.Marshal(map[string]any{
			"action_id": "C12", "target_id": grantID, "preview_id": secondPreview.ID,
			"payload": json.RawMessage(secondIntent),
		})
		if err != nil {
			t.Fatal(err)
		}
		var firstResponse atomic.Bool
		firstResponse.Store(true)
		handler := s.protectedWrite(s.submitCommand)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if firstResponse.CompareAndSwap(true, false) {
				handler.ServeHTTP(&disconnectBeforeResponse{ResponseWriter: w}, r)
				return
			}
			handler.ServeHTTP(w, r)
		}))
		defer server.Close()
		postLost := func() (*http.Response, error) {
			r, err := http.NewRequest(http.MethodPost, server.URL+"/admin/api/commands", strings.NewReader(string(body)))
			if err != nil {
				return nil, err
			}
			r.Header.Set("Origin", server.URL)
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-CSRF-Token", csrf)
			r.Header.Set("Idempotency-Key", "credit-apply-lost-response-key")
			r.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
			return http.DefaultClient.Do(r)
		}
		response, err := postLost()
		if response != nil {
			response.Body.Close()
		}
		if err == nil {
			t.Fatal("expected the committed command's first HTTP response to be lost")
		}
		var commandID, status string
		if err := inspection.QueryRow(`SELECT id,status FROM admin_commands WHERE idempotency_key='credit-apply-lost-response-key'`).Scan(&commandID, &status); err != nil || commandID == "" || commandID == pending.CommandID || status != "succeeded" {
			t.Fatalf("committed command id=%q status=%q err=%v", commandID, status, err)
		}
		checkCommitted := func() {
			t.Helper()
			credit, err := l.CreditBalance(ctx, grantID)
			if err != nil {
				t.Fatal(err)
			}
			balance, err := l.Balance(ctx, target.InvoiceID)
			if err != nil {
				t.Fatal(err)
			}
			if credit.GrantedMinor != 1000 || credit.AppliedMinor != 750 || credit.AvailableMinor != 250 || balance.CreditAppliedMinor != 750 || balance.OutstandingMinor != targetOriginal-750 {
				t.Fatalf("credit=%+v invoice=%+v", credit, balance)
			}
			var applications, amount, receipts, commands, targetOperations, captures int64
			if err := inspection.QueryRow(`SELECT COUNT(*),COALESCE(SUM(amount_minor),0) FROM credit_applications WHERE grant_id=? AND invoice_id=?`, grantID, target.InvoiceID).Scan(&applications, &amount); err != nil {
				t.Fatal(err)
			}
			if err := inspection.QueryRow(`SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, commandID).Scan(&receipts); err != nil {
				t.Fatal(err)
			}
			if err := inspection.QueryRow(`SELECT COUNT(*) FROM admin_commands WHERE idempotency_key='credit-apply-lost-response-key'`).Scan(&commands); err != nil {
				t.Fatal(err)
			}
			if err := inspection.QueryRow(`SELECT COUNT(*) FROM payment_operations WHERE invoice_id=?`, target.InvoiceID).Scan(&targetOperations); err != nil {
				t.Fatal(err)
			}
			if err := provider.QueryRow(`SELECT COUNT(*) FROM captures`).Scan(&captures); err != nil {
				t.Fatal(err)
			}
			if applications != 2 || amount != 750 || receipts != 1 || commands != 1 || targetOperations != 1 || captures != 1 {
				t.Fatalf("applications=%d amount=%d receipts=%d commands=%d target operations=%d captures=%d", applications, amount, receipts, commands, targetOperations, captures)
			}
			var refsJSON string
			if err := inspection.QueryRow(`SELECT result_refs_json FROM admin_command_receipts WHERE command_id=?`, commandID).Scan(&refsJSON); err != nil {
				t.Fatal(err)
			}
			var refs map[string]string
			if err := json.Unmarshal([]byte(refsJSON), &refs); err != nil {
				t.Fatal(err)
			}
			if refs["application_id"] == "" || refs["grant_id"] != grantID || refs["invoice_id"] != target.InvoiceID || refs["amount_minor"] != "250" || refs["currency"] != credit.Currency {
				t.Fatalf("second receipt refs=%v differ from committed application", refs)
			}
		}
		checkCommitted()
		for i := 0; i < 2; i++ {
			response, err := postLost()
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(response.Body)
			response.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			var command struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			}
			if err := json.Unmarshal(data, &command); err != nil || response.StatusCode != http.StatusOK || command.ID != commandID || command.Status != "succeeded" {
				t.Fatalf("replay %d status=%d body=%s decode=%v", i, response.StatusCode, data, err)
			}
			checkCommitted()
		}
	})
}
