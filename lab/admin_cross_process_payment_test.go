package lab

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestAdminCrossProcessPaymentWorker(t *testing.T) {
	if os.Getenv("BILLFORGE_TEST_ADMIN_WORKER") != "1" {
		return
	}
	commercePath := os.Getenv("BILLFORGE_TEST_COMMERCE_PATH")
	providerPath := os.Getenv("BILLFORGE_TEST_PROVIDER_PATH")
	commandID := os.Getenv("BILLFORGE_TEST_COMMAND_ID")
	readyPath := os.Getenv("BILLFORGE_TEST_READY_PATH")
	gatePath := os.Getenv("BILLFORGE_TEST_GATE_PATH")
	if commercePath == "" || providerPath == "" || commandID == "" || readyPath == "" || gatePath == "" {
		t.Fatal("cross-process worker settings are incomplete")
	}
	var clock func() time.Time
	if value := os.Getenv("BILLFORGE_TEST_BUSINESS_TIME"); value != "" {
		at, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			t.Fatal(err)
		}
		clock = func() time.Time { return at }
	}
	l, err := Open(commercePath, providerPath, clock)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := os.WriteFile(readyPath, []byte("ready"), 0o600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		if _, err := os.Stat(gatePath); err == nil {
			break
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatal("cross-process start gate timed out")
		}
		time.Sleep(5 * time.Millisecond)
	}
	command, err := l.AdminExecuteCommand(context.Background(), commandID)
	if err != nil {
		t.Fatal(err)
	}
	if command.Status != "succeeded" && (command.Status != "failed" || command.ErrorCode != "PREVIEW_STALE") {
		t.Fatalf("unexpected command outcome %+v", command)
	}
}

