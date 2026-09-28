package lab

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"
)

type s07ParityPair struct {
	domain, admin             *Lab
	domainClock, adminClock   *time.Time
	domainPaid, adminPaid     Receipt
	domainChange, adminChange ImmediateChange
	adminDispatch             AdminCommand
}

func newS07ParityPair(t *testing.T) s07ParityPair {
	t.Helper()
	ctx := context.Background()
	domain, domainClock := openChangingLab(t)
	admin, adminClock := openChangingLab(t)
	if err := admin.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	const customer = "s07-parity-customer"
	domainPaid := paidBasicSubscription(t, domain, customer)
	adminPaid := paidBasicSubscription(t, admin, customer)
	now := time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)
	*domainClock, *adminClock = now, now

	createBinding := func(l *Lab, subscriptionID string) (Quote, ChangeQuoteBinding) {
		t.Helper()
		quote, err := l.CreateQuoteForCohort(ctx, customer, "pro", "default", 5)
		if err != nil {
			t.Fatal(err)
		}
		binding, err := l.BindChangeQuote(ctx, quote.ID, subscriptionID, "immediate", 1)
		if err != nil {
			t.Fatal(err)
		}
		return quote, binding
	}
	domainQuote, domainBinding := createBinding(domain, domainPaid.SubscriptionID)
	adminQuote, adminBinding := createBinding(admin, adminPaid.SubscriptionID)
	domainChange, err := domain.RequestImmediateProUpgradeAtPrice(ctx, domainPaid.SubscriptionID, 5, 1, "s07-domain-upgrade", domainQuote.ID, domainBinding.Fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(AdminChangePlanPayload{QuoteID: adminQuote.ID, Fingerprint: adminBinding.Fingerprint, Revision: "1"})
	if err != nil {
		t.Fatal(err)
	}
	command := runS07AdminCommand(t, admin, "C04", adminPaid.SubscriptionID, "s07-admin-upgrade", payload, true, "succeeded")
	var refs map[string]string
	if err := json.Unmarshal(command.ResultRefs, &refs); err != nil {
		t.Fatal(err)
	}
	adminChange, err := loadImmediateChange(ctx, admin.db, refs["change_id"])
	if err != nil {
		t.Fatal(err)
	}
	pair := s07ParityPair{
		domain: domain, admin: admin, domainClock: domainClock, adminClock: adminClock,
		domainPaid: domainPaid, adminPaid: adminPaid, domainChange: domainChange, adminChange: adminChange,
	}
	requested := pair.compare(t, "upgrade requested")
	if requested.OldCredit != 1000 || requested.NewCharge != 5000 || requested.Quoted != 4000 || requested.InvoiceTotal != 4000 || requested.CurrentPrice != "basic-v1" || requested.NetApplied != 0 || requested.ProviderCaptures != 1 {
		t.Fatalf("unexpected upgrade request: %+v", requested)
	}
	return pair
}

func runS07AdminCommand(t *testing.T, l *Lab, action, target, key string, payload json.RawMessage, needsPreview bool, wantStatus string) AdminCommand {
	t.Helper()
	ctx := context.Background()
	previewID := ""
	if needsPreview {
		preview, err := l.AdminCreatePreview(ctx, "local-admin", action, target, payload)
		if err != nil {
			t.Fatalf("%s preview: %v", action, err)
		}
		previewID = preview.ID
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", key, action, target, payload, previewID)
	if err != nil {
		t.Fatalf("%s submit: %v", action, err)
	}
	command, err = l.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != wantStatus {
		t.Fatalf("%s execute: %+v err=%v", action, command, err)
	}
	var receipts int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	wantReceipts := 0
	if wantStatus == "succeeded" {
		wantReceipts = 1
	}
	if receipts != wantReceipts {
		t.Fatalf("%s status=%s receipts=%d want=%d", action, command.Status, receipts, wantReceipts)
	}
	return command
}

func (p *s07ParityPair) moveTo(at time.Time) {
	*p.domainClock, *p.adminClock = at, at
}

func (p *s07ParityPair) dispatchUnknown(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	if _, err := p.domain.DispatchNext(ctx, "lost_response"); !errors.Is(err, ErrPaymentUnknown) {
		t.Fatalf("domain lost response: %v", err)
	}
	submitControl(t, p.admin, "s07-admin-fault", "C49", p.adminChange.OperationID, json.RawMessage(`{"operation_kind":"payment","mode":"lost_response"}`))
	p.adminDispatch = runS07AdminCommand(t, p.admin, "C09", p.adminChange.OperationID, "s07-admin-dispatch", json.RawMessage(`{}`), true, "waiting_verification")
	unknown := p.compare(t, "payment unknown")
	if unknown.ChangeStatus != "requested" || unknown.CurrentPrice != "basic-v1" || unknown.OperationStatus != "unknown" || unknown.NetApplied != 0 || unknown.ProviderCaptures != 2 {
		t.Fatalf("unknown payment changed local financial facts: %+v", unknown)
	}
}

func (p *s07ParityPair) reconcile(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	found, err := p.domain.ReconcilePayment(ctx, p.domainChange.OperationID)
	if err != nil || !found {
		t.Fatalf("domain reconcile: found=%v err=%v", found, err)
	}
	runS07AdminCommand(t, p.admin, "C10", p.adminChange.OperationID, "s07-admin-reconcile", json.RawMessage(`{}`), false, "succeeded")
	p.adminDispatch, err = p.admin.AdminExecuteCommand(ctx, p.adminDispatch.ID)
	if err != nil || p.adminDispatch.Status != "succeeded" {
		t.Fatalf("original dispatch recovery: %+v err=%v", p.adminDispatch, err)
	}
	var receipts int
	if err := p.admin.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, p.adminDispatch.ID).Scan(&receipts); err != nil || receipts != 1 {
		t.Fatalf("original dispatch receipt: count=%d err=%v", receipts, err)
	}
}

