package lab

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"time"
)

type adminUsageAdjustmentPayload struct {
	Source          string `json:"source"`
	EventID         string `json:"event_id"`
	SubscriptionID  string `json:"subscription_id"`
	OriginalSource  string `json:"original_source"`
	OriginalEventID string `json:"original_event_id"`
	ReverseQuantity string `json:"reverse_quantity"`
}
type adminCloseUsagePayload struct {
	PeriodIndex string `json:"period_index"`
	Cutoff      string `json:"cutoff"`
}
type adminRerateUsagePayload struct {
	PeriodIndex string `json:"period_index"`
}

func canonicalAdminUsagePayload(actionID string, raw json.RawMessage) ([]byte, error) {
	switch actionID {
	case "C27":
		var p adminUsageAdjustmentPayload
		if err := decodeAdminPayload(raw, &p); err != nil {
			return nil, err
		}
		if p.Source == "" || p.EventID == "" || p.SubscriptionID == "" || p.OriginalSource == "" || p.OriginalEventID == "" {
			return nil, ErrAdminInvalidCommand
		}
		quantity, err := strconv.ParseInt(p.ReverseQuantity, 10, 64)
		if err != nil || quantity <= 0 {
			return nil, ErrAdminInvalidCommand
		}
		p.ReverseQuantity = strconv.FormatInt(quantity, 10)
		return json.Marshal(p)
	case "C28":
		var p adminCloseUsagePayload
		if err := decodeAdminPayload(raw, &p); err != nil {
			return nil, err
		}
		index, err := strconv.Atoi(p.PeriodIndex)
		if err != nil || index < 0 {
			return nil, ErrAdminInvalidCommand
		}
		cutoff, err := canonicalAdminUTC(p.Cutoff)
		if err != nil {
			return nil, ErrAdminInvalidCommand
		}
		p.PeriodIndex, p.Cutoff = strconv.Itoa(index), cutoff
		return json.Marshal(p)
	case "C29":
		var p adminRerateUsagePayload
		if err := decodeAdminPayload(raw, &p); err != nil {
			return nil, err
		}
		index, err := strconv.Atoi(p.PeriodIndex)
		if err != nil || index < 0 {
			return nil, ErrAdminInvalidCommand
		}
		p.PeriodIndex = strconv.Itoa(index)
		return json.Marshal(p)
	default:
		return nil, ErrAdminUnsupportedAction
	}
}

type adminUsageSnapshot struct {
	Sources []byte
	Impact  []byte
}

type usageSourceEvent struct {
	Source         string `json:"source"`
	EventID        string `json:"event_id"`
	Quantity       int64  `json:"quantity"`
	ReceivedAt     int64  `json:"received_at"`
	PriceVersionID string `json:"price_version_id"`
}

