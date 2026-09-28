package lab

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestS07DelayedImmediateUpgradeCorrectsOnlyFundedAmount(t *testing.T) {
	l, clock := openChangingLab(t)
	ctx := context.Background()
	r := paidBasicSubscription(t, l, "immediate-pro")
	*clock = time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)
	change, err := l.RequestImmediateProUpgrade(ctx, r.SubscriptionID, 5, 1, "upgrade-five")
	if err != nil {
		t.Fatal(err)
	}
	if change.OldCreditMinor != 1000 || change.NewChargeMinor != 5000 || change.QuotedAmountMinor != 4000 {
		t.Fatalf("9/16 proration: %+v", change)
	}
	if _, err := l.DispatchNext(ctx, "lost_response"); !errors.Is(err, ErrPaymentUnknown) {
		t.Fatalf("lost payment response: %v", err)
	}
	var plan string
	if err := l.db.QueryRowContext(ctx, `SELECT price_version_id FROM subscriptions WHERE id=?`, r.SubscriptionID).Scan(&plan); err != nil || plan != "basic-v1" {
		t.Fatalf("UNKNOWN must retain Basic: %s, %v", plan, err)
	}
	*clock = time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC)
	if found, err := l.ReconcilePayment(ctx, change.OperationID); err != nil || !found {
		t.Fatalf("reconcile: found=%v err=%v", found, err)
	}
	active, err := loadImmediateChange(ctx, l.db, change.ID)
	if err != nil || active.Status != "active" || active.ActualAmountMinor != 3466 || active.CorrectionMinor != 534 {
		t.Fatalf("delayed activation: %+v, %v", active, err)
	}
	corrections, err := l.RunChangeCorrections(ctx)
	if err != nil || len(corrections) != 1 || corrections[0].ReductionMinor != 534 || corrections[0].NewObligationMinor != 3466 {
		t.Fatalf("correction: %+v, %v", corrections, err)
	}
	if again, err := l.RunChangeCorrections(ctx); err != nil || len(again) != 0 {
		t.Fatalf("correction replay: %+v, %v", again, err)
	}
	if state, err := l.State(ctx); err != nil || len(state.ImmediateChanges) != 1 || state.ImmediateChanges[0].Status != "active" {
		t.Fatalf("change must be inspectable: %+v, %v", state.ImmediateChanges, err)
	}
	var funded int64
	if err := l.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount_minor),0) FROM credit_grants WHERE source_invoice_id=?`, change.InvoiceID).Scan(&funded); err != nil || funded != 534 {
		t.Fatalf("funded credit: %d, %v", funded, err)
	}
}

func TestS07UnknownAtPeriodBoundaryHoldsRenewal(t *testing.T) {
	l, clock := openChangingLab(t)
	ctx := context.Background()
	r := paidBasicSubscription(t, l, "immediate-boundary")
	*clock = time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)
	change, err := l.RequestImmediateProUpgrade(ctx, r.SubscriptionID, 5, 1, "upgrade-boundary")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.DispatchNext(ctx, "lost_response"); !errors.Is(err, ErrPaymentUnknown) {
		t.Fatal(err)
	}
	*clock = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if renewed, err := l.RunRenewals(ctx); err != nil || len(renewed) != 0 {
		t.Fatalf("UNKNOWN boundary must hold renewal: %+v, %v", renewed, err)
	}
	if found, err := l.ReconcilePayment(ctx, change.OperationID); err != nil || !found {
		t.Fatalf("late capture must be observed: %v, %v", found, err)
	}
	state, err := loadImmediateChange(ctx, l.db, change.ID)
	if err != nil || state.Status != "needs_review" {
		t.Fatalf("late capture state: %+v, %v", state, err)
	}
	resolution, err := l.ResolveUnfulfilledImmediateChange(ctx, change.ID)
	if err != nil || resolution.NewObligationMinor != 0 || resolution.ReductionMinor != 4000 {
		t.Fatalf("unfulfilled change resolution: %+v, %v", resolution, err)
	}
	if replay, err := l.ResolveUnfulfilledImmediateChange(ctx, change.ID); err != nil || replay.ID != resolution.ID {
		t.Fatalf("resolution replay: %+v, %v", replay, err)
	}
	if state, err := l.State(ctx); err != nil || len(state.ImmediateChanges) != 1 || !state.ImmediateChanges[0].Resolved {
		t.Fatalf("resolution must be inspectable: %+v, %v", state.ImmediateChanges, err)
	}
	if renewed, err := l.RunRenewals(ctx); err != nil || len(renewed) != 1 || renewed[0].AmountMinor != 2000 {
		t.Fatalf("resolved Basic renewal: %+v, %v", renewed, err)
	}
	if _, err := l.RetryFailedPayment(ctx, change.InvoiceID, "late-retry"); !errors.Is(err, ErrConflict) {
		t.Fatalf("expired supplement retry: %v", err)
	}
}
