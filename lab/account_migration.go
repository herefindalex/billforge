package lab

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"
)

var ErrAccountMigrationStopped = fmt.Errorf("%w: account migration is stopped", ErrConflict)

type AccountLink struct {
	LegacyAccountID string
	CustomerID      string
	BeneficiaryID   string
	Cohort          string
	HasHistory      bool
	ReadOwner       string
	WriterOwner     string
	Stopped         bool
	StopReason      string
}

type ShadowComparison struct {
	ID              string
	LegacyAccountID string
	Kind            string
	ObjectID        string
	Expected        string
	Actual          string
	Matched         bool
	LatencyMillis   int64
	ObservedAt      time.Time
}

type LegacyProvenance struct {
	LegacyInvoiceID        string
	LegacySubscriptionID   string
	LegacyAccountID        string
	CommerceSubscriptionID string
	CommerceInvoiceID      string
	PriceVersionID         string
	Status                 string
	Evidence               string
}

type MigrationThresholds struct {
	MaxQuoteP95Millis    int64
	MaxUnknownPayments   int64
	MaxOpenDiscrepancies int64
}

type MigrationReadiness struct {
	LegacyAccountID    string
	Reconciled         bool
	QuoteMatches       bool
	EntitlementMatches bool
	ProvenanceComplete bool
	QuoteP95Millis     int64
	UnknownPayments    int64
	OpenDiscrepancies  int64
	Ready              bool
}

type AccountEntitlementRead struct {
	Owner          string
	Status         string
	SourceRevision int64
	AsOf           time.Time
}

