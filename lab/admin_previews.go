package lab

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"
)

type AdminPreview struct {
	ID              string          `json:"preview_id"`
	ActorID         string          `json:"actor_id"`
	ActionID        string          `json:"action_id"`
	TargetID        string          `json:"target_id"`
	ExpiresAt       string          `json:"expires_at"`
	SourceVersions  json.RawMessage `json:"source_versions"`
	Impact          json.RawMessage `json:"impact"`
	BlockingReasons []string        `json:"blocking_reasons"`
}

func adminIntentHash(actionID, targetID string, canonical []byte) string {
	encoded, _ := json.Marshal([]any{actionID, targetID, json.RawMessage(canonical)})
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

// adminPreviewExpiry keeps the preview TTL on real time while respecting the
// remaining quote lifetime measured by the business clock. A fixed business
// clock may be earlier or later than real time, so their absolute timestamps
// cannot be compared directly.
func adminPreviewExpiry(created, businessAt, businessDeadline time.Time) (time.Time, error) {
	remaining := businessDeadline.Sub(businessAt)
	if remaining <= 0 {
		return time.Time{}, ErrExpired
	}
	if remaining > 5*time.Minute {
		remaining = 5 * time.Minute
	}
	return created.Add(remaining), nil
}

func (l *Lab) AdminCreatePreview(ctx context.Context, actorID, actionID, targetID string, payload json.RawMessage) (AdminPreview, error) {
	canonical, err := canonicalAdminPayload(actionID, payload)
	if err != nil {
		if errors.Is(err, ErrAdminUnsupportedAction) {
			return AdminPreview{}, err
		}
		return AdminPreview{}, fmt.Errorf("%w: %v", ErrAdminInvalidCommand, err)
	}
	if actionID == "C05" {
		return l.adminCreateCancelPreview(ctx, actorID, targetID, canonical)
	}
	if actionID == "C03" {
		return l.adminCreateSchedulePlanPreview(ctx, actorID, targetID, canonical)
	}
	if actionID == "C04" {
		return l.adminCreateImmediateUpgradePreview(ctx, actorID, targetID, canonical)
	}
	if actionID == "C07" {
		return l.adminCreatePaymentPreview(ctx, actorID, targetID, canonical)
	}
	if actionID == "C08" {
		return l.adminCreateRetryPaymentPreview(ctx, actorID, targetID, canonical)
	}
	if actionID == "C11" {
		return l.adminCreateReductionPreview(ctx, actorID, targetID, canonical)
	}
	if actionID == "C12" {
		return l.adminCreateApplyCreditPreview(ctx, actorID, targetID, canonical)
	}
	if actionID == "C13" {
		return l.adminCreateChangeCorrectionsPreview(ctx, actorID, canonical)
	}
	if actionID == "C14" {
		return l.adminCreateUnfulfilledPreview(ctx, actorID, targetID, canonical)
	}
	if actionID == "C15" {
		return l.adminCreateReserveRefundPreview(ctx, actorID, targetID, canonical)
	}
	if actionID == "C09" || actionID == "C16" {
		return l.adminCreateExternalDispatchPreview(ctx, actorID, actionID, targetID, canonical)
	}
	if actionID == "C18" || actionID == "C19" || actionID == "C20" || actionID == "C21" {
		return l.adminCreateCatalogPreview(ctx, actorID, actionID, targetID, canonical)
	}
	if actionID == "C22" || actionID == "C24" || actionID == "C25" {
		return l.adminCreateMigrationPreview(ctx, actorID, actionID, targetID, canonical)
	}
	if actionID == "C27" || actionID == "C28" || actionID == "C29" {
		return l.adminCreateUsagePreview(ctx, actorID, actionID, targetID, canonical)
	}
	if actionID == "C30" {
		return l.adminCreateUsageCreditNotesPreview(ctx, actorID, targetID, canonical)
	}
	if actionID == "C31" {
		return l.adminCreateContractPreview(ctx, actorID, targetID, canonical)
	}
	if actionID == "C32" {
		return l.adminCreateContractCollectionsPreview(ctx, actorID, targetID, canonical)
	}
	if actionID == "C34" {
		return l.adminCreateRepairPreview(ctx, actorID, targetID, canonical)
	}
	if actionID == "C35" {
		return l.adminCreateManualDecisionPreview(ctx, actorID, targetID, canonical)
	}
	if actionID == "C36" {
		return l.adminCreateAccountLinkPreview(ctx, actorID, targetID, canonical)
	}
	if actionID == "C39" || actionID == "C40" {
		return l.adminCreateProvenancePreview(ctx, actorID, actionID, targetID, canonical)
	}
	if actionID == "C41" || actionID == "C42" || actionID == "C43" {
		return l.adminCreateCutoverPreview(ctx, actorID, actionID, targetID, canonical)
	}
	if actionID == "C44" || actionID == "C45" {
		return l.adminCreateMaintenancePreview(ctx, actorID, actionID, targetID, canonical)
	}
	if actionID == "C46" || actionID == "C47" || actionID == "C48" || actionID == "C49" {
		return l.adminCreateControlPreview(ctx, actorID, actionID, targetID, canonical)
	}
	if actionID == "C06" {
		return l.adminCreateResumePreview(ctx, actorID, targetID, canonical)
	}
	if actionID != "C02" {
		return AdminPreview{}, ErrAdminUnsupportedAction
	}
	if actorID == "" || targetID == "" {
		return AdminPreview{}, ErrAdminInvalidCommand
	}
	var quoteAmount int64
	var customerID, currency, fingerprint, priceID, priceChecksum string
	var contractID, contractChecksum sql.NullString
	var expiresNano int64
	err = l.db.QueryRowContext(ctx, `SELECT q.customer_id,q.amount_minor,q.currency,q.expires_at,q.fingerprint,q.price_version_id,p.checksum,cv.id,cv.checksum FROM quotes q JOIN price_versions p ON p.id=q.price_version_id LEFT JOIN contract_quotes cq ON cq.quote_id=q.id LEFT JOIN contract_versions cv ON cv.id=cq.contract_version_id WHERE q.id=?`, targetID).Scan(&customerID, &quoteAmount, &currency, &expiresNano, &fingerprint, &priceID, &priceChecksum, &contractID, &contractChecksum)
	if err != nil {
		return AdminPreview{}, err
	}
	if err := ensureCommerceWriter(ctx, l.db, customerID); err != nil {
		return AdminPreview{}, err
	}
	var input AdminAcceptQuotePayload
	if err := decodeAdminPayload(canonical, &input); err != nil {
		return AdminPreview{}, err
	}
	if input.Fingerprint != fingerprint {
		return AdminPreview{}, ErrConflict
	}
	var accepted int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM subscriptions WHERE quote_id=?`, targetID).Scan(&accepted); err != nil {
		return AdminPreview{}, err
	}
	if accepted != 0 {
		return AdminPreview{}, ErrConflict
	}
	var boundChange int
	if err := l.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM change_quote_bindings WHERE quote_id=?)`, targetID).Scan(&boundChange); err != nil {
		return AdminPreview{}, err
	}
	if boundChange != 0 {
		return AdminPreview{}, ErrConflict
	}
	quoteExpiry := time.Unix(0, expiresNano).UTC()
	businessNow := l.ClockTime()
	created := time.Now().UTC()
	expires, err := adminPreviewExpiry(created, businessNow, quoteExpiry)
	if err != nil {
		return AdminPreview{}, err
	}
	sources, err := json.Marshal(map[string]string{"quote_fingerprint": fingerprint, "price_version_id": priceID, "price_checksum": priceChecksum, "contract_version_id": contractID.String, "contract_checksum": contractChecksum.String})
	if err != nil {
		return AdminPreview{}, err
	}
	impactFields := map[string]string{"quote_id": targetID, "amount_minor": strconv.FormatInt(quoteAmount, 10), "currency": currency, "price_version_id": priceID, "contract_version_id": contractID.String, "payment_terms": map[bool]string{true: "net30", false: "immediate"}[contractID.Valid]}
	if contractID.Valid {
		impactFields["due_now_minor"] = "0"
		impactFields["first_due_at_estimated"] = businessNow.Add(30 * 24 * time.Hour).Format(time.RFC3339Nano)
	}
	impact, err := json.Marshal(impactFields)
	if err != nil {
		return AdminPreview{}, err
	}
	id, err := newID("prev_")
	if err != nil {
		return AdminPreview{}, err
	}
	now := created.Format(time.RFC3339Nano)
	_, err = l.db.ExecContext(ctx, `INSERT INTO admin_previews(id,actor_id,action_id,target_id,intent_hash,intent_json,source_versions_json,impact_json,expires_at,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, id, actorID, actionID, targetID, adminIntentHash(actionID, targetID, canonical), string(canonical), string(sources), string(impact), expires.Format(time.RFC3339Nano), now)
	if err != nil {
		return AdminPreview{}, err
	}
	return AdminPreview{ID: id, ActorID: actorID, ActionID: actionID, TargetID: targetID, ExpiresAt: expires.Format(time.RFC3339Nano), SourceVersions: sources, Impact: impact, BlockingReasons: []string{}}, nil
}

func (l *Lab) adminCreateCancelPreview(ctx context.Context, actorID, subID string, canonical []byte) (AdminPreview, error) {
	if actorID == "" || subID == "" {
		return AdminPreview{}, ErrAdminInvalidCommand
	}
	var input AdminRevisionPayload
	if err := decodeAdminPayload(canonical, &input); err != nil {
		return AdminPreview{}, err
	}
	if err := ensureCommerceSubscriber(ctx, l.db, subID); err != nil {
		return AdminPreview{}, err
	}
	var currentRevision int64
	var status string
	if err := l.db.QueryRowContext(ctx, `SELECT revision,status FROM subscriptions WHERE id=?`, subID).Scan(&currentRevision, &status); err != nil {
		return AdminPreview{}, err
	}
	if status != "active" || input.Revision != strconv.FormatInt(currentRevision, 10) {
		return AdminPreview{}, ErrConflict
	}
	if err := ensureNoUnresolvedPriceMigration(ctx, l.db, subID); err != nil {
		return AdminPreview{}, err
	}
	now := l.ClockTime()
	var periodEnd int64
	if err := l.db.QueryRowContext(ctx, `SELECT period_end FROM billing_periods WHERE subscription_id=? AND period_start<=? AND period_end>? ORDER BY period_index DESC LIMIT 1`, subID, now.UnixNano(), now.UnixNano()).Scan(&periodEnd); err != nil {
		return AdminPreview{}, err
	}
	sources, err := json.Marshal(map[string]string{"subscription_revision": input.Revision, "subscription_status": status, "period_end": strconv.FormatInt(periodEnd, 10)})
	if err != nil {
		return AdminPreview{}, err
	}
	impact, err := json.Marshal(map[string]string{"subscription_id": subID, "effective_at": time.Unix(0, periodEnd).UTC().Format(time.RFC3339Nano), "action": "schedule_cancel"})
	if err != nil {
		return AdminPreview{}, err
	}
	id, err := newID("prev_")
	if err != nil {
		return AdminPreview{}, err
	}
	created := time.Now().UTC()
	expires := created.Add(5 * time.Minute).Format(time.RFC3339Nano)
	_, err = l.db.ExecContext(ctx, `INSERT INTO admin_previews(id,actor_id,action_id,target_id,intent_hash,intent_json,source_versions_json,impact_json,expires_at,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, id, actorID, "C05", subID, adminIntentHash("C05", subID, canonical), string(canonical), string(sources), string(impact), expires, created.Format(time.RFC3339Nano))
	if err != nil {
		return AdminPreview{}, err
	}
	return AdminPreview{ID: id, ActorID: actorID, ActionID: "C05", TargetID: subID, ExpiresAt: expires, SourceVersions: sources, Impact: impact, BlockingReasons: []string{}}, nil
}

func (l *Lab) adminCreateResumePreview(ctx context.Context, actorID, subID string, canonical []byte) (AdminPreview, error) {
	if actorID == "" || subID == "" {
		return AdminPreview{}, ErrAdminInvalidCommand
	}
	var input AdminRevisionPayload
	if err := decodeAdminPayload(canonical, &input); err != nil {
		return AdminPreview{}, err
	}
	var revision int64
	var status string
	if err := l.db.QueryRowContext(ctx, `SELECT revision,status FROM subscriptions WHERE id=?`, subID).Scan(&revision, &status); err != nil {
		return AdminPreview{}, err
	}
	if err := ensureCommerceSubscriber(ctx, l.db, subID); err != nil {
		return AdminPreview{}, err
	}
	if status != "active" || input.Revision != strconv.FormatInt(revision, 10) {
		return AdminPreview{}, ErrConflict
	}
	var scheduleID string
	var effective int64
	if err := l.db.QueryRowContext(ctx, `SELECT id,effective_at FROM subscription_schedules WHERE subscription_id=? AND kind='cancel' AND status='scheduled'`, subID).Scan(&scheduleID, &effective); err != nil {
		return AdminPreview{}, err
	}
	if l.ClockTime().UnixNano() >= effective {
		return AdminPreview{}, ErrPeriodEnded
	}
	sources, err := json.Marshal(map[string]string{"subscription_revision": input.Revision, "subscription_status": status, "schedule_id": scheduleID, "effective_at": strconv.FormatInt(effective, 10)})
	if err != nil {
		return AdminPreview{}, err
	}
	impact, err := json.Marshal(map[string]string{"subscription_id": subID, "schedule_id": scheduleID, "action": "resume_cancel", "previous_cancel_at": time.Unix(0, effective).UTC().Format(time.RFC3339Nano)})
	if err != nil {
		return AdminPreview{}, err
	}
	id, err := newID("prev_")
	if err != nil {
		return AdminPreview{}, err
	}
	created := time.Now().UTC()
	expires := created.Add(5 * time.Minute).Format(time.RFC3339Nano)
	_, err = l.db.ExecContext(ctx, `INSERT INTO admin_previews(id,actor_id,action_id,target_id,intent_hash,intent_json,source_versions_json,impact_json,expires_at,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, id, actorID, "C06", subID, adminIntentHash("C06", subID, canonical), string(canonical), string(sources), string(impact), expires, created.Format(time.RFC3339Nano))
	if err != nil {
		return AdminPreview{}, err
	}
	return AdminPreview{ID: id, ActorID: actorID, ActionID: "C06", TargetID: subID, ExpiresAt: expires, SourceVersions: sources, Impact: impact, BlockingReasons: []string{}}, nil
}

func (l *Lab) AdminGetPreview(ctx context.Context, actorID, id string) (AdminPreview, error) {
	var p AdminPreview
	var sources, impact string
	err := l.db.QueryRowContext(ctx, `SELECT id,actor_id,action_id,target_id,expires_at,source_versions_json,impact_json FROM admin_previews WHERE id=?`, id).Scan(&p.ID, &p.ActorID, &p.ActionID, &p.TargetID, &p.ExpiresAt, &sources, &impact)
	if err != nil {
		return AdminPreview{}, err
	}
	if p.ActorID != actorID {
		return AdminPreview{}, sql.ErrNoRows
	}
	if !json.Valid([]byte(sources)) || !json.Valid([]byte(impact)) {
		return AdminPreview{}, errors.New("stored preview JSON invalid")
	}
	p.SourceVersions = json.RawMessage(sources)
	p.Impact = json.RawMessage(impact)
	p.BlockingReasons = []string{}
	return p, nil
}
