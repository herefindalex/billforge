package lab

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"time"
)

type immediateUpgradeSnapshot struct {
	Seats       int64
	QuoteExpiry time.Time
	NetMinor    int64
	Sources     []byte
	Impact      []byte
}

func (l *Lab) loadImmediateUpgradeSnapshot(ctx context.Context, tx *sql.Tx, at time.Time, subID string, input AdminChangePlanPayload) (immediateUpgradeSnapshot, error) {
	var snapshot immediateUpgradeSnapshot
	revision, err := strconv.ParseInt(input.Revision, 10, 64)
	if err != nil {
		return snapshot, ErrConflict
	}
	var quoteCustomer, quotePrice, quoteFingerprint, priceChecksum, boundSub, mode, savedFingerprint string
	var quoteExpiry, seats, boundRevision int64
	err = tx.QueryRowContext(ctx, `SELECT q.customer_id,q.price_version_id,q.expires_at,q.fingerprint,q.seat_quantity,p.checksum,b.subscription_id,b.mode,b.expected_revision,b.fingerprint
		FROM quotes q JOIN price_versions p ON p.id=q.price_version_id JOIN change_quote_bindings b ON b.quote_id=q.id WHERE q.id=?`, input.QuoteID).
		Scan(&quoteCustomer, &quotePrice, &quoteExpiry, &quoteFingerprint, &seats, &priceChecksum, &boundSub, &mode, &boundRevision, &savedFingerprint)
	if err != nil {
		return snapshot, err
	}
	var customer, status, fromPrice string
	var currentSeats, currentRevision int64
	err = tx.QueryRowContext(ctx, `SELECT customer_id,status,price_version_id,seat_quantity,revision FROM subscriptions WHERE id=?`, subID).Scan(&customer, &status, &fromPrice, &currentSeats, &currentRevision)
	if err != nil {
		return snapshot, err
	}
	if err := ensureCommerceWriter(ctx, tx, customer); err != nil {
		return snapshot, err
	}
	if quoteCustomer != customer || boundSub != subID || mode != "immediate" || savedFingerprint != input.Fingerprint || boundRevision != revision || currentRevision != revision || status != "active" || !at.Before(time.Unix(0, quoteExpiry)) {
		return snapshot, ErrConflict
	}
	var fromPlan string
	if err := tx.QueryRowContext(ctx, `SELECT plan_id FROM price_versions WHERE id=?`, fromPrice).Scan(&fromPlan); err != nil {
		return snapshot, err
	}
	if fromPlan != "basic" || seats <= 0 {
		return snapshot, ErrConflict
	}
	var pending int
	if err := tx.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM subscription_schedules WHERE subscription_id=? AND status='scheduled')+(SELECT COUNT(*) FROM immediate_changes WHERE subscription_id=? AND status='requested')+(SELECT COUNT(*) FROM subscription_ends WHERE subscription_id=?)+(SELECT COUNT(*) FROM price_migration_items WHERE subscription_id=? AND status IN ('pending','conflicted'))`, subID, subID, subID, subID).Scan(&pending); err != nil {
		return snapshot, err
	}
	if pending != 0 {
		return snapshot, ErrConflict
	}
	var periodStart, periodEnd int64
	var periodInvoice string
	err = tx.QueryRowContext(ctx, `SELECT period_start,period_end,invoice_id FROM billing_periods WHERE subscription_id=? AND period_start<=? AND period_end>? ORDER BY period_index DESC LIMIT 1`, subID, at.UnixNano(), at.UnixNano()).Scan(&periodStart, &periodEnd, &periodInvoice)
	if err != nil {
		return snapshot, err
	}
	balance, err := loadInvoiceBalance(ctx, tx, periodInvoice)
	if err != nil {
		return snapshot, err
	}
	if balance.OutstandingMinor != 0 {
		return snapshot, ErrConflict
	}
	var selectedPrice string
	err = tx.QueryRowContext(ctx, `SELECT p.id FROM catalog_selection c JOIN price_versions p ON p.id=c.price_version_id WHERE c.plan_id='pro' AND c.cohort='default' AND c.effective_at<=? AND p.publication_state='published' AND p.effective_from<=? AND (p.effective_to IS NULL OR p.effective_to>?) ORDER BY c.effective_at DESC LIMIT 1`, at.UnixNano(), at.UnixNano(), at.UnixNano()).Scan(&selectedPrice)
	if err != nil {
		return snapshot, err
	}
	if selectedPrice != quotePrice {
		return snapshot, ErrConflict
	}
	oldTerms, err := loadPriceTerms(ctx, tx, fromPrice)
	if err != nil {
		return snapshot, err
	}
	newTerms, err := loadPriceTerms(ctx, tx, selectedPrice)
	if err != nil {
		return snapshot, err
	}
	if oldTerms.Currency != newTerms.Currency {
		return snapshot, ErrConflict
	}
	oldAmount, err := oldTerms.UpfrontMinor(currentSeats)
	if err != nil {
		return snapshot, err
	}
	newAmount, err := newTerms.UpfrontMinor(seats)
	if err != nil {
		return snapshot, err
	}
	oldCredit, newCharge, net, err := proratedUpgradeAmounts(oldAmount, newAmount, periodEnd-at.UnixNano(), periodEnd-periodStart)
	if err != nil {
		return snapshot, err
	}
	snapshot.Seats, snapshot.QuoteExpiry, snapshot.NetMinor = seats, time.Unix(0, quoteExpiry).UTC(), net
	snapshot.Sources, err = json.Marshal(map[string]string{
		"subscription_revision": input.Revision, "subscription_status": status,
		"from_price_version_id": fromPrice, "current_seats": strconv.FormatInt(currentSeats, 10),
		"quote_id": input.QuoteID, "quote_fingerprint": quoteFingerprint,
		"binding_fingerprint": savedFingerprint, "target_price_version_id": selectedPrice,
		"target_price_checksum": priceChecksum, "period_start": strconv.FormatInt(periodStart, 10),
		"period_end": strconv.FormatInt(periodEnd, 10), "period_invoice_id": periodInvoice,
		"period_invoice_outstanding_minor": strconv.FormatInt(balance.OutstandingMinor, 10),
	})
	if err != nil {
		return immediateUpgradeSnapshot{}, err
	}
	snapshot.Impact, err = json.Marshal(map[string]string{
		"subscription_id": subID, "quote_id": input.QuoteID, "target_price_version_id": selectedPrice,
		"seats": strconv.FormatInt(seats, 10), "old_credit_minor": strconv.FormatInt(oldCredit, 10),
		"new_charge_minor": strconv.FormatInt(newCharge, 10), "net_minor": strconv.FormatInt(net, 10),
		"currency": oldTerms.Currency, "period_end": time.Unix(0, periodEnd).UTC().Format(time.RFC3339Nano),
	})
	return snapshot, err
}

func (l *Lab) adminCreateImmediateUpgradePreview(ctx context.Context, actorID, subID string, canonical []byte) (AdminPreview, error) {
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
	snapshot, err := l.loadImmediateUpgradeSnapshot(ctx, tx, businessAt, subID, input)
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
	_, err = tx.ExecContext(ctx, `INSERT INTO admin_previews(id,actor_id,action_id,target_id,intent_hash,intent_json,source_versions_json,impact_json,expires_at,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, id, actorID, "C04", subID, adminIntentHash("C04", subID, canonical), string(canonical), string(snapshot.Sources), string(snapshot.Impact), expires.Format(time.RFC3339Nano), created.Format(time.RFC3339Nano))
	if err != nil {
		return AdminPreview{}, err
	}
	if err := tx.Commit(); err != nil {
		return AdminPreview{}, err
	}
	return AdminPreview{ID: id, ActorID: actorID, ActionID: "C04", TargetID: subID, ExpiresAt: expires.Format(time.RFC3339Nano), SourceVersions: snapshot.Sources, Impact: snapshot.Impact, BlockingReasons: []string{}}, nil
}

