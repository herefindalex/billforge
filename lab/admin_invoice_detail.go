package lab

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// AdminInvoiceDetail reads one invoice and its monetary state from one SQLite snapshot.
// Related records are bounded; truncation flags make the limits visible to callers.
type AdminInvoiceDetail struct {
	ID                          string
	SubscriptionID              string
	FinalizedAt                 *time.Time
	Period                      *BillingPeriod
	Balance                     InvoiceBalance
	Lines                       []InvoiceLine
	Payments                    []AdminInvoicePayment
	PaymentsTruncated           bool
	Corrections                 []AdminInvoiceCorrection
	CorrectionsTruncated        bool
	CreditApplications          []AdminInvoiceCreditApplication
	CreditApplicationsTruncated bool
	CreditGrants                []AdminInvoiceCreditGrant
	CreditGrantsTruncated       bool
	Refunds                     []AdminInvoiceRefund
	RefundsTruncated            bool
}

type AdminInvoicePayment struct {
	ID          string
	AmountMinor int64
	Currency    string
	Status      string
}

func (l *Lab) AdminInvoiceDetail(ctx context.Context, id string) (AdminInvoiceDetail, error) {
	tx, err := l.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return AdminInvoiceDetail{}, err
	}
	defer tx.Rollback()
	detail := AdminInvoiceDetail{Lines: []InvoiceLine{}, Payments: []AdminInvoicePayment{}}
	var finalized sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT id,subscription_id,finalized_at FROM invoices WHERE id=?`, id).Scan(&detail.ID, &detail.SubscriptionID, &finalized); err != nil {
		return AdminInvoiceDetail{}, err
	}
	if finalized.Valid {
		at := time.Unix(0, finalized.Int64).UTC()
		detail.FinalizedAt = &at
	}
	detail.Balance, err = loadInvoiceBalance(ctx, tx, id)
	if err != nil {
		return AdminInvoiceDetail{}, err
	}
	var period BillingPeriod
	var start, end, due int64
	err = tx.QueryRowContext(ctx, `SELECT period_index,period_start,period_end,due_at,invoice_id FROM billing_periods WHERE invoice_id=?`, id).Scan(&period.Index, &start, &end, &due, &period.InvoiceID)
	if err == nil {
		period.Start = time.Unix(0, start).UTC()
		period.End = time.Unix(0, end).UTC()
		period.DueAt = time.Unix(0, due).UTC()
		detail.Period = &period
	} else if !errors.Is(err, sql.ErrNoRows) {
		return AdminInvoiceDetail{}, err
	}
	lines, err := tx.QueryContext(ctx, `SELECT price_version_id,component_code,amount_minor FROM invoice_lines WHERE invoice_id=? ORDER BY rowid`, id)
	if err != nil {
		return AdminInvoiceDetail{}, err
	}
	for lines.Next() {
		var line InvoiceLine
		if err := lines.Scan(&line.PriceVersionID, &line.ComponentCode, &line.AmountMinor); err != nil {
			lines.Close()
			return AdminInvoiceDetail{}, err
		}
		detail.Lines = append(detail.Lines, line)
	}
	if err := lines.Err(); err != nil {
		lines.Close()
		return AdminInvoiceDetail{}, err
	}
	lines.Close()
	payments, err := tx.QueryContext(ctx, `SELECT id,amount_minor,currency,status FROM payment_operations WHERE invoice_id=? ORDER BY id LIMIT 101`, id)
	if err != nil {
		return AdminInvoiceDetail{}, err
	}
	for payments.Next() {
		var payment AdminInvoicePayment
		if err := payments.Scan(&payment.ID, &payment.AmountMinor, &payment.Currency, &payment.Status); err != nil {
			payments.Close()
			return AdminInvoiceDetail{}, err
		}
		detail.Payments = append(detail.Payments, payment)
	}
	if err := payments.Err(); err != nil {
		payments.Close()
		return AdminInvoiceDetail{}, err
	}
	payments.Close()
	if len(detail.Payments) > 100 {
		detail.Payments = detail.Payments[:100]
		detail.PaymentsTruncated = true
	}
	if err := loadAdminInvoiceProvenance(ctx, tx, id, &detail); err != nil {
		return AdminInvoiceDetail{}, err
	}
	if err := tx.Commit(); err != nil {
		return AdminInvoiceDetail{}, err
	}
	return detail, nil
}
