package lab

import (
	"context"
	"database/sql"
	"time"
)

type AdminInvoiceHistoryCursor struct {
	InvoiceID string `json:"invoice_id"`
	Kind      string `json:"kind"`
	AtNano    int64  `json:"at_nano"`
	ID        string `json:"id"`
}

type AdminInvoiceHistoryPage struct {
	Items    any
	Next     *AdminInvoiceHistoryCursor
	Currency string
}

func invoiceHistoryRows[T any](ctx context.Context, tx *sql.Tx, base, alias, invoiceID, kind string, after *AdminInvoiceHistoryCursor, limit int, scan func(*sql.Rows) (T, error), identity func(T) (int64, string)) (AdminInvoiceHistoryPage, error) {
	args := []any{invoiceID}
	query := base
	if after != nil {
		query += " AND (" + alias + ".created_at<? OR (" + alias + ".created_at=? AND " + alias + ".id<?))"
		args = append(args, after.AtNano, after.AtNano, after.ID)
	}
	query += " ORDER BY " + alias + ".created_at DESC," + alias + ".id DESC LIMIT ?"
	args = append(args, limit+1)
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return AdminInvoiceHistoryPage{}, err
	}
	defer rows.Close()
	items := make([]T, 0)
	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			return AdminInvoiceHistoryPage{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return AdminInvoiceHistoryPage{}, err
	}
	page := AdminInvoiceHistoryPage{Items: items}
	if len(items) > limit {
		items = items[:limit]
		at, id := identity(items[len(items)-1])
		page.Items = items
		page.Next = &AdminInvoiceHistoryCursor{InvoiceID: invoiceID, Kind: kind, AtNano: at, ID: id}
	}
	return page, nil
}

func (l *Lab) AdminInvoiceHistory(ctx context.Context, invoiceID, kind string, after *AdminInvoiceHistoryCursor, limit int) (AdminInvoiceHistoryPage, error) {
	if limit < 1 || limit > 100 || invoiceID == "" {
		return AdminInvoiceHistoryPage{}, ErrConflict
	}
	if after != nil && (after.InvoiceID != invoiceID || after.Kind != kind || after.AtNano <= 0 || after.ID == "") {
		return AdminInvoiceHistoryPage{}, ErrConflict
	}
	tx, err := l.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return AdminInvoiceHistoryPage{}, err
	}
	defer tx.Rollback()
	var currency string
	if err := tx.QueryRowContext(ctx, `SELECT currency FROM invoices WHERE id=?`, invoiceID).Scan(&currency); err != nil {
		return AdminInvoiceHistoryPage{}, err
	}
	var page AdminInvoiceHistoryPage
	switch kind {
	case "corrections":
		page, err = invoiceHistoryRows(ctx, tx,
			`SELECT c.id,c.reduction_minor,c.prior_obligation_minor,c.new_obligation_minor,c.reason,c.created_at,
			 CASE WHEN ac.id IS NOT NULL THEN 'admin_command' WHEN delayed.id IS NOT NULL THEN 'delayed_change' WHEN unfulfilled.id IS NOT NULL THEN 'unfulfilled_change' ELSE 'domain_request' END,
			 COALESCE(ac.id,delayed.id,unfulfilled.id,c.request_key)
			 FROM corrections c
			 LEFT JOIN admin_commands ac ON ac.id=substr(c.request_key,7) AND c.request_key='admin:'||ac.id AND ac.action_id='C11' AND ac.target_id=c.invoice_id
			 LEFT JOIN immediate_changes delayed ON delayed.id=substr(c.request_key,19) AND c.request_key='change-correction:'||delayed.id AND delayed.invoice_id=c.invoice_id
			 LEFT JOIN immediate_changes unfulfilled ON unfulfilled.id=substr(c.request_key,20) AND c.request_key='unfulfilled-change:'||unfulfilled.id AND unfulfilled.invoice_id=c.invoice_id
			 WHERE c.invoice_id=?`, "c", invoiceID, kind, after, limit,
			func(rows *sql.Rows) (AdminInvoiceCorrection, error) {
				var item AdminInvoiceCorrection
				var at int64
				err := rows.Scan(&item.ID, &item.ReductionMinor, &item.PriorObligationMinor, &item.NewObligationMinor, &item.Reason, &at, &item.OriginKind, &item.OriginID)
				item.CreatedAt = time.Unix(0, at).UTC()
				return item, err
			}, func(item AdminInvoiceCorrection) (int64, string) { return item.CreatedAt.UnixNano(), item.ID })
	case "applications":
		page, err = invoiceHistoryRows(ctx, tx,
			`SELECT a.id,a.grant_id,g.source_invoice_id,a.amount_minor,a.created_at
			 FROM credit_applications a JOIN credit_grants g ON g.id=a.grant_id WHERE a.invoice_id=?`,
			"a", invoiceID, kind, after, limit,
			func(rows *sql.Rows) (AdminInvoiceCreditApplication, error) {
				var item AdminInvoiceCreditApplication
				var at int64
				err := rows.Scan(&item.ID, &item.GrantID, &item.SourceInvoiceID, &item.AmountMinor, &at)
				item.CreatedAt = time.Unix(0, at).UTC()
				return item, err
			}, func(item AdminInvoiceCreditApplication) (int64, string) { return item.CreatedAt.UnixNano(), item.ID })
	case "grants":
		page, err = invoiceHistoryRows(ctx, tx,
			`SELECT g.id,r.correction_id,g.release_id,g.source_operation_id,g.amount_minor,g.created_at
			 FROM credit_grants g JOIN allocation_releases r ON r.id=g.release_id WHERE g.source_invoice_id=?`,
			"g", invoiceID, kind, after, limit,
			func(rows *sql.Rows) (AdminInvoiceCreditGrant, error) {
				var item AdminInvoiceCreditGrant
				var at int64
				err := rows.Scan(&item.ID, &item.CorrectionID, &item.ReleaseID, &item.SourceOperationID, &item.AmountMinor, &at)
				item.CreatedAt = time.Unix(0, at).UTC()
				return item, err
			}, func(item AdminInvoiceCreditGrant) (int64, string) { return item.CreatedAt.UnixNano(), item.ID })
	case "refunds":
		page, err = invoiceHistoryRows(ctx, tx,
			`SELECT r.id,r.grant_id,r.amount_minor,r.currency,r.status,r.created_at
			 FROM refund_operations r JOIN credit_grants g ON g.id=r.grant_id WHERE g.source_invoice_id=?`,
			"r", invoiceID, kind, after, limit,
			func(rows *sql.Rows) (AdminInvoiceRefund, error) {
				var item AdminInvoiceRefund
				var at int64
				err := rows.Scan(&item.ID, &item.GrantID, &item.AmountMinor, &item.Currency, &item.Status, &at)
				item.CreatedAt = time.Unix(0, at).UTC()
				return item, err
			}, func(item AdminInvoiceRefund) (int64, string) { return item.CreatedAt.UnixNano(), item.ID })
	default:
		return AdminInvoiceHistoryPage{}, ErrAdminUnsupportedAction
	}
	if err != nil {
		return AdminInvoiceHistoryPage{}, err
	}
	page.Currency = currency
	if err := tx.Commit(); err != nil {
		return AdminInvoiceHistoryPage{}, err
	}
	return page, nil
}
