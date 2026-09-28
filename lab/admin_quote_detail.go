package lab

import (
	"context"
	"database/sql"
	"time"
)

type AdminQuoteDetailState struct {
	QuoteState
	ChangeMode             string
	ChangeSubscriptionID   string
	BindingFingerprint     string
	DueNowMinor            *int64
	NextFullTermFixedMinor int64
	UsageMeterID           string
	IncludedQuantity       int64
	UsageRateNum           int64
	UsageRateDen           int64
}

func (l *Lab) AdminQuoteDetail(ctx context.Context, id string) (AdminQuoteDetailState, error) {
	tx, err := l.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return AdminQuoteDetailState{}, err
	}
	defer tx.Rollback()
	var quote AdminQuoteDetailState
	var expires int64
	err = tx.QueryRowContext(ctx, `SELECT q.id,q.customer_id,q.price_version_id,q.amount_minor,q.currency,q.expires_at,
		q.fingerprint,q.seat_quantity,COALESCE(c.contract_version_id,''),COALESCE(b.mode,''),
		COALESCE(b.subscription_id,''),COALESCE(b.fingerprint,''),
		EXISTS(SELECT 1 FROM subscriptions s WHERE s.quote_id=q.id)
		FROM quotes q LEFT JOIN contract_quotes c ON c.quote_id=q.id LEFT JOIN change_quote_bindings b ON b.quote_id=q.id WHERE q.id=?`, id).
		Scan(&quote.ID, &quote.CustomerID, &quote.PriceVersionID, &quote.AmountMinor,
			&quote.Currency, &expires, &quote.Fingerprint, &quote.SeatQuantity,
			&quote.ContractVersionID, &quote.ChangeMode, &quote.ChangeSubscriptionID,
			&quote.BindingFingerprint, &quote.Accepted)
	if err != nil {
		return AdminQuoteDetailState{}, err
	}
	terms, err := loadPriceTerms(ctx, tx, quote.PriceVersionID)
	if err != nil {
		return AdminQuoteDetailState{}, err
	}
	if terms.Currency != quote.Currency {
		return AdminQuoteDetailState{}, ErrConflict
	}
	quote.ExpiresAt = time.Unix(0, expires).UTC()
	quote.NextFullTermFixedMinor = quote.AmountMinor
	quote.UsageMeterID = terms.MeterID
	quote.IncludedQuantity = terms.IncludedQuantity
	quote.UsageRateNum = terms.UsageRateNum
	quote.UsageRateDen = terms.UsageRateDen
	if quote.ContractVersionID != "" || quote.ChangeMode == "next_period" {
		zero := int64(0)
		quote.DueNowMinor = &zero
	} else if quote.ChangeMode == "" {
		quote.DueNowMinor = &quote.AmountMinor
	}
	if err := tx.Commit(); err != nil {
		return AdminQuoteDetailState{}, err
	}
	return quote, nil
}
