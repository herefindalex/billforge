package lab

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"time"
)

func ensureNoUnresolvedPriceMigration(ctx context.Context, q rowQuerier, subID string) error {
	var pending int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM price_migration_items WHERE subscription_id=? AND status IN ('pending','conflicted')`, subID).Scan(&pending); err != nil {
		return err
	}
	if pending != 0 {
		return ErrConflict
	}
	return nil
}

type PriceMigrationItem struct {
	MigrationID              string
	SubscriptionID           string
	FromPriceVersionID       string
	TargetPriceVersionID     string
	SeatQuantity             int64
	ExpectedRevision         int64
	PriorAmountMinor         int64
	TargetAmountMinor        int64
	CurrentEntitlementStatus string
	ProjectedEntitlementRule string
	EffectiveAt              time.Time
	Status                   string
	ConflictReason           string
}

type PriceMigration struct {
	ID                   string
	Cohort               string
	TargetPriceVersionID string
	Status               string
	Items                []PriceMigrationItem
}

func migratePriceMigrations(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS price_migrations (
id TEXT PRIMARY KEY,
cohort TEXT NOT NULL,
target_price_version_id TEXT NOT NULL REFERENCES price_versions(id),
payload_hash TEXT NOT NULL,
status TEXT NOT NULL CHECK(status IN ('active','paused','completed')),
created_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS price_migration_items (
migration_id TEXT NOT NULL REFERENCES price_migrations(id),
subscription_id TEXT NOT NULL REFERENCES subscriptions(id),
from_price_version_id TEXT NOT NULL REFERENCES price_versions(id),
target_price_version_id TEXT NOT NULL REFERENCES price_versions(id),
seat_quantity INTEGER NOT NULL CHECK(seat_quantity>=0),
expected_revision INTEGER NOT NULL CHECK(expected_revision>0),
prior_amount_minor INTEGER NOT NULL CHECK(prior_amount_minor>0),
target_amount_minor INTEGER NOT NULL CHECK(target_amount_minor>0),
current_entitlement_status TEXT NOT NULL,
projected_entitlement_rule TEXT NOT NULL,
effective_at INTEGER NOT NULL,
status TEXT NOT NULL CHECK(status IN ('pending','applied','conflicted','skipped')),
	conflict_reason TEXT,
	PRIMARY KEY(migration_id,subscription_id));
CREATE UNIQUE INDEX IF NOT EXISTS one_pending_price_migration ON price_migration_items(subscription_id) WHERE status IN ('pending','conflicted');`)
	if err != nil {
		return err
	}
	if err := ensureCatalogColumn(db, "pricing_assignments", "source_migration_id", `ALTER TABLE pricing_assignments ADD COLUMN source_migration_id TEXT REFERENCES price_migrations(id)`); err != nil {
		return err
	}
	return ensureCatalogColumn(db, "price_migration_items", "conflict_reason", `ALTER TABLE price_migration_items ADD COLUMN conflict_reason TEXT`)
}