func usageEventSourceJSON(ctx context.Context, tx *sql.Tx, subID string, index int, receivedBefore int64) (string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT source,event_id,quantity,received_at,price_version_id FROM usage_events WHERE subscription_id=? AND period_index=? AND received_at<=? ORDER BY rowid`, subID, index, receivedBefore)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	events := []usageSourceEvent{}
	for rows.Next() {
		var event usageSourceEvent
		if err := rows.Scan(&event.Source, &event.EventID, &event.Quantity, &event.ReceivedAt, &event.PriceVersionID); err != nil {
			return "", err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(events)
	return string(encoded), err
}

func (l *Lab) loadAdminUsageSnapshot(ctx context.Context, tx *sql.Tx, at time.Time, actionID, targetID string, canonical []byte) (adminUsageSnapshot, error) {
	var snapshot adminUsageSnapshot
	source := map[string]string{}
	impact := map[string]string{}
	switch actionID {
	case "C27":
		var p adminUsageAdjustmentPayload
		if err := decodeAdminPayload(canonical, &p); err != nil {
			return snapshot, err
		}
		var tenant string
		if err := tx.QueryRowContext(ctx, `SELECT customer_id FROM subscriptions WHERE id=?`, p.SubscriptionID).Scan(&tenant); err != nil {
			return snapshot, err
		}
		var originalSub, meter, version string
		var originalQuantity, eventAt int64
		var index int
		if err := tx.QueryRowContext(ctx, `SELECT subscription_id,meter_id,quantity,event_at,period_index,price_version_id FROM usage_events WHERE tenant_id=? AND source=? AND event_id=? AND quantity>0`, tenant, p.OriginalSource, p.OriginalEventID).Scan(&originalSub, &meter, &originalQuantity, &eventAt, &index, &version); err != nil {
			return snapshot, err
		}
		if originalSub != p.SubscriptionID {
			return snapshot, ErrConflict
		}
		var already int64
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(-quantity),0) FROM usage_events WHERE tenant_id=? AND correction_source=? AND correction_event_id=?`, tenant, p.OriginalSource, p.OriginalEventID).Scan(&already); err != nil {
			return snapshot, err
		}
		quantity, _ := strconv.ParseInt(p.ReverseQuantity, 10, 64)
		if quantity > originalQuantity-already {
			return snapshot, ErrConflict
		}
		var duplicate int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_events WHERE tenant_id=? AND source=? AND event_id=?`, tenant, p.Source, p.EventID).Scan(&duplicate); err != nil {
			return snapshot, err
		}
		if duplicate != 0 {
			return snapshot, ErrConflict
		}
		source["tenant_id"], source["original_subscription_id"] = tenant, originalSub
		source["original_meter_id"], source["original_price_version_id"] = meter, version
		source["original_quantity"], source["already_reversed"] = strconv.FormatInt(originalQuantity, 10), strconv.FormatInt(already, 10)
		source["original_event_at"], source["period_index"], source["new_event_absent"] = strconv.FormatInt(eventAt, 10), strconv.Itoa(index), "true"
		impact["subscription_id"], impact["meter_id"], impact["period_index"] = p.SubscriptionID, meter, strconv.Itoa(index)
		impact["original_event_id"], impact["reverse_quantity"] = p.OriginalEventID, p.ReverseQuantity
		impact["remaining_quantity"] = strconv.FormatInt(originalQuantity-already-quantity, 10)
	case "C28", "C29":
		var index int
		var receivedBefore int64
		var version string
		var periodStart, periodEnd int64
		var cutoff time.Time
		if actionID == "C28" {
			var p adminCloseUsagePayload
			if err := decodeAdminPayload(canonical, &p); err != nil {
				return snapshot, err
			}
			index, _ = strconv.Atoi(p.PeriodIndex)
			cutoff, _ = time.Parse(time.RFC3339Nano, p.Cutoff)
			if cutoff.After(at) {
				return snapshot, ErrConflict
			}
			receivedBefore = cutoff.UnixNano()
		} else {
			var p adminRerateUsagePayload
			if err := decodeAdminPayload(canonical, &p); err != nil {
				return snapshot, err
			}
			index, _ = strconv.Atoi(p.PeriodIndex)
			receivedBefore = at.UnixNano()
		}
		if err := tx.QueryRowContext(ctx, `SELECT period_start,period_end FROM billing_periods WHERE subscription_id=? AND period_index=?`, targetID, index).Scan(&periodStart, &periodEnd); err != nil {
			return snapshot, err
		}
		var prior UsageRating
		if actionID == "C28" {
			if receivedBefore < periodEnd {
				return snapshot, ErrConflict
			}
			var existing int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_ratings WHERE subscription_id=? AND period_index=? AND revision=1`, targetID, index).Scan(&existing); err != nil {
				return snapshot, err
			}
			if existing != 0 {
				return snapshot, ErrConflict
			}
			err := tx.QueryRowContext(ctx, `SELECT price_version_id FROM usage_events WHERE subscription_id=? AND period_index=? ORDER BY rowid LIMIT 1`, targetID, index).Scan(&version)
			if errors.Is(err, sql.ErrNoRows) {
				err = tx.QueryRowContext(ctx, `SELECT price_version_id FROM pricing_assignments WHERE subscription_id=? AND effective_start<? AND (effective_end IS NULL OR effective_end>?) ORDER BY assignment_index DESC LIMIT 1`, targetID, periodEnd, periodEnd-1).Scan(&version)
			}
			if err != nil {
				return snapshot, err
			}
			source["rating_absent"] = "true"
		} else {
			if err := tx.QueryRowContext(ctx, `SELECT price_version_id FROM usage_periods WHERE subscription_id=? AND period_index=?`, targetID, index).Scan(&version); err != nil {
				return snapshot, err
			}
			var priorID string
			if err := tx.QueryRowContext(ctx, `SELECT id FROM usage_ratings WHERE subscription_id=? AND period_index=? ORDER BY revision DESC LIMIT 1`, targetID, index).Scan(&priorID); err != nil {
				return snapshot, err
			}
			var err error
			prior, err = loadUsageRating(ctx, tx, priorID)
			if err != nil {
				return snapshot, err
			}
			source["prior_rating_id"], source["prior_revision"] = prior.ID, strconv.Itoa(prior.Revision)
			source["prior_quantity"], source["prior_rounded_minor"] = strconv.FormatInt(prior.Quantity, 10), strconv.FormatInt(prior.RoundedMinor, 10)
		}
		rating, err := rateUsage(ctx, tx, targetID, index, version, receivedBefore)
		if err != nil {
			return snapshot, err
		}
		events, err := usageEventSourceJSON(ctx, tx, targetID, index, receivedBefore)
		if err != nil {
			return snapshot, err
		}
		var checksum string
		if err := tx.QueryRowContext(ctx, `SELECT checksum FROM price_versions WHERE id=?`, version).Scan(&checksum); err != nil {
			return snapshot, err
		}
		source["period_start"], source["period_end"], source["period_index"] = strconv.FormatInt(periodStart, 10), strconv.FormatInt(periodEnd, 10), strconv.Itoa(index)
		source["price_version_id"], source["price_checksum"] = version, checksum
		source["received_before"], source["events_json"] = strconv.FormatInt(receivedBefore, 10), events
		impact["subscription_id"], impact["period_index"], impact["price_version_id"] = targetID, strconv.Itoa(index), version
		impact["quantity"], impact["rounded_minor"] = strconv.FormatInt(rating.Quantity, 10), strconv.FormatInt(rating.RoundedMinor, 10)
		impact["included_quantity"], impact["overage_quantity"] = strconv.FormatInt(rating.IncludedQuantity, 10), strconv.FormatInt(rating.OverageQuantity, 10)
		if actionID == "C28" {
			impact["cutoff"] = cutoff.UTC().Format(time.RFC3339Nano)
			impact["delta_minor"] = strconv.FormatInt(rating.RoundedMinor, 10)
		}
		if actionID == "C29" {
			impact["prior_rounded_minor"] = strconv.FormatInt(prior.RoundedMinor, 10)
			impact["delta_minor"] = strconv.FormatInt(rating.RoundedMinor-prior.RoundedMinor, 10)
		}
	default:
		return snapshot, ErrAdminUnsupportedAction
	}
	var err error
	snapshot.Sources, err = json.Marshal(source)
	if err != nil {
		return adminUsageSnapshot{}, err
	}
	snapshot.Impact, err = json.Marshal(impact)
	return snapshot, err
}

