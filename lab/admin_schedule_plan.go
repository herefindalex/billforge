package lab

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"time"
)

type schedulePlanSnapshot struct {
	PlanID      string
	Seats       int64
	QuoteExpiry time.Time
	Sources     []byte
	Impact      []byte
}

func (l *Lab) loadSchedulePlanSnapshot(ctx context.Context, tx *sql.Tx, at time.Time, subID string, input AdminChangePlanPayload) (schedulePlanSnapshot, error) {
	var result schedulePlanSnapshot
	var quoteExpiry, quotedSeats int64
	var quotePrice, quoteFingerprint, priceChecksum, quoteCustomer, boundSub, mode, savedFingerprint, planID string
	var boundRevision int64
	var amount int64
	var currency string
	err := tx.QueryRowContext(ctx, `SELECT q.customer_id,q.price_version_id,q.amount_minor,q.currency,q.expires_at,q.fingerprint,q.seat_quantity,p.plan_id,p.checksum,b.subscription_id,b.mode,b.expected_revision,b.fingerprint
		FROM quotes q JOIN price_versions p ON p.id=q.price_version_id JOIN change_quote_bindings b ON b.quote_id=q.id WHERE q.id=?`, input.QuoteID).
		Scan(&quoteCustomer, &quotePrice, &amount, &currency, &quoteExpiry, &quoteFingerprint, &quotedSeats, &planID, &priceChecksum, &boundSub, &mode, &boundRevision, &savedFingerprint)
	if err != nil {
		return result, err
	}
	revision, err := strconv.ParseInt(input.Revision, 10, 64)
	if err != nil {
		return result, ErrConflict
	}
	var currentRevision int64
	var status, subCustomer string
	if err := tx.QueryRowContext(ctx, `SELECT customer_id,revision,status FROM subscriptions WHERE id=?`, subID).Scan(&subCustomer, &currentRevision, &status); err != nil {
		return result, err
	}
	if err := ensureCommerceWriter(ctx, tx, subCustomer); err != nil {
		return result, err
	}
	if quoteCustomer != subCustomer || boundSub != subID || mode != "next_period" || savedFingerprint != input.Fingerprint {
		return result, ErrChangeQuoteBindingMismatch
	}
	if status != "active" || !at.Before(time.Unix(0, quoteExpiry)) {
		return result, ErrConflict
	}
	if boundRevision != revision || currentRevision != revision {
		return result, ErrChangeQuoteRevisionChanged
	}
	if err := ensureNoUnresolvedPriceMigration(ctx, tx, subID); err != nil {
		return result, err
	}
	effective, err := currentPeriodEnd(ctx, tx, subID, at)
	if err != nil {
		return result, err
	}
	var selectedPrice string
	err = tx.QueryRowContext(ctx, `SELECT p.id FROM catalog_selection c JOIN price_versions p ON p.id=c.price_version_id WHERE c.plan_id=? AND c.cohort='default' AND c.effective_at<=? AND p.publication_state='published' AND p.effective_from<=? AND (p.effective_to IS NULL OR p.effective_to>?) ORDER BY c.effective_at DESC LIMIT 1`, planID, effective, effective, effective).Scan(&selectedPrice)
	if err != nil {
		return result, err
	}
	if selectedPrice != quotePrice {
		return result, ErrChangeQuotePriceSuperseded
	}
	result.PlanID, result.Seats, result.QuoteExpiry = planID, quotedSeats, time.Unix(0, quoteExpiry).UTC()
	result.Sources, err = json.Marshal(map[string]string{
		"subscription_revision": input.Revision, "subscription_status": status,
		"quote_id": input.QuoteID, "quote_fingerprint": quoteFingerprint,
		"binding_fingerprint": savedFingerprint, "price_version_id": quotePrice,
		"price_checksum": priceChecksum, "period_end": strconv.FormatInt(effective, 10),
	})
	if err != nil {
		return schedulePlanSnapshot{}, err
	}
	result.Impact, err = json.Marshal(map[string]string{
		"subscription_id": subID, "quote_id": input.QuoteID, "plan_id": planID,
		"seats": strconv.FormatInt(quotedSeats, 10), "price_version_id": quotePrice,
		"amount_minor": strconv.FormatInt(amount, 10), "currency": currency,
		"effective_at": time.Unix(0, effective).UTC().Format(time.RFC3339Nano),
	})
	return result, err
}

