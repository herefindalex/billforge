package lab

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestAdminReserveRefundHasFundedGrantAndAtomicReceipt(t *testing.T) {
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
	quote, err := l.CreateQuote(ctx, "admin-refund-customer", "basic")
	if err != nil {
		t.Fatal(err)
	}
	paid, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "admin-refund-checkout")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.DispatchNext(ctx, ""); err != nil {
		t.Fatal(err)
	}
	correction, err := l.PostReduction(ctx, paid.InvoiceID, 1000, "service credit", "admin-refund-correction")
	if err != nil || len(correction.GrantIDs) != 1 {
		t.Fatalf("correction: %+v %v", correction, err)
	}
	grantID := correction.GrantIDs[0]
	payload := json.RawMessage(`{"amount_minor":"500"}`)
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C15", grantID, payload)
	if err != nil {
		t.Fatal(err)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "reserve-refund-001", "C15", grantID, payload, preview.ID)
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
	if refs["refund_id"] == "" {
		t.Fatalf("missing refund ID: %+v", refs)
	}
	var refundAmount int64
	var refundStatus string
	if err := l.db.QueryRowContext(ctx, `SELECT amount_minor,status FROM refund_operations WHERE id=?`, refs["refund_id"]).Scan(&refundAmount, &refundStatus); err != nil {
		t.Fatal(err)
	}
	var receipt int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipt); err != nil {
		t.Fatal(err)
	}
	if refundAmount != 500 || refundStatus != "created" || receipt != 1 {
		t.Fatalf("refund=%d status=%s receipt=%d", refundAmount, refundStatus, receipt)
	}
}

