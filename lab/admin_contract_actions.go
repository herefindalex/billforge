package lab

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"time"
)

type adminPublishContractPayload struct {
	ID                         string `json:"id"`
	CustomerID                 string `json:"customer_id"`
	Version                    string `json:"version"`
	BasePriceVersionID         string `json:"base_price_version_id"`
	FixedMinor                 string `json:"fixed_minor"`
	SeatMinor                  string `json:"seat_minor"`
	EffectiveFrom              string `json:"effective_from"`
	EffectiveTo                string `json:"effective_to"`
	PostContractPriceVersionID string `json:"post_contract_price_version_id,omitempty"`
}

func canonicalAdminContractPayload(raw json.RawMessage) ([]byte, error) {
	var p adminPublishContractPayload
	if err := decodeAdminPayload(raw, &p); err != nil {
		return nil, err
	}
	if p.ID == "" || p.CustomerID == "" || p.BasePriceVersionID == "" {
		return nil, ErrAdminInvalidCommand
	}
	for _, value := range []*string{&p.Version, &p.FixedMinor, &p.SeatMinor} {
		n, err := strconv.ParseInt(*value, 10, 64)
		if err != nil || n <= 0 {
			return nil, ErrAdminInvalidCommand
		}
		*value = strconv.FormatInt(n, 10)
	}
	if !minimumUpfrontTextFits(p.FixedMinor, p.SeatMinor) {
		return nil, ErrAdminInvalidCommand
	}
	fromCanonical, err := canonicalAdminUTC(p.EffectiveFrom)
	if err != nil {
		return nil, ErrAdminInvalidCommand
	}
	toCanonical, err := canonicalAdminUTC(p.EffectiveTo)
	if err != nil {
		return nil, ErrAdminInvalidCommand
	}
	from, _ := time.Parse(time.RFC3339Nano, fromCanonical)
	to, _ := time.Parse(time.RFC3339Nano, toCanonical)
	if !to.After(from) {
		return nil, ErrAdminInvalidCommand
	}
	p.EffectiveFrom = fromCanonical
	p.EffectiveTo = toCanonical
	return json.Marshal(p)
}

func parseAdminContractPayload(canonical []byte) (ContractSpec, error) {
	var p adminPublishContractPayload
	if err := json.Unmarshal(canonical, &p); err != nil {
		return ContractSpec{}, err
	}
	version, _ := strconv.ParseInt(p.Version, 10, 64)
	fixed, _ := strconv.ParseInt(p.FixedMinor, 10, 64)
	seat, _ := strconv.ParseInt(p.SeatMinor, 10, 64)
	from, _ := time.Parse(time.RFC3339Nano, p.EffectiveFrom)
	to, _ := time.Parse(time.RFC3339Nano, p.EffectiveTo)
	return ContractSpec{ID: p.ID, CustomerID: p.CustomerID, Version: version, BasePriceVersionID: p.BasePriceVersionID, FixedMinor: fixed, SeatMinor: seat, EffectiveFrom: from, EffectiveTo: to, PostContractPriceVersionID: p.PostContractPriceVersionID}, nil
}

func (l *Lab) adminCreateContractPreview(ctx context.Context, actorID, targetID string, canonical []byte) (AdminPreview, error) {
	if actorID == "" || targetID != "" {
		return AdminPreview{}, ErrAdminInvalidCommand
	}
	spec, err := parseAdminContractPayload(canonical)
	if err != nil {
		return AdminPreview{}, err
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return AdminPreview{}, err
	}
	state, err := l.publishContractTx(ctx, tx, spec)
	_ = tx.Rollback()
	if err != nil {
		return AdminPreview{}, err
	}
	sources, err := json.Marshal(map[string]string{"contract_checksum": state.Checksum, "base_price_version_id": spec.BasePriceVersionID, "post_price_version_id": spec.PostContractPriceVersionID})
	if err != nil {
		return AdminPreview{}, err
	}
	impact, err := json.Marshal(map[string]string{"contract_id": spec.ID, "customer_id": spec.CustomerID, "version": strconv.FormatInt(spec.Version, 10), "fixed_minor": strconv.FormatInt(spec.FixedMinor, 10), "seat_minor": strconv.FormatInt(spec.SeatMinor, 10), "effective_from": spec.EffectiveFrom.Format(time.RFC3339Nano), "effective_to": spec.EffectiveTo.Format(time.RFC3339Nano)})
	if err != nil {
		return AdminPreview{}, err
	}
	id, err := newID("prev_")
	if err != nil {
		return AdminPreview{}, err
	}
	created := time.Now().UTC()
	expires := created.Add(5 * time.Minute).Format(time.RFC3339Nano)
	_, err = l.db.ExecContext(ctx, `INSERT INTO admin_previews(id,actor_id,action_id,target_id,intent_hash,intent_json,source_versions_json,impact_json,expires_at,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, id, actorID, "C31", "", adminIntentHash("C31", "", canonical), string(canonical), string(sources), string(impact), expires, created.Format(time.RFC3339Nano))
	if err != nil {
		return AdminPreview{}, err
	}
	return AdminPreview{ID: id, ActorID: actorID, ActionID: "C31", ExpiresAt: expires, SourceVersions: sources, Impact: impact, BlockingReasons: []string{}}, nil
}

func (l *Lab) executeAdminContractTx(ctx context.Context, tx *sql.Tx, commandID, actorID, previewID string, canonical []byte) (any, error) {
	var savedIntent, expiry string
	err := tx.QueryRowContext(ctx, `SELECT intent_json,expires_at FROM admin_previews WHERE id=? AND actor_id=? AND action_id='C31' AND target_id='' AND claimed_command_id=?`, previewID, actorID, commandID).Scan(&savedIntent, &expiry)
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
	spec, err := parseAdminContractPayload(canonical)
	if err != nil {
		return nil, err
	}
	state, err := l.publishContractTx(ctx, tx, spec)
	if errors.Is(err, ErrConflict) || errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAdminPreviewStale
	}
	if err != nil {
		return nil, err
	}
	return map[string]string{"contract_id": state.ID, "checksum": state.Checksum}, nil
}