func migrateAccountMigration(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS account_links (
		legacy_account_id TEXT PRIMARY KEY,
		customer_id TEXT NOT NULL UNIQUE,
		beneficiary_id TEXT NOT NULL,
		cohort TEXT NOT NULL,
		has_history INTEGER NOT NULL CHECK(has_history IN (0,1)),
		read_owner TEXT NOT NULL CHECK(read_owner IN ('legacy','commerce')),
		writer_owner TEXT NOT NULL CHECK(writer_owner IN ('legacy','commerce')),
		stopped INTEGER NOT NULL DEFAULT 0 CHECK(stopped IN (0,1)),
		stop_reason TEXT NOT NULL DEFAULT '',
		created_at INTEGER NOT NULL);
	CREATE TABLE IF NOT EXISTS migration_shadows (
		id TEXT PRIMARY KEY,
		legacy_account_id TEXT NOT NULL REFERENCES account_links(legacy_account_id),
		kind TEXT NOT NULL CHECK(kind IN ('quote','entitlement')),
		object_id TEXT NOT NULL DEFAULT '',
		expected TEXT NOT NULL,
		actual TEXT NOT NULL,
		matched INTEGER NOT NULL CHECK(matched IN (0,1)),
		latency_millis INTEGER NOT NULL CHECK(latency_millis>=0),
		observed_at INTEGER NOT NULL);
	CREATE TABLE IF NOT EXISTS legacy_provenance (
		legacy_invoice_id TEXT PRIMARY KEY,
		legacy_subscription_id TEXT NOT NULL,
		legacy_account_id TEXT NOT NULL REFERENCES account_links(legacy_account_id),
		commerce_subscription_id TEXT NOT NULL,
		commerce_invoice_id TEXT NOT NULL,
		price_version_id TEXT NOT NULL,
		status TEXT NOT NULL CHECK(status IN ('complete','manual_review')),
		evidence TEXT NOT NULL);
	CREATE UNIQUE INDEX IF NOT EXISTS legacy_provenance_commerce_invoice_unique ON legacy_provenance(legacy_account_id,commerce_invoice_id);
	CREATE TABLE IF NOT EXISTS account_migration_events (
		id TEXT PRIMARY KEY, legacy_account_id TEXT NOT NULL REFERENCES account_links(legacy_account_id),
		kind TEXT NOT NULL, detail TEXT NOT NULL, at INTEGER NOT NULL);`)
	if err != nil {
		return err
	}
	return ensureCatalogColumn(db, "migration_shadows", "object_id", `ALTER TABLE migration_shadows ADD COLUMN object_id TEXT NOT NULL DEFAULT ''`)
}

func (l *Lab) linkLegacyAccountTx(ctx context.Context, tx *sql.Tx, link AccountLink) (AccountLink, error) {
	if link.LegacyAccountID == "" || link.CustomerID == "" || link.BeneficiaryID == "" || link.Cohort == "" {
		return AccountLink{}, ErrConflict
	}
	history := 0
	if link.HasHistory {
		history = 1
	}
	if existing, err := loadAccountLink(ctx, tx, link.LegacyAccountID); err == nil {
		if existing.CustomerID != link.CustomerID || existing.BeneficiaryID != link.BeneficiaryID || existing.Cohort != link.Cohort || existing.HasHistory != link.HasHistory {
			return AccountLink{}, ErrConflict
		}
		return existing, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return AccountLink{}, err
	}
	if !link.HasHistory {
		var invoices int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM invoices i JOIN subscriptions s ON s.id=i.subscription_id WHERE s.customer_id=?`, link.CustomerID).Scan(&invoices); err != nil {
			return AccountLink{}, err
		}
		if invoices > 0 {
			return AccountLink{}, ErrConflict
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO account_links(legacy_account_id,customer_id,beneficiary_id,cohort,has_history,read_owner,writer_owner,created_at) VALUES(?,?,?,?,?,'legacy','legacy',?) ON CONFLICT(legacy_account_id) DO NOTHING`, link.LegacyAccountID, link.CustomerID, link.BeneficiaryID, link.Cohort, history, l.now().UTC().UnixNano()); err != nil {
		return AccountLink{}, err
	}
	saved, err := loadAccountLink(ctx, tx, link.LegacyAccountID)
	if err != nil {
		return AccountLink{}, err
	}
	if saved.CustomerID != link.CustomerID || saved.BeneficiaryID != link.BeneficiaryID || saved.Cohort != link.Cohort || saved.HasHistory != link.HasHistory {
		return AccountLink{}, ErrConflict
	}
	return saved, nil
}

func (l *Lab) LinkLegacyAccount(ctx context.Context, link AccountLink) (AccountLink, error) {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return AccountLink{}, err
	}
	defer tx.Rollback()
	saved, err := l.linkLegacyAccountTx(ctx, tx, link)
	if err != nil {
		return AccountLink{}, err
	}
	if err := tx.Commit(); err != nil {
		return AccountLink{}, err
	}
	return saved, nil
}

func (l *Lab) AccountLink(ctx context.Context, legacyID string) (AccountLink, error) {
	return loadAccountLink(ctx, l.db, legacyID)
}

func (l *Lab) AccountLinks(ctx context.Context) ([]AccountLink, error) {
	rows, err := l.db.QueryContext(ctx, `SELECT legacy_account_id FROM account_links ORDER BY legacy_account_id`)
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
	out := make([]AccountLink, 0, len(ids))
	for _, id := range ids {
		a, err := l.AccountLink(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

func loadAccountLink(ctx context.Context, q priceQuerier, legacyID string) (AccountLink, error) {
	var a AccountLink
	var history, stopped int
	err := q.QueryRowContext(ctx, `SELECT legacy_account_id,customer_id,beneficiary_id,cohort,has_history,read_owner,writer_owner,stopped,stop_reason FROM account_links WHERE legacy_account_id=?`, legacyID).Scan(&a.LegacyAccountID, &a.CustomerID, &a.BeneficiaryID, &a.Cohort, &history, &a.ReadOwner, &a.WriterOwner, &stopped, &a.StopReason)
	a.HasHistory = history != 0
	a.Stopped = stopped != 0
	return a, err
}

func (l *Lab) recordShadowTx(ctx context.Context, tx *sql.Tx, accountID, kind, objectID, expected, actual string, elapsed time.Duration) (ShadowComparison, error) {
	id, err := newID("shadow_")
	if err != nil {
		return ShadowComparison{}, err
	}
	x := ShadowComparison{ID: id, LegacyAccountID: accountID, Kind: kind, ObjectID: objectID, Expected: expected, Actual: actual, Matched: expected == actual, LatencyMillis: elapsed.Milliseconds(), ObservedAt: l.now().UTC()}
	matched := 0
	if x.Matched {
		matched = 1
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO migration_shadows(id,legacy_account_id,kind,object_id,expected,actual,matched,latency_millis,observed_at) VALUES(?,?,?,?,?,?,?,?,?)`, x.ID, x.LegacyAccountID, x.Kind, x.ObjectID, x.Expected, x.Actual, matched, x.LatencyMillis, x.ObservedAt.UnixNano())
	return x, err
}

