package lab

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

func TestAdminRefundReconcileResolvesUnknownWithoutSecondRefund(t *testing.T) {
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
	paid := paidBasicSubscription(t, l, "admin-unknown-refund")
	correction, err := l.PostReduction(ctx, paid.InvoiceID, 1000, "refund after reduction", "admin-unknown-refund-reduction")
	if err != nil || len(correction.GrantIDs) != 1 {
		t.Fatalf("correction: %+v, %v", correction, err)
	}
	refundID, err := l.ReserveRefund(ctx, correction.GrantIDs[0], 500, "admin-unknown-refund-reserve")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.DispatchRefund(ctx, refundID, "lost_response"); !errors.Is(err, ErrPaymentUnknown) {
		t.Fatalf("lost refund response: %v", err)
	}
	before, err := l.Refund(ctx, refundID)
	if err != nil || before.Status != "unknown" {
		t.Fatalf("unresolved refund: %+v, %v", before, err)
	}
	beforeBalance, err := l.CreditBalance(ctx, correction.GrantIDs[0])
	if err != nil || beforeBalance.ReservedMinor != 500 || beforeBalance.RefundedMinor != 0 || beforeBalance.AvailableMinor != 500 {
		t.Fatalf("unknown refund released reservation: %+v, %v", beforeBalance, err)
	}

	command, replay, err := l.AdminSubmitCommand(ctx, "local-admin", "reconcile-unknown-refund", "C17", refundID, json.RawMessage(`{}`), "")
	if err != nil || replay {
		t.Fatalf("submit reconcile: %+v replay=%t err=%v", command, replay, err)
	}
	command, err = l.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "succeeded" {
		t.Fatalf("execute reconcile: %+v, %v", command, err)
	}
	var refs map[string]string
	if err := json.Unmarshal(command.ResultRefs, &refs); err != nil {
		t.Fatal(err)
	}
	if refs["provider_found"] != "true" || refs["operation_status"] != "succeeded" {
		t.Fatalf("reconcile result: %+v", refs)
	}
	after, err := l.Refund(ctx, refundID)
	if err != nil || after.Status != "succeeded" || after.AmountMinor != 500 {
		t.Fatalf("reconciled refund: %+v, %v", after, err)
	}
	afterBalance, err := l.CreditBalance(ctx, correction.GrantIDs[0])
	if err != nil || afterBalance.ReservedMinor != 0 || afterBalance.RefundedMinor != 500 || afterBalance.AvailableMinor != 500 {
		t.Fatalf("reconciled refund funding: %+v, %v", afterBalance, err)
	}
	replayed, replay, err := l.AdminSubmitCommand(ctx, "local-admin", "reconcile-unknown-refund", "C17", refundID, json.RawMessage(`{}`), "")
	if err != nil || !replay || replayed.ID != command.ID || replayed.Status != "succeeded" {
		t.Fatalf("original command replay: %+v replay=%t err=%v", replayed, replay, err)
	}
	var providerRefunds, receipts int
	if err := l.provider.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM refunds`).Scan(&providerRefunds); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if providerRefunds != 1 || receipts != 1 {
		t.Fatalf("reconcile duplicated effect: provider_refunds=%d receipts=%d", providerRefunds, receipts)
	}
}
