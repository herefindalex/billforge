package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"billforge/lab"
)

func TestReceiptWriteFailureReturnsRecoverableOriginalCommand(t *testing.T) {
	dir := t.TempDir()
	commercePath := filepath.Join(dir, "commerce.db")
	l, err := lab.Open(commercePath, filepath.Join(dir, "provider.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := l.InitAdmin(context.Background()); err != nil {
		t.Fatal(err)
	}
	inspection, err := sql.Open("sqlite3", commercePath)
	if err != nil {
		t.Fatal(err)
	}
	defer inspection.Close()
	if _, err := inspection.Exec(`CREATE TRIGGER fail_admin_receipt BEFORE INSERT ON admin_command_receipts
		BEGIN SELECT RAISE(ABORT,'injected receipt failure'); END`); err != nil {
		t.Fatal(err)
	}
	const token = "receipt-fault-session"
	const csrf = "receipt-fault-csrf"
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
	const body = `{"action_id":"C01","payload":{"customer_id":"receipt-http-fault","plan_id":"basic","cohort":"default","seats":"0"}}`
	post := func() *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/admin/api/commands", strings.NewReader(body))
		r.Header.Set("Origin", "http://127.0.0.1:8080")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		r.Header.Set("Idempotency-Key", "receipt-http-fault-key")
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	first := post()
	if first.Code != http.StatusServiceUnavailable {
		t.Fatalf("receipt write failure status=%d body=%s", first.Code, first.Body.String())
	}
	var pending struct {
		Error struct {
			Code      string `json:"code"`
			Retryable bool   `json:"retryable"`
		} `json:"error"`
		CommandID string `json:"command_id"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &pending); err != nil {
		t.Fatal(err)
	}
	if pending.Error.Code != "COMMAND_PENDING_RETRY" || !pending.Error.Retryable || pending.CommandID == "" {
		t.Fatalf("failure lost recovery instruction: %s", first.Body.String())
	}
	var quotes, receipts, commands int
	checkCounts := func(wantQuotes, wantReceipts, wantCommands int) {
		t.Helper()
		if err := inspection.QueryRow(`SELECT COUNT(*) FROM quotes WHERE customer_id='receipt-http-fault'`).Scan(&quotes); err != nil {
			t.Fatal(err)
		}
		if err := inspection.QueryRow(`SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, pending.CommandID).Scan(&receipts); err != nil {
			t.Fatal(err)
		}
		if err := inspection.QueryRow(`SELECT COUNT(*) FROM admin_commands WHERE id=? AND idempotency_key='receipt-http-fault-key'`, pending.CommandID).Scan(&commands); err != nil {
			t.Fatal(err)
		}
		if quotes != wantQuotes || receipts != wantReceipts || commands != wantCommands {
			t.Fatalf("financial facts quotes=%d receipts=%d commands=%d, want %d/%d/%d", quotes, receipts, commands, wantQuotes, wantReceipts, wantCommands)
		}
	}
	checkCounts(0, 0, 1)
	if _, err := inspection.Exec(`DROP TRIGGER fail_admin_receipt`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		recovered := post()
		if recovered.Code != http.StatusOK {
			t.Fatalf("recovery replay %d status=%d body=%s", i, recovered.Code, recovered.Body.String())
		}
		var command struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		}
		if err := json.Unmarshal(recovered.Body.Bytes(), &command); err != nil {
			t.Fatal(err)
		}
		if command.ID != pending.CommandID || command.Status != "succeeded" {
			t.Fatalf("recovery replay %d returned different command: %s", i, recovered.Body.String())
		}
		checkCounts(1, 1, 1)
	}
}

func TestCommandAdmissionWriteFailureCanRetryOriginalKey(t *testing.T) {
	dir := t.TempDir()
	commercePath := filepath.Join(dir, "commerce.db")
	l, err := lab.Open(commercePath, filepath.Join(dir, "provider.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := l.InitAdmin(context.Background()); err != nil {
		t.Fatal(err)
	}
	inspection, err := sql.Open("sqlite3", commercePath)
	if err != nil {
		t.Fatal(err)
	}
	defer inspection.Close()
	if _, err := inspection.Exec(`CREATE TRIGGER fail_admin_admission BEFORE INSERT ON admin_commands BEGIN SELECT RAISE(ABORT,'injected admission failure'); END`); err != nil {
		t.Fatal(err)
	}

	const token = "admission-fault-session"
	const csrf = "admission-fault-csrf"
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
	post := func() *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/admin/api/commands", strings.NewReader(`{"action_id":"C01","payload":{"customer_id":"admission-http-fault","plan_id":"basic","cohort":"default","seats":"0"}}`))
		r.Header.Set("Origin", "http://127.0.0.1:8080")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		r.Header.Set("Idempotency-Key", "admission-http-fault-key")
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}

	failed := post()
	if failed.Code != http.StatusInternalServerError {
		t.Fatalf("admission failure status=%d body=%s", failed.Code, failed.Body.String())
	}
	var failure struct {
		Error struct {
			Code      string `json:"code"`
			Retryable bool   `json:"retryable"`
		} `json:"error"`
	}
	if err := json.Unmarshal(failed.Body.Bytes(), &failure); err != nil || failure.Error.Code != "COMMAND_ADMISSION_UNKNOWN" || !failure.Error.Retryable {
		t.Fatalf("admission failure response=%s decode=%v", failed.Body.String(), err)
	}
	checkCount := func(query string, want int) {
		t.Helper()
		var count int
		if err := inspection.QueryRow(query).Scan(&count); err != nil || count != want {
			t.Fatalf("%s count=%d error=%v, want %d", query, count, err, want)
		}
	}
	checkCount(`SELECT COUNT(*) FROM admin_commands WHERE idempotency_key='admission-http-fault-key'`, 0)
	checkCount(`SELECT COUNT(*) FROM quotes WHERE customer_id='admission-http-fault'`, 0)
	checkCount(`SELECT COUNT(*) FROM admin_command_receipts`, 0)
	if _, err := inspection.Exec(`DROP TRIGGER fail_admin_admission`); err != nil {
		t.Fatal(err)
	}
	var commandID string
	for i := 0; i < 2; i++ {
		recovered := post()
		wantStatus := http.StatusAccepted
		if i > 0 {
			wantStatus = http.StatusOK
		}
		if recovered.Code != wantStatus {
			t.Fatalf("recovery replay %d status=%d body=%s", i, recovered.Code, recovered.Body.String())
		}
		var command struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		}
		if err := json.Unmarshal(recovered.Body.Bytes(), &command); err != nil || command.ID == "" || command.Status != "succeeded" {
			t.Fatalf("recovery replay %d body=%s decode=%v", i, recovered.Body.String(), err)
		}
		if i == 0 {
			commandID = command.ID
		} else if command.ID != commandID {
			t.Fatalf("replay created a different command: got %s, want %s", command.ID, commandID)
		}
	}
	checkCount(`SELECT COUNT(*) FROM admin_commands WHERE idempotency_key='admission-http-fault-key'`, 1)
	checkCount(`SELECT COUNT(*) FROM quotes WHERE customer_id='admission-http-fault'`, 1)
	checkCount(`SELECT COUNT(*) FROM admin_command_receipts`, 1)
}

