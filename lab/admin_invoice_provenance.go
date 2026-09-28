package lab

import (
	"context"
	"database/sql"
	"time"
)

type AdminInvoiceCorrection struct {
	ID                   string
	ReductionMinor       int64
	PriorObligationMinor int64
	NewObligationMinor   int64
	Reason               string
	OriginKind           string
	OriginID             string
	CreatedAt            time.Time
}

type AdminInvoiceCreditApplication struct {
	ID              string
	GrantID         string
	SourceInvoiceID string
	AmountMinor     int64
	CreatedAt       time.Time
}

type AdminInvoiceCreditGrant struct {
	ID                string
	CorrectionID      string
	ReleaseID         string
	SourceOperationID string
	AmountMinor       int64
	CreatedAt         time.Time
}

type AdminInvoiceRefund struct {
	ID          string
	GrantID     string
	AmountMinor int64
	Currency    string
	Status      string
	CreatedAt   time.Time
}

func boundedInvoiceRows[T any](ctx context.Context, tx *sql.Tx, query, invoiceID string, scan func(*sql.Rows) (T, error)) ([]T, bool, error) {
	rows, err := tx.QueryContext(ctx, query, invoiceID)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	items := make([]T, 0)
	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			return nil, false, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	if len(items) > 100 {
		return items[:100], true, nil
	}
	return items, false, nil
}

func loadAdminInvoiceProvenance(ctx context.Context, tx *sql.Tx, invoiceID string, detail *AdminInvoiceDetail) error {
	var err error
	detail.Corrections, detail.CorrectionsTruncated, err = boundedInvoiceRows(ctx, tx,
		`SELECT c.id,c.reduction_minor,c.prior_obligation_minor,c.new_obligation_minor,c.reason,c.created_at,
		 CASE WHEN ac.id IS NOT NULL THEN 'admin_command'
		      WHEN delayed.id IS NOT NULL THEN 'delayed_change'
		      WHEN unfulfilled.id IS NOT NULL THEN 'unfulfilled_change'
		      ELSE 'domain_request' END,
		 COALESCE(ac.id,delayed.id,unfulfilled.id,c.request_key)
		 FROM corrections c
		 LEFT JOIN admin_commands ac ON ac.id=substr(c.request_key,7) AND c.request_key='admin:'||ac.id AND ac.action_id='C11' AND ac.target_id=c.invoice_id
		 LEFT JOIN immediate_changes delayed ON delayed.id=substr(c.request_key,19) AND c.request_key='change-correction:'||delayed.id AND delayed.invoice_id=c.invoice_id
		 LEFT JOIN immediate_changes unfulfilled ON unfulfilled.id=substr(c.request_key,20) AND c.request_key='unfulfilled-change:'||unfulfilled.id AND unfulfilled.invoice_id=c.invoice_id
		 WHERE c.invoice_id=? ORDER BY c.created_at DESC,c.id DESC LIMIT 101`, invoiceID,
		func(rows *sql.Rows) (AdminInvoiceCorrection, error) {
			var item AdminInvoiceCorrection
			var createdAt int64
			err := rows.Scan(&item.ID, &item.ReductionMinor, &item.PriorObligationMinor, &item.NewObligationMinor, &item.Reason, &createdAt, &item.OriginKind, &item.OriginID)
			item.CreatedAt = time.Unix(0, createdAt).UTC()
			return item, err
		})
	if err != nil {
		return err
	}
	detail.CreditApplications, detail.CreditApplicationsTruncated, err = boundedInvoiceRows(ctx, tx,
		`SELECT a.id,a.grant_id,g.source_invoice_id,a.amount_minor,a.created_at
		 FROM credit_applications a JOIN credit_grants g ON g.id=a.grant_id
		 WHERE a.invoice_id=? ORDER BY a.created_at DESC,a.id DESC LIMIT 101`, invoiceID,
		func(rows *sql.Rows) (AdminInvoiceCreditApplication, error) {
			var item AdminInvoiceCreditApplication
			var createdAt int64
			err := rows.Scan(&item.ID, &item.GrantID, &item.SourceInvoiceID, &item.AmountMinor, &createdAt)
			item.CreatedAt = time.Unix(0, createdAt).UTC()
			return item, err
		})
	if err != nil {
		return err
	}
	detail.CreditGrants, detail.CreditGrantsTruncated, err = boundedInvoiceRows(ctx, tx,
		`SELECT g.id,r.correction_id,g.release_id,g.source_operation_id,g.amount_minor,g.created_at
		 FROM credit_grants g JOIN allocation_releases r ON r.id=g.release_id
		 WHERE g.source_invoice_id=? ORDER BY g.created_at DESC,g.id DESC LIMIT 101`, invoiceID,
		func(rows *sql.Rows) (AdminInvoiceCreditGrant, error) {
			var item AdminInvoiceCreditGrant
			var createdAt int64
			err := rows.Scan(&item.ID, &item.CorrectionID, &item.ReleaseID, &item.SourceOperationID, &item.AmountMinor, &createdAt)
			item.CreatedAt = time.Unix(0, createdAt).UTC()
			return item, err
		})
	if err != nil {
		return err
	}
	detail.Refunds, detail.RefundsTruncated, err = boundedInvoiceRows(ctx, tx,
		`SELECT r.id,r.grant_id,r.amount_minor,r.currency,r.status,r.created_at
		 FROM refund_operations r JOIN credit_grants g ON g.id=r.grant_id
		 WHERE g.source_invoice_id=? ORDER BY r.created_at DESC,r.id DESC LIMIT 101`, invoiceID,
		func(rows *sql.Rows) (AdminInvoiceRefund, error) {
			var item AdminInvoiceRefund
			var createdAt int64
			err := rows.Scan(&item.ID, &item.GrantID, &item.AmountMinor, &item.Currency, &item.Status, &createdAt)
			item.CreatedAt = time.Unix(0, createdAt).UTC()
			return item, err
		})
	return err
}
