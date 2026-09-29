package lab

import (
	"context"
	"database/sql"
	"time"
)

type ChangeQuoteBinding struct {
	QuoteID          string
	SubscriptionID   string
	Mode             string
	ExpectedRevision int64
	Fingerprint      string
}

func migrateChangeQuotes(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS change_quote_bindings (
		quote_id TEXT PRIMARY KEY REFERENCES quotes(id),
		subscription_id TEXT NOT NULL REFERENCES subscriptions(id),
		mode TEXT NOT NULL CHECK(mode IN ('next_period','immediate')),
		expected_revision INTEGER NOT NULL CHECK(expected_revision>0),
		fingerprint TEXT NOT NULL);`)
	return err
}

func (l *Lab) BindChangeQuote(ctx context.Context, quoteID, subID, mode string, expectedRevision int64) (ChangeQuoteBinding, error) {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return ChangeQuoteBinding{}, err
	}
	defer tx.Rollback()
	binding, err := l.bindChangeQuoteTx(ctx, tx, l.now().UTC(), quoteID, subID, mode, expectedRevision)
	if err != nil {
		return ChangeQuoteBinding{}, err
	}
	return binding, tx.Commit()
}

func (l *Lab) bindChangeQuoteTx(ctx context.Context, tx *sql.Tx, at time.Time, quoteID, subID, mode string, expectedRevision int64) (ChangeQuoteBinding, error) {
	if quoteID == "" || subID == "" || (mode != "next_period" && mode != "immediate") || expectedRevision <= 0 {
		return ChangeQuoteBinding{}, ErrConflict
	}
	var quoteCustomer, subCustomer, quoteFingerprint, status string
	var revision, expiry int64
	var previouslyAccepted, contractQuote int
	if err := tx.QueryRowContext(ctx, `SELECT q.customer_id,q.fingerprint,q.expires_at,
		EXISTS(SELECT 1 FROM subscriptions s WHERE s.quote_id=q.id),
		EXISTS(SELECT 1 FROM contract_quotes c WHERE c.quote_id=q.id)
		FROM quotes q WHERE q.id=?`, quoteID).Scan(&quoteCustomer, &quoteFingerprint, &expiry, &previouslyAccepted, &contractQuote); err != nil {
		return ChangeQuoteBinding{}, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT customer_id,revision,status FROM subscriptions WHERE id=?`, subID).Scan(&subCustomer, &revision, &status); err != nil {
		return ChangeQuoteBinding{}, err
	}
	if quoteCustomer != subCustomer || status != "active" || previouslyAccepted != 0 || contractQuote != 0 || !at.Before(time.Unix(0, expiry)) {
		return ChangeQuoteBinding{}, ErrConflict
	}
	if revision != expectedRevision {
		return ChangeQuoteBinding{}, ErrChangeQuoteRevisionChanged
	}
	b := ChangeQuoteBinding{QuoteID: quoteID, SubscriptionID: subID, Mode: mode, ExpectedRevision: expectedRevision, Fingerprint: hash(quoteFingerprint, subID, mode, expectedRevision)}
	if _, err := tx.ExecContext(ctx, `INSERT INTO change_quote_bindings(quote_id,subscription_id,mode,expected_revision,fingerprint) VALUES(?,?,?,?,?) ON CONFLICT(quote_id) DO NOTHING`, b.QuoteID, b.SubscriptionID, b.Mode, b.ExpectedRevision, b.Fingerprint); err != nil {
		return ChangeQuoteBinding{}, err
	}
	var saved ChangeQuoteBinding
	if err := tx.QueryRowContext(ctx, `SELECT quote_id,subscription_id,mode,expected_revision,fingerprint FROM change_quote_bindings WHERE quote_id=?`, quoteID).Scan(&saved.QuoteID, &saved.SubscriptionID, &saved.Mode, &saved.ExpectedRevision, &saved.Fingerprint); err != nil {
		return ChangeQuoteBinding{}, err
	}
	if saved != b {
		return ChangeQuoteBinding{}, ErrConflict
	}
	return b, nil
}

func (l *Lab) ChangeQuoteBinding(ctx context.Context, quoteID string) (ChangeQuoteBinding, error) {
	var b ChangeQuoteBinding
	err := l.db.QueryRowContext(ctx, `SELECT quote_id,subscription_id,mode,expected_revision,fingerprint FROM change_quote_bindings WHERE quote_id=?`, quoteID).Scan(&b.QuoteID, &b.SubscriptionID, &b.Mode, &b.ExpectedRevision, &b.Fingerprint)
	return b, err
}
