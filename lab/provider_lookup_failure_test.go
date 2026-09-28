package lab

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

func TestProviderLookupFailureKeepsPaymentAndRefundUnknownUntilRecovered(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	providerPath := filepath.Join(dir, "provider.db")
	l, err := Open(filepath.Join(dir, "commerce.db"), providerPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	beginUnavailableCommand := func(actionID, targetID, key string) AdminCommand {
		t.Helper()
		command, replay, err := l.AdminSubmitCommand(ctx, "local-admin", key, actionID, targetID, json.RawMessage(`{}`), "")
		if err != nil || replay {
			t.Fatalf("admit %s command=%+v replay=%t err=%v", actionID, command, replay, err)
		}
		if _, err := l.AdminExecuteCommand(ctx, command.ID); err == nil {
			t.Fatalf("%s completed despite unavailable provider", actionID)
		}
		stored, err := l.AdminCommand(ctx, command.ID)
		if err != nil || stored.Status != "accepted" {
			t.Fatalf("%s lost pending command: %+v err=%v", actionID, stored, err)
		}
		var receipts int
		if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil || receipts != 0 {
			t.Fatalf("%s got premature receipt: count=%d err=%v", actionID, receipts, err)
		}
		return command
	}
	finishOriginalCommand := func(command AdminCommand, key string) {
		t.Helper()
		for range 2 {
			finished, err := l.AdminExecuteCommand(ctx, command.ID)
			if err != nil || finished.Status != "succeeded" {
				t.Fatalf("recover %s command=%+v err=%v", command.ActionID, finished, err)
			}
		}
		replayed, replay, err := l.AdminSubmitCommand(ctx, "local-admin", key, command.ActionID, command.TargetID, json.RawMessage(`{}`), "")
		if err != nil || !replay || replayed.ID != command.ID {
			t.Fatalf("original %s key replayed=%+v replay=%t err=%v", command.ActionID, replayed, replay, err)
		}
		var receipts int
		if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil || receipts != 1 {
			t.Fatalf("%s receipts=%d err=%v, want one", command.ActionID, receipts, err)
		}
	}
	quote, err := l.CreateQuote(ctx, "provider-lookup-customer", "basic")
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "provider-lookup-purchase")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.DispatchCapture(ctx, receipt.OperationID, "lost_response"); !errors.Is(err, ErrPaymentUnknown) {
		t.Fatalf("lost provider response = %v, want payment unknown", err)
	}
	assertLocalPayment := func(wantStatus string, wantAllocations int) {
		t.Helper()
		var status string
		if err := l.db.QueryRowContext(ctx, `SELECT status FROM payment_operations WHERE id=?`, receipt.OperationID).Scan(&status); err != nil {
			t.Fatal(err)
		}
		var allocations int
		if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM allocations WHERE operation_id=?`, receipt.OperationID).Scan(&allocations); err != nil {
			t.Fatal(err)
		}
		if status != wantStatus || allocations != wantAllocations {
			t.Fatalf("local payment status=%s allocations=%d, want %s/%d", status, allocations, wantStatus, wantAllocations)
		}
	}
	assertLocalPayment("unknown", 0)
	if err := l.provider.Close(); err != nil {
		t.Fatal(err)
	}
	if found, err := l.ReconcilePayment(ctx, receipt.OperationID); err == nil || found {
		t.Fatalf("unavailable provider lookup found=%t err=%v, want error without a result", found, err)
	}
	paymentCommand := beginUnavailableCommand("C10", receipt.OperationID, "provider-lookup-C10")
	assertLocalPayment("unknown", 0)
	l.provider, err = openProvider(providerPath)
	if err != nil {
		t.Fatal(err)
	}
	finishOriginalCommand(paymentCommand, "provider-lookup-C10")
	assertLocalPayment("succeeded", 1)
	var captures int
	if err := l.provider.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM captures`).Scan(&captures); err != nil {
		t.Fatal(err)
	}
	if captures != 1 {
		t.Fatalf("provider captures = %d, want 1", captures)
	}
	correction, err := l.PostReduction(ctx, receipt.InvoiceID, 1000, "provider lookup refund", "provider-lookup-reduction")
	if err != nil || len(correction.GrantIDs) != 1 {
		t.Fatalf("reduction %+v: %v", correction, err)
	}
	refundID, err := l.ReserveRefund(ctx, correction.GrantIDs[0], 500, "provider-lookup-refund")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.DispatchRefund(ctx, refundID, "lost_response"); !errors.Is(err, ErrPaymentUnknown) {
		t.Fatalf("lost refund response = %v, want unknown", err)
	}
	assertRefund := func(want string) {
		t.Helper()
		refund, err := l.Refund(ctx, refundID)
		if err != nil {
			t.Fatal(err)
		}
		if refund.Status != want {
			t.Fatalf("refund status = %s, want %s", refund.Status, want)
		}
	}
	assertRefund("unknown")
	if err := l.provider.Close(); err != nil {
		t.Fatal(err)
	}
	if found, err := l.ReconcileRefund(ctx, refundID); err == nil || found {
		t.Fatalf("unavailable refund lookup found=%t err=%v, want error without a result", found, err)
	}
	refundCommand := beginUnavailableCommand("C17", refundID, "provider-lookup-C17")
	assertRefund("unknown")
	l.provider, err = openProvider(providerPath)
	if err != nil {
		t.Fatal(err)
	}
	finishOriginalCommand(refundCommand, "provider-lookup-C17")
	assertRefund("succeeded")
	var refunds int
	if err := l.provider.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM refunds`).Scan(&refunds); err != nil {
		t.Fatal(err)
	}
	if refunds != 1 {
		t.Fatalf("provider refunds = %d, want 1", refunds)
	}
}
