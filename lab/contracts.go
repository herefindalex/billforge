package lab

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"time"
)

type ContractSpec struct {
	ID                         string
	CustomerID                 string
	Version                    int64
	BasePriceVersionID         string
	FixedMinor                 int64
	SeatMinor                  int64
	EffectiveFrom              time.Time
	EffectiveTo                time.Time
	PostContractPriceVersionID string
}

type ContractState struct {
	ContractSpec
	Checksum    string
	PublishedAt time.Time
}

func migrateContracts(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS contract_versions (
id TEXT PRIMARY KEY,
customer_id TEXT NOT NULL,
version INTEGER NOT NULL CHECK(version>0),
base_price_version_id TEXT NOT NULL REFERENCES price_versions(id),
fixed_minor INTEGER NOT NULL CHECK(fixed_minor>0),
seat_minor INTEGER NOT NULL CHECK(seat_minor>0),
payment_days INTEGER NOT NULL CHECK(payment_days=30),
effective_from INTEGER NOT NULL,
effective_to INTEGER NOT NULL,
post_price_version_id TEXT REFERENCES price_versions(id),
checksum TEXT NOT NULL,
published_at INTEGER NOT NULL,
UNIQUE(customer_id,version),
CHECK(effective_to>effective_from));
CREATE TRIGGER IF NOT EXISTS contract_immutable_update BEFORE UPDATE ON contract_versions BEGIN SELECT RAISE(ABORT,'published contract immutable'); END;
CREATE TRIGGER IF NOT EXISTS contract_immutable_delete BEFORE DELETE ON contract_versions BEGIN SELECT RAISE(ABORT,'published contract immutable'); END;
CREATE TABLE IF NOT EXISTS contract_quotes (
quote_id TEXT PRIMARY KEY REFERENCES quotes(id),
contract_version_id TEXT NOT NULL REFERENCES contract_versions(id));
CREATE INDEX IF NOT EXISTS contract_quotes_version_quote_idx ON contract_quotes(contract_version_id,quote_id);
CREATE TABLE IF NOT EXISTS contract_subscriptions (
subscription_id TEXT PRIMARY KEY REFERENCES subscriptions(id),
contract_version_id TEXT NOT NULL REFERENCES contract_versions(id));
CREATE INDEX IF NOT EXISTS contract_subscriptions_version_subscription_idx ON contract_subscriptions(contract_version_id,subscription_id);
CREATE TABLE IF NOT EXISTS contract_transitions (
subscription_id TEXT PRIMARY KEY REFERENCES subscriptions(id),
contract_version_id TEXT NOT NULL REFERENCES contract_versions(id),
post_price_version_id TEXT NOT NULL REFERENCES price_versions(id),
effective_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS invoice_contracts (
invoice_id TEXT PRIMARY KEY REFERENCES invoices(id),
contract_version_id TEXT NOT NULL REFERENCES contract_versions(id));`)
	if err != nil {
		return err
	}
	return ensureCatalogColumn(db, "pricing_assignments", "source_contract_id", `ALTER TABLE pricing_assignments ADD COLUMN source_contract_id TEXT REFERENCES contract_versions(id)`)
}

func contractForSubscription(ctx context.Context, q rowQuerier, subID string) (ContractState, bool, error) {
	var id string
	err := q.QueryRowContext(ctx, `SELECT contract_version_id FROM contract_subscriptions WHERE subscription_id=?`, subID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return ContractState{}, false, nil
	}
	if err != nil {
		return ContractState{}, false, err
	}
	c, err := loadContract(ctx, q, id)
	return c, true, err
}

func hasPostContractTransition(ctx context.Context, q rowQuerier, subID string) (bool, error) {
	var count int
	err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM contract_transitions WHERE subscription_id=?`, subID).Scan(&count)
	return count != 0, err
}

