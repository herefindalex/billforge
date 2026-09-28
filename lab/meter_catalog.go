package lab

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"time"
)

var catalogCode = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

type MeterSpec struct {
	ID            string
	Source        string
	Unit          string
	SchemaVersion int64
}

type MeteredPriceSpec struct {
	ID               string
	PlanID           string
	Version          int64
	FixedMinor       int64
	SeatMinor        int64
	MeterID          string
	IncludedQuantity int64
	UsageRateNum     int64
	UsageRateDen     int64
	EffectiveFrom    time.Time
}

func (l *Lab) MeterCatalog(ctx context.Context) ([]MeterSpec, error) {
	rows, err := l.db.QueryContext(ctx, `SELECT id,source,unit,schema_version FROM meter_schemas ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MeterSpec
	for rows.Next() {
		var m MeterSpec
		if err := rows.Scan(&m.ID, &m.Source, &m.Unit, &m.SchemaVersion); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (l *Lab) PriceTerms(ctx context.Context, versionID string) (PriceTerms, error) {
	return loadPriceTerms(ctx, l.db, versionID)
}

func migrateMeterCatalog(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS meter_schemas (
		id TEXT PRIMARY KEY, source TEXT NOT NULL, unit TEXT NOT NULL,
		schema_version INTEGER NOT NULL CHECK(schema_version>0));
		INSERT INTO meter_schemas(id,source,unit,schema_version)
		VALUES('tasks','*','task',1) ON CONFLICT(id) DO NOTHING;`)
	return err
}

// RegisterMeter pins event ownership and units before a price may use a meter.
func (l *Lab) RegisterMeter(ctx context.Context, spec MeterSpec) error {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := l.registerMeterTx(ctx, tx, spec); err != nil {
		return err
	}
	return tx.Commit()
}

func (l *Lab) registerMeterTx(ctx context.Context, tx *sql.Tx, spec MeterSpec) error {
	if !catalogCode.MatchString(spec.ID) || (spec.Source != "*" && !catalogCode.MatchString(spec.Source)) || spec.Unit == "" || spec.SchemaVersion <= 0 {
		return ErrConflict
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO meter_schemas(id,source,unit,schema_version) VALUES(?,?,?,?) ON CONFLICT(id) DO NOTHING`, spec.ID, spec.Source, spec.Unit, spec.SchemaVersion); err != nil {
		return err
	}
	var saved MeterSpec
	if err := tx.QueryRowContext(ctx, `SELECT id,source,unit,schema_version FROM meter_schemas WHERE id=?`, spec.ID).Scan(&saved.ID, &saved.Source, &saved.Unit, &saved.SchemaVersion); err != nil {
		return err
	}
	if saved != spec {
		return ErrConflict
	}
	return nil
}

// PublishMeteredPrice uses the same fixed, seat, allowance, and overage
// components for any registered single-meter SKU. A published version is immutable.
func (l *Lab) PublishMeteredPrice(ctx context.Context, spec MeteredPriceSpec) (PriceTerms, error) {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return PriceTerms{}, err
	}
	defer tx.Rollback()
	terms, err := l.publishMeteredPriceTx(ctx, tx, l.now().UTC(), spec)
	if err != nil {
		return PriceTerms{}, err
	}
	return terms, tx.Commit()
}

func (l *Lab) publishMeteredPriceTx(ctx context.Context, tx *sql.Tx, at time.Time, spec MeteredPriceSpec) (PriceTerms, error) {
	if !catalogCode.MatchString(spec.ID) || !catalogCode.MatchString(spec.PlanID) || !catalogCode.MatchString(spec.MeterID) ||
		spec.Version <= 0 || !minimumUpfrontFits(spec.FixedMinor, spec.SeatMinor) || spec.IncludedQuantity <= 0 ||
		spec.UsageRateNum <= 0 || spec.UsageRateDen <= 0 || !unixNanoTimeFits(spec.EffectiveFrom) {
		return PriceTerms{}, ErrConflict
	}
	var registered string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM meter_schemas WHERE id=?`, spec.MeterID).Scan(&registered); err != nil {
		return PriceTerms{}, err
	}
	checksum := hash(spec.ID, spec.PlanID, spec.Version, "USD", spec.FixedMinor, spec.SeatMinor,
		spec.MeterID, spec.IncludedQuantity, spec.UsageRateNum, spec.UsageRateDen, spec.EffectiveFrom.UTC().UnixNano())
	var oldChecksum, state string
	err := tx.QueryRowContext(ctx, `SELECT checksum,publication_state FROM price_versions WHERE id=?`, spec.ID).Scan(&oldChecksum, &state)
	if err == nil {
		if oldChecksum != checksum || state != "published" {
			return PriceTerms{}, ErrConflict
		}
		return loadPriceTerms(ctx, tx, spec.ID)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return PriceTerms{}, err
	}
	if !unixNanoTimeFits(at) {
		return PriceTerms{}, ErrConflict
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO price_versions(id,plan_id,version,currency,fixed_amount_minor,published_at,checksum,publication_state,effective_from) VALUES(?,?,?,'USD',?,0,'','draft',?)`, spec.ID, spec.PlanID, spec.Version, spec.FixedMinor, spec.EffectiveFrom.UTC().UnixNano()); err != nil {
		return PriceTerms{}, err
	}
	components := []struct {
		code, kind, meter          string
		amount, quantity, num, den int64
	}{
		{spec.MeterID + "_included", "included_quantity", spec.MeterID, 0, spec.IncludedQuantity, 0, 1},
		{spec.MeterID + "_overage", "usage_overage", spec.MeterID, 0, 0, spec.UsageRateNum, spec.UsageRateDen},
	}
	if spec.SeatMinor > 0 {
		components = append(components, struct {
			code, kind, meter          string
			amount, quantity, num, den int64
		}{"seats", "per_seat", "", spec.SeatMinor, 0, 0, 1})
	}
	for _, c := range components {
		if _, err := tx.ExecContext(ctx, `INSERT INTO price_components(price_version_id,component_code,kind,amount_minor,quantity,rate_num,rate_den,meter_id) VALUES(?,?,?,?,?,?,?,?)`, spec.ID, c.code, c.kind, c.amount, c.quantity, c.num, c.den, c.meter); err != nil {
			return PriceTerms{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE price_versions SET publication_state='published',published_at=?,checksum=? WHERE id=? AND publication_state='draft'`, at.UnixNano(), checksum, spec.ID); err != nil {
		return PriceTerms{}, err
	}
	return loadPriceTerms(ctx, tx, spec.ID)
}
