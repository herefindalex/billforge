package lab

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRecordUsageRejectsTimeThatWrapsIntoCurrentBillingPeriod(t *testing.T) {
	l, _ := openChangingLab(t)
	ctx := context.Background()
	receipt := paidProSubscription(t, l, "usage-time-range-customer")
	var periodStart int64
	if err := l.db.QueryRowContext(ctx, `SELECT period_start FROM billing_periods WHERE subscription_id=? AND period_index=0`, receipt.SubscriptionID).Scan(&periodStart); err != nil {
		t.Fatal(err)
	}
	withinPeriod := time.Unix(0, periodStart).UTC().Add(time.Hour)
	// Subtract 2^64 nanoseconds. The true time is centuries earlier, but
	// time.Time.UnixNano wraps to the current billing period's timestamp.
	tooEarly := time.Unix(withinPeriod.Unix()-18446744073, int64(withinPeriod.Nanosecond())-709551616).UTC()
	if !tooEarly.Before(minUnixNanoTime) || tooEarly.UnixNano() != withinPeriod.UnixNano() {
		t.Fatalf("invalid overflow fixture: early=%s target=%s", tooEarly, withinPeriod)
	}
	_, err := l.RecordUsage(ctx, "worker", "wrapped-usage-event", receipt.SubscriptionID, "tasks", 1, tooEarly)
	if !errors.Is(err, ErrConflict) {
		var stored int64
		_ = l.db.QueryRowContext(ctx, `SELECT event_at FROM usage_events WHERE event_id='wrapped-usage-event'`).Scan(&stored)
		t.Fatalf("out-of-range usage admitted: err=%v stored_event_at=%d target=%d", err, stored, withinPeriod.UnixNano())
	}
	var count int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_events WHERE event_id='wrapped-usage-event'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("out-of-range usage persisted: count=%d err=%v", count, err)
	}
}

func TestUsageAdjustmentRejectsOutOfRangeReceiptClock(t *testing.T) {
	l, clock := openChangingLab(t)
	ctx := context.Background()
	receipt := paidProSubscription(t, l, "adjustment-time-range-customer")
	var periodStart int64
	if err := l.db.QueryRowContext(ctx, `SELECT period_start FROM billing_periods WHERE subscription_id=? AND period_index=0`, receipt.SubscriptionID).Scan(&periodStart); err != nil {
		t.Fatal(err)
	}
	eventAt := time.Unix(0, periodStart).UTC().Add(time.Hour)
	*clock = eventAt.Add(time.Hour)
	if _, err := l.RecordUsage(ctx, "worker", "original-usage-event", receipt.SubscriptionID, "tasks", 1, eventAt); err != nil {
		t.Fatal(err)
	}
	*clock = time.Date(2700, time.January, 1, 0, 0, 0, 0, time.UTC)
	if _, err := l.RecordUsage(ctx, "worker", "original-usage-event", receipt.SubscriptionID, "tasks", 1, eventAt); err != nil {
		t.Fatalf("existing event replay should not depend on the current clock: %v", err)
	}
	_, err := l.RecordUsageAdjustment(ctx, "worker", "out-of-range-adjustment", receipt.SubscriptionID, "worker", "original-usage-event", 1)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("usage adjustment admitted with out-of-range receipt clock: %v", err)
	}
	var count int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_events WHERE event_id='out-of-range-adjustment'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("out-of-range adjustment persisted: count=%d err=%v", count, err)
	}
}
