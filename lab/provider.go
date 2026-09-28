package lab

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// FakeProvider persists independently of the commerce database. Capture is
// idempotent by provider key, even when the caller loses its response.
type FakeProvider struct{ db *sql.DB }

type ProviderEvent struct {
	ID       string
	Key      string
	Status   string
	Amount   int64
	Currency string
}

func openProvider(path string) (*FakeProvider, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err = migrateProvider(db); err != nil {
		db.Close()
		return nil, err
	}
	if _, err = db.Exec(`PRAGMA foreign_keys=ON;
PRAGMA busy_timeout=5000;
CREATE TABLE IF NOT EXISTS captures (
  provider_key TEXT PRIMARY KEY,
  amount_minor INTEGER NOT NULL CHECK(amount_minor > 0),
  currency TEXT NOT NULL,
  status TEXT NOT NULL CHECK(status IN ('succeeded','definitively_failed'))
);
CREATE TABLE IF NOT EXISTS decisions (
  provider_key TEXT PRIMARY KEY,
  status TEXT NOT NULL CHECK(status IN ('succeeded','definitively_failed'))
);
CREATE TABLE IF NOT EXISTS refunds (
  provider_key TEXT PRIMARY KEY,
  source_capture_key TEXT NOT NULL REFERENCES captures(provider_key),
  amount_minor INTEGER NOT NULL CHECK(amount_minor>0), currency TEXT NOT NULL,
  status TEXT NOT NULL CHECK(status IN ('succeeded','definitively_failed'))
);
CREATE TABLE IF NOT EXISTS refund_decisions (
  provider_key TEXT PRIMARY KEY,
  status TEXT NOT NULL CHECK(status IN ('succeeded','definitively_failed'))
);
CREATE TRIGGER IF NOT EXISTS provider_refund_budget BEFORE INSERT ON refunds
WHEN NEW.status='succeeded' AND
NEW.amount_minor + COALESCE((SELECT SUM(amount_minor) FROM refunds
WHERE source_capture_key=NEW.source_capture_key AND status='succeeded'),0)
> (SELECT amount_minor FROM captures WHERE provider_key=NEW.source_capture_key AND status='succeeded')
BEGIN SELECT RAISE(ABORT,'provider refund exceeds capture'); END;
CREATE TABLE IF NOT EXISTS provider_control_receipts (
 command_id TEXT PRIMARY KEY,
 payload_hash TEXT NOT NULL,
 target_key TEXT NOT NULL,
 result TEXT NOT NULL,
 committed_at TEXT NOT NULL
);`); err != nil {
		db.Close()
		return nil, err
	}
	return &FakeProvider{db: db}, nil
}

func (p *FakeProvider) Close() error { return p.db.Close() }

func (p *FakeProvider) Capture(ctx context.Context, key string, amount int64, currency string) (ProviderEvent, error) {
	var decision string
	err := p.db.QueryRowContext(ctx, `SELECT status FROM decisions WHERE provider_key=?`, key).Scan(&decision)
	if errors.Is(err, sql.ErrNoRows) {
		decision = "succeeded"
	} else if err != nil {
		return ProviderEvent{}, err
	}
	_, err = p.db.ExecContext(ctx, `INSERT INTO captures(provider_key,amount_minor,currency,status)
VALUES(?,?,?,?) ON CONFLICT(provider_key) DO NOTHING`, key, amount, currency, decision)
	if err != nil {
		return ProviderEvent{}, err
	}
	e, found, err := p.Lookup(ctx, key)
	if err != nil {
		return ProviderEvent{}, err
	}
	if !found || e.Amount != amount || e.Currency != currency {
		return ProviderEvent{}, fmt.Errorf("provider key reused with different payload: %w", ErrConflict)
	}
	return e, nil
}

