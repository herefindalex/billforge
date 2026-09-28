package lab

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type adminLinkAccountPayload struct {
	LegacyAccountID string `json:"legacy_account_id"`
	CustomerID      string `json:"customer_id"`
	BeneficiaryID   string `json:"beneficiary_id"`
	Cohort          string `json:"cohort"`
	HasHistory      string `json:"has_history"`
}

func canonicalAdminLinkAccountPayload(raw json.RawMessage) ([]byte, error) {
	var p adminLinkAccountPayload
	if err := decodeAdminPayload(raw, &p); err != nil {
		return nil, err
	}
	p.LegacyAccountID = strings.TrimSpace(p.LegacyAccountID)
	p.CustomerID = strings.TrimSpace(p.CustomerID)
	p.BeneficiaryID = strings.TrimSpace(p.BeneficiaryID)
	p.Cohort = strings.TrimSpace(p.Cohort)
	if p.LegacyAccountID == "" || p.CustomerID == "" || p.BeneficiaryID == "" || p.Cohort == "" || (p.HasHistory != "true" && p.HasHistory != "false") {
		return nil, ErrAdminInvalidCommand
	}
	return json.Marshal(p)
}

func parseAdminLinkAccountPayload(canonical []byte) (AccountLink, error) {
	var p adminLinkAccountPayload
	if err := json.Unmarshal(canonical, &p); err != nil {
		return AccountLink{}, err
	}
	return AccountLink{LegacyAccountID: p.LegacyAccountID, CustomerID: p.CustomerID, BeneficiaryID: p.BeneficiaryID, Cohort: p.Cohort, HasHistory: p.HasHistory == "true"}, nil
}

func (l *Lab) adminCreateAccountLinkPreview(ctx context.Context, actorID, targetID string, canonical []byte) (AdminPreview, error) {
	if actorID == "" || targetID != "" {
		return AdminPreview{}, ErrAdminInvalidCommand
	}
	link, err := parseAdminLinkAccountPayload(canonical)
	if err != nil {
		return AdminPreview{}, err
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return AdminPreview{}, err
	}
	saved, err := l.linkLegacyAccountTx(ctx, tx, link)
	_ = tx.Rollback()
	if err != nil {
		return AdminPreview{}, err
	}
	sources, err := json.Marshal(map[string]string{"legacy_account_id": saved.LegacyAccountID, "customer_id": saved.CustomerID})
	if err != nil {
		return AdminPreview{}, err
	}
	impact, err := json.Marshal(map[string]string{"legacy_account_id": link.LegacyAccountID, "customer_id": link.CustomerID, "beneficiary_id": link.BeneficiaryID, "cohort": link.Cohort, "has_history": pBool(link.HasHistory)})
	if err != nil {
		return AdminPreview{}, err
	}
	id, err := newID("prev_")
	if err != nil {
		return AdminPreview{}, err
	}
	created := time.Now().UTC()
	expires := created.Add(5 * time.Minute).Format(time.RFC3339Nano)
	_, err = l.db.ExecContext(ctx, `INSERT INTO admin_previews(id,actor_id,action_id,target_id,intent_hash,intent_json,source_versions_json,impact_json,expires_at,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, id, actorID, "C36", "", adminIntentHash("C36", "", canonical), string(canonical), string(sources), string(impact), expires, created.Format(time.RFC3339Nano))
	if err != nil {
		return AdminPreview{}, err
	}
	return AdminPreview{ID: id, ActorID: actorID, ActionID: "C36", ExpiresAt: expires, SourceVersions: sources, Impact: impact, BlockingReasons: []string{}}, nil
}

func pBool(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func (l *Lab) executeAccountLinkTx(ctx context.Context, tx *sql.Tx, commandID, actorID, previewID string, canonical []byte) (any, error) {
	var savedIntent, expiry string
	err := tx.QueryRowContext(ctx, `SELECT intent_json,expires_at FROM admin_previews WHERE id=? AND actor_id=? AND action_id='C36' AND target_id='' AND claimed_command_id=?`, previewID, actorID, commandID).Scan(&savedIntent, &expiry)
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
	link, err := parseAdminLinkAccountPayload(canonical)
	if err != nil {
		return nil, err
	}
	saved, err := l.linkLegacyAccountTx(ctx, tx, link)
	if errors.Is(err, ErrConflict) || errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAdminPreviewStale
	}
	if err != nil {
		return nil, err
	}
	return map[string]string{"legacy_account_id": saved.LegacyAccountID, "customer_id": saved.CustomerID, "read_owner": saved.ReadOwner, "writer_owner": saved.WriterOwner}, nil
}
