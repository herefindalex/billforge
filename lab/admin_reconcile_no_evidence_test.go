package lab

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestAdminReconcilePaymentWaitsForTerminalProviderEvidence(t *testing.T) {
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
	quote, err := l.CreateQuote(ctx, "reconcile-wait-customer", "basic")
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "reconcile-wait-checkout")
	if err != nil {
		t.Fatal(err)
	}
	var providerKey, currency string
	var amount int64
	if err := l.db.QueryRowContext(ctx, `SELECT provider_key,amount_minor,currency FROM payment_operations WHERE id=?`, accepted.OperationID).Scan(&providerKey, &amount, &currency); err != nil {
		t.Fatal(err)
	}
	// Model a process stop after committing the local submitted state and before
	// the provider call. The external outcome is not yet known.
	if _, err := l.db.ExecContext(ctx, `UPDATE payment_operations SET status='submitted' WHERE id=?`, accepted.OperationID); err != nil {
		t.Fatal(err)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "reconcile-no-evidence-payment", "C10", accepted.OperationID, json.RawMessage(`{}`), "")
	if err != nil {
		t.Fatal(err)
	}
	command, err = l.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "waiting_verification" {
		t.Fatalf("missing provider evidence ended payment verification: command=%+v err=%v", command, err)
	}
	var receipts int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil || receipts != 0 {
		t.Fatalf("unverified payment got a receipt: count=%d err=%v", receipts, err)
	}
	if _, err := l.provider.Capture(ctx, providerKey, amount, currency); err != nil {
		t.Fatal(err)
	}
	command, err = l.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "succeeded" {
		t.Fatalf("payment evidence did not complete original command: command=%+v err=%v", command, err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil || receipts != 1 {
		t.Fatalf("verified payment receipt count=%d err=%v", receipts, err)
	}
}

func TestAdminReconcileRefundWaitsForTerminalProviderEvidence(t *testing.T) {
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
	quote, err := l.CreateQuote(ctx, "refund-reconcile-wait-customer", "basic")
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "refund-reconcile-wait-checkout")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.DispatchNext(ctx, ""); err != nil {
		t.Fatal(err)
	}
	correction, err := l.PostReduction(ctx, accepted.InvoiceID, 1000, "refund verification", "refund-reconcile-wait-reduction")
	if err != nil || len(correction.GrantIDs) != 1 {
		t.Fatalf("funded credit: %+v err=%v", correction, err)
	}
	refundID, err := l.ReserveRefund(ctx, correction.GrantIDs[0], 500, "refund-reconcile-wait-reservation")
	if err != nil {
		t.Fatal(err)
	}
	var providerKey, sourceKey, currency string
	var amount int64
	if err := l.db.QueryRowContext(ctx, `SELECT provider_key,source_provider_key,amount_minor,currency FROM refund_operations WHERE id=?`, refundID).Scan(&providerKey, &sourceKey, &amount, &currency); err != nil {
		t.Fatal(err)
	}
	if _, err := l.db.ExecContext(ctx, `UPDATE refund_operations SET status='submitted' WHERE id=?`, refundID); err != nil {
		t.Fatal(err)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "reconcile-no-evidence-refund", "C17", refundID, json.RawMessage(`{}`), "")
	if err != nil {
		t.Fatal(err)
	}
	command, err = l.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "waiting_verification" {
		t.Fatalf("missing provider evidence ended refund verification: command=%+v err=%v", command, err)
	}
	credit, err := l.CreditBalance(ctx, correction.GrantIDs[0])
	if err != nil || credit.ReservedMinor != 500 || credit.AvailableMinor != 500 {
		t.Fatalf("unverified refund released credit: balance=%+v err=%v", credit, err)
	}
	if _, err := l.provider.Refund(ctx, providerKey, sourceKey, amount, currency); err != nil {
		t.Fatal(err)
	}
	command, err = l.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "succeeded" {
		t.Fatalf("refund evidence did not complete original command: command=%+v err=%v", command, err)
	}
	credit, err = l.CreditBalance(ctx, correction.GrantIDs[0])
	if err != nil || credit.ReservedMinor != 0 || credit.RefundedMinor != 500 || credit.AvailableMinor != 500 {
		t.Fatalf("verified refund balance=%+v err=%v", credit, err)
	}
}
