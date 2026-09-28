package lab

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"time"
)

type BillingPeriod struct {
	Index     int
	Start     time.Time
	End       time.Time
	DueAt     time.Time
	InvoiceID string
}

// cycleBoundary keeps the original UTC day and time. For a short month it
// clamps to that month's last day, then returns to the original day later.
func cycleBoundary(anchor time.Time, months int) time.Time {
	anchor = anchor.UTC()
	first := time.Date(anchor.Year(), anchor.Month()+time.Month(months), 1,
		anchor.Hour(), anchor.Minute(), anchor.Second(), anchor.Nanosecond(), time.UTC)
	lastDay := first.AddDate(0, 1, 0).Add(-24 * time.Hour).Day()
	day := anchor.Day()
	if day > lastDay {
		day = lastDay
	}
	return time.Date(first.Year(), first.Month(), day,
		anchor.Hour(), anchor.Minute(), anchor.Second(), anchor.Nanosecond(), time.UTC)
}

func (l *Lab) Periods(ctx context.Context, subscriptionID string) ([]BillingPeriod, error) {
	rows, err := l.db.QueryContext(ctx, `SELECT period_index,period_start,period_end,due_at,invoice_id
FROM billing_periods WHERE subscription_id=? ORDER BY period_index`, subscriptionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var periods []BillingPeriod
	for rows.Next() {
		var p BillingPeriod
		var start, end, due int64
		if err := rows.Scan(&p.Index, &start, &end, &due, &p.InvoiceID); err != nil {
			return nil, err
		}
		p.Start, p.End, p.DueAt = time.Unix(0, start).UTC(), time.Unix(0, end).UTC(), time.Unix(0, due).UTC()
		periods = append(periods, p)
	}
	return periods, rows.Err()
}

// RunRenewals creates at most one next-period invoice per active subscription.
// It never charges an expired period retroactively or bills forward while the
// previous invoice is unpaid. The caller schedules dispatch separately.
func (l *Lab) RunRenewals(ctx context.Context) ([]Receipt, error) {
	rows, err := l.db.QueryContext(ctx, `SELECT id FROM subscriptions WHERE status='active' ORDER BY id`)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	var created []Receipt
	for _, id := range ids {
		r, made, err := l.renewOne(ctx, id)
		if err != nil {
			return created, err
		}
		if made {
			created = append(created, r)
		}
	}
	return created, nil
}

func (l *Lab) renewOne(ctx context.Context, subID string) (Receipt, bool, error) {
	now := l.now().UTC()
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return Receipt{}, false, err
	}
	defer tx.Rollback()
	receipt, made, err := l.renewOneTx(ctx, tx, now, subID)
	if err != nil {
		return Receipt{}, false, err
	}
	return receipt, made, tx.Commit()
}

