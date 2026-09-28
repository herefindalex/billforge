package lab

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type CorrectionResult struct {
	ID                   string
	InvoiceID            string
	ReductionMinor       int64
	PriorObligationMinor int64
	NewObligationMinor   int64
	GrantIDs             []string
}

// cancelUnsentOperations preserves old operation identities while ensuring a
// changed obligation cannot race a payment which has already left the DB.
func cancelUnsentOperations(ctx context.Context, tx *sql.Tx, invoiceID string) error {
	rows, err := tx.QueryContext(ctx, `SELECT id,status FROM payment_operations
WHERE invoice_id=? AND status IN ('created','submitted','unknown')`, invoiceID)
	if err != nil {
		return err
	}
	var created []string
	for rows.Next() {
		var id, status string
		if err = rows.Scan(&id, &status); err != nil {
			rows.Close()
			return err
		}
		if status != "created" {
			rows.Close()
			return ErrConflict
		}
		created = append(created, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range created {
		result, err := tx.ExecContext(ctx, `UPDATE payment_operations SET status='cancelled'
WHERE id=? AND status='created'`, id)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil || n != 1 {
			return ErrConflict
		}
		if _, err := tx.ExecContext(ctx, `UPDATE outbox SET status='done' WHERE id=?`, "capture:"+id); err != nil {
			return err
		}
	}
	return nil
}

func (l *Lab) CreatePayment(ctx context.Context, invoiceID string, amountMinor int64, requestKey string) (string, error) {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	opID, err := l.createPaymentTx(ctx, tx, l.now().UTC(), invoiceID, amountMinor, requestKey)
	if err != nil {
		return "", err
	}
	return opID, tx.Commit()
}

func (l *Lab) createPaymentTx(ctx context.Context, tx *sql.Tx, now time.Time, invoiceID string, amountMinor int64, requestKey string) (string, error) {
	if invoiceID == "" || requestKey == "" || amountMinor <= 0 {
		return "", errors.New("invoice, positive amount and request key are required")
	}
	var savedInvoice, savedOp string
	var savedAmount int64
	err := tx.QueryRowContext(ctx, `SELECT invoice_id,amount_minor,operation_id FROM payment_requests WHERE key=?`, requestKey).
		Scan(&savedInvoice, &savedAmount, &savedOp)
	if err == nil {
		if savedInvoice != invoiceID || savedAmount != amountMinor {
			return "", ErrConflict
		}
		return savedOp, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	periodIndex, end, dueAt, err := loadPayablePeriod(ctx, tx, invoiceID)
	if err != nil {
		return "", err
	}
	if err := ensureImmediateChangePayable(ctx, tx, invoiceID); err != nil {
		return "", err
	}
	contractInvoice, err := isContractInvoice(ctx, tx, invoiceID)
	if err != nil {
		return "", err
	}
	if contractInvoice && now.UnixNano() < dueAt {
		return "", ErrConflict
	}
	if !contractInvoice && now.UnixNano() >= end {
		return "", ErrPeriodEnded
	}
	if !contractInvoice && periodIndex > 0 && now.UnixNano() > time.Unix(0, dueAt).Add(renewalGrace).UnixNano() {
		return "", ErrLateNeedsReview
	}
	// The checkout's initial operation can be replaced by a partial payment.
	// A caller-created operation has its own request identity and must be
	// dispatched or explicitly corrected before another amount is admitted.
	var callerCreated int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM payment_operations o JOIN payment_requests p ON p.operation_id=o.id WHERE o.invoice_id=? AND o.status='created'`, invoiceID).Scan(&callerCreated); err != nil {
		return "", err
	}
	if callerCreated != 0 {
		return "", ErrConflict
	}
	if err := cancelUnsentOperations(ctx, tx, invoiceID); err != nil {
		return "", err
	}
	b, err := loadInvoiceBalance(ctx, tx, invoiceID)
	if err != nil {
		return "", err
	}
	if amountMinor > b.OutstandingMinor {
		return "", ErrConflict
	}
	opID, err := newID("op_")
	if err != nil {
		return "", err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO payment_operations(id,invoice_id,provider_key,amount_minor,currency,status)
VALUES(?,?,?,?,?,'created')`, opID, invoiceID, "capture:"+invoiceID+":"+opID, amountMinor, b.Currency)
	if err != nil {
		return "", err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO outbox(id,kind,object_id,status) VALUES(?,'capture',?,'pending')`, "capture:"+opID, opID)
	if err != nil {
		return "", err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO payment_requests(key,invoice_id,amount_minor,operation_id) VALUES(?,?,?,?)`, requestKey, invoiceID, amountMinor, opID)
	if err != nil {
		return "", err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO audit_events(kind,object_id,at) VALUES('payment_created',?,?)`, opID, now.UnixNano())
	if err != nil {
		return "", err
	}
	return opID, nil
}

func (l *Lab) PostReduction(ctx context.Context, invoiceID string, reductionMinor int64, reason, requestKey string) (CorrectionResult, error) {
	return l.postReduction(ctx, invoiceID, reductionMinor, reason, requestKey, false)
}

func (l *Lab) postReduction(ctx context.Context, invoiceID string, reductionMinor int64, reason, requestKey string, allowZero bool) (CorrectionResult, error) {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return CorrectionResult{}, err
	}
	defer tx.Rollback()
	result, err := l.postReductionTx(ctx, tx, l.now().UTC(), invoiceID, reductionMinor, reason, requestKey, allowZero)
	if err != nil {
		return CorrectionResult{}, err
	}
	return result, tx.Commit()
}

func (l *Lab) postReductionTx(ctx context.Context, tx *sql.Tx, at time.Time, invoiceID string, reductionMinor int64, reason, requestKey string, allowZero bool) (CorrectionResult, error) {
	if invoiceID == "" || reason == "" || requestKey == "" || reductionMinor <= 0 {
		return CorrectionResult{}, errors.New("invoice, positive reduction, reason and request key are required")
	}
	var saved CorrectionResult
	var savedReason string
	err := tx.QueryRowContext(ctx, `SELECT id,invoice_id,reduction_minor,prior_obligation_minor,new_obligation_minor,reason
FROM corrections WHERE request_key=?`, requestKey).
		Scan(&saved.ID, &saved.InvoiceID, &saved.ReductionMinor, &saved.PriorObligationMinor, &saved.NewObligationMinor, &savedReason)
	if err == nil {
		if saved.InvoiceID != invoiceID || saved.ReductionMinor != reductionMinor || savedReason != reason {
			return CorrectionResult{}, ErrConflict
		}
		ids, err := grantIDsForCorrection(ctx, tx, saved.ID)
		saved.GrantIDs = ids
		return saved, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return CorrectionResult{}, err
	}
	if err := cancelUnsentOperations(ctx, tx, invoiceID); err != nil {
		return CorrectionResult{}, err
	}
	b, err := loadInvoiceBalance(ctx, tx, invoiceID)
	if err != nil {
		return CorrectionResult{}, err
	}
	if b.CreditAppliedMinor != 0 || reductionMinor > b.ObligationMinor || (reductionMinor == b.ObligationMinor && !allowZero) {
		return CorrectionResult{}, ErrConflict // full waiver/reversal needs its own policy
	}
	result := CorrectionResult{InvoiceID: invoiceID, ReductionMinor: reductionMinor,
		PriorObligationMinor: b.ObligationMinor, NewObligationMinor: b.ObligationMinor - reductionMinor}
	result.ID, err = newID("corr_")
	if err != nil {
		return CorrectionResult{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO corrections
(id,invoice_id,reduction_minor,prior_obligation_minor,new_obligation_minor,reason,request_key,created_at)
VALUES(?,?,?,?,?,?,?,?)`, result.ID, invoiceID, reductionMinor, result.PriorObligationMinor,
		result.NewObligationMinor, reason, requestKey, at.UnixNano())
	if err != nil {
		return CorrectionResult{}, err
	}
	excess := b.NetAppliedMinor - result.NewObligationMinor
	if excess > 0 {
		rows, err := tx.QueryContext(ctx, `SELECT a.operation_id,a.amount_minor,
COALESCE((SELECT SUM(r.amount_minor) FROM allocation_releases r WHERE r.operation_id=a.operation_id),0)
FROM allocations a WHERE a.invoice_id=? ORDER BY a.rowid DESC`, invoiceID)
		if err != nil {
			return CorrectionResult{}, err
		}
		type source struct {
			op               string
			amount, released int64
		}
		var sources []source
		for rows.Next() {
			var s source
			if err = rows.Scan(&s.op, &s.amount, &s.released); err != nil {
				rows.Close()
				return CorrectionResult{}, err
			}
			sources = append(sources, s)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return CorrectionResult{}, err
		}
		for _, s := range sources {
			if excess == 0 {
				break
			}
			available := s.amount - s.released
			if available <= 0 {
				continue
			}
			amount := available
			if amount > excess {
				amount = excess
			}
			releaseID, err := newID("release_")
			if err != nil {
				return CorrectionResult{}, err
			}
			grantID, err := newID("grant_")
			if err != nil {
				return CorrectionResult{}, err
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO allocation_releases(id,correction_id,operation_id,amount_minor)
VALUES(?,?,?,?)`, releaseID, result.ID, s.op, amount); err != nil {
				return CorrectionResult{}, err
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO credit_grants
(id,release_id,source_operation_id,source_invoice_id,amount_minor,currency,created_at)
VALUES(?,?,?,?,?,?,?)`, grantID, releaseID, s.op, invoiceID, amount, b.Currency, at.UnixNano()); err != nil {
				return CorrectionResult{}, err
			}
			result.GrantIDs = append(result.GrantIDs, grantID)
			excess -= amount
		}
		if excess != 0 {
			return CorrectionResult{}, errors.New("insufficient captured source for correction")
		}
	}
	updated, err := loadInvoiceBalance(ctx, tx, invoiceID)
	if err != nil {
		return CorrectionResult{}, err
	}
	var subID, subStatus string
	if err := tx.QueryRowContext(ctx, `SELECT s.id,s.status FROM subscriptions s JOIN invoices i ON i.subscription_id=s.id WHERE i.id=?`, invoiceID).Scan(&subID, &subStatus); err != nil {
		return CorrectionResult{}, err
	}
	if subStatus == "pending" && updated.OutstandingMinor == 0 {
		activatedAt := at
		if _, err := tx.ExecContext(ctx, `UPDATE billing_periods SET period_start=?,period_end=? WHERE subscription_id=? AND period_index=0`, activatedAt.UnixNano(), cycleBoundary(activatedAt, 1).UnixNano(), subID); err != nil {
			return CorrectionResult{}, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE pricing_assignments SET effective_start=? WHERE subscription_id=? AND assignment_index=0 AND effective_end IS NULL`, activatedAt.UnixNano(), subID); err != nil {
			return CorrectionResult{}, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE subscriptions SET status='active' WHERE id=? AND status='pending'`, subID); err != nil {
			return CorrectionResult{}, err
		}
	}
	if subStatus == "active" || updated.OutstandingMinor == 0 {
		if _, err := tx.ExecContext(ctx, `INSERT INTO outbox(id,kind,object_id,status) VALUES(?,'entitlement',?,'pending')`, "entitlement:"+result.ID, subID); err != nil {
			return CorrectionResult{}, err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO audit_events(kind,object_id,at) VALUES('invoice_reduced',?,?)`, result.ID, at.UnixNano())
	if err != nil {
		return CorrectionResult{}, err
	}
	return result, nil
}

func grantIDsForCorrection(ctx context.Context, tx *sql.Tx, correctionID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT g.id FROM credit_grants g
JOIN allocation_releases r ON r.id=g.release_id WHERE r.correction_id=? ORDER BY r.rowid`, correctionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (l *Lab) ApplyCredit(ctx context.Context, grantID, invoiceID string, amountMinor int64, requestKey string) (string, error) {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	id, err := l.applyCreditTx(ctx, tx, l.now().UTC(), grantID, invoiceID, amountMinor, requestKey)
	if err != nil {
		return "", err
	}
	return id, tx.Commit()
}

func (l *Lab) applyCreditTx(ctx context.Context, tx *sql.Tx, at time.Time, grantID, invoiceID string, amountMinor int64, requestKey string) (string, error) {
	if grantID == "" || invoiceID == "" || requestKey == "" || amountMinor <= 0 {
		return "", errors.New("grant, invoice, positive amount and request key are required")
	}
	var savedID, savedGrant, savedInvoice string
	var savedAmount int64
	err := tx.QueryRowContext(ctx, `SELECT id,grant_id,invoice_id,amount_minor FROM credit_applications WHERE request_key=?`, requestKey).
		Scan(&savedID, &savedGrant, &savedInvoice, &savedAmount)
	if err == nil {
		if savedGrant != grantID || savedInvoice != invoiceID || savedAmount != amountMinor {
			return "", ErrConflict
		}
		return savedID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	var sourceInvoice string
	if err := tx.QueryRowContext(ctx, `SELECT source_invoice_id FROM credit_grants WHERE id=?`, grantID).Scan(&sourceInvoice); err != nil {
		return "", err
	}
	if sourceInvoice == invoiceID {
		return "", ErrConflict
	}
	var sourceCustomer, targetCustomer string
	err = tx.QueryRowContext(ctx, `SELECT s.customer_id FROM invoices i
JOIN subscriptions s ON s.id=i.subscription_id WHERE i.id=?`, sourceInvoice).Scan(&sourceCustomer)
	if err != nil {
		return "", err
	}
	err = tx.QueryRowContext(ctx, `SELECT s.customer_id FROM invoices i
JOIN subscriptions s ON s.id=i.subscription_id WHERE i.id=?`, invoiceID).Scan(&targetCustomer)
	if err != nil {
		return "", err
	}
	if sourceCustomer != targetCustomer {
		return "", ErrConflict
	}
	var periodIndex int
	var dueAt, end int64
	err = tx.QueryRowContext(ctx, `SELECT period_index,due_at,period_end FROM billing_periods WHERE invoice_id=?`, invoiceID).
		Scan(&periodIndex, &dueAt, &end)
	if err != nil {
		return "", err
	}
	if periodIndex == 0 || at.UnixNano() >= end {
		return "", ErrConflict
	}
	if at.UnixNano() > time.Unix(0, dueAt).Add(renewalGrace).UnixNano() {
		return "", ErrLateNeedsReview
	}
	if err := cancelUnsentOperations(ctx, tx, invoiceID); err != nil {
		return "", err
	}
	grant, err := loadCreditBalance(ctx, tx, grantID)
	if err != nil {
		return "", err
	}
	invoice, err := loadInvoiceBalance(ctx, tx, invoiceID)
	if err != nil {
		return "", err
	}
	if grant.Currency != invoice.Currency || amountMinor > grant.AvailableMinor || amountMinor > invoice.OutstandingMinor {
		return "", ErrConflict
	}
	id, err := newID("credit_app_")
	if err != nil {
		return "", err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO credit_applications(id,grant_id,invoice_id,amount_minor,request_key,created_at)
VALUES(?,?,?,?,?,?)`, id, grantID, invoiceID, amountMinor, requestKey, at.UnixNano())
	if err != nil {
		return "", err
	}
	var subID string
	if err := tx.QueryRowContext(ctx, `SELECT subscription_id FROM invoices WHERE id=?`, invoiceID).Scan(&subID); err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO outbox(id,kind,object_id,status)
VALUES(?,'entitlement',?,'pending')`, "entitlement:"+id, subID); err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_events(kind,object_id,at) VALUES('credit_applied',?,?)`, id, at.UnixNano()); err != nil {
		return "", err
	}
	return id, nil
}
