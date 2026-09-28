package lab

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"time"
)

type unfulfilledSnapshot struct {
	InvoiceID string
	Currency  string
	Sources   []byte
	Impact    []byte
}

func (l *Lab) loadUnfulfilledSnapshot(ctx context.Context, tx *sql.Tx, changeID string) (unfulfilledSnapshot, error) {
	var snapshot unfulfilledSnapshot
	change, err := loadImmediateChange(ctx, tx, changeID)
	if err != nil {
		return snapshot, err
	}
	if change.Status != "needs_review" {
		return snapshot, ErrConflict
	}
	var resolved int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM immediate_change_resolutions WHERE change_id=?`, changeID).Scan(&resolved); err != nil {
		return snapshot, err
	}
	if resolved != 0 {
		return snapshot, ErrConflict
	}
	reduction, err := l.loadReductionSnapshot(ctx, tx, change.InvoiceID, change.QuotedAmountMinor, true)
	if err != nil {
		return snapshot, err
	}
	var sources, impact map[string]string
	if err := json.Unmarshal(reduction.Sources, &sources); err != nil {
		return snapshot, err
	}
	if err := json.Unmarshal(reduction.Impact, &impact); err != nil {
		return snapshot, err
	}
	sources["change_id"] = changeID
	sources["change_status"] = change.Status
	sources["change_invoice_id"] = change.InvoiceID
	sources["change_quoted_amount_minor"] = strconv.FormatInt(change.QuotedAmountMinor, 10)
	impact["change_id"] = changeID
	impact["resolution"] = "unfulfilled_refund_credit"
	snapshot.InvoiceID, snapshot.Currency = change.InvoiceID, reduction.Currency
	snapshot.Sources, err = json.Marshal(sources)
	if err != nil {
		return unfulfilledSnapshot{}, err
	}
	snapshot.Impact, err = json.Marshal(impact)
	return snapshot, err
}

func (l *Lab) adminCreateUnfulfilledPreview(ctx context.Context, actorID, changeID string, canonical []byte) (AdminPreview, error) {
	if actorID == "" || changeID == "" {
		return AdminPreview{}, ErrAdminInvalidCommand
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return AdminPreview{}, err
	}
	defer tx.Rollback()
	snapshot, err := l.loadUnfulfilledSnapshot(ctx, tx, changeID)
	if err != nil {
		return AdminPreview{}, err
	}
	created := time.Now().UTC()
	expires := created.Add(5 * time.Minute).Format(time.RFC3339Nano)
	id, err := newID("prev_")
	if err != nil {
		return AdminPreview{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO admin_previews(id,actor_id,action_id,target_id,intent_hash,intent_json,source_versions_json,impact_json,expires_at,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, id, actorID, "C14", changeID, adminIntentHash("C14", changeID, canonical), string(canonical), string(snapshot.Sources), string(snapshot.Impact), expires, created.Format(time.RFC3339Nano))
	if err != nil {
		return AdminPreview{}, err
	}
	if err := tx.Commit(); err != nil {
		return AdminPreview{}, err
	}
	return AdminPreview{ID: id, ActorID: actorID, ActionID: "C14", TargetID: changeID, ExpiresAt: expires, SourceVersions: snapshot.Sources, Impact: snapshot.Impact, BlockingReasons: []string{}}, nil
}

func (l *Lab) executeResolveUnfulfilledTx(ctx context.Context, tx *sql.Tx, commandID, previewID, changeID, businessTime string) (any, error) {
	var storedSources string
	if err := tx.QueryRowContext(ctx, `SELECT source_versions_json FROM admin_previews WHERE id=? AND claimed_command_id=?`, previewID, commandID).Scan(&storedSources); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrAdminPreviewStale
		}
		return nil, err
	}
	snapshot, err := l.loadUnfulfilledSnapshot(ctx, tx, changeID)
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
	result, err := l.resolveUnfulfilledImmediateChangeTx(ctx, tx, at, changeID)
	if errors.Is(err, ErrConflict) || errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAdminPreviewStale
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"change_id": changeID, "invoice_id": snapshot.InvoiceID, "correction_id": result.ID,
		"reduction_minor": strconv.FormatInt(result.ReductionMinor, 10),
		"grant_ids":       result.GrantIDs, "currency": snapshot.Currency,
	}, nil
}