func (l *Lab) adminCreateUsagePreview(ctx context.Context, actorID, actionID, targetID string, canonical []byte) (AdminPreview, error) {
	if actorID == "" || (actionID == "C27" && targetID != "") || (actionID != "C27" && targetID == "") {
		return AdminPreview{}, ErrAdminInvalidCommand
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return AdminPreview{}, err
	}
	defer tx.Rollback()
	snapshot, err := l.loadAdminUsageSnapshot(ctx, tx, l.now().UTC(), actionID, targetID, canonical)
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

func (l *Lab) executeAdminUsageTx(ctx context.Context, tx *sql.Tx, commandID, previewID, actionID, targetID, payloadJSON, businessTime string) (any, error) {
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
	snapshot, err := l.loadAdminUsageSnapshot(ctx, tx, at, actionID, targetID, []byte(payloadJSON))
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
	case "C27":
		var p adminUsageAdjustmentPayload
		if err := decodeAdminPayload([]byte(payloadJSON), &p); err != nil {
			return nil, err
		}
		quantity, _ := strconv.ParseInt(p.ReverseQuantity, 10, 64)
		event, err := l.recordUsageAdjustmentTx(ctx, tx, at, p.Source, p.EventID, p.SubscriptionID, p.OriginalSource, p.OriginalEventID, quantity)
		if err != nil {
			return nil, err
		}
		return map[string]string{"source": event.Source, "event_id": event.EventID, "subscription_id": event.SubscriptionID, "meter_id": event.MeterID, "quantity": strconv.FormatInt(event.Quantity, 10), "period_index": strconv.Itoa(event.PeriodIndex)}, nil
	case "C28":
		var p adminCloseUsagePayload
		if err := decodeAdminPayload([]byte(payloadJSON), &p); err != nil {
			return nil, err
		}
		index, _ := strconv.Atoi(p.PeriodIndex)
		cutoff, _ := time.Parse(time.RFC3339Nano, p.Cutoff)
		rating, err := l.closeUsagePeriodTx(ctx, tx, at, targetID, index, cutoff)
		if err != nil {
			return nil, err
		}
		return map[string]string{"rating_id": rating.ID, "subscription_id": targetID, "period_index": p.PeriodIndex, "revision": strconv.Itoa(rating.Revision), "rounded_minor": strconv.FormatInt(rating.RoundedMinor, 10)}, nil
	case "C29":
		var p adminRerateUsagePayload
		if err := decodeAdminPayload([]byte(payloadJSON), &p); err != nil {
			return nil, err
		}
		index, _ := strconv.Atoi(p.PeriodIndex)
		rating, err := l.rerateUsagePeriodTx(ctx, tx, at, targetID, index)
		if err != nil {
			return nil, err
		}
		return map[string]string{"rating_id": rating.ID, "subscription_id": targetID, "period_index": p.PeriodIndex, "revision": strconv.Itoa(rating.Revision), "rounded_minor": strconv.FormatInt(rating.RoundedMinor, 10), "delta_minor": strconv.FormatInt(rating.DeltaMinor, 10)}, nil
	default:
		return nil, ErrAdminUnsupportedAction
	}
}
