package lab

import (
	"context"
	"database/sql"
	"time"
)

type AdminContractQuote struct {
	ID           string
	SeatQuantity int64
	AmountMinor  int64
	Currency     string
	ExpiresAt    time.Time
	Accepted     bool
}

type AdminContractSubscription struct {
	ID                      string
	Status                  string
	SeatQuantity            int64
	LatestInvoiceID         string
	LatestDueAt             *time.Time
	InvoiceOutstandingMinor *int64
	InvoiceCurrency         string
	Transitioned            bool
}

type AdminContractDetail struct {
	ID                         string
	CustomerID                 string
	Version                    int64
	BasePriceVersionID         string
	Currency                   string
	FixedMinor                 int64
	SeatMinor                  int64
	PaymentDays                int64
	EffectiveFrom              time.Time
	EffectiveTo                time.Time
	PostContractPriceVersionID string
	Checksum                   string
	PublishedAt                time.Time
	QuoteCount                 int64
	SubscriptionCount          int64
	Quotes                     []AdminContractQuote
	QuotesTruncated            bool
	Subscriptions              []AdminContractSubscription
	SubscriptionsTruncated     bool
}

// AdminContractVersionDetail reads contract terms and bounded relationships
// from one commerce snapshot. Invoice outstanding uses the shared ledger logic.
func (l *Lab) AdminContractVersionDetail(ctx context.Context, id string) (AdminContractDetail, error) {
	tx, err := l.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return AdminContractDetail{}, err
	}
	defer tx.Rollback()

	var detail AdminContractDetail
	var effectiveFrom, effectiveTo, publishedAt int64
	var postPrice sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT c.id,c.customer_id,c.version,c.base_price_version_id,p.currency,c.fixed_minor,c.seat_minor,c.payment_days,c.effective_from,c.effective_to,c.post_price_version_id,c.checksum,c.published_at FROM contract_versions c JOIN price_versions p ON p.id=c.base_price_version_id WHERE c.id=?`, id).
		Scan(&detail.ID, &detail.CustomerID, &detail.Version, &detail.BasePriceVersionID, &detail.Currency, &detail.FixedMinor, &detail.SeatMinor, &detail.PaymentDays, &effectiveFrom, &effectiveTo, &postPrice, &detail.Checksum, &publishedAt)
	if err != nil {
		return AdminContractDetail{}, err
	}
	detail.EffectiveFrom = time.Unix(0, effectiveFrom).UTC()
	detail.EffectiveTo = time.Unix(0, effectiveTo).UTC()
	detail.PublishedAt = time.Unix(0, publishedAt).UTC()
	if postPrice.Valid {
		detail.PostContractPriceVersionID = postPrice.String
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM contract_quotes WHERE contract_version_id=?`, id).Scan(&detail.QuoteCount); err != nil {
		return AdminContractDetail{}, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM contract_subscriptions WHERE contract_version_id=?`, id).Scan(&detail.SubscriptionCount); err != nil {
		return AdminContractDetail{}, err
	}

	quoteRows, err := tx.QueryContext(ctx, `SELECT q.id,q.seat_quantity,q.amount_minor,q.currency,q.expires_at,EXISTS(SELECT 1 FROM subscriptions s WHERE s.quote_id=q.id) FROM contract_quotes cq JOIN quotes q ON q.id=cq.quote_id WHERE cq.contract_version_id=? ORDER BY q.rowid DESC LIMIT 21`, id)
	if err != nil {
		return AdminContractDetail{}, err
	}
	detail.Quotes = make([]AdminContractQuote, 0)
	for quoteRows.Next() {
		var quote AdminContractQuote
		var expiresAt int64
		if err := quoteRows.Scan(&quote.ID, &quote.SeatQuantity, &quote.AmountMinor, &quote.Currency, &expiresAt, &quote.Accepted); err != nil {
			quoteRows.Close()
			return AdminContractDetail{}, err
		}
		if len(detail.Quotes) == 20 {
			detail.QuotesTruncated = true
			break
		}
		quote.ExpiresAt = time.Unix(0, expiresAt).UTC()
		detail.Quotes = append(detail.Quotes, quote)
	}
	if err := quoteRows.Err(); err != nil {
		quoteRows.Close()
		return AdminContractDetail{}, err
	}
	if err := quoteRows.Close(); err != nil {
		return AdminContractDetail{}, err
	}

	subRows, err := tx.QueryContext(ctx, `SELECT cs.subscription_id,s.status,s.seat_quantity,p.invoice_id,p.due_at,EXISTS(SELECT 1 FROM contract_transitions ct WHERE ct.subscription_id=s.id) FROM contract_subscriptions cs JOIN subscriptions s ON s.id=cs.subscription_id LEFT JOIN billing_periods p ON p.subscription_id=s.id AND p.period_index=(SELECT MAX(period_index) FROM billing_periods WHERE subscription_id=s.id) WHERE cs.contract_version_id=? ORDER BY cs.subscription_id LIMIT 21`, id)
	if err != nil {
		return AdminContractDetail{}, err
	}
	detail.Subscriptions = make([]AdminContractSubscription, 0)
	for subRows.Next() {
		var sub AdminContractSubscription
		var invoiceID sql.NullString
		var dueAt sql.NullInt64
		if err := subRows.Scan(&sub.ID, &sub.Status, &sub.SeatQuantity, &invoiceID, &dueAt, &sub.Transitioned); err != nil {
			subRows.Close()
			return AdminContractDetail{}, err
		}
		if len(detail.Subscriptions) == 20 {
			detail.SubscriptionsTruncated = true
			break
		}
		if invoiceID.Valid {
			sub.LatestInvoiceID = invoiceID.String
		}
		if dueAt.Valid {
			value := time.Unix(0, dueAt.Int64).UTC()
			sub.LatestDueAt = &value
		}
		detail.Subscriptions = append(detail.Subscriptions, sub)
	}
	if err := subRows.Err(); err != nil {
		subRows.Close()
		return AdminContractDetail{}, err
	}
	if err := subRows.Close(); err != nil {
		return AdminContractDetail{}, err
	}
	for index := range detail.Subscriptions {
		sub := &detail.Subscriptions[index]
		if sub.LatestInvoiceID == "" {
			continue
		}
		balance, err := loadInvoiceBalance(ctx, tx, sub.LatestInvoiceID)
		if err != nil {
			return AdminContractDetail{}, err
		}
		sub.InvoiceOutstandingMinor = &balance.OutstandingMinor
		sub.InvoiceCurrency = balance.Currency
	}
	if err := tx.Commit(); err != nil {
		return AdminContractDetail{}, err
	}
	return detail, nil
}
