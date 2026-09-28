package lab

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestAdminChangeCorrectionBacklogAdvancesPastConflictedBatch(t *testing.T) {
	l, clock := openChangingLab(t)
	ctx := context.Background()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	subscriptions := make([]string, 101)
	for i := range subscriptions {
		subscriptions[i] = paidBasicSubscription(t, l, fmt.Sprintf("change-backlog-%03d", i)).SubscriptionID
	}
	*clock = time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)
	operations := make([]string, len(subscriptions))
	for i, subID := range subscriptions {
		change, err := l.RequestImmediateProUpgrade(ctx, subID, 5, 1, fmt.Sprintf("change-backlog-upgrade-%03d", i))
		if err != nil {
			t.Fatal(err)
		}
		operations[i] = change.OperationID
		if _, err := l.DispatchCapture(ctx, change.OperationID, "lost_response"); !errors.Is(err, ErrPaymentUnknown) {
			t.Fatalf("capture %d: %v", i, err)
		}
	}
	*clock = time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC)
	for i, operationID := range operations {
		if _, err := l.ReconcilePayment(ctx, operationID); err != nil {
			t.Fatalf("reconcile %d: %v", i, err)
		}
	}
	payload := json.RawMessage(`{}`)
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C13", "", payload)
	if err != nil {
		t.Fatal(err)
	}
	var source map[string]string
	if err := json.Unmarshal(preview.SourceVersions, &source); err != nil {
		t.Fatal(err)
	}
	var first []changeCorrectionPreviewItem
	if err := json.Unmarshal([]byte(source["items_json"]), &first); err != nil || len(first) != 100 {
		t.Fatalf("first batch size=%d err=%v", len(first), err)
	}
	for _, item := range first {
		if _, err := l.db.ExecContext(ctx, `UPDATE immediate_changes SET correction_minor=correction_minor+1 WHERE id=?`, item.ChangeID); err != nil {
			t.Fatal(err)
		}
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "change-backlog-first", "C13", "", payload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.AdminExecuteCommand(ctx, command.ID); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox WHERE kind='change_correction' AND status='pending'`).Scan(&remaining); err != nil || remaining != 101 {
		t.Fatalf("conflicted batch should leave all candidates pending: remaining=%d err=%v", remaining, err)
	}
	next, err := l.AdminCreatePreview(ctx, "local-admin", "C13", "", payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(next.SourceVersions, &source); err != nil {
		t.Fatal(err)
	}
	var later []changeCorrectionPreviewItem
	if err := json.Unmarshal([]byte(source["items_json"]), &later); err != nil || len(later) != 100 {
		t.Fatalf("next batch size=%d err=%v", len(later), err)
	}
	firstIDs := make(map[string]bool, len(first))
	for _, item := range first {
		firstIDs[item.ChangeID] = true
	}
	if firstIDs[later[0].ChangeID] {
		t.Fatalf("next batch started with an unresolved item from the first batch: %s", later[0].ChangeID)
	}
}

func TestAdminChangeCorrectionsProcessesOnlyPreviewedItems(t *testing.T) {
	l, clock := openChangingLab(t)
	ctx := context.Background()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	first := paidBasicSubscription(t, l, "admin-correction-one")
	second := paidBasicSubscription(t, l, "admin-correction-two")
	*clock = time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)
	firstChange, err := l.RequestImmediateProUpgrade(ctx, first.SubscriptionID, 5, 1, "admin-correction-upgrade-one")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.DispatchCapture(ctx, firstChange.OperationID, "lost_response"); !errors.Is(err, ErrPaymentUnknown) {
		t.Fatalf("first capture: %v", err)
	}
	*clock = time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC)
	if _, err := l.ReconcilePayment(ctx, firstChange.OperationID); err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{}`)
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C13", "", payload)
	if err != nil {
		t.Fatal(err)
	}
	var impact map[string]string
	if err := json.Unmarshal(preview.Impact, &impact); err != nil {
		t.Fatal(err)
	}
	if impact["count"] != "1" {
		t.Fatalf("wrong preview: %+v", impact)
	}
	if impact["has_more_candidates"] != "false" {
		t.Fatalf("unexpected change correction backlog: %+v", impact)
	}
	secondChange, err := l.RequestImmediateProUpgrade(ctx, second.SubscriptionID, 5, 1, "admin-correction-upgrade-two")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.DispatchCapture(ctx, secondChange.OperationID, "lost_response"); !errors.Is(err, ErrPaymentUnknown) {
		t.Fatalf("second capture: %v", err)
	}
	*clock = time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC)
	if _, err := l.ReconcilePayment(ctx, secondChange.OperationID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := l.AdminSubmitCommand(ctx, "other-admin", "foreign-c13-001", "C13", "", payload, preview.ID); !errors.Is(err, ErrAdminPreviewStale) {
		t.Fatalf("other actor used C13 preview: %v", err)
	}
	var foreignCommands int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_commands WHERE actor_id='other-admin' AND action_id='C13'`).Scan(&foreignCommands); err != nil {
		t.Fatal(err)
	}
	if foreignCommands != 0 {
		t.Fatalf("other actor wrote %d C13 commands", foreignCommands)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "change-corrections-001", "C13", "", payload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	command, err = l.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "succeeded" {
		t.Fatalf("execute: %+v %v", command, err)
	}
	var firstStatus, secondStatus string
	if err := l.db.QueryRowContext(ctx, `SELECT status FROM outbox WHERE id=?`, "change-correction:"+firstChange.ID).Scan(&firstStatus); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT status FROM outbox WHERE id=?`, "change-correction:"+secondChange.ID).Scan(&secondStatus); err != nil {
		t.Fatal(err)
	}
	var receipt int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipt); err != nil {
		t.Fatal(err)
	}
	if firstStatus != "done" || secondStatus != "pending" || receipt != 1 {
		t.Fatalf("first=%s second=%s receipt=%d", firstStatus, secondStatus, receipt)
	}
	detail, err := l.AdminInvoiceDetail(ctx, firstChange.InvoiceID)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Corrections) != 1 || detail.Corrections[0].OriginKind != "delayed_change" || detail.Corrections[0].OriginID != firstChange.ID {
		t.Fatalf("delayed change origin: %+v", detail.Corrections)
	}
	replayed, replay, err := l.AdminSubmitCommand(ctx, "local-admin", "change-corrections-001", "C13", "", payload, preview.ID)
	if err != nil || !replay || replayed.ID != command.ID {
		t.Fatalf("batch replay: %+v replay=%v err=%v", replayed, replay, err)
	}
	replayed, err = l.AdminExecuteCommand(ctx, replayed.ID)
	if err != nil || replayed.Status != "succeeded" {
		t.Fatalf("execute replay: %+v %v", replayed, err)
	}
	var firstCorrections, secondCorrections int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM corrections WHERE invoice_id=?`, firstChange.InvoiceID).Scan(&firstCorrections); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM corrections WHERE invoice_id=?`, secondChange.InvoiceID).Scan(&secondCorrections); err != nil {
		t.Fatal(err)
	}
	if firstCorrections != 1 || secondCorrections != 0 {
		t.Fatalf("batch replay changed financial membership: first=%d second=%d", firstCorrections, secondCorrections)
	}
}

