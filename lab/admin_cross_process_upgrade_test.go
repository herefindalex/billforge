package lab

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestAdminCompetingImmediateUpgradesAcrossProcesses(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	dir := t.TempDir()
	commercePath := filepath.Join(dir, "commerce.db")
	providerPath := filepath.Join(dir, "provider.db")
	now := time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)
	l, err := Open(commercePath, providerPath, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	paid := paidBasicSubscription(t, l, "cross-process-upgrade")
	quote, err := l.CreateQuoteForCohort(ctx, "cross-process-upgrade", "pro", "default", 5)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := l.BindChangeQuote(ctx, quote.ID, paid.SubscriptionID, "immediate", 1)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(AdminChangePlanPayload{QuoteID: quote.ID, Fingerprint: binding.Fingerprint, Revision: "1"})
	if err != nil {
		t.Fatal(err)
	}
	create := func(key string) AdminCommand {
		t.Helper()
		preview, err := l.AdminCreatePreview(ctx, "local-admin", "C04", paid.SubscriptionID, payload)
		if err != nil {
			t.Fatal(err)
		}
		command, _, err := l.AdminSubmitCommand(ctx, "local-admin", key, "C04", paid.SubscriptionID, payload, preview.ID)
		if err != nil {
			t.Fatal(err)
		}
		return command
	}
	a := create("cross-process-upgrade-a")
	b := create("cross-process-upgrade-b")
	executeAdminCommandsAcrossProcesses(t, ctx, commercePath, providerPath, now, a.ID, b.ID)
	var succeeded, stale, receipts, changes, invoices, operations, outbox, revision int
	for _, check := range []struct {
		query string
		args  []any
		out   *int
	}{
		{`SELECT COUNT(*) FROM admin_commands WHERE id IN (?,?) AND status='succeeded'`, []any{a.ID, b.ID}, &succeeded},
		{`SELECT COUNT(*) FROM admin_commands WHERE id IN (?,?) AND status='failed' AND error_code='PREVIEW_STALE'`, []any{a.ID, b.ID}, &stale},
		{`SELECT COUNT(*) FROM admin_command_receipts WHERE command_id IN (?,?)`, []any{a.ID, b.ID}, &receipts},
		{`SELECT COUNT(*) FROM immediate_changes WHERE subscription_id=?`, []any{paid.SubscriptionID}, &changes},
		{`SELECT COUNT(*) FROM supplemental_invoices WHERE subscription_id=?`, []any{paid.SubscriptionID}, &invoices},
		{`SELECT COUNT(*) FROM payment_operations WHERE invoice_id IN (SELECT invoice_id FROM immediate_changes WHERE subscription_id=?)`, []any{paid.SubscriptionID}, &operations},
		{`SELECT COUNT(*) FROM outbox WHERE kind='capture' AND object_id IN (SELECT operation_id FROM immediate_changes WHERE subscription_id=?)`, []any{paid.SubscriptionID}, &outbox},
		{`SELECT revision FROM subscriptions WHERE id=?`, []any{paid.SubscriptionID}, &revision},
	} {
		if err := l.db.QueryRowContext(ctx, check.query, check.args...).Scan(check.out); err != nil {
			t.Fatal(err)
		}
	}
	if succeeded != 1 || stale != 1 || receipts != 1 || changes != 1 || invoices != 1 || operations != 1 || outbox != 1 || revision != 2 {
		t.Fatalf("cross-process upgrade facts: succeeded=%d stale=%d receipts=%d changes=%d invoices=%d operations=%d outbox=%d revision=%d", succeeded, stale, receipts, changes, invoices, operations, outbox, revision)
	}
	for _, attempt := range []struct {
		key string
		id  string
	}{
		{key: "cross-process-upgrade-a", id: a.ID},
		{key: "cross-process-upgrade-b", id: b.ID},
	} {
		var previewID string
		if err := l.db.QueryRowContext(ctx, `SELECT preview_id FROM admin_commands WHERE id=?`, attempt.id).Scan(&previewID); err != nil {
			t.Fatal(err)
		}
		replayed, replay, err := l.AdminSubmitCommand(ctx, "local-admin", attempt.key, "C04", paid.SubscriptionID, payload, previewID)
		if err != nil || !replay || replayed.ID != attempt.id {
			t.Fatalf("cross-process replay changed command: %+v replay=%t err=%v", replayed, replay, err)
		}
	}
	var changesAfter, receiptsAfter int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM immediate_changes WHERE subscription_id=?`, paid.SubscriptionID).Scan(&changesAfter); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id IN (?,?)`, a.ID, b.ID).Scan(&receiptsAfter); err != nil {
		t.Fatal(err)
	}
	if changesAfter != 1 || receiptsAfter != 1 {
		t.Fatalf("replay duplicated upgrade: changes=%d receipts=%d", changesAfter, receiptsAfter)
	}
}