func migrateProvider(db *sql.DB) error {
	var schema string
	err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='captures'`).Scan(&schema)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if !strings.Contains(strings.ToLower(schema), "status = 'succeeded'") {
		return nil
	}
	_, err = db.Exec(`BEGIN;
CREATE TABLE captures_new (
 provider_key TEXT PRIMARY KEY, amount_minor INTEGER NOT NULL CHECK(amount_minor>0),
 currency TEXT NOT NULL, status TEXT NOT NULL CHECK(status IN ('succeeded','definitively_failed')));
INSERT INTO captures_new SELECT provider_key,amount_minor,currency,status FROM captures;
DROP TABLE captures;
ALTER TABLE captures_new RENAME TO captures;
COMMIT;`)
	return err
}

func (p *FakeProvider) SetDecision(ctx context.Context, key, status string) error {
	if status != "succeeded" && status != "definitively_failed" {
		return errors.New("unsupported provider decision")
	}
	var existing string
	err := p.db.QueryRowContext(ctx, `SELECT status FROM captures WHERE provider_key=?`, key).Scan(&existing)
	if err == nil {
		return ErrConflict
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	_, err = p.db.ExecContext(ctx, `INSERT INTO decisions(provider_key,status) VALUES(?,?)
ON CONFLICT(provider_key) DO UPDATE SET status=excluded.status`, key, status)
	return err
}

type RefundEvent struct {
	ID        string
	Key       string
	SourceKey string
	Status    string
	Amount    int64
	Currency  string
}

func (p *FakeProvider) Refund(ctx context.Context, key, sourceKey string, amount int64, currency string) (RefundEvent, error) {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return RefundEvent{}, err
	}
	defer tx.Rollback()
	var captureAmount int64
	var captureCurrency, captureStatus string
	err = tx.QueryRowContext(ctx, `SELECT amount_minor,currency,status FROM captures WHERE provider_key=?`, sourceKey).
		Scan(&captureAmount, &captureCurrency, &captureStatus)
	if err != nil {
		return RefundEvent{}, err
	}
	if captureStatus != "succeeded" || captureCurrency != currency || amount <= 0 || amount > captureAmount {
		return RefundEvent{}, ErrConflict
	}
	var decision string
	err = tx.QueryRowContext(ctx, `SELECT status FROM refund_decisions WHERE provider_key=?`, key).Scan(&decision)
	if errors.Is(err, sql.ErrNoRows) {
		decision = "succeeded"
	} else if err != nil {
		return RefundEvent{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO refunds(provider_key,source_capture_key,amount_minor,currency,status)
VALUES(?,?,?,?,?) ON CONFLICT(provider_key) DO NOTHING`, key, sourceKey, amount, currency, decision)
	if err != nil {
		return RefundEvent{}, err
	}
	var e RefundEvent
	e.ID, e.Key = "provider-refund:"+key, key
	err = tx.QueryRowContext(ctx, `SELECT source_capture_key,status,amount_minor,currency FROM refunds WHERE provider_key=?`, key).
		Scan(&e.SourceKey, &e.Status, &e.Amount, &e.Currency)
	if err != nil {
		return RefundEvent{}, err
	}
	if e.SourceKey != sourceKey || e.Amount != amount || e.Currency != currency {
		return RefundEvent{}, ErrConflict
	}
	return e, tx.Commit()
}

func (p *FakeProvider) LookupRefund(ctx context.Context, key string) (RefundEvent, bool, error) {
	e := RefundEvent{ID: "provider-refund:" + key, Key: key}
	err := p.db.QueryRowContext(ctx, `SELECT source_capture_key,status,amount_minor,currency FROM refunds WHERE provider_key=?`, key).
		Scan(&e.SourceKey, &e.Status, &e.Amount, &e.Currency)
	if errors.Is(err, sql.ErrNoRows) {
		return RefundEvent{}, false, nil
	}
	if err != nil {
		return RefundEvent{}, false, err
	}
	return e, true, nil
}

func (p *FakeProvider) SetRefundDecision(ctx context.Context, key, status string) error {
	if status != "succeeded" && status != "definitively_failed" {
		return errors.New("unsupported refund decision")
	}
	var existing string
	err := p.db.QueryRowContext(ctx, `SELECT status FROM refunds WHERE provider_key=?`, key).Scan(&existing)
	if err == nil {
		return ErrConflict
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	_, err = p.db.ExecContext(ctx, `INSERT INTO refund_decisions(provider_key,status) VALUES(?,?)
ON CONFLICT(provider_key) DO UPDATE SET status=excluded.status`, key, status)
	return err
}

func (p *FakeProvider) Lookup(ctx context.Context, key string) (ProviderEvent, bool, error) {
	e := ProviderEvent{ID: "provider:" + key, Key: key}
	err := p.db.QueryRowContext(ctx, `SELECT status,amount_minor,currency FROM captures WHERE provider_key=?`, key).
		Scan(&e.Status, &e.Amount, &e.Currency)
	if errors.Is(err, sql.ErrNoRows) {
		return ProviderEvent{}, false, nil
	}
	if err != nil {
		return ProviderEvent{}, false, err
	}
	return e, true, nil
}
