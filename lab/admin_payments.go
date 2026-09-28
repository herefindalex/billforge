package lab

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"time"
)

type createPaymentSnapshot struct {
	Currency string
	Sources  []byte
	Impact   []byte
}

func (l *Lab) loadCreatePaymentSnapshot(ctx context.Context, tx *sql.Tx, at time.Time, invoiceID string, amountMinor int64) (createPaymentSnapshot, error) {
	var snapshot createPaymentSnapshot
	periodIndex, periodEnd, dueAt, err := loadPayablePeriod(ctx, tx, invoiceID)
	if err != nil {
		return snapshot, err
	}
	if err := ensureImmediateChangePayable(ctx, tx, invoiceID); err != nil {
		return snapshot, err
	}
	contractInvoice, err := isContractInvoice(ctx, tx, invoiceID)
	if err != nil {
		return snapshot, err
	}
	if contractInvoice && at.UnixNano() < dueAt {
		return snapshot, ErrConflict
	}
	if !contractInvoice && at.UnixNano() >= periodEnd {
		return snapshot, ErrPeriodEnded
	}
	if !contractInvoice && periodIndex > 0 && at.UnixNano() > time.Unix(0, dueAt).Add(renewalGrace).UnixNano() {
		return snapshot, ErrLateNeedsReview
	}
	var activeOps, callerCreated int
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(CASE WHEN status IN ('submitted','unknown') THEN 1 ELSE 0 END),0),COALESCE(SUM(CASE WHEN status='created' AND EXISTS(SELECT 1 FROM payment_requests p WHERE p.operation_id=o.id) THEN 1 ELSE 0 END),0) FROM payment_operations o WHERE invoice_id=?`, invoiceID).Scan(&activeOps, &callerCreated)
	if err != nil {
		return snapshot, err
	}
	if activeOps != 0 || callerCreated != 0 {
		return snapshot, ErrConflict
	}
	balance, err := loadInvoiceBalance(ctx, tx, invoiceID)
	if err != nil {
		return snapshot, err
	}
	if amountMinor <= 0 || amountMinor > balance.OutstandingMinor {
		return snapshot, ErrConflict
	}
	var states string
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(GROUP_CONCAT(id||':'||status,'|'),'') FROM (SELECT id,status FROM payment_operations WHERE invoice_id=? ORDER BY id)`, invoiceID).Scan(&states)
	if err != nil {
		return snapshot, err
	}
	snapshot.Currency = balance.Currency
	snapshot.Sources, err = json.Marshal(map[string]string{
		"invoice_id": invoiceID, "period_index": strconv.Itoa(periodIndex), "period_end": strconv.FormatInt(periodEnd, 10),
		"due_at": strconv.FormatInt(dueAt, 10), "outstanding_minor": strconv.FormatInt(balance.OutstandingMinor, 10),
		"currency": balance.Currency, "operation_states": states,
	})
	if err != nil {
		return createPaymentSnapshot{}, err
	}
	snapshot.Impact, err = json.Marshal(map[string]string{
		"invoice_id": invoiceID, "amount_minor": strconv.FormatInt(amountMinor, 10),
		"outstanding_before_minor": strconv.FormatInt(balance.OutstandingMinor, 10), "currency": balance.Currency,
	})
	return snapshot, err
}

