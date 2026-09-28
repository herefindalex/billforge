package lab

import (
	"context"
	"database/sql"
	"errors"
)

type rowQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type InvoiceBalance struct {
	InvoiceID          string
	Currency           string
	OriginalMinor      int64
	ReductionsMinor    int64
	ObligationMinor    int64
	GrossCapturedMinor int64
	ReleasedMinor      int64
	CreditAppliedMinor int64
	NetAppliedMinor    int64
	OutstandingMinor   int64
}

func loadInvoiceBalance(ctx context.Context, q rowQuerier, invoiceID string) (InvoiceBalance, error) {
	b := InvoiceBalance{InvoiceID: invoiceID}
	err := q.QueryRowContext(ctx, `SELECT i.currency,i.total_minor,
COALESCE((SELECT SUM(c.reduction_minor) FROM corrections c WHERE c.invoice_id=i.id),0),
COALESCE((SELECT SUM(a.amount_minor) FROM allocations a WHERE a.invoice_id=i.id),0),
COALESCE((SELECT SUM(r.amount_minor) FROM allocation_releases r
JOIN allocations a ON a.operation_id=r.operation_id WHERE a.invoice_id=i.id),0),
COALESCE((SELECT SUM(ca.amount_minor) FROM credit_applications ca WHERE ca.invoice_id=i.id),0)
FROM invoices i WHERE i.id=?`, invoiceID).
		Scan(&b.Currency, &b.OriginalMinor, &b.ReductionsMinor,
			&b.GrossCapturedMinor, &b.ReleasedMinor, &b.CreditAppliedMinor)
	if err != nil {
		return InvoiceBalance{}, err
	}
	b.ObligationMinor = b.OriginalMinor - b.ReductionsMinor
	b.NetAppliedMinor = b.GrossCapturedMinor - b.ReleasedMinor + b.CreditAppliedMinor
	if b.ObligationMinor < 0 || b.NetAppliedMinor < 0 || b.NetAppliedMinor > b.ObligationMinor {
		return InvoiceBalance{}, errors.New("invoice financial invariant violated")
	}
	b.OutstandingMinor = b.ObligationMinor - b.NetAppliedMinor
	return b, nil
}

func (l *Lab) Balance(ctx context.Context, invoiceID string) (InvoiceBalance, error) {
	return loadInvoiceBalance(ctx, l.db, invoiceID)
}

type CreditBalance struct {
	GrantID        string
	Currency       string
	GrantedMinor   int64
	AppliedMinor   int64
	ReservedMinor  int64
	RefundedMinor  int64
	AvailableMinor int64
}

func loadCreditBalance(ctx context.Context, q rowQuerier, grantID string) (CreditBalance, error) {
	b := CreditBalance{GrantID: grantID}
	err := q.QueryRowContext(ctx, `SELECT g.currency,g.amount_minor,
COALESCE((SELECT SUM(a.amount_minor) FROM credit_applications a WHERE a.grant_id=g.id),0),
COALESCE((SELECT SUM(r.amount_minor) FROM refund_operations r WHERE r.grant_id=g.id AND r.status IN ('created','submitted','unknown')),0),
COALESCE((SELECT SUM(r.amount_minor) FROM refund_operations r WHERE r.grant_id=g.id AND r.status='succeeded'),0)
FROM credit_grants g WHERE g.id=?`, grantID).
		Scan(&b.Currency, &b.GrantedMinor, &b.AppliedMinor, &b.ReservedMinor, &b.RefundedMinor)
	if err != nil {
		return CreditBalance{}, err
	}
	b.AvailableMinor = b.GrantedMinor - b.AppliedMinor - b.ReservedMinor - b.RefundedMinor
	if b.AvailableMinor < 0 {
		return CreditBalance{}, errors.New("credit grant budget violated")
	}
	return b, nil
}

func (l *Lab) CreditBalance(ctx context.Context, grantID string) (CreditBalance, error) {
	return loadCreditBalance(ctx, l.db, grantID)
}
