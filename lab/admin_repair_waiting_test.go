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
}
