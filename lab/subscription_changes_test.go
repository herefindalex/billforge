package lab

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func openChangingLab(t *testing.T) (*Lab, *time.Time) {
	t.Helper()
	dir := t.TempDir()
	now := fixedNow
	l, err := Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	return l, &now
}

func paidBasicSubscription(t *testing.T, l *Lab, key string) Receipt {
	t.Helper()
	ctx := context.Background()
	q, err := l.CreateQuote(ctx, key, "basic")
	if err != nil {
		t.Fatal(err)
	}
	r, err := l.AcceptQuote(ctx, q.ID, q.Fingerprint, key+":purchase")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.DispatchNext(ctx, ""); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestS06NextPeriodChangeUsesRevisionAndAssignmentHistory(t *testing.T) {
	l, clock := openChangingLab(t)
	ctx := context.Background()
	r := paidBasicSubscription(t, l, "scheduled-change")
	schedule, err := l.ScheduleNextPlan(ctx, r.SubscriptionID, "pro", 5, 1, "next-pro")
	if err != nil || schedule.TargetPriceVersionID != "pro-v1" || schedule.Revision != 2 {
		t.Fatalf("schedule Pro: %+v, %v", schedule, err)
	}
	if replay, err := l.ScheduleNextPlan(ctx, r.SubscriptionID, "pro", 5, 1, "next-pro"); err != nil || replay.ID != schedule.ID || replay.Revision != 2 {
		t.Fatalf("schedule replay: %+v, %v", replay, err)
	}
	if _, err := l.ScheduleNextPlan(ctx, r.SubscriptionID, "pro", 4, 1, "next-pro"); !errors.Is(err, ErrConflict) {
		t.Fatalf("same request key changed seats: %v", err)
	}
	if _, err := l.ScheduleCancel(ctx, r.SubscriptionID, 1, "stale-revision"); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale revision accepted: %v", err)
	}
	if _, err := l.ScheduleCancel(ctx, r.SubscriptionID, 2, "conflicting-cancel"); !errors.Is(err, ErrConflict) {
		t.Fatalf("competing boundary intent accepted: %v", err)
	}
	before, err := l.State(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(before.Subscriptions) != 1 || before.Subscriptions[0].PriceVersionID != "basic-v1" || len(before.Assignments) != 1 || before.Assignments[0].EffectiveEnd != nil {
		t.Fatalf("schedule changed effective plan early: %+v", before)
	}
	*clock = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	renewals, err := l.RunRenewals(ctx)
	if err != nil || len(renewals) != 1 || renewals[0].AmountMinor != 10000 {
		t.Fatalf("boundary Pro renewal: %+v, %v", renewals, err)
	}
	if repeated, err := l.RunRenewals(ctx); err != nil || len(repeated) != 0 {
		t.Fatalf("boundary replay duplicated invoice: %+v, %v", repeated, err)
	}
	after, err := l.State(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if after.Subscriptions[0].PriceVersionID != "pro-v1" || after.Subscriptions[0].SeatQuantity != 5 || after.Subscriptions[0].Revision != 3 || len(after.Assignments) != 2 || after.Assignments[0].EffectiveEnd == nil || after.Assignments[1].EffectiveStart != *clock || after.Schedules[0].Status != "applied" {
		t.Fatalf("boundary assignment state: %+v", after)
	}
}

func TestS06CancelResumeAndBoundaryExpiry(t *testing.T) {
	l, clock := openChangingLab(t)
	ctx := context.Background()
	r := paidBasicSubscription(t, l, "scheduled-cancel")
	cancel, err := l.ScheduleCancel(ctx, r.SubscriptionID, 1, "cancel-at-boundary")
	if err != nil || cancel.Kind != "cancel" || cancel.Revision != 2 {
		t.Fatalf("schedule cancel: %+v, %v", cancel, err)
	}
	if _, err := l.ScheduleNextPlan(ctx, r.SubscriptionID, "pro", 5, 2, "change-before-resume"); !errors.Is(err, ErrConflict) {
		t.Fatalf("change bypassed pending cancel: %v", err)
	}
	resumed, err := l.ResumeCancel(ctx, r.SubscriptionID, 2, "resume-before-boundary")
	if err != nil || resumed.ID != cancel.ID || resumed.Status != "cancelled" || resumed.Revision != 3 {
		t.Fatalf("resume cancel: %+v, %v", resumed, err)
	}
	if replay, err := l.ResumeCancel(ctx, r.SubscriptionID, 2, "resume-before-boundary"); err != nil || replay.ID != cancel.ID || replay.Revision != 3 {
		t.Fatalf("resume replay: %+v, %v", replay, err)
	}
	cancel, err = l.ScheduleCancel(ctx, r.SubscriptionID, 3, "cancel-again")
	if err != nil || cancel.Revision != 4 {
		t.Fatalf("schedule second cancel: %+v, %v", cancel, err)
	}
	*clock = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if renewals, err := l.RunRenewals(ctx); err != nil || len(renewals) != 0 {
		t.Fatalf("cancellation generated renewal: %+v, %v", renewals, err)
	}
	if err := l.RefreshEntitlements(ctx); err != nil {
		t.Fatal(err)
	}
	s, err := l.State(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if s.Subscriptions[0].Status != "ended" || s.Subscriptions[0].EntitlementStatus != "expired" || len(s.Invoices) != 1 || s.Assignments[0].EffectiveEnd == nil {
		t.Fatalf("cancel boundary state: %+v", s)
	}
	if again, err := l.RunRenewals(ctx); err != nil || len(again) != 0 {
		t.Fatalf("ended subscription renewed: %+v, %v", again, err)
	}
	if _, err := l.ResumeCancel(ctx, r.SubscriptionID, 4, "too-late-resume"); err == nil {
		t.Fatal("resume after cancel boundary accepted")
	}
}
