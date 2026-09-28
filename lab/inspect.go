package lab

import (
	"context"
	"database/sql"
	"time"
)

// State is a read-only, transactionally consistent view for the local CLI.
// It intentionally returns all rows because this lab has no production-sized dataset.
type State struct {
	Prices                []PriceVersionState
	CatalogSelections     []CatalogSelectionState
	PriceMigrations       []PriceMigration
	Contracts             []ContractState
	ContractSubscriptions []ContractSubscriptionState
	UsageEvents           []UsageEvent
	UsagePeriods          []UsagePeriodState
	UsageRatings          []UsageRating
	UsageCreditNotes      []UsageCreditNoteState
	Quotes                []QuoteState
	Subscriptions         []SubscriptionState
	Schedules             []ScheduleResult
	ImmediateChanges      []ImmediateChange
	Assignments           []PricingAssignmentState
	Periods               []PeriodState
	Invoices              []InvoiceState
	Payments              []PaymentState
	Corrections           []CorrectionState
	Credits               []CreditState
	CreditApplications    []CreditApplicationState
	Refunds               []RefundInfo
	PendingOutbox         []OutboxState
}

type ContractSubscriptionState struct {
	SubscriptionID    string
	ContractVersionID string
	PostTransitioned  bool
}

type PriceVersionState struct {
	ID            string
	PlanID        string
	Version       int64
	Currency      string
	FixedMinor    int64
	SeatMinor     int64
	State         string
	EffectiveFrom time.Time
}

type CatalogSelectionState struct {
	PlanID         string
	Cohort         string
	PriceVersionID string
	EffectiveAt    time.Time
}

type QuoteState struct {
	Quote
	Accepted bool
}

type SubscriptionState struct {
	ID                        string
	CustomerID                string
	PriceVersionID            string
	SeatQuantity              int64
	Revision                  int64
	Status                    string
	EntitlementStatus         string
	EntitlementReason         string
	EntitlementSourceRevision int64
}

type PeriodState struct {
	SubscriptionID string
	BillingPeriod
}

type PricingAssignmentState struct {
	SubscriptionID   string
	Index            int
	PriceVersionID   string
	SeatQuantity     int64
	EffectiveStart   time.Time
	EffectiveEnd     *time.Time
	SourceScheduleID string
}

type InvoiceState struct {
	ID             string
	SubscriptionID string
	PeriodIndex    int
	Balance        InvoiceBalance
}

type PaymentState struct {
	ID          string
	InvoiceID   string
	ProviderKey string
	AmountMinor int64
	Currency    string
	Status      string
	RequestKey  string
}

type CorrectionState struct {
	ID                   string
	InvoiceID            string
	ReductionMinor       int64
	PriorObligationMinor int64
	NewObligationMinor   int64
	Reason               string
	RequestKey           string
}

type CreditState struct {
	ID                string
	SourceInvoiceID   string
	SourceOperationID string
	Balance           CreditBalance
}

type CreditApplicationState struct {
	ID          string
	GrantID     string
	InvoiceID   string
	AmountMinor int64
	RequestKey  string
}

type OutboxState struct {
	ID       string
	Kind     string
	ObjectID string
}

type ProviderState struct {
	Captures []ProviderCaptureState
	Refunds  []ProviderRefundState
}

type ProviderCaptureState struct {
	ProviderKey string
	AmountMinor int64
	Currency    string
	Status      string
}

type ProviderRefundState struct {
	ProviderKey      string
	SourceCaptureKey string
	AmountMinor      int64
	Currency         string
	Status           string
}

