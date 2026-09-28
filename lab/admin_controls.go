package lab

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

var ErrAdminFaultOwnedByOtherCommand = errors.New("admin fault ticket belongs to another command")

type AdminClockState struct {
	Mode         string `json:"mode"`
	ValueUTC     string `json:"value_utc,omitempty"`
	Revision     int64  `json:"revision"`
	BusinessTime string `json:"business_time"`
}

type AdminFaultTicket struct {
	ID               string `json:"id"`
	OperationKind    string `json:"operation_kind"`
	OperationID      string `json:"operation_id"`
	Mode             string `json:"mode"`
	ClaimedCommandID string `json:"claimed_command_id,omitempty"`
	CreatedAt        string `json:"created_at"`
}

type AdminFaultTicketCursor struct {
	Pending bool
	RowID   int64
}

func (l *Lab) AdminFaultTickets(ctx context.Context) ([]AdminFaultTicket, error) {
	items, _, err := l.AdminFaultTicketsPage(ctx, nil, 100)
	return items, err
}

func (l *Lab) AdminFaultTicketsPage(ctx context.Context, after *AdminFaultTicketCursor, limit int) ([]AdminFaultTicket, *AdminFaultTicketCursor, error) {
	if limit < 1 || limit > 100 {
		return nil, nil, ErrAdminInvalidCommand
	}
	query := `SELECT rowid,id,operation_kind,operation_id,mode,COALESCE(claimed_command_id,''),created_at FROM admin_fault_tickets`
	args := make([]any, 0, 3)
	if after != nil {
		query += ` WHERE (CASE WHEN claimed_command_id IS NULL THEN 1 ELSE 0 END,rowid) < (?,?)`
		pending := 0
		if after.Pending {
			pending = 1
		}
		args = append(args, pending, after.RowID)
	}
	query += ` ORDER BY (claimed_command_id IS NULL) DESC,rowid DESC LIMIT ?`
	args = append(args, limit+1)
	rows, err := l.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	items := make([]AdminFaultTicket, 0, limit+1)
	rowIDs := make([]int64, 0, limit+1)
	for rows.Next() {
		var item AdminFaultTicket
		var rowID int64
		if err := rows.Scan(&rowID, &item.ID, &item.OperationKind, &item.OperationID, &item.Mode, &item.ClaimedCommandID, &item.CreatedAt); err != nil {
			return nil, nil, err
		}
		items = append(items, item)
		rowIDs = append(rowIDs, rowID)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	if len(items) <= limit {
		return items, nil, nil
	}
	items = items[:limit]
	last := items[len(items)-1]
	return items, &AdminFaultTicketCursor{Pending: last.ClaimedCommandID == "", RowID: rowIDs[limit-1]}, nil
}

type adminClockPayload struct {
	Mode     string `json:"mode"`
	ValueUTC string `json:"value_utc,omitempty"`
}

type adminDecisionPayload struct {
	Status string `json:"status"`
}

type adminFaultPayload struct {
	OperationKind string `json:"operation_kind"`
	Mode          string `json:"mode"`
}

func canonicalAdminControlPayload(actionID string, raw json.RawMessage) ([]byte, error) {
	switch actionID {
	case "C46":
		var p adminClockPayload
		if err := decodeAdminPayload(raw, &p); err != nil {
			return nil, err
		}
		if p.Mode == "real" {
			if p.ValueUTC != "" {
				return nil, ErrAdminInvalidCommand
			}
		} else if p.Mode == "fixed" {
			canonical, err := canonicalAdminUTC(p.ValueUTC)
			if err != nil {
				return nil, ErrAdminInvalidCommand
			}
			p.ValueUTC = canonical
		} else {
			return nil, ErrAdminInvalidCommand
		}
		return json.Marshal(p)
	case "C47", "C48":
		var p adminDecisionPayload
		if err := decodeAdminPayload(raw, &p); err != nil {
			return nil, err
		}
		if p.Status != "succeeded" && p.Status != "definitively_failed" {
			return nil, ErrAdminInvalidCommand
		}
		return json.Marshal(p)
	case "C49":
		var p adminFaultPayload
		if err := decodeAdminPayload(raw, &p); err != nil {
			return nil, err
		}
		if (p.OperationKind != "payment" && p.OperationKind != "refund") || (p.Mode != "lost_response" && p.Mode != "crash_after_provider") {
			return nil, ErrAdminInvalidCommand
		}
		return json.Marshal(p)
	}
	return nil, ErrAdminUnsupportedAction
}

func (l *Lab) loadAdminClock(ctx context.Context) error {
	var mode, value string
	var revision int64
	err := l.db.QueryRowContext(ctx, `SELECT mode,COALESCE(value_utc,''),revision FROM admin_lab_clock WHERE id=1`).Scan(&mode, &value, &revision)
	if errors.Is(err, sql.ErrNoRows) {
		mode = "real"
		revision = 0
	} else if err != nil {
		return err
	}
	var fixed *time.Time
	if mode == "fixed" {
		at, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			return err
		}
		fixed = &at
	}
	l.clockMu.Lock()
	l.fixedClock, l.clockRevision = fixed, revision
	l.clockMu.Unlock()
	return nil
}

func (l *Lab) AdminClock(ctx context.Context) (AdminClockState, error) {
	l.clockMu.RLock()
	fixed, revision := l.fixedClock, l.clockRevision
	l.clockMu.RUnlock()
	state := AdminClockState{Mode: "real", Revision: revision, BusinessTime: l.ClockTime().Format(time.RFC3339Nano)}
	if fixed != nil {
		state.Mode = "fixed"
		state.ValueUTC = fixed.Format(time.RFC3339Nano)
	}
	return state, nil
}