func (l *Lab) adminCreateSchedulePlanPreview(ctx context.Context, actorID, subID string, canonical []byte) (AdminPreview, error) {
	if actorID == "" || subID == "" {
		return AdminPreview{}, ErrAdminInvalidCommand
	}
	var input AdminChangePlanPayload
	if err := decodeAdminPayload(canonical, &input); err != nil {
		return AdminPreview{}, err
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return AdminPreview{}, err
	}
	defer tx.Rollback()
	businessAt := l.now().UTC()
	snapshot, err := l.loadSchedulePlanSnapshot(ctx, tx, businessAt, subID, input)
	if err != nil {
		return AdminPreview{}, err
	}
	created := time.Now().UTC()
	expires, err := adminPreviewExpiry(created, businessAt, snapshot.QuoteExpiry)
	if err != nil {
		return AdminPreview{}, err
	}
	id, err := newID("prev_")
	if err != nil {
		return AdminPreview{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO admin_previews(id,actor_id,action_id,target_id,intent_hash,intent_json,source_versions_json,impact_json,expires_at,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, id, actorID, "C03", subID, adminIntentHash("C03", subID, canonical), string(canonical), string(snapshot.Sources), string(snapshot.Impact), expires.Format(time.RFC3339Nano), created.Format(time.RFC3339Nano))
	if err != nil {
		return AdminPreview{}, err
	}
	if err := tx.Commit(); err != nil {
		return AdminPreview{}, err
	}
	return AdminPreview{ID: id, ActorID: actorID, ActionID: "C03", TargetID: subID, ExpiresAt: expires.Format(time.RFC3339Nano), SourceVersions: snapshot.Sources, Impact: snapshot.Impact, BlockingReasons: []string{}}, nil
}

func (l *Lab) executeSchedulePlanTx(ctx context.Context, tx *sql.Tx, commandID, previewID, subID, payloadJSON, businessTime string) (any, error) {
	var input AdminChangePlanPayload
	if err := decodeAdminPayload(json.RawMessage(payloadJSON), &input); err != nil {
		return nil, err
	}
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
	snapshot, err := l.loadSchedulePlanSnapshot(ctx, tx, at, subID, input)
	if errors.Is(err, ErrConflict) || errors.Is(err, sql.ErrNoRows) || errors.Is(err, ErrPeriodEnded) {
		return nil, ErrAdminPreviewStale
	}
	if err != nil {
		return nil, err
	}
	if storedSources != string(snapshot.Sources) {
		return nil, ErrAdminPreviewStale
	}
	revision, err := strconv.ParseInt(input.Revision, 10, 64)
	if err != nil {
		return nil, err
	}
	result, err := l.scheduleNextPlanTx(ctx, tx, at, subID, snapshot.PlanID, snapshot.Seats, revision, "admin:"+commandID, input.QuoteID, input.Fingerprint)
	if errors.Is(err, ErrConflict) || errors.Is(err, sql.ErrNoRows) || errors.Is(err, ErrPeriodEnded) {
		return nil, ErrAdminPreviewStale
	}
	if err != nil {
		return nil, err
	}
	return map[string]string{"schedule_id": result.ID, "subscription_id": subID, "price_version_id": result.TargetPriceVersionID, "effective_at": result.EffectiveAt.UTC().Format(time.RFC3339Nano), "revision": strconv.FormatInt(result.Revision, 10)}, nil
}
