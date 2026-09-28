package lab

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"testing"
)

func TestStoppedCommerceWriterRejectsCancelPreviewAndAcceptedIntent(t *testing.T) {
	l, _ := openChangingLab(t)
	ctx := context.Background()
	const customerID = "stopped-writer-customer"
	const legacyID = "stopped-writer-legacy"
	paid := paidBasicSubscription(t, l, customerID)
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := l.LinkLegacyAccount(ctx, AccountLink{LegacyAccountID: legacyID, CustomerID: customerID, BeneficiaryID: customerID, Cohort: "default", HasHistory: true}); err != nil {
		t.Fatal(err)
	}
	// The writer cutover is fixture setup; this test isolates stop after a
	// previously valid preview and before its financial mutation.
	if _, err := l.db.ExecContext(ctx, `UPDATE account_links SET read_owner='commerce',writer_owner='commerce' WHERE legacy_account_id=?`, legacyID); err != nil {
		t.Fatal(err)
	}
	var revision int64
	if err := l.db.QueryRowContext(ctx, `SELECT revision FROM subscriptions WHERE id=?`, paid.SubscriptionID).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(AdminRevisionPayload{Revision: strconv.FormatInt(revision, 10)})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C05", paid.SubscriptionID, payload)
	if err != nil {
		t.Fatalf("preview before stop: %v", err)
	}
	if _, err := l.StopAccountMigration(ctx, legacyID, "review required"); err != nil {
		t.Fatal(err)
	}
	if _, err := l.AdminCreatePreview(ctx, "local-admin", "C05", paid.SubscriptionID, payload); !errors.Is(err, ErrAccountMigrationStopped) {
		t.Fatalf("preview after stop: %v", err)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "stopped-writer-cancel", "C05", paid.SubscriptionID, payload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	command, err = l.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "failed" || command.ErrorCode != "ACCOUNT_MIGRATION_STOPPED" {
		t.Fatalf("accepted intent after stop: %+v, %v", command, err)
	}
	replayed, replay, err := l.AdminSubmitCommand(ctx, "local-admin", "stopped-writer-cancel", "C05", paid.SubscriptionID, payload, preview.ID)
	if err != nil || !replay || replayed.ID != command.ID {
		t.Fatalf("replay: %+v, replay=%v, %v", replayed, replay, err)
	}
	var schedules int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM subscription_schedules WHERE subscription_id=? AND kind='cancel'`, paid.SubscriptionID).Scan(&schedules); err != nil {
		t.Fatal(err)
	}
	if schedules != 0 {
		t.Fatalf("stopped writer created %d cancellation schedules", schedules)
	}
	var receipts int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if receipts != 0 {
		t.Fatalf("stopped writer created %d success receipts", receipts)
	}
}

func TestStoppedCommerceWriterRejectsAcceptQuotePreviewAndAcceptedIntent(t *testing.T) {
	l, _ := openChangingLab(t)
	ctx := context.Background()
	const customerID = "stopped-accept-customer"
	const legacyID = "stopped-accept-legacy"
	quote, err := l.CreateQuote(ctx, customerID, "basic")
	if err != nil {
		t.Fatal(err)
	}
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := l.LinkLegacyAccount(ctx, AccountLink{LegacyAccountID: legacyID, CustomerID: customerID, BeneficiaryID: customerID, Cohort: "default"}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.db.ExecContext(ctx, `UPDATE account_links SET read_owner='commerce',writer_owner='commerce' WHERE legacy_account_id=?`, legacyID); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(AdminAcceptQuotePayload{Fingerprint: quote.Fingerprint})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C02", quote.ID, payload)
	if err != nil {
		t.Fatalf("preview before stop: %v", err)
	}
	if _, err := l.StopAccountMigration(ctx, legacyID, "review required"); err != nil {
		t.Fatal(err)
	}
	if _, err := l.AdminCreatePreview(ctx, "local-admin", "C02", quote.ID, payload); !errors.Is(err, ErrAccountMigrationStopped) {
		t.Fatalf("preview after stop: %v", err)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "stopped-accept-quote", "C02", quote.ID, payload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	command, err = l.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "failed" || command.ErrorCode != "ACCOUNT_MIGRATION_STOPPED" {
		t.Fatalf("accepted intent after stop: %+v err=%v", command, err)
	}
	replayed, replay, err := l.AdminSubmitCommand(ctx, "local-admin", "stopped-accept-quote", "C02", quote.ID, payload, preview.ID)
	if err != nil || !replay || replayed.ID != command.ID {
		t.Fatalf("same key must recover original command: %+v replay=%v err=%v", replayed, replay, err)
	}
	var subscriptions, receipts, previews int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM subscriptions WHERE quote_id=?`, quote.ID).Scan(&subscriptions); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_previews WHERE action_id='C02' AND target_id=?`, quote.ID).Scan(&previews); err != nil {
		t.Fatal(err)
	}
	if subscriptions != 0 || receipts != 0 || previews != 1 {
		t.Fatalf("subscriptions=%d receipts=%d previews=%d", subscriptions, receipts, previews)
	}
}

func TestStoppedCommerceWriterRejectsResumePreviewAndAcceptedIntent(t *testing.T) {
	l, _ := openChangingLab(t)
	ctx := context.Background()
	const customerID = "stopped-resume-customer"
	const legacyID = "stopped-resume-legacy"
	paid := paidBasicSubscription(t, l, customerID)
	var revision int64
	if err := l.db.QueryRowContext(ctx, `SELECT revision FROM subscriptions WHERE id=?`, paid.SubscriptionID).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	schedule, err := l.ScheduleCancel(ctx, paid.SubscriptionID, revision, "stopped-resume-schedule")
	if err != nil {
		t.Fatal(err)
	}
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := l.LinkLegacyAccount(ctx, AccountLink{LegacyAccountID: legacyID, CustomerID: customerID, BeneficiaryID: customerID, Cohort: "default", HasHistory: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.db.ExecContext(ctx, `UPDATE account_links SET read_owner='commerce',writer_owner='commerce' WHERE legacy_account_id=?`, legacyID); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(AdminRevisionPayload{Revision: strconv.FormatInt(schedule.Revision, 10)})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C06", paid.SubscriptionID, payload)
	if err != nil {
		t.Fatalf("preview before stop: %v", err)
	}
	if _, err := l.StopAccountMigration(ctx, legacyID, "review required"); err != nil {
		t.Fatal(err)
	}
	if _, err := l.AdminCreatePreview(ctx, "local-admin", "C06", paid.SubscriptionID, payload); !errors.Is(err, ErrAccountMigrationStopped) {
		t.Fatalf("preview after stop: %v", err)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "stopped-resume-cancel", "C06", paid.SubscriptionID, payload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	command, err = l.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "failed" || command.ErrorCode != "ACCOUNT_MIGRATION_STOPPED" {
		t.Fatalf("accepted intent after stop: %+v err=%v", command, err)
	}
	replayed, replay, err := l.AdminSubmitCommand(ctx, "local-admin", "stopped-resume-cancel", "C06", paid.SubscriptionID, payload, preview.ID)
	if err != nil || !replay || replayed.ID != command.ID {
		t.Fatalf("same key must recover original command: %+v replay=%v err=%v", replayed, replay, err)
	}
	var status string
	if err := l.db.QueryRowContext(ctx, `SELECT status FROM subscription_schedules WHERE id=?`, schedule.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	var receipts, previews int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_previews WHERE action_id='C06' AND target_id=?`, paid.SubscriptionID).Scan(&previews); err != nil {
		t.Fatal(err)
	}
	if status != "scheduled" || receipts != 0 || previews != 1 {
		t.Fatalf("schedule=%s receipts=%d previews=%d", status, receipts, previews)
	}
}
