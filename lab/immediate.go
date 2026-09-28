package lab

import (
	"context"
	"database/sql"
	"errors"
	"math/big"
	"time"
)

type ImmediateChange struct {
	ID                   string
	SubscriptionID       string
	InvoiceID            string
	OperationID          string
	TargetPriceVersionID string
	SeatQuantity         int64
	OldCreditMinor       int64
	NewChargeMinor       int64
	QuotedAmountMinor    int64
	ActualAmountMinor    int64
	CorrectionMinor      int64
	Status               string
	RequestedAt          time.Time
	ActivatedAt          *time.Time
	Resolved             bool
}

func migrateImmediateChanges(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS immediate_changes (
 id TEXT PRIMARY KEY,
 subscription_id TEXT NOT NULL REFERENCES subscriptions(id),
 from_price_version_id TEXT NOT NULL REFERENCES price_versions(id),
 target_price_version_id TEXT NOT NULL REFERENCES price_versions(id),
 seat_quantity INTEGER NOT NULL CHECK(seat_quantity>0),
 requested_revision INTEGER NOT NULL,
 requested_at INTEGER NOT NULL,
 period_start INTEGER NOT NULL,
 period_end INTEGER NOT NULL,
 old_credit_minor INTEGER NOT NULL CHECK(old_credit_minor>=0),
 new_charge_minor INTEGER NOT NULL CHECK(new_charge_minor>0),
 quoted_amount_minor INTEGER NOT NULL CHECK(quoted_amount_minor>0),
 invoice_id TEXT NOT NULL UNIQUE REFERENCES invoices(id),
 operation_id TEXT NOT NULL UNIQUE REFERENCES payment_operations(id),
 status TEXT NOT NULL CHECK(status IN ('requested','active','definitively_failed','needs_review')),
 activated_at INTEGER,
 actual_amount_minor INTEGER NOT NULL DEFAULT 0 CHECK(actual_amount_minor>=0),
 correction_minor INTEGER NOT NULL DEFAULT 0 CHECK(correction_minor>=0),
 request_key TEXT NOT NULL UNIQUE,
 payload_hash TEXT NOT NULL,
 CHECK(period_end>period_start),
 CHECK(quoted_amount_minor=new_charge_minor-old_credit_minor));
CREATE UNIQUE INDEX IF NOT EXISTS one_requested_immediate_change ON immediate_changes(subscription_id) WHERE status='requested';
CREATE TABLE IF NOT EXISTS supplemental_invoices (
invoice_id TEXT PRIMARY KEY REFERENCES invoices(id),
subscription_id TEXT NOT NULL REFERENCES subscriptions(id),
period_start INTEGER NOT NULL,
period_end INTEGER NOT NULL,
due_at INTEGER NOT NULL,
kind TEXT NOT NULL CHECK(kind IN ('immediate_change')),
change_id TEXT NOT NULL UNIQUE REFERENCES immediate_changes(id),
CHECK(period_end>period_start));
CREATE TABLE IF NOT EXISTS immediate_change_resolutions (
change_id TEXT PRIMARY KEY REFERENCES immediate_changes(id),
correction_id TEXT NOT NULL UNIQUE REFERENCES corrections(id),
resolved_at INTEGER NOT NULL,
kind TEXT NOT NULL CHECK(kind='unfulfilled_refund_credit'));`)
	return err
}

func prorateHalfEven(amount, remaining, total int64) (int64, error) {
	if amount < 0 || remaining < 0 || total <= 0 || remaining > total {
		return 0, ErrConflict
	}
	n := new(big.Int).Mul(big.NewInt(amount), big.NewInt(remaining))
	d := big.NewInt(total)
	q, rem := new(big.Int), new(big.Int)
	q.QuoRem(n, d, rem)
	cmp := new(big.Int).Lsh(rem, 1).Cmp(d)
	if cmp > 0 || (cmp == 0 && q.Bit(0) == 1) {
		q.Add(q, big.NewInt(1))
	}
	if !q.IsInt64() {
		return 0, errors.New("proration amount overflow")
	}
	return q.Int64(), nil
}

func proratedUpgradeAmounts(oldAmount, newAmount, remaining, total int64) (int64, int64, int64, error) {
	oldCredit, err := prorateHalfEven(oldAmount, remaining, total)
	if err != nil {
		return 0, 0, 0, err
	}
	newCharge, err := prorateHalfEven(newAmount, remaining, total)
	if err != nil {
		return 0, 0, 0, err
	}
	if newCharge <= oldCredit {
		return 0, 0, 0, ErrConflict
	}
	return oldCredit, newCharge, newCharge - oldCredit, nil
}

// EstimateImmediateProUpgrade returns the amount payable if the change executes
// at the current clock time. The command recalculates this amount atomically.
func (l *Lab) EstimateImmediateProUpgrade(ctx context.Context, subID, targetPriceVersionID string, seats, expectedRevision int64) (int64, error) {
	if subID == "" || targetPriceVersionID == "" || seats <= 0 || expectedRevision <= 0 {
		return 0, ErrConflict
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	now := l.now().UTC().UnixNano()
	var status, fromVersion, fromPlan, targetPlan string
	var currentSeats, revision int64
	if err := tx.QueryRowContext(ctx, `SELECT status,price_version_id,seat_quantity,revision FROM subscriptions WHERE id=?`, subID).Scan(&status, &fromVersion, &currentSeats, &revision); err != nil {
		return 0, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT plan_id FROM price_versions WHERE id=?`, fromVersion).Scan(&fromPlan); err != nil {
		return 0, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT plan_id FROM price_versions WHERE id=?`, targetPriceVersionID).Scan(&targetPlan); err != nil {
		return 0, err
	}
	if status != "active" || revision != expectedRevision || fromPlan != "basic" || targetPlan != "pro" {
		return 0, ErrConflict
	}
	var pending int
	if err := tx.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM subscription_schedules WHERE subscription_id=? AND status='scheduled')+(SELECT COUNT(*) FROM immediate_changes WHERE subscription_id=? AND status='requested')+(SELECT COUNT(*) FROM subscription_ends WHERE subscription_id=?)+(SELECT COUNT(*) FROM price_migration_items WHERE subscription_id=? AND status IN ('pending','conflicted'))`, subID, subID, subID, subID).Scan(&pending); err != nil {
		return 0, err
	}
	if pending != 0 {
		return 0, ErrConflict
	}
	var periodStart, periodEnd int64
	var periodInvoice string
	if err := tx.QueryRowContext(ctx, `SELECT period_start,period_end,invoice_id FROM billing_periods WHERE subscription_id=? AND period_start<=? AND period_end>? ORDER BY period_index DESC LIMIT 1`, subID, now, now).Scan(&periodStart, &periodEnd, &periodInvoice); err != nil {
		return 0, err
	}
	balance, err := loadInvoiceBalance(ctx, tx, periodInvoice)
	if err != nil {
		return 0, err
	}
	if balance.OutstandingMinor != 0 {
		return 0, ErrConflict
	}
	oldTerms, err := loadPriceTerms(ctx, tx, fromVersion)
	if err != nil {
		return 0, err
	}
	newTerms, err := loadPriceTerms(ctx, tx, targetPriceVersionID)
	if err != nil {
		return 0, err
	}
	if oldTerms.Currency != newTerms.Currency {
		return 0, ErrConflict
	}
	oldAmount, err := oldTerms.UpfrontMinor(currentSeats)
	if err != nil {
		return 0, err
	}
	newAmount, err := newTerms.UpfrontMinor(seats)
	if err != nil {
		return 0, err
	}
	_, _, net, err := proratedUpgradeAmounts(oldAmount, newAmount, periodEnd-now, periodEnd-periodStart)
	return net, err
}