func applyPostContractPrice(ctx context.Context, tx *sql.Tx, subID string, contract ContractState, boundary int64) error {
	if contract.PostContractPriceVersionID == "" {
		return ErrConflict
	}
	var current string
	var revision, seats int64
	if err := tx.QueryRowContext(ctx, `SELECT price_version_id,seat_quantity,revision FROM subscriptions WHERE id=?`, subID).Scan(&current, &seats, &revision); err != nil {
		return err
	}
	if current != contract.BasePriceVersionID {
		return ErrConflict
	}
	var next int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(assignment_index),-1)+1 FROM pricing_assignments WHERE subscription_id=?`, subID).Scan(&next); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE pricing_assignments SET effective_end=? WHERE subscription_id=? AND effective_end IS NULL`, boundary, subID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO pricing_assignments(subscription_id,assignment_index,price_version_id,seat_quantity,effective_start,source_contract_id) VALUES(?,?,?,?,?,?)`, subID, next, contract.PostContractPriceVersionID, seats, boundary, contract.ID); err != nil {
		return err
	}
	r, err := tx.ExecContext(ctx, `UPDATE subscriptions SET price_version_id=?,revision=revision+1 WHERE id=? AND revision=? AND price_version_id=?`, contract.PostContractPriceVersionID, subID, revision, current)
	if err != nil {
		return err
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return ErrConflict
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO contract_transitions(subscription_id,contract_version_id,post_price_version_id,effective_at) VALUES(?,?,?,?)`, subID, contract.ID, contract.PostContractPriceVersionID, boundary); err != nil {
		return err
	}
	return nil
}

func loadContract(ctx context.Context, q rowQuerier, id string) (ContractState, error) {
	var c ContractState
	var from, to, published int64
	var post sql.NullString
	err := q.QueryRowContext(ctx, `SELECT id,customer_id,version,base_price_version_id,fixed_minor,seat_minor,effective_from,effective_to,post_price_version_id,checksum,published_at FROM contract_versions WHERE id=?`, id).Scan(&c.ID, &c.CustomerID, &c.Version, &c.BasePriceVersionID, &c.FixedMinor, &c.SeatMinor, &from, &to, &post, &c.Checksum, &published)
	if err != nil {
		return ContractState{}, err
	}
	c.EffectiveFrom = time.Unix(0, from).UTC()
	c.EffectiveTo = time.Unix(0, to).UTC()
	c.PublishedAt = time.Unix(0, published).UTC()
	if post.Valid {
		c.PostContractPriceVersionID = post.String
	}
	return c, nil
}

func (l *Lab) publishContractTx(ctx context.Context, tx *sql.Tx, spec ContractSpec) (ContractState, error) {
	if spec.ID == "" || spec.CustomerID == "" || spec.Version <= 0 || spec.BasePriceVersionID == "" || spec.SeatMinor <= 0 || !minimumUpfrontFits(spec.FixedMinor, spec.SeatMinor) || !unixNanoTimeFits(spec.EffectiveFrom) || !unixNanoTimeFits(spec.EffectiveTo) || !spec.EffectiveTo.After(spec.EffectiveFrom) {
		return ContractState{}, ErrConflict
	}
	checksum := hash(spec.ID, spec.CustomerID, spec.Version, spec.BasePriceVersionID, spec.FixedMinor, spec.SeatMinor, spec.EffectiveFrom.UTC().UnixNano(), spec.EffectiveTo.UTC().UnixNano(), spec.PostContractPriceVersionID, "net30")
	var saved string
	err := tx.QueryRowContext(ctx, `SELECT checksum FROM contract_versions WHERE id=?`, spec.ID).Scan(&saved)
	if err == nil {
		if saved != checksum {
			return ContractState{}, ErrConflict
		}
		return loadContract(ctx, tx, spec.ID)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return ContractState{}, err
	}
	publishedAt := l.now().UTC()
	if !unixNanoTimeFits(publishedAt) {
		return ContractState{}, ErrConflict
	}
	var basePlan string
	if err := tx.QueryRowContext(ctx, `SELECT plan_id FROM price_versions WHERE id=? AND publication_state='published'`, spec.BasePriceVersionID).Scan(&basePlan); err != nil {
		return ContractState{}, err
	}
	if basePlan != "pro" {
		return ContractState{}, ErrConflict
	}
	if spec.PostContractPriceVersionID != "" {
		var postPlan string
		var postFrom int64
		var postTo sql.NullInt64
		if err := tx.QueryRowContext(ctx, `SELECT plan_id,effective_from,effective_to FROM price_versions WHERE id=? AND publication_state='published'`, spec.PostContractPriceVersionID).Scan(&postPlan, &postFrom, &postTo); err != nil {
			return ContractState{}, err
		}
		at := spec.EffectiveTo.UTC().UnixNano()
		if postPlan != "pro" || postFrom > at || (postTo.Valid && postTo.Int64 <= at) {
			return ContractState{}, ErrConflict
		}
	}
	var post any
	if spec.PostContractPriceVersionID != "" {
		post = spec.PostContractPriceVersionID
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO contract_versions(id,customer_id,version,base_price_version_id,fixed_minor,seat_minor,payment_days,effective_from,effective_to,post_price_version_id,checksum,published_at) VALUES(?,?,?,?,?,?,30,?,?,?,?,?)`, spec.ID, spec.CustomerID, spec.Version, spec.BasePriceVersionID, spec.FixedMinor, spec.SeatMinor, spec.EffectiveFrom.UTC().UnixNano(), spec.EffectiveTo.UTC().UnixNano(), post, checksum, publishedAt.UnixNano()); err != nil {
		return ContractState{}, err
	}
	return loadContract(ctx, tx, spec.ID)
}

func (l *Lab) PublishContract(ctx context.Context, spec ContractSpec) (ContractState, error) {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return ContractState{}, err
	}
	defer tx.Rollback()
	state, err := l.publishContractTx(ctx, tx, spec)
	if err != nil {
		return ContractState{}, err
	}
	if err := tx.Commit(); err != nil {
		return ContractState{}, err
	}
	return state, nil
}

func (l *Lab) IsContractQuote(ctx context.Context, quoteID string) (bool, error) {
	var count int
	err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM contract_quotes WHERE quote_id=?`, quoteID).Scan(&count)
	return count != 0, err
}

