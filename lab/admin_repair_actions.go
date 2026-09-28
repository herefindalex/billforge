package lab

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"time"
)

type adminRepairPayload struct {
	SourceRevision string `json:"source_revision"`
	Evidence       string `json:"evidence"`
}

func canonicalAdminRepairPayload(raw json.RawMessage) ([]byte, error) {
	var p adminRepairPayload
	if err := decodeAdminPayload(raw, &p); err != nil {
		return nil, err
	}
	revision, err := strconv.ParseInt(p.SourceRevision, 10, 64)
	if err != nil || revision < 0 || p.Evidence == "" {
		return nil, ErrAdminInvalidCommand
	}
	p.SourceRevision = strconv.FormatInt(revision, 10)
	return json.Marshal(p)
}

func (l *Lab) adminCreateRepairPreview(ctx context.Context, actorID, targetID string, canonical []byte) (AdminPreview, error) {
	if actorID == "" || targetID == "" {
		return AdminPreview{}, ErrAdminInvalidCommand
	}
	var p adminRepairPayload
	if err := json.Unmarshal(canonical, &p); err != nil {
		return AdminPreview{}, err
	}
	d, err := loadDiscrepancy(ctx, l.db, targetID)
	if err != nil {
		return AdminPreview{}, err
	}
	if d.Status == "resolved" || p.SourceRevision != strconv.FormatInt(d.SourceRevision, 10) || p.Evidence != d.Evidence {
		return AdminPreview{}, ErrConflict
	}
	sources, err := json.Marshal(d)
	if err != nil {
		return AdminPreview{}, err
	}
	impact, err := json.Marshal(map[string]string{"discrepancy_id": d.ID, "classification": d.Classification, "repair_action": repairAction(d), "expected": d.Expected, "actual": d.Actual, "evidence": d.Evidence})
	if err != nil {
		return AdminPreview{}, err
	}
	id, err := newID("prev_")
	if err != nil {
		return AdminPreview{}, err
	}
	created := time.Now().UTC()
	expires := created.Add(5 * time.Minute).Format(time.RFC3339Nano)
	_, err = l.db.ExecContext(ctx, `INSERT INTO admin_previews(id,actor_id,action_id,target_id,intent_hash,intent_json,source_versions_json,impact_json,expires_at,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, id, actorID, "C34", targetID, adminIntentHash("C34", targetID, canonical), string(canonical), string(sources), string(impact), expires, created.Format(time.RFC3339Nano))
	if err != nil {
		return AdminPreview{}, err
	}
	return AdminPreview{ID: id, ActorID: actorID, ActionID: "C34", TargetID: targetID, ExpiresAt: expires, SourceVersions: sources, Impact: impact, BlockingReasons: []string{}}, nil
}

func (l *Lab) adminExecuteRepairCommand(ctx context.Context, commandID string) (AdminCommand, error) {
	var actorID, targetID, status, previewID, payloadJSON string
	if err := l.db.QueryRowContext(ctx, `SELECT actor_id,target_id,status,COALESCE(preview_id,''),payload_json FROM admin_commands WHERE id=? AND action_id='C34'`, commandID).Scan(&actorID, &targetID, &status, &previewID, &payloadJSON); err != nil {
		return AdminCommand{}, err
	}
	if status == "succeeded" || status == "failed" {
		return l.AdminCommand(ctx, commandID)
	}
	if status != "accepted" && status != "waiting_verification" {
		return AdminCommand{}, ErrConflict
	}
	requestKey := "admin:" + commandID
	_, existingErr := loadRepairOperation(ctx, l.db, requestKey)
	if existingErr != nil && !errors.Is(existingErr, sql.ErrNoRows) {
		return AdminCommand{}, existingErr
	}
	if errors.Is(existingErr, sql.ErrNoRows) {
		var savedIntent, savedSources, expiry string
		err := l.db.QueryRowContext(ctx, `SELECT intent_json,source_versions_json,expires_at FROM admin_previews WHERE id=? AND actor_id=? AND action_id='C34' AND target_id=? AND claimed_command_id=?`, previewID, actorID, targetID, commandID).Scan(&savedIntent, &savedSources, &expiry)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return l.adminRejectRepairPreview(ctx, commandID, actorID, targetID)
			}
			return AdminCommand{}, err
		}
		expires, err := time.Parse(time.RFC3339Nano, expiry)
		if err != nil || !time.Now().UTC().Before(expires) || savedIntent != payloadJSON {
			return l.adminRejectRepairPreview(ctx, commandID, actorID, targetID)
		}
		var expected Discrepancy
		if err := json.Unmarshal([]byte(savedSources), &expected); err != nil {
			return AdminCommand{}, err
		}
		current, err := loadDiscrepancy(ctx, l.db, targetID)
		if errors.Is(err, sql.ErrNoRows) {
			return l.adminRejectRepairPreview(ctx, commandID, actorID, targetID)
		}
		if err != nil {
			return AdminCommand{}, err
		}
		if current != expected || current.Status == "resolved" {
			return l.adminRejectRepairPreview(ctx, commandID, actorID, targetID)
		}
	}
	if err := l.adminRenewLease(ctx, commandID); err != nil {
		return AdminCommand{}, err
	}
	repair, err := l.RepairDiscrepancy(ctx, targetID, requestKey)
	if errors.Is(err, ErrPaymentUnknown) {
		if markErr := l.adminMarkWaitingVerification(ctx, commandID, actorID, "C34", targetID); markErr != nil {
			return AdminCommand{}, markErr
		}
		return l.AdminCommand(ctx, commandID)
	}
	if errors.Is(err, ErrConflict) || errors.Is(err, sql.ErrNoRows) {
		if failErr := l.adminFailCommand(ctx, commandID, actorID, "C34", targetID, "DOMAIN_REJECTED"); failErr != nil {
			return AdminCommand{}, failErr
		}
		return l.AdminCommand(ctx, commandID)
	}
	if err != nil {
		return AdminCommand{}, err
	}
	if repair.Status == "waiting" {
		if err := l.adminMarkWaitingVerification(ctx, commandID, actorID, "C34", targetID); err != nil {
			return AdminCommand{}, err
		}
		return l.AdminCommand(ctx, commandID)
	}
	return l.adminFinishExternalCommand(ctx, commandID, map[string]string{"repair_id": repair.ID, "discrepancy_id": targetID, "repair_status": repair.Status, "verification": repair.Verification})
}

func (l *Lab) adminRejectRepairPreview(ctx context.Context, commandID, actorID, targetID string) (AdminCommand, error) {
	if err := l.adminFailCommand(ctx, commandID, actorID, "C34", targetID, "PREVIEW_STALE"); err != nil {
		return AdminCommand{}, err
	}
	return l.AdminCommand(ctx, commandID)
}
