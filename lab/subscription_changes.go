package lab

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type ScheduleResult struct {
	ID                   string
	SubscriptionID       string
	Kind                 string
	TargetPriceVersionID string
	SeatQuantity         int64
	EffectiveAt          time.Time
	Status               string
	Revision             int64
}

func migrateSubscriptionSchedules(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS subscription_schedules (
 id TEXT PRIMARY KEY,
 subscription_id TEXT NOT NULL REFERENCES subscriptions(id),
 kind TEXT NOT NULL CHECK(kind IN ('change','cancel')),
 target_price_version_id TEXT REFERENCES price_versions(id),
 seat_quantity INTEGER NOT NULL DEFAULT 0 CHECK(seat_quantity>=0),
 effective_at INTEGER NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('scheduled','applied','cancelled')),
 request_key TEXT NOT NULL UNIQUE,
 created_revision INTEGER NOT NULL,
 created_at INTEGER NOT NULL,
 applied_at INTEGER,
 CHECK((kind='cancel' AND target_price_version_id IS NULL AND seat_quantity=0) OR
       (kind='change' AND target_price_version_id IS NOT NULL)));
CREATE UNIQUE INDEX IF NOT EXISTS one_pending_schedule ON subscription_schedules(subscription_id) WHERE status='scheduled';
CREATE TABLE IF NOT EXISTS subscription_commands (
 request_key TEXT PRIMARY KEY,
 kind TEXT NOT NULL,
 subscription_id TEXT NOT NULL REFERENCES subscriptions(id),
 payload_hash TEXT NOT NULL,
 schedule_id TEXT NOT NULL REFERENCES subscription_schedules(id),
 result_revision INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS subscription_ends (
 subscription_id TEXT PRIMARY KEY REFERENCES subscriptions(id),
 schedule_id TEXT NOT NULL UNIQUE REFERENCES subscription_schedules(id),
 ended_at INTEGER NOT NULL);
CREATE TRIGGER IF NOT EXISTS subscription_end_immutable_update BEFORE UPDATE ON subscription_ends BEGIN SELECT RAISE(ABORT,'subscription end immutable'); END;
CREATE TRIGGER IF NOT EXISTS subscription_end_immutable_delete BEFORE DELETE ON subscription_ends BEGIN SELECT RAISE(ABORT,'subscription end immutable'); END;
CREATE TABLE IF NOT EXISTS pricing_assignments (
 subscription_id TEXT NOT NULL REFERENCES subscriptions(id),
 assignment_index INTEGER NOT NULL CHECK(assignment_index>=0),
 price_version_id TEXT NOT NULL REFERENCES price_versions(id),
 seat_quantity INTEGER NOT NULL CHECK(seat_quantity>=0),
 effective_start INTEGER NOT NULL,
 effective_end INTEGER,
 source_schedule_id TEXT REFERENCES subscription_schedules(id),
 PRIMARY KEY(subscription_id,assignment_index),
 CHECK(effective_end IS NULL OR effective_end>effective_start));
CREATE TRIGGER IF NOT EXISTS assignment_no_overlap BEFORE INSERT ON pricing_assignments
WHEN EXISTS(SELECT 1 FROM pricing_assignments a WHERE a.subscription_id=NEW.subscription_id
 AND (a.effective_end IS NULL OR a.effective_end>NEW.effective_start)
 AND (NEW.effective_end IS NULL OR NEW.effective_end>a.effective_start))
BEGIN SELECT RAISE(ABORT,'pricing assignment overlap'); END;`); err != nil {
		return err
	}
	_, err := db.Exec(`INSERT INTO pricing_assignments(subscription_id,assignment_index,price_version_id,seat_quantity,effective_start)
SELECT s.id,0,s.price_version_id,s.seat_quantity,p.period_start FROM subscriptions s
JOIN billing_periods p ON p.subscription_id=s.id AND p.period_index=0
WHERE NOT EXISTS(SELECT 1 FROM pricing_assignments a WHERE a.subscription_id=s.id)`)
	return err
}

func loadSchedule(ctx context.Context, tx *sql.Tx, scheduleID string) (ScheduleResult, error) {
	var r ScheduleResult
	var effective int64
	err := tx.QueryRowContext(ctx, `SELECT id,subscription_id,kind,COALESCE(target_price_version_id,''),seat_quantity,effective_at,status FROM subscription_schedules WHERE id=?`, scheduleID).
		Scan(&r.ID, &r.SubscriptionID, &r.Kind, &r.TargetPriceVersionID, &r.SeatQuantity, &effective, &r.Status)
	if err != nil {
		return ScheduleResult{}, err
	}
	r.EffectiveAt = time.Unix(0, effective).UTC()
	return r, nil
}

func commandReplay(ctx context.Context, tx *sql.Tx, key, kind, subID, payloadHash string) (ScheduleResult, bool, error) {
	var savedKind, savedSub, savedHash, scheduleID string
	var revision int64
	err := tx.QueryRowContext(ctx, `SELECT kind,subscription_id,payload_hash,schedule_id,result_revision FROM subscription_commands WHERE request_key=?`, key).
		Scan(&savedKind, &savedSub, &savedHash, &scheduleID, &revision)
	if errors.Is(err, sql.ErrNoRows) {
		return ScheduleResult{}, false, nil
	}
	if err != nil {
		return ScheduleResult{}, false, err
	}
	if savedKind != kind || savedSub != subID || savedHash != payloadHash {
		return ScheduleResult{}, true, ErrConflict
	}
	r, err := loadSchedule(ctx, tx, scheduleID)
	r.Revision = revision
	return r, true, err
}

func currentPeriodEnd(ctx context.Context, tx *sql.Tx, subID string, now time.Time) (int64, error) {
	var end int64
	err := tx.QueryRowContext(ctx, `SELECT period_end FROM billing_periods WHERE subscription_id=? AND period_start<=? AND period_end>? ORDER BY period_index DESC LIMIT 1`, subID, now.UnixNano(), now.UnixNano()).Scan(&end)
	return end, err
}

func (l *Lab) ScheduleNextPlan(ctx context.Context, subID, planID string, seats, expectedRevision int64, requestKey string) (ScheduleResult, error) {
	return l.scheduleNextPlan(ctx, subID, planID, seats, expectedRevision, requestKey, "", "")
}

func (l *Lab) ScheduleNextPlanAtPrice(ctx context.Context, subID, planID string, seats, expectedRevision int64, requestKey, quoteID, fingerprint string) (ScheduleResult, error) {
	if quoteID == "" || fingerprint == "" {
		return ScheduleResult{}, ErrConflict
	}
	return l.scheduleNextPlan(ctx, subID, planID, seats, expectedRevision, requestKey, quoteID, fingerprint)
}

func (l *Lab) scheduleNextPlan(ctx context.Context, subID, planID string, seats, expectedRevision int64, requestKey, quoteID, fingerprint string) (ScheduleResult, error) {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return ScheduleResult{}, err
	}
	defer tx.Rollback()
	result, err := l.scheduleNextPlanTx(ctx, tx, l.now().UTC(), subID, planID, seats, expectedRevision, requestKey, quoteID, fingerprint)
	if err != nil {
		return ScheduleResult{}, err
	}
	return result, tx.Commit()
}

func (l *Lab) scheduleNextPlanTx(ctx context.Context, tx *sql.Tx, now time.Time, subID, planID string, seats, expectedRevision int64, requestKey, quoteID, fingerprint string) (ScheduleResult, error) {
	if subID == "" || planID == "" || requestKey == "" || expectedRevision <= 0 {
		return ScheduleResult{}, errors.New("subscription, plan, revision and request key are required")
	}
	payloadHash := hash(subID, planID, seats, expectedRevision)
	if quoteID != "" {
		payloadHash = hash(subID, planID, seats, expectedRevision, quoteID, fingerprint)
	}
	if replay, found, err := commandReplay(ctx, tx, requestKey, "change", subID, payloadHash); found || err != nil {
		return replay, err
	}
	var commandCustomer string
	if err := tx.QueryRowContext(ctx, `SELECT customer_id FROM subscriptions WHERE id=?`, subID).Scan(&commandCustomer); err != nil {
		return ScheduleResult{}, err
	}
	if err := ensureCommerceWriter(ctx, tx, commandCustomer); err != nil {
		return ScheduleResult{}, err
	}
	quotedPriceVersionID := ""
	if quoteID != "" {
		var boundSub, mode, savedFingerprint string
		var boundRevision, expiry, quotedSeats int64
		if err := tx.QueryRowContext(ctx, `SELECT q.price_version_id,q.expires_at,q.seat_quantity,b.subscription_id,b.mode,b.expected_revision,b.fingerprint FROM quotes q JOIN change_quote_bindings b ON b.quote_id=q.id WHERE q.id=?`, quoteID).Scan(&quotedPriceVersionID, &expiry, &quotedSeats, &boundSub, &mode, &boundRevision, &savedFingerprint); err != nil {
			return ScheduleResult{}, err
		}
		if boundSub != subID || mode != "next_period" || boundRevision != expectedRevision || quotedSeats != seats || savedFingerprint != fingerprint || !now.Before(time.Unix(0, expiry)) {
			return ScheduleResult{}, ErrConflict
		}
	}
	effective, err := currentPeriodEnd(ctx, tx, subID, now)
	if err != nil {
		return ScheduleResult{}, err
	}
	var currentRevision int64
	var status string
	err = tx.QueryRowContext(ctx, `SELECT revision,status FROM subscriptions WHERE id=?`, subID).Scan(&currentRevision, &status)
	if err != nil {
		return ScheduleResult{}, err
	}
	if status != "active" || currentRevision != expectedRevision {
		return ScheduleResult{}, ErrConflict
	}
	var enterprise int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM contract_subscriptions c WHERE c.subscription_id=? AND NOT EXISTS(SELECT 1 FROM contract_transitions t WHERE t.subscription_id=c.subscription_id)`, subID).Scan(&enterprise); err != nil || enterprise != 0 {
		return ScheduleResult{}, ErrConflict
	}
	var pending int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM subscription_schedules WHERE subscription_id=? AND status='scheduled'`, subID).Scan(&pending); err != nil {
		return ScheduleResult{}, err
	}
	if pending != 0 {
		return ScheduleResult{}, ErrConflict
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM immediate_changes WHERE subscription_id=? AND status='requested'`, subID).Scan(&pending); err != nil || pending != 0 {
		return ScheduleResult{}, ErrConflict
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM price_migration_items WHERE subscription_id=? AND status IN ('pending','conflicted')`, subID).Scan(&pending); err != nil || pending != 0 {
		return ScheduleResult{}, ErrConflict
	}
	var ended int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM subscription_ends WHERE subscription_id=?`, subID).Scan(&ended); err != nil || ended != 0 {
		return ScheduleResult{}, ErrConflict
	}
	var target string
	err = tx.QueryRowContext(ctx, `SELECT p.id FROM catalog_selection c JOIN price_versions p ON p.id=c.price_version_id
WHERE c.plan_id=? AND c.cohort='default' AND c.effective_at<=? AND p.publication_state='published'
AND p.effective_from<=? AND (p.effective_to IS NULL OR p.effective_to>?)
ORDER BY c.effective_at DESC LIMIT 1`, planID, effective, effective, effective).Scan(&target)
	if err != nil {
		return ScheduleResult{}, err
	}
	if quotedPriceVersionID != "" && target != quotedPriceVersionID {
		return ScheduleResult{}, ErrConflict
	}
	terms, err := loadPriceTerms(ctx, tx, target)
	if err != nil {
		return ScheduleResult{}, err
	}
	if _, err := terms.UpfrontMinor(seats); err != nil {
		return ScheduleResult{}, err
	}
	id, err := newID("schedule_")
	if err != nil {
		return ScheduleResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO subscription_schedules(id,subscription_id,kind,target_price_version_id,seat_quantity,effective_at,status,request_key,created_revision,created_at) VALUES(?,?,'change',?,?,?,'scheduled',?,?,?)`, id, subID, target, seats, effective, requestKey, expectedRevision, now.UnixNano()); err != nil {
		return ScheduleResult{}, err
	}
	update, err := tx.ExecContext(ctx, `UPDATE subscriptions SET revision=revision+1 WHERE id=? AND revision=?`, subID, expectedRevision)
	if err != nil {
		return ScheduleResult{}, err
	}
	if n, err := update.RowsAffected(); err != nil || n != 1 {
		return ScheduleResult{}, ErrConflict
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO subscription_commands(request_key,kind,subscription_id,payload_hash,schedule_id,result_revision) VALUES(?,'change',?,?,?,?)`, requestKey, subID, payloadHash, id, expectedRevision+1); err != nil {
		return ScheduleResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_events(kind,object_id,at) VALUES('change_scheduled',?,?)`, id, now.UnixNano()); err != nil {
		return ScheduleResult{}, err
	}
	return ScheduleResult{ID: id, SubscriptionID: subID, Kind: "change", TargetPriceVersionID: target, SeatQuantity: seats, EffectiveAt: time.Unix(0, effective).UTC(), Status: "scheduled", Revision: expectedRevision + 1}, nil
}

func (l *Lab) ScheduleCancel(ctx context.Context, subID string, expectedRevision int64, requestKey string) (ScheduleResult, error) {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return ScheduleResult{}, err
	}
	defer tx.Rollback()
	result, err := l.scheduleCancelTx(ctx, tx, l.now().UTC(), subID, expectedRevision, requestKey)
	if err != nil {
		return ScheduleResult{}, err
	}
	return result, tx.Commit()
}

func (l *Lab) scheduleCancelTx(ctx context.Context, tx *sql.Tx, at time.Time, subID string, expectedRevision int64, requestKey string) (ScheduleResult, error) {
	if subID == "" || requestKey == "" || expectedRevision <= 0 {
		return ScheduleResult{}, errors.New("subscription, revision and request key are required")
	}
	payloadHash := hash(subID, "cancel", expectedRevision)
	if replay, found, err := commandReplay(ctx, tx, requestKey, "cancel", subID, payloadHash); found || err != nil {
		return replay, err
	}
	if err := ensureCommerceSubscriber(ctx, tx, subID); err != nil {
		return ScheduleResult{}, err
	}
	now := at.UTC()
	effective, err := currentPeriodEnd(ctx, tx, subID, now)
	if err != nil {
		return ScheduleResult{}, err
	}
	var revision int64
	var status string
	if err := tx.QueryRowContext(ctx, `SELECT revision,status FROM subscriptions WHERE id=?`, subID).Scan(&revision, &status); err != nil {
		return ScheduleResult{}, err
	}
	if status != "active" || revision != expectedRevision {
		return ScheduleResult{}, ErrConflict
	}
	var enterprise int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM contract_subscriptions c WHERE c.subscription_id=? AND NOT EXISTS(SELECT 1 FROM contract_transitions t WHERE t.subscription_id=c.subscription_id)`, subID).Scan(&enterprise); err != nil || enterprise != 0 {
		return ScheduleResult{}, ErrConflict
	}
	var pending int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM subscription_schedules WHERE subscription_id=? AND status='scheduled'`, subID).Scan(&pending); err != nil {
		return ScheduleResult{}, err
	}
	if pending != 0 {
		return ScheduleResult{}, ErrConflict
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM immediate_changes WHERE subscription_id=? AND status='requested'`, subID).Scan(&pending); err != nil || pending != 0 {
		return ScheduleResult{}, ErrConflict
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM price_migration_items WHERE subscription_id=? AND status IN ('pending','conflicted')`, subID).Scan(&pending); err != nil || pending != 0 {
		return ScheduleResult{}, ErrConflict
	}
	id, err := newID("schedule_")
	if err != nil {
		return ScheduleResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO subscription_schedules(id,subscription_id,kind,seat_quantity,effective_at,status,request_key,created_revision,created_at) VALUES(?,?,'cancel',0,?,'scheduled',?,?,?)`, id, subID, effective, requestKey, expectedRevision, now.UnixNano()); err != nil {
		return ScheduleResult{}, err
	}
	update, err := tx.ExecContext(ctx, `UPDATE subscriptions SET revision=revision+1 WHERE id=? AND revision=?`, subID, expectedRevision)
	if err != nil {
		return ScheduleResult{}, err
	}
	if n, err := update.RowsAffected(); err != nil || n != 1 {
		return ScheduleResult{}, ErrConflict
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO subscription_commands(request_key,kind,subscription_id,payload_hash,schedule_id,result_revision) VALUES(?,'cancel',?,?,?,?)`, requestKey, subID, payloadHash, id, expectedRevision+1); err != nil {
		return ScheduleResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_events(kind,object_id,at) VALUES('cancel_scheduled',?,?)`, id, now.UnixNano()); err != nil {
		return ScheduleResult{}, err
	}
	return ScheduleResult{ID: id, SubscriptionID: subID, Kind: "cancel", EffectiveAt: time.Unix(0, effective).UTC(), Status: "scheduled", Revision: expectedRevision + 1}, nil
}

