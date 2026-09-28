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

// disconnectBeforeResponse simulates a connection lost after a command has
// committed but before the client receives any HTTP response bytes.
type disconnectBeforeResponse struct {
	http.ResponseWriter
	disconnected bool
}

func (w *disconnectBeforeResponse) WriteHeader(status int) {
	if w.disconnected {
		return
	}
	w.disconnected = true
	connection, _, err := w.ResponseWriter.(http.Hijacker).Hijack()
	if err == nil {
		connection.Close()
	}
}

func (w *disconnectBeforeResponse) Write(p []byte) (int, error) {
	return len(p), nil
}

func TestLostCommandHTTPResponseReplaysOriginalReceipt(t *testing.T) {
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

	const token = "lost-response-session"
	const csrf = "lost-response-csrf"
	s := &Server{
		lab: l,
		sessions: map[[32]byte]session{hashToken(token): {
			csrf: csrf, expires: time.Now().Add(time.Hour), idleUntil: time.Now().Add(time.Hour),
			capabilities: []string{"read", "subscription.manage"},
		}},
		now: time.Now,
	}
	var first atomic.Bool
	first.Store(true)
	handler := s.protectedWrite(s.submitCommand)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if first.CompareAndSwap(true, false) {
			handler.ServeHTTP(&disconnectBeforeResponse{ResponseWriter: w}, r)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	defer server.Close()

	const body = `{"action_id":"C01","payload":{"customer_id":"lost-response-customer","plan_id":"basic","cohort":"default","seats":"0"}}`
	post := func() (*http.Response, error) {
		t.Helper()
		r, err := http.NewRequest(http.MethodPost, server.URL+"/admin/api/commands", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Origin", server.URL)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		r.Header.Set("Idempotency-Key", "lost-response-key")
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
		return (&http.Client{Timeout: 5 * time.Second}).Do(r)
	}

	response, err := post()
	if err == nil {
		response.Body.Close()
		t.Fatal("expected the first committed command response to be lost")
	}

	inspection, err := sql.Open("sqlite3", commercePath)
	if err != nil {
		t.Fatal(err)
	}
	defer inspection.Close()
	var commandID, status string
	if err := inspection.QueryRow(`SELECT id, status FROM admin_commands WHERE idempotency_key='lost-response-key'`).Scan(&commandID, &status); err != nil {
		t.Fatalf("committed original command: %v", err)
	}
	if status != "succeeded" {
		t.Fatalf("original command status=%s, want succeeded before replay", status)
	}
	checkOneFinancialFact := func(name, query string, args ...any) {
		t.Helper()
		var count int
		if err := inspection.QueryRow(query, args...).Scan(&count); err != nil || count != 1 {
			t.Fatalf("%s count=%d error=%v, want one", name, count, err)
		}
	}
	checkFacts := func() {
		t.Helper()
		checkOneFinancialFact("command", `SELECT COUNT(*) FROM admin_commands WHERE idempotency_key='lost-response-key'`)
		checkOneFinancialFact("quote", `SELECT COUNT(*) FROM quotes WHERE customer_id='lost-response-customer'`)
		checkOneFinancialFact("receipt", `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, commandID)
	}
	checkFacts()

	for i := 0; i < 2; i++ {
		response, err = post()
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(response.Body)
			response.Body.Close()
			t.Fatalf("replay %d status=%d body=%s", i, response.StatusCode, body)
		}
		var command struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		}
		err = json.NewDecoder(response.Body).Decode(&command)
		response.Body.Close()
		if err != nil || command.ID != commandID || command.Status != "succeeded" {
			t.Fatalf("replay %d command=%+v decode=%v, want succeeded %s", i, command, err, commandID)
		}
	}

	checkFacts()
}

func TestClientDisconnectDuringAcceptedQuoteExecutionRecoversOriginalCommand(t *testing.T) {
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
	// Admission commits before execution begins. This trigger keeps the quote
	// write in progress long enough for the client to disconnect after admission.
	if _, err := inspection.Exec(`CREATE TRIGGER slow_quote BEFORE INSERT ON quotes BEGIN
		SELECT max(x) FROM (WITH RECURSIVE cnt(x) AS
			(SELECT 1 UNION ALL SELECT x+1 FROM cnt WHERE x<10000000)
			SELECT x FROM cnt);
	END`); err != nil {
		t.Fatal(err)
	}

	const token = "inflight-disconnect-session"
	const csrf = "inflight-disconnect-csrf"
	s := &Server{
		lab: l,
		sessions: map[[32]byte]session{hashToken(token): {
			csrf: csrf, expires: time.Now().Add(time.Hour), idleUntil: time.Now().Add(time.Hour),
			capabilities: []string{"read", "subscription.manage"},
		}},
		now: time.Now,
	}
	handler := s.protectedWrite(s.submitCommand)
	firstFinished := make(chan struct{})
	var firstRequest atomic.Bool
	firstRequest.Store(true)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if firstRequest.CompareAndSwap(true, false) {
			defer close(firstFinished)
		}
		handler.ServeHTTP(w, r)
	}))
	defer server.Close()

	const body = `{"action_id":"C01","payload":{"customer_id":"inflight-disconnect-customer","plan_id":"basic","cohort":"default","seats":"0"}}`
	newRequest := func(ctx context.Context) *http.Request {
		t.Helper()
		r, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/admin/api/commands", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Origin", server.URL)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		r.Header.Set("Idempotency-Key", "inflight-disconnect-key")
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
		return r
	}
	client := &http.Client{Timeout: 10 * time.Second}
	requestCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request := newRequest(requestCtx)
	clientResult := make(chan error, 1)
	go func() {
		response, err := client.Do(request)
		if response != nil {
			response.Body.Close()
		}
		clientResult <- err
	}()

	var commandID, status, leaseOwner string
	deadline := time.After(2 * time.Second)
	for leaseOwner == "" {
		err := inspection.QueryRow(`SELECT id,status,COALESCE(lease_owner,'') FROM admin_commands WHERE idempotency_key='inflight-disconnect-key'`).Scan(&commandID, &status, &leaseOwner)
		if err != nil && err != sql.ErrNoRows {
			t.Fatal(err)
		}
		if leaseOwner != "" {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("command execution did not acquire its lease after admission; id=%s status=%s", commandID, status)
		case <-time.After(5 * time.Millisecond):
		}
	}
	if status != "accepted" {
		t.Fatalf("command status before disconnect=%s, want accepted", status)
	}
	cancel()
	select {
	case err := <-clientResult:
		if err == nil {
			t.Fatal("client request unexpectedly completed after cancellation")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("client did not return after disconnect")
	}
	select {
	case <-firstFinished:
	case <-time.After(5 * time.Second):
		t.Fatal("server did not finish canceled command execution")
	}
	if err := inspection.QueryRow(`SELECT status,COALESCE(lease_owner,'') FROM admin_commands WHERE id=?`, commandID).Scan(&status, &leaseOwner); err != nil {
		t.Fatal(err)
	}
	if status != "accepted" || leaseOwner != "" {
		t.Fatalf("canceled command status=%s lease_owner=%q, want accepted and released", status, leaseOwner)
	}

	var quotes, receipts int
	if err := inspection.QueryRow(`SELECT COUNT(*) FROM quotes WHERE customer_id='inflight-disconnect-customer'`).Scan(&quotes); err != nil {
		t.Fatal(err)
	}
	if err := inspection.QueryRow(`SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, commandID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if quotes != 0 || receipts != 0 {
		t.Fatalf("canceled execution committed quote=%d receipt=%d", quotes, receipts)
	}
	if _, err := inspection.Exec(`DROP TRIGGER slow_quote`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		response, err := client.Do(newRequest(context.Background()))
		if err != nil {
			t.Fatal(err)
		}
		var command struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		}
		decodeErr := json.NewDecoder(response.Body).Decode(&command)
		response.Body.Close()
		if response.StatusCode != http.StatusOK || decodeErr != nil || command.ID != commandID || command.Status != "succeeded" {
			t.Fatalf("replay %d status=%d command=%+v decode=%v", i, response.StatusCode, command, decodeErr)
		}
	}
	if err := inspection.QueryRow(`SELECT COUNT(*) FROM quotes WHERE customer_id='inflight-disconnect-customer'`).Scan(&quotes); err != nil {
		t.Fatal(err)
	}
	if err := inspection.QueryRow(`SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, commandID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if quotes != 1 || receipts != 1 {
		t.Fatalf("recovered execution quote=%d receipt=%d, want one each", quotes, receipts)
	}
}
