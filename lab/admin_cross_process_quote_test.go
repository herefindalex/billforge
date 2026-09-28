package lab

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestAdminCompetingQuoteAcceptancesAcrossProcesses(t *testing.T) {
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
	quote, err := l.CreateQuoteForCohort(ctx, "cross-process-quote", "pro", "default", 5)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(AdminAcceptQuotePayload{Fingerprint: quote.Fingerprint})
	if err != nil {
		t.Fatal(err)
	}
	create := func(key string) AdminCommand {
		t.Helper()
		preview, err := l.AdminCreatePreview(ctx, "local-admin", "C02", quote.ID, payload)
		if err != nil {
			t.Fatal(err)
		}
		command, _, err := l.AdminSubmitCommand(ctx, "local-admin", key, "C02", quote.ID, payload, preview.ID)
		if err != nil {
			t.Fatal(err)
		}
		return command
	}
	a := create("cross-process-quote-a")
	b := create("cross-process-quote-b")
	executeAdminCommandsAcrossProcesses(t, ctx, commercePath, providerPath, now, a.ID, b.ID)

	var succeeded, stale, receipts, subscriptions, periods, invoices, operations, outbox, amount int
	for _, check := range []struct {
		query string
		args  []any
		out   *int
	}{
		{`SELECT COUNT(*) FROM admin_commands WHERE id IN (?,?) AND status='succeeded'`, []any{a.ID, b.ID}, &succeeded},
		{`SELECT COUNT(*) FROM admin_commands WHERE id IN (?,?) AND status='failed' AND error_code='PREVIEW_STALE'`, []any{a.ID, b.ID}, &stale},
		{`SELECT COUNT(*) FROM admin_command_receipts WHERE command_id IN (?,?)`, []any{a.ID, b.ID}, &receipts},
		{`SELECT COUNT(*) FROM subscriptions WHERE quote_id=?`, []any{quote.ID}, &subscriptions},
		{`SELECT COUNT(*) FROM billing_periods WHERE subscription_id IN (SELECT id FROM subscriptions WHERE quote_id=?)`, []any{quote.ID}, &periods},
		{`SELECT COUNT(*) FROM invoices WHERE subscription_id IN (SELECT id FROM subscriptions WHERE quote_id=?)`, []any{quote.ID}, &invoices},
		{`SELECT COUNT(*) FROM payment_operations WHERE invoice_id IN (SELECT i.id FROM invoices i JOIN subscriptions s ON s.id=i.subscription_id WHERE s.quote_id=?)`, []any{quote.ID}, &operations},
		{`SELECT COUNT(*) FROM outbox WHERE kind='capture' AND object_id IN (SELECT o.id FROM payment_operations o JOIN invoices i ON i.id=o.invoice_id JOIN subscriptions s ON s.id=i.subscription_id WHERE s.quote_id=?)`, []any{quote.ID}, &outbox},
		{`SELECT total_minor FROM invoices WHERE subscription_id IN (SELECT id FROM subscriptions WHERE quote_id=?)`, []any{quote.ID}, &amount},
	} {
		if err := l.db.QueryRowContext(ctx, check.query, check.args...).Scan(check.out); err != nil {
			t.Fatal(err)
		}
	}
	if succeeded != 1 || stale != 1 || receipts != 1 || subscriptions != 1 || periods != 1 || invoices != 1 || operations != 1 || outbox != 1 || int64(amount) != quote.AmountMinor {
		t.Fatalf("cross-process acceptance facts: succeeded=%d stale=%d receipts=%d subscriptions=%d periods=%d invoices=%d operations=%d outbox=%d amount=%d", succeeded, stale, receipts, subscriptions, periods, invoices, operations, outbox, amount)
	}
	for _, attempt := range []struct{ key, id string }{
		{key: "cross-process-quote-a", id: a.ID},
		{key: "cross-process-quote-b", id: b.ID},
	} {
		var previewID string
		if err := l.db.QueryRowContext(ctx, `SELECT preview_id FROM admin_commands WHERE id=?`, attempt.id).Scan(&previewID); err != nil {
			t.Fatal(err)
		}
		replayed, replay, err := l.AdminSubmitCommand(ctx, "local-admin", attempt.key, "C02", quote.ID, payload, previewID)
		if err != nil || !replay || replayed.ID != attempt.id {
			t.Fatalf("cross-process acceptance replay changed command: %+v replay=%t err=%v", replayed, replay, err)
		}
	}
	var subscriptionsAfter, receiptsAfter int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM subscriptions WHERE quote_id=?`, quote.ID).Scan(&subscriptionsAfter); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id IN (?,?)`, a.ID, b.ID).Scan(&receiptsAfter); err != nil {
		t.Fatal(err)
	}
	if subscriptionsAfter != 1 || receiptsAfter != 1 {
		t.Fatalf("replay duplicated acceptance: subscriptions=%d receipts=%d", subscriptionsAfter, receiptsAfter)
	}
}
