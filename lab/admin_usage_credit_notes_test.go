package lab

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestAdminUsageCreditNoteBacklogAdvancesPastConflictedBatch(t *testing.T) {
	l, clock := openChangingLab(t)
	ctx := context.Background()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	paid := paidProSubscription(t, l, "credit-backlog-customer")
	*clock = time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	if _, err := l.RecordUsage(ctx, "worker", "credit-backlog-original", paid.SubscriptionID, "tasks", 20010, time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	*clock = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if _, err := l.CloseUsagePeriod(ctx, paid.SubscriptionID, 0, *clock); err != nil {
		t.Fatal(err)
	}
	renewals, err := l.RunRenewals(ctx)
	if err != nil || len(renewals) != 1 {
		t.Fatalf("renewals: %+v, %v", renewals, err)
	}
	if _, err := l.DispatchNext(ctx, ""); err != nil {
		t.Fatal(err)
	}
	*clock = time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	if _, err := l.RecordUsageAdjustment(ctx, "worker", "credit-backlog-adjustment", paid.SubscriptionID, "worker", "credit-backlog-original", 10); err != nil {
		t.Fatal(err)
	}
	if _, err := l.RerateUsagePeriod(ctx, paid.SubscriptionID, 0); err != nil {
		t.Fatal(err)
	}
	_, err = l.db.ExecContext(ctx, `WITH RECURSIVE seq(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM seq WHERE n<100)
		INSERT INTO usage_periods(subscription_id,period_index,price_version_id,cutoff_at,closed_at,rated_minor,billed_minor,credited_minor,applied_count)
		SELECT p.subscription_id,seq.n,p.price_version_id,p.cutoff_at,p.closed_at,0,1,0,0 FROM usage_periods p,seq
		WHERE p.subscription_id=? AND p.period_index=0`, paid.SubscriptionID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = l.db.ExecContext(ctx, `WITH RECURSIVE seq(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM seq WHERE n<100)
		INSERT INTO usage_invoice_applications(subscription_id,period_index,invoice_id,amount_minor,applied_at)
		SELECT ?,seq.n,?,1,? FROM seq`, paid.SubscriptionID, renewals[0].InvoiceID, clock.UnixNano())
	if err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{}`)
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C30", "", payload)
	if err != nil {
		t.Fatal(err)
	}
	var impact map[string]string
	if err := json.Unmarshal(preview.Impact, &impact); err != nil || impact["has_more_candidates"] != "true" {
		t.Fatalf("first preview backlog=%q err=%v", impact["has_more_candidates"], err)
	}
	var source map[string]string
	if err := json.Unmarshal(preview.SourceVersions, &source); err != nil {
		t.Fatal(err)
	}
	var first []usageCreditNotePreviewPeriod
	if err := json.Unmarshal([]byte(source["periods_json"]), &first); err != nil || len(first) != 100 {
		t.Fatalf("first batch size=%d err=%v", len(first), err)
	}
	if _, err := l.db.ExecContext(ctx, `UPDATE usage_invoice_applications SET amount_minor=amount_minor+1 WHERE subscription_id=? AND period_index<100`, paid.SubscriptionID); err != nil {
		t.Fatal(err)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "credit-backlog-first", "C30", "", payload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.AdminExecuteCommand(ctx, command.ID); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_periods WHERE subscription_id=? AND billed_minor-credited_minor>rated_minor`, paid.SubscriptionID).Scan(&remaining); err != nil || remaining != 101 {
		t.Fatalf("conflicted batch should leave all candidates pending: remaining=%d err=%v", remaining, err)
	}
	next, err := l.AdminCreatePreview(ctx, "local-admin", "C30", "", payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(next.SourceVersions, &source); err != nil {
		t.Fatal(err)
	}
	var later []usageCreditNotePreviewPeriod
	if err := json.Unmarshal([]byte(source["periods_json"]), &later); err != nil || len(later) != 100 {
		t.Fatalf("next batch size=%d err=%v", len(later), err)
	}
	if later[0].PeriodIndex != 100 || later[0].SubscriptionID != paid.SubscriptionID {
		t.Fatalf("next batch did not reach the 101st period: %+v", later[0])
	}
}

func TestAdminUsageCreditNotesFixedBatchAndReceipt(t *testing.T) {
	l, clock := openChangingLab(t)
	ctx := context.Background()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	paid := paidProSubscription(t, l, "admin-credit-note-customer")
	*clock = time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	if _, err := l.RecordUsage(ctx, "worker", "credit-note-original", paid.SubscriptionID, "tasks", 20010, time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	*clock = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if _, err := l.CloseUsagePeriod(ctx, paid.SubscriptionID, 0, *clock); err != nil {
		t.Fatal(err)
	}
	renewals, err := l.RunRenewals(ctx)
	if err != nil || len(renewals) != 1 || renewals[0].AmountMinor != 10001 {
		t.Fatalf("renewals: %+v, %v", renewals, err)
	}
	if _, err := l.DispatchNext(ctx, ""); err != nil {
		t.Fatal(err)
	}
	*clock = time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	if _, err := l.RecordUsageAdjustment(ctx, "worker", "credit-note-adjustment", paid.SubscriptionID, "worker", "credit-note-original", 10); err != nil {
		t.Fatal(err)
	}
	if rating, err := l.RerateUsagePeriod(ctx, paid.SubscriptionID, 0); err != nil || rating.DeltaMinor != -1 {
		t.Fatalf("rerate: %+v, %v", rating, err)
	}

	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C30", "", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	var impact map[string]string
	if err := json.Unmarshal(preview.Impact, &impact); err != nil {
		t.Fatal(err)
	}
	if impact["total_reduction_minor"] != "1" {
		t.Fatalf("unexpected preview: %+v", preview.Impact)
	}
	if impact["has_more_candidates"] != "false" {
		t.Fatalf("unexpected usage credit note backlog: %+v", impact)
	}
	// A newly discovered candidate must stay outside the confirmed snapshot.
	if _, err := l.db.ExecContext(ctx, `INSERT INTO usage_periods(subscription_id,period_index,price_version_id,cutoff_at,closed_at,rated_minor,billed_minor,credited_minor,applied_count) SELECT subscription_id,1,price_version_id,cutoff_at,closed_at,0,1,0,0 FROM usage_periods WHERE subscription_id=? AND period_index=0`, paid.SubscriptionID); err != nil {
		t.Fatal(err)
	}
	if _, err := l.db.ExecContext(ctx, `INSERT INTO usage_invoice_applications(subscription_id,period_index,invoice_id,amount_minor,applied_at) VALUES(?,1,?,1,?)`, paid.SubscriptionID, renewals[0].InvoiceID, clock.Unix()); err != nil {
		t.Fatal(err)
	}
	command, replay, err := l.AdminSubmitCommand(ctx, "local-admin", "usage-credit-note-batch-001", "C30", "", json.RawMessage(`{}`), preview.ID)
	if err != nil || replay {
		t.Fatalf("submit: %+v replay=%v err=%v", command, replay, err)
	}
	command, err = l.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "succeeded" {
		t.Fatalf("execute: %+v, %v", command, err)
	}
	var notes, receipts int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_credit_notes WHERE subscription_id=? AND period_index=0 AND source_invoice_id=?`, paid.SubscriptionID, renewals[0].InvoiceID).Scan(&notes); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if notes != 1 || receipts != 1 {
		t.Fatalf("notes=%d receipts=%d", notes, receipts)
	}
	if _, replay, err := l.AdminSubmitCommand(ctx, "local-admin", "usage-credit-note-batch-001", "C30", "", json.RawMessage(`{}`), preview.ID); err != nil || !replay {
		t.Fatalf("replay=%v err=%v", replay, err)
	}
	var newPeriodNotes int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_credit_notes WHERE subscription_id=? AND period_index=1`, paid.SubscriptionID).Scan(&newPeriodNotes); err != nil {
		t.Fatal(err)
	}
	if newPeriodNotes != 0 {
		t.Fatalf("new period entered the confirmed batch: %d", newPeriodNotes)
	}
	nextPreview, err := l.AdminCreatePreview(ctx, "local-admin", "C30", "", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(nextPreview.Impact, &impact); err != nil || impact["period_count"] != "1" {
		t.Fatalf("next batch: %+v, %v", impact, err)
	}
}
