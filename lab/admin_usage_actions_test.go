package lab

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestAdminUsageAdjustmentRejectsPreviewAfterAnotherCorrection(t *testing.T) {
	l, clock := openChangingLab(t)
	ctx := context.Background()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	paid := paidProSubscription(t, l, "usage-adjustment-race")
	*clock = time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	if _, err := l.RecordUsage(ctx, "worker", "original-race", paid.SubscriptionID, "tasks", 10, time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(adminUsageAdjustmentPayload{
		Source: "admin", EventID: "stale-reverse", SubscriptionID: paid.SubscriptionID,
		OriginalSource: "worker", OriginalEventID: "original-race", ReverseQuantity: "7",
	})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C27", "", payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.RecordUsageAdjustment(ctx, "worker", "concurrent-reverse", paid.SubscriptionID, "worker", "original-race", 4); err != nil {
		t.Fatal(err)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "stale-adjustment", "C27", "", payload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	command, err = l.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "failed" || command.ErrorCode != "PREVIEW_STALE" {
		t.Fatalf("changed correction budget: %+v err=%v", command, err)
	}
	var staleEvents, receipts int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_events WHERE source='admin' AND event_id='stale-reverse'`).Scan(&staleEvents); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if staleEvents != 0 || receipts != 0 {
		t.Fatalf("stale events=%d receipts=%d", staleEvents, receipts)
	}

	freshPayload, err := json.Marshal(adminUsageAdjustmentPayload{
		Source: "admin", EventID: "fresh-reverse", SubscriptionID: paid.SubscriptionID,
		OriginalSource: "worker", OriginalEventID: "original-race", ReverseQuantity: "6",
	})
	if err != nil {
		t.Fatal(err)
	}
	freshPreview, err := l.AdminCreatePreview(ctx, "local-admin", "C27", "", freshPayload)
	if err != nil {
		t.Fatal(err)
	}
	fresh, _, err := l.AdminSubmitCommand(ctx, "local-admin", "fresh-adjustment", "C27", "", freshPayload, freshPreview.ID)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err = l.AdminExecuteCommand(ctx, fresh.ID)
	if err != nil || fresh.Status != "succeeded" {
		t.Fatalf("fresh correction: %+v err=%v", fresh, err)
	}
	var total int64
	if err := l.db.QueryRowContext(ctx, `SELECT SUM(quantity) FROM usage_events WHERE subscription_id=? AND (source='worker' OR source='admin')`, paid.SubscriptionID).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if total != 0 {
		t.Fatalf("corrections exceeded original quantity: total=%d", total)
	}
}

func TestAdminUsageCloseRejectsPreviewAfterNewEligibleEvent(t *testing.T) {
	l, clock := openChangingLab(t)
	ctx := context.Background()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	paid := paidProSubscription(t, l, "usage-close-race")
	*clock = time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	if _, err := l.RecordUsage(ctx, "worker", "close-original", paid.SubscriptionID, "tasks", 20000, time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	*clock = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	payload := json.RawMessage(`{"period_index":"0","cutoff":"2026-10-01T00:00:00Z"}`)
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C28", paid.SubscriptionID, payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.RecordUsage(ctx, "worker", "close-concurrent", paid.SubscriptionID, "tasks", 10, time.Date(2026, 9, 30, 23, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "stale-close", "C28", paid.SubscriptionID, payload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	command, err = l.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "failed" || command.ErrorCode != "PREVIEW_STALE" {
		t.Fatalf("changed close source: %+v err=%v", command, err)
	}
	var ratings, receipts int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_ratings WHERE subscription_id=? AND period_index=0`, paid.SubscriptionID).Scan(&ratings); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if ratings != 0 || receipts != 0 {
		t.Fatalf("stale close ratings=%d receipts=%d", ratings, receipts)
	}
	freshPreview, err := l.AdminCreatePreview(ctx, "local-admin", "C28", paid.SubscriptionID, payload)
	if err != nil {
		t.Fatal(err)
	}
	fresh, _, err := l.AdminSubmitCommand(ctx, "local-admin", "fresh-close", "C28", paid.SubscriptionID, payload, freshPreview.ID)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err = l.AdminExecuteCommand(ctx, fresh.ID)
	if err != nil || fresh.Status != "succeeded" {
		t.Fatalf("fresh close: %+v err=%v", fresh, err)
	}
	var quantity, rounded int64
	if err := l.db.QueryRowContext(ctx, `SELECT quantity,rounded_minor FROM usage_ratings WHERE subscription_id=? AND period_index=0 AND revision=1`, paid.SubscriptionID).Scan(&quantity, &rounded); err != nil {
		t.Fatal(err)
	}
	if quantity != 20010 || rounded != 1 {
		t.Fatalf("fresh rating quantity=%d rounded=%d", quantity, rounded)
	}
}