func previewPriceMigrationItem(ctx context.Context, tx *sql.Tx, subID, target string, now time.Time) (PriceMigrationItem, error) {
	var item PriceMigrationItem
	item.SubscriptionID, item.TargetPriceVersionID = subID, target
	var subStatus string
	err := tx.QueryRowContext(ctx, `SELECT price_version_id,seat_quantity,revision,status FROM subscriptions WHERE id=?`, subID).
		Scan(&item.FromPriceVersionID, &item.SeatQuantity, &item.ExpectedRevision, &subStatus)
	if err != nil {
		return PriceMigrationItem{}, err
	}
	if subStatus != "active" || item.FromPriceVersionID == target {
		return PriceMigrationItem{}, ErrConflict
	}
	var enterprise int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM contract_subscriptions c WHERE c.subscription_id=? AND NOT EXISTS(SELECT 1 FROM contract_transitions t WHERE t.subscription_id=c.subscription_id)`, subID).Scan(&enterprise); err != nil || enterprise != 0 {
		return PriceMigrationItem{}, ErrConflict
	}
	var pending int
	if err := tx.QueryRowContext(ctx, `SELECT
(SELECT COUNT(*) FROM subscription_schedules WHERE subscription_id=? AND status='scheduled')+
(SELECT COUNT(*) FROM immediate_changes WHERE subscription_id=? AND status IN ('requested','needs_review'))+
(SELECT COUNT(*) FROM price_migration_items WHERE subscription_id=? AND status IN ('pending','conflicted'))`, subID, subID, subID).Scan(&pending); err != nil {
		return PriceMigrationItem{}, err
	}
	if pending != 0 {
		return PriceMigrationItem{}, ErrConflict
	}
	end, err := currentPeriodEnd(ctx, tx, subID, now)
	if err != nil {
		return PriceMigrationItem{}, err
	}
	item.EffectiveAt = time.Unix(0, end).UTC()
	var fromPlan, targetPlan, targetState string
	var effectiveFrom int64
	var effectiveTo sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT plan_id FROM price_versions WHERE id=?`, item.FromPriceVersionID).Scan(&fromPlan); err != nil {
		return PriceMigrationItem{}, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT plan_id,publication_state,effective_from,effective_to FROM price_versions WHERE id=?`, target).Scan(&targetPlan, &targetState, &effectiveFrom, &effectiveTo); err != nil {
		return PriceMigrationItem{}, err
	}
	if fromPlan != targetPlan || targetState != "published" || end < effectiveFrom || (effectiveTo.Valid && end >= effectiveTo.Int64) {
		return PriceMigrationItem{}, ErrConflict
	}
	fromTerms, err := loadPriceTerms(ctx, tx, item.FromPriceVersionID)
	if err != nil {
		return PriceMigrationItem{}, err
	}
	targetTerms, err := loadPriceTerms(ctx, tx, target)
	if err != nil {
		return PriceMigrationItem{}, err
	}
	if fromTerms.Currency != targetTerms.Currency {
		return PriceMigrationItem{}, ErrConflict
	}
	item.PriorAmountMinor, err = fromTerms.UpfrontMinor(item.SeatQuantity)
	if err != nil {
		return PriceMigrationItem{}, err
	}
	item.TargetAmountMinor, err = targetTerms.UpfrontMinor(item.SeatQuantity)
	if err != nil {
		return PriceMigrationItem{}, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT status FROM entitlements WHERE subscription_id=?),'unknown')`, subID).Scan(&item.CurrentEntitlementStatus); err != nil {
		return PriceMigrationItem{}, err
	}
	item.ProjectedEntitlementRule = "same_plan_payment_gated"
	item.Status = "pending"
	return item, nil
}

func (l *Lab) PreviewPriceMigration(ctx context.Context, target string, subscriptionIDs []string) ([]PriceMigrationItem, error) {
	if target == "" || len(subscriptionIDs) == 0 {
		return nil, ErrConflict
	}
	tx, err := l.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	items := make([]PriceMigrationItem, 0, len(subscriptionIDs))
	seen := make(map[string]bool, len(subscriptionIDs))
	for _, id := range subscriptionIDs {
		if id == "" || seen[id] {
			return nil, ErrConflict
		}
		seen[id] = true
		item, err := previewPriceMigrationItem(ctx, tx, id, target, l.now().UTC())
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].SubscriptionID < items[j].SubscriptionID })
	return items, nil
}

func loadPriceMigration(ctx context.Context, q priceQuerier, id string) (PriceMigration, error) {
	var m PriceMigration
	err := q.QueryRowContext(ctx, `SELECT id,cohort,target_price_version_id,status FROM price_migrations WHERE id=?`, id).Scan(&m.ID, &m.Cohort, &m.TargetPriceVersionID, &m.Status)
	if err != nil {
		return PriceMigration{}, err
	}
	rows, err := q.QueryContext(ctx, `SELECT subscription_id,from_price_version_id,target_price_version_id,seat_quantity,expected_revision,prior_amount_minor,target_amount_minor,current_entitlement_status,projected_entitlement_rule,effective_at,status,COALESCE(conflict_reason,'') FROM price_migration_items WHERE migration_id=? ORDER BY subscription_id`, id)
	if err != nil {
		return PriceMigration{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var x PriceMigrationItem
		var effective int64
		x.MigrationID = id
		if err := rows.Scan(&x.SubscriptionID, &x.FromPriceVersionID, &x.TargetPriceVersionID, &x.SeatQuantity, &x.ExpectedRevision, &x.PriorAmountMinor, &x.TargetAmountMinor, &x.CurrentEntitlementStatus, &x.ProjectedEntitlementRule, &effective, &x.Status, &x.ConflictReason); err != nil {
			return PriceMigration{}, err
		}
		x.EffectiveAt = time.Unix(0, effective).UTC()
		m.Items = append(m.Items, x)
	}
	return m, rows.Err()
}

func (l *Lab) PlanPriceMigration(ctx context.Context, id, cohort, target string, subscriptionIDs []string) (PriceMigration, error) {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return PriceMigration{}, err
	}
	defer tx.Rollback()
	result, err := l.planPriceMigrationTx(ctx, tx, l.now().UTC(), id, cohort, target, subscriptionIDs)
	if err != nil {
		return PriceMigration{}, err
	}
	return result, tx.Commit()
}