func loadPayablePeriod(ctx context.Context, q rowQuerier, invoiceID string) (int, int64, int64, error) {
	var index int
	var end, due int64
	err := q.QueryRowContext(ctx, `SELECT period_index,period_end,due_at FROM billing_periods WHERE invoice_id=?
UNION ALL SELECT -1,period_end,due_at FROM supplemental_invoices WHERE invoice_id=?`, invoiceID, invoiceID).
		Scan(&index, &end, &due)
	return index, end, due, err
}

func ensureImmediateChangePayable(ctx context.Context, q rowQuerier, invoiceID string) error {
	var status string
	err := q.QueryRowContext(ctx, `SELECT status FROM immediate_changes WHERE invoice_id=?`, invoiceID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if status != "requested" {
		return ErrConflict
	}
	return nil
}

func loadImmediateChange(ctx context.Context, q rowQuerier, id string) (ImmediateChange, error) {
	var x ImmediateChange
	var requested, activated sql.NullInt64
	err := q.QueryRowContext(ctx, `SELECT id,subscription_id,invoice_id,operation_id,target_price_version_id,seat_quantity,old_credit_minor,new_charge_minor,quoted_amount_minor,actual_amount_minor,correction_minor,status,requested_at,activated_at FROM immediate_changes WHERE id=?`, id).
		Scan(&x.ID, &x.SubscriptionID, &x.InvoiceID, &x.OperationID, &x.TargetPriceVersionID, &x.SeatQuantity, &x.OldCreditMinor, &x.NewChargeMinor, &x.QuotedAmountMinor, &x.ActualAmountMinor, &x.CorrectionMinor, &x.Status, &requested, &activated)
	if err != nil {
		return ImmediateChange{}, err
	}
	x.RequestedAt = time.Unix(0, requested.Int64).UTC()
	if activated.Valid {
		value := time.Unix(0, activated.Int64).UTC()
		x.ActivatedAt = &value
	}
	return x, nil
}

func (l *Lab) RequestImmediateProUpgrade(ctx context.Context, subID string, seats, expectedRevision int64, requestKey string) (ImmediateChange, error) {
	return l.requestImmediateProUpgrade(ctx, subID, seats, expectedRevision, requestKey, "", "")
}

func (l *Lab) RequestImmediateProUpgradeAtPrice(ctx context.Context, subID string, seats, expectedRevision int64, requestKey, quoteID, fingerprint string) (ImmediateChange, error) {
	if quoteID == "" || fingerprint == "" {
		return ImmediateChange{}, ErrConflict
	}
	return l.requestImmediateProUpgrade(ctx, subID, seats, expectedRevision, requestKey, quoteID, fingerprint)
}

func (l *Lab) requestImmediateProUpgrade(ctx context.Context, subID string, seats, expectedRevision int64, requestKey, quoteID, fingerprint string) (ImmediateChange, error) {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return ImmediateChange{}, err
	}
	defer tx.Rollback()
	change, err := l.requestImmediateProUpgradeTx(ctx, tx, l.now().UTC(), subID, seats, expectedRevision, requestKey, quoteID, fingerprint)
	if err != nil {
		return ImmediateChange{}, err
	}
	return change, tx.Commit()
}

