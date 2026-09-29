package lab

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestAdminChangeQuoteAndScheduledPlanAtomic(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	now := time.Now().UTC().Truncate(time.Second)
	l, err := Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	sub := paidBasicSubscription(t, l, "admin-change-customer")
	create, _, err := l.AdminSubmitCommand(ctx, "local-admin", "change-quote-001", "C01", "", json.RawMessage(`{"customer_id":"admin-change-customer","plan_id":"pro","cohort":"default","seats":"5","change_subscription_id":"`+sub.SubscriptionID+`","mode":"next_period","revision":"1"}`), "")
	if err != nil {
		t.Fatal(err)
	}
	create, err = l.AdminExecuteCommand(ctx, create.ID)
	if err != nil || create.Status != "succeeded" {
		t.Fatalf("create: %+v %v", create, err)
	}
	var quoteRefs map[string]string
	if err := json.Unmarshal(create.ResultRefs, &quoteRefs); err != nil {
		t.Fatal(err)
	}
	if quoteRefs["quote_id"] == "" || quoteRefs["binding_fingerprint"] == "" {
		t.Fatalf("missing quote binding: %+v", quoteRefs)
	}
	payload, _ := json.Marshal(AdminChangePlanPayload{QuoteID: quoteRefs["quote_id"], Fingerprint: quoteRefs["binding_fingerprint"], Revision: "1"})
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C03", sub.SubscriptionID, payload)
	if err != nil {
		t.Fatal(err)
	}
	var impact map[string]string
	if err := json.Unmarshal(preview.Impact, &impact); err != nil {
		t.Fatal(err)
	}
	if impact["price_version_id"] != "pro-v1" || impact["seats"] != "5" {
		t.Fatalf("unexpected impact: %+v", impact)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "schedule-plan-001", "C03", sub.SubscriptionID, payload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	command, err = l.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "succeeded" {
		t.Fatalf("execute: %+v %v", command, err)
	}
	var refs map[string]string
	if err := json.Unmarshal(command.ResultRefs, &refs); err != nil {
		t.Fatal(err)
	}
	if refs["schedule_id"] == "" || refs["revision"] != "2" {
		t.Fatalf("unexpected result: %+v", refs)
	}
	var schedules, receipts int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM subscription_schedules WHERE id=?`, refs["schedule_id"]).Scan(&schedules); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if schedules != 1 || receipts != 1 {
		t.Fatalf("business fact and receipt differ: schedules=%d receipts=%d", schedules, receipts)
	}
	replayed, replay, err := l.AdminSubmitCommand(ctx, "local-admin", "schedule-plan-001", "C03", sub.SubscriptionID, payload, preview.ID)
	if err != nil || !replay || replayed.ID != command.ID {
		t.Fatalf("replay: %+v %v %v", replayed, replay, err)
	}
}

func TestAdminScheduledPlanReceiptFailureRollsBackAndRecoversOriginalCommand(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	commerce, provider := filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db")
	now := time.Now().UTC().Truncate(time.Second)
	open := func() *Lab {
		t.Helper()
		l, err := Open(commerce, provider, func() time.Time { return now })
		if err != nil {
			t.Fatal(err)
		}
		if err := l.InitAdmin(ctx); err != nil {
			t.Fatal(err)
		}
		return l
	}
	l := open()
	sub := paidBasicSubscription(t, l, "schedule-receipt-failure")
	var revisionBefore int64
	if err := l.db.QueryRowContext(ctx, `SELECT revision FROM subscriptions WHERE id=?`, sub.SubscriptionID).Scan(&revisionBefore); err != nil {
		t.Fatal(err)
	}
	quote, err := l.CreateQuoteForCohort(ctx, "schedule-receipt-failure", "pro", "default", 5)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := l.BindChangeQuote(ctx, quote.ID, sub.SubscriptionID, "next_period", revisionBefore)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(AdminChangePlanPayload{
		QuoteID: quote.ID, Fingerprint: binding.Fingerprint, Revision: strconv.FormatInt(revisionBefore, 10),
	})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C03", sub.SubscriptionID, payload)
	if err != nil {
		t.Fatal(err)
	}
	command, replay, err := l.AdminSubmitCommand(ctx, "local-admin", "schedule-receipt-failure-key", "C03", sub.SubscriptionID, payload, preview.ID)
	if err != nil || replay {
		t.Fatalf("submit schedule: command=%+v replay=%t err=%v", command, replay, err)
	}

	readCount := func(query string, args ...any) int {
		t.Helper()
		var count int
		if err := l.db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}
	schedulesBefore := readCount(`SELECT COUNT(*) FROM subscription_schedules WHERE subscription_id=? AND kind='change'`, sub.SubscriptionID)
	receiptsBefore := readCount(`SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID)
	operationsBefore := readCount(`SELECT COUNT(*) FROM payment_operations`)
	capturesBefore := captureCount(t, l)

	if _, err := l.db.ExecContext(ctx, `CREATE TEMP TRIGGER fail_schedule_receipt BEFORE INSERT ON admin_command_receipts BEGIN SELECT RAISE(ABORT, 'injected receipt failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := l.AdminExecuteCommand(ctx, command.ID); err == nil {
		t.Fatal("receipt failure unexpectedly completed scheduled change")
	}
	if held, err := l.AdminCommand(ctx, command.ID); err != nil || held.Status != "accepted" {
		t.Fatalf("failed receipt did not retain original command: %+v err=%v", held, err)
	}
	if got := readCount(`SELECT COUNT(*) FROM subscription_schedules WHERE subscription_id=? AND kind='change'`, sub.SubscriptionID); got != schedulesBefore {
		t.Fatalf("schedule persisted after failed receipt: %d", got)
	}
	if got := readCount(`SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID); got != receiptsBefore {
		t.Fatalf("receipt persisted after failed receipt: %d", got)
	}
	var revision int64
	if err := l.db.QueryRowContext(ctx, `SELECT revision FROM subscriptions WHERE id=?`, sub.SubscriptionID).Scan(&revision); err != nil || revision != revisionBefore {
		t.Fatalf("subscription revision after failed receipt: %d err=%v", revision, err)
	}
	if got := readCount(`SELECT COUNT(*) FROM payment_operations`); got != operationsBefore {
		t.Fatalf("payment obligation after failed receipt: %d", got)
	}
	if got := captureCount(t, l); got != capturesBefore {
		t.Fatalf("provider capture after failed receipt: %d", got)
	}

	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	l = open()
	defer l.Close()
	for i := 0; i < 2; i++ {
		if err := l.AdminResumeAccepted(ctx); err != nil {
			t.Fatal(err)
		}
	}
	command, err = l.AdminCommand(ctx, command.ID)
	if err != nil || command.Status != "succeeded" {
		t.Fatalf("recovered schedule: %+v err=%v", command, err)
	}
	if got := readCount(`SELECT COUNT(*) FROM subscription_schedules WHERE subscription_id=? AND kind='change'`, sub.SubscriptionID); got != schedulesBefore+1 {
		t.Fatalf("schedules after recovery: %d", got)
	}
	if got := readCount(`SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID); got != receiptsBefore+1 {
		t.Fatalf("receipts after recovery: %d", got)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT revision FROM subscriptions WHERE id=?`, sub.SubscriptionID).Scan(&revision); err != nil || revision != revisionBefore+1 {
		t.Fatalf("subscription revision after recovery: %d err=%v", revision, err)
	}
	if got := readCount(`SELECT COUNT(*) FROM payment_operations`); got != operationsBefore {
		t.Fatalf("schedule created a payment obligation: %d", got)
	}
	if got := captureCount(t, l); got != capturesBefore {
		t.Fatalf("schedule dispatched provider capture: %d", got)
	}
	replayed, replay, err := l.AdminSubmitCommand(ctx, "local-admin", "schedule-receipt-failure-key", "C03", sub.SubscriptionID, payload, preview.ID)
	if err != nil || !replay || replayed.ID != command.ID || replayed.Status != "succeeded" {
		t.Fatalf("replay changed recovered schedule: %+v replay=%t err=%v", replayed, replay, err)
	}
	changed, err := json.Marshal(AdminChangePlanPayload{QuoteID: quote.ID, Fingerprint: binding.Fingerprint, Revision: strconv.FormatInt(revisionBefore+1, 10)})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := l.AdminSubmitCommand(ctx, "local-admin", "schedule-receipt-failure-key", "C03", sub.SubscriptionID, changed, preview.ID); !errors.Is(err, ErrAdminIdempotencyConflict) {
		t.Fatalf("changed payload reused original key: %v", err)
	}
}
