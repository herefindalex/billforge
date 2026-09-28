package lab

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"time"
)

type reserveRefundSnapshot struct {
	Currency string
	Sources  []byte
	Impact   []byte
}

func (l *Lab) loadReserveRefundSnapshot(ctx context.Context, tx *sql.Tx, grantID string, amountMinor int64) (reserveRefundSnapshot, error) {
	var snapshot reserveRefundSnapshot
	credit, err := loadCreditBalance(ctx, tx, grantID)
	if err != nil {
		return snapshot, err
	}
	if amountMinor <= 0 || amountMinor > credit.AvailableMinor {
		return snapshot, ErrConflict
	}
	var sourceOperation, sourceInvoice, sourceStatus string
	if err := tx.QueryRowContext(ctx, `SELECT g.source_operation_id,g.source_invoice_id,o.status FROM credit_grants g JOIN payment_operations o ON o.id=g.source_operation_id WHERE g.id=?`, grantID).Scan(&sourceOperation, &sourceInvoice, &sourceStatus); err != nil {
		return snapshot, err
	}
	if sourceStatus != "succeeded" {
		return snapshot, ErrConflict
	}
	snapshot.Currency = credit.Currency
	snapshot.Sources, err = json.Marshal(map[string]string{
		"grant_id": grantID, "source_operation_id": sourceOperation, "source_invoice_id": sourceInvoice,
		"source_operation_status": sourceStatus, "granted_minor": strconv.FormatInt(credit.GrantedMinor, 10),
		"applied_minor": strconv.FormatInt(credit.AppliedMinor, 10), "reserved_minor": strconv.FormatInt(credit.ReservedMinor, 10),
		"refunded_minor": strconv.FormatInt(credit.RefundedMinor, 10), "available_minor": strconv.FormatInt(credit.AvailableMinor, 10),
		"currency": credit.Currency,
	})
	if err != nil {
		return reserveRefundSnapshot{}, err
	}
	snapshot.Impact, err = json.Marshal(map[string]string{
		"grant_id": grantID, "refund_amount_minor": strconv.FormatInt(amountMinor, 10),
		"available_before_minor": strconv.FormatInt(credit.AvailableMinor, 10),
		"available_after_minor":  strconv.FormatInt(credit.AvailableMinor-amountMinor, 10),
		"currency":               credit.Currency,
	})
	return snapshot, err
}

func (l *Lab) adminCreateReserveRefundPreview(ctx context.Context, actorID, grantID string, canonical []byte) (AdminPreview, error) {
	if actorID == "" || grantID == "" {
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
	snapshot, err := l.loadReserveRefundSnapshot(ctx, tx, grantID, amount)
	if err != nil {
		return AdminPreview{}, err
	}
	created := time.Now().UTC()
	expires := created.Add(5 * time.Minute).Format(time.RFC3339Nano)
	id, err := newID("prev_")
	if err != nil {
		return AdminPreview{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO admin_previews(id,actor_id,action_id,target_id,intent_hash,intent_json,source_versions_json,impact_json,expires_at,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, id, actorID, "C15", grantID, adminIntentHash("C15", grantID, canonical), string(canonical), string(snapshot.Sources), string(snapshot.Impact), expires, created.Format(time.RFC3339Nano))
	if err != nil {
		return AdminPreview{}, err
	}
	if err := tx.Commit(); err != nil {
		return AdminPreview{}, err
	}
	return AdminPreview{ID: id, ActorID: actorID, ActionID: "C15", TargetID: grantID, ExpiresAt: expires, SourceVersions: snapshot.Sources, Impact: snapshot.Impact, BlockingReasons: []string{}}, nil
}

func (l *Lab) executeReserveRefundTx(ctx context.Context, tx *sql.Tx, commandID, previewID, grantID, payloadJSON, businessTime string) (any, error) {
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
	snapshot, err := l.loadReserveRefundSnapshot(ctx, tx, grantID, amount)
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
	refundID, err := l.reserveRefundTx(ctx, tx, at, grantID, amount, "admin:"+commandID)
	if errors.Is(err, ErrConflict) || errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAdminPreviewStale
	}
	if err != nil {
		return nil, err
	}
	return map[string]string{"refund_id": refundID, "grant_id": grantID, "amount_minor": input.AmountMinor, "currency": snapshot.Currency}, nil
}