func (l *Lab) requestImmediateProUpgradeTx(ctx context.Context, tx *sql.Tx, now time.Time, subID string, seats, expectedRevision int64, requestKey, quoteID, fingerprint string) (ImmediateChange, error) {
	if subID == "" || seats <= 0 || expectedRevision <= 0 || requestKey == "" {
		return ImmediateChange{}, errors.New("subscription, positive seats, revision and request key are required")
	}
	payloadHash := hash(subID, "immediate-pro", seats, expectedRevision)
	if quoteID != "" {
		payloadHash = hash(subID, "immediate-pro", seats, expectedRevision, quoteID, fingerprint)
	}
	var savedID, savedHash string
	err := tx.QueryRowContext(ctx, `SELECT id,payload_hash FROM immediate_changes WHERE request_key=?`, requestKey).Scan(&savedID, &savedHash)
	if err == nil {
		if savedHash != payloadHash {
			return ImmediateChange{}, ErrConflict
		}
		return loadImmediateChange(ctx, tx, savedID)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return ImmediateChange{}, err
	}
	quotedPriceVersionID := ""
	if quoteID != "" {
		var boundSub, mode, savedFingerprint string
		var boundRevision, expiry, quotedSeats int64
		if err := tx.QueryRowContext(ctx, `SELECT q.price_version_id,q.expires_at,q.seat_quantity,b.subscription_id,b.mode,b.expected_revision,b.fingerprint FROM quotes q JOIN change_quote_bindings b ON b.quote_id=q.id WHERE q.id=?`, quoteID).Scan(&quotedPriceVersionID, &expiry, &quotedSeats, &boundSub, &mode, &boundRevision, &savedFingerprint); err != nil {
			return ImmediateChange{}, err
		}
		if boundSub != subID || mode != "immediate" || boundRevision != expectedRevision || quotedSeats != seats || savedFingerprint != fingerprint || !now.Before(time.Unix(0, expiry)) {
			return ImmediateChange{}, ErrConflict
		}
	}
	var status, fromVersion string
	var revision, currentSeats int64
	err = tx.QueryRowContext(ctx, `SELECT status,price_version_id,seat_quantity,revision FROM subscriptions WHERE id=?`, subID).
		Scan(&status, &fromVersion, &currentSeats, &revision)
	if err != nil {
		return ImmediateChange{}, err
	}
	if status != "active" || revision != expectedRevision {
		return ImmediateChange{}, ErrConflict
	}
	var commandCustomer string
	if err := tx.QueryRowContext(ctx, `SELECT customer_id FROM subscriptions WHERE id=?`, subID).Scan(&commandCustomer); err != nil {
		return ImmediateChange{}, err
	}
	if err := ensureCommerceWriter(ctx, tx, commandCustomer); err != nil {
		return ImmediateChange{}, err
	}
	var fromPlan string
	if err := tx.QueryRowContext(ctx, `SELECT plan_id FROM price_versions WHERE id=?`, fromVersion).Scan(&fromPlan); err != nil {
		return ImmediateChange{}, err
	}
	if fromPlan != "basic" {
		return ImmediateChange{}, ErrConflict
	}
	var pending int
	if err := tx.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM subscription_schedules WHERE subscription_id=? AND status='scheduled')+(SELECT COUNT(*) FROM immediate_changes WHERE subscription_id=? AND status='requested')+(SELECT COUNT(*) FROM subscription_ends WHERE subscription_id=?)+(SELECT COUNT(*) FROM price_migration_items WHERE subscription_id=? AND status IN ('pending','conflicted'))`, subID, subID, subID, subID).Scan(&pending); err != nil {
		return ImmediateChange{}, err
	}
	if pending != 0 {
		return ImmediateChange{}, ErrConflict
	}
	var periodStart, periodEnd int64
	var periodInvoice string
	err = tx.QueryRowContext(ctx, `SELECT period_start,period_end,invoice_id FROM billing_periods WHERE subscription_id=? AND period_start<=? AND period_end>? ORDER BY period_index DESC LIMIT 1`, subID, now.UnixNano(), now.UnixNano()).
		Scan(&periodStart, &periodEnd, &periodInvoice)
	if err != nil {
		return ImmediateChange{}, err
	}
	currentBalance, err := loadInvoiceBalance(ctx, tx, periodInvoice)
	if err != nil || currentBalance.OutstandingMinor != 0 {
		return ImmediateChange{}, ErrConflict
	}
	var target string
	err = tx.QueryRowContext(ctx, `SELECT p.id FROM catalog_selection c JOIN price_versions p ON p.id=c.price_version_id WHERE c.plan_id='pro' AND c.cohort='default' AND c.effective_at<=? AND p.publication_state='published' AND p.effective_from<=? AND (p.effective_to IS NULL OR p.effective_to>?) ORDER BY c.effective_at DESC LIMIT 1`, now.UnixNano(), now.UnixNano(), now.UnixNano()).Scan(&target)
	if err != nil {
		return ImmediateChange{}, err
	}
	if quotedPriceVersionID != "" && target != quotedPriceVersionID {
		return ImmediateChange{}, ErrConflict
	}
	oldTerms, err := loadPriceTerms(ctx, tx, fromVersion)
	if err != nil {
		return ImmediateChange{}, err
	}
	newTerms, err := loadPriceTerms(ctx, tx, target)
	if err != nil {
		return ImmediateChange{}, err
	}
	if oldTerms.Currency != newTerms.Currency {
		return ImmediateChange{}, ErrConflict
	}
	oldAmount, err := oldTerms.UpfrontMinor(currentSeats)
	if err != nil {
		return ImmediateChange{}, err
	}
	newAmount, err := newTerms.UpfrontMinor(seats)
	if err != nil {
		return ImmediateChange{}, err
	}
	remaining, total := periodEnd-now.UnixNano(), periodEnd-periodStart
	oldCredit, newCharge, net, err := proratedUpgradeAmounts(oldAmount, newAmount, remaining, total)
	if err != nil {
		return ImmediateChange{}, err
	}
	changeID, err := newID("change_")
	if err != nil {
		return ImmediateChange{}, err
	}
	invoiceID, err := newID("inv_")
	if err != nil {
		return ImmediateChange{}, err
	}
	opID, err := newID("op_")
	if err != nil {
		return ImmediateChange{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO invoices(id,subscription_id,total_minor,currency,finalized_at) VALUES(?,?,?,?,NULL)`, invoiceID, subID, net, oldTerms.Currency); err != nil {
		return ImmediateChange{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO invoice_lines(invoice_id,price_version_id,component_code,amount_minor) VALUES(?,?,'basic_unused',?)`, invoiceID, fromVersion, -oldCredit); err != nil {
		return ImmediateChange{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO invoice_lines(invoice_id,price_version_id,component_code,amount_minor) VALUES(?,?,'pro_remaining',?)`, invoiceID, target, newCharge); err != nil {
		return ImmediateChange{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE invoices SET finalized_at=? WHERE id=?`, now.UnixNano(), invoiceID); err != nil {
		return ImmediateChange{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO payment_operations(id,invoice_id,provider_key,amount_minor,currency,status) VALUES(?,?,?,?,?,'created')`, opID, invoiceID, "capture:"+invoiceID, net, oldTerms.Currency); err != nil {
		return ImmediateChange{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO immediate_changes(id,subscription_id,from_price_version_id,target_price_version_id,seat_quantity,requested_revision,requested_at,period_start,period_end,old_credit_minor,new_charge_minor,quoted_amount_minor,invoice_id,operation_id,status,request_key,payload_hash) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,'requested',?,?)`, changeID, subID, fromVersion, target, seats, expectedRevision+1, now.UnixNano(), periodStart, periodEnd, oldCredit, newCharge, net, invoiceID, opID, requestKey, payloadHash); err != nil {
		return ImmediateChange{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO supplemental_invoices(invoice_id,subscription_id,period_start,period_end,due_at,kind,change_id) VALUES(?,?,?,?,?,'immediate_change',?)`, invoiceID, subID, periodStart, periodEnd, now.UnixNano(), changeID); err != nil {
		return ImmediateChange{}, err
	}
	update, err := tx.ExecContext(ctx, `UPDATE subscriptions SET revision=revision+1 WHERE id=? AND revision=?`, subID, expectedRevision)
	if err != nil {
		return ImmediateChange{}, err
	}
	if n, err := update.RowsAffected(); err != nil || n != 1 {
		return ImmediateChange{}, ErrConflict
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO outbox(id,kind,object_id,status) VALUES(?,'capture',?,'pending')`, "capture:"+opID, opID); err != nil {
		return ImmediateChange{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_events(kind,object_id,at) VALUES('immediate_change_requested',?,?)`, changeID, now.UnixNano()); err != nil {
		return ImmediateChange{}, err
	}
	return ImmediateChange{ID: changeID, SubscriptionID: subID, InvoiceID: invoiceID, OperationID: opID, TargetPriceVersionID: target, SeatQuantity: seats, OldCreditMinor: oldCredit, NewChargeMinor: newCharge, QuotedAmountMinor: net, Status: "requested", RequestedAt: now}, nil
}

func (l *Lab) observeImmediateChange(ctx context.Context, tx *sql.Tx, invoiceID, result string) error {
	var id, subID, fromVersion, targetVersion, status string
	var seats, requestedRevision, periodStart, periodEnd, quoted int64
	err := tx.QueryRowContext(ctx, `SELECT id,subscription_id,from_price_version_id,target_price_version_id,status,seat_quantity,requested_revision,period_start,period_end,quoted_amount_minor FROM immediate_changes WHERE invoice_id=?`, invoiceID).
		Scan(&id, &subID, &fromVersion, &targetVersion, &status, &seats, &requestedRevision, &periodStart, &periodEnd, &quoted)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if status != "requested" {
		return nil
	}
	if result == "definitively_failed" {
		_, err := tx.ExecContext(ctx, `UPDATE immediate_changes SET status='definitively_failed' WHERE id=? AND status='requested'`, id)
		return err
	}
	balance, err := loadInvoiceBalance(ctx, tx, invoiceID)
	if err != nil || balance.OutstandingMinor != 0 {
		return err
	}
	now := l.now().UTC()
	var revision int64
	var subStatus string
	if err := tx.QueryRowContext(ctx, `SELECT revision,status FROM subscriptions WHERE id=?`, subID).Scan(&revision, &subStatus); err != nil {
		return err
	}
	var pendingSchedule, ended int
	if err := tx.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM subscription_schedules WHERE subscription_id=? AND status='scheduled'),(SELECT COUNT(*) FROM subscription_ends WHERE subscription_id=?)`, subID, subID).Scan(&pendingSchedule, &ended); err != nil {
		return err
	}
	if now.UnixNano() >= periodEnd || revision != requestedRevision || subStatus != "active" || pendingSchedule != 0 || ended != 0 {
		_, err := tx.ExecContext(ctx, `UPDATE immediate_changes SET status='needs_review' WHERE id=? AND status='requested'`, id)
		return err
	}
	oldTerms, err := loadPriceTerms(ctx, tx, fromVersion)
	if err != nil {
		return err
	}
	newTerms, err := loadPriceTerms(ctx, tx, targetVersion)
	if err != nil {
		return err
	}
	oldAmount, err := oldTerms.UpfrontMinor(0)
	if err != nil {
		return err
	}
	newAmount, err := newTerms.UpfrontMinor(seats)
	if err != nil {
		return err
	}
	remaining, total := periodEnd-now.UnixNano(), periodEnd-periodStart
	oldCredit, err := prorateHalfEven(oldAmount, remaining, total)
	if err != nil {
		return err
	}
	newCharge, err := prorateHalfEven(newAmount, remaining, total)
	if err != nil {
		return err
	}
	actual := newCharge - oldCredit
	correction := quoted - actual
	if actual <= 0 || correction < 0 {
		_, err := tx.ExecContext(ctx, `UPDATE immediate_changes SET status='needs_review' WHERE id=? AND status='requested'`, id)
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE pricing_assignments SET effective_end=? WHERE subscription_id=? AND effective_end IS NULL`, now.UnixNano(), subID); err != nil {
		return err
	}
	var nextIndex int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(assignment_index),-1)+1 FROM pricing_assignments WHERE subscription_id=?`, subID).Scan(&nextIndex); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO pricing_assignments(subscription_id,assignment_index,price_version_id,seat_quantity,effective_start) VALUES(?,?,?,?,?)`, subID, nextIndex, targetVersion, seats, now.UnixNano()); err != nil {
		return err
	}
	update, err := tx.ExecContext(ctx, `UPDATE subscriptions SET price_version_id=?,seat_quantity=?,revision=revision+1 WHERE id=? AND revision=?`, targetVersion, seats, subID, requestedRevision)
	if err != nil {
		return err
	}
	if n, err := update.RowsAffected(); err != nil || n != 1 {
		return ErrConflict
	}
	if _, err := tx.ExecContext(ctx, `UPDATE immediate_changes SET status='active',activated_at=?,actual_amount_minor=?,correction_minor=? WHERE id=? AND status='requested'`, now.UnixNano(), actual, correction, id); err != nil {
		return err
	}
	if correction > 0 {
		if _, err := tx.ExecContext(ctx, `INSERT INTO outbox(id,kind,object_id,status) VALUES(?,'change_correction',?,'pending')`, "change-correction:"+id, id); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO audit_events(kind,object_id,at) VALUES('immediate_change_activated',?,?)`, id, now.UnixNano())
	return err
}

func (l *Lab) RunChangeCorrections(ctx context.Context) ([]CorrectionResult, error) {
	rows, err := l.db.QueryContext(ctx, `SELECT object_id FROM outbox WHERE kind='change_correction' AND status='pending' ORDER BY rowid`)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
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
	var results []CorrectionResult
	for _, id := range ids {
		tx, err := l.db.BeginTx(ctx, nil)
		if err != nil {
			return results, err
		}
		result, err := l.runChangeCorrectionTx(ctx, tx, l.now().UTC(), id)
		if err != nil {
			tx.Rollback()
			return results, err
		}
		if err := tx.Commit(); err != nil {
			return results, err
		}
		results = append(results, result)
	}
	return results, nil
}

func (l *Lab) runChangeCorrectionTx(ctx context.Context, tx *sql.Tx, at time.Time, changeID string) (CorrectionResult, error) {
	var pending int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox WHERE id=? AND kind='change_correction' AND object_id=? AND status='pending'`, "change-correction:"+changeID, changeID).Scan(&pending); err != nil {
		return CorrectionResult{}, err
	}
	if pending != 1 {
		return CorrectionResult{}, ErrConflict
	}
	var invoiceID string
	var amount int64
	if err := tx.QueryRowContext(ctx, `SELECT invoice_id,correction_minor FROM immediate_changes WHERE id=? AND status='active'`, changeID).Scan(&invoiceID, &amount); err != nil {
		return CorrectionResult{}, err
	}
	result, err := l.postReductionTx(ctx, tx, at, invoiceID, amount, "delayed Pro activation", "change-correction:"+changeID, false)
	if err != nil {
		return CorrectionResult{}, err
	}
	updated, err := tx.ExecContext(ctx, `UPDATE outbox SET status='done' WHERE id=? AND status='pending'`, "change-correction:"+changeID)
	if err != nil {
		return CorrectionResult{}, err
	}
	if count, err := updated.RowsAffected(); err != nil || count != 1 {
		return CorrectionResult{}, ErrConflict
	}
	return result, nil
}

