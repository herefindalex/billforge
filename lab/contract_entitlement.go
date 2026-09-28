package lab

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

func contractEntitlementDecision(ctx context.Context, tx *sql.Tx, subID string, now time.Time) (string, string, error) {
	var ends int64
	var post sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT c.effective_to,c.post_price_version_id FROM contract_subscriptions s JOIN contract_versions c ON c.id=s.contract_version_id WHERE s.subscription_id=?`, subID).Scan(&ends, &post)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", nil
	}
	if err != nil {
		return "", "", err
	}
	if now.UnixNano() < ends {
		return "active", "enterprise_contract_net30", nil
	}
	if !post.Valid || post.String == "" {
		return "suspended", "contract_expired_next_price_missing", nil
	}
	return "", "", nil
}

func upsertContractEntitlement(ctx context.Context, tx *sql.Tx, subID, invoiceID, state, reason string, now time.Time) error {
	var sourceOp string
	if err := tx.QueryRowContext(ctx, `SELECT source_operation_id FROM entitlements WHERE subscription_id=?`, subID).Scan(&sourceOp); err != nil {
		return err
	}
	var revision int64
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM subscriptions WHERE id=?`, subID).Scan(&revision); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO entitlements(subscription_id,status,source_operation_id,updated_at,reason,source_invoice_id,grace_deadline,source_revision) VALUES(?,?,?,?,?,?,NULL,?) ON CONFLICT(subscription_id) DO UPDATE SET status=excluded.status,source_operation_id=excluded.source_operation_id,updated_at=excluded.updated_at,reason=excluded.reason,source_invoice_id=excluded.source_invoice_id,grace_deadline=NULL,source_revision=excluded.source_revision`, subID, state, sourceOp, now.UnixNano(), reason, invoiceID, revision)
	return err
}