func TestAdminChangeCorrectionBatchResumesCommittedItemAfterReceiptFailure(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	commercePath := filepath.Join(dir, "commerce.db")
	providerPath := filepath.Join(dir, "provider.db")
	now := fixedNow
	l, err := Open(commercePath, providerPath, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}

	paid := paidBasicSubscription(t, l, "batch-receipt-failure")
	now = time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)
	change, err := l.RequestImmediateProUpgrade(ctx, paid.SubscriptionID, 5, 1, "batch-receipt-failure-upgrade")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.DispatchCapture(ctx, change.OperationID, "lost_response"); !errors.Is(err, ErrPaymentUnknown) {
		t.Fatalf("expected unknown upgrade payment: %v", err)
	}
	now = time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC)
	if _, err := l.ReconcilePayment(ctx, change.OperationID); err != nil {
		t.Fatal(err)
	}
	providerCaptures := captureCount(t, l)
	payload := json.RawMessage(`{}`)
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C13", "", payload)
	if err != nil {
		t.Fatal(err)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "batch-receipt-failure-command", "C13", "", payload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.db.ExecContext(ctx, `CREATE TEMP TRIGGER fail_batch_receipt BEFORE INSERT ON admin_command_receipts BEGIN SELECT RAISE(ABORT, 'injected receipt failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := l.AdminExecuteCommand(ctx, command.ID); err == nil {
		t.Fatal("receipt failure must stop batch completion")
	}
	assertFacts := func(wantReceipt int) {
		t.Helper()
		checks := []struct {
			name  string
			query string
			arg   string
			want  int
		}{
			{"correction", `SELECT COUNT(*) FROM corrections WHERE invoice_id=?`, change.InvoiceID, 1},
			{"credit_grant", `SELECT COUNT(*) FROM credit_grants WHERE source_invoice_id=?`, change.InvoiceID, 1},
			{"job", `SELECT COUNT(*) FROM admin_jobs WHERE command_id=?`, command.ID, 1},
			{"completed_item", `SELECT COUNT(*) FROM admin_job_items WHERE job_id=? AND status='succeeded'`, "job:" + command.ID, 1},
			{"receipt", `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID, wantReceipt},
		}
		for _, check := range checks {
			var got int
			if err := l.db.QueryRowContext(ctx, check.query, check.arg).Scan(&got); err != nil || got != check.want {
				t.Fatalf("%s: got=%d want=%d err=%v", check.name, got, check.want, err)
			}
		}
		if got := captureCount(t, l); got != providerCaptures {
			t.Fatalf("batch invoked provider again: captures=%d want=%d", got, providerCaptures)
		}
	}
	assertFacts(0)
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	l, err = Open(commercePath, providerPath, func() time.Time { return now })
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
	recovered, err := l.AdminCommand(ctx, command.ID)
	if err != nil || recovered.Status != "succeeded" {
		t.Fatalf("batch did not recover: %+v err=%v", recovered, err)
	}
	replayed, replay, err := l.AdminSubmitCommand(ctx, "local-admin", "batch-receipt-failure-command", "C13", "", payload, preview.ID)
	if err != nil || !replay || replayed.ID != command.ID {
		t.Fatalf("replay changed batch command: %+v replay=%v err=%v", replayed, replay, err)
	}
	assertFacts(1)
}