func walkStateRows(ctx context.Context, tx *sql.Tx, query string, scan func(*sql.Rows) error) error {
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := scan(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (l *Lab) State(ctx context.Context) (State, error) {
	tx, err := l.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return State{}, err
	}
	defer tx.Rollback()
	s := State{}
	if err := walkStateRows(ctx, tx, `SELECT p.id,p.plan_id,p.version,p.currency,p.fixed_amount_minor,COALESCE((SELECT amount_minor FROM price_components WHERE price_version_id=p.id AND component_code='seats'),0),p.publication_state,p.effective_from FROM price_versions p ORDER BY p.plan_id,p.version`, func(rows *sql.Rows) error {
		var x PriceVersionState
		var effective int64
		if err := rows.Scan(&x.ID, &x.PlanID, &x.Version, &x.Currency, &x.FixedMinor, &x.SeatMinor, &x.State, &effective); err != nil {
			return err
		}
		x.EffectiveFrom = time.Unix(0, effective).UTC()
		s.Prices = append(s.Prices, x)
		return nil
	}); err != nil {
		return State{}, err
	}
	if err := walkStateRows(ctx, tx, `SELECT plan_id,cohort,price_version_id,effective_at FROM catalog_selection ORDER BY plan_id,cohort,effective_at`, func(rows *sql.Rows) error {
		var x CatalogSelectionState
		var effective int64
		if err := rows.Scan(&x.PlanID, &x.Cohort, &x.PriceVersionID, &effective); err != nil {
			return err
		}
		x.EffectiveAt = time.Unix(0, effective).UTC()
		s.CatalogSelections = append(s.CatalogSelections, x)
		return nil
	}); err != nil {
		return State{}, err
	}
	var migrationIDs []string
	if err := walkStateRows(ctx, tx, `SELECT id FROM price_migrations ORDER BY created_at,id`, func(rows *sql.Rows) error {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		migrationIDs = append(migrationIDs, id)
		return nil
	}); err != nil {
		return State{}, err
	}
	for _, id := range migrationIDs {
		x, err := loadPriceMigration(ctx, tx, id)
		if err != nil {
			return State{}, err
		}
		s.PriceMigrations = append(s.PriceMigrations, x)
	}
	var contractIDs []string
	if err := walkStateRows(ctx, tx, `SELECT id FROM contract_versions ORDER BY customer_id,version`, func(rows *sql.Rows) error {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		contractIDs = append(contractIDs, id)
		return nil
	}); err != nil {
		return State{}, err
	}
	for _, id := range contractIDs {
		c, err := loadContract(ctx, tx, id)
		if err != nil {
			return State{}, err
		}
		s.Contracts = append(s.Contracts, c)
	}
	if err := walkStateRows(ctx, tx, `SELECT s.subscription_id,s.contract_version_id,EXISTS(SELECT 1 FROM contract_transitions t WHERE t.subscription_id=s.subscription_id) FROM contract_subscriptions s ORDER BY s.subscription_id`, func(rows *sql.Rows) error {
		var x ContractSubscriptionState
		if err := rows.Scan(&x.SubscriptionID, &x.ContractVersionID, &x.PostTransitioned); err != nil {
			return err
		}
		s.ContractSubscriptions = append(s.ContractSubscriptions, x)
		return nil
	}); err != nil {
		return State{}, err
	}
	if err := walkStateRows(ctx, tx, `SELECT tenant_id,source,event_id,subscription_id,meter_id,quantity,event_at,received_at,period_index,price_version_id FROM usage_events ORDER BY received_at,event_id`, func(rows *sql.Rows) error {
		var x UsageEvent
		var occurred, received int64
		if err := rows.Scan(&x.TenantID, &x.Source, &x.EventID, &x.SubscriptionID, &x.MeterID, &x.Quantity, &occurred, &received, &x.PeriodIndex, &x.PriceVersionID); err != nil {
			return err
		}
		x.EventAt = time.Unix(0, occurred).UTC()
		x.ReceivedAt = time.Unix(0, received).UTC()
		s.UsageEvents = append(s.UsageEvents, x)
		return nil
	}); err != nil {
		return State{}, err
	}
	if err := walkStateRows(ctx, tx, `SELECT subscription_id,period_index,price_version_id,cutoff_at,closed_at,rated_minor,billed_minor,credited_minor,applied_count FROM usage_periods ORDER BY subscription_id,period_index`, func(rows *sql.Rows) error {
		var x UsagePeriodState
		var cutoff, closed int64
		if err := rows.Scan(&x.SubscriptionID, &x.PeriodIndex, &x.PriceVersionID, &cutoff, &closed, &x.RatedMinor, &x.BilledMinor, &x.CreditedMinor, &x.AppliedCount); err != nil {
			return err
		}
		x.CutoffAt = time.Unix(0, cutoff).UTC()
		x.ClosedAt = time.Unix(0, closed).UTC()
		s.UsagePeriods = append(s.UsagePeriods, x)
		return nil
	}); err != nil {
		return State{}, err
	}
	if err := walkStateRows(ctx, tx, `SELECT id,subscription_id,period_index,revision,price_version_id,quantity,included_quantity,overage_quantity,exact_minor_numerator,exact_minor_denominator,rounded_minor,delta_minor,rated_at FROM usage_ratings ORDER BY subscription_id,period_index,revision`, func(rows *sql.Rows) error {
		var x UsageRating
		var rated int64
		if err := rows.Scan(&x.ID, &x.SubscriptionID, &x.PeriodIndex, &x.Revision, &x.PriceVersionID, &x.Quantity, &x.IncludedQuantity, &x.OverageQuantity, &x.ExactMinorNumerator, &x.ExactMinorDenominator, &x.RoundedMinor, &x.DeltaMinor, &rated); err != nil {
			return err
		}
		x.RatedAt = time.Unix(0, rated).UTC()
		s.UsageRatings = append(s.UsageRatings, x)
		return nil
	}); err != nil {
		return State{}, err
	}
	if err := walkStateRows(ctx, tx, `SELECT id,subscription_id,period_index,source_invoice_id,amount_minor,correction_id FROM usage_credit_notes ORDER BY created_at,id`, func(rows *sql.Rows) error {
		var x UsageCreditNoteState
		if err := rows.Scan(&x.ID, &x.SubscriptionID, &x.PeriodIndex, &x.SourceInvoiceID, &x.AmountMinor, &x.CorrectionID); err != nil {
			return err
		}
		s.UsageCreditNotes = append(s.UsageCreditNotes, x)
		return nil
	}); err != nil {
		return State{}, err
	}
	if err := walkStateRows(ctx, tx, `SELECT q.id,q.customer_id,q.price_version_id,q.amount_minor,q.currency,q.expires_at,q.fingerprint,q.seat_quantity,COALESCE(c.contract_version_id,''),EXISTS(SELECT 1 FROM subscriptions WHERE quote_id=q.id) FROM quotes q LEFT JOIN contract_quotes c ON c.quote_id=q.id ORDER BY q.rowid`, func(rows *sql.Rows) error {
		var q QuoteState
		var expires int64
		var accepted int
		if err := rows.Scan(&q.ID, &q.CustomerID, &q.PriceVersionID, &q.AmountMinor, &q.Currency, &expires, &q.Fingerprint, &q.SeatQuantity, &q.ContractVersionID, &accepted); err != nil {
			return err
		}
		q.ExpiresAt = time.Unix(0, expires).UTC()
		q.Accepted = accepted != 0
		s.Quotes = append(s.Quotes, q)
		return nil
	}); err != nil {
		return State{}, err
	}
	if err := walkStateRows(ctx, tx, `SELECT s.id,s.customer_id,s.price_version_id,s.seat_quantity,s.revision,CASE WHEN EXISTS(SELECT 1 FROM subscription_ends se WHERE se.subscription_id=s.id) THEN 'ended' ELSE s.status END,COALESCE(e.status,''),COALESCE(e.reason,''),COALESCE(e.source_revision,0) FROM subscriptions s LEFT JOIN entitlements e ON e.subscription_id=s.id ORDER BY s.created_at,s.id`, func(rows *sql.Rows) error {
		var x SubscriptionState
		if err := rows.Scan(&x.ID, &x.CustomerID, &x.PriceVersionID, &x.SeatQuantity, &x.Revision, &x.Status, &x.EntitlementStatus, &x.EntitlementReason, &x.EntitlementSourceRevision); err != nil {
			return err
		}
		s.Subscriptions = append(s.Subscriptions, x)
		return nil
	}); err != nil {
		return State{}, err
	}
	if err := walkStateRows(ctx, tx, `SELECT id,subscription_id,kind,COALESCE(target_price_version_id,''),seat_quantity,effective_at,status,created_revision+1 FROM subscription_schedules ORDER BY rowid`, func(rows *sql.Rows) error {
		var x ScheduleResult
		var effective int64
		if err := rows.Scan(&x.ID, &x.SubscriptionID, &x.Kind, &x.TargetPriceVersionID, &x.SeatQuantity, &effective, &x.Status, &x.Revision); err != nil {
			return err
		}
		x.EffectiveAt = time.Unix(0, effective).UTC()
		s.Schedules = append(s.Schedules, x)
		return nil
	}); err != nil {
		return State{}, err
	}
	if err := walkStateRows(ctx, tx, `SELECT c.id,c.subscription_id,c.invoice_id,c.operation_id,c.target_price_version_id,c.seat_quantity,c.old_credit_minor,c.new_charge_minor,c.quoted_amount_minor,c.actual_amount_minor,c.correction_minor,c.status,c.requested_at,c.activated_at,EXISTS(SELECT 1 FROM immediate_change_resolutions r WHERE r.change_id=c.id) FROM immediate_changes c ORDER BY c.requested_at,c.id`, func(rows *sql.Rows) error {
		var x ImmediateChange
		var requested int64
		var activated sql.NullInt64
		if err := rows.Scan(&x.ID, &x.SubscriptionID, &x.InvoiceID, &x.OperationID, &x.TargetPriceVersionID, &x.SeatQuantity, &x.OldCreditMinor, &x.NewChargeMinor, &x.QuotedAmountMinor, &x.ActualAmountMinor, &x.CorrectionMinor, &x.Status, &requested, &activated, &x.Resolved); err != nil {
			return err
		}
		x.RequestedAt = time.Unix(0, requested).UTC()
		if activated.Valid {
			value := time.Unix(0, activated.Int64).UTC()
			x.ActivatedAt = &value
		}
		s.ImmediateChanges = append(s.ImmediateChanges, x)
		return nil
	}); err != nil {
		return State{}, err
	}
	if err := walkStateRows(ctx, tx, `SELECT subscription_id,assignment_index,price_version_id,seat_quantity,effective_start,effective_end,COALESCE(source_schedule_id,'') FROM pricing_assignments ORDER BY subscription_id,assignment_index`, func(rows *sql.Rows) error {
		var x PricingAssignmentState
		var start int64
		var end sql.NullInt64
		if err := rows.Scan(&x.SubscriptionID, &x.Index, &x.PriceVersionID, &x.SeatQuantity, &start, &end, &x.SourceScheduleID); err != nil {
			return err
		}
		x.EffectiveStart = time.Unix(0, start).UTC()
		if end.Valid {
			value := time.Unix(0, end.Int64).UTC()
			x.EffectiveEnd = &value
		}
		s.Assignments = append(s.Assignments, x)
		return nil
	}); err != nil {
		return State{}, err
	}
	if err := walkStateRows(ctx, tx, `SELECT subscription_id,period_index,period_start,period_end,due_at,invoice_id FROM billing_periods ORDER BY subscription_id,period_index`, func(rows *sql.Rows) error {
		var x PeriodState
		var start, end, due int64
		if err := rows.Scan(&x.SubscriptionID, &x.Index, &start, &end, &due, &x.InvoiceID); err != nil {
			return err
		}
		x.Start, x.End, x.DueAt = time.Unix(0, start).UTC(), time.Unix(0, end).UTC(), time.Unix(0, due).UTC()
		s.Periods = append(s.Periods, x)
		return nil
	}); err != nil {
		return State{}, err
	}
	if err := walkStateRows(ctx, tx, `SELECT i.id,i.subscription_id,COALESCE(p.period_index,-1) FROM invoices i LEFT JOIN billing_periods p ON p.invoice_id=i.id ORDER BY i.finalized_at,i.id`, func(rows *sql.Rows) error {
		var x InvoiceState
		if err := rows.Scan(&x.ID, &x.SubscriptionID, &x.PeriodIndex); err != nil {
			return err
		}
		s.Invoices = append(s.Invoices, x)
		return nil
	}); err != nil {
		return State{}, err
	}
	for i := range s.Invoices {
		balance, err := loadInvoiceBalance(ctx, tx, s.Invoices[i].ID)
		if err != nil {
			return State{}, err
		}
		s.Invoices[i].Balance = balance
	}
	if err := walkStateRows(ctx, tx, `SELECT o.id,o.invoice_id,o.provider_key,o.amount_minor,o.currency,o.status,COALESCE((SELECT key FROM payment_requests WHERE operation_id=o.id),(SELECT key FROM payment_retry_requests WHERE operation_id=o.id),'') FROM payment_operations o ORDER BY o.rowid`, func(rows *sql.Rows) error {
		var x PaymentState
		if err := rows.Scan(&x.ID, &x.InvoiceID, &x.ProviderKey, &x.AmountMinor, &x.Currency, &x.Status, &x.RequestKey); err != nil {
			return err
		}
		s.Payments = append(s.Payments, x)
		return nil
	}); err != nil {
		return State{}, err
	}
	if err := walkStateRows(ctx, tx, `SELECT id,invoice_id,reduction_minor,prior_obligation_minor,new_obligation_minor,reason,request_key FROM corrections ORDER BY rowid`, func(rows *sql.Rows) error {
		var x CorrectionState
		if err := rows.Scan(&x.ID, &x.InvoiceID, &x.ReductionMinor, &x.PriorObligationMinor, &x.NewObligationMinor, &x.Reason, &x.RequestKey); err != nil {
			return err
		}
		s.Corrections = append(s.Corrections, x)
		return nil
	}); err != nil {
		return State{}, err
	}
	if err := walkStateRows(ctx, tx, `SELECT id,source_invoice_id,source_operation_id FROM credit_grants ORDER BY rowid`, func(rows *sql.Rows) error {
		var x CreditState
		if err := rows.Scan(&x.ID, &x.SourceInvoiceID, &x.SourceOperationID); err != nil {
			return err
		}
		s.Credits = append(s.Credits, x)
		return nil
	}); err != nil {
		return State{}, err
	}
	for i := range s.Credits {
		balance, err := loadCreditBalance(ctx, tx, s.Credits[i].ID)
		if err != nil {
			return State{}, err
		}
		s.Credits[i].Balance = balance
	}
	if err := walkStateRows(ctx, tx, `SELECT id,grant_id,invoice_id,amount_minor,request_key FROM credit_applications ORDER BY rowid`, func(rows *sql.Rows) error {
		var x CreditApplicationState
		if err := rows.Scan(&x.ID, &x.GrantID, &x.InvoiceID, &x.AmountMinor, &x.RequestKey); err != nil {
			return err
		}
		s.CreditApplications = append(s.CreditApplications, x)
		return nil
	}); err != nil {
		return State{}, err
	}
	if err := walkStateRows(ctx, tx, `SELECT id,grant_id,amount_minor,currency,status FROM refund_operations ORDER BY rowid`, func(rows *sql.Rows) error {
		var x RefundInfo
		if err := rows.Scan(&x.ID, &x.GrantID, &x.AmountMinor, &x.Currency, &x.Status); err != nil {
			return err
		}
		s.Refunds = append(s.Refunds, x)
		return nil
	}); err != nil {
		return State{}, err
	}
	if err := walkStateRows(ctx, tx, `SELECT id,kind,object_id FROM outbox WHERE status='pending' ORDER BY rowid`, func(rows *sql.Rows) error {
		var x OutboxState
		if err := rows.Scan(&x.ID, &x.Kind, &x.ObjectID); err != nil {
			return err
		}
		s.PendingOutbox = append(s.PendingOutbox, x)
		return nil
	}); err != nil {
		return State{}, err
	}
	if err := tx.Commit(); err != nil {
		return State{}, err
	}
	return s, nil
}

// ProviderState reads the independent fake provider database. Its observation
// cannot be atomic with State's Commerce snapshot.
func (l *Lab) ProviderState(ctx context.Context) (ProviderState, error) {
	tx, err := l.provider.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return ProviderState{}, err
	}
	defer tx.Rollback()
	s := ProviderState{}
	if err := walkStateRows(ctx, tx, `SELECT provider_key,amount_minor,currency,status FROM captures ORDER BY rowid`, func(rows *sql.Rows) error {
		var x ProviderCaptureState
		if err := rows.Scan(&x.ProviderKey, &x.AmountMinor, &x.Currency, &x.Status); err != nil {
			return err
		}
		s.Captures = append(s.Captures, x)
		return nil
	}); err != nil {
		return ProviderState{}, err
	}
	if err := walkStateRows(ctx, tx, `SELECT provider_key,source_capture_key,amount_minor,currency,status FROM refunds ORDER BY rowid`, func(rows *sql.Rows) error {
		var x ProviderRefundState
		if err := rows.Scan(&x.ProviderKey, &x.SourceCaptureKey, &x.AmountMinor, &x.Currency, &x.Status); err != nil {
			return err
		}
		s.Refunds = append(s.Refunds, x)
		return nil
	}); err != nil {
		return ProviderState{}, err
	}
	if err := tx.Commit(); err != nil {
		return ProviderState{}, err
	}
	return s, nil
}
