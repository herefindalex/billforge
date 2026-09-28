package lab

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestAdminCreateQuoteAtomicReceiptAndReplay(t *testing.T) {
	dir := t.TempDir()
	l, err := Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	ctx := context.Background()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{"customer_id":"cust-1","plan_id":"basic","cohort":"default","seats":"0"}`)
	command, replay, err := l.AdminSubmitCommand(ctx, "local-admin", "create-quote-001", "C01", "", payload, "")
	if err != nil || replay || command.Status != "accepted" {
		t.Fatalf("submit: %+v replay=%v err=%v", command, replay, err)
	}
	command, err = l.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "succeeded" {
		t.Fatalf("execute: %+v err=%v", command, err)
	}
	var refs map[string]string
	if err := json.Unmarshal(command.ResultRefs, &refs); err != nil {
		t.Fatal(err)
	}
	if refs["quote_id"] == "" {
		t.Fatal("missing quote reference")
	}
	var quotes, receipts, audit int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM quotes`).Scan(&quotes); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts`).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_audit WHERE command_id=?`, command.ID).Scan(&audit); err != nil {
		t.Fatal(err)
	}
	if quotes != 1 || receipts != 1 || audit != 2 {
		t.Fatalf("facts and evidence: quotes=%d receipts=%d audit=%d", quotes, receipts, audit)
	}
	var auditActor, auditRequest, auditAction, auditCommand, auditResult string
	if err := l.db.QueryRowContext(ctx, `SELECT a.actor_id,c.idempotency_key,a.action_id,a.command_id,COALESCE(a.after_json,'')
		FROM admin_audit a JOIN admin_commands c ON c.id=a.command_id
		WHERE a.command_id=? AND a.reason='succeeded'`, command.ID).
		Scan(&auditActor, &auditRequest, &auditAction, &auditCommand, &auditResult); err != nil {
		t.Fatal(err)
	}
	if auditActor != "local-admin" || auditRequest != "create-quote-001" || auditAction != "C01" || auditCommand != command.ID {
		t.Fatalf("audit references: actor=%q request=%q action=%q command=%q", auditActor, auditRequest, auditAction, auditCommand)
	}
	var auditRefs map[string]string
	if err := json.Unmarshal([]byte(auditResult), &auditRefs); err != nil {
		t.Fatal(err)
	}
	if auditRefs["quote_id"] != refs["quote_id"] {
		t.Fatalf("audit result quote=%q, command result quote=%q", auditRefs["quote_id"], refs["quote_id"])
	}
	again, replay, err := l.AdminSubmitCommand(ctx, "local-admin", "create-quote-001", "C01", "", payload, "")
	if err != nil || !replay || again.ID != command.ID {
		t.Fatalf("replay: %+v replay=%v err=%v", again, replay, err)
	}
	changed := json.RawMessage(`{"customer_id":"cust-2","plan_id":"basic","cohort":"default","seats":"0"}`)
	if _, _, err := l.AdminSubmitCommand(ctx, "local-admin", "create-quote-001", "C01", "", changed, ""); !errors.Is(err, ErrAdminIdempotencyConflict) {
		t.Fatalf("changed payload: %v", err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM quotes`).Scan(&quotes); err != nil {
		t.Fatal(err)
	}
	if quotes != 1 {
		t.Fatalf("replay created %d quotes", quotes)
	}
}

func TestAdminCreateQuoteResumesAfterAdmission(t *testing.T) {
	dir := t.TempDir()
	commerce := filepath.Join(dir, "commerce.db")
	provider := filepath.Join(dir, "provider.db")
	l, err := Open(commerce, provider, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "create-quote-002", "C01", "", json.RawMessage(`{"customer_id":"cust-1","plan_id":"basic","seats":"0"}`), "")
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	l, err = Open(commerce, provider, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	if err := l.AdminResumeAccepted(ctx); err != nil {
		t.Fatal(err)
	}
	if err := l.AdminResumeAccepted(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := l.AdminCommand(ctx, command.ID)
	if err != nil || got.Status != "succeeded" {
		t.Fatalf("resumed: %+v err=%v", got, err)
	}
	var quotes int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM quotes`).Scan(&quotes); err != nil {
		t.Fatal(err)
	}
	if quotes != 1 {
		t.Fatalf("resume created %d quotes", quotes)
	}
}

