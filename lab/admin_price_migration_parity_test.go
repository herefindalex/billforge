package lab

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"
)

func TestAdminPriceMigrationAtBoundaryMatchesDomain(t *testing.T) {
	ctx := context.Background()
	now := fixedNow
	open := func() *Lab {
		t.Helper()
		dir := t.TempDir()
		l, err := Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), func() time.Time { return now })
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = l.Close() })
		return l
	}
	domain, admin := open(), open()
	if err := admin.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	domainSubs := []Receipt{paidProSubscription(t, domain, "migration-parity-one"), paidProSubscription(t, domain, "migration-parity-two")}
	adminSubs := []Receipt{paidProSubscription(t, admin, "migration-parity-one"), paidProSubscription(t, admin, "migration-parity-two")}
	runAdmin := func(action, key string, payload json.RawMessage) {
		t.Helper()
		preview, err := admin.AdminCreatePreview(ctx, "local-admin", action, "", payload)
		if err != nil {
			t.Fatalf("%s preview: %v", action, err)
		}
		command, _, err := admin.AdminSubmitCommand(ctx, "local-admin", key, action, "", payload, preview.ID)
		if err != nil {
			t.Fatalf("%s submit: %v", action, err)
		}
		command, err = admin.AdminExecuteCommand(ctx, command.ID)
		if err != nil || command.Status != "succeeded" {
			t.Fatalf("%s execute: %+v err=%v", action, command, err)
		}
	}
	marshal := func(value any) json.RawMessage {
		t.Helper()
		payload, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return payload
	}
	comparePeriod := func(label string, index, subscriber, expectedMinor int, expectedVersion string, expectedApplied int64) {
		t.Helper()
		d := loadRenewalFactsAt(t, domain, domainSubs[subscriber].SubscriptionID, index)
		a := loadRenewalFactsAt(t, admin, adminSubs[subscriber].SubscriptionID, index)
		if !reflect.DeepEqual(d, a) || a.InvoiceMinor != int64(expectedMinor) || a.PaymentMinor != int64(expectedMinor) || a.PriceVersion != expectedVersion || a.Allocated != expectedApplied {
			t.Fatalf("%s period differs: domain=%+v admin=%+v", label, d, a)
		}
	}
	compareAssignments := func(label string, subscriber, expectedCount int, expectedVersion, expectedMigration string) {
		t.Helper()
		read := func(l *Lab, subID string) (int, string, string) {
			t.Helper()
			var count int
			var version, source string
			if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pricing_assignments WHERE subscription_id=?`, subID).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if err := l.db.QueryRowContext(ctx, `SELECT price_version_id,COALESCE(source_migration_id,'') FROM pricing_assignments WHERE subscription_id=? AND effective_end IS NULL`, subID).Scan(&version, &source); err != nil {
				t.Fatal(err)
			}
			return count, version, source
		}
		dCount, dVersion, dSource := read(domain, domainSubs[subscriber].SubscriptionID)
		aCount, aVersion, aSource := read(admin, adminSubs[subscriber].SubscriptionID)
		if dCount != expectedCount || aCount != expectedCount || dVersion != expectedVersion || aVersion != expectedVersion || dSource != expectedMigration || aSource != expectedMigration {
			t.Fatalf("%s assignment differs: domain=%d/%s/%s admin=%d/%s/%s", label, dCount, dVersion, dSource, aCount, aVersion, aSource)
		}
	}
	compareMigration := func(id, expectedStatus string, expectedItems int) {
		t.Helper()
		for _, l := range []*Lab{domain, admin} {
			batch, err := l.PriceMigration(ctx, id)
			if err != nil || batch.Status != expectedStatus || len(batch.Items) != expectedItems {
				t.Fatalf("migration %s: %+v err=%v", id, batch, err)
			}
			for _, item := range batch.Items {
				if item.Status != "applied" {
					t.Fatalf("migration %s item: %+v", id, item)
				}
			}
		}
	}

	spec := proV2Spec()
	if _, err := domain.PublishProPrice(ctx, spec); err != nil {
		t.Fatal(err)
	}
	runAdmin("C18", "migration-parity-publish", marshal(adminProPricePayload{
		ID: spec.ID, Version: "2", FixedMinor: "6000", SeatMinor: "1000", IncludedTasks: "20000",
		UsageRateNum: "1", UsageRateDen: "10", EffectiveFrom: spec.EffectiveFrom.Format(time.RFC3339Nano),
	}))
	if err := domain.SelectCatalogPrice(ctx, "pro", "A", fixedNow, "pro-v2"); err != nil {
		t.Fatal(err)
	}
	runAdmin("C21", "migration-parity-select", marshal(adminSelectionPayload{
		PlanID: "pro", Cohort: "A", EffectiveAt: fixedNow.Format(time.RFC3339Nano), PriceVersionID: "pro-v2",
	}))
	domainIDs := []string{domainSubs[0].SubscriptionID, domainSubs[1].SubscriptionID}
	adminIDs := []string{adminSubs[0].SubscriptionID, adminSubs[1].SubscriptionID}
	if _, err := domain.PlanPriceMigration(ctx, "migration-parity-a", "A", "pro-v2", domainIDs); err != nil {
		t.Fatal(err)
	}
	runAdmin("C22", "migration-parity-plan", marshal(adminPlanMigrationPayload{
		ID: "migration-parity-a", Cohort: "A", TargetPriceVersionID: "pro-v2", SubscriptionIDs: adminIDs,
	}))
	for i := range domainSubs {
		compareAssignments("before migration", i, 1, "pro-v1", "")
	}
	now = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if renewed, err := domain.RunRenewals(ctx); err != nil || len(renewed) != 2 {
		t.Fatalf("domain v2 renewals: %+v err=%v", renewed, err)
	}
	runAdmin("C44", "migration-parity-v2-renewals", json.RawMessage(`{}`))
	compareMigration("migration-parity-a", "completed", 2)
	for i := range domainSubs {
		comparePeriod("v2 renewal", 1, i, 11000, "pro-v2", 0)
		compareAssignments("v2 assignment", i, 2, "pro-v2", "migration-parity-a")
	}
	for _, l := range []*Lab{domain, admin} {
		for range domainSubs {
			if _, err := l.DispatchNext(ctx, ""); err != nil {
				t.Fatal(err)
			}
		}
	}
	for i := range domainSubs {
		comparePeriod("settled v2 renewal", 1, i, 11000, "pro-v2", 11000)
	}
	if _, err := domain.PlanPriceMigration(ctx, "migration-parity-reverse", "default", "pro-v1", []string{domainSubs[0].SubscriptionID}); err != nil {
		t.Fatal(err)
	}
	runAdmin("C22", "migration-parity-reverse-plan", marshal(adminPlanMigrationPayload{
		ID: "migration-parity-reverse", Cohort: "default", TargetPriceVersionID: "pro-v1", SubscriptionIDs: []string{adminSubs[0].SubscriptionID},
	}))
	now = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	if renewed, err := domain.RunRenewals(ctx); err != nil || len(renewed) != 2 {
		t.Fatalf("domain reverse renewals: %+v err=%v", renewed, err)
	}
	runAdmin("C44", "migration-parity-reverse-renewals", json.RawMessage(`{}`))
	compareMigration("migration-parity-reverse", "completed", 1)
	comparePeriod("reverse v1 renewal", 2, 0, 10000, "pro-v1", 0)
	comparePeriod("continuing v2 renewal", 2, 1, 11000, "pro-v2", 0)
	compareAssignments("reversed assignment", 0, 3, "pro-v1", "migration-parity-reverse")
	compareAssignments("continuing assignment", 1, 2, "pro-v2", "migration-parity-a")
}

func TestAdminPartialPriceMigrationRequiresExplicitSkipMatchesDomain(t *testing.T) {
	ctx := context.Background()
	now := fixedNow
	open := func() *Lab {
		t.Helper()
		dir := t.TempDir()
		l, err := Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), func() time.Time { return now })
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = l.Close() })
		return l
	}
	domain, admin := open(), open()
	if err := admin.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	setup := func(l *Lab) []string {
		t.Helper()
		first := paidProSubscription(t, l, "partial-parity-one")
		second := paidProSubscription(t, l, "partial-parity-two")
		ids := []string{first.SubscriptionID, second.SubscriptionID}
		sort.Strings(ids)
		prepareMigration(t, l, ids)
		if _, err := l.db.ExecContext(ctx, `UPDATE subscriptions SET revision=revision+1 WHERE id=?`, ids[1]); err != nil {
			t.Fatal(err)
		}
		return ids
	}
	domainIDs, adminIDs := setup(domain), setup(admin)
	runAdmin := func(action, target, key string, payload json.RawMessage) {
		t.Helper()
		preview, err := admin.AdminCreatePreview(ctx, "local-admin", action, target, payload)
		if err != nil {
			t.Fatalf("%s preview: %v", action, err)
		}
		command, _, err := admin.AdminSubmitCommand(ctx, "local-admin", key, action, target, payload, preview.ID)
		if err != nil {
			t.Fatalf("%s submit: %v", action, err)
		}
		command, err = admin.AdminExecuteCommand(ctx, command.ID)
		if err != nil || command.Status != "succeeded" {
			t.Fatalf("%s execute: %+v err=%v", action, command, err)
		}
	}
	compareMigration := func(expectedStatus, firstStatus, secondStatus string) {
		t.Helper()
		for _, l := range []*Lab{domain, admin} {
			batch, err := l.PriceMigration(ctx, "migration-a")
			if err != nil || batch.Status != expectedStatus || len(batch.Items) != 2 || batch.Items[0].Status != firstStatus || batch.Items[1].Status != secondStatus {
				t.Fatalf("migration state: %+v err=%v", batch, err)
			}
			if secondStatus == "conflicted" && batch.Items[1].ConflictReason != "revision_changed" {
				t.Fatalf("missing revision conflict: %+v", batch.Items[1])
			}
		}
	}
	comparePeriod := func(index int, expectedMinor int64) {
		t.Helper()
		d := loadRenewalFactsAt(t, domain, domainIDs[index], 1)
		a := loadRenewalFactsAt(t, admin, adminIDs[index], 1)
		if !reflect.DeepEqual(d, a) || a.InvoiceMinor != expectedMinor || a.PaymentMinor != expectedMinor {
			t.Fatalf("period %d differs: domain=%+v admin=%+v", index, d, a)
		}
	}

	now = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if renewed, err := domain.RunRenewals(ctx); err != nil || len(renewed) != 1 || renewed[0].SubscriptionID != domainIDs[0] || renewed[0].AmountMinor != 11000 {
		t.Fatalf("domain partial migration: %+v err=%v", renewed, err)
	}
	runAdmin("C44", "", "partial-parity-first-renewal", json.RawMessage(`{}`))
	compareMigration("paused", "applied", "conflicted")
	comparePeriod(0, 11000)
	for _, item := range []struct {
		l   *Lab
		ids []string
	}{{domain, domainIDs}, {admin, adminIDs}} {
		var next int
		if err := item.l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM billing_periods WHERE subscription_id=? AND period_index=1`, item.ids[1]).Scan(&next); err != nil || next != 0 {
			t.Fatalf("conflicted item renewed before review: count=%d err=%v", next, err)
		}
	}
	if err := domain.SkipPriceMigrationItem(ctx, "migration-a", domainIDs[1]); err != nil {
		t.Fatal(err)
	}
	skipPayload, err := json.Marshal(adminSkipMigrationPayload{SubscriptionID: adminIDs[1], Reason: "revision changed; retain existing price"})
	if err != nil {
		t.Fatal(err)
	}
	runAdmin("C24", "migration-a", "partial-parity-explicit-skip", skipPayload)
	if renewed, err := domain.RunRenewals(ctx); err != nil || len(renewed) != 1 || renewed[0].SubscriptionID != domainIDs[1] || renewed[0].AmountMinor != 10000 {
		t.Fatalf("domain skipped renewal: %+v err=%v", renewed, err)
	}
	runAdmin("C44", "", "partial-parity-second-renewal", json.RawMessage(`{}`))
	compareMigration("completed", "applied", "skipped")
	comparePeriod(0, 11000)
	comparePeriod(1, 10000)
	for _, item := range []struct {
		l   *Lab
		ids []string
	}{{domain, domainIDs}, {admin, adminIDs}} {
		for i, expected := range []string{"pro-v2", "pro-v1"} {
			var version string
			if err := item.l.db.QueryRowContext(ctx, `SELECT price_version_id FROM pricing_assignments WHERE subscription_id=? AND effective_end IS NULL`, item.ids[i]).Scan(&version); err != nil || version != expected {
				t.Fatalf("post-skip price version=%s want=%s err=%v", version, expected, err)
			}
		}
	}
}

