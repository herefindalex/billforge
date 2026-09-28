package lab

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type RefundInfo struct {
	ID          string
	GrantID     string
	AmountMinor int64
	Currency    string
	Status      string
}

func (l *Lab) ReserveRefund(ctx context.Context, grantID string, amountMinor int64, requestKey string) (string, error) {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	id, err := l.reserveRefundTx(ctx, tx, l.now().UTC(), grantID, amountMinor, requestKey)
	if err != nil {
		return "", err
	}
	return id, tx.Commit()
}

func (l *Lab) reserveRefundTx(ctx context.Context, tx *sql.Tx, at time.Time, grantID string, amountMinor int64, requestKey string) (string, error) {
	if grantID == "" || requestKey == "" || amountMinor <= 0 {
		return "", errors.New("grant, positive amount and request key are required")
	}
	var savedID, savedGrant string
	var savedAmount int64
	err := tx.QueryRowContext(ctx, `SELECT id,grant_id,amount_minor FROM refund_operations WHERE request_key=?`, requestKey).
		Scan(&savedID, &savedGrant, &savedAmount)
	if err == nil {
		if savedGrant != grantID || savedAmount != amountMinor {
			return "", ErrConflict
		}
		return savedID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	credit, err := loadCreditBalance(ctx, tx, grantID)
	if err != nil {
		return "", err
	}
	if amountMinor > credit.AvailableMinor {
		return "", ErrConflict
	}
	var sourceKey string
	err = tx.QueryRowContext(ctx, `SELECT o.provider_key FROM credit_grants g
JOIN payment_operations o ON o.id=g.source_operation_id WHERE g.id=? AND o.status='succeeded'`, grantID).
		Scan(&sourceKey)
	if err != nil {
		return "", err
	}
	id, err := newID("refund_")
	if err != nil {
		return "", err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO refund_operations
(id,grant_id,provider_key,source_provider_key,amount_minor,currency,status,request_key,created_at)
VALUES(?,?,?,?,?,?,'created',?,?)`, id, grantID, "refund:"+id, sourceKey,
		amountMinor, credit.Currency, requestKey, at.UnixNano())
	if err != nil {
		return "", err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO outbox(id,kind,object_id,status) VALUES(?,'refund',?,'pending')`, "refund:"+id, id)
	if err != nil {
		return "", err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO audit_events(kind,object_id,at) VALUES('refund_reserved',?,?)`, id, at.UnixNano())
	if err != nil {
		return "", err
	}
	return id, nil
}

func (l *Lab) Refund(ctx context.Context, id string) (RefundInfo, error) {
	var r RefundInfo
	err := l.db.QueryRowContext(ctx, `SELECT id,grant_id,amount_minor,currency,status FROM refund_operations WHERE id=?`, id).
		Scan(&r.ID, &r.GrantID, &r.AmountMinor, &r.Currency, &r.Status)
	return r, err
}

func (l *Lab) DispatchRefundNext(ctx context.Context, fault string) (string, error) {
	return l.dispatchRefund(ctx, "", fault)
}

func (l *Lab) DispatchRefund(ctx context.Context, refundID, fault string) (string, error) {
	if refundID == "" {
		return "", ErrConflict
	}
	return l.dispatchRefund(ctx, refundID, fault)
}

func (l *Lab) dispatchRefund(ctx context.Context, refundID, fault string) (string, error) {
	return l.dispatchRefundAt(ctx, refundID, fault, l.ClockTime())
}

func (l *Lab) dispatchRefundAt(ctx context.Context, refundID, fault string, at time.Time) (string, error) {
	if fault != "" && fault != "crash_after_provider" && fault != "lost_response" {
		return "", errors.New("unsupported fault")
	}
	var id, key, source, currency, status string
	var amount int64
	query := `SELECT r.id,r.provider_key,r.source_provider_key,r.amount_minor,r.currency,r.status
FROM outbox b JOIN refund_operations r ON r.id=b.object_id
WHERE b.kind='refund' AND b.status='pending'`
	var args []any
	if refundID != "" {
		query += ` AND r.id=?`
		args = append(args, refundID)
	}
	query += ` ORDER BY b.rowid LIMIT 1`
	err := l.db.QueryRowContext(ctx, query, args...).
		Scan(&id, &key, &source, &amount, &currency, &status)
	if err != nil {
		return "", err
	}
	if status == "submitted" || status == "unknown" {
		e, found, err := l.provider.LookupRefund(ctx, key)
		if err != nil {
			return id, err
		}
		if !found {
			return id, ErrPaymentUnknown
		}
		e.ID = "lookup-refund:" + key
		return id, l.applyRefundObservationAt(ctx, e, at)
	}
	if status != "created" {
		return id, ErrConflict
	}
	if err := l.adminMarkSubmitted(ctx, "refund_operations", id); err != nil {
		return id, err
	}
	e, err := l.provider.Refund(ctx, key, source, amount, currency)
	if err != nil {
		return id, err
	}
	switch fault {
	case "crash_after_provider":
		return id, ErrInjectedCrash
	case "lost_response":
		_, err = l.db.ExecContext(ctx, `UPDATE refund_operations SET status='unknown' WHERE id=? AND status='submitted'`, id)
		if err != nil {
			return id, err
		}
		return id, ErrPaymentUnknown
	default:
		return id, l.applyRefundObservationAt(ctx, e, at)
	}
}

func (l *Lab) ReconcileRefund(ctx context.Context, id string) (bool, error) {
	var key string
	if err := l.db.QueryRowContext(ctx, `SELECT provider_key FROM refund_operations WHERE id=?`, id).Scan(&key); err != nil {
		return false, err
	}
	e, found, err := l.provider.LookupRefund(ctx, key)
	if err != nil || !found {
		return found, err
	}
	e.ID = "lookup-refund:" + key
	return true, l.applyRefundObservation(ctx, e)
}

func (l *Lab) HandleRefundWebhook(ctx context.Context, e RefundEvent) error {
	actual, found, err := l.provider.LookupRefund(ctx, e.Key)
	if err != nil {
		return err
	}
	if !found || actual.SourceKey != e.SourceKey || actual.Status != e.Status ||
		actual.Amount != e.Amount || actual.Currency != e.Currency {
		return ErrConflict
	}
	return l.applyRefundObservation(ctx, e)
}

func (l *Lab) applyRefundObservation(ctx context.Context, e RefundEvent) error {
	return l.applyRefundObservationAt(ctx, e, l.ClockTime())
}

func (l *Lab) applyRefundObservationAt(ctx context.Context, e RefundEvent, at time.Time) error {
	if e.ID == "" || e.Key == "" ||
		(e.Status != "succeeded" && e.Status != "definitively_failed") {
		return errors.New("invalid refund observation")
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Reserve SQLite's writer slot before reading the refund and inbox state.
	// A concurrent reservation may otherwise commit between the read and the
	// later observation write, leaving this transaction unable to upgrade.
	if _, err := tx.ExecContext(ctx, `UPDATE refund_operations SET status=status WHERE provider_key=?`, e.Key); err != nil {
		return err
	}
	var id, source, currency, status string
	var amount int64
	err = tx.QueryRowContext(ctx, `SELECT id,source_provider_key,amount_minor,currency,status
FROM refund_operations WHERE provider_key=?`, e.Key).Scan(&id, &source, &amount, &currency, &status)
	if err != nil {
		return err
	}
	if e.SourceKey != source || e.Amount != amount || e.Currency != currency ||
		(status == "succeeded" && e.Status != "succeeded") ||
		(status == "definitively_failed" && e.Status != "definitively_failed") {
		return ErrConflict
	}
	fingerprint := hash(e.Key, e.SourceKey, e.Status, e.Amount, e.Currency)
	var existing string
	err = tx.QueryRowContext(ctx, `SELECT payload_hash FROM inbox WHERE event_id=?`, e.ID).Scan(&existing)
	if err == nil {
		if existing != fingerprint {
			return ErrConflict
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO inbox(event_id,provider_key,status,payload_hash,received_at)
VALUES(?,?,?,?,?)`, e.ID, e.Key, e.Status, fingerprint, at.UnixNano())
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE refund_operations SET status=? WHERE id=?`, e.Status, id)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE outbox SET status='done' WHERE id=?`, "refund:"+id)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO audit_events(kind,object_id,at) VALUES(?,?,?)`, "refund_"+e.Status, id, at.UnixNano())
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (l *Lab) SetFakeRefundDecision(ctx context.Context, refundID, status string) error {
	var key string
	if err := l.db.QueryRowContext(ctx, `SELECT provider_key FROM refund_operations WHERE id=?`, refundID).Scan(&key); err != nil {
		return err
	}
	return l.provider.SetRefundDecision(ctx, key, status)
}