func (l *Lab) renewOneTx(ctx context.Context, tx *sql.Tx, now time.Time, subID string) (Receipt, bool, error) {
	var err error
	var index int
	var priorEnd, anchor, seats int64
	var priorInvoice, priceVersionID string
	err = tx.QueryRowContext(ctx, `SELECT p.period_index,p.period_end,p.invoice_id,p0.period_start,s.price_version_id,s.seat_quantity
FROM billing_periods p JOIN subscriptions s ON s.id=p.subscription_id
JOIN billing_periods p0 ON p0.subscription_id=s.id AND p0.period_index=0
		WHERE p.subscription_id=? AND s.status='active' AND NOT EXISTS(SELECT 1 FROM subscription_ends e WHERE e.subscription_id=s.id) ORDER BY p.period_index DESC LIMIT 1`, subID).
		Scan(&index, &priorEnd, &priorInvoice, &anchor, &priceVersionID, &seats)
	if errors.Is(err, sql.ErrNoRows) {
		return Receipt{}, false, nil
	}
	if err != nil {
		return Receipt{}, false, err
	}
	if now.UnixNano() < priorEnd {
		return Receipt{}, false, nil
	}
	contract, hasContract, err := contractForSubscription(ctx, tx, subID)
	if err != nil {
		return Receipt{}, false, err
	}
	transitioned, err := hasPostContractTransition(ctx, tx, subID)
	if err != nil {
		return Receipt{}, false, err
	}
	contractRenewal := hasContract && priorEnd < contract.EffectiveTo.UnixNano()
	postContract := hasContract && !transitioned && priorEnd >= contract.EffectiveTo.UnixNano() && contract.PostContractPriceVersionID != ""
	if hasContract && priorEnd >= contract.EffectiveTo.UnixNano() && contract.PostContractPriceVersionID == "" {
		if _, err := tx.ExecContext(ctx, `INSERT INTO renewal_holds(subscription_id,period_index,reason,detected_at) VALUES(?,?,'contract_next_price_missing',?) ON CONFLICT(subscription_id,period_index) DO NOTHING`, subID, index+1, now.UnixNano()); err != nil {
			return Receipt{}, false, err
		}
		return Receipt{}, false, nil
	}
	schedule, hasSchedule, err := scheduleForBoundary(ctx, tx, subID, priorEnd)
	if err != nil {
		return Receipt{}, false, err
	}
	if hasSchedule && schedule.Kind == "cancel" {
		if err := applyBoundaryCancel(ctx, tx, schedule, now); err != nil {
			return Receipt{}, false, err
		}
		return Receipt{}, false, nil
	}
	end := cycleBoundary(time.Unix(0, anchor), index+2)
	if contractRenewal && end.After(contract.EffectiveTo) {
		if _, err := tx.ExecContext(ctx, `INSERT INTO renewal_holds(subscription_id,period_index,reason,detected_at) VALUES(?,?,'contract_partial_period_requires_review',?) ON CONFLICT(subscription_id,period_index) DO NOTHING`, subID, index+1, now.UnixNano()); err != nil {
			return Receipt{}, false, err
		}
		return Receipt{}, false, nil
	}
	if !now.Before(end) || !now.Before(time.Unix(0, priorEnd).Add(renewalGrace)) {
		_, err = tx.ExecContext(ctx, `INSERT INTO renewal_holds(subscription_id,period_index,reason,detected_at)
VALUES(?,?,'worker_missed_grace',?) ON CONFLICT(subscription_id,period_index) DO NOTHING`,
			subID, index+1, now.UnixNano())
		if err != nil {
			return Receipt{}, false, err
		}
		return Receipt{}, false, nil
	}
	previous, err := loadInvoiceBalance(ctx, tx, priorInvoice)
	if err != nil {
		return Receipt{}, false, err
	}
	if previous.OutstandingMinor != 0 && !contractRenewal {
		return Receipt{}, false, nil
	}
	var unresolvedChange int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM immediate_changes c WHERE c.subscription_id=? AND c.period_end=?
AND (c.status='requested' OR (c.status='needs_review' AND NOT EXISTS
(SELECT 1 FROM immediate_change_resolutions r WHERE r.change_id=c.id)))`, subID, priorEnd).Scan(&unresolvedChange); err != nil {
		return Receipt{}, false, err
	}
	if unresolvedChange != 0 {
		if _, err := tx.ExecContext(ctx, `INSERT INTO renewal_holds(subscription_id,period_index,reason,detected_at) VALUES(?,?,'immediate_change_unresolved',?) ON CONFLICT(subscription_id,period_index) DO NOTHING`, subID, index+1, now.UnixNano()); err != nil {
			return Receipt{}, false, err
		}
		return Receipt{}, false, nil
	}
	migratedPrice, migrationHold, err := applyPriceMigrationAtBoundary(ctx, tx, subID, priorEnd, now, hasSchedule)
	if err != nil {
		return Receipt{}, false, err
	}
	if migrationHold {
		if _, err := tx.ExecContext(ctx, `INSERT INTO renewal_holds(subscription_id,period_index,reason,detected_at) VALUES(?,?,'price_migration_paused',?) ON CONFLICT(subscription_id,period_index) DO NOTHING`, subID, index+1, now.UnixNano()); err != nil {
			return Receipt{}, false, err
		}
		return Receipt{}, false, nil
	}
	if migratedPrice != "" {
		priceVersionID = migratedPrice
	}
	if hasSchedule && schedule.Kind == "change" {
		if err := applyBoundaryChange(ctx, tx, schedule, now); err != nil {
			return Receipt{}, false, err
		}
		priceVersionID, seats = schedule.TargetPriceVersionID, schedule.SeatQuantity
	}
	if postContract {
		if err := applyPostContractPrice(ctx, tx, subID, contract, priorEnd); err != nil {
			return Receipt{}, false, err
		}
		priceVersionID = contract.PostContractPriceVersionID
	}
	terms, err := loadPriceTerms(ctx, tx, priceVersionID)
	if err != nil {
		return Receipt{}, false, err
	}
	amount, err := terms.UpfrontMinor(seats)
	if err != nil {
		return Receipt{}, false, err
	}
	fixedMinor, seatMinor := terms.FixedMinor, terms.SeatMinor
	if contractRenewal {
		if priceVersionID != contract.BasePriceVersionID || seats > (math.MaxInt64-contract.FixedMinor)/contract.SeatMinor {
			return Receipt{}, false, ErrConflict
		}
		fixedMinor, seatMinor = contract.FixedMinor, contract.SeatMinor
		amount = fixedMinor + seatMinor*seats
	}
	usageLines, usageTotal, err := pendingUsageLines(ctx, tx, subID, priorEnd)
	if errors.Is(err, ErrUsageCreditNoteRequired) {
		if _, holdErr := tx.ExecContext(ctx, `INSERT INTO renewal_holds(subscription_id,period_index,reason,detected_at) VALUES(?,?,'usage_credit_note_required',?) ON CONFLICT(subscription_id,period_index) DO NOTHING`, subID, index+1, now.UnixNano()); holdErr != nil {
			return Receipt{}, false, holdErr
		}
		return Receipt{}, false, nil
	}
	if err != nil {
		return Receipt{}, false, err
	}
	if usageTotal > math.MaxInt64-amount {
		return Receipt{}, false, ErrConflict
	}
	amount += usageTotal
	currency := terms.Currency
	var r Receipt
	r.SubscriptionID, r.AmountMinor, r.Currency = subID, amount, currency
	r.InvoiceID, err = newID("inv_")
	if err != nil {
		return Receipt{}, false, err
	}
	r.OperationID, err = newID("op_")
	if err != nil {
		return Receipt{}, false, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO invoices(id,subscription_id,total_minor,currency,finalized_at)
VALUES(?,?,?,?,NULL)`, r.InvoiceID, subID, amount, currency)
	if err != nil {
		return Receipt{}, false, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO invoice_lines(invoice_id,price_version_id,component_code,amount_minor)
		VALUES(?,?,'fixed',?)`, r.InvoiceID, priceVersionID, fixedMinor)
	if err != nil {
		return Receipt{}, false, err
	}
	if seatMinor > 0 {
		if _, err = tx.ExecContext(ctx, `INSERT INTO invoice_lines(invoice_id,price_version_id,component_code,amount_minor)
			VALUES(?,?,'seats',?)`, r.InvoiceID, priceVersionID, seatMinor*seats); err != nil {
			return Receipt{}, false, err
		}
	}
	if err := applyUsageLines(ctx, tx, subID, r.InvoiceID, now, usageLines); err != nil {
		return Receipt{}, false, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE invoices SET finalized_at=? WHERE id=?`, now.UnixNano(), r.InvoiceID)
	if err != nil {
		return Receipt{}, false, err
	}
	if contractRenewal {
		if _, err := tx.ExecContext(ctx, `INSERT INTO invoice_contracts(invoice_id,contract_version_id) VALUES(?,?)`, r.InvoiceID, contract.ID); err != nil {
			return Receipt{}, false, err
		}
	}
	dueAt := priorEnd
	if contractRenewal {
		dueAt = time.Unix(0, priorEnd).Add(30 * 24 * time.Hour).UnixNano()
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO billing_periods(subscription_id,period_index,period_start,period_end,due_at,invoice_id)
VALUES(?,?,?,?,?,?)`, subID, index+1, priorEnd, end.UnixNano(), dueAt, r.InvoiceID)
	if err != nil {
		return Receipt{}, false, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO payment_operations(id,invoice_id,provider_key,amount_minor,currency,status)
VALUES(?,?,?,?,?,'created')`, r.OperationID, r.InvoiceID, "capture:"+r.InvoiceID, amount, currency)
	if err != nil {
		return Receipt{}, false, err
	}
	if !contractRenewal {
		_, err = tx.ExecContext(ctx, `INSERT INTO outbox(id,kind,object_id,status) VALUES(?,'capture',?,'pending')`, "capture:"+r.OperationID, r.OperationID)
		if err != nil {
			return Receipt{}, false, err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO outbox(id,kind,object_id,status) VALUES(?,'entitlement',?,'pending')`, "entitlement:period:"+subID+":"+r.InvoiceID, subID)
	if err != nil {
		return Receipt{}, false, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO audit_events(kind,object_id,at) VALUES('renewal_finalized',?,?)`, r.InvoiceID, now.UnixNano())
	if err != nil {
		return Receipt{}, false, err
	}
	return r, true, nil
}