func (l *Lab) adminCreatePaymentPreview(ctx context.Context, actorID, invoiceID string, canonical []byte) (AdminPreview, error) {
	if actorID == "" || invoiceID == "" {
		return AdminPreview{}, ErrAdminInvalidCommand
	}
	var input AdminAmountPayload
	if err := decodeAdminPayload(canonical, &input); err != nil {
		return AdminPreview{}, err
	}
	amount, err := strconv.ParseInt(input.AmountMinor, 10, 64)
	if err != nil {
		return AdminPreview{}, err
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return AdminPreview{}, err
	}
	defer tx.Rollback()
	snapshot, err := l.loadCreatePaymentSnapshot(ctx, tx, l.now().UTC(), invoiceID, amount)
	if err != nil {
		return AdminPreview{}, err
	}
	created := time.Now().UTC()
	expires := created.Add(5 * time.Minute).Format(time.RFC3339Nano)
	id, err := newID("prev_")
	if err != nil {
		return AdminPreview{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO admin_previews(id,actor_id,action_id,target_id,intent_hash,intent_json,source_versions_json,impact_json,expires_at,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, id, actorID, "C07", invoiceID, adminIntentHash("C07", invoiceID, canonical), string(canonical), string(snapshot.Sources), string(snapshot.Impact), expires, created.Format(time.RFC3339Nano))
	if err != nil {
		return AdminPreview{}, err
	}
	if err := tx.Commit(); err != nil {
		return AdminPreview{}, err
	}
	return AdminPreview{ID: id, ActorID: actorID, ActionID: "C07", TargetID: invoiceID, ExpiresAt: expires, SourceVersions: snapshot.Sources, Impact: snapshot.Impact, BlockingReasons: []string{}}, nil
}

func (l *Lab) executeCreatePaymentTx(ctx context.Context, tx *sql.Tx, commandID, previewID, invoiceID, payloadJSON, businessTime string) (any, error) {
	var input AdminAmountPayload
	if err := decodeAdminPayload(json.RawMessage(payloadJSON), &input); err != nil {
		return nil, err
	}
	amount, err := strconv.ParseInt(input.AmountMinor, 10, 64)
	if err != nil {
		return nil, err
	}
	var storedSources string
	if err := tx.QueryRowContext(ctx, `SELECT source_versions_json FROM admin_previews WHERE id=? AND claimed_command_id=?`, previewID, commandID).Scan(&storedSources); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrAdminPreviewStale
		}
		return nil, err
	}
	at, err := time.Parse(time.RFC3339Nano, businessTime)
	if err != nil {
		return nil, err
	}
	snapshot, err := l.loadCreatePaymentSnapshot(ctx, tx, at, invoiceID, amount)
	if errors.Is(err, ErrConflict) || errors.Is(err, sql.ErrNoRows) || errors.Is(err, ErrPeriodEnded) || errors.Is(err, ErrLateNeedsReview) {
		return nil, ErrAdminPreviewStale
	}
	if err != nil {
		return nil, err
	}
	if storedSources != string(snapshot.Sources) {
		return nil, ErrAdminPreviewStale
	}
	opID, err := l.createPaymentTx(ctx, tx, at, invoiceID, amount, "admin:"+commandID)
	if errors.Is(err, ErrConflict) || errors.Is(err, sql.ErrNoRows) || errors.Is(err, ErrPeriodEnded) || errors.Is(err, ErrLateNeedsReview) {
		return nil, ErrAdminPreviewStale
	}
	if err != nil {
		return nil, err
	}
	return map[string]string{"operation_id": opID, "invoice_id": invoiceID, "amount_minor": input.AmountMinor, "currency": snapshot.Currency}, nil
}

type retryPaymentSnapshot struct {
	InvoiceID   string
	AmountMinor int64
	Currency    string
	Sources     []byte
	Impact      []byte
}