func TestAdminConcurrentQuoteAdmissionAndReceiptFailureRecovery(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	commerce, provider := filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db")
	l, err := Open(commerce, provider, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{"customer_id":"concurrent-receipt-customer","plan_id":"basic","cohort":"default","seats":"0"}`)
	type admission struct {
		command AdminCommand
		replay  bool
		err     error
	}
	start := make(chan struct{})
	results := make(chan admission, 2)
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			command, replay, err := l.AdminSubmitCommand(ctx, "local-admin", "concurrent-receipt-001", "C01", "", payload, "")
			results <- admission{command, replay, err}
		}()
	}
	close(start)
	first, second := <-results, <-results
	if first.err != nil || second.err != nil || first.command.ID == "" || first.command.ID != second.command.ID || first.replay == second.replay {
		t.Fatalf("concurrent admission: first=%+v second=%+v", first, second)
	}
	if _, err := l.db.ExecContext(ctx, `CREATE TEMP TRIGGER fail_admin_receipt BEFORE INSERT ON admin_command_receipts BEGIN SELECT RAISE(ABORT, 'injected receipt failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := l.AdminExecuteCommand(ctx, first.command.ID); err == nil {
		t.Fatal("receipt failure should abort the domain transaction")
	}
	var quotes, receipts int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM quotes WHERE customer_id=?`, "concurrent-receipt-customer").Scan(&quotes); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, first.command.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if quotes != 0 || receipts != 0 {
		t.Fatalf("receipt failure left partial facts: quotes=%d receipts=%d", quotes, receipts)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	l, err = Open(commerce, provider, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	if err := l.AdminResumeAccepted(ctx); err != nil {
		t.Fatal(err)
	}
	command, err := l.AdminCommand(ctx, first.command.ID)
	if err != nil || command.Status != "succeeded" {
		t.Fatalf("original command not recovered: %+v, %v", command, err)
	}
	replayed, replay, err := l.AdminSubmitCommand(ctx, "local-admin", "concurrent-receipt-001", "C01", "", payload, "")
	if err != nil || !replay || replayed.ID != command.ID {
		t.Fatalf("response-loss replay changed command: %+v replay=%v err=%v", replayed, replay, err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM quotes WHERE customer_id=?`, "concurrent-receipt-customer").Scan(&quotes); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if quotes != 1 || receipts != 1 {
		t.Fatalf("recovered command duplicated or lost facts: quotes=%d receipts=%d", quotes, receipts)
	}
}

