package lab

import (
	"context"
	"errors"
	"testing"
)

func TestS11UncertainCaptureBlocksReductionUntilReconciled(t *testing.T) {
	l, _, _ := openTestLab(t)
	ctx := context.Background()
	r := purchaseHundred(t, l, "customer-a", "uncertain-correction-checkout")
	if _, err := l.DispatchNext(ctx, "lost_response"); !errors.Is(err, ErrPaymentUnknown) {
		t.Fatalf("expected unknown capture: %v", err)
	}
	if _, err := l.PostReduction(ctx, r.InvoiceID, 2000, "service correction", "uncertain-correction"); !errors.Is(err, ErrConflict) {
		t.Fatalf("reduction while capture is unknown: %v", err)
	}
	if b, err := l.Balance(ctx, r.InvoiceID); err != nil || b.ObligationMinor != 10000 || b.GrossCapturedMinor != 0 {
		t.Fatalf("rejected reduction changed local facts: %+v, %v", b, err)
	}
	if found, err := l.ReconcilePayment(ctx, r.OperationID); err != nil || !found {
		t.Fatalf("capture lookup: found=%v err=%v", found, err)
	}
	c, err := l.PostReduction(ctx, r.InvoiceID, 2000, "service correction", "uncertain-correction")
	if err != nil || len(c.GrantIDs) != 1 {
		t.Fatalf("reduction after confirmed capture: %+v, %v", c, err)
	}
}

func TestS11NewPaymentDoesNotSilentlyCancelCallerOperation(t *testing.T) {
	l, _, _ := openTestLab(t)
	ctx := context.Background()
	r := purchaseHundred(t, l, "customer-a", "caller-payment-checkout")
	op, err := l.CreatePayment(ctx, r.InvoiceID, 6000, "pay-sixty")
	if err != nil {
		t.Fatal(err)
	}
	if repeated, err := l.CreatePayment(ctx, r.InvoiceID, 6000, "pay-sixty"); err != nil || repeated != op {
		t.Fatalf("payment request replay: %q, %v", repeated, err)
	}
	if _, err := l.CreatePayment(ctx, r.InvoiceID, 4000, "pay-forty-too-early"); !errors.Is(err, ErrConflict) {
		t.Fatalf("second request silently replaced pending payment: %v", err)
	}
	var status string
	if err := l.db.QueryRow(`SELECT status FROM payment_operations WHERE id=?`, op).Scan(&status); err != nil || status != "created" {
		t.Fatalf("first payment no longer dispatchable: status=%q err=%v", status, err)
	}
	if _, err := l.DispatchNext(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := l.CreatePayment(ctx, r.InvoiceID, 4000, "pay-forty"); err != nil {
		t.Fatalf("payment after prior capture: %v", err)
	}
}

func TestS11ReductionThatClosesInitialReceivableActivatesSubscription(t *testing.T) {
	l, _, _ := openTestLab(t)
	ctx := context.Background()
	r := purchaseHundred(t, l, "customer-a", "fully-funded-by-reduction")
	if _, err := l.CreatePayment(ctx, r.InvoiceID, 6000, "pay-sixty-before-reduction"); err != nil {
		t.Fatal(err)
	}
	if _, err := l.DispatchNext(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if got := snapshot(t, l, r.SubscriptionID); got.SubscriptionStatus != "pending" {
		t.Fatalf("partial payment unexpectedly activated subscription: %+v", got)
	}
	c, err := l.PostReduction(ctx, r.InvoiceID, 4000, "service correction", "close-receivable")
	if err != nil || len(c.GrantIDs) != 0 {
		t.Fatalf("reduction to captured amount: %+v, %v", c, err)
	}
	if err := l.RefreshEntitlements(ctx); err != nil {
		t.Fatal(err)
	}
	if got := snapshot(t, l, r.SubscriptionID); got.SubscriptionStatus != "active" || got.EntitlementStatus != "active" {
		t.Fatalf("fully funded correction did not activate service: %+v", got)
	}
	if b, err := l.Balance(ctx, r.InvoiceID); err != nil || b.ObligationMinor != 6000 || b.NetAppliedMinor != 6000 || b.OutstandingMinor != 0 {
		t.Fatalf("closed receivable: %+v, %v", b, err)
	}
}

func TestS11GrantRejectsForeignCustomerAndDirectSQLOvercommit(t *testing.T) {
	l, _, _ := openTestLab(t)
	ctx := context.Background()
	a := purchaseHundred(t, l, "customer-a", "grant-owner-checkout")
	if _, err := l.DispatchNext(ctx, ""); err != nil {
		t.Fatal(err)
	}
	c, err := l.PostReduction(ctx, a.InvoiceID, 2000, "service correction", "grant-owner-correction")
	if err != nil || len(c.GrantIDs) != 1 {
		t.Fatalf("funded grant: %+v, %v", c, err)
	}
	grant := c.GrantIDs[0]
	b := purchaseHundred(t, l, "customer-b", "foreign-customer-checkout")
	if _, err := l.ApplyCredit(ctx, grant, b.InvoiceID, 1000, "foreign-credit"); !errors.Is(err, ErrConflict) {
		t.Fatalf("foreign customer spent grant: %v", err)
	}
	id, err := l.ReserveRefund(ctx, grant, 1500, "refund-fifteen")
	if err != nil {
		t.Fatal(err)
	}
	if repeated, err := l.ReserveRefund(ctx, grant, 1500, "refund-fifteen"); err != nil || repeated != id {
		t.Fatalf("refund request replay: %q, %v", repeated, err)
	}
	if _, err := l.ReserveRefund(ctx, grant, 1000, "refund-fifteen"); !errors.Is(err, ErrConflict) {
		t.Fatalf("refund request key accepted changed amount: %v", err)
	}
	var sourceKey, currency string
	if err := l.db.QueryRow(`SELECT source_provider_key,currency FROM refund_operations WHERE id=?`, id).Scan(&sourceKey, &currency); err != nil {
		t.Fatal(err)
	}
	if _, err := l.db.Exec(`INSERT INTO refund_operations(id,grant_id,provider_key,source_provider_key,amount_minor,currency,status,request_key,created_at) VALUES(?,?,?,?,?,?,'created',?,0)`, "refund-direct-overbudget", grant, "refund:direct-overbudget", sourceKey, 600, currency, "direct-overbudget"); err == nil {
		t.Fatal("SQLite accepted refund reservations exceeding the grant")
	}
	if balance, err := l.CreditBalance(ctx, grant); err != nil || balance.ReservedMinor != 1500 || balance.AvailableMinor != 500 {
		t.Fatalf("grant budget after rejected insert: %+v, %v", balance, err)
	}
}
