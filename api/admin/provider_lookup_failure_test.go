package admin

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"billforge/lab"
)

func TestUnavailableProviderLookupKeepsOriginalHTTPCommandRetryable(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	commercePath := filepath.Join(dir, "commerce.db")
	providerPath := filepath.Join(dir, "provider.db")
	l, err := lab.Open(commercePath, providerPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	quote, err := l.CreateQuote(ctx, "http-provider-lookup-customer", "basic")
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "http-provider-lookup-purchase")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.DispatchCapture(ctx, receipt.OperationID, "lost_response"); !errors.Is(err, lab.ErrPaymentUnknown) {
		t.Fatalf("lost capture response: %v", err)
	}
	h, err := New(l, Config{Username: "admin", Password: "local-test-password"})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(h)
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 10 * time.Second}
	csrfResponse, err := client.Get(server.URL + "/admin/api/session/csrf")
	if err != nil {
		t.Fatal(err)
	}
	var nonce struct {
		Token string `json:"csrf_token"`
	}
	if err := json.NewDecoder(csrfResponse.Body).Decode(&nonce); err != nil {
		csrfResponse.Body.Close()
		t.Fatal(err)
	}
	csrfResponse.Body.Close()
	login, err := http.NewRequest(http.MethodPost, server.URL+"/admin/api/session", bytes.NewBufferString(`{"username":"admin","password":"local-test-password"}`))
	if err != nil {
		t.Fatal(err)
	}
	login.Header.Set("Origin", server.URL)
	login.Header.Set("Content-Type", "application/json")
	login.Header.Set("X-CSRF-Token", nonce.Token)
	loginResponse, err := client.Do(login)
	if err != nil {
		t.Fatal(err)
	}
	var session struct {
		Token string `json:"csrf_token"`
	}
	if err := json.NewDecoder(loginResponse.Body).Decode(&session); err != nil {
		loginResponse.Body.Close()
		t.Fatal(err)
	}
	loginResponse.Body.Close()
	if loginResponse.StatusCode != http.StatusOK || session.Token == "" {
		t.Fatalf("login status=%d token=%q", loginResponse.StatusCode, session.Token)
	}
	locker, err := sql.Open("sqlite3", providerPath)
	if err != nil {
		t.Fatal(err)
	}
	defer locker.Close()
	lockConn, err := locker.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer lockConn.Close()
	if _, err := lockConn.ExecContext(ctx, `BEGIN EXCLUSIVE`); err != nil {
		t.Fatal(err)
	}
	locked := true
	defer func() {
		if locked {
			_, _ = lockConn.ExecContext(ctx, `ROLLBACK`)
		}
	}()
	const key = "http-provider-lookup-C10"
	body, err := json.Marshal(map[string]any{"action_id": "C10", "target_id": receipt.OperationID, "payload": map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	submit := func() (int, []byte) {
		t.Helper()
		request, err := http.NewRequest(http.MethodPost, server.URL+"/admin/api/commands", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Origin", server.URL)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-CSRF-Token", session.Token)
		request.Header.Set("Idempotency-Key", key)
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		return response.StatusCode, data
	}
	status, result := submit()
	var pending struct {
		CommandID string `json:"command_id"`
		Error     struct {
			Code      string `json:"code"`
			Retryable bool   `json:"retryable"`
		} `json:"error"`
	}
	if err := json.Unmarshal(result, &pending); err != nil {
		t.Fatal(err)
	}
	if status != http.StatusServiceUnavailable || pending.Error.Code != "COMMAND_PENDING_RETRY" || !pending.Error.Retryable || pending.CommandID == "" {
		t.Fatalf("unavailable provider status=%d body=%s", status, result)
	}
	commerce, err := sql.Open("sqlite3", commercePath)
	if err != nil {
		t.Fatal(err)
	}
	defer commerce.Close()
	var commandStatus string
	var commandReceipts, allocations int
	if err := commerce.QueryRow(`SELECT status FROM admin_commands WHERE id=?`, pending.CommandID).Scan(&commandStatus); err != nil {
		t.Fatal(err)
	}
	if err := commerce.QueryRow(`SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, pending.CommandID).Scan(&commandReceipts); err != nil {
		t.Fatal(err)
	}
	if err := commerce.QueryRow(`SELECT COUNT(*) FROM allocations WHERE operation_id=?`, receipt.OperationID).Scan(&allocations); err != nil {
		t.Fatal(err)
	}
	if commandStatus != "accepted" || commandReceipts != 0 || allocations != 0 {
		t.Fatalf("unavailable provider command=%s receipts=%d allocations=%d", commandStatus, commandReceipts, allocations)
	}
	if _, err := lockConn.ExecContext(ctx, `ROLLBACK`); err != nil {
		t.Fatal(err)
	}
	locked = false
	status, result = submit()
	var finished struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(result, &finished); err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || finished.ID != pending.CommandID || finished.Status != "succeeded" {
		t.Fatalf("recovered provider status=%d body=%s", status, result)
	}
	status, result = submit()
	var replayed struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(result, &replayed); err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || replayed.ID != pending.CommandID || replayed.Status != "succeeded" {
		t.Fatalf("original key replay status=%d body=%s", status, result)
	}
	if err := commerce.QueryRow(`SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, pending.CommandID).Scan(&commandReceipts); err != nil {
		t.Fatal(err)
	}
	if err := commerce.QueryRow(`SELECT COUNT(*) FROM allocations WHERE operation_id=?`, receipt.OperationID).Scan(&allocations); err != nil {
		t.Fatal(err)
	}
	if commandReceipts != 1 || allocations != 1 {
		t.Fatalf("recovered command receipts=%d allocations=%d, want one each", commandReceipts, allocations)
	}
}

func TestUnavailableRefundLookupKeepsReservationAndOriginalHTTPCommand(t *testing.T) {
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
	quote, err := l.CreateQuote(ctx, "http-refund-lookup-customer", "basic")
	if err != nil {
		t.Fatal(err)
	}
	purchase, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "http-refund-lookup-purchase")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.DispatchCapture(ctx, purchase.OperationID, ""); err != nil {
		t.Fatal(err)
	}
	correction, err := l.PostReduction(ctx, purchase.InvoiceID, 1000, "provider lookup refund", "http-refund-lookup-reduction")
	if err != nil || len(correction.GrantIDs) != 1 {
		t.Fatalf("reduction=%+v error=%v", correction, err)
	}
	grantID := correction.GrantIDs[0]
	refundID, err := l.ReserveRefund(ctx, grantID, 500, "http-refund-lookup-reservation")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.DispatchRefund(ctx, refundID, "lost_response"); !errors.Is(err, lab.ErrPaymentUnknown) {
		t.Fatalf("lost refund response: %v", err)
	}
	assertCredit := func(wantReserved, wantRefunded int64) {
		t.Helper()
		balance, err := l.CreditBalance(ctx, grantID)
		if err != nil {
			t.Fatal(err)
		}
		if balance.GrantedMinor != 1000 || balance.ReservedMinor != wantReserved || balance.RefundedMinor != wantRefunded || balance.AvailableMinor != 500 {
			t.Fatalf("credit balance=%+v, want granted=1000 reserved=%d refunded=%d available=500", balance, wantReserved, wantRefunded)
		}
	}
	assertCredit(500, 0)

	const token = "refund-lookup-session"
	const csrf = "refund-lookup-csrf"
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
	server := httptest.NewServer(mux)
	defer server.Close()
	client := &http.Client{Timeout: 10 * time.Second}
	post := func() (int, []byte) {
		t.Helper()
		body, err := json.Marshal(map[string]any{"action_id": "C17", "target_id": refundID, "payload": map[string]any{}})
		if err != nil {
			t.Fatal(err)
		}
		r, err := http.NewRequest(http.MethodPost, server.URL+"/admin/api/commands", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Origin", server.URL)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		r.Header.Set("Idempotency-Key", "http-refund-lookup-C17")
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
		response, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		return response.StatusCode, data
	}

	provider, err := sql.Open("sqlite3", providerPath)
	if err != nil {
		t.Fatal(err)
	}
	defer provider.Close()
	lockConn, err := provider.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer lockConn.Close()
	if _, err := lockConn.ExecContext(ctx, `BEGIN EXCLUSIVE`); err != nil {
		t.Fatal(err)
	}
	locked := true
	defer func() {
		if locked {
			_, _ = lockConn.ExecContext(ctx, `ROLLBACK`)
		}
	}()

	status, data := post()
	var pending struct {
		CommandID string `json:"command_id"`
		Error     struct {
			Code      string `json:"code"`
			Retryable bool   `json:"retryable"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &pending); err != nil || status != http.StatusServiceUnavailable || pending.Error.Code != "COMMAND_PENDING_RETRY" || !pending.Error.Retryable || pending.CommandID == "" {
		t.Fatalf("unavailable refund lookup status=%d body=%s decode=%v", status, data, err)
	}
	refund, err := l.Refund(ctx, refundID)
	if err != nil || refund.Status != "unknown" {
		t.Fatalf("unverified refund=%+v error=%v", refund, err)
	}
	assertCredit(500, 0)
	commerce, err := sql.Open("sqlite3", commercePath)
	if err != nil {
		t.Fatal(err)
	}
	defer commerce.Close()
	var commandStatus string
	var receipts int
	if err := commerce.QueryRow(`SELECT status FROM admin_commands WHERE id=?`, pending.CommandID).Scan(&commandStatus); err != nil {
		t.Fatal(err)
	}
	if err := commerce.QueryRow(`SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, pending.CommandID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if commandStatus != "accepted" || receipts != 0 {
		t.Fatalf("pending command status=%s receipts=%d", commandStatus, receipts)
	}

	if _, err := lockConn.ExecContext(ctx, `ROLLBACK`); err != nil {
		t.Fatal(err)
	}
	locked = false
	for i := 0; i < 2; i++ {
		status, data = post()
		var command struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		}
		if err := json.Unmarshal(data, &command); err != nil || status != http.StatusOK || command.ID != pending.CommandID || command.Status != "succeeded" {
			t.Fatalf("refund replay %d status=%d body=%s decode=%v", i, status, data, err)
		}
	}
	refund, err = l.Refund(ctx, refundID)
	if err != nil || refund.Status != "succeeded" {
		t.Fatalf("recovered refund=%+v error=%v", refund, err)
	}
	assertCredit(0, 500)
	if err := commerce.QueryRow(`SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, pending.CommandID).Scan(&receipts); err != nil || receipts != 1 {
		t.Fatalf("recovered receipts=%d error=%v, want one", receipts, err)
	}
	var providerRefunds int
	if err := provider.QueryRow(`SELECT COUNT(*) FROM refunds`).Scan(&providerRefunds); err != nil || providerRefunds != 1 {
		t.Fatalf("provider refunds=%d error=%v, want one", providerRefunds, err)
	}
}