func TestAdminPauseAndResumePriceMigrationMatchesDomain(t *testing.T) {
	ctx := context.Background()
	now := fixedNow
	open := func() *Lab {
		t.Helper()
		dir := t.TempDir()
		l, err := Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), func() time.Time { return now })
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = l.Close() })
		return l
	}
	domain, admin := open(), open()
	if err := admin.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	domainReceipt := paidProSubscription(t, domain, "pause-resume-parity")
	adminReceipt := paidProSubscription(t, admin, "pause-resume-parity")
	prepareMigration(t, domain, []string{domainReceipt.SubscriptionID})
	prepareMigration(t, admin, []string{adminReceipt.SubscriptionID})
	runAdmin := func(action, key string, previewRequired bool) {
		t.Helper()
		payload := json.RawMessage(`{}`)
		previewID := ""
		if previewRequired {
			preview, err := admin.AdminCreatePreview(ctx, "local-admin", action, "migration-a", payload)
			if err != nil {
				t.Fatalf("%s preview: %v", action, err)
			}
			previewID = preview.ID
		}
		command, _, err := admin.AdminSubmitCommand(ctx, "local-admin", key, action, "migration-a", payload, previewID)
		if err != nil {
			t.Fatalf("%s submit: %v", action, err)
		}
		command, err = admin.AdminExecuteCommand(ctx, command.ID)
		if err != nil || command.Status != "succeeded" {
			t.Fatalf("%s execute: %+v err=%v", action, command, err)
		}
	}
	compareMigration := func(expected string) {
		t.Helper()
		for _, l := range []*Lab{domain, admin} {
			batch, err := l.PriceMigration(ctx, "migration-a")
			if err != nil || batch.Status != expected || len(batch.Items) != 1 || batch.Items[0].Status != "pending" {
				t.Fatalf("migration %s: %+v err=%v", expected, batch, err)
			}
		}
	}
	if err := domain.PausePriceMigration(ctx, "migration-a"); err != nil {
		t.Fatal(err)
	}
	runAdmin("C23", "pause-resume-parity-pause", false)
	compareMigration("paused")
	if err := domain.ResumePriceMigration(ctx, "migration-a"); err != nil {
		t.Fatal(err)
	}
	runAdmin("C25", "pause-resume-parity-resume", true)
	compareMigration("active")
	now = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if renewed, err := domain.RunRenewals(ctx); err != nil || len(renewed) != 1 || renewed[0].AmountMinor != 11000 {
		t.Fatalf("domain resumed migration renewal: %+v err=%v", renewed, err)
	}
	payload := json.RawMessage(`{}`)
	preview, err := admin.AdminCreatePreview(ctx, "local-admin", "C44", "", payload)
	if err != nil {
		t.Fatal(err)
	}
	command, _, err := admin.AdminSubmitCommand(ctx, "local-admin", "pause-resume-parity-renewal", "C44", "", payload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	command, err = admin.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "succeeded" {
		t.Fatalf("admin resumed migration renewal: %+v err=%v", command, err)
	}
	d, a := loadRenewalFacts(t, domain, domainReceipt.SubscriptionID), loadRenewalFacts(t, admin, adminReceipt.SubscriptionID)
	if !reflect.DeepEqual(d, a) || a.InvoiceMinor != 11000 || a.PriceVersion != "pro-v2" {
		t.Fatalf("resumed migration financial facts differ: domain=%+v admin=%+v", d, a)
	}
	for _, l := range []*Lab{domain, admin} {
		batch, err := l.PriceMigration(ctx, "migration-a")
		if err != nil || batch.Status != "completed" || batch.Items[0].Status != "applied" {
			t.Fatalf("completed migration: %+v err=%v", batch, err)
		}
	}
}