func TestAdminAcceptQuotePreviewAndAtomicReceipt(t *testing.T) {
	dir := t.TempDir()
	l, err := Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	ctx := context.Background()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	quote, err := l.CreateQuote(ctx, "cust-1", "basic")
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(AdminAcceptQuotePayload{Fingerprint: quote.Fingerprint})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C02", quote.ID, payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.AdminGetPreview(ctx, "other-admin", preview.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("another actor could read preview: %v", err)
	}
	if _, _, err := l.AdminSubmitCommand(ctx, "other-admin", "other-accept-quote-001", "C02", quote.ID, payload, preview.ID); !errors.Is(err, ErrAdminPreviewStale) {
		t.Fatalf("another actor could submit preview: %v", err)
	}
	var impact map[string]string
	if err := json.Unmarshal(preview.Impact, &impact); err != nil {
		t.Fatal(err)
	}
	if impact["amount_minor"] != "2000" || impact["currency"] != "USD" {
		t.Fatalf("wrong preview impact: %+v", impact)
	}
	command, replay, err := l.AdminSubmitCommand(ctx, "local-admin", "accept-quote-001", "C02", quote.ID, payload, preview.ID)
	if err != nil || replay {
		t.Fatalf("submit: replay=%v err=%v", replay, err)
	}
	command, err = l.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "succeeded" {
		t.Fatalf("execute: %+v err=%v", command, err)
	}
	var refs map[string]string
	if err := json.Unmarshal(command.ResultRefs, &refs); err != nil {
		t.Fatal(err)
	}
	if refs["amount_minor"] != "2000" || refs["invoice_id"] == "" || refs["operation_id"] == "" {
		t.Fatalf("wrong result refs: %+v", refs)
	}
	var subscriptions, receipts, outbox int
	for _, item := range []struct {
		query  string
		target *int
	}{
		{`SELECT COUNT(*) FROM subscriptions`, &subscriptions},
		{`SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, &receipts},
		{`SELECT COUNT(*) FROM outbox WHERE kind='capture'`, &outbox},
	} {
		args := []any{}
		if item.target == &receipts {
			args = append(args, command.ID)
		}
		if err := l.db.QueryRowContext(ctx, item.query, args...).Scan(item.target); err != nil {
			t.Fatal(err)
		}
	}
	if subscriptions != 1 || receipts != 1 || outbox != 1 {
		t.Fatalf("facts and receipt: subscriptions=%d receipts=%d outbox=%d", subscriptions, receipts, outbox)
	}
	again, replay, err := l.AdminSubmitCommand(ctx, "local-admin", "accept-quote-001", "C02", quote.ID, payload, preview.ID)
	if err != nil || !replay || again.ID != command.ID {
		t.Fatalf("replay: %+v replay=%v err=%v", again, replay, err)
	}
	if _, _, err := l.AdminSubmitCommand(ctx, "local-admin", "accept-quote-002", "C02", quote.ID, payload, preview.ID); !errors.Is(err, ErrAdminPreviewStale) {
		t.Fatalf("preview reuse: %v", err)
	}
}

func TestAdminAcceptQuoteRollsBackFinancialFactsWhenReceiptWriteFails(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	commerce, provider := filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db")
	l, err := Open(commerce, provider, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	quote, err := l.CreateQuote(ctx, "receipt-failure-customer", "basic")
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(AdminAcceptQuotePayload{Fingerprint: quote.Fingerprint})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C02", quote.ID, payload)
	if err != nil {
		t.Fatal(err)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "accept-receipt-failure-001", "C02", quote.ID, payload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.db.ExecContext(ctx, `CREATE TEMP TRIGGER fail_admin_receipt BEFORE INSERT ON admin_command_receipts BEGIN SELECT RAISE(ABORT, 'injected receipt failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := l.AdminExecuteCommand(ctx, command.ID); err == nil {
		t.Fatal("receipt insertion failure must abort quote acceptance")
	}
	for _, table := range []string{"subscriptions", "invoices", "invoice_lines", "payment_operations", "outbox", "admin_command_receipts"} {
		var count int
		if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s survived failed receipt: count=%d err=%v", table, count, err)
		}
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	l, err = Open(commerce, provider, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	if err := l.AdminResumeAccepted(ctx); err != nil {
		t.Fatal(err)
	}
	if err := l.AdminResumeAccepted(ctx); err != nil {
		t.Fatal(err)
	}
	command, err = l.AdminCommand(ctx, command.ID)
	if err != nil || command.Status != "succeeded" {
		t.Fatalf("acceptance did not recover: %+v, %v", command, err)
	}
	replayed, replay, err := l.AdminSubmitCommand(ctx, "local-admin", "accept-receipt-failure-001", "C02", quote.ID, payload, preview.ID)
	if err != nil || !replay || replayed.ID != command.ID {
		t.Fatalf("replay changed accepted command: %+v replay=%v err=%v", replayed, replay, err)
	}
	for _, table := range []string{"subscriptions", "invoices", "payment_operations", "outbox", "admin_command_receipts"} {
		var count int
		if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&count); err != nil || count != 1 {
			t.Fatalf("%s after recovery: count=%d err=%v", table, count, err)
		}
	}
	if got := captureCount(t, l); got != 0 {
		t.Fatalf("local recovery unexpectedly called provider: captures=%d", got)
	}
}

func TestAdminPauseMigrationAtomicReceipt(t *testing.T) {
	dir := t.TempDir()
	l, err := Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	ctx := context.Background()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := l.db.ExecContext(ctx, `INSERT INTO price_migrations(id,cohort,target_price_version_id,payload_hash,status,created_at) VALUES('batch-1','default','pro-v1','fixture','active',0)`); err != nil {
		t.Fatal(err)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "pause-batch-001", "C23", "batch-1", json.RawMessage(`{}`), "")
	if err != nil {
		t.Fatal(err)
	}
	command, err = l.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "succeeded" {
		t.Fatalf("pause: %+v err=%v", command, err)
	}
	var status string
	if err := l.db.QueryRowContext(ctx, `SELECT status FROM price_migrations WHERE id='batch-1'`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "paused" {
		t.Fatalf("migration is %s", status)
	}
	var receipts int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if receipts != 1 {
		t.Fatalf("receipt count %d", receipts)
	}
	replayed, same, err := l.AdminSubmitCommand(ctx, "local-admin", "pause-batch-001", "C23", "batch-1", json.RawMessage(`{}`), "")
	if err != nil || !same || replayed.ID != command.ID {
		t.Fatalf("replay: %+v same=%v err=%v", replayed, same, err)
	}
}

func TestAdminRecordUsageAtomicReceiptAndExactQuantity(t *testing.T) {
	l, clock := openChangingLab(t)
	ctx := context.Background()
	receipt := paidProSubscription(t, l, "usage-admin-tenant")
	*clock = time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(AdminRecordUsagePayload{
		Source: "worker", EventID: "admin-event-1", SubscriptionID: receipt.SubscriptionID,
		MeterID: "tasks", OccurredAt: "2026-09-29T12:00:00Z", Quantity: "20003",
	})
	if err != nil {
		t.Fatal(err)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "record-usage-001", "C26", "", payload, "")
	if err != nil {
		t.Fatal(err)
	}
	command, err = l.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "succeeded" {
		t.Fatalf("usage command: %+v err=%v", command, err)
	}
	var refs map[string]string
	if err := json.Unmarshal(command.ResultRefs, &refs); err != nil {
		t.Fatal(err)
	}
	if refs["quantity"] != "20003" || refs["period_index"] != "0" {
		t.Fatalf("usage refs: %+v", refs)
	}
	var events, receipts int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_events WHERE source='worker' AND event_id='admin-event-1'`).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if events != 1 || receipts != 1 {
		t.Fatalf("facts and receipt: events=%d receipts=%d", events, receipts)
	}
	if _, replay, err := l.AdminSubmitCommand(ctx, "local-admin", "record-usage-001", "C26", "", payload, ""); err != nil || !replay {
		t.Fatalf("usage replay=%v err=%v", replay, err)
	}
	changed, _ := json.Marshal(AdminRecordUsagePayload{Source: "worker", EventID: "admin-event-1", SubscriptionID: receipt.SubscriptionID, MeterID: "tasks", OccurredAt: "2026-09-29T12:00:00Z", Quantity: "20004"})
	if _, _, err := l.AdminSubmitCommand(ctx, "local-admin", "record-usage-001", "C26", "", changed, ""); !errors.Is(err, ErrAdminIdempotencyConflict) {
		t.Fatalf("changed quantity: %v", err)
	}
}

func TestAdminScheduleCancelPreviewAndReceipt(t *testing.T) {
	l, _ := openChangingLab(t)
	ctx := context.Background()
	receipt := paidBasicSubscription(t, l, "cancel-admin-customer")
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	var revision int64
	if err := l.db.QueryRowContext(ctx, `SELECT revision FROM subscriptions WHERE id=?`, receipt.SubscriptionID).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(AdminRevisionPayload{Revision: strconv.FormatInt(revision, 10)})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C05", receipt.SubscriptionID, payload)
	if err != nil {
		t.Fatal(err)
	}
	var impact map[string]string
	if err := json.Unmarshal(preview.Impact, &impact); err != nil {
		t.Fatal(err)
	}
	if impact["effective_at"] == "" {
		t.Fatalf("missing cancellation date: %+v", impact)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "schedule-cancel-001", "C05", receipt.SubscriptionID, payload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	command, err = l.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "succeeded" {
		t.Fatalf("cancel command: %+v err=%v", command, err)
	}
	var schedules, receipts int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM subscription_schedules WHERE subscription_id=? AND kind='cancel'`, receipt.SubscriptionID).Scan(&schedules); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if schedules != 1 || receipts != 1 {
		t.Fatalf("schedule=%d receipt=%d", schedules, receipts)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT revision FROM subscriptions WHERE id=?`, receipt.SubscriptionID).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	resumePayload, err := json.Marshal(AdminRevisionPayload{Revision: strconv.FormatInt(revision, 10)})
	if err != nil {
		t.Fatal(err)
	}
	resumePreview, err := l.AdminCreatePreview(ctx, "local-admin", "C06", receipt.SubscriptionID, resumePayload)
	if err != nil {
		t.Fatal(err)
	}
	resume, _, err := l.AdminSubmitCommand(ctx, "local-admin", "resume-cancel-001", "C06", receipt.SubscriptionID, resumePayload, resumePreview.ID)
	if err != nil {
		t.Fatal(err)
	}
	resume, err = l.AdminExecuteCommand(ctx, resume.ID)
	if err != nil || resume.Status != "succeeded" {
		t.Fatalf("resume command: %+v err=%v", resume, err)
	}
	var scheduleStatus string
	if err := l.db.QueryRowContext(ctx, `SELECT status FROM subscription_schedules WHERE subscription_id=? AND kind='cancel'`, receipt.SubscriptionID).Scan(&scheduleStatus); err != nil {
		t.Fatal(err)
	}
	if scheduleStatus != "cancelled" {
		t.Fatalf("schedule remained %s", scheduleStatus)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, resume.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if receipts != 1 {
		t.Fatalf("resume receipt=%d", receipts)
	}
}