func (l *Lab) planPriceMigrationTx(ctx context.Context, tx *sql.Tx, at time.Time, id, cohort, target string, subscriptionIDs []string) (PriceMigration, error) {
	if id == "" || cohort == "" || target == "" || len(subscriptionIDs) == 0 {
		return PriceMigration{}, ErrConflict
	}
	ids := append([]string(nil), subscriptionIDs...)
	sort.Strings(ids)
	payload := hash(cohort, target, ids)
	var saved string
	err := tx.QueryRowContext(ctx, `SELECT payload_hash FROM price_migrations WHERE id=?`, id).Scan(&saved)
	if err == nil {
		if saved != payload {
			return PriceMigration{}, ErrConflict
		}
		return loadPriceMigration(ctx, tx, id)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return PriceMigration{}, err
	}
	items := make([]PriceMigrationItem, 0, len(ids))
	for i, subID := range ids {
		if subID == "" || (i > 0 && subID == ids[i-1]) {
			return PriceMigration{}, ErrConflict
		}
		x, err := previewPriceMigrationItem(ctx, tx, subID, target, at)
		if err != nil {
			return PriceMigration{}, err
		}
		x.MigrationID = id
		var selected string
		if err := tx.QueryRowContext(ctx, `SELECT price_version_id FROM catalog_selection WHERE plan_id='pro' AND cohort=? AND effective_at<=? ORDER BY effective_at DESC LIMIT 1`, cohort, x.EffectiveAt.UnixNano()).Scan(&selected); err != nil {
			return PriceMigration{}, err
		}
		if selected != target {
			return PriceMigration{}, ErrConflict
		}
		items = append(items, x)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO price_migrations(id,cohort,target_price_version_id,payload_hash,status,created_at) VALUES(?,?,?,?,'active',?)`, id, cohort, target, payload, at.UnixNano()); err != nil {
		return PriceMigration{}, err
	}
	for _, x := range items {
		if _, err := tx.ExecContext(ctx, `INSERT INTO price_migration_items(migration_id,subscription_id,from_price_version_id,target_price_version_id,seat_quantity,expected_revision,prior_amount_minor,target_amount_minor,current_entitlement_status,projected_entitlement_rule,effective_at,status) VALUES(?,?,?,?,?,?,?,?,?,?,?,'pending')`, id, x.SubscriptionID, x.FromPriceVersionID, x.TargetPriceVersionID, x.SeatQuantity, x.ExpectedRevision, x.PriorAmountMinor, x.TargetAmountMinor, x.CurrentEntitlementStatus, x.ProjectedEntitlementRule, x.EffectiveAt.UnixNano()); err != nil {
			return PriceMigration{}, err
		}
	}
	return PriceMigration{ID: id, Cohort: cohort, TargetPriceVersionID: target, Status: "active", Items: items}, nil
}

func (l *Lab) PriceMigration(ctx context.Context, id string) (PriceMigration, error) {
	return loadPriceMigration(ctx, l.db, id)
}

func (l *Lab) PausePriceMigration(ctx context.Context, id string) error {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := pausePriceMigrationTx(ctx, tx, id); err != nil {
		return err
	}
	return tx.Commit()
}

func pausePriceMigrationTx(ctx context.Context, tx *sql.Tx, id string) error {
	r, err := tx.ExecContext(ctx, `UPDATE price_migrations SET status='paused' WHERE id=? AND status='active'`, id)
	if err != nil {
		return err
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return ErrConflict
	}
	return nil
}

func (l *Lab) SkipPriceMigrationItem(ctx context.Context, id, subID string) error {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := l.skipPriceMigrationItemTx(ctx, tx, id, subID); err != nil {
		return err
	}
	return tx.Commit()
}

func (l *Lab) skipPriceMigrationItemTx(ctx context.Context, tx *sql.Tx, id, subID string) error {
	r, err := tx.ExecContext(ctx, `UPDATE price_migration_items SET status='skipped' WHERE migration_id=? AND subscription_id=? AND status IN ('pending','conflicted') AND EXISTS(SELECT 1 FROM price_migrations WHERE id=? AND status='paused')`, id, subID, id)
	if err != nil {
		return err
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return ErrConflict
	}
	if _, err := tx.ExecContext(ctx, `UPDATE price_migrations SET status='completed' WHERE id=? AND status='paused' AND NOT EXISTS(SELECT 1 FROM price_migration_items WHERE migration_id=? AND status IN ('pending','conflicted'))`, id, id); err != nil {
		return err
	}
	return nil
}

func (l *Lab) ResumePriceMigration(ctx context.Context, id string) error {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := l.resumePriceMigrationTx(ctx, tx, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (l *Lab) resumePriceMigrationTx(ctx context.Context, tx *sql.Tx, id string) error {
	var conflicts int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM price_migration_items WHERE migration_id=? AND status='conflicted'`, id).Scan(&conflicts); err != nil {
		return err
	}
	if conflicts != 0 {
		return ErrConflict
	}
	r, err := tx.ExecContext(ctx, `UPDATE price_migrations SET status='active' WHERE id=? AND status='paused'`, id)
	if err != nil {
		return err
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return ErrConflict
	}
	return nil
}

