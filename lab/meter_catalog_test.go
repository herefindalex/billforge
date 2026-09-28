package lab

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestP01AITokensSKUUsesGenericMeteredPath(t *testing.T) {
	l, clock := openChangingLab(t)
	ctx := context.Background()
	meter := MeterSpec{ID: "ai_tokens", Source: "ai_gateway", Unit: "token", SchemaVersion: 1}
	if err := l.RegisterMeter(ctx, meter); err != nil {
		t.Fatal(err)
	}
	if err := l.RegisterMeter(ctx, meter); err != nil {
		t.Fatal(err)
	}
	if err := l.RegisterMeter(ctx, MeterSpec{ID: "ai_tokens", Source: "other", Unit: "token", SchemaVersion: 1}); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed meter registration: %v", err)
	}
	spec := MeteredPriceSpec{ID: "ai_v1", PlanID: "ai", Version: 1, FixedMinor: 3000, MeterID: "ai_tokens", IncludedQuantity: 100, UsageRateNum: 1, UsageRateDen: 5, EffectiveFrom: *clock}
	terms, err := l.PublishMeteredPrice(ctx, spec)
	if err != nil || terms.MeterID != "ai_tokens" || terms.IncludedQuantity != 100 || terms.IncludedTasks != 0 {
		t.Fatalf("terms: %+v %v", terms, err)
	}
	if _, err := l.PublishMeteredPrice(ctx, spec); err != nil {
		t.Fatalf("publish replay: %v", err)
	}
	changed := spec
	changed.FixedMinor = 3100
	if _, err := l.PublishMeteredPrice(ctx, changed); !errors.Is(err, ErrConflict) {
		t.Fatalf("published price mutated: %v", err)
	}
	if err := l.SelectCatalogPrice(ctx, "ai", "default", *clock, spec.ID); err != nil {
		t.Fatal(err)
	}
	q, err := l.CreateQuote(ctx, "ai-customer", "ai")
	if err != nil || q.AmountMinor != 3000 || q.PriceVersionID != spec.ID {
		t.Fatalf("AI quote: %+v %v", q, err)
	}
	r, err := l.AcceptQuote(ctx, q.ID, q.Fingerprint, "ai:purchase")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.DispatchNext(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if err := l.RebuildEntitlements(ctx); err != nil {
		t.Fatal(err)
	}
	*clock = time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	eventAt := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	if _, err := l.RecordUsage(ctx, "wrong_source", "e1", r.SubscriptionID, "ai_tokens", 105, eventAt); !errors.Is(err, ErrConflict) {
		t.Fatalf("wrong source: %v", err)
	}
	if _, err := l.RecordUsage(ctx, "ai_gateway", "e1", r.SubscriptionID, "tasks", 105, eventAt); !errors.Is(err, ErrConflict) {
		t.Fatalf("wrong meter: %v", err)
	}
	e, err := l.RecordUsage(ctx, "ai_gateway", "e1", r.SubscriptionID, "ai_tokens", 105, eventAt)
	if err != nil || e.MeterID != "ai_tokens" {
		t.Fatalf("AI usage: %+v %v", e, err)
	}
	if _, err := l.RecordUsage(ctx, "ai_gateway", "e1", r.SubscriptionID, "ai_tokens", 105, eventAt); err != nil {
		t.Fatalf("replay: %v", err)
	}
	if _, err := l.RecordUsage(ctx, "ai_gateway", "e1", r.SubscriptionID, "ai_tokens", 106, eventAt); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed event: %v", err)
	}
	*clock = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	rating, err := l.CloseUsagePeriod(ctx, r.SubscriptionID, 0, *clock)
	if err != nil || rating.Quantity != 105 || rating.IncludedQuantity != 100 || rating.OverageQuantity != 5 || rating.RoundedMinor != 1 {
		t.Fatalf("AI rating: %+v %v", rating, err)
	}
	renewals, err := l.RunRenewals(ctx)
	if err != nil || len(renewals) != 1 || renewals[0].AmountMinor != 3001 {
		t.Fatalf("AI renewal: %+v %v", renewals, err)
	}
	var amount int64
	if err := l.db.QueryRowContext(ctx, `SELECT amount_minor FROM invoice_lines WHERE invoice_id=? AND component_code='usage:ai_tokens:period:0'`, renewals[0].InvoiceID).Scan(&amount); err != nil || amount != 1 {
		t.Fatalf("AI invoice line: %d %v", amount, err)
	}
}
