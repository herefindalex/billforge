package lab

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

var (
	ErrConflict                   = errors.New("conflicting identity or payload")
	ErrChangeQuoteBindingMismatch = fmt.Errorf("%w: quote does not match its change binding", ErrConflict)
	ErrChangeQuoteRevisionChanged = fmt.Errorf("%w: subscription revision changed after quote binding", ErrConflict)
	ErrChangeQuotePriceSuperseded = fmt.Errorf("%w: quote price is no longer selected", ErrConflict)
	ErrExpired                    = errors.New("quote expired")
	ErrPeriodEnded                = errors.New("service period ended")
	ErrLateNeedsReview            = errors.New("late payment requires service correction review")
	ErrInjectedCrash              = errors.New("injected crash after provider capture")
	ErrPaymentUnknown             = errors.New("provider response lost; payment outcome unknown")
)

type Clock func() time.Time

type Lab struct {
	db               *sql.DB
	provider         *FakeProvider
	now              Clock
	baseNow          Clock
	clockMu          sync.RWMutex
	fixedClock       *time.Time
	clockRevision    int64
	batchMu          sync.Mutex
	adminExecutionMu sync.Mutex
	workerID         string
}

type Quote struct {
	ID                string
	CustomerID        string
	PriceVersionID    string
	ContractVersionID string
	SeatQuantity      int64
	AmountMinor       int64
	Currency          string
	ExpiresAt         time.Time
	Fingerprint       string
}

type Receipt struct {
	SubscriptionID string
	InvoiceID      string
	OperationID    string
	AmountMinor    int64
	Currency       string
}

type Snapshot struct {
	SubscriptionStatus string
	InvoiceTotalMinor  int64
	OperationStatus    string
	AllocationCount    int64
	AllocatedMinor     int64
	EntitlementStatus  string
}

func (l *Lab) ClockTime() time.Time {
	l.clockMu.RLock()
	fixed := l.fixedClock
	l.clockMu.RUnlock()
	if fixed != nil {
		return fixed.UTC()
	}
	return l.now().UTC()
}

