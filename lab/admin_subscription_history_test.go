package lab

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

func TestAdminSubscriptionHistoryPaginatesWithoutRepeatingAndShowsEntitlementSource(t *testing.T) {
	l, clock := openChangingLab(t)
	ctx := context.Background()
	receipt := paidBasicSubscription(t, l, "history-customer")
	if err := l.RefreshEntitlements(ctx); err != nil {
		t.Fatal(err)
	}
	entitlement, err := l.AdminSubscriptionEntitlement(ctx, receipt.SubscriptionID)
	if err != nil || entitlement == nil || entitlement.SourceOperationID != receipt.OperationID || entitlement.Status == "" {
		t.Fatalf("entitlement=%+v err=%v", entitlement, err)
	}
	*clock = cycleBoundary(*clock, 1)
	if _, err := l.RunRenewals(ctx); err != nil {
		t.Fatal(err)
	}
	first, err := l.AdminSubscriptionPeriods(ctx, receipt.SubscriptionID, nil, 1)
	if err != nil || len(first.Items) != 1 || first.Items[0].Index != 1 || first.NextIndex == nil {
		t.Fatalf("first periods=%+v err=%v", first, err)
	}
	second, err := l.AdminSubscriptionPeriods(ctx, receipt.SubscriptionID, first.NextIndex, 1)
	if err != nil || len(second.Items) != 1 || second.Items[0].Index != 0 || second.NextIndex != nil {
		t.Fatalf("second periods=%+v err=%v", second, err)
	}
	seen := map[string]bool{}
	kinds := map[string]bool{}
	var cursor *AdminTimelineCursor
	for page := 0; page < 20; page++ {
		result, err := l.AdminSubscriptionTimeline(ctx, receipt.SubscriptionID, cursor, 1)
		if err != nil {
			t.Fatal(err)
		}
		for _, event := range result.Items {
			key := event.At.Format("2006-01-02T15:04:05.999999999Z07:00") + event.Kind + event.Reference
			if seen[key] {
				t.Fatalf("timeline event repeated: %+v", event)
			}
			seen[key] = true
			kinds[event.Kind] = true
		}
		if result.Next == nil {
			break
		}
		cursor = result.Next
	}
	if len(seen) < 3 {
		t.Fatalf("timeline contains only %d events", len(seen))
	}
	if !kinds["invoice_finalized"] || !kinds["provider_succeeded"] {
		t.Fatalf("financial events missing from timeline: %+v", kinds)
	}
	if _, err := l.AdminSubscriptionTimeline(ctx, "missing-subscription", nil, 10); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing timeline error=%v", err)
	}
}