func (l *Lab) shadowQuoteTx(ctx context.Context, tx *sql.Tx, accountID, planID string, seats, legacyAmountMinor int64, legacyCurrency string) (ShadowComparison, error) {
	if planID == "" || seats < 0 || legacyAmountMinor < 0 || legacyCurrency == "" {
		return ShadowComparison{}, ErrConflict
	}
	a, err := loadAccountLink(ctx, tx, accountID)
	if err != nil {
		return ShadowComparison{}, err
	}
	start := time.Now()
	actual := "PRICE_UNAVAILABLE"
	var version string
	now := l.now().UTC().UnixNano()
	err = tx.QueryRowContext(ctx, `SELECT p.id FROM catalog_selection c JOIN price_versions p ON p.id=c.price_version_id WHERE c.plan_id=? AND c.cohort=? AND c.effective_at<=? AND p.publication_state='published' AND p.effective_from<=? AND (p.effective_to IS NULL OR p.effective_to>?) ORDER BY c.effective_at DESC LIMIT 1`, planID, a.Cohort, now, now, now).Scan(&version)
	if err == nil {
		terms, e := loadPriceTerms(ctx, tx, version)
		if e != nil {
			return ShadowComparison{}, e
		}
		amount, e := terms.UpfrontMinor(seats)
		if e != nil {
			return ShadowComparison{}, e
		}
		actual = fmt.Sprintf("%d %s", amount, terms.Currency)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return ShadowComparison{}, err
	}
	expected := fmt.Sprintf("%d %s", legacyAmountMinor, legacyCurrency)
	return l.recordShadowTx(ctx, tx, accountID, "quote", fmt.Sprintf("%s:%d", planID, seats), expected, actual, time.Since(start))
}

func (l *Lab) ShadowQuote(ctx context.Context, accountID, planID string, seats, legacyAmountMinor int64, legacyCurrency string) (ShadowComparison, error) {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return ShadowComparison{}, err
	}
	defer tx.Rollback()
	result, err := l.shadowQuoteTx(ctx, tx, accountID, planID, seats, legacyAmountMinor, legacyCurrency)
	if err != nil {
		return ShadowComparison{}, err
	}
	return result, tx.Commit()
}

func (l *Lab) shadowEntitlementTx(ctx context.Context, tx *sql.Tx, accountID, subID, legacyStatus string) (ShadowComparison, error) {
	if legacyStatus == "" {
		return ShadowComparison{}, ErrConflict
	}
	a, err := loadAccountLink(ctx, tx, accountID)
	if err != nil {
		return ShadowComparison{}, err
	}
	start := time.Now()
	actual := "missing"
	var customer, sourceStatus, projection string
	var revision, projectionRevision int64
	err = tx.QueryRowContext(ctx, `SELECT s.customer_id,s.status,s.revision,COALESCE(e.status,''),COALESCE(e.source_revision,0) FROM subscriptions s LEFT JOIN entitlements e ON e.subscription_id=s.id WHERE s.id=?`, subID).Scan(&customer, &sourceStatus, &revision, &projection, &projectionRevision)
	if err == nil {
		if customer != a.CustomerID {
			return ShadowComparison{}, ErrConflict
		}
		actual = "pending"
		if sourceStatus == "active" && projectionRevision == revision && projection != "" {
			actual = projection
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return ShadowComparison{}, err
	}
	return l.recordShadowTx(ctx, tx, accountID, "entitlement", subID, legacyStatus, actual, time.Since(start))
}

func (l *Lab) ShadowEntitlement(ctx context.Context, accountID, subID, legacyStatus string) (ShadowComparison, error) {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return ShadowComparison{}, err
	}
	defer tx.Rollback()
	result, err := l.shadowEntitlementTx(ctx, tx, accountID, subID, legacyStatus)
	if err != nil {
		return ShadowComparison{}, err
	}
	return result, tx.Commit()
}