func (l *Lab) adminBusinessSnapshotTx(ctx context.Context, tx *sql.Tx) (time.Time, int64, error) {
	var mode, value string
	var revision int64
	err := tx.QueryRowContext(ctx, `SELECT mode,COALESCE(value_utc,''),revision FROM admin_lab_clock WHERE id=1`).Scan(&mode, &value, &revision)
	if errors.Is(err, sql.ErrNoRows) {
		return l.baseNow().UTC(), 0, nil
	}
	if err != nil {
		return time.Time{}, 0, err
	}
	if mode == "fixed" {
		at, err := time.Parse(time.RFC3339Nano, value)
		return at.UTC(), revision, err
	}
	return l.baseNow().UTC(), revision, nil
}

func (l *Lab) executeAdminClockTx(ctx context.Context, tx *sql.Tx, payloadJSON string) (any, error) {
	var p adminClockPayload
	if err := decodeAdminPayload(json.RawMessage(payloadJSON), &p); err != nil {
		return nil, err
	}
	var revision int64
	err := tx.QueryRowContext(ctx, `SELECT revision FROM admin_lab_clock WHERE id=1`).Scan(&revision)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	revision++
	if _, err := tx.ExecContext(ctx, `INSERT INTO admin_lab_clock(id,mode,value_utc,revision,updated_at) VALUES(1,?,?,?,?) ON CONFLICT(id) DO UPDATE SET mode=excluded.mode,value_utc=excluded.value_utc,revision=excluded.revision,updated_at=excluded.updated_at`, p.Mode, p.ValueUTC, revision, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return nil, err
	}
	return map[string]any{"mode": p.Mode, "value_utc": p.ValueUTC, "revision": revision}, nil
}

func (l *Lab) executeAdminFaultTx(ctx context.Context, tx *sql.Tx, commandID, targetID, payloadJSON string) (any, error) {
	var p adminFaultPayload
	if err := decodeAdminPayload(json.RawMessage(payloadJSON), &p); err != nil {
		return nil, err
	}
	var status string
	table := "payment_operations"
	if p.OperationKind == "refund" {
		table = "refund_operations"
	}
	if err := tx.QueryRowContext(ctx, `SELECT status FROM `+table+` WHERE id=?`, targetID).Scan(&status); err != nil {
		return nil, err
	}
	if status != "created" {
		return nil, ErrConflict
	}
	ticketID, err := newID("fault_")
	if err != nil {
		return nil, err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO admin_fault_tickets(id,operation_kind,operation_id,mode,created_at) VALUES(?,?,?,?,?) ON CONFLICT(operation_kind,operation_id) DO NOTHING`, ticketID, p.OperationKind, targetID, p.Mode, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if inserted != 1 {
		return nil, ErrConflict
	}
	return map[string]string{"ticket_id": ticketID, "operation_kind": p.OperationKind, "operation_id": targetID, "mode": p.Mode}, nil
}

func (l *Lab) claimAdminFault(ctx context.Context, commandID, kind, operationID string) (string, error) {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if err := adminReserveWriterTx(ctx, tx, commandID); err != nil {
		return "", err
	}
	if err := adminCheckLeaseTx(ctx, tx, commandID); err != nil {
		return "", err
	}
	var mode, claimedCommandID string
	err = tx.QueryRowContext(ctx, `SELECT mode,COALESCE(claimed_command_id,'') FROM admin_fault_tickets WHERE operation_kind=? AND operation_id=?`, kind, operationID).Scan(&mode, &claimedCommandID)
	hasTicket := !errors.Is(err, sql.ErrNoRows)
	if err != nil && hasTicket {
		return "", err
	}
	if claimedCommandID != "" && claimedCommandID != commandID {
		return "", ErrAdminFaultOwnedByOtherCommand
	}
	actionID := "C09"
	if kind == "refund" {
		actionID = "C16"
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO admin_external_dispatch_claims(action_id,target_id,command_id,created_at) VALUES(?,?,?,?) ON CONFLICT(action_id,target_id) DO NOTHING`, actionID, operationID, commandID, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return "", err
	}
	var owner string
	if err := tx.QueryRowContext(ctx, `SELECT command_id FROM admin_external_dispatch_claims WHERE action_id=? AND target_id=?`, actionID, operationID).Scan(&owner); err != nil {
		return "", err
	}
	if owner != commandID {
		return "", ErrAdminPreviewStale
	}
	if !hasTicket {
		return "", tx.Commit()
	}
	if claimedCommandID != "" {
		// A restart after the claim commits must reuse this command's fault.
		if err := tx.Commit(); err != nil {
			return "", err
		}
		if claimedCommandID == commandID {
			return mode, nil
		}
		return "", ErrAdminFaultOwnedByOtherCommand
	}
	result, err := tx.ExecContext(ctx, `UPDATE admin_fault_tickets SET claimed_command_id=? WHERE operation_kind=? AND operation_id=? AND claimed_command_id IS NULL`, commandID, kind, operationID)
	if err != nil {
		return "", err
	}
	n, err := result.RowsAffected()
	if err != nil || n != 1 {
		return "", ErrConflict
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return mode, nil
}

func (l *Lab) releaseUnusedAdminFault(ctx context.Context, commandID, kind, operationID string) error {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := adminReserveWriterTx(ctx, tx, commandID); err != nil {
		return err
	}
	if err := adminCheckLeaseTx(ctx, tx, commandID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE admin_fault_tickets SET claimed_command_id=NULL WHERE operation_kind=? AND operation_id=? AND claimed_command_id=?`, kind, operationID, commandID); err != nil {
		return err
	}
	return tx.Commit()
}
