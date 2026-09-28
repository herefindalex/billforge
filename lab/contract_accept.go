package lab

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"time"
)

func (l *Lab) AcceptContractQuote(ctx context.Context, quoteID, fingerprint, requestKey string) (Receipt, error) {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return Receipt{}, err
	}
	defer tx.Rollback()
	r, err := l.acceptContractQuoteTx(ctx, tx, l.now().UTC(), quoteID, fingerprint, requestKey)
	if err != nil {
		return Receipt{}, err
	}
	if err := tx.Commit(); err != nil {
		return Receipt{}, err
	}
	return r, nil
}

func (l *Lab) acceptContractQuoteTx(ctx context.Context, tx *sql.Tx, now time.Time, quoteID, fingerprint, requestKey string) (Receipt, error) {
	if quoteID == "" || fingerprint == "" || requestKey == "" {
		return Receipt{}, ErrConflict
	}
	payload := hash(quoteID, fingerprint)
	var err error
	var savedHash, savedSub string
	err = tx.QueryRowContext(ctx, `SELECT payload_hash,subscription_id FROM accept_requests WHERE key=?`, requestKey).Scan(&savedHash, &savedSub)
	if err == nil {
		if savedHash != payload {
			return Receipt{}, ErrConflict
		}
		var r Receipt
		err = tx.QueryRowContext(ctx, `SELECT s.id,i.id,o.id,i.total_minor,i.currency FROM subscriptions s JOIN billing_periods p ON p.subscription_id=s.id AND p.period_index=0 JOIN invoices i ON i.id=p.invoice_id JOIN payment_operations o ON o.invoice_id=i.id JOIN contract_subscriptions cs ON cs.subscription_id=s.id WHERE s.id=?`, savedSub).Scan(&r.SubscriptionID, &r.InvoiceID, &r.OperationID, &r.AmountMinor, &r.Currency)
		return r, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Receipt{}, err
	}
	var q Quote
	var expires int64
	err = tx.QueryRowContext(ctx, `SELECT q.customer_id,q.price_version_id,q.amount_minor,q.currency,q.expires_at,q.fingerprint,q.seat_quantity,c.contract_version_id FROM quotes q JOIN contract_quotes c ON c.quote_id=q.id WHERE q.id=?`, quoteID).Scan(&q.CustomerID, &q.PriceVersionID, &q.AmountMinor, &q.Currency, &expires, &q.Fingerprint, &q.SeatQuantity, &q.ContractVersionID)
	if err != nil {
		return Receipt{}, err
	}
	q.ID = quoteID
	if fingerprint != q.Fingerprint {
		return Receipt{}, ErrConflict
	}
	if err := ensureCommerceWriter(ctx, tx, q.CustomerID); err != nil {
		return Receipt{}, err
	}
	if now.UnixNano() >= expires {
		return Receipt{}, ErrExpired
	}
	contract, err := loadContract(ctx, tx, q.ContractVersionID)
	if err != nil {
		return Receipt{}, err
	}
	if contract.CustomerID != q.CustomerID || contract.BasePriceVersionID != q.PriceVersionID || now.Before(contract.EffectiveFrom) || !now.Before(contract.EffectiveTo) || cycleBoundary(now, 1).After(contract.EffectiveTo) {
		return Receipt{}, ErrConflict
	}
	if q.SeatQuantity <= 0 || q.SeatQuantity > (math.MaxInt64-contract.FixedMinor)/contract.SeatMinor || q.AmountMinor != contract.FixedMinor+contract.SeatMinor*q.SeatQuantity {
		return Receipt{}, ErrConflict
	}
	var accepted int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM subscriptions WHERE quote_id=?`, quoteID).Scan(&accepted); err != nil {
		return Receipt{}, err
	}
	if accepted != 0 {
		return Receipt{}, ErrConflict
	}
	r := Receipt{AmountMinor: q.AmountMinor, Currency: q.Currency}
	r.SubscriptionID, err = newID("sub_")
	if err != nil {
		return Receipt{}, err
	}
	r.InvoiceID, err = newID("inv_")
	if err != nil {
		return Receipt{}, err
	}
	r.OperationID, err = newID("op_")
	if err != nil {
		return Receipt{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO subscriptions(id,quote_id,customer_id,price_version_id,status,created_at,seat_quantity) VALUES(?,?,?,?,'active',?,?)`, r.SubscriptionID, quoteID, q.CustomerID, q.PriceVersionID, now.UnixNano(), q.SeatQuantity); err != nil {
		return Receipt{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO contract_subscriptions(subscription_id,contract_version_id) VALUES(?,?)`, r.SubscriptionID, contract.ID); err != nil {
		return Receipt{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO invoices(id,subscription_id,total_minor,currency,finalized_at) VALUES(?,?,?,?,NULL)`, r.InvoiceID, r.SubscriptionID, q.AmountMinor, q.Currency); err != nil {
		return Receipt{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO invoice_lines(invoice_id,price_version_id,component_code,amount_minor) VALUES(?,?,'fixed',?)`, r.InvoiceID, q.PriceVersionID, contract.FixedMinor); err != nil {
		return Receipt{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO invoice_lines(invoice_id,price_version_id,component_code,amount_minor) VALUES(?,?,'seats',?)`, r.InvoiceID, q.PriceVersionID, contract.SeatMinor*q.SeatQuantity); err != nil {
		return Receipt{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE invoices SET finalized_at=? WHERE id=?`, now.UnixNano(), r.InvoiceID); err != nil {
		return Receipt{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO invoice_contracts(invoice_id,contract_version_id) VALUES(?,?)`, r.InvoiceID, contract.ID); err != nil {
		return Receipt{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO billing_periods(subscription_id,period_index,period_start,period_end,due_at,invoice_id) VALUES(?,0,?,?,?,?)`, r.SubscriptionID, now.UnixNano(), cycleBoundary(now, 1).UnixNano(), now.Add(30*24*time.Hour).UnixNano(), r.InvoiceID); err != nil {
		return Receipt{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO pricing_assignments(subscription_id,assignment_index,price_version_id,seat_quantity,effective_start,source_contract_id) VALUES(?,0,?,?,?,?)`, r.SubscriptionID, q.PriceVersionID, q.SeatQuantity, now.UnixNano(), contract.ID); err != nil {
		return Receipt{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO payment_operations(id,invoice_id,provider_key,amount_minor,currency,status) VALUES(?,?,?,?,?,'created')`, r.OperationID, r.InvoiceID, "capture:"+r.InvoiceID, q.AmountMinor, q.Currency); err != nil {
		return Receipt{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO entitlements(subscription_id,status,source_operation_id,updated_at,reason,source_invoice_id,source_revision) VALUES(?,'active',?,?,'enterprise_contract_net30',?,1)`, r.SubscriptionID, r.OperationID, now.UnixNano(), r.InvoiceID); err != nil {
		return Receipt{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO accept_requests(key,payload_hash,subscription_id) VALUES(?,?,?)`, requestKey, payload, r.SubscriptionID); err != nil {
		return Receipt{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_events(kind,object_id,at) VALUES('contract_quote_accepted',?,?)`, r.SubscriptionID, now.UnixNano()); err != nil {
		return Receipt{}, err
	}
	return r, nil
}

func (l *Lab) CollectDueContractInvoices(ctx context.Context) (int, error) {
	now := l.now().UTC().UnixNano()
	r, err := l.db.ExecContext(ctx, `INSERT INTO outbox(id,kind,object_id,status)
SELECT 'capture:'||o.id,'capture',o.id,'pending' FROM payment_operations o
JOIN billing_periods p ON p.invoice_id=o.invoice_id
JOIN invoice_contracts c ON c.invoice_id=o.invoice_id
WHERE o.status='created' AND p.due_at<=? AND NOT EXISTS(SELECT 1 FROM outbox b WHERE b.id='capture:'||o.id)`, now)
	if err != nil {
		return 0, err
	}
	n, _ := r.RowsAffected()
	return int(n), nil
}