func (l *Lab) ReadAccountEntitlement(ctx context.Context, accountID, subID string) (AccountEntitlementRead, error) {
	a, err := l.AccountLink(ctx, accountID)
	if err != nil {
		return AccountEntitlementRead{}, err
	}
	if a.ReadOwner == "legacy" {
		var status string
		if err := l.db.QueryRowContext(ctx, `SELECT expected FROM migration_shadows WHERE legacy_account_id=? AND kind='entitlement' AND object_id=? ORDER BY observed_at DESC,rowid DESC LIMIT 1`, accountID, subID).Scan(&status); err != nil {
			return AccountEntitlementRead{}, err
		}
		return AccountEntitlementRead{Owner: "legacy", Status: status, AsOf: l.now().UTC()}, nil
	}
	var customer, sourceStatus, projection string
	var revision, projectionRevision int64
	if err := l.db.QueryRowContext(ctx, `SELECT s.customer_id,s.status,s.revision,COALESCE(e.status,''),COALESCE(e.source_revision,0) FROM subscriptions s LEFT JOIN entitlements e ON e.subscription_id=s.id WHERE s.id=?`, subID).Scan(&customer, &sourceStatus, &revision, &projection, &projectionRevision); err != nil {
		return AccountEntitlementRead{}, err
	}
	if customer != a.CustomerID {
		return AccountEntitlementRead{}, ErrConflict
	}
	status := "pending"
	if sourceStatus == "active" && projectionRevision == revision && projection != "" {
		status = projection
	}
	return AccountEntitlementRead{Owner: "commerce", Status: status, SourceRevision: projectionRevision, AsOf: l.now().UTC()}, nil
}

func (l *Lab) backfillLegacyProvenanceTx(ctx context.Context, tx *sql.Tx, record LegacyProvenance) (LegacyProvenance, error) {
	if record.LegacyInvoiceID == "" || record.LegacySubscriptionID == "" || record.LegacyAccountID == "" || record.CommerceSubscriptionID == "" || record.CommerceInvoiceID == "" || record.PriceVersionID == "" {
		return LegacyProvenance{}, ErrConflict
	}
	a, err := loadAccountLink(ctx, tx, record.LegacyAccountID)
	if err != nil {
		return LegacyProvenance{}, err
	}
	var customer, invoiceSub string
	err = tx.QueryRowContext(ctx, `SELECT customer_id FROM subscriptions WHERE id=?`, record.CommerceSubscriptionID).Scan(&customer)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return LegacyProvenance{}, err
	}
	invoiceErr := tx.QueryRowContext(ctx, `SELECT subscription_id FROM invoices WHERE id=?`, record.CommerceInvoiceID).Scan(&invoiceSub)
	if invoiceErr != nil && !errors.Is(invoiceErr, sql.ErrNoRows) {
		return LegacyProvenance{}, invoiceErr
	}
	record.Status = "manual_review"
	record.Evidence = "source mapping or immutable invoice provenance missing"
	if err == nil && invoiceErr == nil && customer == a.CustomerID && invoiceSub == record.CommerceSubscriptionID {
		var lines int
		var assignmentPrice string
		if e := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM invoice_lines WHERE invoice_id=? AND price_version_id=?`, record.CommerceInvoiceID, record.PriceVersionID).Scan(&lines); e != nil {
			return LegacyProvenance{}, e
		}
		assignmentErr := tx.QueryRowContext(ctx, `SELECT a.price_version_id FROM pricing_assignments a JOIN billing_periods p ON p.subscription_id=a.subscription_id WHERE p.invoice_id=? AND a.effective_start<=p.period_start AND (a.effective_end IS NULL OR a.effective_end>p.period_start) ORDER BY a.assignment_index DESC LIMIT 1`, record.CommerceInvoiceID).Scan(&assignmentPrice)
		if assignmentErr != nil && !errors.Is(assignmentErr, sql.ErrNoRows) {
			return LegacyProvenance{}, assignmentErr
		}
		if lines > 0 && assignmentErr == nil && assignmentPrice == record.PriceVersionID {
			record.Status = "complete"
			record.Evidence = "subscription, invoice and immutable price line agree"
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO legacy_provenance(legacy_invoice_id,legacy_subscription_id,legacy_account_id,commerce_subscription_id,commerce_invoice_id,price_version_id,status,evidence) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(legacy_invoice_id) DO NOTHING`, record.LegacyInvoiceID, record.LegacySubscriptionID, record.LegacyAccountID, record.CommerceSubscriptionID, record.CommerceInvoiceID, record.PriceVersionID, record.Status, record.Evidence); err != nil {
		return LegacyProvenance{}, err
	}
	var saved LegacyProvenance
	if err := tx.QueryRowContext(ctx, `SELECT legacy_invoice_id,legacy_subscription_id,legacy_account_id,commerce_subscription_id,commerce_invoice_id,price_version_id,status,evidence FROM legacy_provenance WHERE legacy_invoice_id=?`, record.LegacyInvoiceID).Scan(&saved.LegacyInvoiceID, &saved.LegacySubscriptionID, &saved.LegacyAccountID, &saved.CommerceSubscriptionID, &saved.CommerceInvoiceID, &saved.PriceVersionID, &saved.Status, &saved.Evidence); err != nil {
		return LegacyProvenance{}, err
	}
	if saved != record {
		return LegacyProvenance{}, ErrConflict
	}
	return saved, nil
}