func (l *Lab) CreateContractQuote(ctx context.Context, customerID, contractID string, seats int64) (Quote, error) {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return Quote{}, err
	}
	defer tx.Rollback()
	q, err := l.createContractQuoteTx(ctx, tx, l.now().UTC(), customerID, contractID, seats)
	if err != nil {
		return Quote{}, err
	}
	if err := tx.Commit(); err != nil {
		return Quote{}, err
	}
	return q, nil
}

func (l *Lab) createContractQuoteTx(ctx context.Context, tx *sql.Tx, now time.Time, customerID, contractID string, seats int64) (Quote, error) {
	if customerID == "" || contractID == "" || seats <= 0 {
		return Quote{}, ErrConflict
	}
	expires, expiryErr := quoteExpiryAt(now)
	if expiryErr != nil {
		return Quote{}, expiryErr
	}
	contract, err := loadContract(ctx, tx, contractID)
	if err != nil {
		return Quote{}, err
	}
	if contract.CustomerID != customerID || now.Before(contract.EffectiveFrom) || !now.Before(contract.EffectiveTo) || cycleBoundary(now, 1).After(contract.EffectiveTo) {
		return Quote{}, ErrConflict
	}
	if seats > (math.MaxInt64-contract.FixedMinor)/contract.SeatMinor {
		return Quote{}, ErrConflict
	}
	terms, err := loadPriceTerms(ctx, tx, contract.BasePriceVersionID)
	if err != nil {
		return Quote{}, err
	}
	q := Quote{CustomerID: customerID, PriceVersionID: contract.BasePriceVersionID, ContractVersionID: contractID, SeatQuantity: seats, AmountMinor: contract.FixedMinor + contract.SeatMinor*seats, Currency: terms.Currency, ExpiresAt: expires}
	q.ID, err = newID("quote_")
	if err != nil {
		return Quote{}, err
	}
	q.Fingerprint = hash(q.ID, q.CustomerID, q.PriceVersionID, terms.Checksum, contractID, contract.Checksum, seats, q.AmountMinor, q.Currency, q.ExpiresAt.UnixNano())
	if _, err := tx.ExecContext(ctx, `INSERT INTO quotes(id,customer_id,price_version_id,amount_minor,currency,expires_at,fingerprint,seat_quantity) VALUES(?,?,?,?,?,?,?,?)`, q.ID, q.CustomerID, q.PriceVersionID, q.AmountMinor, q.Currency, q.ExpiresAt.UnixNano(), q.Fingerprint, seats); err != nil {
		return Quote{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO contract_quotes(quote_id,contract_version_id) VALUES(?,?)`, q.ID, contractID); err != nil {
		return Quote{}, err
	}
	return q, nil
}

func isContractInvoice(ctx context.Context, q rowQuerier, invoiceID string) (bool, error) {
	var count int
	err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM invoice_contracts WHERE invoice_id=?`, invoiceID).Scan(&count)
	return count != 0, err
}
