package lab

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestAdminCreatePaymentReplacesUnsentOperationAtomically(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	l, err := Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	quote, err := l.CreateQuote(ctx, "admin-payment-customer", "basic")
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "admin-payment-checkout")
	if err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{"amount_minor":"1000"}`)
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C07", accepted.InvoiceID, payload)
	if err != nil {
		t.Fatal(err)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "partial-payment-001", "C07", accepted.InvoiceID, payload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	command, err = l.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "succeeded" {
		t.Fatalf("execute: %+v %v", command, err)
	}
	var refs map[string]string
	if err := json.Unmarshal(command.ResultRefs, &refs); err != nil {
		t.Fatal(err)
	}
	if refs["operation_id"] == "" || refs["amount_minor"] != "1000" {
		t.Fatalf("wrong refs: %+v", refs)
	}
	var oldStatus, newStatus string
	if err := l.db.QueryRowContext(ctx, `SELECT status FROM payment_operations WHERE id=?`, accepted.OperationID).Scan(&oldStatus); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT status FROM payment_operations WHERE id=?`, refs["operation_id"]).Scan(&newStatus); err != nil {
		t.Fatal(err)
	}
	var receipt int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipt); err != nil {
		t.Fatal(err)
	}
	if oldStatus != "cancelled" || newStatus != "created" || receipt != 1 {
		t.Fatalf("states: old=%s new=%s receipt=%d", oldStatus, newStatus, receipt)
	}
}

func TestAdminCreatePaymentReceiptFailureRestoresOriginalCollectionUntilRecovery(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	commerce, provider := filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db")
	l, err := Open(commerce, provider, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	quote, err := l.CreateQuote(ctx, "payment-receipt-failure-customer", "basic")
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "payment-receipt-failure-checkout")
	if err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{"amount_minor":"1000"}`)
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C07", accepted.InvoiceID, payload)
	if err != nil {
		t.Fatal(err)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "payment-receipt-failure-001", "C07", accepted.InvoiceID, payload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.db.ExecContext(ctx, `CREATE TEMP TRIGGER fail_payment_receipt BEFORE INSERT ON admin_command_receipts BEGIN SELECT RAISE(ABORT, 'injected receipt failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := l.AdminExecuteCommand(ctx, command.ID); err == nil {
		t.Fatal("receipt failure must roll back payment replacement")
	}
	var oldStatus, oldOutboxStatus string
	if err := l.db.QueryRowContext(ctx, `SELECT status FROM payment_operations WHERE id=?`, accepted.OperationID).Scan(&oldStatus); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT status FROM outbox WHERE id=?`, "capture:"+accepted.OperationID).Scan(&oldOutboxStatus); err != nil {
		t.Fatal(err)
	}
	var operations, receipts int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM payment_operations WHERE invoice_id=?`, accepted.InvoiceID).Scan(&operations); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if oldStatus != "created" || oldOutboxStatus != "pending" || operations != 1 || receipts != 0 {
		t.Fatalf("failed receipt changed collection: old=%s outbox=%s operations=%d receipts=%d", oldStatus, oldOutboxStatus, operations, receipts)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	l, err = Open(commerce, provider, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := l.AdminResumeAccepted(ctx); err != nil {
			t.Fatal(err)
		}
	}
	command, err = l.AdminCommand(ctx, command.ID)
	if err != nil || command.Status != "succeeded" {
		t.Fatalf("payment replacement did not recover: %+v err=%v", command, err)
	}
	replayed, replay, err := l.AdminSubmitCommand(ctx, "local-admin", "payment-receipt-failure-001", "C07", accepted.InvoiceID, payload, preview.ID)
	if err != nil || !replay || replayed.ID != command.ID {
		t.Fatalf("same request key changed recovered command: %+v replay=%v err=%v", replayed, replay, err)
	}
	var refs map[string]string
	if err := json.Unmarshal(command.ResultRefs, &refs); err != nil {
		t.Fatal(err)
	}
	newOperationID := refs["operation_id"]
	if newOperationID == "" || newOperationID == accepted.OperationID || refs["amount_minor"] != "1000" {
		t.Fatalf("invalid recovered operation: %+v", refs)
	}
	var newStatus, newOutboxStatus string
	if err := l.db.QueryRowContext(ctx, `SELECT status FROM payment_operations WHERE id=?`, accepted.OperationID).Scan(&oldStatus); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT status FROM payment_operations WHERE id=?`, newOperationID).Scan(&newStatus); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT status FROM outbox WHERE id=?`, "capture:"+accepted.OperationID).Scan(&oldOutboxStatus); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT status FROM outbox WHERE id=?`, "capture:"+newOperationID).Scan(&newOutboxStatus); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM payment_operations WHERE invoice_id=?`, accepted.InvoiceID).Scan(&operations); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if oldStatus != "cancelled" || newStatus != "created" || oldOutboxStatus != "done" || newOutboxStatus != "pending" || operations != 2 || receipts != 1 || captureCount(t, l) != 0 {
		t.Fatalf("recovered collection: old=%s new=%s old_outbox=%s new_outbox=%s operations=%d receipts=%d", oldStatus, newStatus, oldOutboxStatus, newOutboxStatus, operations, receipts)
	}
}

