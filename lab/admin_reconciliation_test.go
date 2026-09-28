package lab

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"
)

func TestAdminReconciliationRunHasAtomicReceipt(t *testing.T) {
	l, _ := openChangingLab(t)
	ctx := context.Background()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(AdminReconciliationPayload{AsOf: fixedNow.UTC().Format(time.RFC3339Nano)})
	if err != nil {
		t.Fatal(err)
	}
	command, replay, err := l.AdminSubmitCommand(ctx, "local-admin", "run-reconciliation-001", "C33", "", payload, "")
	if err != nil || replay {
		t.Fatalf("submit: %+v replay=%v err=%v", command, replay, err)
	}
	command, err = l.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "succeeded" {
		t.Fatalf("execute: %+v err=%v", command, err)
	}
	var runs, receipts int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM reconciliation_runs`).Scan(&runs); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if runs != 1 || receipts != 1 {
		t.Fatalf("runs=%d receipts=%d", runs, receipts)
	}
	if _, replay, err := l.AdminSubmitCommand(ctx, "local-admin", "run-reconciliation-001", "C33", "", payload, ""); err != nil || !replay {
		t.Fatalf("replay=%v err=%v", replay, err)
	}
}

func TestAdminRepairDiscrepancyResumesByStableRequestKey(t *testing.T) {
	l, _, _ := openTestLab(t)
	ctx := context.Background()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	purchased := purchase(t, l)
	if _, err := l.DispatchNext(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if err := l.RebuildEntitlements(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := l.db.ExecContext(ctx, `DELETE FROM entitlements WHERE subscription_id=?`, purchased.SubscriptionID); err != nil {
		t.Fatal(err)
	}
	d := findingFor(t, reconcileNow(t, l), "entitlement_projection", purchased.SubscriptionID)
	payload, err := json.Marshal(adminRepairPayload{SourceRevision: strconv.FormatInt(d.SourceRevision, 10), Evidence: d.Evidence})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C34", d.ID, payload)
	if err != nil {
		t.Fatal(err)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "repair-discrepancy-001", "C34", d.ID, payload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	command, err = l.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "succeeded" {
		t.Fatalf("execute: %+v err=%v", command, err)
	}
	var operations, receipts int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM repair_operations WHERE request_key=?`, "admin:"+command.ID).Scan(&operations); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if operations != 1 || receipts != 1 {
		t.Fatalf("operations=%d receipts=%d", operations, receipts)
	}
	if again, err := l.AdminExecuteCommand(ctx, command.ID); err != nil || again.Status != "succeeded" {
		t.Fatalf("resume: %+v err=%v", again, err)
	}
}

func TestAdminManualDecisionStoresReasonAndReceipt(t *testing.T) {
	l, _, _ := openTestLab(t)
	ctx := context.Background()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := l.provider.db.ExecContext(ctx, `INSERT INTO captures(provider_key,amount_minor,currency,status) VALUES('capture:admin-foreign',100,'USD','succeeded')`); err != nil {
		t.Fatal(err)
	}
	d := findingFor(t, reconcileNow(t, l), "unknown_provider_capture", "")
	payload := json.RawMessage(`{"decision":"investigate_provider","reason":"Provider capture has no matching local operation"}`)
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C35", d.ID, payload)
	if err != nil {
		t.Fatal(err)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "manual-decision-001", "C35", d.ID, payload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	command, err = l.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "succeeded" {
		t.Fatalf("execute: %+v err=%v", command, err)
	}
	var reviewer, reason, status string
	if err := l.db.QueryRowContext(ctx, `SELECT reviewer,reason FROM manual_decisions WHERE discrepancy_id=?`, d.ID).Scan(&reviewer, &reason); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT status FROM discrepancies WHERE id=?`, d.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	var receipts int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if reviewer != "local-admin" || reason != "Provider capture has no matching local operation" || status != "investigating" || receipts != 1 {
		t.Fatalf("reviewer=%q reason=%q status=%q receipts=%d", reviewer, reason, status, receipts)
	}
}

func TestAdminManualDecisionPreviewRejectsChangedDiscrepancy(t *testing.T) {
	l, _, _ := openTestLab(t)
	ctx := context.Background()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := l.provider.db.ExecContext(ctx, `INSERT INTO captures(provider_key,amount_minor,currency,status) VALUES('capture:manual-race',100,'USD','succeeded')`); err != nil {
		t.Fatal(err)
	}
	d := findingFor(t, reconcileNow(t, l), "unknown_provider_capture", "")
	payload := json.RawMessage(`{"decision":"investigate_provider","reason":"Review unmatched capture"}`)
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C35", d.ID, payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.RecordManualDecision(ctx, d.ID, "other-reviewer", "investigate_provider"); err != nil {
		t.Fatal(err)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "stale-manual-decision", "C35", d.ID, payload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	command, err = l.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "failed" || command.ErrorCode != "PREVIEW_STALE" {
		t.Fatalf("changed discrepancy command=%+v err=%v", command, err)
	}
	var decisions, receipts int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM manual_decisions WHERE discrepancy_id=?`, d.ID).Scan(&decisions); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if decisions != 1 || receipts != 0 {
		t.Fatalf("stale decision effects=%d receipts=%d", decisions, receipts)
	}
	freshPreview, err := l.AdminCreatePreview(ctx, "local-admin", "C35", d.ID, payload)
	if err != nil {
		t.Fatal(err)
	}
	fresh, _, err := l.AdminSubmitCommand(ctx, "local-admin", "fresh-manual-decision", "C35", d.ID, payload, freshPreview.ID)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err = l.AdminExecuteCommand(ctx, fresh.ID)
	if err != nil || fresh.Status != "succeeded" {
		t.Fatalf("fresh decision=%+v err=%v", fresh, err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM manual_decisions WHERE discrepancy_id=?`, d.ID).Scan(&decisions); err != nil {
		t.Fatal(err)
	}
	if decisions != 2 {
		t.Fatalf("fresh decision count=%d", decisions)
	}
}
