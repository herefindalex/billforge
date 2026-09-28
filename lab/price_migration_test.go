package lab

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"testing"
	"time"
)

func paidProSubscription(t *testing.T, l *Lab, customer string) Receipt {
	t.Helper()
	ctx := context.Background()
	q, err := l.CreateQuoteWithSeats(ctx, customer, "pro", 5)
	if err != nil {
		t.Fatal(err)
	}
	r, err := l.AcceptQuote(ctx, q.ID, q.Fingerprint, customer+":purchase")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.DispatchNext(ctx, ""); err != nil {
		t.Fatal(err)
	}
	return r
}

func prepareMigration(t *testing.T, l *Lab, ids []string) PriceMigration {
	t.Helper()
	ctx := context.Background()
	if _, err := l.PublishProPrice(ctx, proV2Spec()); err != nil {
		t.Fatal(err)
	}
	if err := l.SelectCatalogPrice(ctx, "pro", "A", fixedNow, "pro-v2"); err != nil {
		t.Fatal(err)
	}
	preview, err := l.PreviewPriceMigration(ctx, "pro-v2", ids)
	if err != nil || len(preview) != len(ids) {
		t.Fatalf("preview: %+v, %v", preview, err)
	}
	for _, x := range preview {
		if x.PriorAmountMinor != 10000 || x.TargetAmountMinor != 11000 || !x.EffectiveAt.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)) {
			t.Fatalf("migration oracle: %+v", x)
		}
	}
	m, err := l.PlanPriceMigration(ctx, "migration-a", "A", "pro-v2", ids)
	if err != nil {
		t.Fatal(err)
	}
	if replay, err := l.PlanPriceMigration(ctx, "migration-a", "A", "pro-v2", ids); err != nil || replay.ID != m.ID || len(replay.Items) != len(ids) {
		t.Fatalf("migration replay: %+v, %v", replay, err)
	}
	return m
}

func TestPriceMigrationAddsConflictReasonWithoutChangingExistingItems(t *testing.T) {
	l, _ := openChangingLab(t)
	paid := paidProSubscription(t, l, "migration-schema-upgrade")
	prepareMigration(t, l, []string{paid.SubscriptionID})
	if _, err := l.db.Exec(`ALTER TABLE price_migration_items DROP COLUMN conflict_reason`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := migratePriceMigrations(l.db); err != nil {
			t.Fatal(err)
		}
	}
	migration, err := l.PriceMigration(context.Background(), "migration-a")
	if err != nil || len(migration.Items) != 1 || migration.Items[0].Status != "pending" || migration.Items[0].ConflictReason != "" {
		t.Fatalf("upgraded migration: %+v %v", migration, err)
	}
}

