package lab

import (
	"context"
	"errors"
	"testing"
)

var cutoverLimits = MigrationThresholds{MaxQuoteP95Millis: 5000, MaxUnknownPayments: 0, MaxOpenDiscrepancies: 0}

func TestP03ShadowReadThenSingleWriterCutover(t *testing.T) {
	l, _, _ := openTestLab(t)
	ctx := context.Background()
	link, err := l.LinkLegacyAccount(ctx, AccountLink{LegacyAccountID: "legacy:new", CustomerID: "new-customer", BeneficiaryID: "new-customer", Cohort: "default"})
	if err != nil || link.WriterOwner != "legacy" || link.ReadOwner != "legacy" {
		t.Fatalf("account link: %+v %v", link, err)
	}
	if _, err := l.LinkLegacyAccount(ctx, AccountLink{LegacyAccountID: "legacy:new", CustomerID: "different", BeneficiaryID: "new-customer", Cohort: "default"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("mapping changed: %v", err)
	}
	q, err := l.CreateQuote(ctx, "new-customer", "basic")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.AcceptQuote(ctx, q.ID, q.Fingerprint, "migration:new-buy"); !errors.Is(err, ErrConflict) {
		t.Fatalf("legacy writer allowed commerce checkout: %v", err)
	}
	quoteShadow, err := l.ShadowQuote(ctx, "legacy:new", "basic", 0, 2100, "USD")
	if err != nil || quoteShadow.Matched {
		t.Fatalf("wrong quote matched: %+v %v", quoteShadow, err)
	}
	if _, err := l.SwitchAccountRead(ctx, "legacy:new", cutoverLimits); !errors.Is(err, ErrConflict) {
		t.Fatalf("mismatched shadow passed: %v", err)
	}
	quoteShadow, err = l.ShadowQuote(ctx, "legacy:new", "basic", 0, 2000, "USD")
	if err != nil || !quoteShadow.Matched {
		t.Fatalf("quote shadow: %+v %v", quoteShadow, err)
	}
	entShadow, err := l.ShadowEntitlement(ctx, "legacy:new", "future-sub", "missing")
	if err != nil || !entShadow.Matched {
		t.Fatalf("entitlement shadow: %+v %v", entShadow, err)
	}
	if _, err := l.RunReconciliation(ctx, fixedNow); err != nil {
		t.Fatal(err)
	}
	read, err := l.ReadAccountEntitlement(ctx, "legacy:new", "future-sub")
	if err != nil || read.Owner != "legacy" || read.Status != "missing" {
		t.Fatalf("legacy adapter read: %+v %v", read, err)
	}
	if _, err := l.SwitchAccountWriter(ctx, "legacy:new", cutoverLimits); !errors.Is(err, ErrConflict) {
		t.Fatalf("writer switched before read: %v", err)
	}
	readLink, err := l.SwitchAccountRead(ctx, "legacy:new", cutoverLimits)
	if err != nil || readLink.ReadOwner != "commerce" || readLink.WriterOwner != "legacy" {
		t.Fatalf("read cutover: %+v %v", readLink, err)
	}
	writeLink, err := l.SwitchAccountWriter(ctx, "legacy:new", cutoverLimits)
	if err != nil || writeLink.WriterOwner != "commerce" {
		t.Fatalf("writer cutover: %+v %v", writeLink, err)
	}
	r, err := l.AcceptQuote(ctx, q.ID, q.Fingerprint, "migration:new-buy")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.DispatchNext(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if err := l.RebuildEntitlements(ctx); err != nil {
		t.Fatal(err)
	}
	read, err = l.ReadAccountEntitlement(ctx, "legacy:new", r.SubscriptionID)
	if err != nil || read.Owner != "commerce" || read.Status != "active" {
		t.Fatalf("commerce adapter read: %+v %v", read, err)
	}
	stopped, err := l.StopAccountMigration(ctx, "legacy:new", "shadow alert")
	if err != nil || !stopped.Stopped || stopped.WriterOwner != "commerce" || stopped.ReadOwner != "commerce" {
		t.Fatalf("stopped cutover lost writer state: %+v %v", stopped, err)
	}
	other, err := l.CreateQuote(ctx, "new-customer", "basic")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.AcceptQuote(ctx, other.ID, other.Fingerprint, "migration:second-buy"); !errors.Is(err, ErrConflict) {
		t.Fatalf("stopped account accepted new charge: %v", err)
	}
	if replay, err := l.AcceptQuote(ctx, q.ID, q.Fingerprint, "migration:new-buy"); err != nil || replay.SubscriptionID != r.SubscriptionID {
		t.Fatalf("original command replay: %+v %v", replay, err)
	}
}

func TestP03HistoricalProvenanceAndUnknownPaymentGate(t *testing.T) {
	l, _, _ := openTestLab(t)
	ctx := context.Background()
	q, err := l.CreateQuote(ctx, "historical-customer", "basic")
	if err != nil {
		t.Fatal(err)
	}
	r, err := l.AcceptQuote(ctx, q.ID, q.Fingerprint, "historic-buy")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.DispatchNext(ctx, "lost_response"); err != ErrPaymentUnknown {
		t.Fatalf("unknown result: %v", err)
	}
	if _, err := l.LinkLegacyAccount(ctx, AccountLink{LegacyAccountID: "legacy:history", CustomerID: "historical-customer", BeneficiaryID: "historical-customer", Cohort: "default", HasHistory: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.ShadowQuote(ctx, "legacy:history", "basic", 0, 2000, "USD"); err != nil {
		t.Fatal(err)
	}
	if _, err := l.ShadowEntitlement(ctx, "legacy:history", r.SubscriptionID, "pending"); err != nil {
		t.Fatal(err)
	}
	if _, err := l.RunReconciliation(ctx, fixedNow); err != nil {
		t.Fatal(err)
	}
	ready, err := l.MigrationReadiness(ctx, "legacy:history", MigrationThresholds{MaxQuoteP95Millis: 5000, MaxUnknownPayments: 10, MaxOpenDiscrepancies: 10})
	if err != nil || ready.Ready || ready.UnknownPayments != 1 || ready.ProvenanceComplete {
		t.Fatalf("unknown payment cutover readiness: %+v %v", ready, err)
	}
	bad, err := l.BackfillLegacyProvenance(ctx, LegacyProvenance{LegacyInvoiceID: "legacy:wrong", LegacySubscriptionID: "legacy-sub", LegacyAccountID: "legacy:history", CommerceSubscriptionID: r.SubscriptionID, CommerceInvoiceID: r.InvoiceID, PriceVersionID: "pro-v1"})
	if err != nil || bad.Status != "manual_review" {
		t.Fatalf("wrong backfill: %+v %v", bad, err)
	}
	if found, err := l.ReconcilePayment(ctx, r.OperationID); err != nil || !found {
		t.Fatalf("original provider lookup: %t %v", found, err)
	}
	if err := l.RebuildEntitlements(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := l.ShadowEntitlement(ctx, "legacy:history", r.SubscriptionID, "active"); err != nil {
		t.Fatal(err)
	}
	if _, err := l.RunReconciliation(ctx, fixedNow); err != nil {
		t.Fatal(err)
	}
	ready, err = l.MigrationReadiness(ctx, "legacy:history", cutoverLimits)
	if err != nil || ready.Ready || ready.UnknownPayments != 0 || ready.ProvenanceComplete {
		t.Fatalf("manual review did not block cutover: %+v %v", ready, err)
	}
	corrected, err := l.ResolveLegacyProvenance(ctx, LegacyProvenance{LegacyInvoiceID: "legacy:wrong", LegacySubscriptionID: "legacy-sub", LegacyAccountID: "legacy:history", CommerceSubscriptionID: r.SubscriptionID, CommerceInvoiceID: r.InvoiceID, PriceVersionID: "basic-v1"}, "reviewer-1", "verified original invoice")
	if err != nil || corrected.Status != "complete" {
		t.Fatalf("reviewed backfill: %+v %v", corrected, err)
	}
	ready, err = l.MigrationReadiness(ctx, "legacy:history", cutoverLimits)
	if err != nil || !ready.Ready {
		t.Fatalf("reviewed account not ready: %+v %v", ready, err)
	}
}