func (l *Lab) executeImmediateUpgradeTx(ctx context.Context, tx *sql.Tx, commandID, previewID, subID, payloadJSON, businessTime string) (any, error) {
	var input AdminChangePlanPayload
	if err := decodeAdminPayload(json.RawMessage(payloadJSON), &input); err != nil {
		return nil, err
	}
	var storedSources, storedImpact string
	if err := tx.QueryRowContext(ctx, `SELECT source_versions_json,impact_json FROM admin_previews WHERE id=? AND claimed_command_id=?`, previewID, commandID).Scan(&storedSources, &storedImpact); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrAdminPreviewStale
		}
		return nil, err
	}
	at, err := time.Parse(time.RFC3339Nano, businessTime)
	if err != nil {
		return nil, err
	}
	snapshot, err := l.loadImmediateUpgradeSnapshot(ctx, tx, at, subID, input)
	if errors.Is(err, ErrConflict) || errors.Is(err, sql.ErrNoRows) || errors.Is(err, ErrPeriodEnded) {
		return nil, ErrAdminPreviewStale
	}
	if err != nil {
		return nil, err
	}
	if storedSources != string(snapshot.Sources) {
		return nil, ErrAdminPreviewStale
	}
	var impact map[string]string
	if err := json.Unmarshal([]byte(storedImpact), &impact); err != nil {
		return nil, err
	}
	maxNet, err := strconv.ParseInt(impact["net_minor"], 10, 64)
	if err != nil {
		return nil, err
	}
	if snapshot.NetMinor > maxNet {
		return nil, ErrAdminPreviewStale
	}
	revision, err := strconv.ParseInt(input.Revision, 10, 64)
	if err != nil {
		return nil, err
	}
	change, err := l.requestImmediateProUpgradeTx(ctx, tx, at, subID, snapshot.Seats, revision, "admin:"+commandID, input.QuoteID, input.Fingerprint)
	if errors.Is(err, ErrConflict) || errors.Is(err, sql.ErrNoRows) || errors.Is(err, ErrPeriodEnded) {
		return nil, ErrAdminPreviewStale
	}
	if err != nil {
		return nil, err
	}
	if change.QuotedAmountMinor != snapshot.NetMinor {
		return nil, ErrAdminPreviewStale
	}
	return map[string]string{
		"change_id": change.ID, "invoice_id": change.InvoiceID, "operation_id": change.OperationID,
		"subscription_id": subID, "target_price_version_id": change.TargetPriceVersionID,
		"net_minor": strconv.FormatInt(change.QuotedAmountMinor, 10), "currency": impact["currency"],
	}, nil
}
