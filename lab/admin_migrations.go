package lab

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"
)

type adminPlanMigrationPayload struct {
	ID                   string   `json:"id"`
	Cohort               string   `json:"cohort"`
	TargetPriceVersionID string   `json:"target_price_version_id"`
	SubscriptionIDs      []string `json:"subscription_ids"`
}

type adminSkipMigrationPayload struct {
	SubscriptionID string `json:"subscription_id"`
	Reason         string `json:"reason"`
}

func canonicalAdminMigrationPayload(actionID string, raw json.RawMessage) ([]byte, error) {
	switch actionID {
	case "C22":
		var p adminPlanMigrationPayload
		if err := decodeAdminPayload(raw, &p); err != nil {
			return nil, err
		}
		if p.ID == "" || p.Cohort == "" || p.TargetPriceVersionID == "" || len(p.SubscriptionIDs) == 0 || len(p.SubscriptionIDs) > 100 {
			return nil, ErrAdminInvalidCommand
		}
		for _, id := range p.SubscriptionIDs {
			if strings.TrimSpace(id) == "" || id != strings.TrimSpace(id) {
				return nil, ErrAdminInvalidCommand
			}
		}
		sort.Strings(p.SubscriptionIDs)
		for i := 1; i < len(p.SubscriptionIDs); i++ {
			if p.SubscriptionIDs[i] == p.SubscriptionIDs[i-1] {
				return nil, ErrAdminInvalidCommand
			}
		}
		return json.Marshal(p)
	case "C24":
		var p adminSkipMigrationPayload
		if err := decodeAdminPayload(raw, &p); err != nil {
			return nil, err
		}
		p.Reason = strings.TrimSpace(p.Reason)
		if p.SubscriptionID == "" || p.Reason == "" {
			return nil, ErrAdminInvalidCommand
		}
		return json.Marshal(p)
	case "C25":
		var p struct{}
		if err := decodeAdminPayload(raw, &p); err != nil {
			return nil, err
		}
		return []byte(`{}`), nil
	default:
		return nil, ErrAdminUnsupportedAction
	}
}

type adminMigrationSnapshot struct {
	Sources []byte
	Impact  []byte
}

