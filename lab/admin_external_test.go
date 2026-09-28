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