// ResolveUnfulfilledImmediateChange removes the full supplemental obligation
// when payment arrived after the service period and no upgrade was activated.
// PostReduction releases only captured funds as credit; an operator can then
// reserve a refund against that credit.
func (l *Lab) ResolveUnfulfilledImmediateChange(ctx context.Context, changeID string) (CorrectionResult, error) {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return CorrectionResult{}, err
	}
	defer tx.Rollback()
	result, err := l.resolveUnfulfilledImmediateChangeTx(ctx, tx, l.now().UTC(), changeID)
	if err != nil {
		return CorrectionResult{}, err
	}
	return result, tx.Commit()
}

func (l *Lab) resolveUnfulfilledImmediateChangeTx(ctx context.Context, tx *sql.Tx, at time.Time, changeID string) (CorrectionResult, error) {
	change, err := loadImmediateChange(ctx, tx, changeID)
	if err != nil {
		return CorrectionResult{}, err
	}
	if change.Status != "needs_review" {
		return CorrectionResult{}, ErrConflict
	}
	result, err := l.postReductionTx(ctx, tx, at, change.InvoiceID, change.QuotedAmountMinor, "late payment without Pro activation", "unfulfilled-change:"+changeID, true)
	if err != nil {
		return CorrectionResult{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO immediate_change_resolutions(change_id,correction_id,resolved_at,kind)
VALUES(?,?,?,'unfulfilled_refund_credit') ON CONFLICT(change_id) DO NOTHING`, changeID, result.ID, at.UnixNano())
	if err != nil {
		return CorrectionResult{}, err
	}
	return result, nil
}
