package lab

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

func TestAdminPaymentDispatchAndReconciliationReceipts(t *testing.T) {
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
	quote, err := l.CreateQuote(ctx, "admin-dispatch-customer", "basic")
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "admin-dispatch-checkout")
	if err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{}`)
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C09", accepted.OperationID, payload)
	if err != nil {
		t.Fatal(err)
	}
	dispatch, _, err := l.AdminSubmitCommand(ctx, "local-admin", "dispatch-payment-001", "C09", accepted.OperationID, payload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	dispatch, err = l.AdminExecuteCommand(ctx, dispatch.ID)
	if err != nil || dispatch.Status != "succeeded" {
		t.Fatalf("dispatch: %+v %v", dispatch, err)
	}
	reconcile, _, err := l.AdminSubmitCommand(ctx, "local-admin", "reconcile-payment-001", "C10", accepted.OperationID, payload, "")
	if err != nil {
		t.Fatal(err)
	}
	reconcile, err = l.AdminExecuteCommand(ctx, reconcile.ID)
	if err != nil || reconcile.Status != "succeeded" {
		t.Fatalf("reconcile: %+v %v", reconcile, err)
	}
	var refs map[string]string
	if err := json.Unmarshal(reconcile.ResultRefs, &refs); err != nil {
		t.Fatal(err)
	}
	if refs["provider_found"] != "true" || refs["operation_status"] != "succeeded" {
		t.Fatalf("reconcile refs: %+v", refs)
	}
	var receipts int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id IN (?,?)`, dispatch.ID, reconcile.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if receipts != 2 {
		t.Fatalf("receipts=%d", receipts)
	}
}

func TestAdminPaymentDispatchReceiptFailureRecoversWithoutSecondCapture(t *testing.T) {
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
	quote, err := l.CreateQuote(ctx, "dispatch-receipt-failure-customer", "basic")
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "dispatch-receipt-failure-checkout")
	if err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{}`)
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C09", accepted.OperationID, payload)
	if err != nil {
		t.Fatal(err)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "dispatch-receipt-failure-key", "C09", accepted.OperationID, payload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.db.ExecContext(ctx, `CREATE TEMP TRIGGER fail_dispatch_receipt BEFORE INSERT ON admin_command_receipts BEGIN SELECT RAISE(ABORT, 'injected receipt failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := l.AdminExecuteCommand(ctx, command.ID); err == nil {
		t.Fatal("receipt failure must leave external dispatch recoverable")
	}
	balance, err := l.Balance(ctx, accepted.InvoiceID)
	if err != nil {
		t.Fatal(err)
	}
	var operationStatus string
	var allocations, receipts int
	if err := l.db.QueryRowContext(ctx, `SELECT status FROM payment_operations WHERE id=?`, accepted.OperationID).Scan(&operationStatus); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM allocations WHERE operation_id=?`, accepted.OperationID).Scan(&allocations); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if operationStatus != "succeeded" || balance.GrossCapturedMinor != 2000 || balance.OutstandingMinor != 0 || allocations != 1 || receipts != 0 || captureCount(t, l) != 1 {
		t.Fatalf("external capture before receipt recovery: operation=%s balance=%+v allocations=%d receipts=%d", operationStatus, balance, allocations, receipts)
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
		t.Fatalf("dispatch command did not recover: %+v err=%v", command, err)
	}
	var refs map[string]string
	if err := json.Unmarshal(command.ResultRefs, &refs); err != nil {
		t.Fatal(err)
	}
	if refs["target_id"] != accepted.OperationID || refs["operation_status"] != "succeeded" {
		t.Fatalf("recovered dispatch references wrong operation: %+v", refs)
	}
	replayed, replay, err := l.AdminSubmitCommand(ctx, "local-admin", "dispatch-receipt-failure-key", "C09", accepted.OperationID, payload, preview.ID)
	if err != nil || !replay || replayed.ID != command.ID {
		t.Fatalf("original key did not replay recovered dispatch: %+v replay=%v err=%v", replayed, replay, err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM allocations WHERE operation_id=?`, accepted.OperationID).Scan(&allocations); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if allocations != 1 || receipts != 1 || captureCount(t, l) != 1 {
		t.Fatalf("dispatch recovery repeated a financial effect: allocations=%d receipts=%d", allocations, receipts)
	}
}

