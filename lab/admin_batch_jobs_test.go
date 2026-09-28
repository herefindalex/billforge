package lab

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestAdminContractCollectionBatchResumesOnlyUncommittedItems(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	commerce, provider := filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db")
	now := fixedNow
	open := func() *Lab {
		l, err := Open(commerce, provider, func() time.Time { return now })
		if err != nil {
			t.Fatal(err)
		}
		if err := l.InitAdmin(ctx); err != nil {
			t.Fatal(err)
		}
		return l
	}
	l := open()
	spec := acmeContract("")
	if _, err := l.PublishContract(ctx, spec); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		quote, err := l.CreateContractQuote(ctx, spec.CustomerID, spec.ID, 5)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := l.AcceptContractQuote(ctx, quote.ID, quote.Fingerprint, "contract-resume-accept-"+[]string{"a", "b"}[i]); err != nil {
			t.Fatal(err)
		}
	}
	now = time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	payload := json.RawMessage(`{}`)
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C32", "", payload)
	if err != nil {
		t.Fatal(err)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "contract-resume-batch", "C32", "", payload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.adminFreezeBatch(ctx, command.ID); err != nil {
		t.Fatal(err)
	}
	targets, previewAt, err := adminBatchTargets("C32", string(preview.SourceVersions))
	if err != nil || len(targets) != 2 {
		t.Fatalf("frozen collection targets=%d err=%v", len(targets), err)
	}
	var itemID string
	if err := l.db.QueryRowContext(ctx, `SELECT id FROM admin_job_items WHERE job_id=? AND target_id=?`, "job:"+command.ID, targets[0].ID).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	if err := l.adminRunBatchItem(ctx, command.ID, "C32", previewAt, itemID, targets[0]); err != nil {
		t.Fatal(err)
	}
	var before int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox WHERE id=?`, "capture:"+targets[0].ID).Scan(&before); err != nil || before != 1 {
		t.Fatalf("committed capture outbox=%d err=%v", before, err)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	l = open()
	defer l.Close()
	if err := l.AdminResumeAccepted(ctx); err != nil {
		t.Fatal(err)
	}
	command, err = l.AdminCommand(ctx, command.ID)
	if err != nil || command.Status != "succeeded" {
		t.Fatalf("resumed command %+v err=%v", command, err)
	}
	var total, receipts int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox WHERE id LIKE 'capture:%'`).Scan(&total); err != nil || total != 2 {
		t.Fatalf("capture outbox total=%d err=%v", total, err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil || receipts != 1 {
		t.Fatalf("command receipts=%d err=%v", receipts, err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox WHERE id=?`, "capture:"+targets[0].ID).Scan(&before); err != nil || before != 1 {
		t.Fatalf("committed capture duplicated after restart: count=%d err=%v", before, err)
	}
}

func TestAdminRenewalBatchResumesOnlyUncommittedItems(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	commerce, provider := filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db")
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	open := func() *Lab {
		l, err := Open(commerce, provider, func() time.Time { return now })
		if err != nil {
			t.Fatal(err)
		}
		if err := l.InitAdmin(ctx); err != nil {
			t.Fatal(err)
		}
		return l
	}
	l := open()
	first := paidProSubscription(t, l, "renewal-resume-first")
	second := paidProSubscription(t, l, "renewal-resume-second")
	now = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	payload := json.RawMessage(`{}`)
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C44", "", payload)
	if err != nil {
		t.Fatal(err)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "renewal-resume-batch", "C44", "", payload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.adminFreezeBatch(ctx, command.ID); err != nil {
		t.Fatal(err)
	}
	targets, previewAt, err := adminBatchTargets("C44", string(preview.SourceVersions))
	if err != nil || len(targets) != 2 {
		t.Fatalf("frozen renewal targets=%d err=%v", len(targets), err)
	}
	var itemID string
	if err := l.db.QueryRowContext(ctx, `SELECT id FROM admin_job_items WHERE job_id=? AND target_id=?`, "job:"+command.ID, targets[0].ID).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	if err := l.adminRunBatchItem(ctx, command.ID, "C44", previewAt, itemID, targets[0]); err != nil {
		t.Fatal(err)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	l = open()
	defer l.Close()
	if err := l.AdminResumeAccepted(ctx); err != nil {
		t.Fatal(err)
	}
	command, err = l.AdminCommand(ctx, command.ID)
	if err != nil || command.Status != "succeeded" {
		t.Fatalf("resumed renewal command %+v err=%v", command, err)
	}
	for _, subID := range []string{first.SubscriptionID, second.SubscriptionID} {
		var periods, operations int
		if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM billing_periods WHERE subscription_id=? AND period_index=1`, subID).Scan(&periods); err != nil || periods != 1 {
			t.Fatalf("renewal periods for %s=%d err=%v", subID, periods, err)
		}
		if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM payment_operations o JOIN billing_periods p ON p.invoice_id=o.invoice_id WHERE p.subscription_id=? AND p.period_index=1`, subID).Scan(&operations); err != nil || operations != 1 {
			t.Fatalf("renewal payment obligations for %s=%d err=%v", subID, operations, err)
		}
	}
}

func TestAdminUsageCreditNoteBatchResumesOnlyUncommittedItems(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	commerce, provider := filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db")
	now := fixedNow
	open := func() *Lab {
		l, err := Open(commerce, provider, func() time.Time { return now })
		if err != nil {
			t.Fatal(err)
		}
		if err := l.InitAdmin(ctx); err != nil {
			t.Fatal(err)
		}
		return l
	}
	l := open()
	first := paidProSubscription(t, l, "credit-resume-first")
	second := paidProSubscription(t, l, "credit-resume-second")
	subs := []struct {
		id    string
		event string
	}{
		{first.SubscriptionID, "credit-resume-event-first"},
		{second.SubscriptionID, "credit-resume-event-second"},
	}
	now = time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	for _, sub := range subs {
		if _, err := l.RecordUsage(ctx, "worker", sub.event, sub.id, "tasks", 20010, time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
	}
	now = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for _, sub := range subs {
		if _, err := l.CloseUsagePeriod(ctx, sub.id, 0, now); err != nil {
			t.Fatal(err)
		}
	}
	renewals, err := l.RunRenewals(ctx)
	if err != nil || len(renewals) != 2 {
		t.Fatalf("renewals=%d err=%v", len(renewals), err)
	}
	for range renewals {
		if _, err := l.DispatchNext(ctx, ""); err != nil {
			t.Fatal(err)
		}
	}
	now = time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	for _, sub := range subs {
		if _, err := l.RecordUsageAdjustment(ctx, "worker", sub.event+"-adjust", sub.id, "worker", sub.event, 10); err != nil {
			t.Fatal(err)
		}
		if _, err := l.RerateUsagePeriod(ctx, sub.id, 0); err != nil {
			t.Fatal(err)
		}
	}
	payload := json.RawMessage(`{}`)
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C30", "", payload)
	if err != nil {
		t.Fatal(err)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "credit-resume-batch", "C30", "", payload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.adminFreezeBatch(ctx, command.ID); err != nil {
		t.Fatal(err)
	}
	targets, previewAt, err := adminBatchTargets("C30", string(preview.SourceVersions))
	if err != nil || len(targets) != 2 {
		t.Fatalf("frozen credit targets=%d err=%v", len(targets), err)
	}
	var itemID string
	if err := l.db.QueryRowContext(ctx, `SELECT id FROM admin_job_items WHERE job_id=? AND target_id=?`, "job:"+command.ID, targets[0].ID).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	if err := l.adminRunBatchItem(ctx, command.ID, "C30", previewAt, itemID, targets[0]); err != nil {
		t.Fatal(err)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	l = open()
	defer l.Close()
	if err := l.AdminResumeAccepted(ctx); err != nil {
		t.Fatal(err)
	}
	command, err = l.AdminCommand(ctx, command.ID)
	if err != nil || command.Status != "succeeded" {
		t.Fatalf("resumed credit command %+v err=%v", command, err)
	}
	for _, sub := range subs {
		var notes, credited int
		if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_credit_notes WHERE subscription_id=? AND period_index=0`, sub.id).Scan(&notes); err != nil || notes != 1 {
			t.Fatalf("credit notes for %s=%d err=%v", sub.id, notes, err)
		}
		if err := l.db.QueryRowContext(ctx, `SELECT credited_minor FROM usage_periods WHERE subscription_id=? AND period_index=0`, sub.id).Scan(&credited); err != nil || credited != 1 {
			t.Fatalf("credited amount for %s=%d err=%v", sub.id, credited, err)
		}
	}
}