func TestAdminReserveRefundReceiptFailureRollsBackReservationAndRecoversOnce(t *testing.T) {
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
	correction, err := l.PostReduction(ctx, paid.InvoiceID, 1000, "service credit", "refund-receipt-failure-correction")
	if err != nil || len(correction.GrantIDs) != 1 {
		t.Fatalf("correction: %+v %v", correction, err)
	}
	grantID := correction.GrantIDs[0]
	payload := json.RawMessage(`{"amount_minor":"500"}`)
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C15", grantID, payload)
	if err != nil {
		t.Fatal(err)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "refund-receipt-failure-001", "C15", grantID, payload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.db.ExecContext(ctx, `CREATE TEMP TRIGGER fail_refund_receipt BEFORE INSERT ON admin_command_receipts BEGIN SELECT RAISE(ABORT, 'injected receipt failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := l.AdminExecuteCommand(ctx, command.ID); err == nil {
		t.Fatal("receipt failure must abort the refund reservation")
	}
	for _, check := range []struct {
		query string
		args  []any
	}{
		{`SELECT COUNT(*) FROM refund_operations WHERE grant_id=?`, []any{grantID}},
		{`SELECT COUNT(*) FROM outbox WHERE kind='refund'`, nil},
		{`SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, []any{command.ID}},
	} {
		var count int
		if err := l.db.QueryRowContext(ctx, check.query, check.args...).Scan(&count); err != nil || count != 0 {
			t.Fatalf("failed receipt left a financial fact: query=%s count=%d err=%v", check.query, count, err)
		}
	}
	balance, err := l.CreditBalance(ctx, grantID)
	if err != nil || balance.ReservedMinor != 0 || balance.AvailableMinor != 1000 {
		t.Fatalf("failed receipt reserved credit: %+v err=%v", balance, err)
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
		t.Fatalf("refund reservation did not recover: %+v err=%v", command, err)
	}
	replayed, replay, err := l.AdminSubmitCommand(ctx, "local-admin", "refund-receipt-failure-001", "C15", grantID, payload, preview.ID)
	if err != nil || !replay || replayed.ID != command.ID {
		t.Fatalf("same request key changed recovered command: %+v replay=%v err=%v", replayed, replay, err)
	}
	var refunds, reserved, outbox, receipts, providerRefunds int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(amount_minor),0) FROM refund_operations WHERE grant_id=?`, grantID).Scan(&refunds, &reserved); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox WHERE kind='refund'`).Scan(&outbox); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if err := l.provider.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM refunds`).Scan(&providerRefunds); err != nil {
		t.Fatal(err)
	}
	balance, err = l.CreditBalance(ctx, grantID)
	if err != nil || refunds != 1 || reserved != 500 || outbox != 1 || receipts != 1 || providerRefunds != 0 || balance.ReservedMinor != 500 || balance.AvailableMinor != 500 {
		t.Fatalf("recovered refund state: refunds=%d reserved=%d outbox=%d receipts=%d provider=%d balance=%+v err=%v", refunds, reserved, outbox, receipts, providerRefunds, balance, err)
	}
}

func TestAdminReserveRefundRejectsStaleSourceEvenWhenBudgetStillFits(t *testing.T) {
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
	quote, err := l.CreateQuote(ctx, "refund-stale-source", "basic")
	if err != nil {
		t.Fatal(err)
	}
	paid, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "refund-stale-checkout")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.DispatchNext(ctx, ""); err != nil {
		t.Fatal(err)
	}
	correction, err := l.PostReduction(ctx, paid.InvoiceID, 1000, "refund budget", "refund-stale-correction")
	if err != nil || len(correction.GrantIDs) != 1 {
		t.Fatalf("correction: %+v %v", correction, err)
	}
	grantID := correction.GrantIDs[0]
	payload := json.RawMessage(`{"amount_minor":"500"}`)
	stalePreview, err := l.AdminCreatePreview(ctx, "local-admin", "C15", grantID, payload)
	if err != nil {
		t.Fatal(err)
	}
	winningPreview, err := l.AdminCreatePreview(ctx, "local-admin", "C15", grantID, payload)
	if err != nil {
		t.Fatal(err)
	}
	staleCommand, _, err := l.AdminSubmitCommand(ctx, "local-admin", "refund-stale-first", "C15", grantID, payload, stalePreview.ID)
	if err != nil {
		t.Fatal(err)
	}
	winningCommand, _, err := l.AdminSubmitCommand(ctx, "local-admin", "refund-stale-winner", "C15", grantID, payload, winningPreview.ID)
	if err != nil {
		t.Fatal(err)
	}
	winningCommand, err = l.AdminExecuteCommand(ctx, winningCommand.ID)
	if err != nil || winningCommand.Status != "succeeded" {
		t.Fatalf("winning command: %+v %v", winningCommand, err)
	}
	winningReplay, replay, err := l.AdminSubmitCommand(ctx, "local-admin", "refund-stale-winner", "C15", grantID, payload, winningPreview.ID)
	if err != nil || !replay || winningReplay.ID != winningCommand.ID || winningReplay.Status != "succeeded" {
		t.Fatalf("winning replay: %+v replay=%v err=%v", winningReplay, replay, err)
	}
	staleCommand, err = l.AdminExecuteCommand(ctx, staleCommand.ID)
	if err != nil || staleCommand.Status != "failed" || staleCommand.ErrorCode != "PREVIEW_STALE" {
		t.Fatalf("stale command: %+v %v", staleCommand, err)
	}
	replayed, replay, err := l.AdminSubmitCommand(ctx, "local-admin", "refund-stale-first", "C15", grantID, payload, stalePreview.ID)
	if err != nil || !replay || replayed.ID != staleCommand.ID || replayed.Status != "failed" || replayed.ErrorCode != "PREVIEW_STALE" {
		t.Fatalf("stale replay: %+v replay=%v err=%v", replayed, replay, err)
	}
	var refundCount, refundTotal, receiptCount int64
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(amount_minor),0) FROM refund_operations WHERE grant_id=?`, grantID).Scan(&refundCount, &refundTotal); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, staleCommand.ID).Scan(&receiptCount); err != nil {
		t.Fatal(err)
	}
	if refundCount != 1 || refundTotal != 500 || receiptCount != 0 {
		t.Fatalf("stale command affected money: refunds=%d total=%d receipts=%d", refundCount, refundTotal, receiptCount)
	}
	freshPreview, err := l.AdminCreatePreview(ctx, "local-admin", "C15", grantID, payload)
	if err != nil {
		t.Fatal(err)
	}
	freshCommand, _, err := l.AdminSubmitCommand(ctx, "local-admin", "refund-stale-fresh", "C15", grantID, payload, freshPreview.ID)
	if err != nil {
		t.Fatal(err)
	}
	freshCommand, err = l.AdminExecuteCommand(ctx, freshCommand.ID)
	if err != nil || freshCommand.Status != "succeeded" {
		t.Fatalf("fresh command: %+v %v", freshCommand, err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(amount_minor),0) FROM refund_operations WHERE grant_id=?`, grantID).Scan(&refundCount, &refundTotal); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id IN (?,?)`, winningCommand.ID, freshCommand.ID).Scan(&receiptCount); err != nil {
		t.Fatal(err)
	}
	if refundCount != 2 || refundTotal != 1000 || receiptCount != 2 {
		t.Fatalf("reconfirmed reservations: refunds=%d total=%d receipts=%d", refundCount, refundTotal, receiptCount)
	}
}