func (l *Lab) BackfillLegacyProvenance(ctx context.Context, record LegacyProvenance) (LegacyProvenance, error) {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return LegacyProvenance{}, err
	}
	defer tx.Rollback()
	saved, err := l.backfillLegacyProvenanceTx(ctx, tx, record)
	if err != nil {
		return LegacyProvenance{}, err
	}
	return saved, tx.Commit()
}

// ResolveLegacyProvenance records a reviewed correction to the ID mapping.
// It never edits the source invoice, assignment or price version.
func (l *Lab) resolveLegacyProvenanceTx(ctx context.Context, tx *sql.Tx, corrected LegacyProvenance, reviewer, decision string) (LegacyProvenance, error) {
	if reviewer == "" || decision == "" || corrected.LegacyInvoiceID == "" || corrected.CommerceSubscriptionID == "" || corrected.CommerceInvoiceID == "" || corrected.PriceVersionID == "" {
		return LegacyProvenance{}, ErrConflict
	}
	var old LegacyProvenance
	if err := tx.QueryRowContext(ctx, `SELECT legacy_invoice_id,legacy_subscription_id,legacy_account_id,commerce_subscription_id,commerce_invoice_id,price_version_id,status,evidence FROM legacy_provenance WHERE legacy_invoice_id=?`, corrected.LegacyInvoiceID).Scan(&old.LegacyInvoiceID, &old.LegacySubscriptionID, &old.LegacyAccountID, &old.CommerceSubscriptionID, &old.CommerceInvoiceID, &old.PriceVersionID, &old.Status, &old.Evidence); err != nil {
		return LegacyProvenance{}, err
	}
	if old.Status != "manual_review" || corrected.LegacySubscriptionID != old.LegacySubscriptionID || corrected.LegacyAccountID != old.LegacyAccountID {
		return LegacyProvenance{}, ErrConflict
	}
	a, err := loadAccountLink(ctx, tx, old.LegacyAccountID)
	if err != nil {
		return LegacyProvenance{}, err
	}
	var customer, invoiceSub, assignmentPrice string
	if err := tx.QueryRowContext(ctx, `SELECT customer_id FROM subscriptions WHERE id=?`, corrected.CommerceSubscriptionID).Scan(&customer); err != nil {
		return LegacyProvenance{}, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT subscription_id FROM invoices WHERE id=?`, corrected.CommerceInvoiceID).Scan(&invoiceSub); err != nil {
		return LegacyProvenance{}, err
	}
	if customer != a.CustomerID || invoiceSub != corrected.CommerceSubscriptionID {
		return LegacyProvenance{}, ErrConflict
	}
	var lines int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM invoice_lines WHERE invoice_id=? AND price_version_id=?`, corrected.CommerceInvoiceID, corrected.PriceVersionID).Scan(&lines); err != nil {
		return LegacyProvenance{}, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT a.price_version_id FROM pricing_assignments a JOIN billing_periods p ON p.subscription_id=a.subscription_id WHERE p.invoice_id=? AND a.effective_start<=p.period_start AND (a.effective_end IS NULL OR a.effective_end>p.period_start) ORDER BY a.assignment_index DESC LIMIT 1`, corrected.CommerceInvoiceID).Scan(&assignmentPrice); err != nil {
		return LegacyProvenance{}, err
	}
	if lines == 0 || assignmentPrice != corrected.PriceVersionID {
		return LegacyProvenance{}, ErrConflict
	}
	corrected.Status = "complete"
	corrected.Evidence = "reviewed by " + reviewer + ": " + decision + "; immutable invoice and assignment agree"
	if _, err := tx.ExecContext(ctx, `UPDATE legacy_provenance SET commerce_subscription_id=?,commerce_invoice_id=?,price_version_id=?,status='complete',evidence=? WHERE legacy_invoice_id=? AND status='manual_review'`, corrected.CommerceSubscriptionID, corrected.CommerceInvoiceID, corrected.PriceVersionID, corrected.Evidence, corrected.LegacyInvoiceID); err != nil {
		return LegacyProvenance{}, err
	}
	id, err := newID("cutover_")
	if err != nil {
		return LegacyProvenance{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO account_migration_events(id,legacy_account_id,kind,detail,at) VALUES(?,?,'provenance_resolved',?,?)`, id, old.LegacyAccountID, corrected.LegacyInvoiceID+" / "+reviewer+" / "+decision, l.now().UTC().UnixNano()); err != nil {
		return LegacyProvenance{}, err
	}
	return corrected, nil
}