func TestAdminBatchReceiptSeparatesTerminalItemStatuses(t *testing.T) {
	l, _ := openChangingLab(t)
	ctx := context.Background()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"success", "failed", "conflicted", "waiting", "skipped"} {
		paidProSubscription(t, l, "batch-summary-"+name)
	}
	payload := json.RawMessage(`{}`)
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C45", "", payload)
	if err != nil {
		t.Fatal(err)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "batch-summary-statuses", "C45", "", payload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.adminFreezeBatch(ctx, command.ID); err != nil {
		t.Fatal(err)
	}
	rows, err := l.db.QueryContext(ctx, `SELECT id FROM admin_job_items WHERE job_id=? ORDER BY rowid`, "job:"+command.ID)
	if err != nil {
		t.Fatal(err)
	}
	var itemIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		itemIDs = append(itemIDs, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		t.Fatal(err)
	}
	rows.Close()
	statuses := []string{"succeeded", "failed", "conflicted", "waiting_verification", "skipped"}
	if len(itemIDs) != len(statuses) {
		t.Fatalf("frozen items=%d", len(itemIDs))
	}
	for i, id := range itemIDs {
		if _, err := l.db.ExecContext(ctx, `UPDATE admin_job_items SET status=? WHERE id=?`, statuses[i], id); err != nil {
			t.Fatal(err)
		}
	}
	finished, err := l.adminFinishBatch(ctx, command.ID, "C45", "local-admin")
	if err != nil {
		t.Fatal(err)
	}
	var refs map[string]json.RawMessage
	if err := json.Unmarshal(finished.ResultRefs, &refs); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"succeeded_count", "failed_count", "conflicted_count", "waiting_verification_count", "skipped_count"} {
		if string(refs[key]) != `"1"` {
			t.Fatalf("%s=%s, refs=%s", key, refs[key], finished.ResultRefs)
		}
	}
	if string(refs["status"]) != `"partial"` {
		t.Fatalf("job summary should be partial: %s", finished.ResultRefs)
	}
}