func (l *Lab) loadAdminMigrationSnapshot(ctx context.Context, tx *sql.Tx, at time.Time, actionID, targetID string, canonical []byte) (adminMigrationSnapshot, error) {
	var snapshot adminMigrationSnapshot
	source := map[string]string{}
	impact := map[string]string{}
	switch actionID {
	case "C22":
		var p adminPlanMigrationPayload
		if err := decodeAdminPayload(canonical, &p); err != nil {
			return snapshot, err
		}
		var existing int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM price_migrations WHERE id=?`, p.ID).Scan(&existing); err != nil {
			return snapshot, err
		}
		if existing != 0 {
			return snapshot, ErrConflict
		}
		var targetChecksum, targetState string
		if err := tx.QueryRowContext(ctx, `SELECT checksum,publication_state FROM price_versions WHERE id=?`, p.TargetPriceVersionID).Scan(&targetChecksum, &targetState); err != nil {
			return snapshot, err
		}
		if targetState != "published" {
			return snapshot, ErrConflict
		}
		items := make([]PriceMigrationItem, 0, len(p.SubscriptionIDs))
		for _, subID := range p.SubscriptionIDs {
			item, err := previewPriceMigrationItem(ctx, tx, subID, p.TargetPriceVersionID, at)
			if err != nil {
				return snapshot, err
			}
			var selected string
			if err := tx.QueryRowContext(ctx, `SELECT price_version_id FROM catalog_selection WHERE plan_id='pro' AND cohort=? AND effective_at<=? ORDER BY effective_at DESC LIMIT 1`, p.Cohort, item.EffectiveAt.UnixNano()).Scan(&selected); err != nil {
				return snapshot, err
			}
			if selected != p.TargetPriceVersionID {
				return snapshot, ErrConflict
			}
			items = append(items, item)
		}
		itemJSON, err := json.Marshal(items)
		if err != nil {
			return snapshot, err
		}
		source["target_checksum"], source["items_json"], source["migration_absent"] = targetChecksum, string(itemJSON), "true"
		impact["migration_id"], impact["cohort"], impact["target_price_version_id"] = p.ID, p.Cohort, p.TargetPriceVersionID
		impact["item_count"], impact["subscription_ids"], impact["items_json"] = strconv.Itoa(len(items)), strings.Join(p.SubscriptionIDs, ", "), string(itemJSON)
	case "C24":
		var p adminSkipMigrationPayload
		if err := decodeAdminPayload(canonical, &p); err != nil {
			return snapshot, err
		}
		var batchStatus, itemStatus, fromPrice, targetPrice string
		var revision, effective int64
		err := tx.QueryRowContext(ctx, `SELECT m.status,i.status,i.from_price_version_id,i.target_price_version_id,i.expected_revision,i.effective_at FROM price_migrations m JOIN price_migration_items i ON i.migration_id=m.id WHERE m.id=? AND i.subscription_id=?`, targetID, p.SubscriptionID).Scan(&batchStatus, &itemStatus, &fromPrice, &targetPrice, &revision, &effective)
		if err != nil {
			return snapshot, err
		}
		if batchStatus != "paused" || (itemStatus != "pending" && itemStatus != "conflicted") {
			return snapshot, ErrConflict
		}
		source["batch_status"], source["item_status"] = batchStatus, itemStatus
		source["from_price_version_id"], source["target_price_version_id"] = fromPrice, targetPrice
		source["expected_revision"], source["effective_at_nanos"] = strconv.FormatInt(revision, 10), strconv.FormatInt(effective, 10)
		impact["migration_id"], impact["subscription_id"], impact["new_status"], impact["reason"] = targetID, p.SubscriptionID, "skipped", p.Reason
	case "C25":
		batch, err := loadPriceMigration(ctx, tx, targetID)
		if err != nil {
			return snapshot, err
		}
		if batch.Status != "paused" {
			return snapshot, ErrConflict
		}
		for _, item := range batch.Items {
			if item.Status == "conflicted" {
				return snapshot, ErrConflict
			}
		}
		itemJSON, err := json.Marshal(batch.Items)
		if err != nil {
			return snapshot, err
		}
		source["batch_status"], source["target_price_version_id"], source["items_json"] = batch.Status, batch.TargetPriceVersionID, string(itemJSON)
		impact["migration_id"], impact["target_price_version_id"], impact["new_status"] = targetID, batch.TargetPriceVersionID, "active"
		impact["item_count"] = strconv.Itoa(len(batch.Items))
	default:
		return snapshot, ErrAdminUnsupportedAction
	}
	var err error
	snapshot.Sources, err = json.Marshal(source)
	if err != nil {
		return adminMigrationSnapshot{}, err
	}
	snapshot.Impact, err = json.Marshal(impact)
	return snapshot, err
}

func (l *Lab) adminCreateMigrationPreview(ctx context.Context, actorID, actionID, targetID string, canonical []byte) (AdminPreview, error) {
	if actorID == "" || (actionID == "C22" && targetID != "") || (actionID != "C22" && targetID == "") {
		return AdminPreview{}, ErrAdminInvalidCommand
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return AdminPreview{}, err
	}
	defer tx.Rollback()
	snapshot, err := l.loadAdminMigrationSnapshot(ctx, tx, l.now().UTC(), actionID, targetID, canonical)
	if err != nil {
		return AdminPreview{}, err
	}
	created := time.Now().UTC()
	expires := created.Add(5 * time.Minute).Format(time.RFC3339Nano)
	id, err := newID("prev_")
	if err != nil {
		return AdminPreview{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO admin_previews(id,actor_id,action_id,target_id,intent_hash,intent_json,source_versions_json,impact_json,expires_at,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, id, actorID, actionID, targetID, adminIntentHash(actionID, targetID, canonical), string(canonical), string(snapshot.Sources), string(snapshot.Impact), expires, created.Format(time.RFC3339Nano))
	if err != nil {
		return AdminPreview{}, err
	}
	if err := tx.Commit(); err != nil {
		return AdminPreview{}, err
	}
	return AdminPreview{ID: id, ActorID: actorID, ActionID: actionID, TargetID: targetID, ExpiresAt: expires, SourceVersions: snapshot.Sources, Impact: snapshot.Impact, BlockingReasons: []string{}}, nil
}

func (l *Lab) executeAdminMigrationTx(ctx context.Context, tx *sql.Tx, commandID, previewID, actionID, targetID, payloadJSON, businessTime string) (any, error) {
	var storedSources string
	if err := tx.QueryRowContext(ctx, `SELECT source_versions_json FROM admin_previews WHERE id=? AND claimed_command_id=?`, previewID, commandID).Scan(&storedSources); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrAdminPreviewStale
		}
		return nil, err
	}
	at, err := time.Parse(time.RFC3339Nano, businessTime)
	if err != nil {
		return nil, err
	}
	snapshot, err := l.loadAdminMigrationSnapshot(ctx, tx, at, actionID, targetID, []byte(payloadJSON))
	if errors.Is(err, ErrConflict) || errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAdminPreviewStale
	}
	if err != nil {
		return nil, err
	}
	if storedSources != string(snapshot.Sources) {
		return nil, ErrAdminPreviewStale
	}
	switch actionID {
	case "C22":
		var p adminPlanMigrationPayload
		if err := decodeAdminPayload([]byte(payloadJSON), &p); err != nil {
			return nil, err
		}
		batch, err := l.planPriceMigrationTx(ctx, tx, at, p.ID, p.Cohort, p.TargetPriceVersionID, p.SubscriptionIDs)
		if err != nil {
			return nil, err
		}
		return map[string]string{"migration_id": batch.ID, "cohort": batch.Cohort, "target_price_version_id": batch.TargetPriceVersionID, "item_count": strconv.Itoa(len(batch.Items)), "status": batch.Status}, nil
	case "C24":
		var p adminSkipMigrationPayload
		if err := decodeAdminPayload([]byte(payloadJSON), &p); err != nil {
			return nil, err
		}
		if err := l.skipPriceMigrationItemTx(ctx, tx, targetID, p.SubscriptionID); err != nil {
			return nil, err
		}
		return map[string]string{"migration_id": targetID, "subscription_id": p.SubscriptionID, "status": "skipped", "reason": p.Reason}, nil
	case "C25":
		if err := l.resumePriceMigrationTx(ctx, tx, targetID); err != nil {
			return nil, err
		}
		return map[string]string{"migration_id": targetID, "status": "active"}, nil
	default:
		return nil, ErrAdminUnsupportedAction
	}
}
