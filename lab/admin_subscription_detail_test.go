package lab

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

func TestAdminSubscriptionDetailShowsActualPricePeriodAndScheduledCancel(t *testing.T) {
	l, _ := openChangingLab(t)
	ctx := context.Background()
	receipt := paidProSubscription(t, l, "detail-customer")
	detail, err := l.AdminSubscriptionDetail(ctx, receipt.SubscriptionID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.PriceVersionID != "pro-v1" || detail.PricePlanID != "pro" || detail.SeatQuantity != 5 || detail.ActualFixedMinor != 5000 || detail.ActualSeatMinor != 1000 {
		t.Fatalf("unexpected actual price: %+v", detail)
	}
	if detail.CurrentPeriod == nil || detail.CurrentPeriod.InvoiceID != receipt.InvoiceID || detail.CurrentPeriod.Index != 0 {
		t.Fatalf("missing current period: %+v", detail.CurrentPeriod)
	}
	if _, err := l.ScheduleCancel(ctx, receipt.SubscriptionID, detail.Revision, "detail-cancel"); err != nil {
		t.Fatal(err)
	}
	detail, err = l.AdminSubscriptionDetail(ctx, receipt.SubscriptionID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.ScheduledCancel == nil || detail.ScheduledCancel.Status != "scheduled" || detail.ScheduledChange != nil {
		t.Fatalf("unexpected schedule: %+v", detail)
	}
	if _, err := l.AdminSubscriptionDetail(ctx, "missing-subscription"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing subscription error = %v", err)
	}
}