// RetryFailedPayment is allowed only after a definitive failure and while no
// other operation for the invoice is unresolved or successful. requestKey
// makes replay of the retry command return the original new operation.
func (l *Lab) RetryFailedPayment(ctx context.Context, invoiceID, requestKey string) (string, error) {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	opID, err := l.retryFailedPaymentTx(ctx, tx, l.now().UTC(), invoiceID, requestKey)
	if err != nil {
		return "", err
	}
	return opID, tx.Commit()
}

func (l *Lab) retryFailedPaymentTx(ctx context.Context, tx *sql.Tx, now time.Time, invoiceID, requestKey string) (string, error) {
	if invoiceID == "" || requestKey == "" {
		return "", errors.New("invoice and request key are required")
	}
	var savedInvoice, savedOperation string
	err := tx.QueryRowContext(ctx, `SELECT invoice_id,operation_id FROM payment_retry_requests WHERE key=?`, requestKey).
		Scan(&savedInvoice, &savedOperation)
	if err == nil {
		if savedInvoice != invoiceID {
			return "", ErrConflict
		}
		return savedOperation, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	periodIndex, periodEnd, dueAt, err := loadPayablePeriod(ctx, tx, invoiceID)
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
	if !contractInvoice && now.UnixNano() >= periodEnd {
		return "", ErrPeriodEnded
	}
	if !contractInvoice && periodIndex > 0 && now.UnixNano() > time.Unix(0, dueAt).Add(renewalGrace).UnixNano() {
		return "", ErrLateNeedsReview
	}
	b, err := loadInvoiceBalance(ctx, tx, invoiceID)
	if err != nil {
		return "", err
	}
	if b.OutstandingMinor <= 0 {
		return "", ErrConflict
	}
	var failed, unresolved int
	err = tx.QueryRowContext(ctx, `SELECT
COALESCE(SUM(CASE WHEN status='definitively_failed' THEN 1 ELSE 0 END),0),
COALESCE(SUM(CASE WHEN status NOT IN ('definitively_failed','cancelled') THEN 1 ELSE 0 END),0)
FROM payment_operations WHERE invoice_id=?`, invoiceID).Scan(&failed, &unresolved)
	if err != nil {
		return "", err
	}
	if failed == 0 || unresolved != 0 {
		return "", ErrConflict
	}
	opID, err := newID("op_")
	if err != nil {
		return "", err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO payment_operations(id,invoice_id,provider_key,amount_minor,currency,status)
VALUES(?,?,?,?,?,'created')`, opID, invoiceID, "capture:"+invoiceID+":"+opID, b.OutstandingMinor, b.Currency)
	if err != nil {
		return "", err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO outbox(id,kind,object_id,status) VALUES(?,'capture',?,'pending')`, "capture:"+opID, opID)
	if err != nil {
		return "", err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO payment_retry_requests(key,invoice_id,operation_id) VALUES(?,?,?)`, requestKey, invoiceID, opID)
	if err != nil {
		return "", err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO audit_events(kind,object_id,at) VALUES('payment_retry_created',?,?)`, opID, now.UnixNano())
	if err != nil {
		return "", err
	}
	return opID, nil
}

// SetFakePaymentDecision is a test/lab control, not a production payment API.
func (l *Lab) SetFakePaymentDecision(ctx context.Context, operationID, status string) error {
	var key string
	if err := l.db.QueryRowContext(ctx, `SELECT provider_key FROM payment_operations WHERE id=?`, operationID).Scan(&key); err != nil {
		return err
	}
	return l.provider.SetDecision(ctx, key, status)
}
