package lab

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const renewalGrace = 7 * 24 * time.Hour

// RefreshEntitlements projects service access from immutable billing periods,
// allocations and the current clock. A scheduler can rerun it at the grace
// deadline even when no webhook arrives.
func (l *Lab) RefreshEntitlements(ctx context.Context) error {
	rows, err := l.db.QueryContext(ctx, `SELECT id FROM subscriptions WHERE status='active' ORDER BY id`)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := l.refreshOneEntitlement(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

func (l *Lab) refreshOneEntitlement(ctx context.Context, subID string) error {
	now := l.now().UTC()
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := l.refreshOneEntitlementTx(ctx, tx, now, subID); err != nil {
		return err
	}
	return tx.Commit()
}

func (l *Lab) refreshOneEntitlementTx(ctx context.Context, tx *sql.Tx, now time.Time, subID string) error {
	var invoiceID string
	var dueAt int64
	err := tx.QueryRowContext(ctx, `SELECT p.invoice_id,p.due_at
FROM billing_periods p
WHERE p.subscription_id=? AND p.period_start<=? AND p.period_end>?
ORDER BY p.period_index DESC LIMIT 1`, subID, now.UnixNano(), now.UnixNano()).
		Scan(&invoiceID, &dueAt)
	noCurrentPeriod := errors.Is(err, sql.ErrNoRows)
	if err != nil && !noCurrentPeriod {
		return err
	}
	if noCurrentPeriod {
		err = tx.QueryRowContext(ctx, `SELECT invoice_id,due_at FROM billing_periods
WHERE subscription_id=? ORDER BY period_index DESC LIMIT 1`, subID).Scan(&invoiceID, &dueAt)
		if err != nil {
			return err
		}
	}
	contractState, contractReason, err := contractEntitlementDecision(ctx, tx, subID, now)
	if err != nil {
		return err
	}
	if contractState != "" {
		if err := upsertContractEntitlement(ctx, tx, subID, invoiceID, contractState, contractReason, now); err != nil {
			return err
		}
		return nil
	}
	var sourceOp, state, reason string
	var graceDeadline sql.NullInt64
	var currentPaid bool
	if !noCurrentPeriod {
		balance, err := loadInvoiceBalance(ctx, tx, invoiceID)
		if err != nil {
			return err
		}
		currentPaid = balance.OutstandingMinor == 0
	}
	if !noCurrentPeriod && currentPaid {
		state, reason = "active", "current_invoice_funded"
		err = tx.QueryRowContext(ctx, `SELECT a.operation_id FROM allocations a
JOIN invoices i ON i.id=a.invoice_id WHERE i.subscription_id=?
ORDER BY i.finalized_at DESC,a.rowid DESC LIMIT 1`, subID).Scan(&sourceOp)
		if err != nil {
			return err
		}
	} else {
		// Every active self-serve subscription has a previously confirmed
		// payment. Keep that source distinct from the current unpaid invoice.
		err = tx.QueryRowContext(ctx, `SELECT a.operation_id FROM allocations a
JOIN invoices i ON i.id=a.invoice_id WHERE i.subscription_id=?
ORDER BY i.finalized_at DESC,a.rowid DESC LIMIT 1`, subID).Scan(&sourceOp)
		if err != nil {
			return err
		}
		if noCurrentPeriod {
			state, reason = "suspended", "no_current_period"
		} else {
			deadline := time.Unix(0, dueAt).UTC().Add(renewalGrace)
			graceDeadline = sql.NullInt64{Int64: deadline.UnixNano(), Valid: true}
			if now.Before(deadline) {
				state, reason = "grace", "renewal_invoice_open"
			} else {
				var unresolved int
				err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM payment_operations
WHERE invoice_id=? AND status IN ('submitted','unknown')`, invoiceID).Scan(&unresolved)
				if err != nil {
					return err
				}
				if unresolved > 0 {
					state, reason = "grace", "payment_verification_pending"
				} else {
					state, reason = "suspended", "grace_expired"
				}
			}
		}
	}
	var endedAt int64
	err = tx.QueryRowContext(ctx, `SELECT ended_at FROM subscription_ends WHERE subscription_id=?`, subID).Scan(&endedAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil && now.UnixNano() >= endedAt {
		state, reason = "expired", "cancel_at"
		graceDeadline = sql.NullInt64{}
	}
	var sourceRevision int64
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM subscriptions WHERE id=?`, subID).Scan(&sourceRevision); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO entitlements
(subscription_id,status,source_operation_id,updated_at,reason,source_invoice_id,grace_deadline,source_revision)
VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(subscription_id) DO UPDATE SET
status=excluded.status,source_operation_id=excluded.source_operation_id,
updated_at=excluded.updated_at,reason=excluded.reason,
source_invoice_id=excluded.source_invoice_id,grace_deadline=excluded.grace_deadline,source_revision=excluded.source_revision`,
		subID, state, sourceOp, now.UnixNano(), reason, invoiceID, graceDeadline, sourceRevision)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE outbox SET status='done' WHERE kind='entitlement' AND object_id=? AND status='pending'`, subID)
	if err != nil {
		return err
	}
	return nil
}