func TestRefundReservationReceiptFailureRetainsBudgetAndOriginalHTTPCommand(t *testing.T) {
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
	quote, err := l.CreateQuote(ctx, "refund-http-receipt-fault", "basic")
	if err != nil {
		t.Fatal(err)
	}
	purchase, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "refund-http-receipt-checkout")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.DispatchCapture(ctx, purchase.OperationID, ""); err != nil {
		t.Fatal(err)
	}
	correction, err := l.PostReduction(ctx, purchase.InvoiceID, 1000, "refund credit", "refund-http-receipt-reduction")
	if err != nil || len(correction.GrantIDs) != 1 {
		t.Fatalf("correction=%+v error=%v", correction, err)
	}
	grantID := correction.GrantIDs[0]
	payload := json.RawMessage(`{"amount_minor":"500"}`)
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C15", grantID, payload)
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
	if _, err := inspection.Exec(`CREATE TRIGGER fail_refund_admin_receipt BEFORE INSERT ON admin_command_receipts BEGIN SELECT RAISE(ABORT,'injected receipt failure'); END`); err != nil {
		t.Fatal(err)
	}

	const token = "refund-receipt-fault-session"
	const csrf = "refund-receipt-fault-csrf"
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
			"action_id": "C15", "target_id": grantID, "preview_id": preview.ID,
			"payload": map[string]any{"amount_minor": "500"},
		})
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/admin/api/commands", strings.NewReader(string(body)))
		r.Header.Set("Origin", "http://127.0.0.1:8080")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		r.Header.Set("Idempotency-Key", "refund-http-receipt-key")
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
	assertFacts := func(wantReserved, wantRefunds, wantReceipts int64) {
		t.Helper()
		balance, err := l.CreditBalance(ctx, grantID)
		if err != nil {
			t.Fatal(err)
		}
		if balance.GrantedMinor != 1000 || balance.AvailableMinor != 1000-wantReserved || balance.ReservedMinor != wantReserved || balance.RefundedMinor != 0 {
			t.Fatalf("credit balance=%+v, want reserved=%d refunded=0", balance, wantReserved)
		}
		var refunds, receipts, commands, providerRefunds int64
		if err := inspection.QueryRow(`SELECT COUNT(*) FROM refund_operations WHERE grant_id=?`, grantID).Scan(&refunds); err != nil {
			t.Fatal(err)
		}
		if err := inspection.QueryRow(`SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, pending.CommandID).Scan(&receipts); err != nil {
			t.Fatal(err)
		}
		if err := inspection.QueryRow(`SELECT COUNT(*) FROM admin_commands WHERE idempotency_key='refund-http-receipt-key'`).Scan(&commands); err != nil {
			t.Fatal(err)
		}
		if err := provider.QueryRow(`SELECT COUNT(*) FROM refunds`).Scan(&providerRefunds); err != nil {
			t.Fatal(err)
		}
		if refunds != wantRefunds || receipts != wantReceipts || commands != 1 || providerRefunds != 0 {
			t.Fatalf("refunds=%d receipts=%d commands=%d provider_refunds=%d, want %d/%d/1/0", refunds, receipts, commands, providerRefunds, wantRefunds, wantReceipts)
		}
		if wantRefunds == 1 {
			var amount int64
			var status string
			if err := inspection.QueryRow(`SELECT amount_minor,status FROM refund_operations WHERE grant_id=?`, grantID).Scan(&amount, &status); err != nil || amount != 500 || status != "created" {
				t.Fatalf("reserved refund amount=%d status=%s error=%v, want 500/created", amount, status, err)
			}
		}
	}
	assertFacts(0, 0, 0)
	if _, err := inspection.Exec(`DROP TRIGGER fail_refund_admin_receipt`); err != nil {
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
	}
	assertFacts(500, 1, 1)
}
