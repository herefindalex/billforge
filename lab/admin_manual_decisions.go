package lab

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type adminManualDecisionPayload struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

func canonicalAdminManualDecisionPayload(raw json.RawMessage) ([]byte, error) {
	var p adminManualDecisionPayload
	if err := decodeAdminPayload(raw, &p); err != nil {
		return nil, err
	}
	p.Decision = strings.TrimSpace(p.Decision)
	p.Reason = strings.TrimSpace(p.Reason)
	if p.Decision == "" || p.Reason == "" {
		return nil, ErrAdminInvalidCommand
	}
	return json.Marshal(p)
}

func (l *Lab) adminCreateManualDecisionPreview(ctx context.Context, actorID, targetID string, canonical []byte) (AdminPreview, error) {
	if actorID == "" || targetID == "" {
		return AdminPreview{}, ErrAdminInvalidCommand
	}
	d, err := loadDiscrepancy(ctx, l.db, targetID)
	if err != nil {
		return AdminPreview{}, err
	}
	if d.Status == "resolved" || (d.Classification != "MANUAL_REVIEW" && d.Classification != "UNSAFE_TO_REPAIR") {
		return AdminPreview{}, ErrConflict
	}
	sources, err := json.Marshal(d)
	if err != nil {
		return AdminPreview{}, err
	}
	var p adminManualDecisionPayload
	if err := json.Unmarshal(canonical, &p); err != nil {
		return AdminPreview{}, err
	}
	impact, err := json.Marshal(map[string]string{"discrepancy_id": d.ID, "classification": d.Classification, "expected": d.Expected, "actual": d.Actual, "decision": p.Decision, "reason": p.Reason})
	if err != nil {
		return AdminPreview{}, err
	}
	id, err := newID("prev_")
	if err != nil {
		return AdminPreview{}, err
	}
	created := time.Now().UTC()
	expires := created.Add(5 * time.Minute).Format(time.RFC3339Nano)
	_, err = l.db.ExecContext(ctx, `INSERT INTO admin_previews(id,actor_id,action_id,target_id,intent_hash,intent_json,source_versions_json,impact_json,expires_at,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, id, actorID, "C35", targetID, adminIntentHash("C35", targetID, canonical), string(canonical), string(sources), string(impact), expires, created.Format(time.RFC3339Nano))
	if err != nil {
		return AdminPreview{}, err
	}
	return AdminPreview{ID: id, ActorID: actorID, ActionID: "C35", TargetID: targetID, ExpiresAt: expires, SourceVersions: sources, Impact: impact, BlockingReasons: []string{}}, nil
}

func (l *Lab) executeManualDecisionTx(ctx context.Context, tx *sql.Tx, commandID, actorID, targetID, previewID string, canonical []byte) (any, error) {
	var savedIntent, savedSources, expiry string
	err := tx.QueryRowContext(ctx, `SELECT intent_json,source_versions_json,expires_at FROM admin_previews WHERE id=? AND actor_id=? AND action_id='C35' AND target_id=? AND claimed_command_id=?`, previewID, actorID, targetID, commandID).Scan(&savedIntent, &savedSources, &expiry)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAdminPreviewStale
	}
	if err != nil {
		return nil, err
	}
	expires, err := time.Parse(time.RFC3339Nano, expiry)
	if err != nil || !time.Now().UTC().Before(expires) || savedIntent != string(canonical) {
		return nil, ErrAdminPreviewStale
	}
	var expected Discrepancy
	if err := json.Unmarshal([]byte(savedSources), &expected); err != nil {
		return nil, err
	}
	current, err := loadDiscrepancy(ctx, tx, targetID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAdminPreviewStale
	}
	if err != nil {
		return nil, err
	}
	if current != expected || current.Status == "resolved" {
		return nil, ErrAdminPreviewStale
	}
	var p adminManualDecisionPayload
	if err := json.Unmarshal(canonical, &p); err != nil {
		return nil, err
	}
	id, err := l.recordManualDecisionTx(ctx, tx, targetID, actorID, p.Decision, p.Reason)
	if errors.Is(err, ErrConflict) {
		return nil, ErrAdminPreviewStale
	}
	if err != nil {
		return nil, err
	}
	return map[string]string{"decision_id": id, "discrepancy_id": targetID}, nil
}
