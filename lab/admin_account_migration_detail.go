package lab

import (
	"context"
	"database/sql"
	"time"
)

type AdminAccountMigrationEvent struct {
	ID     string
	Kind   string
	Detail string
	At     time.Time
}

type AdminAccountMigrationDetail struct {
	Link                AccountLink
	Shadows             []ShadowComparison
	Provenance          []LegacyProvenance
	Events              []AdminAccountMigrationEvent
	ShadowsTruncated    bool
	ProvenanceTruncated bool
	EventsTruncated     bool
}

func (l *Lab) AdminAccountMigrationDetail(ctx context.Context, id string) (AdminAccountMigrationDetail, error) {
	tx, err := l.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return AdminAccountMigrationDetail{}, err
	}
	defer tx.Rollback()
	d := AdminAccountMigrationDetail{Shadows: []ShadowComparison{}, Provenance: []LegacyProvenance{}, Events: []AdminAccountMigrationEvent{}}
	d.Link, err = loadAccountLink(ctx, tx, id)
	if err != nil {
		return AdminAccountMigrationDetail{}, err
	}
	shadows, err := tx.QueryContext(ctx, `SELECT id,kind,object_id,expected,actual,matched,latency_millis,observed_at FROM migration_shadows WHERE legacy_account_id=? ORDER BY observed_at DESC,id DESC LIMIT 101`, id)
	if err != nil {
		return AdminAccountMigrationDetail{}, err
	}
	for shadows.Next() {
		var item ShadowComparison
		var matched int
		var observed int64
		if err := shadows.Scan(&item.ID, &item.Kind, &item.ObjectID, &item.Expected, &item.Actual, &matched, &item.LatencyMillis, &observed); err != nil {
			shadows.Close()
			return AdminAccountMigrationDetail{}, err
		}
		item.LegacyAccountID = id
		item.Matched = matched != 0
		item.ObservedAt = time.Unix(0, observed).UTC()
		d.Shadows = append(d.Shadows, item)
	}
	if err := shadows.Err(); err != nil {
		shadows.Close()
		return AdminAccountMigrationDetail{}, err
	}
	shadows.Close()
	if len(d.Shadows) > 100 {
		d.Shadows = d.Shadows[:100]
		d.ShadowsTruncated = true
	}
	provenance, err := tx.QueryContext(ctx, `SELECT legacy_invoice_id,legacy_subscription_id,commerce_subscription_id,commerce_invoice_id,price_version_id,status,evidence FROM legacy_provenance WHERE legacy_account_id=? ORDER BY legacy_invoice_id LIMIT 101`, id)
	if err != nil {
		return AdminAccountMigrationDetail{}, err
	}
	for provenance.Next() {
		var item LegacyProvenance
		if err := provenance.Scan(&item.LegacyInvoiceID, &item.LegacySubscriptionID, &item.CommerceSubscriptionID, &item.CommerceInvoiceID, &item.PriceVersionID, &item.Status, &item.Evidence); err != nil {
			provenance.Close()
			return AdminAccountMigrationDetail{}, err
		}
		item.LegacyAccountID = id
		d.Provenance = append(d.Provenance, item)
	}
	if err := provenance.Err(); err != nil {
		provenance.Close()
		return AdminAccountMigrationDetail{}, err
	}
	provenance.Close()
	if len(d.Provenance) > 100 {
		d.Provenance = d.Provenance[:100]
		d.ProvenanceTruncated = true
	}
	events, err := tx.QueryContext(ctx, `SELECT id,kind,detail,at FROM account_migration_events WHERE legacy_account_id=? ORDER BY at DESC,id DESC LIMIT 101`, id)
	if err != nil {
		return AdminAccountMigrationDetail{}, err
	}
	for events.Next() {
		var item AdminAccountMigrationEvent
		var at int64
		if err := events.Scan(&item.ID, &item.Kind, &item.Detail, &at); err != nil {
			events.Close()
			return AdminAccountMigrationDetail{}, err
		}
		item.At = time.Unix(0, at).UTC()
		d.Events = append(d.Events, item)
	}
	if err := events.Err(); err != nil {
		events.Close()
		return AdminAccountMigrationDetail{}, err
	}
	events.Close()
	if len(d.Events) > 100 {
		d.Events = d.Events[:100]
		d.EventsTruncated = true
	}
	if err := tx.Commit(); err != nil {
		return AdminAccountMigrationDetail{}, err
	}
	return d, nil
}

func (l *Lab) AdminMigrationReadiness(ctx context.Context, id string, limits MigrationThresholds) (MigrationReadiness, error) {
	tx, err := l.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return MigrationReadiness{}, err
	}
	defer tx.Rollback()
	readiness, err := l.migrationReadiness(ctx, tx, id, limits)
	if err != nil {
		return MigrationReadiness{}, err
	}
	if err := tx.Commit(); err != nil {
		return MigrationReadiness{}, err
	}
	return readiness, nil
}
