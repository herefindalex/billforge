package lab

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"
)

func TestAdminUsagePeriodDetailEstimateAndRatingHistory(t *testing.T) {
	l, clock := openChangingLab(t)
	ctx := context.Background()
	paid := paidProSubscription(t, l, "usage-period-detail")
	*clock = time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	beforeEvents, err := l.AdminUsagePeriodDetail(ctx, paid.SubscriptionID, 0)
	if err != nil || beforeEvents.Status != "estimated" || beforeEvents.Estimate == nil || beforeEvents.Estimate.Quantity != 0 {
		t.Fatalf("empty open period: %+v, %v", beforeEvents, err)
	}
	eventAt := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	if _, err := l.RecordUsage(ctx, "worker", "detail-event-1", paid.SubscriptionID, "tasks", 20010, eventAt); err != nil {
		t.Fatal(err)
	}

	open, err := l.AdminUsagePeriodDetail(ctx, paid.SubscriptionID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if open.Status != "estimated" || open.Estimate == nil || open.Estimate.Quantity != 20010 || open.Estimate.RoundedMinor != 1 || open.LatestRating != nil || open.Currency != "USD" {
		t.Fatalf("open period: %+v", open)
	}
	events, total, _, err := l.AdminFilteredResourcePage(ctx, "usage-events", 0, 20, map[string]string{"subscription_id": paid.SubscriptionID, "period_index": "0"})
	if err != nil || total != 1 || len(events) != 1 {
		t.Fatalf("period source events: %d rows, total %d, %v", len(events), total, err)
	}
	if _, _, _, err := l.AdminFilteredResourcePage(ctx, "usage-events", 0, 20, map[string]string{"period_index": "0 OR 1=1"}); !errors.Is(err, ErrAdminInvalidCommand) {
		t.Fatalf("invalid period filter: %v", err)
	}
	empty, err := l.AdminUsagePeriodRatings(ctx, paid.SubscriptionID, 0, nil, 1)
	if err != nil || len(empty.Items) != 0 || empty.Next != nil {
		t.Fatalf("open ratings: %+v, %v", empty, err)
	}

	*clock = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if _, err := l.CloseUsagePeriod(ctx, paid.SubscriptionID, 0, *clock); err != nil {
		t.Fatal(err)
	}
	finalized, err := l.AdminUsagePeriodDetail(ctx, paid.SubscriptionID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if finalized.Status != "finalized" || finalized.Estimate != nil || finalized.LatestRating == nil || finalized.LatestRating.Revision != 1 || finalized.RatedMinor != 1 || finalized.CutoffAt == nil {
		t.Fatalf("finalized period: %+v", finalized)
	}

	*clock = time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	if _, err := l.RecordUsage(ctx, "worker", "detail-event-2", paid.SubscriptionID, "tasks", 10, eventAt); err != nil {
		t.Fatal(err)
	}
	if _, err := l.RerateUsagePeriod(ctx, paid.SubscriptionID, 0); err != nil {
		t.Fatal(err)
	}
	rerated, err := l.AdminUsagePeriodDetail(ctx, paid.SubscriptionID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if rerated.Status != "rerated" || rerated.LatestRating == nil || rerated.LatestRating.Revision != 2 || rerated.LatestRating.DeltaMinor != 1 || rerated.RatedMinor != 2 {
		t.Fatalf("rerated period: %+v", rerated)
	}
	first, err := l.AdminUsagePeriodRatings(ctx, paid.SubscriptionID, 0, nil, 1)
	if err != nil || len(first.Items) != 1 || first.Items[0].Revision != 2 || first.Next == nil {
		t.Fatalf("first rating page: %+v, %v", first, err)
	}
	second, err := l.AdminUsagePeriodRatings(ctx, paid.SubscriptionID, 0, first.Next, 1)
	if err != nil || len(second.Items) != 1 || second.Items[0].Revision != 1 || second.Next != nil {
		t.Fatalf("second rating page: %+v, %v", second, err)
	}
	if _, err := l.AdminUsagePeriodRatings(ctx, paid.SubscriptionID, 1, first.Next, 1); !errors.Is(err, ErrConflict) {
		t.Fatalf("cross-period cursor: %v", err)
	}
	if _, err := l.AdminUsagePeriodDetail(ctx, paid.SubscriptionID, 99); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing period: %v", err)
	}
}
