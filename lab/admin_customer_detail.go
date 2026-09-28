package lab

import (
	"context"
	"database/sql"
	"time"
)

type AdminCustomerSummary struct {
	ID                string
	SubscriptionCount int64
	QuoteCount        int64
}

type AdminCustomerQuote struct {
	ID                string
	AmountMinor       int64
	Currency          string
	ExpiresAt         time.Time
	Accepted          bool
	ContractVersionID string
}

type AdminCustomerInvoice struct {
	ID             string
	SubscriptionID string
	TotalMinor     int64
	Currency       string
}

type AdminCustomerCredit struct {
	ID          string
	InvoiceID   string
	AmountMinor int64
	Currency    string
}

type AdminCustomerDetail struct {
	ID                string
	SubscriptionCount int64
	QuoteCount        int64
	InvoiceCount      int64
	CreditCount       int64
	Subscriptions     []SubscriptionState
	Quotes            []AdminCustomerQuote
	Invoices          []AdminCustomerInvoice
	Credits           []AdminCustomerCredit
	LegacyAccountID   string
	ReadOwner         string
	WriterOwner       string
	RelatedLimit      int
}

// AdminCustomerPage uses customer IDs as a stable keyset cursor. There is no
// mutable customer table: customer identities come from quotes and subscriptions.
func (l *Lab) AdminCustomerPage(ctx context.Context, after string, limit int) ([]AdminCustomerSummary, int64, string, error) {
	if limit < 1 || limit > 100 {
		return nil, 0, "", ErrAdminInvalidCommand
	}
	tx, err := l.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, 0, "", err
	}
	defer tx.Rollback()
	var total int64
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM (SELECT customer_id FROM quotes UNION SELECT customer_id FROM subscriptions)`).Scan(&total); err != nil {
		return nil, 0, "", err
	}
	rows, err := tx.QueryContext(ctx, `SELECT c.customer_id,
		(SELECT COUNT(*) FROM subscriptions s WHERE s.customer_id=c.customer_id),
		(SELECT COUNT(*) FROM quotes q WHERE q.customer_id=c.customer_id)
		FROM (SELECT customer_id FROM quotes UNION SELECT customer_id FROM subscriptions) c
		WHERE c.customer_id>? ORDER BY c.customer_id LIMIT ?`, after, limit+1)
	if err != nil {
		return nil, 0, "", err
	}
	items := make([]AdminCustomerSummary, 0, limit)
	for rows.Next() {
		var item AdminCustomerSummary
		if err := rows.Scan(&item.ID, &item.SubscriptionCount, &item.QuoteCount); err != nil {
			rows.Close()
			return nil, 0, "", err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, 0, "", err
	}
	rows.Close()
	next := ""
	if len(items) > limit {
		items = items[:limit]
		next = items[len(items)-1].ID
	}
	if err := tx.Commit(); err != nil {
		return nil, 0, "", err
	}
	return items, total, next, nil
}

func (l *Lab) AdminCustomerDetail(ctx context.Context, id string) (AdminCustomerDetail, error) {
	tx, err := l.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return AdminCustomerDetail{}, err
	}
	defer tx.Rollback()
	var d AdminCustomerDetail
	d.ID, d.RelatedLimit = id, 100
	var exists int
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM quotes WHERE customer_id=?) OR EXISTS(SELECT 1 FROM subscriptions WHERE customer_id=?)`, id, id).Scan(&exists)
	if err != nil {
		return AdminCustomerDetail{}, err
	}
	if exists == 0 {
		return AdminCustomerDetail{}, sql.ErrNoRows
	}
	for _, count := range []struct {
		query string
		dest  *int64
	}{
		{`SELECT COUNT(*) FROM subscriptions WHERE customer_id=?`, &d.SubscriptionCount},
		{`SELECT COUNT(*) FROM quotes WHERE customer_id=?`, &d.QuoteCount},
		{`SELECT COUNT(*) FROM invoices i JOIN subscriptions s ON s.id=i.subscription_id WHERE s.customer_id=?`, &d.InvoiceCount},
		{`SELECT COUNT(*) FROM credit_grants g JOIN invoices i ON i.id=g.source_invoice_id JOIN subscriptions s ON s.id=i.subscription_id WHERE s.customer_id=?`, &d.CreditCount},
	} {
		if err := tx.QueryRowContext(ctx, count.query, id).Scan(count.dest); err != nil {
			return AdminCustomerDetail{}, err
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT s.id,s.customer_id,s.price_version_id,s.seat_quantity,s.revision,
		CASE WHEN EXISTS(SELECT 1 FROM subscription_ends se WHERE se.subscription_id=s.id) THEN 'ended' ELSE s.status END,
		COALESCE(e.status,''),COALESCE(e.reason,''),COALESCE(e.source_revision,0)
		FROM subscriptions s LEFT JOIN entitlements e ON e.subscription_id=s.id
		WHERE s.customer_id=? ORDER BY s.created_at DESC,s.id DESC LIMIT 100`, id)
	if err != nil {
		return AdminCustomerDetail{}, err
	}
	for rows.Next() {
		var item SubscriptionState
		if err := rows.Scan(&item.ID, &item.CustomerID, &item.PriceVersionID, &item.SeatQuantity, &item.Revision, &item.Status, &item.EntitlementStatus, &item.EntitlementReason, &item.EntitlementSourceRevision); err != nil {
			rows.Close()
			return AdminCustomerDetail{}, err
		}
		d.Subscriptions = append(d.Subscriptions, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return AdminCustomerDetail{}, err
	}
	rows.Close()
	rows, err = tx.QueryContext(ctx, `SELECT q.id,q.amount_minor,q.currency,q.expires_at,
		EXISTS(SELECT 1 FROM subscriptions s WHERE s.quote_id=q.id),COALESCE(c.contract_version_id,'')
		FROM quotes q LEFT JOIN contract_quotes c ON c.quote_id=q.id
		WHERE q.customer_id=? ORDER BY q.rowid DESC LIMIT 100`, id)
	if err != nil {
		return AdminCustomerDetail{}, err
	}
	for rows.Next() {
		var item AdminCustomerQuote
		var expires int64
		if err := rows.Scan(&item.ID, &item.AmountMinor, &item.Currency, &expires, &item.Accepted, &item.ContractVersionID); err != nil {
			rows.Close()
			return AdminCustomerDetail{}, err
		}
		item.ExpiresAt = time.Unix(0, expires).UTC()
		d.Quotes = append(d.Quotes, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return AdminCustomerDetail{}, err
	}
	rows.Close()
	rows, err = tx.QueryContext(ctx, `SELECT i.id,i.subscription_id,i.total_minor,i.currency
		FROM invoices i JOIN subscriptions s ON s.id=i.subscription_id
		WHERE s.customer_id=? ORDER BY i.rowid DESC LIMIT 100`, id)
	if err != nil {
		return AdminCustomerDetail{}, err
	}
	for rows.Next() {
		var item AdminCustomerInvoice
		if err := rows.Scan(&item.ID, &item.SubscriptionID, &item.TotalMinor, &item.Currency); err != nil {
			rows.Close()
			return AdminCustomerDetail{}, err
		}
		d.Invoices = append(d.Invoices, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return AdminCustomerDetail{}, err
	}
	rows.Close()
	rows, err = tx.QueryContext(ctx, `SELECT g.id,g.source_invoice_id,g.amount_minor,g.currency
		FROM credit_grants g JOIN invoices i ON i.id=g.source_invoice_id JOIN subscriptions s ON s.id=i.subscription_id
		WHERE s.customer_id=? ORDER BY g.rowid DESC LIMIT 100`, id)
	if err != nil {
		return AdminCustomerDetail{}, err
	}
	for rows.Next() {
		var item AdminCustomerCredit
		if err := rows.Scan(&item.ID, &item.InvoiceID, &item.AmountMinor, &item.Currency); err != nil {
			rows.Close()
			return AdminCustomerDetail{}, err
		}
		d.Credits = append(d.Credits, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return AdminCustomerDetail{}, err
	}
	rows.Close()
	err = tx.QueryRowContext(ctx, `SELECT legacy_account_id,read_owner,writer_owner FROM account_links WHERE customer_id=?`, id).
		Scan(&d.LegacyAccountID, &d.ReadOwner, &d.WriterOwner)
	if err != nil && err != sql.ErrNoRows {
		return AdminCustomerDetail{}, err
	}
	if err := tx.Commit(); err != nil {
		return AdminCustomerDetail{}, err
	}
	return d, nil
}
