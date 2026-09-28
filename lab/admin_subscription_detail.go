package lab

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// AdminSubscriptionDetail is a bounded, consistent read for one subscription.
// Amounts remain integer minor units so the API can serialize them as strings.
type AdminSubscriptionDetail struct {
	SubscriptionState
	QuoteID           string
	ContractVersionID string
	PricePlanID       string
	Currency          string
	ActualFixedMinor  int64
	ActualSeatMinor   int64
	CurrentPeriod     *BillingPeriod
	ScheduledChange   *ScheduleResult
	ScheduledCancel   *ScheduleResult
	HoldReason        string
}

func (l *Lab) AdminSubscriptionDetail(ctx context.Context, id string) (AdminSubscriptionDetail, error) {
	tx, err := l.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return AdminSubscriptionDetail{}, err
	}
	defer tx.Rollback()
	var d AdminSubscriptionDetail
	var catalogFixed, catalogSeat, contractFixed, contractSeat int64
	var transitioned int
	err = tx.QueryRowContext(ctx, `SELECT s.id,s.customer_id,s.price_version_id,s.seat_quantity,s.revision,
		CASE WHEN EXISTS(SELECT 1 FROM subscription_ends se WHERE se.subscription_id=s.id) THEN 'ended' ELSE s.status END,
		COALESCE(e.status,''),COALESCE(e.reason,''),COALESCE(e.source_revision,0),COALESCE(s.quote_id,''),
		COALESCE(cs.contract_version_id,''),p.plan_id,p.currency,p.fixed_amount_minor,
		COALESCE((SELECT amount_minor FROM price_components pc WHERE pc.price_version_id=p.id AND pc.component_code='seats'),0),
		COALESCE(cv.fixed_minor,0),COALESCE(cv.seat_minor,0),
		EXISTS(SELECT 1 FROM contract_transitions ct WHERE ct.subscription_id=s.id)
		FROM subscriptions s
		JOIN price_versions p ON p.id=s.price_version_id
		LEFT JOIN entitlements e ON e.subscription_id=s.id
		LEFT JOIN contract_subscriptions cs ON cs.subscription_id=s.id
		LEFT JOIN contract_versions cv ON cv.id=cs.contract_version_id
		WHERE s.id=?`, id).Scan(
		&d.ID, &d.CustomerID, &d.PriceVersionID, &d.SeatQuantity, &d.Revision, &d.Status,
		&d.EntitlementStatus, &d.EntitlementReason, &d.EntitlementSourceRevision,
		&d.QuoteID, &d.ContractVersionID, &d.PricePlanID, &d.Currency,
		&catalogFixed, &catalogSeat, &contractFixed, &contractSeat, &transitioned,
	)
	if err != nil {
		return AdminSubscriptionDetail{}, err
	}
	d.ActualFixedMinor, d.ActualSeatMinor = catalogFixed, catalogSeat
	if d.ContractVersionID != "" && transitioned == 0 {
		d.ActualFixedMinor, d.ActualSeatMinor = contractFixed, contractSeat
	}
	var period BillingPeriod
	var start, end, due int64
	err = tx.QueryRowContext(ctx, `SELECT period_index,period_start,period_end,due_at,invoice_id
		FROM billing_periods WHERE subscription_id=? ORDER BY period_index DESC LIMIT 1`, id).
		Scan(&period.Index, &start, &end, &due, &period.InvoiceID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return AdminSubscriptionDetail{}, err
	}
	if err == nil {
		period.Start = time.Unix(0, start).UTC()
		period.End = time.Unix(0, end).UTC()
		period.DueAt = time.Unix(0, due).UTC()
		d.CurrentPeriod = &period
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,kind,COALESCE(target_price_version_id,''),seat_quantity,effective_at,status,created_revision+1
		FROM subscription_schedules WHERE subscription_id=? AND status='scheduled' ORDER BY effective_at,id`, id)
	if err != nil {
		return AdminSubscriptionDetail{}, err
	}
	for rows.Next() {
		var schedule ScheduleResult
		var effective int64
		schedule.SubscriptionID = id
		if err := rows.Scan(&schedule.ID, &schedule.Kind, &schedule.TargetPriceVersionID, &schedule.SeatQuantity, &effective, &schedule.Status, &schedule.Revision); err != nil {
			rows.Close()
			return AdminSubscriptionDetail{}, err
		}
		schedule.EffectiveAt = time.Unix(0, effective).UTC()
		if schedule.Kind == "cancel" {
			d.ScheduledCancel = &schedule
		} else {
			d.ScheduledChange = &schedule
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return AdminSubscriptionDetail{}, err
	}
	rows.Close()
	err = tx.QueryRowContext(ctx, `SELECT reason FROM renewal_holds WHERE subscription_id=? ORDER BY period_index DESC LIMIT 1`, id).Scan(&d.HoldReason)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return AdminSubscriptionDetail{}, err
	}
	if err := tx.Commit(); err != nil {
		return AdminSubscriptionDetail{}, err
	}
	return d, nil
}
