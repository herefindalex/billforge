package lab

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// The provider receipt and decision share a provider transaction. The commerce
// command can be completed later from that receipt after an interrupted run.
func (p *FakeProvider) adminSetDecision(ctx context.Context, commandID, kind, key, status string) error {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	intentHash := hash(kind, key, status)
	var priorHash, priorKey string
	err = tx.QueryRowContext(ctx, `SELECT payload_hash,target_key FROM provider_control_receipts WHERE command_id=?`, commandID).Scan(&priorHash, &priorKey)
	if err == nil {
		if priorHash != intentHash || priorKey != key {
			return ErrConflict
		}
		return tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	completedTable, decisionTable := "captures", "decisions"
	if kind == "refund" {
		completedTable, decisionTable = "refunds", "refund_decisions"
	}
	var existing string
	err = tx.QueryRowContext(ctx, `SELECT status FROM `+completedTable+` WHERE provider_key=?`, key).Scan(&existing)
	if err == nil {
		return ErrConflict
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	err = tx.QueryRowContext(ctx, `SELECT status FROM `+decisionTable+` WHERE provider_key=?`, key).Scan(&existing)
	if err == nil && existing != status {
		return ErrConflict
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO `+decisionTable+`(provider_key,status) VALUES(?,?) ON CONFLICT(provider_key) DO NOTHING`, key, status); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO provider_control_receipts(command_id,payload_hash,target_key,result,committed_at) VALUES(?,?,?,?,?)`, commandID, intentHash, key, status, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	return tx.Commit()
}

func (l *Lab) adminExecuteProviderControl(ctx context.Context, commandID string) (AdminCommand, error) {
	var actionID, actorID, targetID, status, raw string
	if err := l.db.QueryRowContext(ctx, `SELECT action_id,actor_id,target_id,status,payload_json FROM admin_commands WHERE id=?`, commandID).Scan(&actionID, &actorID, &targetID, &status, &raw); err != nil {
		return AdminCommand{}, err
	}
	if status == "succeeded" || status == "failed" {
		return l.AdminCommand(ctx, commandID)
	}
	if status != "accepted" {
		return AdminCommand{}, ErrConflict
	}
	var payload adminDecisionPayload
	if err := decodeAdminPayload(json.RawMessage(raw), &payload); err != nil {
		return AdminCommand{}, err
	}
	kind, table := "payment", "payment_operations"
	if actionID == "C48" {
		kind, table = "refund", "refund_operations"
	}
	var key, operationStatus string
	err := l.db.QueryRowContext(ctx, `SELECT provider_key,status FROM `+table+` WHERE id=?`, targetID).Scan(&key, &operationStatus)
	if errors.Is(err, sql.ErrNoRows) {
		if err := l.adminFailCommand(ctx, commandID, actorID, actionID, targetID, "DOMAIN_REJECTED"); err != nil {
			return AdminCommand{}, err
		}
		return l.AdminCommand(ctx, commandID)
	}
	if err != nil {
		return AdminCommand{}, err
	}
	var priorReceipt string
	receiptErr := l.provider.db.QueryRowContext(ctx, `SELECT target_key FROM provider_control_receipts WHERE command_id=?`, commandID).Scan(&priorReceipt)
	if receiptErr != nil && !errors.Is(receiptErr, sql.ErrNoRows) {
		return AdminCommand{}, receiptErr
	}
	if operationStatus != "created" && errors.Is(receiptErr, sql.ErrNoRows) {
		if err := l.adminFailCommand(ctx, commandID, actorID, actionID, targetID, "DOMAIN_REJECTED"); err != nil {
			return AdminCommand{}, err
		}
		return l.AdminCommand(ctx, commandID)
	}
	if err := l.adminRenewLease(ctx, commandID); err != nil {
		return AdminCommand{}, err
	}
	if err := l.provider.adminSetDecision(ctx, commandID, kind, key, payload.Status); err != nil {
		if errors.Is(err, ErrConflict) {
			if err := l.adminFailCommand(ctx, commandID, actorID, actionID, targetID, "DOMAIN_REJECTED"); err != nil {
				return AdminCommand{}, err
			}
			return l.AdminCommand(ctx, commandID)
		}
		return AdminCommand{}, err
	}
	return l.adminFinishExternalCommand(ctx, commandID, map[string]string{"operation_kind": kind, "operation_id": targetID, "status": payload.Status})
}
