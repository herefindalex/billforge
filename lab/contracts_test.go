package lab

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"
)

func acmeContract(post string) ContractSpec {
	return ContractSpec{ID: "acme-contract-v1", CustomerID: "acme", Version: 1, BasePriceVersionID: "pro-v1", FixedMinor: 4000, SeatMinor: 700, EffectiveFrom: fixedNow, EffectiveTo: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), PostContractPriceVersionID: post}
}

func TestS10AcmeContractNet30AndMissingNextPriceHold(t *testing.T) {
	l, clock := openChangingLab(t)
	ctx := context.Background()
	spec := acmeContract("")
	contract, err := l.PublishContract(ctx, spec)
	if err != nil || contract.FixedMinor != 4000 || contract.SeatMinor != 700 {
		t.Fatalf("publish contract: %+v, %v", contract, err)
	}
	if _, err := l.PublishContract(ctx, spec); err != nil {
		t.Fatalf("contract replay: %v", err)
	}
	changed := spec
	changed.FixedMinor = 4100
	if _, err := l.PublishContract(ctx, changed); !errors.Is(err, ErrConflict) {
		t.Fatalf("immutable contract: %v", err)
	}
	q, err := l.CreateContractQuote(ctx, "acme", spec.ID, 5)
	if err != nil || q.AmountMinor != 7500 || q.ContractVersionID != spec.ID {
		t.Fatalf("Acme quote: %+v, %v", q, err)
	}
	if _, err := l.AcceptQuote(ctx, q.ID, q.Fingerprint, "wrong-accept"); !errors.Is(err, ErrConflict) {
		t.Fatalf("self-serve accept of contract: %v", err)
	}
	r, err := l.AcceptContractQuote(ctx, q.ID, q.Fingerprint, "acme-accept")
	if err != nil || r.AmountMinor != 7500 {
		t.Fatalf("Acme accept: %+v, %v", r, err)
	}
	if replay, err := l.AcceptContractQuote(ctx, q.ID, q.Fingerprint, "acme-accept"); err != nil || replay.OperationID != r.OperationID {
		t.Fatalf("accept replay: %+v, %v", replay, err)
	}
	if captureCount(t, l) != 0 {
		t.Fatal("Net30 must not capture at activation")
	}
	if _, err := l.DispatchNext(ctx, ""); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("premature capture: %v", err)
	}
	if count, err := l.CollectDueContractInvoices(ctx); err != nil || count != 0 {
		t.Fatalf("premature collection: %d, %v", count, err)
	}
	if err := l.RefreshEntitlements(ctx); err != nil {
		t.Fatal(err)
	}
	state, err := l.State(ctx)
	if err != nil || len(state.Subscriptions) != 1 || state.Subscriptions[0].EntitlementStatus != "active" {
		t.Fatalf("service before payment: %+v, %v", state.Subscriptions, err)
	}
	var due int64
	if err := l.db.QueryRowContext(ctx, `SELECT due_at FROM billing_periods WHERE subscription_id=? AND period_index=0`, r.SubscriptionID).Scan(&due); err != nil || due != time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC).UnixNano() {
		t.Fatalf("Net30 due: %d, %v", due, err)
	}
	*clock = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if renewed, err := l.RunRenewals(ctx); err != nil || len(renewed) != 0 {
		t.Fatalf("missing post-contract price must hold: %+v, %v", renewed, err)
	}
	var reason string
	if err := l.db.QueryRowContext(ctx, `SELECT reason FROM renewal_holds WHERE subscription_id=? AND period_index=1`, r.SubscriptionID).Scan(&reason); err != nil || reason != "contract_next_price_missing" {
		t.Fatalf("hold reason: %s, %v", reason, err)
	}
	if err := l.RefreshEntitlements(ctx); err != nil {
		t.Fatal(err)
	}
	state, err = l.State(ctx)
	if err != nil || state.Subscriptions[0].EntitlementStatus != "suspended" {
		t.Fatalf("expired contract entitlement: %+v, %v", state.Subscriptions, err)
	}
	if count, err := l.CollectDueContractInvoices(ctx); err != nil || count != 1 {
		t.Fatalf("due collection: %d, %v", count, err)
	}
	if _, err := l.DispatchNext(ctx, ""); err != nil {
		t.Fatalf("collect Net30 after service period: %v", err)
	}
	if captureCount(t, l) != 1 {
		t.Fatal("contract invoice not captured once")
	}
	if again, err := l.RunRenewals(ctx); err != nil || len(again) != 0 {
		t.Fatalf("payment must not invent post-contract price: %+v, %v", again, err)
	}
}

func TestS10ContractRenewalAndExplicitPostContractPrice(t *testing.T) {
	l, clock := openChangingLab(t)
	ctx := context.Background()
	spec := acmeContract("pro-v1")
	spec.EffectiveTo = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	if _, err := l.PublishContract(ctx, spec); err != nil {
		t.Fatal(err)
	}
	q, err := l.CreateContractQuote(ctx, "acme", spec.ID, 5)
	if err != nil {
		t.Fatal(err)
	}
	r, err := l.AcceptContractQuote(ctx, q.ID, q.Fingerprint, "multi-period-acme")
	if err != nil {
		t.Fatal(err)
	}
	*clock = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	renewed, err := l.RunRenewals(ctx)
	if err != nil || len(renewed) != 1 || renewed[0].AmountMinor != 7500 {
		t.Fatalf("contract renewal while first Net30 invoice due: %+v, %v", renewed, err)
	}
	if captureCount(t, l) != 0 {
		t.Fatal("contract renewal captured before collection")
	}
	if count, err := l.CollectDueContractInvoices(ctx); err != nil || count != 1 {
		t.Fatalf("first due collection: %d, %v", count, err)
	}
	if _, err := l.DispatchNext(ctx, ""); err != nil {
		t.Fatal(err)
	}
	*clock = time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)
	if count, err := l.CollectDueContractInvoices(ctx); err != nil || count != 1 {
		t.Fatalf("second due collection: %d, %v", count, err)
	}
	if _, err := l.DispatchNext(ctx, ""); err != nil {
		t.Fatal(err)
	}
	*clock = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	post, err := l.RunRenewals(ctx)
	if err != nil || len(post) != 1 || post[0].AmountMinor != 10000 {
		t.Fatalf("explicit post-contract price: %+v, %v", post, err)
	}
	var version string
	var history int
	if err := l.db.QueryRowContext(ctx, `SELECT price_version_id FROM subscriptions WHERE id=?`, r.SubscriptionID).Scan(&version); err != nil || version != "pro-v1" {
		t.Fatalf("post contract version: %s, %v", version, err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pricing_assignments WHERE subscription_id=?`, r.SubscriptionID).Scan(&history); err != nil || history != 2 {
		t.Fatalf("post contract assignment: %d, %v", history, err)
	}
	if _, err := l.DispatchNext(ctx, ""); err != nil {
		t.Fatalf("post-contract self-serve capture: %v", err)
	}
	*clock = time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	after, err := l.RunRenewals(ctx)
	if err != nil || len(after) != 1 || after[0].AmountMinor != 10000 {
		t.Fatalf("subsequent self-serve renewal: %+v, %v", after, err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pricing_assignments WHERE subscription_id=?`, r.SubscriptionID).Scan(&history); err != nil || history != 2 {
		t.Fatalf("post transition replayed: %d, %v", history, err)
	}
}
