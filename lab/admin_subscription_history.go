package lab

import (
	"context"
	"database/sql"
	"strconv"
	"time"
)

type AdminPeriodPage struct {
	Items     []BillingPeriod
	NextIndex *int
}

type AdminSubscriptionEvent struct {
	At        time.Time
	Kind      string
	Reference string
	Status    string
}

type AdminTimelineCursor struct {
	AtNano    int64  `json:"at_nano"`
	Kind      string `json:"kind"`
	Reference string `json:"reference"`
}

type AdminTimelinePage struct {
	Items []AdminSubscriptionEvent
	Next  *AdminTimelineCursor
}

type AdminEntitlementDetail struct {
	SubscriptionID    string
	Status            string
	Reason            string
	SourceOperationID string
	SourceInvoiceID   string
	SourceRevision    int64
	UpdatedAt         time.Time
	GraceDeadline     *time.Time
}

func subscriptionExists(ctx context.Context, tx *sql.Tx, id string) error {
	var found int
	return tx.QueryRowContext(ctx, `SELECT 1 FROM subscriptions WHERE id=?`, id).Scan(&found)
}

func (l *Lab) AdminSubscriptionPeriods(ctx context.Context, id string, before *int, limit int) (AdminPeriodPage, error) {
	if limit < 1 || limit > 100 || (before != nil && *before < 0) {
		return AdminPeriodPage{}, ErrAdminInvalidCommand
	}
	tx, err := l.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return AdminPeriodPage{}, err
	}
	defer tx.Rollback()
	if err := subscriptionExists(ctx, tx, id); err != nil {
		return AdminPeriodPage{}, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT period_index,period_start,period_end,due_at,invoice_id
		FROM billing_periods WHERE subscription_id=? AND (? IS NULL OR period_index<?)
		ORDER BY period_index DESC LIMIT ?`, id, before, before, limit+1)
	if err != nil {
		return AdminPeriodPage{}, err
	}
	page := AdminPeriodPage{Items: make([]BillingPeriod, 0, limit)}
	for rows.Next() {
		var item BillingPeriod
		var start, end, due int64
		if err := rows.Scan(&item.Index, &start, &end, &due, &item.InvoiceID); err != nil {
			rows.Close()
			return AdminPeriodPage{}, err
		}
		item.Start = time.Unix(0, start).UTC()
		item.End = time.Unix(0, end).UTC()
		item.DueAt = time.Unix(0, due).UTC()
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return AdminPeriodPage{}, err
	}
	rows.Close()
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		last := page.Items[len(page.Items)-1].Index
		page.NextIndex = &last
	}
	if err := tx.Commit(); err != nil {
		return AdminPeriodPage{}, err
	}
	return page, nil
}

func (l *Lab) AdminSubscriptionTimeline(ctx context.Context, id string, after *AdminTimelineCursor, limit int) (AdminTimelinePage, error) {
	if limit < 1 || limit > 100 || (after != nil && (after.AtNano == 0 || after.Kind == "")) {
		return AdminTimelinePage{}, ErrAdminInvalidCommand
	}
	tx, err := l.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return AdminTimelinePage{}, err
	}
	defer tx.Rollback()
	if err := subscriptionExists(ctx, tx, id); err != nil {
		return AdminTimelinePage{}, err
	}
	var cursorAt any
	var cursorKind, cursorRef string
	if after != nil {
		cursorAt, cursorKind, cursorRef = after.AtNano, after.Kind, after.Reference
	}
	rows, err := tx.QueryContext(ctx, `WITH events(at,kind,reference,status) AS (
		SELECT period_start,'period_started',invoice_id,'effective' FROM billing_periods WHERE subscription_id=?
		UNION ALL SELECT created_at,'schedule_created',id,status FROM subscription_schedules WHERE subscription_id=?
		UNION ALL SELECT applied_at,'schedule_applied',id,status FROM subscription_schedules WHERE subscription_id=? AND applied_at IS NOT NULL
		UNION ALL SELECT effective_at,'schedule_planned',id,status FROM subscription_schedules WHERE subscription_id=? AND status='scheduled'
		UNION ALL SELECT updated_at,'entitlement_updated',source_operation_id,status FROM entitlements WHERE subscription_id=?
		UNION ALL SELECT detected_at,'renewal_hold',CAST(period_index AS TEXT),reason FROM renewal_holds WHERE subscription_id=?
		UNION ALL SELECT ended_at,'subscription_ended',schedule_id,'ended' FROM subscription_ends WHERE subscription_id=?
		UNION ALL SELECT effective_at,'contract_transition',post_price_version_id,'effective' FROM contract_transitions WHERE subscription_id=?
		UNION ALL SELECT requested_at,'immediate_change_requested',id,status FROM immediate_changes WHERE subscription_id=?
		UNION ALL SELECT activated_at,'immediate_change_activated',id,status FROM immediate_changes WHERE subscription_id=? AND activated_at IS NOT NULL
		UNION ALL SELECT finalized_at,'invoice_finalized',id,'finalized' FROM invoices WHERE subscription_id=? AND finalized_at IS NOT NULL
		UNION ALL SELECT ae.at,ae.kind,ae.object_id,ae.kind FROM audit_events ae
			JOIN payment_operations po ON po.id=ae.object_id
			JOIN invoices i ON i.id=po.invoice_id
			WHERE i.subscription_id=? AND (ae.kind IN ('payment_created','payment_retry_created') OR ae.kind LIKE 'provider_%')
		UNION ALL SELECT at,kind,object_id,'recorded' FROM audit_events WHERE object_id=? AND kind IN ('quote_accepted','contract_quote_accepted')
	)
	SELECT at,kind,reference,status FROM events
	WHERE (? IS NULL OR at<? OR (at=? AND kind<?) OR (at=? AND kind=? AND reference<?))
	ORDER BY at DESC,kind DESC,reference DESC LIMIT ?`,
		id, id, id, id, id, id, id, id, id, id, id, id, id,
		cursorAt, cursorAt, cursorAt, cursorKind, cursorAt, cursorKind, cursorRef, limit+1)
	if err != nil {
		return AdminTimelinePage{}, err
	}
	page := AdminTimelinePage{Items: make([]AdminSubscriptionEvent, 0, limit)}
	for rows.Next() {
		var item AdminSubscriptionEvent
		var at int64
		if err := rows.Scan(&at, &item.Kind, &item.Reference, &item.Status); err != nil {
			rows.Close()
			return AdminTimelinePage{}, err
		}
		item.At = time.Unix(0, at).UTC()
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return AdminTimelinePage{}, err
	}
	rows.Close()
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		last := page.Items[len(page.Items)-1]
		page.Next = &AdminTimelineCursor{AtNano: last.At.UnixNano(), Kind: last.Kind, Reference: last.Reference}
	}
	if err := tx.Commit(); err != nil {
		return AdminTimelinePage{}, err
	}
	return page, nil
}

func (l *Lab) AdminSubscriptionEntitlement(ctx context.Context, id string) (*AdminEntitlementDetail, error) {
	tx, err := l.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := subscriptionExists(ctx, tx, id); err != nil {
		return nil, err
	}
	var detail AdminEntitlementDetail
	var updated int64
	var sourceInvoice sql.NullString
	var grace sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT subscription_id,status,reason,source_operation_id,source_invoice_id,
		COALESCE(source_revision,0),updated_at,grace_deadline FROM entitlements WHERE subscription_id=?`, id).
		Scan(&detail.SubscriptionID, &detail.Status, &detail.Reason, &detail.SourceOperationID,
			&sourceInvoice, &detail.SourceRevision, &updated, &grace)
	if err == sql.ErrNoRows {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	detail.SourceInvoiceID = sourceInvoice.String
	detail.UpdatedAt = time.Unix(0, updated).UTC()
	if grace.Valid {
		deadline := time.Unix(0, grace.Int64).UTC()
		detail.GraceDeadline = &deadline
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &detail, nil
}

func AdminPeriodCursor(index int) string {
	return strconv.Itoa(index)
}
