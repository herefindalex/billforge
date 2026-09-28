package lab

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestAdminAITokenMeteredSKUUsesGenericFinancialPath(t *testing.T) {
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
	runAdmin := func(action, target, key string, payload json.RawMessage, previewRequired bool) AdminCommand {
		t.Helper()
		previewID := ""
		if previewRequired {
			preview, err := admin.AdminCreatePreview(ctx, "local-admin", action, target, payload)
			if err != nil {
				t.Fatalf("%s preview: %v", action, err)
			}
			previewID = preview.ID
		}
		command, _, err := admin.AdminSubmitCommand(ctx, "local-admin", key, action, target, payload, previewID)
		if err != nil {
			t.Fatalf("%s submit: %v", action, err)
		}
		command, err = admin.AdminExecuteCommand(ctx, command.ID)
		if err != nil || command.Status != "succeeded" {
			t.Fatalf("%s execute: %+v err=%v", action, command, err)
		}
		return command
	}
	marshal := func(value any) json.RawMessage {
		t.Helper()
		payload, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return payload
	}
	refs := func(command AdminCommand) map[string]string {
		t.Helper()
		var result map[string]string
		if err := json.Unmarshal(command.ResultRefs, &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	meter := MeterSpec{ID: "ai_tokens", Source: "ai_gateway", Unit: "token", SchemaVersion: 1}
	if err := domain.RegisterMeter(ctx, meter); err != nil {
		t.Fatal(err)
	}
	runAdmin("C19", "", "ai-parity-register-meter", marshal(adminMeterPayload{
		ID: meter.ID, Source: meter.Source, Unit: meter.Unit, SchemaVersion: "1",
	}), true)
	spec := MeteredPriceSpec{
		ID: "ai_v1", PlanID: "ai", Version: 1, FixedMinor: 3000, MeterID: "ai_tokens",
		IncludedQuantity: 100, UsageRateNum: 1, UsageRateDen: 5, EffectiveFrom: fixedNow,
	}
	if _, err := domain.PublishMeteredPrice(ctx, spec); err != nil {
		t.Fatal(err)
	}
	runAdmin("C20", "", "ai-parity-publish-price", marshal(adminMeteredPricePayload{
		ID: spec.ID, PlanID: spec.PlanID, Version: "1", FixedMinor: "3000", SeatMinor: "0",
		MeterID: spec.MeterID, IncludedQuantity: "100", UsageRateNum: "1", UsageRateDen: "5",
		EffectiveFrom: fixedNow.Format(time.RFC3339Nano),
	}), true)
	if err := domain.SelectCatalogPrice(ctx, "ai", "default", fixedNow, spec.ID); err != nil {
		t.Fatal(err)
	}
	runAdmin("C21", "", "ai-parity-select-price", marshal(adminSelectionPayload{
		PlanID: "ai", Cohort: "default", EffectiveAt: fixedNow.Format(time.RFC3339Nano), PriceVersionID: spec.ID,
	}), true)
	domainQuote, err := domain.CreateQuote(ctx, "ai-parity-customer", "ai")
	if err != nil || domainQuote.AmountMinor != 3000 || domainQuote.PriceVersionID != spec.ID {
		t.Fatalf("domain AI quote: %+v err=%v", domainQuote, err)
	}
	domainReceipt, err := domain.AcceptQuote(ctx, domainQuote.ID, domainQuote.Fingerprint, "ai-parity-domain-accept")
	if err != nil {
		t.Fatal(err)
	}
	adminQuote := refs(runAdmin("C01", "", "ai-parity-quote", marshal(AdminCreateQuotePayload{
		CustomerID: "ai-parity-customer", PlanID: "ai", Cohort: "default", Seats: "0",
	}), false))
	var fingerprint string
	if err := admin.db.QueryRowContext(ctx, `SELECT fingerprint FROM quotes WHERE id=?`, adminQuote["quote_id"]).Scan(&fingerprint); err != nil {
		t.Fatal(err)
	}
	adminAccepted := refs(runAdmin("C02", adminQuote["quote_id"], "ai-parity-accept", marshal(AdminAcceptQuotePayload{Fingerprint: fingerprint}), true))
	domainSub, adminSub := domainReceipt.SubscriptionID, adminAccepted["subscription_id"]
	for _, l := range []*Lab{domain, admin} {
		if _, err := l.DispatchNext(ctx, ""); err != nil {
			t.Fatal(err)
		}
		if err := l.RebuildEntitlements(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if d, a := loadPurchaseFacts(t, domain, domainSub), loadPurchaseFacts(t, admin, adminSub); !reflect.DeepEqual(d, a) || a.PriceVersionID != "ai_v1" || a.InvoiceMinor != 3000 || a.PaymentStatus != "succeeded" {
		t.Fatalf("AI purchase differs: domain=%+v admin=%+v", d, a)
	}
	now = time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	eventAt := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	if _, err := domain.RecordUsage(ctx, "ai_gateway", "ai-parity-event", domainSub, "ai_tokens", 105, eventAt); err != nil {
		t.Fatal(err)
	}
	runAdmin("C26", "", "ai-parity-record-usage", marshal(AdminRecordUsagePayload{
		Source: "ai_gateway", EventID: "ai-parity-event", SubscriptionID: adminSub,
		MeterID: "ai_tokens", OccurredAt: eventAt.Format(time.RFC3339Nano), Quantity: "105",
	}), false)
	now = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	domainRating, err := domain.CloseUsagePeriod(ctx, domainSub, 0, now)
	if err != nil || domainRating.Quantity != 105 || domainRating.IncludedQuantity != 100 || domainRating.OverageQuantity != 5 || domainRating.RoundedMinor != 1 {
		t.Fatalf("domain AI rating: %+v err=%v", domainRating, err)
	}
	runAdmin("C28", adminSub, "ai-parity-close-usage", json.RawMessage(`{"period_index":"0","cutoff":"2026-10-01T00:00:00Z"}`), true)
	var quantity, included, overage, rounded int64
	if err := admin.db.QueryRowContext(ctx, `SELECT quantity,included_quantity,overage_quantity,rounded_minor FROM usage_ratings WHERE subscription_id=? AND period_index=0 AND revision=1`, adminSub).Scan(&quantity, &included, &overage, &rounded); err != nil || quantity != 105 || included != 100 || overage != 5 || rounded != 1 {
		t.Fatalf("admin AI rating: quantity=%d included=%d overage=%d rounded=%d err=%v", quantity, included, overage, rounded, err)
	}
	if renewed, err := domain.RunRenewals(ctx); err != nil || len(renewed) != 1 || renewed[0].AmountMinor != 3001 {
		t.Fatalf("domain AI renewal: %+v err=%v", renewed, err)
	}
	runAdmin("C44", "", "ai-parity-renewal", json.RawMessage(`{}`), true)
	d, a := loadRenewalFacts(t, domain, domainSub), loadRenewalFacts(t, admin, adminSub)
	if !reflect.DeepEqual(d, a) || a.InvoiceMinor != 3001 || a.PaymentMinor != 3001 {
		t.Fatalf("AI renewal differs: domain=%+v admin=%+v", d, a)
	}
	foundUsageLine := false
	for _, line := range a.InvoiceLines {
		if line == "usage:ai_tokens:period:0:1" {
			foundUsageLine = true
		}
	}
	if !foundUsageLine {
		t.Fatalf("AI renewal missing token charge: %v", a.InvoiceLines)
	}
}