func (l *Lab) ResumeCancel(ctx context.Context, subID string, expectedRevision int64, requestKey string) (ScheduleResult, error) {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return ScheduleResult{}, err
	}
	defer tx.Rollback()
	result, err := l.resumeCancelTx(ctx, tx, l.now().UTC(), subID, expectedRevision, requestKey)
	if err != nil {
		return ScheduleResult{}, err
	}
	return result, tx.Commit()
}

func (l *Lab) resumeCancelTx(ctx context.Context, tx *sql.Tx, at time.Time, subID string, expectedRevision int64, requestKey string) (ScheduleResult, error) {
	if subID == "" || requestKey == "" || expectedRevision <= 0 {
		return ScheduleResult{}, errors.New("subscription, revision and request key are required")
	}
	payloadHash := hash(subID, "resume", expectedRevision)
	if replay, found, err := commandReplay(ctx, tx, requestKey, "resume", subID, payloadHash); found || err != nil {
		return replay, err
	}
	if err := ensureCommerceSubscriber(ctx, tx, subID); err != nil {
		return ScheduleResult{}, err
	}
	var id string
	var effective int64
	var err error
	err = tx.QueryRowContext(ctx, `SELECT id,effective_at FROM subscription_schedules WHERE subscription_id=? AND kind='cancel' AND status='scheduled'`, subID).Scan(&id, &effective)
	if err != nil {
		return ScheduleResult{}, err
	}
	if at.UTC().UnixNano() >= effective {
		return ScheduleResult{}, ErrPeriodEnded
	}
	update, err := tx.ExecContext(ctx, `UPDATE subscriptions SET revision=revision+1 WHERE id=? AND status='active' AND revision=?`, subID, expectedRevision)
	if err != nil {
		return ScheduleResult{}, err
	}
	if n, err := update.RowsAffected(); err != nil || n != 1 {
		return ScheduleResult{}, ErrConflict
	}
	if _, err := tx.ExecContext(ctx, `UPDATE subscription_schedules SET status='cancelled',applied_at=? WHERE id=? AND status='scheduled'`, at.UTC().UnixNano(), id); err != nil {
		return ScheduleResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO subscription_commands(request_key,kind,subscription_id,payload_hash,schedule_id,result_revision) VALUES(?,'resume',?,?,?,?)`, requestKey, subID, payloadHash, id, expectedRevision+1); err != nil {
		return ScheduleResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_events(kind,object_id,at) VALUES('cancel_resumed',?,?)`, id, at.UTC().UnixNano()); err != nil {
		return ScheduleResult{}, err
	}
	return ScheduleResult{ID: id, SubscriptionID: subID, Kind: "cancel", EffectiveAt: time.Unix(0, effective).UTC(), Status: "cancelled", Revision: expectedRevision + 1}, nil
}