func TestAdminRetryPaymentReceiptFailureRecoversOneObligation(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	commerce, provider := filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db")
	l, err := Open(commerce, provider, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	quote, err := l.CreateQuote(ctx, "retry-receipt-failure-customer", "basic")
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "retry-receipt-failure-checkout")
	if err != nil {
		t.Fatal(err)
	}
	if err := l.SetFakePaymentDecision(ctx, accepted.OperationID, "definitively_failed"); err != nil {
		t.Fatal(err)
	}
	if _, err := l.DispatchNext(ctx, ""); err != nil {
		t.Fatal(err)
	}
	providerAttempts := captureCount(t, l)
	payload := json.RawMessage(`{}`)
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C08", accepted.OperationID, payload)
	if err != nil {
		t.Fatal(err)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "retry-receipt-failure-key", "C08", accepted.OperationID, payload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.db.ExecContext(ctx, `CREATE TEMP TRIGGER fail_retry_receipt BEFORE INSERT ON admin_command_receipts BEGIN SELECT RAISE(ABORT, 'injected receipt failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := l.AdminExecuteCommand(ctx, command.ID); err == nil {
		t.Fatal("receipt failure must roll back retry obligation")
	}
	var status string
	var operations, retryRequests, receipts int
	if err := l.db.QueryRowContext(ctx, `SELECT status FROM payment_operations WHERE id=?`, accepted.OperationID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM payment_operations WHERE invoice_id=?`, accepted.InvoiceID).Scan(&operations); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM payment_retry_requests WHERE key=?`, "admin:"+command.ID).Scan(&retryRequests); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if status != "definitively_failed" || operations != 1 || retryRequests != 0 || receipts != 0 || captureCount(t, l) != providerAttempts {
		t.Fatalf("failed receipt changed collection: status=%s operations=%d retry_requests=%d receipts=%d", status, operations, retryRequests, receipts)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	l, err = Open(commerce, provider, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := l.AdminResumeAccepted(ctx); err != nil {
			t.Fatal(err)
		}
	}
	command, err = l.AdminCommand(ctx, command.ID)
	if err != nil || command.Status != "succeeded" {
		t.Fatalf("retry command did not recover: %+v err=%v", command, err)
	}
	replayed, replay, err := l.AdminSubmitCommand(ctx, "local-admin", "retry-receipt-failure-key", "C08", accepted.OperationID, payload, preview.ID)
	if err != nil || !replay || replayed.ID != command.ID {
		t.Fatalf("same request key changed recovered command: %+v replay=%v err=%v", replayed, replay, err)
	}
	var refs map[string]string
	if err := json.Unmarshal(command.ResultRefs, &refs); err != nil {
		t.Fatal(err)
	}
	newOperationID := refs["operation_id"]
	if newOperationID == "" || newOperationID == accepted.OperationID || refs["failed_operation_id"] != accepted.OperationID || refs["amount_minor"] != "2000" {
		t.Fatalf("wrong recovered retry references: %+v", refs)
	}
	var newStatus, outboxStatus string
	if err := l.db.QueryRowContext(ctx, `SELECT status FROM payment_operations WHERE id=?`, newOperationID).Scan(&newStatus); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT status FROM payment_operations WHERE id=?`, accepted.OperationID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT status FROM outbox WHERE id=?`, "capture:"+newOperationID).Scan(&outboxStatus); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM payment_operations WHERE invoice_id=?`, accepted.InvoiceID).Scan(&operations); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM payment_retry_requests WHERE key=? AND operation_id=?`, "admin:"+command.ID, newOperationID).Scan(&retryRequests); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if status != "definitively_failed" || newStatus != "created" || outboxStatus != "pending" || operations != 2 || retryRequests != 1 || receipts != 1 || captureCount(t, l) != providerAttempts {
		t.Fatalf("recovered retry: old=%s new=%s outbox=%s operations=%d retry_requests=%d receipts=%d", status, newStatus, outboxStatus, operations, retryRequests, receipts)
	}
	if _, err := l.DispatchCapture(ctx, newOperationID, ""); err != nil {
		t.Fatal(err)
	}
	balance, err := l.Balance(ctx, accepted.InvoiceID)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT status FROM payment_operations WHERE id=?`, newOperationID).Scan(&newStatus); err != nil {
		t.Fatal(err)
	}
	if newStatus != "succeeded" || balance.GrossCapturedMinor != 2000 || balance.OutstandingMinor != 0 || captureCount(t, l) != providerAttempts+1 {
		t.Fatalf("recovered retry did not collect exactly once: status=%s balance=%+v", newStatus, balance)
	}
}

