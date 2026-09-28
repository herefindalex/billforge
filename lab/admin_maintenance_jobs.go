package lab

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
	"time"
)

type adminMaintenanceItem struct {
	SubscriptionID string `json:"subscription_id"`
	PeriodIndex    int    `json:"period_index"`
	Revision       int64  `json:"revision"`
}

type adminMaintenanceSource struct {
	AsOf  string                 `json:"as_of"`
	Items []adminMaintenanceItem `json:"items"`
}

const maintenanceBatchLimit = 100

func dueMaintenanceItems(ctx context.Context, tx *sql.Tx, actionID string, at time.Time) ([]adminMaintenanceItem, bool, error) {
	var lastTargetID, previousSource string
	// The pinned source preserves selection order even when a batch wraps past the largest ID.
	err := tx.QueryRowContext(ctx, `SELECT p.source_versions_json FROM admin_jobs j JOIN admin_commands c ON c.id=j.command_id JOIN admin_previews p ON p.id=c.preview_id WHERE j.kind=? ORDER BY j.created_at DESC,j.id DESC LIMIT 1`, actionID).Scan(&previousSource)
	if err != nil && err != sql.ErrNoRows {
		return nil, false, err
	}
	if err == nil {
		var source adminMaintenanceSource
		if err := json.Unmarshal([]byte(previousSource), &source); err != nil {
			return nil, false, err
		}
		if len(source.Items) == 0 {
			return nil, false, ErrAdminPreviewStale
		}
		lastTargetID = source.Items[len(source.Items)-1].SubscriptionID
		if lastTargetID == "" {
			return nil, false, ErrAdminPreviewStale
		}
	}
	query := `SELECT s.id,COALESCE((SELECT MAX(period_index) FROM billing_periods WHERE subscription_id=s.id),-1),s.revision FROM subscriptions s WHERE s.status='active'`
	args := []any{}
	if actionID == "C44" {
		query = `SELECT s.id,p.period_index,s.revision FROM subscriptions s JOIN billing_periods p ON p.subscription_id=s.id WHERE s.status='active' AND p.period_end<=? AND p.period_index=(SELECT MAX(p2.period_index) FROM billing_periods p2 WHERE p2.subscription_id=s.id)`
		args = append(args, at.UTC().UnixNano())
	}
	items := make([]adminMaintenanceItem, 0)
	load := func(operator string) error {
		queryArgs := append(append([]any{}, args...), lastTargetID, maintenanceBatchLimit+1-len(items))
		rows, err := tx.QueryContext(ctx, query+` AND s.id `+operator+` ? ORDER BY s.id LIMIT ?`, queryArgs...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item adminMaintenanceItem
			if err := rows.Scan(&item.SubscriptionID, &item.PeriodIndex, &item.Revision); err != nil {
				return err
			}
			items = append(items, item)
		}
		return rows.Err()
	}
	if err := load(">"); err != nil {
		return nil, false, err
	}
	if len(items) <= maintenanceBatchLimit && lastTargetID != "" {
		if err := load("<="); err != nil {
			return nil, false, err
		}
	}
	hasMore := len(items) > maintenanceBatchLimit
	if hasMore {
		items = items[:maintenanceBatchLimit]
	}
	return items, hasMore, nil
}

func (l *Lab) adminCreateMaintenancePreview(ctx context.Context, actorID, actionID, targetID string, canonical []byte) (AdminPreview, error) {
	if actorID == "" || targetID != "" {
		return AdminPreview{}, ErrAdminInvalidCommand
	}
	at := l.ClockTime()
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return AdminPreview{}, err
	}
	defer tx.Rollback()
	items, hasMore, err := dueMaintenanceItems(ctx, tx, actionID, at)
	if err != nil {
		return AdminPreview{}, err
	}
	if len(items) == 0 {
		return AdminPreview{}, ErrConflict
	}
	sources, err := json.Marshal(adminMaintenanceSource{AsOf: at.Format(time.RFC3339Nano), Items: items})
	if err != nil {
		return AdminPreview{}, err
	}
	impact, err := json.Marshal(map[string]any{"subscription_count": strconv.Itoa(len(items)), "has_more_candidates": strconv.FormatBool(hasMore), "items": items})
	if err != nil {
		return AdminPreview{}, err
	}
	id, err := newID("prev_")
	if err != nil {
		return AdminPreview{}, err
	}
	created := time.Now().UTC()
	expires := created.Add(5 * time.Minute).Format(time.RFC3339Nano)
	_, err = tx.ExecContext(ctx, `INSERT INTO admin_previews(id,actor_id,action_id,target_id,intent_hash,intent_json,source_versions_json,impact_json,expires_at,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, id, actorID, actionID, "", adminIntentHash(actionID, "", canonical), string(canonical), string(sources), string(impact), expires, created.Format(time.RFC3339Nano))
	if err != nil {
		return AdminPreview{}, err
	}
	if err := tx.Commit(); err != nil {
		return AdminPreview{}, err
	}
	return AdminPreview{ID: id, ActorID: actorID, ActionID: actionID, ExpiresAt: expires, SourceVersions: sources, Impact: impact, BlockingReasons: []string{}}, nil
}
