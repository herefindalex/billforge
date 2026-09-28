package lab

import (
	"context"
	"encoding/json"
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