func TestAdminRetryPaymentTargetsDefinitivelyFailedOperation(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	l, err := Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	quote, err := l.CreateQuote(ctx, "admin-retry-customer", "basic")
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "admin-retry-checkout")
	if err != nil {
		t.Fatal(err)
	}
	if err := l.SetFakePaymentDecision(ctx, accepted.OperationID, "definitively_failed"); err != nil {
		t.Fatal(err)
	}
	if _, err := l.DispatchNext(ctx, ""); err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{}`)
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C08", accepted.OperationID, payload)
	if err != nil {
		t.Fatal(err)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "retry-payment-001", "C08", accepted.OperationID, payload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	command, err = l.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "succeeded" {
		t.Fatalf("execute: %+v %v", command, err)
	}
	var refs map[string]string
	if err := json.Unmarshal(command.ResultRefs, &refs); err != nil {
		t.Fatal(err)
	}
	if refs["operation_id"] == "" || refs["operation_id"] == accepted.OperationID || refs["failed_operation_id"] != accepted.OperationID {
		t.Fatalf("wrong refs: %+v", refs)
	}
	var retries, receipts int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM payment_retry_requests WHERE operation_id=?`, refs["operation_id"]).Scan(&retries); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if retries != 1 || receipts != 1 {
		t.Fatalf("retries=%d receipts=%d", retries, receipts)
	}
}

func TestAdminRetryPaymentAfterReplacingUnsentOperation(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	l, err := Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	quote, err := l.CreateQuote(ctx, "replaced-retry-customer", "basic")
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "replaced-retry-checkout")
	if err != nil {
		t.Fatal(err)
	}

	partialPayload := json.RawMessage(`{"amount_minor":"1000"}`)
	partialPreview, err := l.AdminCreatePreview(ctx, "local-admin", "C07", accepted.InvoiceID, partialPayload)
	if err != nil {
		t.Fatal(err)
	}
	partial, _, err := l.AdminSubmitCommand(ctx, "local-admin", "replaced-retry-partial-001", "C07", accepted.InvoiceID, partialPayload, partialPreview.ID)
	if err != nil {
		t.Fatal(err)
	}
	partial, err = l.AdminExecuteCommand(ctx, partial.ID)
	if err != nil || partial.Status != "succeeded" {
		t.Fatalf("partial payment: %+v %v", partial, err)
	}
	var partialRefs map[string]string
	if err := json.Unmarshal(partial.ResultRefs, &partialRefs); err != nil {
		t.Fatal(err)
	}
	partialID := partialRefs["operation_id"]
	if partialID == "" {
		t.Fatal("partial operation missing")
	}
	if err := l.SetFakePaymentDecision(ctx, partialID, "definitively_failed"); err != nil {
		t.Fatal(err)
	}
	if _, err := l.DispatchCapture(ctx, partialID, ""); err != nil {
		t.Fatal(err)
	}

	retryPayload := json.RawMessage(`{}`)
	retryPreview, err := l.AdminCreatePreview(ctx, "local-admin", "C08", partialID, retryPayload)
	if err != nil {
		t.Fatalf("cancelled replaced operation must not block retry: %v", err)
	}
	retry, _, err := l.AdminSubmitCommand(ctx, "local-admin", "replaced-retry-command-001", "C08", partialID, retryPayload, retryPreview.ID)
	if err != nil {
		t.Fatal(err)
	}
	retry, err = l.AdminExecuteCommand(ctx, retry.ID)
	if err != nil || retry.Status != "succeeded" {
		t.Fatalf("retry: %+v %v", retry, err)
	}
	var refs map[string]string
	if err := json.Unmarshal(retry.ResultRefs, &refs); err != nil {
		t.Fatal(err)
	}
	if refs["failed_operation_id"] != partialID || refs["operation_id"] == "" || refs["operation_id"] == partialID {
		t.Fatalf("wrong retry references: %+v", refs)
	}
	var cancelled, amount, retries int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM payment_operations WHERE id=? AND status='cancelled'`, accepted.OperationID).Scan(&cancelled); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT amount_minor FROM payment_operations WHERE id=?`, refs["operation_id"]).Scan(&amount); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM payment_retry_requests WHERE operation_id=?`, refs["operation_id"]).Scan(&retries); err != nil {
		t.Fatal(err)
	}
	if cancelled != 1 || amount != 2000 || retries != 1 {
		t.Fatalf("cancelled=%d retry amount=%d retry records=%d", cancelled, amount, retries)
	}
}
