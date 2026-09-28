package lab

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"
)

func TestAdminInvoiceDetailReadsOneConsistentMonetarySnapshot(t *testing.T) {
	l, _ := openChangingLab(t)
	ctx := context.Background()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	receipt := paidBasicSubscription(t, l, "invoice-detail-customer")
	detail, err := l.AdminInvoiceDetail(ctx, receipt.InvoiceID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.ID != receipt.InvoiceID || detail.SubscriptionID != receipt.SubscriptionID || detail.FinalizedAt == nil {
		t.Fatalf("invoice identity/finalization = %+v", detail)
	}
	if detail.Period == nil || detail.Period.Index != 0 || detail.Period.InvoiceID != receipt.InvoiceID {
		t.Fatalf("invoice period = %+v", detail.Period)
	}
	if detail.Balance.OriginalMinor <= 0 || detail.Balance.OutstandingMinor != 0 || detail.Balance.NetAppliedMinor != detail.Balance.ObligationMinor {
		t.Fatalf("invoice balance = %+v", detail.Balance)
	}
	if len(detail.Lines) == 0 || len(detail.Payments) != 1 || detail.Payments[0].ID != receipt.OperationID || detail.Payments[0].Status != "succeeded" || detail.PaymentsTruncated {
		t.Fatalf("invoice lines/payments = %+v", detail)
	}
	if _, err := l.AdminInvoiceDetail(ctx, "missing-invoice"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing invoice error = %v", err)
	}
}

func TestAdminInvoiceDetailLinksCorrectionsCreditAndRefunds(t *testing.T) {
	l, clock := openChangingLab(t)
	ctx := context.Background()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	paid := paidBasicSubscription(t, l, "invoice-provenance-customer")
	correction, err := l.PostReduction(ctx, paid.InvoiceID, 1000, "service reduction", "invoice-provenance-reduction")
	if err != nil || len(correction.GrantIDs) != 1 {
		t.Fatalf("correction: %+v %v", correction, err)
	}
	grantID := correction.GrantIDs[0]
	refundID, err := l.ReserveRefund(ctx, grantID, 250, "invoice-provenance-refund")
	if err != nil {
		t.Fatal(err)
	}
	*clock = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	renewals, err := l.RunRenewals(ctx)
	if err != nil || len(renewals) != 1 {
		t.Fatalf("renewals: %+v %v", renewals, err)
	}
	applicationID, err := l.ApplyCredit(ctx, grantID, renewals[0].InvoiceID, 500, "invoice-provenance-apply")
	if err != nil {
		t.Fatal(err)
	}

	source, err := l.AdminInvoiceDetail(ctx, paid.InvoiceID)
	if err != nil {
		t.Fatal(err)
	}
	if len(source.Corrections) != 1 || source.Corrections[0].ID != correction.ID || source.Corrections[0].Reason != "service reduction" || source.Corrections[0].ReductionMinor != 1000 || source.Corrections[0].OriginKind != "domain_request" || source.Corrections[0].OriginID != "invoice-provenance-reduction" {
		t.Fatalf("source corrections: %+v", source.Corrections)
	}
	if len(source.CreditGrants) != 1 || source.CreditGrants[0].ID != grantID || source.CreditGrants[0].CorrectionID != correction.ID || source.CreditGrants[0].SourceOperationID != paid.OperationID {
		t.Fatalf("source grants: %+v", source.CreditGrants)
	}
	if len(source.Refunds) != 1 || source.Refunds[0].ID != refundID || source.Refunds[0].GrantID != grantID || source.Refunds[0].AmountMinor != 250 || source.Refunds[0].Status != "created" {
		t.Fatalf("source refunds: %+v", source.Refunds)
	}
	if len(source.CreditApplications) != 0 || source.CorrectionsTruncated || source.CreditGrantsTruncated || source.RefundsTruncated {
		t.Fatalf("unexpected source provenance: %+v", source)
	}
	target, err := l.AdminInvoiceDetail(ctx, renewals[0].InvoiceID)
	if err != nil {
		t.Fatal(err)
	}
	if len(target.CreditApplications) != 1 || target.CreditApplications[0].ID != applicationID || target.CreditApplications[0].GrantID != grantID || target.CreditApplications[0].SourceInvoiceID != paid.InvoiceID || target.CreditApplications[0].AmountMinor != 500 {
		t.Fatalf("target applications: %+v", target.CreditApplications)
	}
	if target.Balance.CreditAppliedMinor != 500 || len(target.Corrections) != 0 || len(target.CreditGrants) != 0 || len(target.Refunds) != 0 || target.CreditApplicationsTruncated {
		t.Fatalf("unexpected target provenance: %+v", target)
	}
	applications, err := l.AdminInvoiceHistory(ctx, renewals[0].InvoiceID, "applications", nil, 20)
	if err != nil || applications.Next != nil || len(applications.Items.([]AdminInvoiceCreditApplication)) != 1 || applications.Items.([]AdminInvoiceCreditApplication)[0].ID != applicationID {
		t.Fatalf("application history: %+v %v", applications, err)
	}
	refunds, err := l.AdminInvoiceHistory(ctx, paid.InvoiceID, "refunds", nil, 20)
	if err != nil || refunds.Next != nil || len(refunds.Items.([]AdminInvoiceRefund)) != 1 || refunds.Items.([]AdminInvoiceRefund)[0].ID != refundID {
		t.Fatalf("refund history: %+v %v", refunds, err)
	}
	if _, err := l.ReserveRefund(ctx, grantID, 100, "invoice-provenance-refund-second"); err != nil {
		t.Fatal(err)
	}
	if _, err := l.ApplyCredit(ctx, grantID, renewals[0].InvoiceID, 100, "invoice-provenance-apply-second"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ kind string }{{"applications"}, {"refunds"}} {
		invoiceID := paid.InvoiceID
		if tc.kind == "applications" {
			invoiceID = renewals[0].InvoiceID
		}
		first, err := l.AdminInvoiceHistory(ctx, invoiceID, tc.kind, nil, 1)
		if err != nil || first.Next == nil {
			t.Fatalf("%s first page: %+v %v", tc.kind, first, err)
		}
		second, err := l.AdminInvoiceHistory(ctx, invoiceID, tc.kind, first.Next, 1)
		if err != nil || second.Next != nil {
			t.Fatalf("%s second page: %+v %v", tc.kind, second, err)
		}
		switch tc.kind {
		case "applications":
			a := first.Items.([]AdminInvoiceCreditApplication)
			b := second.Items.([]AdminInvoiceCreditApplication)
			if len(a) != 1 || len(b) != 1 || a[0].ID == b[0].ID {
				t.Fatalf("application cursor repeated: %+v %+v", a, b)
			}
		case "refunds":
			a := first.Items.([]AdminInvoiceRefund)
			b := second.Items.([]AdminInvoiceRefund)
			if len(a) != 1 || len(b) != 1 || a[0].ID == b[0].ID {
				t.Fatalf("refund cursor repeated: %+v %+v", a, b)
			}
		}
	}
}
