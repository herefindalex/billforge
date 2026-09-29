package lab

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"time"
)

func (l *Lab) adminCreateControlPreview(ctx context.Context, actorID, actionID, targetID string, canonical []byte) (AdminPreview, error) {
	if actorID == "" || (actionID == "C46" && targetID != "") || (actionID != "C46" && targetID == "") {
		return AdminPreview{}, ErrAdminInvalidCommand
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return AdminPreview{}, err
	}
	defer tx.Rollback()
	sources, impact, err := l.adminControlSnapshot(ctx, tx, actionID, targetID, canonical)
	if err != nil {
		return AdminPreview{}, err
	}
	created := time.Now().UTC()
	expires := created.Add(5 * time.Minute).Format(time.RFC3339Nano)
	id, err := newID("prev_")
	if err != nil {
		return AdminPreview{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO admin_previews(id,actor_id,action_id,target_id,intent_hash,intent_json,source_versions_json,impact_json,expires_at,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, id, actorID, actionID, targetID, adminIntentHash(actionID, targetID, canonical), string(canonical), string(sources), string(impact), expires, created.Format(time.RFC3339Nano))
	if err != nil {
		return AdminPreview{}, err
	}
	if err := tx.Commit(); err != nil {
		return AdminPreview{}, err
	}
	return AdminPreview{ID: id, ActorID: actorID, ActionID: actionID, TargetID: targetID, ExpiresAt: expires, SourceVersions: sources, Impact: impact, BlockingReasons: []string{}}, nil
}

func (l *Lab) adminControlSnapshot(ctx context.Context, tx *sql.Tx, actionID, targetID string, canonical []byte) (json.RawMessage, json.RawMessage, error) {
	var sources, impact map[string]string
	switch actionID {
	case "C46":
		var payload adminClockPayload
		if err := decodeAdminPayload(canonical, &payload); err != nil {
			return nil, nil, err
		}
		var mode, value string
		var revision int64
		err := tx.QueryRowContext(ctx, `SELECT mode,COALESCE(value_utc,''),revision FROM admin_lab_clock WHERE id=1`).Scan(&mode, &value, &revision)
		if errors.Is(err, sql.ErrNoRows) {
			mode = "real"
		} else if err != nil {
			return nil, nil, err
		}
		sources = map[string]string{"mode": mode, "value_utc": value, "revision": strconv.FormatInt(revision, 10)}
		impact = map[string]string{"current_mode": mode, "current_value_utc": value, "proposed_mode": payload.Mode, "proposed_value_utc": payload.ValueUTC}
	case "C47", "C48":
		var payload adminDecisionPayload
		if err := decodeAdminPayload(canonical, &payload); err != nil {
			return nil, nil, err
		}
		table, decisionTable, completedTable := "payment_operations", "decisions", "captures"
		if actionID == "C48" {
			table, decisionTable, completedTable = "refund_operations", "refund_decisions", "refunds"
		}
		var key, status string
		if err := tx.QueryRowContext(ctx, `SELECT provider_key,status FROM `+table+` WHERE id=?`, targetID).Scan(&key, &status); err != nil {
			return nil, nil, err
		}
		if status != "created" {
			return nil, nil, ErrConflict
		}
		var completed string
		err := l.provider.db.QueryRowContext(ctx, `SELECT status FROM `+completedTable+` WHERE provider_key=?`, key).Scan(&completed)
		if err == nil {
			return nil, nil, ErrConflict
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, nil, err
		}
		var existing string
		err = l.provider.db.QueryRowContext(ctx, `SELECT status FROM `+decisionTable+` WHERE provider_key=?`, key).Scan(&existing)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, nil, err
		}
		if existing != "" && existing != payload.Status {
			return nil, nil, ErrConflict
		}
		sources = map[string]string{"operation_status": status, "provider_key": key, "current_decision": existing}
		impact = map[string]string{"operation_id": targetID, "current_decision": existing, "proposed_decision": payload.Status}
	case "C49":
		var payload adminFaultPayload
		if err := decodeAdminPayload(canonical, &payload); err != nil {
			return nil, nil, err
		}
		table := "payment_operations"
		if payload.OperationKind == "refund" {
			table = "refund_operations"
		}
		var status string
		if err := tx.QueryRowContext(ctx, `SELECT status FROM `+table+` WHERE id=?`, targetID).Scan(&status); err != nil {
			return nil, nil, err
		}
		if status != "created" {
			return nil, nil, ErrConflict
		}
		var ticket string
		err := tx.QueryRowContext(ctx, `SELECT id FROM admin_fault_tickets WHERE operation_kind=? AND operation_id=?`, payload.OperationKind, targetID).Scan(&ticket)
		if err == nil {
			return nil, nil, ErrConflict
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, nil, err
		}
		sources = map[string]string{"operation_status": status, "operation_kind": payload.OperationKind, "fault_ticket": ""}
		impact = map[string]string{"operation_id": targetID, "operation_kind": payload.OperationKind, "fault_mode": payload.Mode}
	default:
		return nil, nil, ErrAdminUnsupportedAction
	}
	encodedSources, err := json.Marshal(sources)
	if err != nil {
		return nil, nil, err
	}
	encodedImpact, err := json.Marshal(impact)
	return encodedSources, encodedImpact, err
}

func (l *Lab) adminControlPreviewCurrentTx(ctx context.Context, tx *sql.Tx, commandID, previewID, actionID, targetID string, canonical []byte) error {
	if previewID == "" {
		return nil
	}
	var stored string
	if err := tx.QueryRowContext(ctx, `SELECT source_versions_json FROM admin_previews WHERE id=? AND claimed_command_id=?`, previewID, commandID).Scan(&stored); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrAdminPreviewStale
		}
		return err
	}
	current, _, err := l.adminControlSnapshot(ctx, tx, actionID, targetID, canonical)
	if errors.Is(err, ErrConflict) || errors.Is(err, sql.ErrNoRows) {
		return ErrAdminPreviewStale
	}
	if err != nil {
		return err
	}
	if stored != string(current) {
		return ErrAdminPreviewStale
	}
	return nil
}
