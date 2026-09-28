package lab

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestAdminCreditDetailSeparatesAvailableReservedAndRefunded(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	now := fixedNow
	l, err := Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	purchase := purchaseHundred(t, l, "credit-detail-customer", "credit-detail-checkout")
	if _, err := l.DispatchNext(ctx, ""); err != nil {
		t.Fatal(err)
	}
	correction, err := l.PostReduction(ctx, purchase.InvoiceID, 1000, "credit detail", "credit-detail-reduction")
	if err != nil || len(correction.GrantIDs) != 1 {
		t.Fatalf("correction: %+v %v", correction, err)
	}
	grantID := correction.GrantIDs[0]
	check := func(applied, reserved, refunded, available int64) {
		t.Helper()
		detail, err := l.AdminCreditDetail(ctx, grantID)
		if err != nil {
			t.Fatal(err)
		}
		if detail.ID != grantID || detail.SourceInvoiceID != purchase.InvoiceID || detail.SourceCorrectionID != correction.ID || detail.SourceOperationID != purchase.OperationID || detail.ReleaseID == "" || detail.CreatedAt.IsZero() {
			t.Fatalf("credit provenance: %+v", detail)
		}
		b := detail.Balance
		if b.GrantID != grantID || b.Currency != "USD" || b.GrantedMinor != 1000 || b.AppliedMinor != applied || b.ReservedMinor != reserved || b.RefundedMinor != refunded || b.AvailableMinor != available {
			t.Fatalf("credit balance: %+v", b)
		}
	}
	check(0, 0, 0, 1000)
	refundID, err := l.ReserveRefund(ctx, grantID, 400, "credit-detail-refund")
	if err != nil {
		t.Fatal(err)
	}
	check(0, 400, 0, 600)
	if _, err := l.DispatchRefund(ctx, refundID, "lost_response"); !errors.Is(err, ErrPaymentUnknown) {
		t.Fatalf("lost refund response: %v", err)
	}
	check(0, 400, 0, 600)
	found, err := l.ReconcileRefund(ctx, refundID)
	if err != nil || !found {
		t.Fatalf("reconcile refund: found=%t err=%v", found, err)
	}
	check(0, 0, 400, 600)
	now = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	renewals, err := l.RunRenewals(ctx)
	if err != nil || len(renewals) != 1 {
		t.Fatalf("renewals: %+v %v", renewals, err)
	}
	if _, err := l.ApplyCredit(ctx, grantID, renewals[0].InvoiceID, 200, "credit-detail-application"); err != nil {
		t.Fatal(err)
	}
	check(200, 0, 400, 400)
	if _, err := l.AdminCreditDetail(ctx, "missing"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing credit: %v", err)
	}
}
