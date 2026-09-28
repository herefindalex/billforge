package lab

import (
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"time"
)

type changeCorrectionPreviewItem struct {
	ChangeID       string `json:"change_id"`
	InvoiceID      string `json:"invoice_id"`
	ReductionMinor string `json:"reduction_minor"`
	SourceJSON     string `json:"source_json"`
}

func (l *Lab) loadChangeCorrectionPreviewItem(ctx context.Context, tx *sql.Tx, changeID string) (changeCorrectionPreviewItem, error) {
	var item changeCorrectionPreviewItem
	item.ChangeID = changeID
	var pending int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox WHERE id=? AND kind='change_correction' AND object_id=? AND status='pending'`, "change-correction:"+changeID, changeID).Scan(&pending); err != nil {
		return item, err
	}
	if pending != 1 {
		return item, ErrConflict
	}
	var amount int64
	if err := tx.QueryRowContext(ctx, `SELECT invoice_id,correction_minor FROM immediate_changes WHERE id=? AND status='active'`, changeID).Scan(&item.InvoiceID, &amount); err != nil {
		return item, err
	}
	if amount <= 0 {
		return item, ErrConflict
	}
	reduction, err := l.loadReductionSnapshot(ctx, tx, item.InvoiceID, amount, false)
	if err != nil {
		return item, err
	}
	item.ReductionMinor = strconv.FormatInt(amount, 10)
	item.SourceJSON = string(reduction.Sources)
	return item, nil
}

func (l *Lab) adminCreateChangeCorrectionsPreview(ctx context.Context, actorID string, canonical []byte) (AdminPreview, error) {
	if actorID == "" {
		return AdminPreview{}, ErrAdminInvalidCommand
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return AdminPreview{}, err
	}
	defer tx.Rollback()
	previousSource, err := lastCommittedBatchSource(ctx, tx, "C13")
	if err != nil {
		return AdminPreview{}, err
	}
	var cursor int64
	if previousSource != "" {
		var previous map[string]string
		if err := json.Unmarshal([]byte(previousSource), &previous); err != nil {
			return AdminPreview{}, err
		}
		var items []changeCorrectionPreviewItem
		if err := json.Unmarshal([]byte(previous["items_json"]), &items); err != nil || len(items) == 0 {
			return AdminPreview{}, ErrAdminPreviewStale
		}
		lastID := items[len(items)-1].ChangeID
		if err := tx.QueryRowContext(ctx, `SELECT rowid FROM outbox WHERE id=?`, "change-correction:"+lastID).Scan(&cursor); err != nil {
			return AdminPreview{}, err
		}
	}
	var ids []string
	readCandidates := func(query string, cursor int64) error {
		rows, err := tx.QueryContext(ctx, query, cursor, 101-len(ids))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return err
			}
			ids = append(ids, id)
		}
		return rows.Err()
	}
	if err := readCandidates(`SELECT object_id FROM outbox WHERE kind='change_correction' AND status='pending' AND rowid>? ORDER BY rowid LIMIT ?`, cursor); err != nil {
		return AdminPreview{}, err
	}
	if cursor > 0 && len(ids) <= 100 {
		if err := readCandidates(`SELECT object_id FROM outbox WHERE kind='change_correction' AND status='pending' AND rowid<=? ORDER BY rowid LIMIT ?`, cursor); err != nil {
			return AdminPreview{}, err
		}
	}
	if len(ids) == 0 {
		return AdminPreview{}, ErrConflict
	}
	hasMore := len(ids) > 100
	if hasMore {
		ids = ids[:100]
	}
	items := make([]changeCorrectionPreviewItem, 0, len(ids))
	var total int64
	for _, id := range ids {
		item, err := l.loadChangeCorrectionPreviewItem(ctx, tx, id)
		if err != nil {
			return AdminPreview{}, err
		}
		amount, _ := strconv.ParseInt(item.ReductionMinor, 10, 64)
		if amount <= 0 || amount > math.MaxInt64-total {
			return AdminPreview{}, ErrConflict
		}
		total += amount
		items = append(items, item)
	}
	itemJSON, err := json.Marshal(items)
	if err != nil {
		return AdminPreview{}, err
	}
	sources, err := json.Marshal(map[string]string{"items_json": string(itemJSON)})
	if err != nil {
		return AdminPreview{}, err
	}
	impact, err := json.Marshal(map[string]string{"count": strconv.Itoa(len(items)), "has_more_candidates": strconv.FormatBool(hasMore), "change_ids": strings.Join(ids, ", "), "total_reduction_minor": strconv.FormatInt(total, 10), "items_json": string(itemJSON)})
	if err != nil {
		return AdminPreview{}, err
	}
	created := time.Now().UTC()
	expires := created.Add(5 * time.Minute).Format(time.RFC3339Nano)
	id, err := newID("prev_")
	if err != nil {
		return AdminPreview{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO admin_previews(id,actor_id,action_id,target_id,intent_hash,intent_json,source_versions_json,impact_json,expires_at,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, id, actorID, "C13", "", adminIntentHash("C13", "", canonical), string(canonical), string(sources), string(impact), expires, created.Format(time.RFC3339Nano))
	if err != nil {
		return AdminPreview{}, err
	}
	if err := tx.Commit(); err != nil {
		return AdminPreview{}, err
	}
	return AdminPreview{ID: id, ActorID: actorID, ActionID: "C13", TargetID: "", ExpiresAt: expires, SourceVersions: sources, Impact: impact, BlockingReasons: []string{}}, nil
}
