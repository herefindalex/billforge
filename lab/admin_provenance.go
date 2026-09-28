package lab

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

type adminBackfillProvenancePayload struct {
	LegacyInvoiceID        string `json:"legacy_invoice_id"`
	LegacySubscriptionID   string `json:"legacy_subscription_id"`
	CommerceSubscriptionID string `json:"commerce_subscription_id"`
	CommerceInvoiceID      string `json:"commerce_invoice_id"`
	PriceVersionID         string `json:"price_version_id"`
}

type adminResolveProvenancePayload struct {
	CommerceSubscriptionID string `json:"commerce_subscription_id"`
	CommerceInvoiceID      string `json:"commerce_invoice_id"`
	PriceVersionID         string `json:"price_version_id"`
	Decision               string `json:"decision"`
}

func canonicalAdminProvenancePayload(actionID string, raw json.RawMessage) ([]byte, error) {
	if actionID == "C39" {
		var p adminBackfillProvenancePayload
		if err := decodeAdminPayload(raw, &p); err != nil {
			return nil, err
		}
		if p.LegacyInvoiceID == "" || p.LegacySubscriptionID == "" || p.CommerceSubscriptionID == "" || p.CommerceInvoiceID == "" || p.PriceVersionID == "" {
			return nil, ErrAdminInvalidCommand
		}
		return json.Marshal(p)
	}
	var p adminResolveProvenancePayload
	if err := decodeAdminPayload(raw, &p); err != nil {
		return nil, err
	}
	if p.CommerceSubscriptionID == "" || p.CommerceInvoiceID == "" || p.PriceVersionID == "" || p.Decision == "" {
		return nil, ErrAdminInvalidCommand
	}
	return json.Marshal(p)
}

func loadLegacyProvenance(ctx context.Context, tx *sql.Tx, legacyInvoiceID string) (LegacyProvenance, error) {
	var p LegacyProvenance
	err := tx.QueryRowContext(ctx, `SELECT legacy_invoice_id,legacy_subscription_id,legacy_account_id,commerce_subscription_id,commerce_invoice_id,price_version_id,status,evidence FROM legacy_provenance WHERE legacy_invoice_id=?`, legacyInvoiceID).Scan(&p.LegacyInvoiceID, &p.LegacySubscriptionID, &p.LegacyAccountID, &p.CommerceSubscriptionID, &p.CommerceInvoiceID, &p.PriceVersionID, &p.Status, &p.Evidence)
	return p, err
}

func (l *Lab) applyAdminProvenanceTx(ctx context.Context, tx *sql.Tx, actionID, targetID, actorID string, canonical []byte) (LegacyProvenance, error) {
	if actionID == "C39" {
		var p adminBackfillProvenancePayload
		if err := json.Unmarshal(canonical, &p); err != nil {
			return LegacyProvenance{}, err
		}
		return l.backfillLegacyProvenanceTx(ctx, tx, LegacyProvenance{LegacyInvoiceID: p.LegacyInvoiceID, LegacySubscriptionID: p.LegacySubscriptionID, LegacyAccountID: targetID, CommerceSubscriptionID: p.CommerceSubscriptionID, CommerceInvoiceID: p.CommerceInvoiceID, PriceVersionID: p.PriceVersionID})
	}
	var p adminResolveProvenancePayload
	if err := json.Unmarshal(canonical, &p); err != nil {
		return LegacyProvenance{}, err
	}
	old, err := loadLegacyProvenance(ctx, tx, targetID)
	if err != nil {
		return LegacyProvenance{}, err
	}
	old.CommerceSubscriptionID = p.CommerceSubscriptionID
	old.CommerceInvoiceID = p.CommerceInvoiceID
	old.PriceVersionID = p.PriceVersionID
	return l.resolveLegacyProvenanceTx(ctx, tx, old, actorID, p.Decision)
}

func (l *Lab) adminCreateProvenancePreview(ctx context.Context, actorID, actionID, targetID string, canonical []byte) (AdminPreview, error) {
	if actorID == "" || targetID == "" {
		return AdminPreview{}, ErrAdminInvalidCommand
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return AdminPreview{}, err
	}
	var before LegacyProvenance
	if actionID == "C40" {
		before, err = loadLegacyProvenance(ctx, tx, targetID)
		if err != nil {
			_ = tx.Rollback()
			return AdminPreview{}, err
		}
	}
	result, err := l.applyAdminProvenanceTx(ctx, tx, actionID, targetID, actorID, canonical)
	_ = tx.Rollback()
	if err != nil {
		return AdminPreview{}, err
	}
	sources, err := json.Marshal(before)
	if err != nil {
		return AdminPreview{}, err
	}
	impact, err := json.Marshal(map[string]string{"legacy_invoice_id": result.LegacyInvoiceID, "status": result.Status, "evidence": result.Evidence, "commerce_invoice_id": result.CommerceInvoiceID, "price_version_id": result.PriceVersionID})
	if err != nil {
		return AdminPreview{}, err
	}
	id, err := newID("prev_")
	if err != nil {
		return AdminPreview{}, err
	}
	created := time.Now().UTC()
	expires := created.Add(5 * time.Minute).Format(time.RFC3339Nano)
	_, err = l.db.ExecContext(ctx, `INSERT INTO admin_previews(id,actor_id,action_id,target_id,intent_hash,intent_json,source_versions_json,impact_json,expires_at,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, id, actorID, actionID, targetID, adminIntentHash(actionID, targetID, canonical), string(canonical), string(sources), string(impact), expires, created.Format(time.RFC3339Nano))
	if err != nil {
		return AdminPreview{}, err
	}
	return AdminPreview{ID: id, ActorID: actorID, ActionID: actionID, TargetID: targetID, ExpiresAt: expires, SourceVersions: sources, Impact: impact, BlockingReasons: []string{}}, nil
}

func (l *Lab) executeAdminProvenanceTx(ctx context.Context, tx *sql.Tx, commandID, actorID, actionID, targetID, previewID string, canonical []byte) (any, error) {
	var savedIntent, savedSources, expiry string
	err := tx.QueryRowContext(ctx, `SELECT intent_json,source_versions_json,expires_at FROM admin_previews WHERE id=? AND actor_id=? AND action_id=? AND target_id=? AND claimed_command_id=?`, previewID, actorID, actionID, targetID, commandID).Scan(&savedIntent, &savedSources, &expiry)
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
	if actionID == "C40" {
		var expected LegacyProvenance
		if err := json.Unmarshal([]byte(savedSources), &expected); err != nil {
			return nil, err
		}
		current, err := loadLegacyProvenance(ctx, tx, targetID)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && current != expected) {
			return nil, ErrAdminPreviewStale
		}
		if err != nil {
			return nil, err
		}
	}
	result, err := l.applyAdminProvenanceTx(ctx, tx, actionID, targetID, actorID, canonical)
	if errors.Is(err, ErrConflict) || errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAdminPreviewStale
	}
	if err != nil {
		return nil, err
	}
	return map[string]string{"legacy_invoice_id": result.LegacyInvoiceID, "status": result.Status, "evidence": result.Evidence}, nil
}