func (l *Lab) loadRetryPaymentSnapshot(ctx context.Context, tx *sql.Tx, at time.Time, operationID string) (retryPaymentSnapshot, error) {
	var snapshot retryPaymentSnapshot
	var status string
	if err := tx.QueryRowContext(ctx, `SELECT invoice_id,status FROM payment_operations WHERE id=?`, operationID).Scan(&snapshot.InvoiceID, &status); err != nil {
		return snapshot, err
	}
	if status != "definitively_failed" {
		return snapshot, ErrConflict
	}
	var failed, unresolved int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(CASE WHEN status='definitively_failed' THEN 1 ELSE 0 END),0),COALESCE(SUM(CASE WHEN status NOT IN ('definitively_failed','cancelled') THEN 1 ELSE 0 END),0) FROM payment_operations WHERE invoice_id=?`, snapshot.InvoiceID).Scan(&failed, &unresolved); err != nil {
		return snapshot, err
	}
	if failed == 0 || unresolved != 0 {
		return snapshot, ErrConflict
	}
	balance, err := loadInvoiceBalance(ctx, tx, snapshot.InvoiceID)
	if err != nil {
		return snapshot, err
	}
	if balance.OutstandingMinor <= 0 {
		return snapshot, ErrConflict
	}
	base, err := l.loadCreatePaymentSnapshot(ctx, tx, at, snapshot.InvoiceID, balance.OutstandingMinor)
	if err != nil {
		return snapshot, err
	}
	var source map[string]string
	if err := json.Unmarshal(base.Sources, &source); err != nil {
		return snapshot, err
	}
	source["failed_operation_id"] = operationID
	source["failed_operation_status"] = status
	snapshot.Sources, err = json.Marshal(source)
	if err != nil {
		return snapshot, err
	}
	snapshot.AmountMinor, snapshot.Currency = balance.OutstandingMinor, balance.Currency
	snapshot.Impact, err = json.Marshal(map[string]string{
		"failed_operation_id": operationID, "invoice_id": snapshot.InvoiceID,
		"retry_amount_minor": strconv.FormatInt(snapshot.AmountMinor, 10), "currency": snapshot.Currency,
	})
	return snapshot, err
}

func (l *Lab) adminCreateRetryPaymentPreview(ctx context.Context, actorID, operationID string, canonical []byte) (AdminPreview, error) {
	if actorID == "" || operationID == "" {
		return AdminPreview{}, ErrAdminInvalidCommand
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return AdminPreview{}, err
	}
	defer tx.Rollback()
	snapshot, err := l.loadRetryPaymentSnapshot(ctx, tx, l.now().UTC(), operationID)
	if err != nil {
		return AdminPreview{}, err
	}
	created := time.Now().UTC()
	expires := created.Add(5 * time.Minute).Format(time.RFC3339Nano)
	id, err := newID("prev_")
	if err != nil {
		return AdminPreview{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO admin_previews(id,actor_id,action_id,target_id,intent_hash,intent_json,source_versions_json,impact_json,expires_at,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, id, actorID, "C08", operationID, adminIntentHash("C08", operationID, canonical), string(canonical), string(snapshot.Sources), string(snapshot.Impact), expires, created.Format(time.RFC3339Nano))
	if err != nil {
		return AdminPreview{}, err
	}
	if err := tx.Commit(); err != nil {
		return AdminPreview{}, err
	}
	return AdminPreview{ID: id, ActorID: actorID, ActionID: "C08", TargetID: operationID, ExpiresAt: expires, SourceVersions: snapshot.Sources, Impact: snapshot.Impact, BlockingReasons: []string{}}, nil
}

func (l *Lab) executeRetryPaymentTx(ctx context.Context, tx *sql.Tx, commandID, previewID, operationID, businessTime string) (any, error) {
	var storedSources string
	if err := tx.QueryRowContext(ctx, `SELECT source_versions_json FROM admin_previews WHERE id=? AND claimed_command_id=?`, previewID, commandID).Scan(&storedSources); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrAdminPreviewStale
		}
		return nil, err
	}
	at, err := time.Parse(time.RFC3339Nano, businessTime)
	if err != nil {
		return nil, err
	}
	snapshot, err := l.loadRetryPaymentSnapshot(ctx, tx, at, operationID)
	if errors.Is(err, ErrConflict) || errors.Is(err, sql.ErrNoRows) || errors.Is(err, ErrPeriodEnded) || errors.Is(err, ErrLateNeedsReview) {
		return nil, ErrAdminPreviewStale
	}
	if err != nil {
		return nil, err
	}
	if storedSources != string(snapshot.Sources) {
		return nil, ErrAdminPreviewStale
	}
	opID, err := l.retryFailedPaymentTx(ctx, tx, at, snapshot.InvoiceID, "admin:"+commandID)
	if errors.Is(err, ErrConflict) || errors.Is(err, sql.ErrNoRows) || errors.Is(err, ErrPeriodEnded) || errors.Is(err, ErrLateNeedsReview) {
		return nil, ErrAdminPreviewStale
	}
	if err != nil {
		return nil, err
	}
	return map[string]string{
		"operation_id": opID, "failed_operation_id": operationID, "invoice_id": snapshot.InvoiceID,
		"amount_minor": strconv.FormatInt(snapshot.AmountMinor, 10), "currency": snapshot.Currency,
	}, nil
}
