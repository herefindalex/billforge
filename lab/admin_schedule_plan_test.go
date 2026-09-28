package lab

import (
	"context"
	"encoding/json"
	"path/filepath"
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
