package lab

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func purchaseHundred(t *testing.T, l *Lab, customer, key string) Receipt {
	t.Helper()
	ctx := context.Background()
	_, err := l.db.Exec(`INSERT INTO price_versions(id,plan_id,version,currency,fixed_amount_minor,published_at,checksum)
VALUES('hundred-v1','hundred',1,'USD',10000,0,'hundred-v1-checksum') ON CONFLICT(id) DO NOTHING;
INSERT INTO catalog_selection(plan_id,cohort,effective_at,price_version_id)
VALUES('hundred','default',0,'hundred-v1') ON CONFLICT(plan_id,cohort,effective_at) DO NOTHING;`)
	if err != nil {
		t.Fatal(err)
	}
	q, err := l.CreateQuote(ctx, customer, "hundred")
	if err != nil || q.AmountMinor != 10000 {
		t.Fatalf("hundred quote: %+v, %v", q, err)
	}
	r, err := l.AcceptQuote(ctx, q.ID, q.Fingerprint, key)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestS11UnpaidReductionCreatesNoFundedCredit(t *testing.T) {
	l, _, _ := openTestLab(t)
	ctx := context.Background()
	r := purchaseHundred(t, l, "customer-a", "unpaid-checkout")
	c, err := l.PostReduction(ctx, r.InvoiceID, 2000, "service correction", "unpaid-correction")
	if err != nil || len(c.GrantIDs) != 0 || c.PriorObligationMinor != 10000 || c.NewObligationMinor != 8000 {
		t.Fatalf("unpaid correction: %+v, %v", c, err)
	}
	if repeated, err := l.PostReduction(ctx, r.InvoiceID, 2000, "service correction", "unpaid-correction"); err != nil || repeated.ID != c.ID {
		t.Fatalf("correction replay: %+v, %v", repeated, err)
	}
	if _, err := l.PostReduction(ctx, r.InvoiceID, 1000, "different", "unpaid-correction"); !errors.Is(err, ErrConflict) {
		t.Fatalf("same key changed payload: %v", err)
	}
	b, err := l.Balance(ctx, r.InvoiceID)
	if err != nil || b.OutstandingMinor != 8000 || b.NetAppliedMinor != 0 {
		t.Fatalf("unpaid balance: %+v, %v", b, err)
	}
	if _, err := l.DispatchNext(ctx, ""); !errors.Is(err, sql.ErrNoRows) || captureCount(t, l) != 0 {
		t.Fatalf("old $100 operation was not cancelled: %v", err)
	}
	op, err := l.CreatePayment(ctx, r.InvoiceID, 8000, "pay-corrected")
	if err != nil || op == r.OperationID {
		t.Fatalf("corrected operation: %q, %v", op, err)
	}
	if _, err := l.DispatchNext(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if got := snapshot(t, l, r.SubscriptionID); got.SubscriptionStatus != "active" || got.AllocatedMinor != 8000 {
		t.Fatalf("corrected purchase not active: %+v", got)
	}
}

func TestS11PartPaidReductionOnlyReducesReceivable(t *testing.T) {
	l, _, _ := openTestLab(t)
	ctx := context.Background()
	r := purchaseHundred(t, l, "customer-a", "partial-checkout")
	partialOp, err := l.CreatePayment(ctx, r.InvoiceID, 6000, "pay-sixty")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.DispatchNext(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if s := snapshot(t, l, r.SubscriptionID); s.SubscriptionStatus != "pending" || s.AllocatedMinor != 6000 {
		t.Fatalf("part payment activated service: %+v", s)
	}
	c, err := l.PostReduction(ctx, r.InvoiceID, 2000, "service correction", "partial-correction")
	if err != nil || len(c.GrantIDs) != 0 {
		t.Fatalf("partial correction created cash credit: %+v, %v", c, err)
	}
	b, err := l.Balance(ctx, r.InvoiceID)
	if err != nil || b.ObligationMinor != 8000 || b.NetAppliedMinor != 6000 || b.OutstandingMinor != 2000 {
		t.Fatalf("partial balance: %+v, %v", b, err)
	}
	op, err := l.CreatePayment(ctx, r.InvoiceID, 2000, "pay-remainder")
	if err != nil || op == partialOp {
		t.Fatalf("remainder operation: %q, %v", op, err)
	}
	if _, err := l.DispatchNext(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if got := snapshot(t, l, r.SubscriptionID); got.SubscriptionStatus != "active" || got.AllocatedMinor != 8000 {
		t.Fatalf("remainder not applied: %+v", got)
	}
}

func TestS11FundedCreditSharesApplicationAndRefundBudget(t *testing.T) {
	dir := t.TempDir()
	now := fixedNow
	l, err := Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	ctx := context.Background()
	r := purchaseHundred(t, l, "customer-a", "full-checkout")
	if _, err := l.DispatchNext(ctx, ""); err != nil {
		t.Fatal(err)
	}
	c, err := l.PostReduction(ctx, r.InvoiceID, 2000, "service correction", "full-correction")
	if err != nil || len(c.GrantIDs) != 1 {
		t.Fatalf("full correction: %+v, %v", c, err)
	}
	grant := c.GrantIDs[0]
	b, err := l.Balance(ctx, r.InvoiceID)
	if err != nil || b.OriginalMinor != 10000 || b.ObligationMinor != 8000 || b.GrossCapturedMinor != 10000 || b.ReleasedMinor != 2000 || b.NetAppliedMinor != 8000 || b.OutstandingMinor != 0 {
		t.Fatalf("funded correction ledger: %+v, %v", b, err)
	}
	credit, err := l.CreditBalance(ctx, grant)
	if err != nil || credit.GrantedMinor != 2000 || credit.AvailableMinor != 2000 {
		t.Fatalf("funded grant: %+v, %v", credit, err)
	}
	now = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	renewed, err := l.RunRenewals(ctx)
	if err != nil || len(renewed) != 1 {
		t.Fatalf("renewal: %+v, %v", renewed, err)
	}
	if _, err := l.ApplyCredit(ctx, grant, renewed[0].InvoiceID, 1000, "apply-ten"); err != nil {
		t.Fatal(err)
	}
	if _, err := l.DispatchNext(ctx, ""); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("credit application left stale full-price capture dispatchable: %v", err)
	}
	target, err := l.Balance(ctx, renewed[0].InvoiceID)
	if err != nil || target.CreditAppliedMinor != 1000 || target.OutstandingMinor != 9000 {
		t.Fatalf("credit application: %+v, %v", target, err)
	}
	if _, err := l.CreatePayment(ctx, renewed[0].InvoiceID, 9000, "pay-after-credit"); err != nil {
		t.Fatal(err)
	}
	if _, err := l.DispatchNext(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if err := l.RefreshEntitlements(ctx); err != nil {
		t.Fatal(err)
	}
	if got := snapshot(t, l, r.SubscriptionID).EntitlementStatus; got != "active" {
		t.Fatalf("credit plus capture did not fund renewal: %q", got)
	}
	if _, err := l.ReserveRefund(ctx, grant, 1500, "too-much-after-apply"); !errors.Is(err, ErrConflict) {
		t.Fatalf("credit and refund overcommitted grant: %v", err)
	}
	refundID, err := l.ReserveRefund(ctx, grant, 1000, "refund-ten")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.DispatchRefundNext(ctx, "lost_response"); !errors.Is(err, ErrPaymentUnknown) {
		t.Fatalf("refund response should be unknown: %v", err)
	}
	credit, err = l.CreditBalance(ctx, grant)
	if err != nil || credit.AppliedMinor != 1000 || credit.ReservedMinor != 1000 || credit.AvailableMinor != 0 {
		t.Fatalf("unknown refund released budget: %+v, %v", credit, err)
	}
	if _, err := l.ReserveRefund(ctx, grant, 1, "second-refund"); !errors.Is(err, ErrConflict) {
		t.Fatalf("second refund admitted while unknown: %v", err)
	}
	if found, err := l.ReconcileRefund(ctx, refundID); err != nil || !found {
		t.Fatalf("refund lookup: found=%v err=%v", found, err)
	}
	credit, err = l.CreditBalance(ctx, grant)
	if err != nil || credit.RefundedMinor != 1000 || credit.ReservedMinor != 0 || credit.AvailableMinor != 0 {
		t.Fatalf("refund budget after success: %+v, %v", credit, err)
	}
	var key string
	if err := l.db.QueryRow(`SELECT provider_key FROM refund_operations WHERE id=?`, refundID).Scan(&key); err != nil {
		t.Fatal(err)
	}
	var source string
	if err := l.provider.db.QueryRow(`SELECT source_capture_key FROM refunds WHERE provider_key=?`, key).Scan(&source); err != nil {
		t.Fatal(err)
	}
	if source != "capture:"+r.InvoiceID {
		t.Fatalf("refund lost original capture attribution: %q", source)
	}
}

func TestRefundFailureReleasesReservationAndConcurrentRequestsStayBounded(t *testing.T) {
	dir := t.TempDir()
	local, provider := filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db")
	l, err := Open(local, provider, func() time.Time { return fixedNow })
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	ctx := context.Background()
	r := purchaseHundred(t, l, "customer-a", "refund-budget-checkout")
	if _, err := l.DispatchNext(ctx, ""); err != nil {
		t.Fatal(err)
	}
	c, err := l.PostReduction(ctx, r.InvoiceID, 2000, "service correction", "refund-budget-correction")
	if err != nil {
		t.Fatal(err)
	}
	grant := c.GrantIDs[0]
	failedID, err := l.ReserveRefund(ctx, grant, 1500, "fail-fifteen")
	if err != nil {
		t.Fatal(err)
	}
	if err := l.SetFakeRefundDecision(ctx, failedID, "definitively_failed"); err != nil {
		t.Fatal(err)
	}
	if _, err := l.DispatchRefundNext(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if got, err := l.CreditBalance(ctx, grant); err != nil || got.AvailableMinor != 2000 {
		t.Fatalf("failed refund retained reservation: %+v, %v", got, err)
	}
	l2, err := Open(local, provider, func() time.Time { return fixedNow })
	if err != nil {
		t.Fatal(err)
	}
	defer l2.Close()
	start := make(chan struct{})
	var wg sync.WaitGroup
	var results [2]error
	for i, client := range []*Lab{l, l2} {
		wg.Add(1)
		go func(i int, client *Lab) {
			defer wg.Done()
			<-start
			_, results[i] = client.ReserveRefund(ctx, grant, 1500, "parallel-"+string(rune('a'+i)))
		}(i, client)
	}
	close(start)
	wg.Wait()
	successes := 0
	for _, err := range results {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent admissions: successes=%d errors=%v", successes, results)
	}
	if got, err := l.CreditBalance(ctx, grant); err != nil || got.ReservedMinor != 1500 || got.AvailableMinor != 500 {
		t.Fatalf("concurrent budget exceeded: %+v, %v", got, err)
	}
}
