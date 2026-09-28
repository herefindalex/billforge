package lab

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"time"
)

type UsageEvent struct {
	TenantID       string
	Source         string
	EventID        string
	SubscriptionID string
	MeterID        string
	Quantity       int64
	EventAt        time.Time
	ReceivedAt     time.Time
	PeriodIndex    int
	PriceVersionID string
}

type UsageRating struct {
	ID                    string
	SubscriptionID        string
	PeriodIndex           int
	Revision              int
	PriceVersionID        string
	Quantity              int64
	IncludedQuantity      int64
	OverageQuantity       int64
	ExactMinorNumerator   int64
	ExactMinorDenominator int64
	RoundedMinor          int64
	DeltaMinor            int64
	RatedAt               time.Time
}

type UsagePeriodState struct {
	SubscriptionID string
	PeriodIndex    int
	PriceVersionID string
	CutoffAt       time.Time
	ClosedAt       time.Time
	RatedMinor     int64
	BilledMinor    int64
	CreditedMinor  int64
	AppliedCount   int
}

type UsageCreditNoteState struct {
	ID              string
	SubscriptionID  string
	PeriodIndex     int
	SourceInvoiceID string
	AmountMinor     int64
	CorrectionID    string
}

func migrateUsage(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS usage_events (
tenant_id TEXT NOT NULL,
source TEXT NOT NULL,
event_id TEXT NOT NULL,
payload_hash TEXT NOT NULL,
subscription_id TEXT NOT NULL REFERENCES subscriptions(id),
meter_id TEXT NOT NULL,
	quantity INTEGER NOT NULL CHECK(quantity!=0),
	correction_source TEXT NOT NULL DEFAULT '',
	correction_event_id TEXT NOT NULL DEFAULT '',
event_at INTEGER NOT NULL,
received_at INTEGER NOT NULL,
period_index INTEGER NOT NULL,
price_version_id TEXT NOT NULL REFERENCES price_versions(id),
PRIMARY KEY(tenant_id,source,event_id));
CREATE TABLE IF NOT EXISTS usage_periods (
subscription_id TEXT NOT NULL REFERENCES subscriptions(id),
period_index INTEGER NOT NULL,
price_version_id TEXT NOT NULL REFERENCES price_versions(id),
cutoff_at INTEGER NOT NULL,
closed_at INTEGER NOT NULL,
rated_minor INTEGER NOT NULL CHECK(rated_minor>=0),
	billed_minor INTEGER NOT NULL DEFAULT 0 CHECK(billed_minor>=0),
	credited_minor INTEGER NOT NULL DEFAULT 0 CHECK(credited_minor>=0),
applied_count INTEGER NOT NULL DEFAULT 0 CHECK(applied_count>=0),
PRIMARY KEY(subscription_id,period_index));
CREATE TABLE IF NOT EXISTS usage_ratings (
id TEXT PRIMARY KEY,
subscription_id TEXT NOT NULL,
period_index INTEGER NOT NULL,
revision INTEGER NOT NULL CHECK(revision>0),
price_version_id TEXT NOT NULL REFERENCES price_versions(id),
quantity INTEGER NOT NULL CHECK(quantity>=0),
included_quantity INTEGER NOT NULL CHECK(included_quantity>=0),
overage_quantity INTEGER NOT NULL CHECK(overage_quantity>=0),
exact_minor_numerator INTEGER NOT NULL CHECK(exact_minor_numerator>=0),
exact_minor_denominator INTEGER NOT NULL CHECK(exact_minor_denominator>0),
rounded_minor INTEGER NOT NULL CHECK(rounded_minor>=0),
delta_minor INTEGER NOT NULL,
rated_at INTEGER NOT NULL,
UNIQUE(subscription_id,period_index,revision),
FOREIGN KEY(subscription_id,period_index) REFERENCES usage_periods(subscription_id,period_index));
CREATE TABLE IF NOT EXISTS usage_invoice_applications (
subscription_id TEXT NOT NULL,
period_index INTEGER NOT NULL,
invoice_id TEXT NOT NULL REFERENCES invoices(id),
amount_minor INTEGER NOT NULL CHECK(amount_minor>=0),
applied_at INTEGER NOT NULL,
PRIMARY KEY(subscription_id,period_index,invoice_id),
FOREIGN KEY(subscription_id,period_index) REFERENCES usage_periods(subscription_id,period_index));
CREATE TABLE IF NOT EXISTS usage_credit_notes (
id TEXT PRIMARY KEY,
subscription_id TEXT NOT NULL,
period_index INTEGER NOT NULL,
source_invoice_id TEXT NOT NULL REFERENCES invoices(id),
amount_minor INTEGER NOT NULL CHECK(amount_minor>0),
correction_id TEXT NOT NULL UNIQUE REFERENCES corrections(id),
request_key TEXT NOT NULL UNIQUE,
created_at INTEGER NOT NULL,
FOREIGN KEY(subscription_id,period_index) REFERENCES usage_periods(subscription_id,period_index));`)
	return err
}

func (l *Lab) RecordUsage(ctx context.Context, source, eventID, subID, meterID string, quantity int64, eventAt time.Time) (UsageEvent, error) {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return UsageEvent{}, err
	}
	defer tx.Rollback()
	event, err := l.recordUsageTx(ctx, tx, l.now().UTC(), source, eventID, subID, meterID, quantity, eventAt)
	if err != nil {
		return UsageEvent{}, err
	}
	return event, tx.Commit()
}

func (l *Lab) recordUsageTx(ctx context.Context, tx *sql.Tx, at time.Time, source, eventID, subID, meterID string, quantity int64, eventAt time.Time) (UsageEvent, error) {
	if source == "" || eventID == "" || subID == "" || meterID == "" || quantity <= 0 || !unixNanoTimeFits(eventAt) {
		return UsageEvent{}, ErrConflict
	}
	var err error
	var tenant string
	if err := tx.QueryRowContext(ctx, `SELECT customer_id FROM subscriptions WHERE id=?`, subID).Scan(&tenant); err != nil {
		return UsageEvent{}, err
	}
	fingerprint := hash(subID, meterID, quantity, eventAt.UTC().UnixNano())
	var savedHash string
	err = tx.QueryRowContext(ctx, `SELECT payload_hash FROM usage_events WHERE tenant_id=? AND source=? AND event_id=?`, tenant, source, eventID).Scan(&savedHash)
	if err == nil {
		if savedHash != fingerprint {
			return UsageEvent{}, ErrConflict
		}
		var e UsageEvent
		var occurred, received int64
		err = tx.QueryRowContext(ctx, `SELECT subscription_id,meter_id,quantity,event_at,received_at,period_index,price_version_id FROM usage_events WHERE tenant_id=? AND source=? AND event_id=?`, tenant, source, eventID).Scan(&e.SubscriptionID, &e.MeterID, &e.Quantity, &occurred, &received, &e.PeriodIndex, &e.PriceVersionID)
		if err != nil {
			return UsageEvent{}, err
		}
		e.TenantID, e.Source, e.EventID = tenant, source, eventID
		e.EventAt, e.ReceivedAt = time.Unix(0, occurred).UTC(), time.Unix(0, received).UTC()
		return e, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return UsageEvent{}, err
	}
	if !unixNanoTimeFits(at) {
		return UsageEvent{}, ErrConflict
	}
	if err := ensureCommerceWriter(ctx, tx, tenant); err != nil {
		return UsageEvent{}, err
	}
	var index int
	if err := tx.QueryRowContext(ctx, `SELECT period_index FROM billing_periods WHERE subscription_id=? AND period_start<=? AND period_end>? ORDER BY period_index DESC LIMIT 1`, subID, eventAt.UTC().UnixNano(), eventAt.UTC().UnixNano()).Scan(&index); err != nil {
		return UsageEvent{}, err
	}
	var version string
	if err := tx.QueryRowContext(ctx, `SELECT price_version_id FROM pricing_assignments WHERE subscription_id=? AND effective_start<=? AND (effective_end IS NULL OR effective_end>?) ORDER BY assignment_index DESC LIMIT 1`, subID, eventAt.UTC().UnixNano(), eventAt.UTC().UnixNano()).Scan(&version); err != nil {
		return UsageEvent{}, err
	}
	terms, err := loadPriceTerms(ctx, tx, version)
	if err != nil {
		return UsageEvent{}, err
	}
	if terms.UsageRateNum <= 0 || terms.IncludedQuantity <= 0 || terms.MeterID != meterID {
		return UsageEvent{}, ErrConflict
	}
	var meterSource string
	if err := tx.QueryRowContext(ctx, `SELECT source FROM meter_schemas WHERE id=?`, meterID).Scan(&meterSource); err != nil {
		return UsageEvent{}, err
	}
	if meterSource != "*" && meterSource != source {
		return UsageEvent{}, ErrConflict
	}
	now := at.UTC()
	if eventAt.After(now) {
		return UsageEvent{}, ErrConflict
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO usage_events(tenant_id,source,event_id,payload_hash,subscription_id,meter_id,quantity,event_at,received_at,period_index,price_version_id) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, tenant, source, eventID, fingerprint, subID, meterID, quantity, eventAt.UTC().UnixNano(), now.UnixNano(), index, version); err != nil {
		return UsageEvent{}, err
	}
	return UsageEvent{TenantID: tenant, Source: source, EventID: eventID, SubscriptionID: subID, MeterID: meterID, Quantity: quantity, EventAt: eventAt.UTC(), ReceivedAt: now, PeriodIndex: index, PriceVersionID: version}, nil
}

// RecordUsageAdjustment reverses all or part of one accepted positive event.
// It has a new event identity and retains the original event time and period.
func (l *Lab) RecordUsageAdjustment(ctx context.Context, source, eventID, subID, originalSource, originalEventID string, reverseQuantity int64) (UsageEvent, error) {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return UsageEvent{}, err
	}
	defer tx.Rollback()
	event, err := l.recordUsageAdjustmentTx(ctx, tx, l.now().UTC(), source, eventID, subID, originalSource, originalEventID, reverseQuantity)
	if err != nil {
		return UsageEvent{}, err
	}
	return event, tx.Commit()
}

func (l *Lab) recordUsageAdjustmentTx(ctx context.Context, tx *sql.Tx, at time.Time, source, eventID, subID, originalSource, originalEventID string, reverseQuantity int64) (UsageEvent, error) {
	if source == "" || eventID == "" || subID == "" || originalSource == "" || originalEventID == "" || reverseQuantity <= 0 {
		return UsageEvent{}, ErrConflict
	}
	var tenant string
	if err := tx.QueryRowContext(ctx, `SELECT customer_id FROM subscriptions WHERE id=?`, subID).Scan(&tenant); err != nil {
		return UsageEvent{}, err
	}
	var originalSub, meter, version string
	var originalQuantity, eventAt int64
	var index int
	if err := tx.QueryRowContext(ctx, `SELECT subscription_id,meter_id,quantity,event_at,period_index,price_version_id FROM usage_events WHERE tenant_id=? AND source=? AND event_id=? AND quantity>0`, tenant, originalSource, originalEventID).Scan(&originalSub, &meter, &originalQuantity, &eventAt, &index, &version); err != nil {
		return UsageEvent{}, err
	}
	if originalSub != subID {
		return UsageEvent{}, ErrConflict
	}
	fingerprint := hash(subID, meter, -reverseQuantity, eventAt, originalSource, originalEventID)
	var savedHash string
	err := tx.QueryRowContext(ctx, `SELECT payload_hash FROM usage_events WHERE tenant_id=? AND source=? AND event_id=?`, tenant, source, eventID).Scan(&savedHash)
	if err == nil {
		if savedHash != fingerprint {
			return UsageEvent{}, ErrConflict
		}
		var received int64
		if err := tx.QueryRowContext(ctx, `SELECT received_at FROM usage_events WHERE tenant_id=? AND source=? AND event_id=?`, tenant, source, eventID).Scan(&received); err != nil {
			return UsageEvent{}, err
		}
		return UsageEvent{TenantID: tenant, Source: source, EventID: eventID, SubscriptionID: subID, MeterID: meter, Quantity: -reverseQuantity, EventAt: time.Unix(0, eventAt).UTC(), ReceivedAt: time.Unix(0, received).UTC(), PeriodIndex: index, PriceVersionID: version}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return UsageEvent{}, err
	}
	if !unixNanoTimeFits(at) {
		return UsageEvent{}, ErrConflict
	}
	var already int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(-quantity),0) FROM usage_events WHERE tenant_id=? AND correction_source=? AND correction_event_id=?`, tenant, originalSource, originalEventID).Scan(&already); err != nil {
		return UsageEvent{}, err
	}
	if reverseQuantity > originalQuantity-already {
		return UsageEvent{}, ErrConflict
	}
	now := at
	if _, err := tx.ExecContext(ctx, `INSERT INTO usage_events(tenant_id,source,event_id,payload_hash,subscription_id,meter_id,quantity,correction_source,correction_event_id,event_at,received_at,period_index,price_version_id) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, tenant, source, eventID, fingerprint, subID, meter, -reverseQuantity, originalSource, originalEventID, eventAt, now.UnixNano(), index, version); err != nil {
		return UsageEvent{}, err
	}
	return UsageEvent{TenantID: tenant, Source: source, EventID: eventID, SubscriptionID: subID, MeterID: meter, Quantity: -reverseQuantity, EventAt: time.Unix(0, eventAt).UTC(), ReceivedAt: now, PeriodIndex: index, PriceVersionID: version}, nil
}

func roundHalfEvenRatio(numerator, denominator int64) (int64, error) {
	if numerator < 0 || denominator <= 0 {
		return 0, ErrConflict
	}
	q, r := new(big.Int), new(big.Int)
	q.QuoRem(big.NewInt(numerator), big.NewInt(denominator), r)
	cmp := new(big.Int).Lsh(r, 1).Cmp(big.NewInt(denominator))
	if cmp > 0 || (cmp == 0 && q.Bit(0) == 1) {
		q.Add(q, big.NewInt(1))
	}
	if !q.IsInt64() {
		return 0, ErrConflict
	}
	return q.Int64(), nil
}

func rateUsage(ctx context.Context, tx *sql.Tx, subID string, index int, version string, receivedBefore int64) (UsageRating, error) {
	terms, err := loadPriceTerms(ctx, tx, version)
	if err != nil {
		return UsageRating{}, err
	}
	if terms.UsageRateNum <= 0 || terms.IncludedQuantity <= 0 || terms.MeterID == "" {
		return UsageRating{}, ErrConflict
	}
	var quantity int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(quantity),0) FROM usage_events WHERE subscription_id=? AND period_index=? AND meter_id=? AND price_version_id=? AND received_at<=?`, subID, index, terms.MeterID, version, receivedBefore).Scan(&quantity); err != nil {
		return UsageRating{}, err
	}
	var other int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_events WHERE subscription_id=? AND period_index=? AND (meter_id!=? OR price_version_id!=?) AND received_at<=?`, subID, index, terms.MeterID, version, receivedBefore).Scan(&other); err != nil {
		return UsageRating{}, err
	}
	if other != 0 {
		return UsageRating{}, ErrConflict
	}
	overage := quantity - terms.IncludedQuantity
	if overage < 0 {
		overage = 0
	}
	product := new(big.Int).Mul(big.NewInt(overage), big.NewInt(terms.UsageRateNum))
	if !product.IsInt64() {
		return UsageRating{}, ErrConflict
	}
	rounded, err := roundHalfEvenRatio(product.Int64(), terms.UsageRateDen)
	if err != nil {
		return UsageRating{}, err
	}
	return UsageRating{SubscriptionID: subID, PeriodIndex: index, PriceVersionID: version, Quantity: quantity, IncludedQuantity: terms.IncludedQuantity, OverageQuantity: overage, ExactMinorNumerator: product.Int64(), ExactMinorDenominator: terms.UsageRateDen, RoundedMinor: rounded}, nil
}

func (l *Lab) CloseUsagePeriod(ctx context.Context, subID string, index int, cutoff time.Time) (UsageRating, error) {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return UsageRating{}, err
	}
	defer tx.Rollback()
	rating, err := l.closeUsagePeriodTx(ctx, tx, l.now().UTC(), subID, index, cutoff)
	if err != nil {
		return UsageRating{}, err
	}
	return rating, tx.Commit()
}

func (l *Lab) closeUsagePeriodTx(ctx context.Context, tx *sql.Tx, at time.Time, subID string, index int, cutoff time.Time) (UsageRating, error) {
	if subID == "" || index < 0 || cutoff.IsZero() || cutoff.After(at) {
		return UsageRating{}, ErrConflict
	}
	var start, end int64
	if err := tx.QueryRowContext(ctx, `SELECT period_start,period_end FROM billing_periods WHERE subscription_id=? AND period_index=?`, subID, index).Scan(&start, &end); err != nil {
		return UsageRating{}, err
	}
	if cutoff.UTC().UnixNano() < end {
		return UsageRating{}, ErrConflict
	}
	var existing string
	err := tx.QueryRowContext(ctx, `SELECT id FROM usage_ratings WHERE subscription_id=? AND period_index=? AND revision=1`, subID, index).Scan(&existing)
	if err == nil {
		return loadUsageRating(ctx, tx, existing)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return UsageRating{}, err
	}
	var version string
	err = tx.QueryRowContext(ctx, `SELECT price_version_id FROM usage_events WHERE subscription_id=? AND period_index=? ORDER BY rowid LIMIT 1`, subID, index).Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		err = tx.QueryRowContext(ctx, `SELECT price_version_id FROM pricing_assignments WHERE subscription_id=? AND effective_start< ? AND (effective_end IS NULL OR effective_end>?) ORDER BY assignment_index DESC LIMIT 1`, subID, end, end-1).Scan(&version)
	}
	if err != nil {
		return UsageRating{}, err
	}
	rating, err := rateUsage(ctx, tx, subID, index, version, cutoff.UTC().UnixNano())
	if err != nil {
		return UsageRating{}, err
	}
	rating.ID, err = newID("rating_")
	if err != nil {
		return UsageRating{}, err
	}
	rating.Revision = 1
	rating.DeltaMinor = rating.RoundedMinor
	rating.RatedAt = at
	if _, err := tx.ExecContext(ctx, `INSERT INTO usage_periods(subscription_id,period_index,price_version_id,cutoff_at,closed_at,rated_minor) VALUES(?,?,?,?,?,?)`, subID, index, version, cutoff.UTC().UnixNano(), rating.RatedAt.UnixNano(), rating.RoundedMinor); err != nil {
		return UsageRating{}, err
	}
	if err := insertUsageRating(ctx, tx, rating); err != nil {
		return UsageRating{}, err
	}
	return rating, nil
}

func insertUsageRating(ctx context.Context, tx *sql.Tx, r UsageRating) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO usage_ratings(id,subscription_id,period_index,revision,price_version_id,quantity,included_quantity,overage_quantity,exact_minor_numerator,exact_minor_denominator,rounded_minor,delta_minor,rated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, r.ID, r.SubscriptionID, r.PeriodIndex, r.Revision, r.PriceVersionID, r.Quantity, r.IncludedQuantity, r.OverageQuantity, r.ExactMinorNumerator, r.ExactMinorDenominator, r.RoundedMinor, r.DeltaMinor, r.RatedAt.UnixNano())
	return err
}

func loadUsageRating(ctx context.Context, q rowQuerier, id string) (UsageRating, error) {
	var r UsageRating
	var rated int64
	err := q.QueryRowContext(ctx, `SELECT id,subscription_id,period_index,revision,price_version_id,quantity,included_quantity,overage_quantity,exact_minor_numerator,exact_minor_denominator,rounded_minor,delta_minor,rated_at FROM usage_ratings WHERE id=?`, id).Scan(&r.ID, &r.SubscriptionID, &r.PeriodIndex, &r.Revision, &r.PriceVersionID, &r.Quantity, &r.IncludedQuantity, &r.OverageQuantity, &r.ExactMinorNumerator, &r.ExactMinorDenominator, &r.RoundedMinor, &r.DeltaMinor, &rated)
	if err != nil {
		return UsageRating{}, err
	}
	r.RatedAt = time.Unix(0, rated).UTC()
	return r, nil
}

func (l *Lab) RerateUsagePeriod(ctx context.Context, subID string, index int) (UsageRating, error) {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return UsageRating{}, err
	}
	defer tx.Rollback()
	rating, err := l.rerateUsagePeriodTx(ctx, tx, l.now().UTC(), subID, index)
	if err != nil {
		return UsageRating{}, err
	}
	return rating, tx.Commit()
}

func (l *Lab) rerateUsagePeriodTx(ctx context.Context, tx *sql.Tx, at time.Time, subID string, index int) (UsageRating, error) {
	var version string
	if err := tx.QueryRowContext(ctx, `SELECT price_version_id FROM usage_periods WHERE subscription_id=? AND period_index=?`, subID, index).Scan(&version); err != nil {
		return UsageRating{}, err
	}
	var priorID string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM usage_ratings WHERE subscription_id=? AND period_index=? ORDER BY revision DESC LIMIT 1`, subID, index).Scan(&priorID); err != nil {
		return UsageRating{}, err
	}
	prior, err := loadUsageRating(ctx, tx, priorID)
	if err != nil {
		return UsageRating{}, err
	}
	current, err := rateUsage(ctx, tx, subID, index, version, at.UnixNano())
	if err != nil {
		return UsageRating{}, err
	}
	if current.Quantity == prior.Quantity {
		return prior, nil
	}
	current.Revision = prior.Revision + 1
	current.DeltaMinor = current.RoundedMinor - prior.RoundedMinor
	current.RatedAt = at
	current.ID, err = newID("rating_")
	if err != nil {
		return UsageRating{}, err
	}
	if err := insertUsageRating(ctx, tx, current); err != nil {
		return UsageRating{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE usage_periods SET rated_minor=? WHERE subscription_id=? AND period_index=?`, current.RoundedMinor, subID, index); err != nil {
		return UsageRating{}, err
	}
	return current, nil
}

type pendingUsageLine struct {
	PeriodIndex    int
	PriceVersionID string
	MeterID        string
	AmountMinor    int64
}

var ErrUsageCreditNoteRequired = errors.New("usage credit note required before renewal")

func pendingUsageLines(ctx context.Context, tx *sql.Tx, subID string, boundary int64) ([]pendingUsageLine, int64, error) {
	rows, err := tx.QueryContext(ctx, `SELECT u.period_index,u.price_version_id,u.rated_minor,u.billed_minor,u.credited_minor,u.applied_count FROM usage_periods u JOIN billing_periods p ON p.subscription_id=u.subscription_id AND p.period_index=u.period_index WHERE u.subscription_id=? AND p.period_end<=? ORDER BY u.period_index`, subID, boundary)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var lines []pendingUsageLine
	var total int64
	for rows.Next() {
		var line pendingUsageLine
		var rated, billed, credited int64
		var count int
		if err := rows.Scan(&line.PeriodIndex, &line.PriceVersionID, &rated, &billed, &credited, &count); err != nil {
			return nil, 0, err
		}
		terms, err := loadPriceTerms(ctx, tx, line.PriceVersionID)
		if err != nil || terms.MeterID == "" {
			return nil, 0, ErrConflict
		}
		line.MeterID = terms.MeterID
		line.AmountMinor = rated - (billed - credited)
		if line.AmountMinor < 0 {
			return nil, 0, fmt.Errorf("period %d: %w", line.PeriodIndex, ErrUsageCreditNoteRequired)
		}
		if line.AmountMinor > 0 || count == 0 {
			lines = append(lines, line)
			total += line.AmountMinor
		}
	}
	return lines, total, rows.Err()
}

func applyUsageLines(ctx context.Context, tx *sql.Tx, subID, invoiceID string, now time.Time, lines []pendingUsageLine) error {
	for _, line := range lines {
		if _, err := tx.ExecContext(ctx, `INSERT INTO invoice_lines(invoice_id,price_version_id,component_code,amount_minor) VALUES(?,?,?,?)`, invoiceID, line.PriceVersionID, fmt.Sprintf("usage:%s:period:%d", line.MeterID, line.PeriodIndex), line.AmountMinor); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO usage_invoice_applications(subscription_id,period_index,invoice_id,amount_minor,applied_at) VALUES(?,?,?,?,?)`, subID, line.PeriodIndex, invoiceID, line.AmountMinor, now.UnixNano()); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE usage_periods SET billed_minor=billed_minor+?,applied_count=applied_count+1 WHERE subscription_id=? AND period_index=?`, line.AmountMinor, subID, line.PeriodIndex); err != nil {
			return err
		}
	}
	return nil
}

// RunUsageCreditNotes reduces the invoice that actually carried a now
// over-rated usage line. The existing correction engine releases only funded
// allocation as credit; the original finalized invoice remains immutable.
func (l *Lab) RunUsageCreditNotes(ctx context.Context) ([]CorrectionResult, error) {
	rows, err := l.db.QueryContext(ctx, `SELECT subscription_id,period_index FROM usage_periods WHERE billed_minor-credited_minor>rated_minor ORDER BY subscription_id,period_index`)
	if err != nil {
		return nil, err
	}
	type period struct {
		sub   string
		index int
	}
	var periods []period
	for rows.Next() {
		var p period
		if err := rows.Scan(&p.sub, &p.index); err != nil {
			rows.Close()
			return nil, err
		}
		periods = append(periods, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	var results []CorrectionResult
	for _, p := range periods {
		for {
			tx, err := l.db.BeginTx(ctx, nil)
			if err != nil {
				return results, err
			}
			correction, applied, err := l.runUsageCreditNoteTx(ctx, tx, l.now().UTC(), p.sub, p.index)
			if err != nil {
				tx.Rollback()
				return results, err
			}
			if !applied {
				tx.Rollback()
				break
			}
			if err := tx.Commit(); err != nil {
				return results, err
			}
			results = append(results, correction)
		}
	}
	return results, nil
}

func (l *Lab) runUsageCreditNoteTx(ctx context.Context, tx *sql.Tx, at time.Time, subID string, index int) (CorrectionResult, bool, error) {
	var billed, credited, rated int64
	if err := tx.QueryRowContext(ctx, `SELECT billed_minor,credited_minor,rated_minor FROM usage_periods WHERE subscription_id=? AND period_index=?`, subID, index).Scan(&billed, &credited, &rated); err != nil {
		return CorrectionResult{}, false, err
	}
	needed := billed - credited - rated
	if needed <= 0 {
		return CorrectionResult{}, false, nil
	}
	var invoiceID string
	var available int64
	err := tx.QueryRowContext(ctx, `SELECT a.invoice_id,a.amount_minor-COALESCE((SELECT SUM(n.amount_minor) FROM usage_credit_notes n WHERE n.subscription_id=a.subscription_id AND n.period_index=a.period_index AND n.source_invoice_id=a.invoice_id),0) FROM usage_invoice_applications a WHERE a.subscription_id=? AND a.period_index=? AND a.amount_minor>COALESCE((SELECT SUM(n.amount_minor) FROM usage_credit_notes n WHERE n.subscription_id=a.subscription_id AND n.period_index=a.period_index AND n.source_invoice_id=a.invoice_id),0) ORDER BY a.applied_at DESC,a.invoice_id DESC LIMIT 1`, subID, index).Scan(&invoiceID, &available)
	if err != nil {
		return CorrectionResult{}, false, err
	}
	amount := needed
	if available < amount {
		amount = available
	}
	if amount <= 0 {
		return CorrectionResult{}, false, ErrConflict
	}
	key := fmt.Sprintf("usage-credit:%s:%d:%s:%d:%d:%d", subID, index, invoiceID, billed, credited, rated)
	correction, err := l.postReductionTx(ctx, tx, at, invoiceID, amount, fmt.Sprintf("usage rerating for period %d", index), key, false)
	if err != nil {
		return CorrectionResult{}, false, err
	}
	id, err := newID("usage-credit_")
	if err != nil {
		return CorrectionResult{}, false, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO usage_credit_notes(id,subscription_id,period_index,source_invoice_id,amount_minor,correction_id,request_key,created_at) VALUES(?,?,?,?,?,?,?,?)`, id, subID, index, invoiceID, amount, correction.ID, key, at.UnixNano()); err != nil {
		return CorrectionResult{}, false, err
	}
	updated, err := tx.ExecContext(ctx, `UPDATE usage_periods SET credited_minor=credited_minor+? WHERE subscription_id=? AND period_index=? AND billed_minor-credited_minor-rated_minor>=?`, amount, subID, index, amount)
	if err != nil {
		return CorrectionResult{}, false, err
	}
	if count, err := updated.RowsAffected(); err != nil || count != 1 {
		return CorrectionResult{}, false, ErrConflict
	}
	return correction, true, nil
}
