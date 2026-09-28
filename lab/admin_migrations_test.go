package lab

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestAdminPriceMigrationPlanSkipAndResume(t *testing.T) {
	l, _ := openChangingLab(t)
	ctx := context.Background()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	first := paidProSubscription(t, l, "admin-migration-one")
	second := paidProSubscription(t, l, "admin-migration-two")
	if _, err := l.PublishProPrice(ctx, proV2Spec()); err != nil {
		t.Fatal(err)
	}
	if err := l.SelectCatalogPrice(ctx, "pro", "A", fixedNow, "pro-v2"); err != nil {
		t.Fatal(err)
	}
	planPayload, _ := json.Marshal(adminPlanMigrationPayload{ID: "admin-migration-001", Cohort: "A", TargetPriceVersionID: "pro-v2", SubscriptionIDs: []string{first.SubscriptionID, second.SubscriptionID}})
	planPreview, err := l.AdminCreatePreview(ctx, "local-admin", "C22", "", planPayload)
	if err != nil {
		t.Fatal(err)
	}
	plan, _, err := l.AdminSubmitCommand(ctx, "local-admin", "plan-migration-001", "C22", "", planPayload, planPreview.ID)
	if err != nil {
		t.Fatal(err)
	}
	plan, err = l.AdminExecuteCommand(ctx, plan.ID)
	if err != nil || plan.Status != "succeeded" {
		t.Fatalf("plan: %+v %v", plan, err)
	}
	pause, _, err := l.AdminSubmitCommand(ctx, "local-admin", "pause-migration-001", "C23", "admin-migration-001", json.RawMessage(`{}`), "")
	if err != nil {
		t.Fatal(err)
	}
	pause, err = l.AdminExecuteCommand(ctx, pause.ID)
	if err != nil || pause.Status != "succeeded" {
		t.Fatalf("pause: %+v %v", pause, err)
	}
	skipPayload, _ := json.Marshal(adminSkipMigrationPayload{SubscriptionID: first.SubscriptionID, Reason: "customer opted out"})
	skipPreview, err := l.AdminCreatePreview(ctx, "local-admin", "C24", "admin-migration-001", skipPayload)
	if err != nil {
		t.Fatal(err)
	}
	skip, _, err := l.AdminSubmitCommand(ctx, "local-admin", "skip-migration-001", "C24", "admin-migration-001", skipPayload, skipPreview.ID)
	if err != nil {
		t.Fatal(err)
	}
	skip, err = l.AdminExecuteCommand(ctx, skip.ID)
	if err != nil || skip.Status != "succeeded" {
		t.Fatalf("skip: %+v %v", skip, err)
	}
	resumePayload := json.RawMessage(`{}`)
	resumePreview, err := l.AdminCreatePreview(ctx, "local-admin", "C25", "admin-migration-001", resumePayload)
	if err != nil {
		t.Fatal(err)
	}
	resume, _, err := l.AdminSubmitCommand(ctx, "local-admin", "resume-migration-001", "C25", "admin-migration-001", resumePayload, resumePreview.ID)
	if err != nil {
		t.Fatal(err)
	}
	resume, err = l.AdminExecuteCommand(ctx, resume.ID)
	if err != nil || resume.Status != "succeeded" {
		t.Fatalf("resume: %+v %v", resume, err)
	}
	batch, err := l.PriceMigration(ctx, "admin-migration-001")
	if err != nil || batch.Status != "active" {
		t.Fatalf("batch: %+v %v", batch, err)
	}
	var skipped, pending, receipts int
	for _, item := range batch.Items {
		if item.Status == "skipped" {
			skipped++
		}
		if item.Status == "pending" {
			pending++
		}
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id IN (?,?,?,?)`, plan.ID, pause.ID, skip.ID, resume.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if skipped != 1 || pending != 1 || receipts != 4 {
		t.Fatalf("skipped=%d pending=%d receipts=%d", skipped, pending, receipts)
	}
}

func TestAdminMigrationResumeInvalidatesConcurrentSkipPreview(t *testing.T) {
	ctx := context.Background()
	l, _ := openChangingLab(t)
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	sub := paidProSubscription(t, l, "migration-skip-resume-race")
	if _, err := l.PublishProPrice(ctx, proV2Spec()); err != nil {
		t.Fatal(err)
	}
	if err := l.SelectCatalogPrice(ctx, "pro", "A", fixedNow, "pro-v2"); err != nil {
		t.Fatal(err)
	}
	const migrationID = "migration-skip-resume-race"
	if _, err := l.PlanPriceMigration(ctx, migrationID, "A", "pro-v2", []string{sub.SubscriptionID}); err != nil {
		t.Fatal(err)
	}
	if err := l.PausePriceMigration(ctx, migrationID); err != nil {
		t.Fatal(err)
	}
	skipPayload, err := json.Marshal(adminSkipMigrationPayload{SubscriptionID: sub.SubscriptionID, Reason: "operator skip"})
	if err != nil {
		t.Fatal(err)
	}
	skipPreview, err := l.AdminCreatePreview(ctx, "local-admin", "C24", migrationID, skipPayload)
	if err != nil {
		t.Fatal(err)
	}
	resumePayload := json.RawMessage(`{}`)
	resumePreview, err := l.AdminCreatePreview(ctx, "local-admin", "C25", migrationID, resumePayload)
	if err != nil {
		t.Fatal(err)
	}
	skip, _, err := l.AdminSubmitCommand(ctx, "local-admin", "migration-stale-skip", "C24", migrationID, skipPayload, skipPreview.ID)
	if err != nil {
		t.Fatal(err)
	}
	resume, _, err := l.AdminSubmitCommand(ctx, "local-admin", "migration-first-resume", "C25", migrationID, resumePayload, resumePreview.ID)
	if err != nil {
		t.Fatal(err)
	}
	resume, err = l.AdminExecuteCommand(ctx, resume.ID)
	if err != nil || resume.Status != "succeeded" {
		t.Fatalf("resume before skip %+v %v", resume, err)
	}
	skip, err = l.AdminExecuteCommand(ctx, skip.ID)
	if err != nil || skip.Status != "failed" || skip.ErrorCode != "PREVIEW_STALE" {
		t.Fatalf("old skip preview %+v %v", skip, err)
	}
	batch, err := l.PriceMigration(ctx, migrationID)
	if err != nil || batch.Status != "active" || len(batch.Items) != 1 || batch.Items[0].Status != "pending" {
		t.Fatalf("resume changed membership or old skip was applied: %+v %v", batch, err)
	}
	var receipts int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id IN (?,?)`, skip.ID, resume.ID).Scan(&receipts); err != nil || receipts != 1 {
		t.Fatalf("stale skip and resume receipts %d %v", receipts, err)
	}
	replayed, replay, err := l.AdminSubmitCommand(ctx, "local-admin", "migration-stale-skip", "C24", migrationID, skipPayload, skipPreview.ID)
	if err != nil || !replay || replayed.ID != skip.ID || replayed.Status != "failed" {
		t.Fatalf("stale skip replay %+v replay=%t err=%v", replayed, replay, err)
	}
	changedPayload, err := json.Marshal(adminSkipMigrationPayload{SubscriptionID: sub.SubscriptionID, Reason: "different reason"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := l.AdminSubmitCommand(ctx, "local-admin", "migration-stale-skip", "C24", migrationID, changedPayload, skipPreview.ID); !errors.Is(err, ErrAdminIdempotencyConflict) {
		t.Fatalf("changed skip reused original key: %v", err)
	}
	if err := l.PausePriceMigration(ctx, migrationID); err != nil {
		t.Fatal(err)
	}
	freshPreview, err := l.AdminCreatePreview(ctx, "local-admin", "C24", migrationID, skipPayload)
	if err != nil {
		t.Fatal(err)
	}
	fresh, _, err := l.AdminSubmitCommand(ctx, "local-admin", "migration-fresh-skip", "C24", migrationID, skipPayload, freshPreview.ID)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err = l.AdminExecuteCommand(ctx, fresh.ID)
	if err != nil || fresh.Status != "succeeded" {
		t.Fatalf("fresh skip %+v %v", fresh, err)
	}
	batch, err = l.PriceMigration(ctx, migrationID)
	if err != nil || batch.Status != "completed" || len(batch.Items) != 1 || batch.Items[0].Status != "skipped" {
		t.Fatalf("fresh skip did not complete the skipped-only batch: %+v %v", batch, err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id IN (?,?,?)`, skip.ID, resume.ID, fresh.ID).Scan(&receipts); err != nil || receipts != 2 {
		t.Fatalf("migration control receipts after fresh skip %d %v", receipts, err)
	}
}