// Open uses distinct SQLite files so the simulated provider's committed
// capture can survive a commerce crash. The paths must not be the same.
func Open(localPath, providerPath string, clock Clock) (*Lab, error) {
	if localPath == providerPath {
		return nil, errors.New("commerce and provider databases must be distinct")
	}
	if clock == nil {
		clock = time.Now
	}
	db, err := sql.Open("sqlite3", localPath)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err = migrateLegacy(db); err != nil {
		db.Close()
		return nil, err
	}
	// The allocation budget now includes corrections and credit applications.
	if _, err = db.Exec(`DROP TRIGGER IF EXISTS allocation_not_overfunded`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err = db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	if err = migrateCatalog(db); err != nil {
		db.Close()
		return nil, err
	}
	if err = migrateMeterCatalog(db); err != nil {
		db.Close()
		return nil, err
	}
	if err = migrateChangeQuotes(db); err != nil {
		db.Close()
		return nil, err
	}
	if err = migrateAccountMigration(db); err != nil {
		db.Close()
		return nil, err
	}
	if _, err = db.Exec(`INSERT INTO price_versions(id,plan_id,version,currency,fixed_amount_minor,published_at,checksum)
		VALUES('basic-v1','basic',1,'USD',2000,0,?) ON CONFLICT(id) DO NOTHING;
		INSERT INTO catalog_selection(plan_id,cohort,effective_at,price_version_id)
		VALUES('basic','default',0,'basic-v1') ON CONFLICT(plan_id,cohort,effective_at) DO NOTHING;`, hash("basic-v1", "basic", 1, "USD", 2000)); err != nil {
		db.Close()
		return nil, err
	}
	if err = ensureEntitlementColumns(db); err != nil {
		db.Close()
		return nil, err
	}
	if err = backfillInitialPeriods(db); err != nil {
		db.Close()
		return nil, err
	}
	if err = migrateSubscriptionSchedules(db); err != nil {
		db.Close()
		return nil, err
	}
	if err = migrateImmediateChanges(db); err != nil {
		db.Close()
		return nil, err
	}
	if err = migratePriceMigrations(db); err != nil {
		db.Close()
		return nil, err
	}
	if err = migrateUsage(db); err != nil {
		db.Close()
		return nil, err
	}
	if err = migrateContracts(db); err != nil {
		db.Close()
		return nil, err
	}
	if err = migrateReconciliation(db); err != nil {
		db.Close()
		return nil, err
	}
	var foreignKeyErrors int
	if err = db.QueryRow(`SELECT COUNT(*) FROM pragma_foreign_key_check`).Scan(&foreignKeyErrors); err != nil {
		db.Close()
		return nil, fmt.Errorf("foreign key check: %w", err)
	}
	if foreignKeyErrors != 0 {
		db.Close()
		return nil, fmt.Errorf("foreign key check found %d violations", foreignKeyErrors)
	}
	provider, err := openProvider(providerPath)
	if err != nil {
		db.Close()
		return nil, err
	}
	workerID, err := newID("worker_")
	if err != nil {
		provider.Close()
		db.Close()
		return nil, err
	}
	l := &Lab{db: db, provider: provider, baseNow: clock, workerID: workerID}
	l.now = func() time.Time {
		l.clockMu.RLock()
		fixed := l.fixedClock
		l.clockMu.RUnlock()
		if fixed != nil {
			return fixed.UTC()
		}
		return clock()
	}
	var hasAdminClock int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='admin_lab_clock'`).Scan(&hasAdminClock); err != nil {
		provider.Close()
		db.Close()
		return nil, err
	}
	if hasAdminClock != 0 {
		if err := l.loadAdminClock(context.Background()); err != nil {
			provider.Close()
			db.Close()
			return nil, err
		}
	}
	return l, nil
}

func (l *Lab) Close() error {
	a := l.db.Close()
	b := l.provider.Close()
	return errors.Join(a, b)
}

func newID(prefix string) (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(b[:]), nil
}

func hash(parts ...any) string {
	h := sha256.New()
	for _, part := range parts {
		fmt.Fprintf(h, "%d:%v|", len(fmt.Sprint(part)), part)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (l *Lab) CreateQuote(ctx context.Context, customerID, planID string) (Quote, error) {
	return l.CreateQuoteWithSeats(ctx, customerID, planID, 0)
}

func (l *Lab) CreateQuoteWithSeats(ctx context.Context, customerID, planID string, seats int64) (Quote, error) {
	return l.CreateQuoteForCohort(ctx, customerID, planID, "default", seats)
}

func (l *Lab) CreateQuoteForCohort(ctx context.Context, customerID, planID, cohort string, seats int64) (Quote, error) {
	return l.createQuoteForCohort(ctx, l.db, l.now().UTC(), customerID, planID, cohort, seats)
}

type quoteWriter interface {
	priceQuerier
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func (l *Lab) createQuoteForCohort(ctx context.Context, db quoteWriter, at time.Time, customerID, planID, cohort string, seats int64) (Quote, error) {
	if customerID == "" || planID == "" || cohort == "" {
		return Quote{}, errors.New("customer and plan are required")
	}
	at = at.UTC()
	expires, expiryErr := quoteExpiryAt(at)
	if expiryErr != nil {
		return Quote{}, expiryErr
	}
	q := Quote{CustomerID: customerID, SeatQuantity: seats, ExpiresAt: expires}
	err := db.QueryRowContext(ctx, `SELECT p.id FROM catalog_selection c JOIN price_versions p ON p.id=c.price_version_id WHERE c.plan_id=? AND c.cohort=? AND c.effective_at<=? AND p.publication_state='published' AND p.effective_from<=? AND (p.effective_to IS NULL OR p.effective_to>?) ORDER BY c.effective_at DESC LIMIT 1`, planID, cohort, at.UnixNano(), at.UnixNano(), at.UnixNano()).Scan(&q.PriceVersionID)
	if err != nil {
		return Quote{}, err
	}
	terms, err := loadPriceTerms(ctx, db, q.PriceVersionID)
	if err != nil {
		return Quote{}, err
	}
	q.AmountMinor, err = terms.UpfrontMinor(seats)
	if err != nil {
		return Quote{}, err
	}
	q.Currency = terms.Currency
	q.ID, err = newID("quote_")
	if err != nil {
		return Quote{}, err
	}
	q.Fingerprint = hash(q.ID, customerID, q.PriceVersionID, terms.Checksum, seats, q.AmountMinor, q.Currency, q.ExpiresAt.UnixNano())
	_, err = db.ExecContext(ctx, `INSERT INTO quotes(id,customer_id,price_version_id,amount_minor,currency,expires_at,fingerprint,seat_quantity) VALUES(?,?,?,?,?,?,?,?)`, q.ID, q.CustomerID, q.PriceVersionID, q.AmountMinor, q.Currency, q.ExpiresAt.UnixNano(), q.Fingerprint, seats)
	return q, err
}

// AcceptQuote commits the invoice, a single payment obligation and its outbox
// job together. No provider call occurs inside this transaction.
func (l *Lab) AcceptQuote(ctx context.Context, quoteID, fingerprint, idempotencyKey string) (Receipt, error) {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return Receipt{}, err
	}
	defer tx.Rollback()
	receipt, err := l.acceptQuoteTx(ctx, tx, l.now().UTC(), quoteID, fingerprint, idempotencyKey)
	if err != nil {
		return Receipt{}, err
	}
	return receipt, tx.Commit()
}

func (l *Lab) acceptQuoteTx(ctx context.Context, tx *sql.Tx, at time.Time, quoteID, fingerprint, idempotencyKey string) (Receipt, error) {
	var err error
	at = at.UTC()
	if quoteID == "" || fingerprint == "" || idempotencyKey == "" {
		return Receipt{}, errors.New("quote, fingerprint and idempotency key are required")
	}
	var contractQuote int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM contract_quotes WHERE quote_id=?`, quoteID).Scan(&contractQuote); err != nil {
		return Receipt{}, err
	}
	if contractQuote != 0 {
		return Receipt{}, ErrConflict
	}
	payloadHash := hash(quoteID, fingerprint)
	var savedHash, savedSub string
	err = tx.QueryRowContext(ctx, `SELECT payload_hash,subscription_id FROM accept_requests WHERE key=?`, idempotencyKey).Scan(&savedHash, &savedSub)
	if err == nil {
		if savedHash != payloadHash {
			return Receipt{}, ErrConflict
		}
		var r Receipt
		err = tx.QueryRowContext(ctx, `SELECT s.id,i.id,o.id,i.total_minor,i.currency FROM subscriptions s
JOIN billing_periods p ON p.subscription_id=s.id AND p.period_index=0
JOIN invoices i ON i.id=p.invoice_id JOIN payment_operations o ON o.invoice_id=i.id
WHERE s.id=? ORDER BY o.rowid LIMIT 1`, savedSub).Scan(&r.SubscriptionID, &r.InvoiceID, &r.OperationID, &r.AmountMinor, &r.Currency)
		return r, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Receipt{}, err
	}
	if at.Before(minUnixNanoTime) || at.After(maxUnixNanoTime) {
		return Receipt{}, ErrConflict
	}
	var q Quote
	var expiry int64
	err = tx.QueryRowContext(ctx, `SELECT customer_id,price_version_id,amount_minor,currency,expires_at,fingerprint,seat_quantity
		FROM quotes WHERE id=?`, quoteID).Scan(&q.CustomerID, &q.PriceVersionID, &q.AmountMinor, &q.Currency, &expiry, &q.Fingerprint, &q.SeatQuantity)
	if err != nil {
		return Receipt{}, err
	}
	if q.Fingerprint != fingerprint {
		return Receipt{}, ErrConflict
	}
	var boundChange int
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM change_quote_bindings WHERE quote_id=?)`, quoteID).Scan(&boundChange); err != nil {
		return Receipt{}, err
	}
	if boundChange != 0 {
		return Receipt{}, ErrConflict
	}
	if err := ensureCommerceWriter(ctx, tx, q.CustomerID); err != nil {
		return Receipt{}, err
	}
	if at.UnixNano() >= expiry {
		return Receipt{}, ErrExpired
	}
	var acceptedID string
	err = tx.QueryRowContext(ctx, `SELECT id FROM subscriptions WHERE quote_id=?`, quoteID).Scan(&acceptedID)
	if err == nil {
		return Receipt{}, ErrConflict
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Receipt{}, err
	}
	var published int64
	var state string
	err = tx.QueryRowContext(ctx, `SELECT published_at,publication_state FROM price_versions WHERE id=?`, q.PriceVersionID).Scan(&published, &state)
	if err != nil || state != "published" || published > at.UnixNano() {
		return Receipt{}, errors.New("price version unavailable")
	}
	terms, err := loadPriceTerms(ctx, tx, q.PriceVersionID)
	if err != nil {
		return Receipt{}, err
	}
	calculated, err := terms.UpfrontMinor(q.SeatQuantity)
	if err != nil || calculated != q.AmountMinor || terms.Currency != q.Currency {
		return Receipt{}, ErrConflict
	}
	acceptedAt := at
	periodEnd := cycleBoundary(acceptedAt, 1)
	if !periodEnd.After(acceptedAt) || periodEnd.After(maxUnixNanoTime) {
		return Receipt{}, ErrConflict
	}
	var r Receipt
	for _, id := range []*string{&r.SubscriptionID, &r.InvoiceID, &r.OperationID} {
		*id, err = newID("")
		if err != nil {
			return Receipt{}, err
		}
	}
	r.SubscriptionID = "sub_" + r.SubscriptionID
	r.InvoiceID = "inv_" + r.InvoiceID
	r.OperationID = "op_" + r.OperationID
	r.AmountMinor, r.Currency = q.AmountMinor, q.Currency
	_, err = tx.ExecContext(ctx, `INSERT INTO subscriptions(id,quote_id,customer_id,price_version_id,status,created_at,seat_quantity)
		VALUES(?,?,?,?,'pending',?,?)`, r.SubscriptionID, quoteID, q.CustomerID, q.PriceVersionID, acceptedAt.UnixNano(), q.SeatQuantity)
	if err != nil {
		return Receipt{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO invoices(id,subscription_id,total_minor,currency,finalized_at)
VALUES(?,?,?,?,NULL)`, r.InvoiceID, r.SubscriptionID, q.AmountMinor, q.Currency)
	if err != nil {
		return Receipt{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO invoice_lines(invoice_id,price_version_id,component_code,amount_minor)
		VALUES(?,?,'fixed',?)`, r.InvoiceID, q.PriceVersionID, terms.FixedMinor)
	if err != nil {
		return Receipt{}, err
	}
	if terms.SeatMinor > 0 {
		if _, err = tx.ExecContext(ctx, `INSERT INTO invoice_lines(invoice_id,price_version_id,component_code,amount_minor)
			VALUES(?,?,'seats',?)`, r.InvoiceID, q.PriceVersionID, terms.SeatMinor*q.SeatQuantity); err != nil {
			return Receipt{}, err
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE invoices SET finalized_at=? WHERE id=?`, acceptedAt.UnixNano(), r.InvoiceID)
	if err != nil {
		return Receipt{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO billing_periods(subscription_id,period_index,period_start,period_end,due_at,invoice_id)
		VALUES(?,0,?,?,?,?)`, r.SubscriptionID, acceptedAt.UnixNano(), periodEnd.UnixNano(), acceptedAt.UnixNano(), r.InvoiceID)
	if err != nil {
		return Receipt{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO pricing_assignments(subscription_id,assignment_index,price_version_id,seat_quantity,effective_start) VALUES(?,0,?,?,?)`, r.SubscriptionID, q.PriceVersionID, q.SeatQuantity, acceptedAt.UnixNano()); err != nil {
		return Receipt{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO payment_operations(id,invoice_id,provider_key,amount_minor,currency,status)
VALUES(?,?,?,?,?,'created')`, r.OperationID, r.InvoiceID, "capture:"+r.InvoiceID, q.AmountMinor, q.Currency)
	if err != nil {
		return Receipt{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO outbox(id,kind,object_id,status) VALUES(?, 'capture', ?, 'pending')`, "capture:"+r.OperationID, r.OperationID)
	if err != nil {
		return Receipt{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO accept_requests(key,payload_hash,subscription_id) VALUES(?,?,?)`, idempotencyKey, payloadHash, r.SubscriptionID)
	if err != nil {
		return Receipt{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO audit_events(kind,object_id,at) VALUES('quote_accepted',?,?)`, r.SubscriptionID, acceptedAt.UnixNano())
	if err != nil {
		return Receipt{}, err
	}
	return r, nil
}

// DispatchNext captures one obligation. The fault modes deliberately return
// after the provider has committed, before the local outcome is known.
func (l *Lab) DispatchNext(ctx context.Context, fault string) (string, error) {
	return l.dispatchCapture(ctx, "", fault)
}

func (l *Lab) DispatchCapture(ctx context.Context, operationID, fault string) (string, error) {
	if operationID == "" {
		return "", ErrConflict
	}
	return l.dispatchCapture(ctx, operationID, fault)
}

func (l *Lab) dispatchCapture(ctx context.Context, operationID, fault string) (string, error) {
	return l.dispatchCaptureAt(ctx, operationID, fault, l.ClockTime())
}

func (l *Lab) dispatchCaptureAt(ctx context.Context, operationID, fault string, at time.Time) (string, error) {
	if fault != "" && fault != "crash_after_provider" && fault != "lost_response" {
		return "", errors.New("unsupported fault")
	}
	at = at.UTC()
	if at.Before(minUnixNanoTime) || at.After(maxUnixNanoTime) {
		return "", ErrConflict
	}
	var opID, key, currency, status string
	var amount int64
	query := `SELECT o.id,o.provider_key,o.amount_minor,o.currency,o.status
FROM outbox b JOIN payment_operations o ON o.id=b.object_id
WHERE b.kind='capture' AND b.status='pending'`
	var args []any
	if operationID != "" {
		query += ` AND o.id=?`
		args = append(args, operationID)
	}
	query += ` ORDER BY b.rowid LIMIT 1`
	err := l.db.QueryRowContext(ctx, query, args...).Scan(&opID, &key, &amount, &currency, &status)
	if err != nil {
		return "", err
	}
	if status == "submitted" || status == "unknown" {
		e, found, err := l.provider.Lookup(ctx, key)
		if err != nil {
			return opID, err
		}
		if !found {
			return opID, ErrPaymentUnknown
		}
		e.ID = "lookup:" + key
		return opID, l.applyObservationAt(ctx, e, at)
	}
	if status != "created" {
		return opID, ErrConflict
	}
	var periodIndex int
	var periodEnd, dueAt int64
	err = l.db.QueryRowContext(ctx, `SELECT p.period_index,p.period_end,p.due_at FROM payment_operations o
		JOIN billing_periods p ON p.invoice_id=o.invoice_id WHERE o.id=?
		UNION ALL SELECT -1,s.period_end,s.due_at FROM payment_operations o
		JOIN supplemental_invoices s ON s.invoice_id=o.invoice_id WHERE o.id=?`, opID, opID).
		Scan(&periodIndex, &periodEnd, &dueAt)
	if err != nil {
		return opID, err
	}
	var invoiceID string
	if err := l.db.QueryRowContext(ctx, `SELECT invoice_id FROM payment_operations WHERE id=?`, opID).Scan(&invoiceID); err != nil {
		return opID, err
	}
	contractInvoice, err := isContractInvoice(ctx, l.db, invoiceID)
	if err != nil {
		return opID, err
	}
	if contractInvoice && at.UnixNano() < dueAt {
		return opID, ErrConflict
	}
	if !contractInvoice && at.UnixNano() >= periodEnd {
		return opID, ErrPeriodEnded
	}
	if !contractInvoice && periodIndex > 0 && at.UnixNano() > time.Unix(0, dueAt).Add(renewalGrace).UnixNano() {
		return opID, ErrLateNeedsReview
	}
	var subscriptionStatus string
	if err := l.db.QueryRowContext(ctx, `SELECT s.status FROM payment_operations o JOIN invoices i ON i.id=o.invoice_id JOIN subscriptions s ON s.id=i.subscription_id WHERE o.id=?`, opID).Scan(&subscriptionStatus); err != nil {
		return opID, err
	}
	if subscriptionStatus == "pending" {
		activationEnd := cycleBoundary(at, 1)
		if !activationEnd.After(at) || activationEnd.After(maxUnixNanoTime) {
			return opID, ErrConflict
		}
	}
	if err := l.adminMarkSubmitted(ctx, "payment_operations", opID); err != nil {
		return opID, err
	}
	e, err := l.provider.Capture(ctx, key, amount, currency)
	if err != nil {
		return opID, err
	}
	switch fault {
	case "crash_after_provider":
		return opID, ErrInjectedCrash
	case "lost_response":
		_, err = l.db.ExecContext(ctx, `UPDATE payment_operations SET status='unknown'
WHERE id=? AND status!='succeeded'`, opID)
		if err != nil {
			return opID, err
		}
		return opID, ErrPaymentUnknown
	case "":
		return opID, l.applyObservationAt(ctx, e, at)
	}
	return opID, errors.New("unreachable fault mode")
}

func (l *Lab) ReconcilePayment(ctx context.Context, opID string) (bool, error) {
	var key string
	err := l.db.QueryRowContext(ctx, `SELECT provider_key FROM payment_operations WHERE id=?`, opID).Scan(&key)
	if err != nil {
		return false, err
	}
	e, found, err := l.provider.Lookup(ctx, key)
	if err != nil || !found {
		return found, err
	}
	e.ID = "lookup:" + key
	return true, l.applyObservation(ctx, e)
}

// HandleWebhook verifies success against the provider's own persisted fact.
// Pending observations are recorded but cannot reverse a confirmed capture.
func (l *Lab) HandleWebhook(ctx context.Context, e ProviderEvent) error {
	if e.Status == "succeeded" || e.Status == "definitively_failed" {
		actual, found, err := l.provider.Lookup(ctx, e.Key)
		if err != nil {
			return err
		}
		if !found || actual.Status != e.Status || actual.Amount != e.Amount || actual.Currency != e.Currency {
			return ErrConflict
		}
	}
	return l.applyObservation(ctx, e)
}

func (l *Lab) applyObservation(ctx context.Context, e ProviderEvent) error {
	return l.applyObservationAt(ctx, e, l.ClockTime())
}

func (l *Lab) applyObservationAt(ctx context.Context, e ProviderEvent, at time.Time) error {
	if e.ID == "" || e.Key == "" || (e.Status != "pending" && e.Status != "succeeded" && e.Status != "definitively_failed") {
		return errors.New("invalid provider observation")
	}
	at = at.UTC()
	if at.Before(minUnixNanoTime) || at.After(maxUnixNanoTime) {
		return ErrConflict
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Provider evidence is committed with the operation state. Reserve the
	// SQLite writer slot before reading either one so a concurrent replacement
	// cannot turn this deferred read transaction into a failed lock upgrade.
	if _, err := tx.ExecContext(ctx, `UPDATE payment_operations SET status=status WHERE provider_key=?`, e.Key); err != nil {
		return err
	}
	var opID, invoiceID, subID, opStatus, subStatus, currency string
	var amount int64
	err = tx.QueryRowContext(ctx, `SELECT o.id,o.invoice_id,i.subscription_id,o.status,s.status,o.amount_minor,o.currency
FROM payment_operations o JOIN invoices i ON i.id=o.invoice_id
JOIN subscriptions s ON s.id=i.subscription_id WHERE o.provider_key=?`, e.Key).
		Scan(&opID, &invoiceID, &subID, &opStatus, &subStatus, &amount, &currency)
	if err != nil {
		return err
	}
	if e.Amount != amount || e.Currency != currency {
		return ErrConflict
	}
	if opStatus == "cancelled" ||
		(opStatus == "succeeded" && e.Status == "definitively_failed") ||
		(opStatus == "definitively_failed" && e.Status == "succeeded") {
		return ErrConflict
	}
	fingerprint := hash(e.Key, e.Status, e.Amount, e.Currency)
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
	if e.Status == "succeeded" {
		_, err = tx.ExecContext(ctx, `UPDATE payment_operations SET status='succeeded' WHERE id=?`, opID)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO allocations(operation_id,invoice_id,amount_minor)
VALUES(?,?,?) ON CONFLICT(operation_id) DO NOTHING`, opID, invoiceID, amount)
		if err != nil {
			return err
		}
		balance, err := loadInvoiceBalance(ctx, tx, invoiceID)
		if err != nil {
			return err
		}
		if subStatus == "pending" && balance.OutstandingMinor == 0 {
			activatedAt := at.UTC()
			activationEnd := cycleBoundary(activatedAt, 1)
			if !activationEnd.After(activatedAt) || activationEnd.After(maxUnixNanoTime) {
				return ErrConflict
			}
			_, err = tx.ExecContext(ctx, `UPDATE billing_periods SET period_start=?,period_end=?
WHERE subscription_id=? AND period_index=0`, activatedAt.UnixNano(), activationEnd.UnixNano(), subID)
			if err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `UPDATE pricing_assignments SET effective_start=? WHERE subscription_id=? AND assignment_index=0 AND effective_end IS NULL`, activatedAt.UnixNano(), subID); err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx, `UPDATE subscriptions SET status='active' WHERE id=? AND status='pending'`, subID)
			if err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO outbox(id,kind,object_id,status)
VALUES(?,'entitlement',?,'pending') ON CONFLICT(id) DO NOTHING`, "entitlement:"+opID, subID)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE outbox SET status='done' WHERE id=?`, "capture:"+opID)
		if err != nil {
			return err
		}
	}
	if e.Status == "definitively_failed" {
		_, err = tx.ExecContext(ctx, `UPDATE payment_operations SET status='definitively_failed' WHERE id=?`, opID)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE outbox SET status='done' WHERE id=?`, "capture:"+opID)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO outbox(id,kind,object_id,status)
VALUES(?,'entitlement',?,'pending') ON CONFLICT(id) DO NOTHING`, "entitlement:"+opID, subID)
		if err != nil {
			return err
		}
	}
	if err := l.observeImmediateChange(ctx, tx, invoiceID, e.Status); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO audit_events(kind,object_id,at) VALUES(?,?,?)`, "provider_"+e.Status, opID, at.UnixNano())
	if err != nil {
		return err
	}
	return tx.Commit()
}

// RebuildEntitlements is safe to rerun after a crash. Source facts, not a
// payment boolean, determine whether the self-serve subscription is active.
func (l *Lab) RebuildEntitlements(ctx context.Context) error {
	return l.RefreshEntitlements(ctx)
}

func (l *Lab) Snapshot(ctx context.Context, subscriptionID string) (Snapshot, error) {
	var s Snapshot
	err := l.db.QueryRowContext(ctx, `SELECT CASE WHEN EXISTS(SELECT 1 FROM subscription_ends se WHERE se.subscription_id=sub.id) THEN 'ended' ELSE sub.status END,i.total_minor,o.status,
(SELECT COUNT(*) FROM allocations a WHERE a.invoice_id=i.id),
COALESCE((SELECT SUM(a.amount_minor) FROM allocations a WHERE a.invoice_id=i.id),0),
COALESCE((SELECT e.status FROM entitlements e WHERE e.subscription_id=sub.id),'pending')
FROM subscriptions sub
JOIN billing_periods p ON p.subscription_id=sub.id
JOIN invoices i ON i.id=p.invoice_id
JOIN payment_operations o ON o.invoice_id=i.id
WHERE sub.id=? ORDER BY p.period_index DESC,o.rowid DESC LIMIT 1`, subscriptionID).
		Scan(&s.SubscriptionStatus, &s.InvoiceTotalMinor, &s.OperationStatus,
			&s.AllocationCount, &s.AllocatedMinor, &s.EntitlementStatus)
	return s, err
}

const schema = `PRAGMA foreign_keys=ON;
PRAGMA busy_timeout=5000;
CREATE TABLE IF NOT EXISTS price_versions (
 id TEXT PRIMARY KEY, plan_id TEXT NOT NULL, version INTEGER NOT NULL,
 currency TEXT NOT NULL, fixed_amount_minor INTEGER NOT NULL CHECK(fixed_amount_minor>0),
 published_at INTEGER NOT NULL, checksum TEXT NOT NULL, UNIQUE(plan_id,version));
CREATE TRIGGER IF NOT EXISTS price_immutable_update BEFORE UPDATE ON price_versions BEGIN SELECT RAISE(ABORT,'published price immutable'); END;
CREATE TRIGGER IF NOT EXISTS price_immutable_delete BEFORE DELETE ON price_versions BEGIN SELECT RAISE(ABORT,'published price immutable'); END;
CREATE TABLE IF NOT EXISTS catalog_selection (
 plan_id TEXT NOT NULL, cohort TEXT NOT NULL, effective_at INTEGER NOT NULL,
 price_version_id TEXT NOT NULL REFERENCES price_versions(id),
 PRIMARY KEY(plan_id,cohort,effective_at));
CREATE TRIGGER IF NOT EXISTS selection_plan_match BEFORE INSERT ON catalog_selection
WHEN NEW.plan_id != (SELECT plan_id FROM price_versions WHERE id=NEW.price_version_id)
BEGIN SELECT RAISE(ABORT,'catalog selection plan mismatch'); END;
CREATE TRIGGER IF NOT EXISTS selection_immutable_update BEFORE UPDATE ON catalog_selection BEGIN SELECT RAISE(ABORT,'catalog selection immutable'); END;
CREATE TRIGGER IF NOT EXISTS selection_immutable_delete BEFORE DELETE ON catalog_selection BEGIN SELECT RAISE(ABORT,'catalog selection immutable'); END;
CREATE TABLE IF NOT EXISTS quotes (
 id TEXT PRIMARY KEY, customer_id TEXT NOT NULL, price_version_id TEXT NOT NULL REFERENCES price_versions(id),
 amount_minor INTEGER NOT NULL CHECK(amount_minor>0), currency TEXT NOT NULL,
 expires_at INTEGER NOT NULL, fingerprint TEXT NOT NULL);
CREATE TRIGGER IF NOT EXISTS quote_immutable_update BEFORE UPDATE ON quotes BEGIN SELECT RAISE(ABORT,'quote immutable'); END;
CREATE TRIGGER IF NOT EXISTS quote_immutable_delete BEFORE DELETE ON quotes BEGIN SELECT RAISE(ABORT,'quote immutable'); END;
CREATE TABLE IF NOT EXISTS subscriptions (
 id TEXT PRIMARY KEY, quote_id TEXT NOT NULL UNIQUE REFERENCES quotes(id),
 customer_id TEXT NOT NULL, price_version_id TEXT NOT NULL REFERENCES price_versions(id),
 status TEXT NOT NULL CHECK(status IN ('pending','active')), created_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS invoices (
 id TEXT PRIMARY KEY, subscription_id TEXT NOT NULL REFERENCES subscriptions(id),
 total_minor INTEGER NOT NULL CHECK(total_minor>0), currency TEXT NOT NULL, finalized_at INTEGER);
CREATE TABLE IF NOT EXISTS billing_periods (
 subscription_id TEXT NOT NULL REFERENCES subscriptions(id), period_index INTEGER NOT NULL CHECK(period_index>=0),
 period_start INTEGER NOT NULL, period_end INTEGER NOT NULL, due_at INTEGER NOT NULL,
 invoice_id TEXT NOT NULL UNIQUE REFERENCES invoices(id),
 PRIMARY KEY(subscription_id,period_index), UNIQUE(subscription_id,period_start), CHECK(period_end>period_start));
CREATE TRIGGER IF NOT EXISTS period_immutable_update BEFORE UPDATE ON billing_periods
WHEN OLD.period_index!=0 OR (SELECT status FROM subscriptions WHERE id=OLD.subscription_id)!='pending'
BEGIN SELECT RAISE(ABORT,'effective billing period immutable'); END;
CREATE TRIGGER IF NOT EXISTS period_immutable_delete BEFORE DELETE ON billing_periods
BEGIN SELECT RAISE(ABORT,'billing period immutable'); END;
CREATE TABLE IF NOT EXISTS renewal_holds (
 subscription_id TEXT NOT NULL REFERENCES subscriptions(id), period_index INTEGER NOT NULL,
 reason TEXT NOT NULL, detected_at INTEGER NOT NULL,
 PRIMARY KEY(subscription_id,period_index));
CREATE TABLE IF NOT EXISTS invoice_lines (
 invoice_id TEXT NOT NULL REFERENCES invoices(id), price_version_id TEXT NOT NULL REFERENCES price_versions(id),
 component_code TEXT NOT NULL, amount_minor INTEGER NOT NULL, PRIMARY KEY(invoice_id,component_code));
CREATE TRIGGER IF NOT EXISTS invoice_finalization_balance BEFORE UPDATE OF finalized_at ON invoices
WHEN OLD.finalized_at IS NULL AND NEW.finalized_at IS NOT NULL AND
COALESCE((SELECT SUM(amount_minor) FROM invoice_lines WHERE invoice_id=NEW.id),0) != NEW.total_minor
BEGIN SELECT RAISE(ABORT,'invoice lines do not equal total'); END;
CREATE TRIGGER IF NOT EXISTS invoice_immutable_update BEFORE UPDATE ON invoices
WHEN OLD.finalized_at IS NOT NULL BEGIN SELECT RAISE(ABORT,'finalized invoice immutable'); END;
CREATE TRIGGER IF NOT EXISTS invoice_immutable_delete BEFORE DELETE ON invoices BEGIN SELECT RAISE(ABORT,'finalized invoice immutable'); END;
CREATE TRIGGER IF NOT EXISTS line_after_finalization BEFORE INSERT ON invoice_lines
WHEN (SELECT finalized_at FROM invoices WHERE id=NEW.invoice_id) IS NOT NULL
BEGIN SELECT RAISE(ABORT,'finalized invoice immutable'); END;
CREATE TRIGGER IF NOT EXISTS line_immutable_update BEFORE UPDATE ON invoice_lines BEGIN SELECT RAISE(ABORT,'finalized line immutable'); END;
CREATE TRIGGER IF NOT EXISTS line_immutable_delete BEFORE DELETE ON invoice_lines BEGIN SELECT RAISE(ABORT,'finalized line immutable'); END;
CREATE TABLE IF NOT EXISTS payment_operations (
 id TEXT PRIMARY KEY, invoice_id TEXT NOT NULL REFERENCES invoices(id),
 provider_key TEXT NOT NULL UNIQUE, amount_minor INTEGER NOT NULL CHECK(amount_minor>0),
 currency TEXT NOT NULL, status TEXT NOT NULL CHECK(status IN ('created','submitted','unknown','succeeded','definitively_failed','cancelled')));
CREATE TRIGGER IF NOT EXISTS operation_matches_invoice BEFORE INSERT ON payment_operations
WHEN NEW.amount_minor > (SELECT total_minor FROM invoices WHERE id=NEW.invoice_id)
OR NEW.currency != (SELECT currency FROM invoices WHERE id=NEW.invoice_id)
OR (SELECT finalized_at FROM invoices WHERE id=NEW.invoice_id) IS NULL
BEGIN SELECT RAISE(ABORT,'operation does not match finalized invoice'); END;
CREATE TRIGGER IF NOT EXISTS operation_obligation_budget BEFORE INSERT ON payment_operations
WHEN NEW.amount_minor >
(SELECT total_minor FROM invoices WHERE id=NEW.invoice_id)
- COALESCE((SELECT SUM(reduction_minor) FROM corrections WHERE invoice_id=NEW.invoice_id),0)
- COALESCE((SELECT SUM(amount_minor) FROM allocations WHERE invoice_id=NEW.invoice_id),0)
+ COALESCE((SELECT SUM(r.amount_minor) FROM allocation_releases r JOIN allocations a ON a.operation_id=r.operation_id WHERE a.invoice_id=NEW.invoice_id),0)
- COALESCE((SELECT SUM(amount_minor) FROM credit_applications WHERE invoice_id=NEW.invoice_id),0)
- COALESCE((SELECT SUM(amount_minor) FROM payment_operations WHERE invoice_id=NEW.invoice_id AND status IN ('created','submitted','unknown')),0)
BEGIN SELECT RAISE(ABORT,'payment operation exceeds open obligation'); END;
CREATE TRIGGER IF NOT EXISTS operation_payload_immutable BEFORE UPDATE OF invoice_id,provider_key,amount_minor,currency
ON payment_operations BEGIN SELECT RAISE(ABORT,'payment operation payload immutable'); END;
CREATE TRIGGER IF NOT EXISTS operation_success_final BEFORE UPDATE OF status ON payment_operations
WHEN OLD.status='succeeded' AND NEW.status!='succeeded'
BEGIN SELECT RAISE(ABORT,'successful payment cannot regress'); END;
CREATE TRIGGER IF NOT EXISTS operation_failure_final BEFORE UPDATE OF status ON payment_operations
WHEN OLD.status='definitively_failed' AND NEW.status!='definitively_failed'
BEGIN SELECT RAISE(ABORT,'failed operation cannot be reused'); END;
CREATE TRIGGER IF NOT EXISTS operation_cancel_final BEFORE UPDATE OF status ON payment_operations
WHEN OLD.status='cancelled' AND NEW.status!='cancelled'
BEGIN SELECT RAISE(ABORT,'cancelled operation cannot be reused'); END;
CREATE TABLE IF NOT EXISTS allocations (
 operation_id TEXT PRIMARY KEY REFERENCES payment_operations(id), invoice_id TEXT NOT NULL REFERENCES invoices(id),
 amount_minor INTEGER NOT NULL CHECK(amount_minor>0));
CREATE TRIGGER IF NOT EXISTS allocation_matches_operation BEFORE INSERT ON allocations
WHEN NEW.amount_minor != (SELECT amount_minor FROM payment_operations WHERE id=NEW.operation_id)
OR NEW.invoice_id != (SELECT invoice_id FROM payment_operations WHERE id=NEW.operation_id)
OR (SELECT status FROM payment_operations WHERE id=NEW.operation_id) != 'succeeded'
BEGIN SELECT RAISE(ABORT,'allocation does not match successful operation'); END;
CREATE TRIGGER IF NOT EXISTS allocation_not_overfunded BEFORE INSERT ON allocations
WHEN NOT EXISTS (SELECT 1 FROM allocations WHERE operation_id=NEW.operation_id)
AND COALESCE((SELECT SUM(amount_minor) FROM allocations WHERE invoice_id=NEW.invoice_id),0)
- COALESCE((SELECT SUM(r.amount_minor) FROM allocation_releases r JOIN allocations a ON a.operation_id=r.operation_id WHERE a.invoice_id=NEW.invoice_id),0)
+ COALESCE((SELECT SUM(amount_minor) FROM credit_applications WHERE invoice_id=NEW.invoice_id),0)
+ NEW.amount_minor
> (SELECT total_minor FROM invoices WHERE id=NEW.invoice_id)
- COALESCE((SELECT SUM(reduction_minor) FROM corrections WHERE invoice_id=NEW.invoice_id),0)
BEGIN SELECT RAISE(ABORT,'invoice overfunded'); END;
CREATE TRIGGER IF NOT EXISTS allocation_immutable_update BEFORE UPDATE ON allocations BEGIN SELECT RAISE(ABORT,'allocation immutable'); END;
CREATE TRIGGER IF NOT EXISTS allocation_immutable_delete BEFORE DELETE ON allocations BEGIN SELECT RAISE(ABORT,'allocation immutable'); END;
CREATE TABLE IF NOT EXISTS corrections (
 id TEXT PRIMARY KEY, invoice_id TEXT NOT NULL REFERENCES invoices(id),
 reduction_minor INTEGER NOT NULL CHECK(reduction_minor>0),
 prior_obligation_minor INTEGER NOT NULL, new_obligation_minor INTEGER NOT NULL CHECK(new_obligation_minor>=0),
 reason TEXT NOT NULL, request_key TEXT NOT NULL UNIQUE, created_at INTEGER NOT NULL);
CREATE TRIGGER IF NOT EXISTS correction_budget BEFORE INSERT ON corrections
WHEN NEW.reduction_minor > (SELECT total_minor FROM invoices WHERE id=NEW.invoice_id)
- COALESCE((SELECT SUM(reduction_minor) FROM corrections WHERE invoice_id=NEW.invoice_id),0)
BEGIN SELECT RAISE(ABORT,'correction exceeds obligation'); END;
CREATE TRIGGER IF NOT EXISTS correction_snapshot BEFORE INSERT ON corrections
WHEN NEW.prior_obligation_minor != (SELECT total_minor FROM invoices WHERE id=NEW.invoice_id)
- COALESCE((SELECT SUM(reduction_minor) FROM corrections WHERE invoice_id=NEW.invoice_id),0)
OR NEW.new_obligation_minor != NEW.prior_obligation_minor-NEW.reduction_minor
BEGIN SELECT RAISE(ABORT,'correction obligation snapshot mismatch'); END;
CREATE TRIGGER IF NOT EXISTS correction_immutable_update BEFORE UPDATE ON corrections BEGIN SELECT RAISE(ABORT,'correction immutable'); END;
CREATE TRIGGER IF NOT EXISTS correction_immutable_delete BEFORE DELETE ON corrections BEGIN SELECT RAISE(ABORT,'correction immutable'); END;
CREATE TABLE IF NOT EXISTS allocation_releases (
 id TEXT PRIMARY KEY, correction_id TEXT NOT NULL REFERENCES corrections(id),
 operation_id TEXT NOT NULL REFERENCES allocations(operation_id),
 amount_minor INTEGER NOT NULL CHECK(amount_minor>0), UNIQUE(correction_id,operation_id));
CREATE TRIGGER IF NOT EXISTS release_budget BEFORE INSERT ON allocation_releases
WHEN NEW.amount_minor + COALESCE((SELECT SUM(amount_minor) FROM allocation_releases WHERE operation_id=NEW.operation_id),0)
> (SELECT amount_minor FROM allocations WHERE operation_id=NEW.operation_id)
BEGIN SELECT RAISE(ABORT,'allocation release exceeds capture'); END;
CREATE TRIGGER IF NOT EXISTS release_source BEFORE INSERT ON allocation_releases
WHEN (SELECT invoice_id FROM allocations WHERE operation_id=NEW.operation_id)
!= (SELECT invoice_id FROM corrections WHERE id=NEW.correction_id)
BEGIN SELECT RAISE(ABORT,'release source invoice mismatch'); END;
CREATE TRIGGER IF NOT EXISTS release_immutable_update BEFORE UPDATE ON allocation_releases BEGIN SELECT RAISE(ABORT,'release immutable'); END;
CREATE TRIGGER IF NOT EXISTS release_immutable_delete BEFORE DELETE ON allocation_releases BEGIN SELECT RAISE(ABORT,'release immutable'); END;
CREATE TABLE IF NOT EXISTS credit_grants (
 id TEXT PRIMARY KEY, release_id TEXT NOT NULL UNIQUE REFERENCES allocation_releases(id),
 source_operation_id TEXT NOT NULL REFERENCES payment_operations(id),
 source_invoice_id TEXT NOT NULL REFERENCES invoices(id),
 amount_minor INTEGER NOT NULL CHECK(amount_minor>0), currency TEXT NOT NULL, created_at INTEGER NOT NULL);
CREATE TRIGGER IF NOT EXISTS grant_matches_release BEFORE INSERT ON credit_grants
WHEN NEW.amount_minor != (SELECT amount_minor FROM allocation_releases WHERE id=NEW.release_id)
OR NEW.source_operation_id != (SELECT operation_id FROM allocation_releases WHERE id=NEW.release_id)
OR NEW.source_invoice_id != (SELECT invoice_id FROM allocations WHERE operation_id=NEW.source_operation_id)
OR NEW.currency != (SELECT currency FROM payment_operations WHERE id=NEW.source_operation_id)
BEGIN SELECT RAISE(ABORT,'credit grant has no matching funded release'); END;
CREATE TRIGGER IF NOT EXISTS grant_immutable_update BEFORE UPDATE ON credit_grants BEGIN SELECT RAISE(ABORT,'credit grant immutable'); END;
CREATE TRIGGER IF NOT EXISTS grant_immutable_delete BEFORE DELETE ON credit_grants BEGIN SELECT RAISE(ABORT,'credit grant immutable'); END;
CREATE TABLE IF NOT EXISTS credit_applications (
 id TEXT PRIMARY KEY, grant_id TEXT NOT NULL REFERENCES credit_grants(id),
 invoice_id TEXT NOT NULL REFERENCES invoices(id), amount_minor INTEGER NOT NULL CHECK(amount_minor>0),
 request_key TEXT NOT NULL UNIQUE, created_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS refund_operations (
 id TEXT PRIMARY KEY, grant_id TEXT NOT NULL REFERENCES credit_grants(id),
 provider_key TEXT NOT NULL UNIQUE, source_provider_key TEXT NOT NULL,
 amount_minor INTEGER NOT NULL CHECK(amount_minor>0), currency TEXT NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('created','submitted','unknown','succeeded','definitively_failed')),
 request_key TEXT NOT NULL UNIQUE, created_at INTEGER NOT NULL);
CREATE TRIGGER IF NOT EXISTS credit_application_budget BEFORE INSERT ON credit_applications
WHEN NEW.amount_minor + COALESCE((SELECT SUM(amount_minor) FROM credit_applications WHERE grant_id=NEW.grant_id),0)
+ COALESCE((SELECT SUM(amount_minor) FROM refund_operations WHERE grant_id=NEW.grant_id AND status!='definitively_failed'),0)
> (SELECT amount_minor FROM credit_grants WHERE id=NEW.grant_id)
BEGIN SELECT RAISE(ABORT,'credit grant exhausted'); END;
CREATE TRIGGER IF NOT EXISTS credit_application_source BEFORE INSERT ON credit_applications
WHEN NEW.invoice_id=(SELECT source_invoice_id FROM credit_grants WHERE id=NEW.grant_id)
BEGIN SELECT RAISE(ABORT,'credit cannot apply to source invoice'); END;
CREATE TRIGGER IF NOT EXISTS credit_application_customer BEFORE INSERT ON credit_applications
WHEN (SELECT s.customer_id FROM invoices i JOIN subscriptions s ON s.id=i.subscription_id WHERE i.id=NEW.invoice_id)
!= (SELECT s.customer_id FROM credit_grants g JOIN invoices i ON i.id=g.source_invoice_id
JOIN subscriptions s ON s.id=i.subscription_id WHERE g.id=NEW.grant_id)
BEGIN SELECT RAISE(ABORT,'credit customer mismatch'); END;
CREATE TRIGGER IF NOT EXISTS credit_application_target_budget BEFORE INSERT ON credit_applications
WHEN NEW.amount_minor >
(SELECT total_minor FROM invoices WHERE id=NEW.invoice_id)
- COALESCE((SELECT SUM(reduction_minor) FROM corrections WHERE invoice_id=NEW.invoice_id),0)
- COALESCE((SELECT SUM(amount_minor) FROM allocations WHERE invoice_id=NEW.invoice_id),0)
+ COALESCE((SELECT SUM(r.amount_minor) FROM allocation_releases r JOIN allocations a ON a.operation_id=r.operation_id WHERE a.invoice_id=NEW.invoice_id),0)
- COALESCE((SELECT SUM(amount_minor) FROM credit_applications WHERE invoice_id=NEW.invoice_id),0)
BEGIN SELECT RAISE(ABORT,'credit application exceeds invoice outstanding'); END;
CREATE TRIGGER IF NOT EXISTS refund_reservation_budget BEFORE INSERT ON refund_operations
WHEN NEW.amount_minor + COALESCE((SELECT SUM(amount_minor) FROM refund_operations WHERE grant_id=NEW.grant_id AND status!='definitively_failed'),0)
+ COALESCE((SELECT SUM(amount_minor) FROM credit_applications WHERE grant_id=NEW.grant_id),0)
> (SELECT amount_minor FROM credit_grants WHERE id=NEW.grant_id)
BEGIN SELECT RAISE(ABORT,'credit grant exhausted'); END;
CREATE TRIGGER IF NOT EXISTS refund_payload_immutable BEFORE UPDATE OF grant_id,provider_key,source_provider_key,amount_minor,currency,request_key
ON refund_operations BEGIN SELECT RAISE(ABORT,'refund payload immutable'); END;
CREATE TRIGGER IF NOT EXISTS refund_terminal_immutable BEFORE UPDATE OF status ON refund_operations
WHEN OLD.status IN ('succeeded','definitively_failed') AND NEW.status!=OLD.status
BEGIN SELECT RAISE(ABORT,'refund terminal state immutable'); END;
CREATE TRIGGER IF NOT EXISTS credit_application_immutable_update BEFORE UPDATE ON credit_applications BEGIN SELECT RAISE(ABORT,'credit application immutable'); END;
CREATE TRIGGER IF NOT EXISTS credit_application_immutable_delete BEFORE DELETE ON credit_applications BEGIN SELECT RAISE(ABORT,'credit application immutable'); END;
CREATE TABLE IF NOT EXISTS payment_requests (
 key TEXT PRIMARY KEY, invoice_id TEXT NOT NULL REFERENCES invoices(id),
 amount_minor INTEGER NOT NULL, operation_id TEXT NOT NULL UNIQUE REFERENCES payment_operations(id));
CREATE TABLE IF NOT EXISTS accept_requests (
 key TEXT PRIMARY KEY, payload_hash TEXT NOT NULL, subscription_id TEXT NOT NULL REFERENCES subscriptions(id));
CREATE TABLE IF NOT EXISTS payment_retry_requests (
 key TEXT PRIMARY KEY, invoice_id TEXT NOT NULL REFERENCES invoices(id),
 operation_id TEXT NOT NULL UNIQUE REFERENCES payment_operations(id));
CREATE TABLE IF NOT EXISTS outbox (
 id TEXT PRIMARY KEY, kind TEXT NOT NULL, object_id TEXT NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('pending','done')));
CREATE TABLE IF NOT EXISTS inbox (
 event_id TEXT PRIMARY KEY, provider_key TEXT NOT NULL, status TEXT NOT NULL,
 payload_hash TEXT NOT NULL, received_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS entitlements (
 subscription_id TEXT PRIMARY KEY REFERENCES subscriptions(id), status TEXT NOT NULL,
 source_operation_id TEXT NOT NULL REFERENCES payment_operations(id), updated_at INTEGER NOT NULL,
 reason TEXT NOT NULL DEFAULT 'payment_confirmed', source_invoice_id TEXT REFERENCES invoices(id),
 grace_deadline INTEGER);
CREATE TABLE IF NOT EXISTS audit_events (
 id INTEGER PRIMARY KEY AUTOINCREMENT, kind TEXT NOT NULL, object_id TEXT NOT NULL, at INTEGER NOT NULL);`