type s07FinancialFacts struct {
	ChangeStatus, TargetPrice, CurrentPrice, OperationStatus, Entitlement                string
	Seats, OldCredit, NewCharge, Quoted, Actual, Correction, InvoiceTotal, PaymentAmount int64
	Obligation, GrossCaptured, Released, NetApplied, Outstanding                         int64
	CorrectionCount, CorrectionTotal, GrantCount, GrantTotal, ResolutionCount            int64
	RenewalCount, RenewalInvoice                                                         int64
	ProviderCaptures                                                                     int
}

func loadS07FinancialFacts(t *testing.T, l *Lab, paid Receipt, changeID string) s07FinancialFacts {
	t.Helper()
	ctx := context.Background()
	change, err := loadImmediateChange(ctx, l.db, changeID)
	if err != nil {
		t.Fatal(err)
	}
	facts := s07FinancialFacts{
		ChangeStatus: change.Status, TargetPrice: change.TargetPriceVersionID, Seats: change.SeatQuantity,
		OldCredit: change.OldCreditMinor, NewCharge: change.NewChargeMinor, Quoted: change.QuotedAmountMinor,
		Actual: change.ActualAmountMinor, Correction: change.CorrectionMinor, ProviderCaptures: captureCount(t, l),
	}
	if err := l.db.QueryRowContext(ctx, `SELECT price_version_id FROM subscriptions WHERE id=?`, paid.SubscriptionID).Scan(&facts.CurrentPrice); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COALESCE((SELECT status FROM entitlements WHERE subscription_id=?),'pending')`, paid.SubscriptionID).Scan(&facts.Entitlement); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT total_minor FROM invoices WHERE id=?`, change.InvoiceID).Scan(&facts.InvoiceTotal); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT amount_minor,status FROM payment_operations WHERE id=?`, change.OperationID).Scan(&facts.PaymentAmount, &facts.OperationStatus); err != nil {
		t.Fatal(err)
	}
	balance, err := loadInvoiceBalance(ctx, l.db, change.InvoiceID)
	if err != nil {
		t.Fatal(err)
	}
	facts.Obligation, facts.GrossCaptured, facts.Released = balance.ObligationMinor, balance.GrossCapturedMinor, balance.ReleasedMinor
	facts.NetApplied, facts.Outstanding = balance.NetAppliedMinor, balance.OutstandingMinor
	for _, item := range []struct {
		query string
		args  []any
		dest  []any
	}{
		{`SELECT COUNT(*),COALESCE(SUM(reduction_minor),0) FROM corrections WHERE invoice_id=?`, []any{change.InvoiceID}, []any{&facts.CorrectionCount, &facts.CorrectionTotal}},
		{`SELECT COUNT(*),COALESCE(SUM(amount_minor),0) FROM credit_grants WHERE source_invoice_id=?`, []any{change.InvoiceID}, []any{&facts.GrantCount, &facts.GrantTotal}},
		{`SELECT COUNT(*) FROM immediate_change_resolutions WHERE change_id=?`, []any{changeID}, []any{&facts.ResolutionCount}},
		{`SELECT COUNT(*) FROM billing_periods WHERE subscription_id=? AND period_index=1`, []any{paid.SubscriptionID}, []any{&facts.RenewalCount}},
		{`SELECT COALESCE((SELECT i.total_minor FROM billing_periods p JOIN invoices i ON i.id=p.invoice_id WHERE p.subscription_id=? AND p.period_index=1),0)`, []any{paid.SubscriptionID}, []any{&facts.RenewalInvoice}},
	} {
		if err := l.db.QueryRowContext(ctx, item.query, item.args...).Scan(item.dest...); err != nil {
			t.Fatal(err)
		}
	}
	return facts
}

func (p *s07ParityPair) compare(t *testing.T, stage string) s07FinancialFacts {
	t.Helper()
	domain := loadS07FinancialFacts(t, p.domain, p.domainPaid, p.domainChange.ID)
	admin := loadS07FinancialFacts(t, p.admin, p.adminPaid, p.adminChange.ID)
	if !reflect.DeepEqual(domain, admin) {
		t.Fatalf("%s financial facts differ:\ndomain: %+v\nadmin: %+v", stage, domain, admin)
	}
	return admin
}

func TestAdminS07DelayedUpgradeCorrectionMatchesDomain(t *testing.T) {
	pair := newS07ParityPair(t)
	pair.dispatchUnknown(t)
	pair.moveTo(time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC))
	pair.reconcile(t)
	ctx := context.Background()
	if err := pair.domain.RefreshEntitlements(ctx); err != nil {
		t.Fatal(err)
	}
	runS07AdminCommand(t, pair.admin, "C45", "", "s07-admin-refresh", json.RawMessage(`{}`), true, "succeeded")
	before := pair.compare(t, "delayed activation")
	if before.ChangeStatus != "active" || before.Actual != 3466 || before.Correction != 534 || before.CurrentPrice != "pro-v1" || before.Entitlement != "active" || before.ProviderCaptures != 2 || before.Obligation != 4000 || before.NetApplied != 4000 || before.Outstanding != 0 {
		t.Fatalf("unexpected delayed activation: %+v", before)
	}
	corrections, err := pair.domain.RunChangeCorrections(ctx)
	if err != nil || len(corrections) != 1 {
		t.Fatalf("domain corrections: %+v err=%v", corrections, err)
	}
	runS07AdminCommand(t, pair.admin, "C13", "", "s07-admin-corrections", json.RawMessage(`{}`), true, "succeeded")
	after := pair.compare(t, "funded correction")
	if after.CorrectionCount != 1 || after.CorrectionTotal != 534 || after.GrantCount != 1 || after.GrantTotal != 534 || after.Obligation != 3466 || after.Released != 534 || after.NetApplied != 3466 || after.Outstanding != 0 || after.ProviderCaptures != 2 {
		t.Fatalf("unexpected funded correction: %+v", after)
	}
}

func TestAdminS07UnknownAtBoundaryAndResolutionMatchesDomain(t *testing.T) {
	pair := newS07ParityPair(t)
	pair.dispatchUnknown(t)
	pair.moveTo(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	ctx := context.Background()
	renewals, err := pair.domain.RunRenewals(ctx)
	if err != nil || len(renewals) != 0 {
		t.Fatalf("domain held renewal: %+v err=%v", renewals, err)
	}
	runS07AdminCommand(t, pair.admin, "C44", "", "s07-admin-held-renewal", json.RawMessage(`{}`), true, "succeeded")
	held := pair.compare(t, "unknown at period boundary")
	if held.RenewalCount != 0 || held.CurrentPrice != "basic-v1" || held.OperationStatus != "unknown" {
		t.Fatalf("renewal was not held: %+v", held)
	}
	pair.reconcile(t)
	needsReview := pair.compare(t, "late capture requires review")
	if needsReview.ChangeStatus != "needs_review" || needsReview.RenewalCount != 0 {
		t.Fatalf("late capture state: %+v", needsReview)
	}
	resolution, err := pair.domain.ResolveUnfulfilledImmediateChange(ctx, pair.domainChange.ID)
	if err != nil || resolution.ReductionMinor != 4000 || resolution.NewObligationMinor != 0 {
		t.Fatalf("domain resolution: %+v err=%v", resolution, err)
	}
	runS07AdminCommand(t, pair.admin, "C14", pair.adminChange.ID, "s07-admin-resolution", json.RawMessage(`{}`), true, "succeeded")
	resolved := pair.compare(t, "unfulfilled change resolved")
	if resolved.ResolutionCount != 1 || resolved.CorrectionCount != 1 || resolved.CorrectionTotal != 4000 || resolved.GrantCount != 1 || resolved.GrantTotal != 4000 || resolved.Obligation != 0 || resolved.Released != 4000 || resolved.NetApplied != 0 || resolved.CurrentPrice != "basic-v1" || resolved.ProviderCaptures != 2 {
		t.Fatalf("unexpected resolution: %+v", resolved)
	}
	renewals, err = pair.domain.RunRenewals(ctx)
	if err != nil || len(renewals) != 1 || renewals[0].AmountMinor != 2000 {
		t.Fatalf("domain Basic renewal: %+v err=%v", renewals, err)
	}
	runS07AdminCommand(t, pair.admin, "C44", "", "s07-admin-basic-renewal", json.RawMessage(`{}`), true, "succeeded")
	final := pair.compare(t, "Basic renewal after resolution")
	if final.RenewalCount != 1 || final.RenewalInvoice != 2000 || final.CurrentPrice != "basic-v1" || final.ProviderCaptures != 2 {
		t.Fatalf("unexpected Basic renewal: %+v", final)
	}
}
