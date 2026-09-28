package lab

import (
	"context"
	"time"
)

func (l *Lab) AdminQuoteDetail(ctx context.Context, id string) (QuoteState, error) {
	var quote QuoteState
	var expires int64
	err := l.db.QueryRowContext(ctx, `SELECT q.id,q.customer_id,q.price_version_id,q.amount_minor,q.currency,q.expires_at,
		q.fingerprint,q.seat_quantity,COALESCE(c.contract_version_id,''),
		EXISTS(SELECT 1 FROM subscriptions s WHERE s.quote_id=q.id)
		FROM quotes q LEFT JOIN contract_quotes c ON c.quote_id=q.id WHERE q.id=?`, id).
		Scan(&quote.ID, &quote.CustomerID, &quote.PriceVersionID, &quote.AmountMinor,
			&quote.Currency, &expires, &quote.Fingerprint, &quote.SeatQuantity,
			&quote.ContractVersionID, &quote.Accepted)
	if err != nil {
		return QuoteState{}, err
	}
	quote.ExpiresAt = time.Unix(0, expires).UTC()
	return quote, nil
}