func TestAdminCompetingPaymentCommandsAcrossProcesses(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	dir := t.TempDir()
	commercePath := filepath.Join(dir, "commerce.db")
	providerPath := filepath.Join(dir, "provider.db")
	l, err := Open(commercePath, providerPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	purchase := purchaseHundred(t, l, "cross-process-payment", "cross-process-checkout")
	create := func(key, amount string) AdminCommand {
		t.Helper()
		payload := json.RawMessage(`{"amount_minor":"` + amount + `"}`)
		preview, err := l.AdminCreatePreview(ctx, "local-admin", "C07", purchase.InvoiceID, payload)
		if err != nil {
			t.Fatal(err)
		}
		command, _, err := l.AdminSubmitCommand(ctx, "local-admin", key, "C07", purchase.InvoiceID, payload, preview.ID)
		if err != nil {
			t.Fatal(err)
		}
		return command
	}
	a := create("cross-process-six", "6000")
	b := create("cross-process-seven", "7000")
	gatePath := filepath.Join(dir, "start")
	type worker struct {
		cmd       *exec.Cmd
		output    bytes.Buffer
		readyPath string
	}
	workers := []*worker{
		{readyPath: filepath.Join(dir, "ready-a")},
		{readyPath: filepath.Join(dir, "ready-b")},
	}
	for i, command := range []AdminCommand{a, b} {
		worker := workers[i]
		worker.cmd = exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAdminCrossProcessPaymentWorker$")
		worker.cmd.Env = append(os.Environ(),
			"BILLFORGE_TEST_ADMIN_WORKER=1",
			"BILLFORGE_TEST_COMMERCE_PATH="+commercePath,
			"BILLFORGE_TEST_PROVIDER_PATH="+providerPath,
			"BILLFORGE_TEST_COMMAND_ID="+command.ID,
			"BILLFORGE_TEST_READY_PATH="+worker.readyPath,
			"BILLFORGE_TEST_GATE_PATH="+gatePath,
		)
		worker.cmd.Stdout = &worker.output
		worker.cmd.Stderr = &worker.output
		if err := worker.cmd.Start(); err != nil {
			t.Fatal(err)
		}
	}
	for {
		allReady := true
		for _, worker := range workers {
			if _, err := os.Stat(worker.readyPath); os.IsNotExist(err) {
				allReady = false
			} else if err != nil {
				t.Fatal(err)
			}
		}
		if allReady {
			break
		}
		if ctx.Err() != nil {
			t.Fatalf("workers did not become ready: %v", ctx.Err())
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := os.WriteFile(gatePath, []byte("go"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, worker := range workers {
		if err := worker.cmd.Wait(); err != nil {
			t.Fatalf("worker failed: %v\n%s", err, worker.output.String())
		}
	}
	var succeeded, stale, receipts, operations, created, oldCancelled int
	for _, query := range []struct {
		sql  string
		args []any
		out  *int
	}{
		{`SELECT COUNT(*) FROM admin_commands WHERE id IN (?,?) AND status='succeeded'`, []any{a.ID, b.ID}, &succeeded},
		{`SELECT COUNT(*) FROM admin_commands WHERE id IN (?,?) AND status='failed' AND error_code='PREVIEW_STALE'`, []any{a.ID, b.ID}, &stale},
		{`SELECT COUNT(*) FROM admin_command_receipts WHERE command_id IN (?,?)`, []any{a.ID, b.ID}, &receipts},
		{`SELECT COUNT(*) FROM payment_operations WHERE invoice_id=?`, []any{purchase.InvoiceID}, &operations},
		{`SELECT COUNT(*) FROM payment_operations WHERE invoice_id=? AND status='created' AND amount_minor IN (6000,7000)`, []any{purchase.InvoiceID}, &created},
		{`SELECT COUNT(*) FROM payment_operations WHERE id=? AND status='cancelled'`, []any{purchase.OperationID}, &oldCancelled},
	} {
		if err := l.db.QueryRowContext(ctx, query.sql, query.args...).Scan(query.out); err != nil {
			t.Fatal(err)
		}
	}
	if succeeded != 1 || stale != 1 || receipts != 1 || operations != 2 || created != 1 || oldCancelled != 1 {
		t.Fatalf("cross-process outcome succeeded=%d stale=%d receipts=%d operations=%d created=%d oldCancelled=%d", succeeded, stale, receipts, operations, created, oldCancelled)
	}
	if captures := captureCount(t, l); captures != 0 {
		t.Fatalf("C07 dispatched provider unexpectedly: %d", captures)
	}
}

func TestAdminCompetingRetryCommandsAcrossProcesses(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	dir := t.TempDir()
	commercePath := filepath.Join(dir, "commerce.db")
	providerPath := filepath.Join(dir, "provider.db")
	l, err := Open(commercePath, providerPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	purchase := purchaseHundred(t, l, "cross-process-retry", "cross-process-retry-checkout")
	if err := l.SetFakePaymentDecision(ctx, purchase.OperationID, "definitively_failed"); err != nil {
		t.Fatal(err)
	}
	if _, err := l.DispatchNext(ctx, ""); err != nil {
		t.Fatal(err)
	}
	create := func(key string) AdminCommand {
		t.Helper()
		preview, err := l.AdminCreatePreview(ctx, "local-admin", "C08", purchase.OperationID, json.RawMessage(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		command, _, err := l.AdminSubmitCommand(ctx, "local-admin", key, "C08", purchase.OperationID, json.RawMessage(`{}`), preview.ID)
		if err != nil {
			t.Fatal(err)
		}
		return command
	}
	a := create("cross-process-retry-a")
	b := create("cross-process-retry-b")
	gatePath := filepath.Join(dir, "start")
	type worker struct {
		cmd       *exec.Cmd
		output    bytes.Buffer
		readyPath string
	}
	workers := []*worker{
		{readyPath: filepath.Join(dir, "ready-a")},
		{readyPath: filepath.Join(dir, "ready-b")},
	}
	for i, command := range []AdminCommand{a, b} {
		worker := workers[i]
		worker.cmd = exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAdminCrossProcessPaymentWorker$")
		worker.cmd.Env = append(os.Environ(),
			"BILLFORGE_TEST_ADMIN_WORKER=1",
			"BILLFORGE_TEST_COMMERCE_PATH="+commercePath,
			"BILLFORGE_TEST_PROVIDER_PATH="+providerPath,
			"BILLFORGE_TEST_COMMAND_ID="+command.ID,
			"BILLFORGE_TEST_READY_PATH="+worker.readyPath,
			"BILLFORGE_TEST_GATE_PATH="+gatePath,
		)
		worker.cmd.Stdout = &worker.output
		worker.cmd.Stderr = &worker.output
		if err := worker.cmd.Start(); err != nil {
			t.Fatal(err)
		}
	}
	for {
		allReady := true
		for _, worker := range workers {
			if _, err := os.Stat(worker.readyPath); os.IsNotExist(err) {
				allReady = false
			} else if err != nil {
				t.Fatal(err)
			}
		}
		if allReady {
			break
		}
		if ctx.Err() != nil {
			t.Fatalf("workers did not become ready: %v", ctx.Err())
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := os.WriteFile(gatePath, []byte("go"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, worker := range workers {
		if err := worker.cmd.Wait(); err != nil {
			t.Fatalf("retry worker failed: %v\n%s", err, worker.output.String())
		}
	}
	var succeeded, stale, receipts, retries, operations int
	for _, query := range []struct {
		sql  string
		args []any
		out  *int
	}{
		{`SELECT COUNT(*) FROM admin_commands WHERE id IN (?,?) AND status='succeeded'`, []any{a.ID, b.ID}, &succeeded},
		{`SELECT COUNT(*) FROM admin_commands WHERE id IN (?,?) AND status='failed' AND error_code='PREVIEW_STALE'`, []any{a.ID, b.ID}, &stale},
		{`SELECT COUNT(*) FROM admin_command_receipts WHERE command_id IN (?,?)`, []any{a.ID, b.ID}, &receipts},
		{`SELECT COUNT(*) FROM payment_retry_requests WHERE invoice_id=?`, []any{purchase.InvoiceID}, &retries},
		{`SELECT COUNT(*) FROM payment_operations WHERE invoice_id=?`, []any{purchase.InvoiceID}, &operations},
	} {
		if err := l.db.QueryRowContext(ctx, query.sql, query.args...).Scan(query.out); err != nil {
			t.Fatal(err)
		}
	}
	if succeeded != 1 || stale != 1 || receipts != 1 || retries != 1 || operations != 2 {
		t.Fatalf("cross-process retry outcome succeeded=%d stale=%d receipts=%d retries=%d operations=%d", succeeded, stale, receipts, retries, operations)
	}
	var newProviderKey string
	if err := l.db.QueryRowContext(ctx, `SELECT provider_key FROM payment_operations WHERE invoice_id=? AND status='created'`, purchase.InvoiceID).Scan(&newProviderKey); err != nil {
		t.Fatal(err)
	}
	var providerRows int
	if err := l.provider.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM captures WHERE provider_key=?`, newProviderKey).Scan(&providerRows); err != nil || providerRows != 0 {
		t.Fatalf("new retry key dispatched=%d err=%v", providerRows, err)
	}
}