func TestAdminUsageRerateRejectsPreviewAfterNewLateEvent(t *testing.T) {
	l, clock := openChangingLab(t)
	ctx := context.Background()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	paid := paidProSubscription(t, l, "usage-rerate-race")
	*clock = time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	periodEventAt := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	if _, err := l.RecordUsage(ctx, "worker", "rerate-original", paid.SubscriptionID, "tasks", 20000, periodEventAt); err != nil {
		t.Fatal(err)
	}
	*clock = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if _, err := l.CloseUsagePeriod(ctx, paid.SubscriptionID, 0, *clock); err != nil {
		t.Fatal(err)
	}
	*clock = time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	lateAt := time.Date(2026, 9, 30, 23, 0, 0, 0, time.UTC)
	if _, err := l.RecordUsage(ctx, "worker", "rerate-first-late", paid.SubscriptionID, "tasks", 7, lateAt); err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{"period_index":"0"}`)
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C29", paid.SubscriptionID, payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.RecordUsage(ctx, "worker", "rerate-second-late", paid.SubscriptionID, "tasks", 3, lateAt); err != nil {
		t.Fatal(err)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "stale-rerate", "C29", paid.SubscriptionID, payload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	command, err = l.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "failed" || command.ErrorCode != "PREVIEW_STALE" {
		t.Fatalf("changed rerate source: %+v err=%v", command, err)
	}
	var ratings, receipts int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_ratings WHERE subscription_id=? AND period_index=0`, paid.SubscriptionID).Scan(&ratings); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if ratings != 1 || receipts != 0 {
		t.Fatalf("stale rerate ratings=%d receipts=%d", ratings, receipts)
	}
	freshPreview, err := l.AdminCreatePreview(ctx, "local-admin", "C29", paid.SubscriptionID, payload)
	if err != nil {
		t.Fatal(err)
	}
	fresh, _, err := l.AdminSubmitCommand(ctx, "local-admin", "fresh-rerate", "C29", paid.SubscriptionID, payload, freshPreview.ID)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err = l.AdminExecuteCommand(ctx, fresh.ID)
	if err != nil || fresh.Status != "succeeded" {
		t.Fatalf("fresh rerate: %+v err=%v", fresh, err)
	}
	var quantity, rounded int64
	if err := l.db.QueryRowContext(ctx, `SELECT quantity,rounded_minor FROM usage_ratings WHERE subscription_id=? AND period_index=0 AND revision=2`, paid.SubscriptionID).Scan(&quantity, &rounded); err != nil {
		t.Fatal(err)
	}
	if quantity != 20010 || rounded != 1 {
		t.Fatalf("fresh rerating quantity=%d rounded=%d", quantity, rounded)
	}
}

func TestAdminUsageCloseRerateAndAdjustment(t *testing.T) {
	l, clock := openChangingLab(t)
	ctx := context.Background()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	paid := paidProSubscription(t, l, "admin-usage-customer")
	*clock = time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	eventAt := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	if _, err := l.RecordUsage(ctx, "worker", "admin-original", paid.SubscriptionID, "tasks", 20010, eventAt); err != nil {
		t.Fatal(err)
	}
	*clock = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	closePayload := json.RawMessage(`{"period_index":"0","cutoff":"2026-10-01T00:00:00Z"}`)
	closePreview, err := l.AdminCreatePreview(ctx, "local-admin", "C28", paid.SubscriptionID, closePayload)
	if err != nil {
		t.Fatal(err)
	}
	closeCommand, _, err := l.AdminSubmitCommand(ctx, "local-admin", "admin-close-usage-001", "C28", paid.SubscriptionID, closePayload, closePreview.ID)
	if err != nil {
		t.Fatal(err)
	}
	closeCommand, err = l.AdminExecuteCommand(ctx, closeCommand.ID)
	if err != nil || closeCommand.Status != "succeeded" {
		t.Fatalf("close: %+v %v", closeCommand, err)
	}
	*clock = time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	lateAt := time.Date(2026, 9, 30, 23, 0, 0, 0, time.UTC)
	if _, err := l.RecordUsage(ctx, "worker", "admin-late", paid.SubscriptionID, "tasks", 7, lateAt); err != nil {
		t.Fatal(err)
	}
	reratePayload := json.RawMessage(`{"period_index":"0"}`)
	reratePreview, err := l.AdminCreatePreview(ctx, "local-admin", "C29", paid.SubscriptionID, reratePayload)
	if err != nil {
		t.Fatal(err)
	}
	rerateCommand, _, err := l.AdminSubmitCommand(ctx, "local-admin", "admin-rerate-usage-001", "C29", paid.SubscriptionID, reratePayload, reratePreview.ID)
	if err != nil {
		t.Fatal(err)
	}
	rerateCommand, err = l.AdminExecuteCommand(ctx, rerateCommand.ID)
	if err != nil || rerateCommand.Status != "succeeded" {
		t.Fatalf("rerate: %+v %v", rerateCommand, err)
	}
	var rerateRefs map[string]string
	if err := json.Unmarshal(rerateCommand.ResultRefs, &rerateRefs); err != nil {
		t.Fatal(err)
	}
	if rerateRefs["revision"] != "2" {
		t.Fatalf("rerate refs: %+v", rerateRefs)
	}
	adjustPayload, _ := json.Marshal(adminUsageAdjustmentPayload{Source: "worker", EventID: "admin-adjust-late", SubscriptionID: paid.SubscriptionID, OriginalSource: "worker", OriginalEventID: "admin-late", ReverseQuantity: "7"})
	adjustPreview, err := l.AdminCreatePreview(ctx, "local-admin", "C27", "", adjustPayload)
	if err != nil {
		t.Fatal(err)
	}
	adjustCommand, _, err := l.AdminSubmitCommand(ctx, "local-admin", "admin-adjust-usage-001", "C27", "", adjustPayload, adjustPreview.ID)
	if err != nil {
		t.Fatal(err)
	}
	adjustCommand, err = l.AdminExecuteCommand(ctx, adjustCommand.ID)
	if err != nil || adjustCommand.Status != "succeeded" {
		t.Fatalf("adjust: %+v %v", adjustCommand, err)
	}
	var reversed int64
	if err := l.db.QueryRowContext(ctx, `SELECT quantity FROM usage_events WHERE source='worker' AND event_id='admin-adjust-late'`).Scan(&reversed); err != nil {
		t.Fatal(err)
	}
	var receipts int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id IN (?,?,?)`, closeCommand.ID, rerateCommand.ID, adjustCommand.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if reversed != -7 || receipts != 3 {
		t.Fatalf("reversed=%d receipts=%d", reversed, receipts)
	}
}