func TestAdminBatchResumesCommittedItemsAndMarksChangedSources(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	commerce, provider := filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db")
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	open := func() *Lab {
		l, err := Open(commerce, provider, func() time.Time { return now })
		if err != nil {
			t.Fatal(err)
		}
		if err := l.InitAdmin(ctx); err != nil {
			t.Fatal(err)
		}
		return l
	}
	l := open()
	first := paidProSubscription(t, l, "batch-resume-first")
	second := paidProSubscription(t, l, "batch-resume-second")
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C45", "", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "batch-resume-001", "C45", "", json.RawMessage(`{}`), preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.adminFreezeBatch(ctx, command.ID); err != nil {
		t.Fatal(err)
	}
	var sourceJSON string
	if err := l.db.QueryRowContext(ctx, `SELECT source_versions_json FROM admin_previews WHERE id=?`, preview.ID).Scan(&sourceJSON); err != nil {
		t.Fatal(err)
	}
	targets, _, err := adminBatchTargets("C45", sourceJSON)
	if err != nil {
		t.Fatal(err)
	}
	var completedID string
	for _, target := range targets {
		if target.ID != first.SubscriptionID {
			continue
		}
		if err := l.db.QueryRowContext(ctx, `SELECT id FROM admin_job_items WHERE job_id=? AND target_id=?`, "job:"+command.ID, target.ID).Scan(&completedID); err != nil {
			t.Fatal(err)
		}
		if err := l.adminRunBatchItem(ctx, command.ID, "C45", now, completedID, target); err != nil {
			t.Fatal(err)
		}
	}
	if completedID == "" {
		t.Fatal("first subscription missing from frozen batch")
	}
	if _, err := l.db.ExecContext(ctx, `UPDATE subscriptions SET revision=revision+1 WHERE id=?`, second.SubscriptionID); err != nil {
		t.Fatal(err)
	}
	var before string
	if err := l.db.QueryRowContext(ctx, `SELECT result_refs_json FROM admin_job_items WHERE id=?`, completedID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	l = open()
	defer l.Close()
	if err := l.AdminResumeAccepted(ctx); err != nil {
		t.Fatal(err)
	}
	command, err = l.AdminCommand(ctx, command.ID)
	if err != nil || command.Status != "succeeded" {
		t.Fatalf("resumed command %+v %v", command, err)
	}
	var status, after string
	if err := l.db.QueryRowContext(ctx, `SELECT status,result_refs_json FROM admin_job_items WHERE id=?`, completedID).Scan(&status, &after); err != nil || status != "succeeded" || after != before {
		t.Fatalf("committed item changed: %q %q %v", status, after, err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT status FROM admin_job_items WHERE job_id=? AND target_id=?`, "job:"+command.ID, second.SubscriptionID).Scan(&status); err != nil || status != "conflicted" {
		t.Fatalf("changed item status %q %v", status, err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT status FROM admin_jobs WHERE command_id=?`, command.ID).Scan(&status); err != nil || status != "partial" {
		t.Fatalf("job status %q %v", status, err)
	}
	job, err := l.AdminJob(ctx, "job:"+command.ID)
	if err != nil || job.Status != "partial" || len(job.Items) != 2 {
		t.Fatalf("job detail %+v %v", job, err)
	}
	var receipts int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil || receipts != 1 {
		t.Fatalf("command receipts %d %v", receipts, err)
	}
}

func TestAdminBatchKeepsAcceptedBusinessTimeAfterClockChange(t *testing.T) {
	ctx := context.Background()
	l, clock := openChangingLab(t)
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	paid := paidProSubscription(t, l, "batch-business-time")
	*clock = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C44", "", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "batch-clock-001", "C44", "", json.RawMessage(`{}`), preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.adminFreezeBatch(ctx, command.ID); err != nil {
		t.Fatal(err)
	}
	submitControl(t, l, "batch-clock-change-001", "C46", "", json.RawMessage(`{"mode":"fixed","value_utc":"2027-01-01T00:00:00Z"}`))
	command, err = l.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "succeeded" {
		t.Fatalf("batch after clock change %+v %v", command, err)
	}
	var periods int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM billing_periods WHERE subscription_id=?`, paid.SubscriptionID).Scan(&periods); err != nil || periods != 2 {
		t.Fatalf("renewal periods %d %v", periods, err)
	}
}