func (l *Lab) ResolveLegacyProvenance(ctx context.Context, corrected LegacyProvenance, reviewer, decision string) (LegacyProvenance, error) {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return LegacyProvenance{}, err
	}
	defer tx.Rollback()
	saved, err := l.resolveLegacyProvenanceTx(ctx, tx, corrected, reviewer, decision)
	if err != nil {
		return LegacyProvenance{}, err
	}
	return saved, tx.Commit()
}

func (l *Lab) MigrationReadiness(ctx context.Context, accountID string, limits MigrationThresholds) (MigrationReadiness, error) {
	return l.migrationReadiness(ctx, l.db, accountID, limits)
}

func (l *Lab) migrationReadiness(ctx context.Context, q priceQuerier, accountID string, limits MigrationThresholds) (MigrationReadiness, error) {
	if limits.MaxQuoteP95Millis <= 0 || limits.MaxUnknownPayments < 0 || limits.MaxOpenDiscrepancies < 0 {
		return MigrationReadiness{}, ErrConflict
	}
	a, err := loadAccountLink(ctx, q, accountID)
	if err != nil {
		return MigrationReadiness{}, err
	}
	r := MigrationReadiness{LegacyAccountID: accountID, ProvenanceComplete: !a.HasHistory}
	for _, kind := range []string{"quote", "entitlement"} {
		var matched int
		err := q.QueryRowContext(ctx, `SELECT matched FROM migration_shadows WHERE legacy_account_id=? AND kind=? ORDER BY observed_at DESC,rowid DESC LIMIT 1`, accountID, kind).Scan(&matched)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return MigrationReadiness{}, err
		}
		if kind == "quote" {
			r.QuoteMatches = err == nil && matched == 1
		} else {
			r.EntitlementMatches = err == nil && matched == 1
		}
	}
	if a.HasHistory {
		var total, needsReview int
		if err := q.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(CASE WHEN status!='complete' THEN 1 ELSE 0 END),0) FROM legacy_provenance WHERE legacy_account_id=?`, accountID).Scan(&total, &needsReview); err != nil {
			return MigrationReadiness{}, err
		}
		r.ProvenanceComplete = total > 0 && needsReview == 0
		var unbackfilled int
		if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM invoices i JOIN subscriptions s ON s.id=i.subscription_id WHERE s.customer_id=? AND NOT EXISTS (SELECT 1 FROM legacy_provenance p WHERE p.legacy_account_id=? AND p.commerce_invoice_id=i.id AND p.status='complete')`, a.CustomerID, accountID).Scan(&unbackfilled); err != nil {
			return MigrationReadiness{}, err
		}
		r.ProvenanceComplete = r.ProvenanceComplete && unbackfilled == 0
	}
	rows, err := q.QueryContext(ctx, `SELECT latency_millis FROM migration_shadows WHERE legacy_account_id=? AND kind='quote' ORDER BY latency_millis`, accountID)
	if err != nil {
		return MigrationReadiness{}, err
	}
	var latencies []int64
	for rows.Next() {
		var n int64
		if e := rows.Scan(&n); e != nil {
			rows.Close()
			return MigrationReadiness{}, e
		}
		latencies = append(latencies, n)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return MigrationReadiness{}, err
	}
	if len(latencies) > 0 {
		sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
		r.QuoteP95Millis = latencies[(95*len(latencies)+99)/100-1]
	}
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM payment_operations o JOIN invoices i ON i.id=o.invoice_id JOIN subscriptions s ON s.id=i.subscription_id WHERE s.customer_id=? AND o.status IN ('created','submitted','unknown')`, a.CustomerID).Scan(&r.UnknownPayments); err != nil {
		return MigrationReadiness{}, err
	}
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM discrepancies d WHERE d.status!='resolved' AND (d.object_id IN (SELECT id FROM subscriptions WHERE customer_id=?) OR d.object_id IN (SELECT id FROM invoices WHERE subscription_id IN (SELECT id FROM subscriptions WHERE customer_id=?)) OR d.object_id IN (SELECT id FROM payment_operations WHERE invoice_id IN (SELECT id FROM invoices WHERE subscription_id IN (SELECT id FROM subscriptions WHERE customer_id=?))))`, a.CustomerID, a.CustomerID, a.CustomerID).Scan(&r.OpenDiscrepancies); err != nil {
		return MigrationReadiness{}, err
	}
	var shadowAt, reconAt int64
	if err := q.QueryRowContext(ctx, `SELECT COALESCE(MAX(observed_at),0) FROM migration_shadows WHERE legacy_account_id=?`, accountID).Scan(&shadowAt); err != nil {
		return MigrationReadiness{}, err
	}
	if err := q.QueryRowContext(ctx, `SELECT COALESCE(MAX(created_at),0) FROM reconciliation_runs`).Scan(&reconAt); err != nil {
		return MigrationReadiness{}, err
	}
	r.Reconciled = reconAt > 0 && reconAt >= shadowAt
	r.Ready = !a.Stopped && r.Reconciled && r.QuoteMatches && r.EntitlementMatches && r.ProvenanceComplete && r.QuoteP95Millis <= limits.MaxQuoteP95Millis && r.UnknownPayments == 0 && r.UnknownPayments <= limits.MaxUnknownPayments && r.OpenDiscrepancies <= limits.MaxOpenDiscrepancies
	return r, nil
}

