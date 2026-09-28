package lab

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"testing"
	"time"
)

func TestAdminRenewalAndEntitlementJobsPinItems(t *testing.T) {
	l, clock := openChangingLab(t)
	ctx := context.Background()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	paid := paidProSubscription(t, l, "admin-maintenance-customer")
	*clock = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct{ action, key string }{{"C44", "admin-renewals-001"}, {"C45", "admin-entitlements-001"}} {
		preview, err := l.AdminCreatePreview(ctx, "local-admin", tc.action, "", json.RawMessage(`{}`))
		if err != nil {
			t.Fatalf("preview %s: %v", tc.action, err)
		}
		command, _, err := l.AdminSubmitCommand(ctx, "local-admin", tc.key, tc.action, "", json.RawMessage(`{}`), preview.ID)
		if err != nil {
			t.Fatalf("submit %s: %v", tc.action, err)
		}
		command, err = l.AdminExecuteCommand(ctx, command.ID)
		if err != nil || command.Status != "succeeded" {
			t.Fatalf("execute %s: %+v err=%v", tc.action, command, err)
		}
		var items, receipts int
		if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_job_items i JOIN admin_jobs j ON j.id=i.job_id WHERE j.command_id=?`, command.ID).Scan(&items); err != nil {
			t.Fatal(err)
		}
		if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil {
			t.Fatal(err)
		}
		if items != 1 || receipts != 1 {
			t.Fatalf("%s items=%d receipts=%d", tc.action, items, receipts)
		}
	}
	var periods int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM billing_periods WHERE subscription_id=?`, paid.SubscriptionID).Scan(&periods); err != nil || periods != 2 {
		t.Fatalf("periods=%d err=%v", periods, err)
	}
}

func TestAdminEntitlementRefreshSkipsChangedRevisionWithoutBlockingOtherItems(t *testing.T) {
	l, _ := openChangingLab(t)
	ctx := context.Background()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	changed := paidBasicSubscription(t, l, "entitlement-changed-revision")
	stable := paidBasicSubscription(t, l, "entitlement-stable-revision")
	if _, err := l.db.ExecContext(ctx, `DELETE FROM entitlements WHERE subscription_id IN (?,?)`, changed.SubscriptionID, stable.SubscriptionID); err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{}`)
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C45", "", payload)
	if err != nil {
		t.Fatal(err)
	}
	var revision int64
	if err := l.db.QueryRowContext(ctx, `SELECT revision FROM subscriptions WHERE id=?`, changed.SubscriptionID).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	if _, err := l.ScheduleCancel(ctx, changed.SubscriptionID, revision, "entitlement-source-change"); err != nil {
		t.Fatal(err)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "entitlement-changed-batch", "C45", "", payload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	command, err = l.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "succeeded" {
		t.Fatalf("batch command=%+v err=%v", command, err)
	}
	var changedStatus, changedCode, stableStatus string
	if err := l.db.QueryRowContext(ctx, `SELECT status,error_code FROM admin_job_items WHERE job_id=? AND target_id=?`, "job:"+command.ID, changed.SubscriptionID).Scan(&changedStatus, &changedCode); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT status FROM admin_job_items WHERE job_id=? AND target_id=?`, "job:"+command.ID, stable.SubscriptionID).Scan(&stableStatus); err != nil {
		t.Fatal(err)
	}
	var changedEntitlements, stableEntitlements, receipts int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM entitlements WHERE subscription_id=?`, changed.SubscriptionID).Scan(&changedEntitlements); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM entitlements WHERE subscription_id=? AND status='active'`, stable.SubscriptionID).Scan(&stableEntitlements); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if changedStatus != "conflicted" || changedCode != "SOURCE_CHANGED" || stableStatus != "succeeded" || changedEntitlements != 0 || stableEntitlements != 1 || receipts != 1 {
		t.Fatalf("changed=%s/%s stable=%s entitlements=%d/%d receipts=%d", changedStatus, changedCode, stableStatus, changedEntitlements, stableEntitlements, receipts)
	}
}

