package lab

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"time"
)

type reductionSnapshot struct {
	Currency   string
	PriorMinor int64
	NewMinor   int64
	GrantMinor int64
	Sources    []byte
	Impact     []byte
}

func (l *Lab) loadReductionSnapshot(ctx context.Context, tx *sql.Tx, invoiceID string, reductionMinor int64, allowZero bool) (reductionSnapshot, error) {
	var snapshot reductionSnapshot
	var active int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM payment_operations WHERE invoice_id=? AND status IN ('submitted','unknown')`, invoiceID).Scan(&active); err != nil {
		return snapshot, err
	}
	if active != 0 {
		return snapshot, ErrConflict
	}
	balance, err := loadInvoiceBalance(ctx, tx, invoiceID)
	if err != nil {
		return snapshot, err
	}
	if reductionMinor <= 0 || reductionMinor > balance.ObligationMinor || (reductionMinor == balance.ObligationMinor && !allowZero) || balance.CreditAppliedMinor != 0 {
		return snapshot, ErrConflict
	}
	var operationStates, allocationStates string
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(GROUP_CONCAT(id||':'||status,'|'),'') FROM (SELECT id,status FROM payment_operations WHERE invoice_id=? ORDER BY id)`, invoiceID).Scan(&operationStates); err != nil {
		return snapshot, err
	}
	var createdPayments int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM payment_operations WHERE invoice_id=? AND status='created'`, invoiceID).Scan(&createdPayments); err != nil {
		return snapshot, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(GROUP_CONCAT(operation_id||':'||amount_minor||':'||released,'|'),'') FROM (SELECT a.operation_id,a.amount_minor,COALESCE((SELECT SUM(r.amount_minor) FROM allocation_releases r WHERE r.operation_id=a.operation_id),0) AS released FROM allocations a WHERE a.invoice_id=? ORDER BY a.rowid)`, invoiceID).Scan(&allocationStates); err != nil {
		return snapshot, err
	}
	snapshot.Currency = balance.Currency
	snapshot.PriorMinor = balance.ObligationMinor
	snapshot.NewMinor = balance.ObligationMinor - reductionMinor
	if balance.NetAppliedMinor > snapshot.NewMinor {
		snapshot.GrantMinor = balance.NetAppliedMinor - snapshot.NewMinor
	}
	outstandingAfter := snapshot.NewMinor - balance.NetAppliedMinor
	if outstandingAfter < 0 {
		outstandingAfter = 0
	}
	snapshot.Sources, err = json.Marshal(map[string]string{
		"invoice_id": invoiceID, "obligation_minor": strconv.FormatInt(balance.ObligationMinor, 10),
		"net_applied_minor": strconv.FormatInt(balance.NetAppliedMinor, 10), "credit_applied_minor": strconv.FormatInt(balance.CreditAppliedMinor, 10),
		"currency": balance.Currency, "operation_states": operationStates, "allocation_states": allocationStates,
	})
	if err != nil {
		return reductionSnapshot{}, err
	}
	snapshot.Impact, err = json.Marshal(map[string]string{
		"invoice_id": invoiceID, "reduction_minor": strconv.FormatInt(reductionMinor, 10),
		"prior_obligation_minor":          strconv.FormatInt(snapshot.PriorMinor, 10),
		"new_obligation_minor":            strconv.FormatInt(snapshot.NewMinor, 10),
		"credit_grant_minor":              strconv.FormatInt(snapshot.GrantMinor, 10),
		"invoice_outstanding_after_minor": strconv.FormatInt(outstandingAfter, 10),
		"created_payments_to_cancel":      strconv.Itoa(createdPayments),
		"currency":                        balance.Currency,
	})
	return snapshot, err
}

func (l *Lab) adminCreateReductionPreview(ctx context.Context, actorID, invoiceID string, canonical []byte) (AdminPreview, error) {
	if actorID == "" || invoiceID == "" {
		return AdminPreview{}, ErrAdminInvalidCommand
	}
	var input AdminReductionPayload
	if err := decodeAdminPayload(canonical, &input); err != nil {
		return AdminPreview{}, err
	}
	amount, err := strconv.ParseInt(input.ReductionMinor, 10, 64)
	if err != nil {
		return AdminPreview{}, err
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return AdminPreview{}, err
	}
	defer tx.Rollback()
	snapshot, err := l.loadReductionSnapshot(ctx, tx, invoiceID, amount, false)
	if err != nil {
		return AdminPreview{}, err
	}
	created := time.Now().UTC()
	expires := created.Add(5 * time.Minute).Format(time.RFC3339Nano)
	id, err := newID("prev_")
	if err != nil {
		return AdminPreview{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO admin_previews(id,actor_id,action_id,target_id,intent_hash,intent_json,source_versions_json,impact_json,expires_at,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, id, actorID, "C11", invoiceID, adminIntentHash("C11", invoiceID, canonical), string(canonical), string(snapshot.Sources), string(snapshot.Impact), expires, created.Format(time.RFC3339Nano))
	if err != nil {
		return AdminPreview{}, err
	}
	if err := tx.Commit(); err != nil {
		return AdminPreview{}, err
	}
	return AdminPreview{ID: id, ActorID: actorID, ActionID: "C11", TargetID: invoiceID, ExpiresAt: expires, SourceVersions: snapshot.Sources, Impact: snapshot.Impact, BlockingReasons: []string{}}, nil
}

func (l *Lab) executePostReductionTx(ctx context.Context, tx *sql.Tx, commandID, previewID, invoiceID, payloadJSON, businessTime string) (any, error) {
	var input AdminReductionPayload
	if err := decodeAdminPayload(json.RawMessage(payloadJSON), &input); err != nil {
		return nil, err
	}
	amount, err := strconv.ParseInt(input.ReductionMinor, 10, 64)
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
	snapshot, err := l.loadReductionSnapshot(ctx, tx, invoiceID, amount, false)
	if errors.Is(err, ErrConflict) || errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAdminPreviewStale
	}
	if err != nil {
		return nil, err
	}
	if storedSources != string(snapshot.Sources) {
		return nil, ErrAdminPreviewStale
	}
	at, err := time.Parse(time.RFC3339Nano, businessTime)
	if err != nil {
		return nil, err
	}
	result, err := l.postReductionTx(ctx, tx, at, invoiceID, amount, input.Reason, "admin:"+commandID, false)
	if errors.Is(err, ErrConflict) || errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAdminPreviewStale
	}
	if err != nil {
		return nil, err
	}
	if result.PriorObligationMinor != snapshot.PriorMinor || result.NewObligationMinor != snapshot.NewMinor {
		return nil, ErrAdminPreviewStale
	}
	return map[string]any{
		"correction_id": result.ID, "invoice_id": invoiceID, "reduction_minor": input.ReductionMinor,
		"prior_obligation_minor": strconv.FormatInt(result.PriorObligationMinor, 10),
		"new_obligation_minor":   strconv.FormatInt(result.NewObligationMinor, 10),
		"grant_ids":              result.GrantIDs, "currency": snapshot.Currency,
	}, nil
}