func (l *Lab) switchAccountReadTx(ctx context.Context, tx *sql.Tx, accountID string, limits MigrationThresholds) (AccountLink, error) {
	ready, err := l.migrationReadiness(ctx, tx, accountID, limits)
	if err != nil {
		return AccountLink{}, err
	}
	if !ready.Ready {
		return AccountLink{}, ErrConflict
	}
	if _, err := tx.ExecContext(ctx, `UPDATE account_links SET read_owner='commerce' WHERE legacy_account_id=? AND stopped=0`, accountID); err != nil {
		return AccountLink{}, err
	}
	return loadAccountLink(ctx, tx, accountID)
}

func (l *Lab) SwitchAccountRead(ctx context.Context, accountID string, limits MigrationThresholds) (AccountLink, error) {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return AccountLink{}, err
	}
	defer tx.Rollback()
	result, err := l.switchAccountReadTx(ctx, tx, accountID, limits)
	if err != nil {
		return AccountLink{}, err
	}
	return result, tx.Commit()
}

func (l *Lab) switchAccountWriterTx(ctx context.Context, tx *sql.Tx, accountID string, limits MigrationThresholds) (AccountLink, error) {
	ready, err := l.migrationReadiness(ctx, tx, accountID, limits)
	if err != nil {
		return AccountLink{}, err
	}
	if !ready.Ready {
		return AccountLink{}, ErrConflict
	}
	var readOwner, writerOwner string
	var stopped int
	if err := tx.QueryRowContext(ctx, `SELECT read_owner,writer_owner,stopped FROM account_links WHERE legacy_account_id=?`, accountID).Scan(&readOwner, &writerOwner, &stopped); err != nil {
		return AccountLink{}, err
	}
	if readOwner != "commerce" || stopped != 0 {
		return AccountLink{}, ErrConflict
	}
	if writerOwner == "legacy" {
		if _, err := tx.ExecContext(ctx, `UPDATE account_links SET writer_owner='commerce' WHERE legacy_account_id=? AND writer_owner='legacy' AND stopped=0`, accountID); err != nil {
			return AccountLink{}, err
		}
		id, err := newID("cutover_")
		if err != nil {
			return AccountLink{}, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO account_migration_events(id,legacy_account_id,kind,detail,at) VALUES(?,?,'writer_cutover',?,?)`, id, accountID, "legacy -> commerce", l.now().UTC().UnixNano()); err != nil {
			return AccountLink{}, err
		}
	}
	return loadAccountLink(ctx, tx, accountID)
}

func (l *Lab) SwitchAccountWriter(ctx context.Context, accountID string, limits MigrationThresholds) (AccountLink, error) {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return AccountLink{}, err
	}
	defer tx.Rollback()
	result, err := l.switchAccountWriterTx(ctx, tx, accountID, limits)
	if err != nil {
		return AccountLink{}, err
	}
	return result, tx.Commit()
}

func (l *Lab) stopAccountMigrationTx(ctx context.Context, tx *sql.Tx, accountID, reason string) (AccountLink, error) {
	if reason == "" {
		return AccountLink{}, ErrConflict
	}
	result, err := tx.ExecContext(ctx, `UPDATE account_links SET stopped=1,stop_reason=? WHERE legacy_account_id=?`, reason, accountID)
	if err != nil {
		return AccountLink{}, err
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		return AccountLink{}, sql.ErrNoRows
	}
	id, err := newID("cutover_")
	if err != nil {
		return AccountLink{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO account_migration_events(id,legacy_account_id,kind,detail,at) VALUES(?,?,'stopped',?,?)`, id, accountID, reason, l.now().UTC().UnixNano()); err != nil {
		return AccountLink{}, err
	}
	return loadAccountLink(ctx, tx, accountID)
}

func (l *Lab) StopAccountMigration(ctx context.Context, accountID, reason string) (AccountLink, error) {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return AccountLink{}, err
	}
	defer tx.Rollback()
	result, err := l.stopAccountMigrationTx(ctx, tx, accountID, reason)
	if err != nil {
		return AccountLink{}, err
	}
	return result, tx.Commit()
}

func ensureCommerceWriter(ctx context.Context, q rowQuerier, customerID string) error {
	var owner string
	var stopped int
	err := q.QueryRowContext(ctx, `SELECT writer_owner,stopped FROM account_links WHERE customer_id=?`, customerID).Scan(&owner, &stopped)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if stopped != 0 {
		return ErrAccountMigrationStopped
	}
	if owner != "commerce" {
		return ErrConflict
	}
	return nil
}

func ensureCommerceSubscriber(ctx context.Context, q rowQuerier, subID string) error {
	var customer string
	if err := q.QueryRowContext(ctx, `SELECT customer_id FROM subscriptions WHERE id=?`, subID).Scan(&customer); err != nil {
		return err
	}
	return ensureCommerceWriter(ctx, q, customer)
}
