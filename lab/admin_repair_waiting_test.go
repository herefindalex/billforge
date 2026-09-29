package lab

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strconv"
	"testing"
)

func TestAdminRepairLookupWaitsUntilOriginalProviderEvidenceArrives(t *testing.T) {
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
	quote, err := l.CreateQuote(ctx, "repair-wait", "basic")
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "repair-wait-checkout")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.db.ExecContext(ctx, `UPDATE payment_operations SET status='submitted' WHERE id=? AND status='created'`, accepted.OperationID); err != nil {
		t.Fatal(err)
	}
	run, err := l.RunReconciliation(ctx, l.now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	d := findingFor(t, run, "payment_unknown", accepted.OperationID)
	payload, err := json.Marshal(adminRepairPayload{SourceRevision: strconv.FormatInt(d.SourceRevision, 10), Evidence: d.Evidence})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C34", d.ID, payload)
	if err != nil {
		t.Fatal(err)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "repair-wait-001", "C34", d.ID, payload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	command, err = l.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "waiting_verification" {
		t.Fatalf("missing provider evidence must keep repair waiting: %+v, %v", command, err)
	}
	var receipts int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil || receipts != 0 {
		t.Fatalf("waiting repair has receipt: %d, %v", receipts, err)
	}
	command, err = l.AdminVerifyExistingObligation(ctx, command.ID)
	if err != nil || command.Status != "waiting_verification" {
		t.Fatalf("retry without evidence must remain waiting: %+v, %v", command, err)
	}
	var key, currency string
	var amount int64
	if err := l.db.QueryRowContext(ctx, `SELECT provider_key,amount_minor,currency FROM payment_operations WHERE id=?`, accepted.OperationID).Scan(&key, &amount, &currency); err != nil {
		t.Fatal(err)
	}
	if _, err := l.provider.Capture(ctx, key, amount, currency); err != nil {
		t.Fatal(err)
	}
	command, err = l.AdminVerifyExistingObligation(ctx, command.ID)
	if err != nil || command.Status != "succeeded" {
		t.Fatalf("original evidence must complete repair: %+v, %v", command, err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil || receipts != 1 {
		t.Fatalf("completed repair receipt count: %d, %v", receipts, err)
	}
	var paymentStatus string
	if err := l.db.QueryRowContext(ctx, `SELECT status FROM payment_operations WHERE id=?`, accepted.OperationID).Scan(&paymentStatus); err != nil || paymentStatus != "succeeded" {
		t.Fatalf("repair receipt without settled local payment: %q, %v", paymentStatus, err)
	}
	var allocations int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM allocations WHERE operation_id=?`, accepted.OperationID).Scan(&allocations); err != nil || allocations != 1 {
		t.Fatalf("repair receipt without exactly one allocation: %d, %v", allocations, err)
	}
}

func TestRepairWaitingLookupBlocksLateProviderAmountMismatch(t *testing.T) {
	ctx := context.Background()
	l, _, _ := openTestLab(t)
	receipt := purchase(t, l)
	if _, err := l.db.ExecContext(ctx, `UPDATE payment_operations SET status='submitted' WHERE id=? AND status='created'`, receipt.OperationID); err != nil {
		t.Fatal(err)
	}
	d := findingFor(t, reconcileNow(t, l), "payment_unknown", receipt.OperationID)
	repair, err := l.RepairDiscrepancy(ctx, d.ID, "late-mismatched-evidence")
	if err != nil || repair.Status != "waiting" {
		t.Fatalf("missing evidence should keep repair waiting: %+v, %v", repair, err)
	}
	waitingRepairID := repair.ID
	var key, currency string
	var amount int64
	if err := l.db.QueryRowContext(ctx, `SELECT provider_key,amount_minor,currency FROM payment_operations WHERE id=?`, receipt.OperationID).Scan(&key, &amount, &currency); err != nil {
		t.Fatal(err)
	}
	if _, err := l.provider.Capture(ctx, key, amount+1, currency); err != nil {
		t.Fatal(err)
	}
	findingFor(t, reconcileNow(t, l), "provider_amount_mismatch", receipt.OperationID)
	repair, err = l.RepairDiscrepancy(ctx, d.ID, "late-mismatched-evidence")
	if err != nil || repair.ID != waitingRepairID || repair.Status != "blocked" || repair.Verification != "provider amount mismatch requires manual investigation" {
		t.Fatalf("late mismatched evidence must block original repair: %+v, %v", repair, err)
	}
	var paymentStatus string
	if err := l.db.QueryRowContext(ctx, `SELECT status FROM payment_operations WHERE id=?`, receipt.OperationID).Scan(&paymentStatus); err != nil || paymentStatus != "submitted" {
		t.Fatalf("mismatched evidence changed payment: %q, %v", paymentStatus, err)
	}
	var allocations, repairCount int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM allocations WHERE operation_id=?`, receipt.OperationID).Scan(&allocations); err != nil || allocations != 0 {
		t.Fatalf("mismatched evidence allocated money: %d, %v", allocations, err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM repair_operations WHERE discrepancy_id=?`, d.ID).Scan(&repairCount); err != nil || repairCount != 1 {
		t.Fatalf("retry created another repair: %d, %v", repairCount, err)
	}
}