func scheduleForBoundary(ctx context.Context, tx *sql.Tx, subID string, boundary int64) (ScheduleResult, bool, error) {
	var id string
	err := tx.QueryRowContext(ctx, `SELECT id FROM subscription_schedules WHERE subscription_id=? AND status='scheduled' AND effective_at=?`, subID, boundary).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return ScheduleResult{}, false, nil
	}
	if err != nil {
		return ScheduleResult{}, false, err
	}
	r, err := loadSchedule(ctx, tx, id)
	return r, true, err
}

func applyBoundaryChange(ctx context.Context, tx *sql.Tx, schedule ScheduleResult, now time.Time) error {
	var revision int64
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM subscriptions WHERE id=?`, schedule.SubscriptionID).Scan(&revision); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE pricing_assignments SET effective_end=? WHERE subscription_id=? AND effective_end IS NULL`, schedule.EffectiveAt.UnixNano(), schedule.SubscriptionID); err != nil {
		return err
	}
	var nextIndex int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(assignment_index),-1)+1 FROM pricing_assignments WHERE subscription_id=?`, schedule.SubscriptionID).Scan(&nextIndex); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO pricing_assignments(subscription_id,assignment_index,price_version_id,seat_quantity,effective_start,source_schedule_id) VALUES(?,?,?,?,?,?)`, schedule.SubscriptionID, nextIndex, schedule.TargetPriceVersionID, schedule.SeatQuantity, schedule.EffectiveAt.UnixNano(), schedule.ID); err != nil {
		return err
	}
	update, err := tx.ExecContext(ctx, `UPDATE subscriptions SET price_version_id=?,seat_quantity=?,revision=revision+1 WHERE id=? AND revision=?`, schedule.TargetPriceVersionID, schedule.SeatQuantity, schedule.SubscriptionID, revision)
	if err != nil {
		return err
	}
	if n, err := update.RowsAffected(); err != nil || n != 1 {
		return ErrConflict
	}
	if _, err := tx.ExecContext(ctx, `UPDATE subscription_schedules SET status='applied',applied_at=? WHERE id=? AND status='scheduled'`, now.UnixNano(), schedule.ID); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO audit_events(kind,object_id,at) VALUES('change_applied',?,?)`, schedule.ID, now.UnixNano())
	return err
}

func applyBoundaryCancel(ctx context.Context, tx *sql.Tx, schedule ScheduleResult, now time.Time) error {
	if now.Before(schedule.EffectiveAt) {
		return fmt.Errorf("cancel boundary has not arrived")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO subscription_ends(subscription_id,schedule_id,ended_at) VALUES(?,?,?)`, schedule.SubscriptionID, schedule.ID, schedule.EffectiveAt.UnixNano()); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE pricing_assignments SET effective_end=? WHERE subscription_id=? AND effective_end IS NULL`, schedule.EffectiveAt.UnixNano(), schedule.SubscriptionID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE subscriptions SET revision=revision+1 WHERE id=?`, schedule.SubscriptionID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE subscription_schedules SET status='applied',applied_at=? WHERE id=? AND status='scheduled'`, now.UnixNano(), schedule.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO outbox(id,kind,object_id,status) VALUES(?,'entitlement',?,'pending')`, "entitlement:end:"+schedule.ID, schedule.SubscriptionID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO audit_events(kind,object_id,at) VALUES('cancel_applied',?,?)`, schedule.ID, now.UnixNano())
	return err
}