func TestS08MigrationAtBoundaryAndReplay(t *testing.T) {
	l, clock := openChangingLab(t)
	ctx := context.Background()
	a := paidProSubscription(t, l, "migration-one")
	b := paidProSubscription(t, l, "migration-two")
	prepareMigration(t, l, []string{a.SubscriptionID, b.SubscriptionID})
	*clock = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	renewed, err := l.RunRenewals(ctx)
	if err != nil || len(renewed) != 2 {
		t.Fatalf("renewals: %+v, %v", renewed, err)
	}
	for _, r := range renewed {
		if r.AmountMinor != 11000 {
			t.Fatalf("v2 renewal: %+v", r)
		}
	}
	m, err := l.PriceMigration(ctx, "migration-a")
	if err != nil || m.Status != "completed" {
		t.Fatalf("migration completed: %+v, %v", m, err)
	}
	for _, item := range m.Items {
		if item.Status != "applied" {
			t.Fatalf("item: %+v", item)
		}
	}
	for _, id := range []string{a.SubscriptionID, b.SubscriptionID} {
		var version, source string
		if err := l.db.QueryRowContext(ctx, `SELECT price_version_id,source_migration_id FROM pricing_assignments WHERE subscription_id=? AND effective_end IS NULL`, id).Scan(&version, &source); err != nil || version != "pro-v2" || source != "migration-a" {
			t.Fatalf("assignment: %s %s %v", version, source, err)
		}
	}
	if again, err := l.RunRenewals(ctx); err != nil || len(again) != 0 {
		t.Fatalf("boundary replay: %+v, %v", again, err)
	}
	for range renewed {
		if _, err := l.DispatchNext(ctx, ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := l.PlanPriceMigration(ctx, "reverse-a", "default", "pro-v1", []string{a.SubscriptionID}); err != nil {
		t.Fatalf("reverse migration plan: %v", err)
	}
	*clock = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	reversed, err := l.RunRenewals(ctx)
	if err != nil || len(reversed) != 2 {
		t.Fatalf("reverse renewals: %+v, %v", reversed, err)
	}
	for _, r := range reversed {
		want := int64(11000)
		if r.SubscriptionID == a.SubscriptionID {
			want = 10000
		}
		if r.AmountMinor != want {
			t.Fatalf("reverse amount: %+v want %d", r, want)
		}
	}
	var history int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pricing_assignments WHERE subscription_id=?`, a.SubscriptionID).Scan(&history); err != nil || history != 3 {
		t.Fatalf("reverse assignment history: %d, %v", history, err)
	}
}

func TestS08PartialMigrationStopsAndRequiresExplicitSkip(t *testing.T) {
	l, clock := openChangingLab(t)
	ctx := context.Background()
	a := paidProSubscription(t, l, "migration-partial-one")
	b := paidProSubscription(t, l, "migration-partial-two")
	ids := []string{a.SubscriptionID, b.SubscriptionID}
	sort.Strings(ids)
	prepareMigration(t, l, ids)
	if _, err := l.db.ExecContext(ctx, `UPDATE subscriptions SET revision=revision+1 WHERE id=?`, ids[1]); err != nil {
		t.Fatal(err)
	}
	*clock = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	renewed, err := l.RunRenewals(ctx)
	if err != nil || len(renewed) != 1 || renewed[0].SubscriptionID != ids[0] || renewed[0].AmountMinor != 11000 {
		t.Fatalf("partial renewal: %+v, %v", renewed, err)
	}
	m, err := l.PriceMigration(ctx, "migration-a")
	if err != nil || m.Status != "paused" || m.Items[0].Status != "applied" || m.Items[1].Status != "conflicted" || m.Items[1].ConflictReason != "revision_changed" {
		t.Fatalf("paused migration: %+v, %v", m, err)
	}
	if err := l.ResumePriceMigration(ctx, m.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("resume without review: %v", err)
	}
	if err := l.SkipPriceMigrationItem(ctx, m.ID, ids[1]); err != nil {
		t.Fatal(err)
	}
	renewed, err = l.RunRenewals(ctx)
	if err != nil || len(renewed) != 1 || renewed[0].SubscriptionID != ids[1] || renewed[0].AmountMinor != 10000 {
		t.Fatalf("skipped item keeps v1: %+v, %v", renewed, err)
	}
	m, err = l.PriceMigration(ctx, m.ID)
	if err != nil || m.Status != "completed" || m.Items[1].Status != "skipped" {
		t.Fatalf("resolved partial migration: %+v, %v", m, err)
	}
}

func TestPendingMigrationRejectsScheduleChangesBeforeAdmission(t *testing.T) {
	l, _ := openChangingLab(t)
	ctx := context.Background()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	paid := paidProSubscription(t, l, "migration-cancel-conflict")
	prepareMigration(t, l, []string{paid.SubscriptionID})
	if _, err := l.AdminCreatePreview(ctx, "local-admin", "C05", paid.SubscriptionID, json.RawMessage(`{"revision":"1"}`)); !errors.Is(err, ErrConflict) {
		t.Fatalf("cancel preview did not expose pending migration: %v", err)
	}
	changeQuote, err := l.CreateQuoteForCohort(ctx, "migration-cancel-conflict", "pro", "default", 5)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := l.BindChangeQuote(ctx, changeQuote.ID, paid.SubscriptionID, "next_period", 1)
	if err != nil {
		t.Fatal(err)
	}
	changePayload, err := json.Marshal(AdminChangePlanPayload{QuoteID: changeQuote.ID, Fingerprint: binding.Fingerprint, Revision: "1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.AdminCreatePreview(ctx, "local-admin", "C03", paid.SubscriptionID, changePayload); !errors.Is(err, ErrConflict) {
		t.Fatalf("next plan preview did not expose pending migration: %v", err)
	}
	if _, err := l.ScheduleCancel(ctx, paid.SubscriptionID, 1, "cancel-after-migration-plan"); !errors.Is(err, ErrConflict) {
		t.Fatalf("schedule cancel bypassed pending migration: %v", err)
	}
	if _, err := l.ScheduleNextPlan(ctx, paid.SubscriptionID, "basic", 0, 1, "change-after-migration-plan"); !errors.Is(err, ErrConflict) {
		t.Fatalf("schedule plan bypassed pending migration: %v", err)
	}
	migration, err := l.PriceMigration(ctx, "migration-a")
	if err != nil || migration.Status != "active" || len(migration.Items) != 1 || migration.Items[0].Status != "pending" {
		t.Fatalf("rejected schedule changed migration: %+v %v", migration, err)
	}
	var schedules int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM subscription_schedules WHERE subscription_id=?`, paid.SubscriptionID).Scan(&schedules); err != nil || schedules != 0 {
		t.Fatalf("rejected schedule wrote state: %d %v", schedules, err)
	}
}

func TestMigrationPlannedAfterCancelPreviewRejectsStaleCommand(t *testing.T) {
	l, _ := openChangingLab(t)
	ctx := context.Background()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	paid := paidProSubscription(t, l, "migration-after-cancel-preview")
	payload := json.RawMessage(`{"revision":"1"}`)
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C05", paid.SubscriptionID, payload)
	if err != nil {
		t.Fatal(err)
	}
	prepareMigration(t, l, []string{paid.SubscriptionID})
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "cancel-after-migration-plan", "C05", paid.SubscriptionID, payload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	command, err = l.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "failed" || command.ErrorCode != "PREVIEW_STALE" {
		t.Fatalf("stale cancel command: %+v, %v", command, err)
	}
	var schedules int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM subscription_schedules WHERE subscription_id=?`, paid.SubscriptionID).Scan(&schedules); err != nil || schedules != 0 {
		t.Fatalf("stale cancel wrote schedule: %d %v", schedules, err)
	}
	migration, err := l.PriceMigration(ctx, "migration-a")
	if err != nil || migration.Items[0].Status != "pending" {
		t.Fatalf("stale cancel changed migration: %+v, %v", migration, err)
	}
}

func TestPriceMigrationConflictPreservesFinancialFacts(t *testing.T) {
	for _, tc := range []struct {
		name   string
		reason string
	}{
		{name: "source_price", reason: "source_price_changed"},
		{name: "seat_quantity", reason: "seat_quantity_changed"},
		{name: "scheduled_change", reason: "scheduled_change"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l, clock := openChangingLab(t)
			ctx := context.Background()
			paid := paidProSubscription(t, l, "migration-conflict-"+tc.name)
			prepareMigration(t, l, []string{paid.SubscriptionID})
			var boundary int64
			if err := l.db.QueryRowContext(ctx, `SELECT period_end FROM billing_periods WHERE subscription_id=? AND period_index=0`, paid.SubscriptionID).Scan(&boundary); err != nil {
				t.Fatal(err)
			}
			switch tc.name {
			case "source_price":
				_, err := l.db.ExecContext(ctx, `UPDATE subscriptions SET price_version_id='basic-v1' WHERE id=?`, paid.SubscriptionID)
				if err != nil {
					t.Fatal(err)
				}
			case "seat_quantity":
				_, err := l.db.ExecContext(ctx, `UPDATE subscriptions SET seat_quantity=6 WHERE id=?`, paid.SubscriptionID)
				if err != nil {
					t.Fatal(err)
				}
			case "scheduled_change":
				_, err := l.db.ExecContext(ctx, `INSERT INTO subscription_schedules(id,subscription_id,kind,target_price_version_id,seat_quantity,effective_at,status,request_key,created_revision,created_at) VALUES(?,?,'change','basic-v1',0,?,'scheduled',?,1,?)`, "schedule-conflict", paid.SubscriptionID, boundary, "schedule-conflict", fixedNow.UnixNano())
				if err != nil {
					t.Fatal(err)
				}
			}
			*clock = time.Unix(0, boundary).UTC()
			renewed, err := l.RunRenewals(ctx)
			if err != nil || len(renewed) != 0 {
				t.Fatalf("conflicted renewal: %+v, %v", renewed, err)
			}
			migration, err := l.PriceMigration(ctx, "migration-a")
			if err != nil || migration.Status != "paused" || len(migration.Items) != 1 || migration.Items[0].Status != "conflicted" || migration.Items[0].ConflictReason != tc.reason {
				t.Fatalf("conflict state: %+v, %v", migration, err)
			}
			var periods, invoices, assignments int
			if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM billing_periods WHERE subscription_id=?`, paid.SubscriptionID).Scan(&periods); err != nil {
				t.Fatal(err)
			}
			if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM invoices WHERE subscription_id=?`, paid.SubscriptionID).Scan(&invoices); err != nil {
				t.Fatal(err)
			}
			if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pricing_assignments WHERE subscription_id=?`, paid.SubscriptionID).Scan(&assignments); err != nil {
				t.Fatal(err)
			}
			if periods != 1 || invoices != 1 || assignments != 1 {
				t.Fatalf("conflict changed financial facts: periods=%d invoices=%d assignments=%d", periods, invoices, assignments)
			}
		})
	}
}