// applyPriceMigrationAtBoundary shares the renewal transaction so that the
// assignment switch and invoice always commit together.
func applyPriceMigrationAtBoundary(ctx context.Context, tx *sql.Tx, subID string, boundary int64, now time.Time, hasSchedule bool) (string, bool, error) {
	var id, target, from, itemStatus, jobStatus string
	var seats, revision int64
	err := tx.QueryRowContext(ctx, `SELECT i.migration_id,i.target_price_version_id,i.from_price_version_id,i.seat_quantity,i.expected_revision,i.status,m.status FROM price_migration_items i JOIN price_migrations m ON m.id=i.migration_id WHERE i.subscription_id=? AND i.effective_at=? AND i.status IN ('pending','conflicted')`, subID, boundary).Scan(&id, &target, &from, &seats, &revision, &itemStatus, &jobStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if jobStatus != "active" || itemStatus != "pending" {
		return "", true, nil
	}
	var current, subStatus string
	var actualSeats, actualRevision int64
	if err := tx.QueryRowContext(ctx, `SELECT price_version_id,seat_quantity,revision,status FROM subscriptions WHERE id=?`, subID).Scan(&current, &actualSeats, &actualRevision, &subStatus); err != nil {
		return "", false, err
	}
	if hasSchedule || current != from || actualSeats != seats || actualRevision != revision || subStatus != "active" {
		reason := "revision_changed"
		switch {
		case hasSchedule:
			reason = "scheduled_change"
		case current != from:
			reason = "source_price_changed"
		case actualSeats != seats:
			reason = "seat_quantity_changed"
		case subStatus != "active":
			reason = "subscription_inactive"
		}
		if _, err := tx.ExecContext(ctx, `UPDATE price_migration_items SET status='conflicted',conflict_reason=? WHERE migration_id=? AND subscription_id=?`, reason, id, subID); err != nil {
			return "", false, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE price_migrations SET status='paused' WHERE id=?`, id); err != nil {
			return "", false, err
		}
		return "", true, nil
	}
	var nextIndex int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(assignment_index),-1)+1 FROM pricing_assignments WHERE subscription_id=?`, subID).Scan(&nextIndex); err != nil {
		return "", false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE pricing_assignments SET effective_end=? WHERE subscription_id=? AND effective_end IS NULL`, boundary, subID); err != nil {
		return "", false, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO pricing_assignments(subscription_id,assignment_index,price_version_id,seat_quantity,effective_start,source_migration_id) VALUES(?,?,?,?,?,?)`, subID, nextIndex, target, seats, boundary, id); err != nil {
		return "", false, err
	}
	r, err := tx.ExecContext(ctx, `UPDATE subscriptions SET price_version_id=?,revision=revision+1 WHERE id=? AND revision=? AND price_version_id=?`, target, subID, revision, from)
	if err != nil {
		return "", false, err
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return "", false, ErrConflict
	}
	if _, err := tx.ExecContext(ctx, `UPDATE price_migration_items SET status='applied' WHERE migration_id=? AND subscription_id=? AND status='pending'`, id, subID); err != nil {
		return "", false, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_events(kind,object_id,at) VALUES('price_migration_applied',?,?)`, id+":"+subID, now.UnixNano()); err != nil {
		return "", false, err
	}
	var remaining int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM price_migration_items WHERE migration_id=? AND status IN ('pending','conflicted')`, id).Scan(&remaining); err != nil {
		return "", false, err
	}
	if remaining == 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE price_migrations SET status='completed' WHERE id=?`, id); err != nil {
			return "", false, err
		}
	}
	return target, false, nil
}
