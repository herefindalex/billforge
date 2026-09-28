package lab

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestS09UsageCloseLateEventAndOriginalPeriodDebit(t *testing.T) {
	l, clock := openChangingLab(t)
	ctx := context.Background()
	r := paidProSubscription(t, l, "usage-tenant")
	*clock = time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	eventAt := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	e, err := l.RecordUsage(ctx, "worker", "event-1", r.SubscriptionID, "tasks", 20003, eventAt)
	if err != nil || e.PeriodIndex != 0 || e.PriceVersionID != "pro-v1" {
		t.Fatalf("usage ingest: %+v, %v", e, err)
	}
	if replay, err := l.RecordUsage(ctx, "worker", "event-1", r.SubscriptionID, "tasks", 20003, eventAt); err != nil || replay.ReceivedAt != e.ReceivedAt {
		t.Fatalf("usage replay: %+v, %v", replay, err)
	}
	if _, err := l.RecordUsage(ctx, "worker", "event-1", r.SubscriptionID, "tasks", 20004, eventAt); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed payload reused ID: %v", err)
	}
	*clock = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	initial, err := l.CloseUsagePeriod(ctx, r.SubscriptionID, 0, *clock)
	if err != nil || initial.Quantity != 20003 || initial.OverageQuantity != 3 || initial.ExactMinorNumerator != 3 || initial.ExactMinorDenominator != 10 || initial.RoundedMinor != 0 {
		t.Fatalf("20,003 rating: %+v, %v", initial, err)
	}
	first, err := l.RunRenewals(ctx)
	if err != nil || len(first) != 1 || first[0].AmountMinor != 10000 {
		t.Fatalf("first usage renewal: %+v, %v", first, err)
	}
	var amount int64
	if err := l.db.QueryRowContext(ctx, `SELECT amount_minor FROM invoice_lines WHERE invoice_id=? AND component_code='usage:tasks:period:0'`, first[0].InvoiceID).Scan(&amount); err != nil || amount != 0 {
		t.Fatalf("zero rounded line preserved: %d, %v", amount, err)
	}
	if _, err := l.DispatchNext(ctx, ""); err != nil {
		t.Fatal(err)
	}
	*clock = time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	lateAt := time.Date(2026, 9, 30, 23, 0, 0, 0, time.UTC)
	if _, err := l.RecordUsage(ctx, "worker", "event-2", r.SubscriptionID, "tasks", 7, lateAt); err != nil {
		t.Fatal(err)
	}
	updated, err := l.RerateUsagePeriod(ctx, r.SubscriptionID, 0)
	if err != nil || updated.Revision != 2 || updated.Quantity != 20010 || updated.ExactMinorNumerator != 10 || updated.RoundedMinor != 1 || updated.DeltaMinor != 1 {
		t.Fatalf("late rerating: %+v, %v", updated, err)
	}
	if replay, err := l.RerateUsagePeriod(ctx, r.SubscriptionID, 0); err != nil || replay.ID != updated.ID {
		t.Fatalf("rerate replay: %+v, %v", replay, err)
	}
	*clock = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	second, err := l.RunRenewals(ctx)
	if err != nil || len(second) != 1 || second[0].AmountMinor != 10001 {
		t.Fatalf("late debit on next invoice: %+v, %v", second, err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT amount_minor FROM invoice_lines WHERE invoice_id=? AND component_code='usage:tasks:period:0'`, second[0].InvoiceID).Scan(&amount); err != nil || amount != 1 {
		t.Fatalf("original period late debit: %d, %v", amount, err)
	}
	if again, err := l.RunRenewals(ctx); err != nil || len(again) != 0 {
		t.Fatalf("renewal replay: %+v, %v", again, err)
	}
	if _, err := l.DispatchNext(ctx, ""); err != nil {
		t.Fatal(err)
	}
	*clock = time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	adjusted, err := l.RecordUsageAdjustment(ctx, "worker", "adjust-2", r.SubscriptionID, "worker", "event-2", 7)
	if err != nil || adjusted.Quantity != -7 || adjusted.PeriodIndex != 0 {
		t.Fatalf("usage adjustment: %+v, %v", adjusted, err)
	}
	if _, err := l.RecordUsageAdjustment(ctx, "worker", "adjust-2", r.SubscriptionID, "worker", "event-2", 7); err != nil {
		t.Fatalf("adjustment replay: %v", err)
	}
	if _, err := l.RecordUsageAdjustment(ctx, "worker", "adjust-extra", r.SubscriptionID, "worker", "event-2", 1); !errors.Is(err, ErrConflict) {
		t.Fatalf("over-reversal: %v", err)
	}
	decreased, err := l.RerateUsagePeriod(ctx, r.SubscriptionID, 0)
	if err != nil || decreased.Revision != 3 || decreased.RoundedMinor != 0 || decreased.DeltaMinor != -1 {
		t.Fatalf("negative rerating: %+v, %v", decreased, err)
	}
	*clock = time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	if held, err := l.RunRenewals(ctx); err != nil || len(held) != 0 {
		t.Fatalf("negative usage must hold renewal until credit note: %+v, %v", held, err)
	}
	notes, err := l.RunUsageCreditNotes(ctx)
	if err != nil || len(notes) != 1 || notes[0].InvoiceID != second[0].InvoiceID || notes[0].ReductionMinor != 1 {
		t.Fatalf("usage credit note: %+v, %v", notes, err)
	}
	if repeat, err := l.RunUsageCreditNotes(ctx); err != nil || len(repeat) != 0 {
		t.Fatalf("credit note replay: %+v, %v", repeat, err)
	}
	var funded int64
	if err := l.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount_minor),0) FROM credit_grants WHERE source_invoice_id=?`, second[0].InvoiceID).Scan(&funded); err != nil || funded != 1 {
		t.Fatalf("funded credit: %d, %v", funded, err)
	}
	if resumed, err := l.RunRenewals(ctx); err != nil || len(resumed) != 1 || resumed[0].AmountMinor != 10000 {
		t.Fatalf("renewal after credit note: %+v, %v", resumed, err)
	}
}
