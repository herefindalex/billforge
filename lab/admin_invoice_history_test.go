package lab

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestAdminInvoiceHistoryPagesBeyondDetailLimitWithoutRepeats(t *testing.T) {
	l, clock := openChangingLab(t)
	ctx := context.Background()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	paid := paidBasicSubscription(t, l, "invoice-history-customer")
	correctionIDs := make(map[string]bool)
	grantIDs := make(map[string]bool)
	for i := 0; i < 101; i++ {
		correction, err := l.PostReduction(ctx, paid.InvoiceID, 1, "history correction", fmt.Sprintf("invoice-history-%03d", i))
		if err != nil || len(correction.GrantIDs) != 1 {
			t.Fatalf("correction %d: %+v %v", i, correction, err)
		}
		correctionIDs[correction.ID] = true
		grantIDs[correction.GrantIDs[0]] = true
	}
	detail, err := l.AdminInvoiceDetail(ctx, paid.InvoiceID)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Corrections) != 100 || !detail.CorrectionsTruncated || len(detail.CreditGrants) != 100 || !detail.CreditGrantsTruncated {
		t.Fatalf("detail did not expose the history limit: corrections=%d/%v grants=%d/%v", len(detail.Corrections), detail.CorrectionsTruncated, len(detail.CreditGrants), detail.CreditGrantsTruncated)
	}
	first, err := l.AdminInvoiceHistory(ctx, paid.InvoiceID, "corrections", nil, 50)
	if err != nil || first.Next == nil {
		t.Fatalf("first page: %+v %v", first, err)
	}
	grants, err := l.AdminInvoiceHistory(ctx, paid.InvoiceID, "grants", nil, 50)
	if err != nil || grants.Next == nil {
		t.Fatalf("grant first page: %+v %v", grants, err)
	}
	*clock = clock.Add(time.Second)
	if _, err := l.PostReduction(ctx, paid.InvoiceID, 1, "newer correction", "invoice-history-newer"); err != nil {
		t.Fatal(err)
	}
	assertInvoiceHistoryIDs(t, l, paid.InvoiceID, "corrections", first, correctionIDs, func(item AdminInvoiceCorrection) string { return item.ID })
	assertInvoiceHistoryIDs(t, l, paid.InvoiceID, "grants", grants, grantIDs, func(item AdminInvoiceCreditGrant) string { return item.ID })
	if _, err := l.AdminInvoiceHistory(ctx, paid.InvoiceID, "grants", first.Next, 20); !errors.Is(err, ErrConflict) {
		t.Fatalf("cross-kind cursor accepted: %v", err)
	}
	if _, err := l.AdminInvoiceHistory(ctx, "missing-invoice", "corrections", nil, 20); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing invoice: %v", err)
	}
	if _, err := l.AdminInvoiceHistory(ctx, paid.InvoiceID, "unknown", nil, 20); !errors.Is(err, ErrAdminUnsupportedAction) {
		t.Fatalf("unknown kind: %v", err)
	}
}

func TestAdminInvoiceApplicationAndRefundHistoryPagesBeyondDetailLimit(t *testing.T) {
	l, clock := openChangingLab(t)
	ctx := context.Background()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	paid := paidBasicSubscription(t, l, "invoice-spending-history-customer")
	correction, err := l.PostReduction(ctx, paid.InvoiceID, 1000, "history credit", "invoice-spending-history-credit")
	if err != nil || len(correction.GrantIDs) != 1 {
		t.Fatalf("credit: %+v %v", correction, err)
	}
	grantID := correction.GrantIDs[0]
	*clock = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	renewals, err := l.RunRenewals(ctx)
	if err != nil || len(renewals) != 1 {
		t.Fatalf("renewals: %+v %v", renewals, err)
	}
	targetID := renewals[0].InvoiceID
	applicationIDs := make(map[string]bool)
	refundIDs := make(map[string]bool)
	for i := 0; i < 101; i++ {
		applicationID, err := l.ApplyCredit(ctx, grantID, targetID, 1, fmt.Sprintf("history-application-%03d", i))
		if err != nil {
			t.Fatalf("application %d: %v", i, err)
		}
		applicationIDs[applicationID] = true
		refundID, err := l.ReserveRefund(ctx, grantID, 1, fmt.Sprintf("history-refund-%03d", i))
		if err != nil {
			t.Fatalf("refund %d: %v", i, err)
		}
		refundIDs[refundID] = true
	}
	source, err := l.AdminInvoiceDetail(ctx, paid.InvoiceID)
	if err != nil || len(source.Refunds) != 100 || !source.RefundsTruncated {
		t.Fatalf("refund detail limit: %+v %v", source, err)
	}
	target, err := l.AdminInvoiceDetail(ctx, targetID)
	if err != nil || len(target.CreditApplications) != 100 || !target.CreditApplicationsTruncated {
		t.Fatalf("application detail limit: %+v %v", target, err)
	}
	applications, err := l.AdminInvoiceHistory(ctx, targetID, "applications", nil, 50)
	if err != nil || applications.Next == nil {
		t.Fatalf("application first page: %+v %v", applications, err)
	}
	refunds, err := l.AdminInvoiceHistory(ctx, paid.InvoiceID, "refunds", nil, 50)
	if err != nil || refunds.Next == nil {
		t.Fatalf("refund first page: %+v %v", refunds, err)
	}
	*clock = clock.Add(time.Second)
	if _, err := l.ApplyCredit(ctx, grantID, targetID, 1, "history-application-newer"); err != nil {
		t.Fatal(err)
	}
	if _, err := l.ReserveRefund(ctx, grantID, 1, "history-refund-newer"); err != nil {
		t.Fatal(err)
	}
	assertInvoiceHistoryIDs(t, l, targetID, "applications", applications, applicationIDs, func(item AdminInvoiceCreditApplication) string { return item.ID })
	assertInvoiceHistoryIDs(t, l, paid.InvoiceID, "refunds", refunds, refundIDs, func(item AdminInvoiceRefund) string { return item.ID })
}

func assertInvoiceHistoryIDs[T any](t *testing.T, l *Lab, invoiceID, kind string, first AdminInvoiceHistoryPage, expected map[string]bool, identity func(T) string) {
	t.Helper()
	seen := make(map[string]bool)
	for page := first; ; {
		items := page.Items.([]T)
		if len(items) > 50 {
			t.Fatalf("%s page exceeds limit: %d", kind, len(items))
		}
		for _, item := range items {
			id := identity(item)
			if seen[id] || !expected[id] {
				t.Fatalf("%s history returned repeated or newly inserted row %s", kind, id)
			}
			seen[id] = true
		}
		if page.Next == nil {
			break
		}
		var err error
		page, err = l.AdminInvoiceHistory(context.Background(), invoiceID, kind, page.Next, 50)
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(seen) != len(expected) {
		t.Fatalf("%s history rows=%d, want %d", kind, len(seen), len(expected))
	}
}