func TestAdminRenewalJobKeepsPreviewMembershipWhenAnotherSubscriptionBecomesDue(t *testing.T) {
	l, clock := openChangingLab(t)
	ctx := context.Background()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	first := paidProSubscription(t, l, "renewal-pinned-first")
	*clock = time.Date(2026, 9, 1, 0, 2, 0, 0, time.UTC)
	second := paidProSubscription(t, l, "renewal-pinned-second")
	*clock = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C44", "", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	var source adminMaintenanceSource
	if err := json.Unmarshal(preview.SourceVersions, &source); err != nil {
		t.Fatal(err)
	}
	if len(source.Items) != 1 || source.Items[0].SubscriptionID != first.SubscriptionID {
		t.Fatalf("unexpected pinned renewal members: %+v", source.Items)
	}
	*clock = time.Date(2026, 10, 1, 0, 2, 0, 0, time.UTC)
	command, replay, err := l.AdminSubmitCommand(ctx, "local-admin", "renewal-pinned-first-batch", "C44", "", json.RawMessage(`{}`), preview.ID)
	if err != nil || replay {
		t.Fatalf("submit first batch: %+v replay=%t err=%v", command, replay, err)
	}
	command, err = l.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "succeeded" {
		t.Fatalf("execute first batch: %+v, %v", command, err)
	}
	var firstPeriods, secondPeriods, firstItems, secondItems int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM billing_periods WHERE subscription_id=?`, first.SubscriptionID).Scan(&firstPeriods); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM billing_periods WHERE subscription_id=?`, second.SubscriptionID).Scan(&secondPeriods); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_job_items WHERE job_id=? AND target_id=?`, "job:"+command.ID, first.SubscriptionID).Scan(&firstItems); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_job_items WHERE job_id=? AND target_id=?`, "job:"+command.ID, second.SubscriptionID).Scan(&secondItems); err != nil {
		t.Fatal(err)
	}
	if firstPeriods != 2 || secondPeriods != 1 || firstItems != 1 || secondItems != 0 {
		t.Fatalf("first batch escaped pinned members: periods=%d/%d items=%d/%d", firstPeriods, secondPeriods, firstItems, secondItems)
	}
	replayed, replay, err := l.AdminSubmitCommand(ctx, "local-admin", "renewal-pinned-first-batch", "C44", "", json.RawMessage(`{}`), preview.ID)
	if err != nil || !replay || replayed.ID != command.ID {
		t.Fatalf("first batch replay: %+v replay=%t err=%v", replayed, replay, err)
	}
	newPreview, err := l.AdminCreatePreview(ctx, "local-admin", "C44", "", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(newPreview.SourceVersions, &source); err != nil {
		t.Fatal(err)
	}
	if len(source.Items) != 1 || source.Items[0].SubscriptionID != second.SubscriptionID {
		t.Fatalf("new renewal batch members: %+v", source.Items)
	}
	newCommand, _, err := l.AdminSubmitCommand(ctx, "local-admin", "renewal-pinned-second-batch", "C44", "", json.RawMessage(`{}`), newPreview.ID)
	if err != nil {
		t.Fatal(err)
	}
	newCommand, err = l.AdminExecuteCommand(ctx, newCommand.ID)
	if err != nil || newCommand.Status != "succeeded" {
		t.Fatalf("execute second batch: %+v, %v", newCommand, err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM billing_periods WHERE subscription_id=?`, second.SubscriptionID).Scan(&secondPeriods); err != nil || secondPeriods != 2 {
		t.Fatalf("second renewal: periods=%d err=%v", secondPeriods, err)
	}
}

func TestAdminEntitlementJobsReachSubscriptionBeyondFirstHundred(t *testing.T) {
	l, _ := openChangingLab(t)
	ctx := context.Background()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	all := make(map[string]bool, 101)
	for i := 0; i < 101; i++ {
		paid := paidBasicSubscription(t, l, fmt.Sprintf("entitlement-rotation-%03d", i))
		all[paid.SubscriptionID] = true
	}
	firstPreview, err := l.AdminCreatePreview(ctx, "local-admin", "C45", "", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	var first adminMaintenanceSource
	if err := json.Unmarshal(firstPreview.SourceVersions, &first); err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 100 {
		t.Fatalf("first entitlement batch has %d items", len(first.Items))
	}
	var firstImpact map[string]json.RawMessage
	if err := json.Unmarshal(firstPreview.Impact, &firstImpact); err != nil || string(firstImpact["has_more_candidates"]) != `"true"` {
		t.Fatalf("first entitlement preview hid remaining candidates: %s, %v", firstPreview.Impact, err)
	}
	for _, item := range first.Items {
		delete(all, item.SubscriptionID)
	}
	if len(all) != 1 {
		t.Fatalf("expected one subscription outside first batch, got %d", len(all))
	}
	var skippedID string
	for id := range all {
		skippedID = id
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "entitlement-rotation-first", "C45", "", json.RawMessage(`{}`), firstPreview.ID)
	if err != nil {
		t.Fatal(err)
	}
	command, err = l.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "succeeded" {
		t.Fatalf("first entitlement batch: %+v, %v", command, err)
	}
	secondPreview, err := l.AdminCreatePreview(ctx, "local-admin", "C45", "", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	var second adminMaintenanceSource
	if err := json.Unmarshal(secondPreview.SourceVersions, &second); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range second.Items {
		if item.SubscriptionID == skippedID {
			found = true
		}
	}
	if !found {
		t.Fatalf("subscription %s was starved by repeated C45 batches", skippedID)
	}
	secondCommand, _, err := l.AdminSubmitCommand(ctx, "local-admin", "entitlement-rotation-second", "C45", "", json.RawMessage(`{}`), secondPreview.ID)
	if err != nil {
		t.Fatal(err)
	}
	secondCommand, err = l.AdminExecuteCommand(ctx, secondCommand.ID)
	if err != nil || secondCommand.Status != "succeeded" {
		t.Fatalf("second entitlement batch: %+v, %v", secondCommand, err)
	}
	var entitlementStatus string
	if err := l.db.QueryRowContext(ctx, `SELECT status FROM entitlements WHERE subscription_id=?`, skippedID).Scan(&entitlementStatus); err != nil || entitlementStatus != "active" {
		t.Fatalf("previously skipped entitlement: %s, %v", entitlementStatus, err)
	}
	thirdPreview, err := l.AdminCreatePreview(ctx, "local-admin", "C45", "", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	var third adminMaintenanceSource
	if err := json.Unmarshal(thirdPreview.SourceVersions, &third); err != nil {
		t.Fatal(err)
	}
	wantNext := first.Items[99].SubscriptionID
	found = false
	for _, item := range third.Items {
		if item.SubscriptionID == wantNext {
			found = true
		}
	}
	if !found {
		t.Fatalf("wrapped batch kept refreshing the same first 100; missing %s", wantNext)
	}
}

func TestAdminRenewalJobsReachDueSubscriptionAfterHeldHundred(t *testing.T) {
	l, clock := openChangingLab(t)
	ctx := context.Background()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, 101)
	for i := 0; i < 101; i++ {
		paid := paidProSubscription(t, l, fmt.Sprintf("renewal-rotation-%03d", i))
		ids = append(ids, paid.SubscriptionID)
	}
	sort.Strings(ids)
	prepareMigration(t, l, ids[:100])
	if err := l.PausePriceMigration(ctx, "migration-a"); err != nil {
		t.Fatal(err)
	}
	*clock = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	firstPreview, err := l.AdminCreatePreview(ctx, "local-admin", "C44", "", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	var first adminMaintenanceSource
	if err := json.Unmarshal(firstPreview.SourceVersions, &first); err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 100 || first.Items[99].SubscriptionID != ids[99] {
		t.Fatalf("first renewal batch members: %+v", first.Items)
	}
	var firstImpact map[string]json.RawMessage
	if err := json.Unmarshal(firstPreview.Impact, &firstImpact); err != nil || string(firstImpact["has_more_candidates"]) != `"true"` {
		t.Fatalf("first renewal preview hid remaining candidate: %s, %v", firstPreview.Impact, err)
	}
	firstCommand, _, err := l.AdminSubmitCommand(ctx, "local-admin", "renewal-rotation-held", "C44", "", json.RawMessage(`{}`), firstPreview.ID)
	if err != nil {
		t.Fatal(err)
	}
	firstCommand, err = l.AdminExecuteCommand(ctx, firstCommand.ID)
	if err != nil || firstCommand.Status != "succeeded" {
		t.Fatalf("first renewal batch: %+v, %v", firstCommand, err)
	}
	var unplannedPeriods int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM billing_periods WHERE subscription_id=?`, ids[100]).Scan(&unplannedPeriods); err != nil || unplannedPeriods != 1 {
		t.Fatalf("unplanned subscription renewed without membership: periods=%d err=%v", unplannedPeriods, err)
	}
	secondPreview, err := l.AdminCreatePreview(ctx, "local-admin", "C44", "", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	var second adminMaintenanceSource
	if err := json.Unmarshal(secondPreview.SourceVersions, &second); err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 100 || second.Items[0].SubscriptionID != ids[100] {
		t.Fatalf("held subscriptions starved unplanned renewal: %+v", second.Items)
	}
	secondCommand, _, err := l.AdminSubmitCommand(ctx, "local-admin", "renewal-rotation-unplanned", "C44", "", json.RawMessage(`{}`), secondPreview.ID)
	if err != nil {
		t.Fatal(err)
	}
	secondCommand, err = l.AdminExecuteCommand(ctx, secondCommand.ID)
	if err != nil || secondCommand.Status != "succeeded" {
		t.Fatalf("second renewal batch: %+v, %v", secondCommand, err)
	}
	var amount int64
	if err := l.db.QueryRowContext(ctx, `SELECT i.total_minor FROM billing_periods p JOIN invoices i ON i.id=p.invoice_id WHERE p.subscription_id=? AND p.period_index=1`, ids[100]).Scan(&amount); err != nil || amount != 10000 {
		t.Fatalf("unplanned renewal amount: %d, %v", amount, err)
	}
}