func TestAdminPaymentDispatchWaitsForProviderEvidence(t *testing.T) {
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
	quote, err := l.CreateQuote(ctx, "admin-unknown-customer", "basic")
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "admin-unknown-checkout")
	if err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{}`)
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C09", accepted.OperationID, payload)
	if err != nil {
		t.Fatal(err)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "dispatch-payment-unknown-001", "C09", accepted.OperationID, payload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.db.ExecContext(ctx, `UPDATE payment_operations SET status='unknown' WHERE id=?`, accepted.OperationID); err != nil {
		t.Fatal(err)
	}
	command, err = l.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "waiting_verification" {
		t.Fatalf("waiting: %+v %v", command, err)
	}
	var key, currency string
	var amount int64
	if err := l.db.QueryRowContext(ctx, `SELECT provider_key,amount_minor,currency FROM payment_operations WHERE id=?`, accepted.OperationID).Scan(&key, &amount, &currency); err != nil {
		t.Fatal(err)
	}
	if _, err := l.provider.Capture(ctx, key, amount, currency); err != nil {
		t.Fatal(err)
	}
	command, err = l.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "succeeded" {
		t.Fatalf("resumed: %+v %v", command, err)
	}
	var receipts int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if receipts != 1 {
		t.Fatalf("receipt=%d", receipts)
	}
}

func TestAdminRefundDispatchReceiptFailureRecoversWithoutSecondRefund(t *testing.T) {
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
	quote, err := l.CreateQuote(ctx, "refund-receipt-failure-customer", "basic")
	if err != nil {
		t.Fatal(err)
	}
	paid, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "refund-receipt-failure-checkout")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.DispatchNext(ctx, ""); err != nil {
		t.Fatal(err)
	}
	correction, err := l.PostReduction(ctx, paid.InvoiceID, 1000, "service credit", "refund-receipt-failure-reduction")
	if err != nil || len(correction.GrantIDs) != 1 {
		t.Fatalf("correction: %+v err=%v", correction, err)
	}
	grantID := correction.GrantIDs[0]
	refundID, err := l.ReserveRefund(ctx, grantID, 400, "refund-receipt-failure-reservation")
	if err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{}`)
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C16", refundID, payload)
	if err != nil {
		t.Fatal(err)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "refund-receipt-failure-key", "C16", refundID, payload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.db.ExecContext(ctx, `CREATE TEMP TRIGGER fail_refund_dispatch_receipt BEFORE INSERT ON admin_command_receipts BEGIN SELECT RAISE(ABORT, 'injected receipt failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := l.AdminExecuteCommand(ctx, command.ID); err == nil {
		t.Fatal("receipt failure must leave external refund recoverable")
	}
	credit, err := l.CreditBalance(ctx, grantID)
	if err != nil {
		t.Fatal(err)
	}
	var refundStatus string
	var providerRefunds, receipts int
	if err := l.db.QueryRowContext(ctx, `SELECT status FROM refund_operations WHERE id=?`, refundID).Scan(&refundStatus); err != nil {
		t.Fatal(err)
	}
	if err := l.provider.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM refunds`).Scan(&providerRefunds); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if refundStatus != "succeeded" || credit.RefundedMinor != 400 || credit.ReservedMinor != 0 || providerRefunds != 1 || receipts != 0 {
		t.Fatalf("external refund before receipt recovery: status=%s credit=%+v provider_refunds=%d receipts=%d", refundStatus, credit, providerRefunds, receipts)
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
		t.Fatalf("refund command did not recover: %+v err=%v", command, err)
	}
	var refs map[string]string
	if err := json.Unmarshal(command.ResultRefs, &refs); err != nil {
		t.Fatal(err)
	}
	if refs["target_id"] != refundID || refs["operation_status"] != "succeeded" {
		t.Fatalf("recovered refund references wrong operation: %+v", refs)
	}
	replayed, replay, err := l.AdminSubmitCommand(ctx, "local-admin", "refund-receipt-failure-key", "C16", refundID, payload, preview.ID)
	if err != nil || !replay || replayed.ID != command.ID {
		t.Fatalf("original key did not replay recovered refund: %+v replay=%v err=%v", replayed, replay, err)
	}
	credit, err = l.CreditBalance(ctx, grantID)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.provider.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM refunds`).Scan(&providerRefunds); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if credit.RefundedMinor != 400 || credit.ReservedMinor != 0 || providerRefunds != 1 || receipts != 1 {
		t.Fatalf("refund recovery repeated a financial effect: credit=%+v provider_refunds=%d receipts=%d", credit, providerRefunds, receipts)
	}
}

func TestAdminRefundDispatchTargetsExactRefund(t *testing.T) {
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
	quote, err := l.CreateQuote(ctx, "admin-refund-dispatch-customer", "basic")
	if err != nil {
		t.Fatal(err)
	}
	paid, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "admin-refund-dispatch-checkout")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.DispatchNext(ctx, ""); err != nil {
		t.Fatal(err)
	}
	correction, err := l.PostReduction(ctx, paid.InvoiceID, 1000, "service credit", "admin-refund-dispatch-reduction")
	if err != nil || len(correction.GrantIDs) != 1 {
		t.Fatalf("correction: %+v %v", correction, err)
	}
	firstRefund, err := l.ReserveRefund(ctx, correction.GrantIDs[0], 400, "admin-refund-dispatch-first")
	if err != nil {
		t.Fatal(err)
	}
	secondRefund, err := l.ReserveRefund(ctx, correction.GrantIDs[0], 300, "admin-refund-dispatch-second")
	if err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{}`)
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C16", secondRefund, payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := l.AdminSubmitCommand(ctx, "other-admin", "foreign-c16-001", "C16", secondRefund, payload, preview.ID); !errors.Is(err, ErrAdminPreviewStale) {
		t.Fatalf("other actor used C16 preview: %v", err)
	}
	var foreignCommands int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_commands WHERE actor_id='other-admin' AND action_id='C16'`).Scan(&foreignCommands); err != nil {
		t.Fatal(err)
	}
	if foreignCommands != 0 {
		t.Fatalf("other actor wrote %d C16 commands", foreignCommands)
	}
	dispatch, _, err := l.AdminSubmitCommand(ctx, "local-admin", "dispatch-refund-001", "C16", secondRefund, payload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	dispatch, err = l.AdminExecuteCommand(ctx, dispatch.ID)
	if err != nil || dispatch.Status != "succeeded" {
		t.Fatalf("dispatch: %+v %v", dispatch, err)
	}
	first, err := l.Refund(ctx, firstRefund)
	if err != nil || first.Status != "created" {
		t.Fatalf("wrong refund dispatched: %+v %v", first, err)
	}
	second, err := l.Refund(ctx, secondRefund)
	if err != nil || second.Status != "succeeded" {
		t.Fatalf("second refund: %+v %v", second, err)
	}
	reconcile, _, err := l.AdminSubmitCommand(ctx, "local-admin", "reconcile-refund-001", "C17", secondRefund, payload, "")
	if err != nil {
		t.Fatal(err)
	}
	reconcile, err = l.AdminExecuteCommand(ctx, reconcile.ID)
	if err != nil || reconcile.Status != "succeeded" {
		t.Fatalf("reconcile: %+v %v", reconcile, err)
	}
	var refs map[string]string
	if err := json.Unmarshal(reconcile.ResultRefs, &refs); err != nil {
		t.Fatal(err)
	}
	if refs["provider_found"] != "true" || refs["operation_status"] != "succeeded" {
		t.Fatalf("wrong refs: %+v", refs)
	}
}
