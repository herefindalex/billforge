package lab

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
	"time"
)

func dueContractOperations(ctx context.Context, tx *sql.Tx, at time.Time, cursor string) ([]string, bool, error) {
	var ids []string
	readCandidates := func(comparison string) error {
		query := `SELECT o.id FROM payment_operations o
			JOIN billing_periods p ON p.invoice_id=o.invoice_id
			JOIN invoice_contracts c ON c.invoice_id=o.invoice_id
			WHERE o.status='created' AND p.due_at<=? AND NOT EXISTS(SELECT 1 FROM outbox b WHERE b.id='capture:'||o.id)
			AND o.id` + comparison + `? ORDER BY o.id LIMIT ?`
		rows, err := tx.QueryContext(ctx, query, at.UTC().UnixNano(), cursor, 101-len(ids))
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
	if err := readCandidates(">"); err != nil {
		return nil, false, err
	}
	if cursor != "" && len(ids) <= 100 {
		if err := readCandidates("<="); err != nil {
			return nil, false, err
		}
	}
	hasMore := len(ids) > 100
	if hasMore {
		ids = ids[:100]
	}
	return ids, hasMore, nil
}

func (l *Lab) adminCreateContractCollectionsPreview(ctx context.Context, actorID, targetID string, canonical []byte) (AdminPreview, error) {
	if actorID == "" || targetID != "" {
		return AdminPreview{}, ErrAdminInvalidCommand
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return AdminPreview{}, err
	}
	defer tx.Rollback()
	previousSource, err := lastCommittedBatchSource(ctx, tx, "C32")
	if err != nil {
		return AdminPreview{}, err
	}
	var cursor string
	if previousSource != "" {
		var previous struct {
			OperationIDs []string `json:"operation_ids"`
		}
		if err := json.Unmarshal([]byte(previousSource), &previous); err != nil || len(previous.OperationIDs) == 0 {
			return AdminPreview{}, ErrAdminPreviewStale
		}
		cursor = previous.OperationIDs[len(previous.OperationIDs)-1]
	}
	ids, hasMore, err := dueContractOperations(ctx, tx, l.ClockTime(), cursor)
	if err != nil {
		return AdminPreview{}, err
	}
	if len(ids) == 0 {
		return AdminPreview{}, ErrConflict
	}
	sources, err := json.Marshal(map[string]any{"operation_ids": ids, "as_of": l.ClockTime().Format(time.RFC3339Nano)})
	if err != nil {
		return AdminPreview{}, err
	}
	impact, err := json.Marshal(map[string]any{"count": strconv.Itoa(len(ids)), "has_more_candidates": strconv.FormatBool(hasMore), "operation_ids": ids})
	if err != nil {
		return AdminPreview{}, err
	}
	id, err := newID("prev_")
	if err != nil {
		return AdminPreview{}, err
	}
	created := time.Now().UTC()
	expires := created.Add(5 * time.Minute).Format(time.RFC3339Nano)
	_, err = tx.ExecContext(ctx, `INSERT INTO admin_previews(id,actor_id,action_id,target_id,intent_hash,intent_json,source_versions_json,impact_json,expires_at,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, id, actorID, "C32", "", adminIntentHash("C32", "", canonical), string(canonical), string(sources), string(impact), expires, created.Format(time.RFC3339Nano))
	if err != nil {
		return AdminPreview{}, err
	}
	if err := tx.Commit(); err != nil {
		return AdminPreview{}, err
	}
	return AdminPreview{ID: id, ActorID: actorID, ActionID: "C32", ExpiresAt: expires, SourceVersions: sources, Impact: impact, BlockingReasons: []string{}}, nil
}
