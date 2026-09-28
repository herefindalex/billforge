package lab

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ProPriceSpec is the currently supported published Pro component vocabulary.
// A later SKU with another meter requires its own explicit terms and billing path.
type ProPriceSpec struct {
	ID            string
	Version       int64
	FixedMinor    int64
	SeatMinor     int64
	IncludedTasks int64
	UsageRateNum  int64
	UsageRateDen  int64
	EffectiveFrom time.Time
}

func (l *Lab) PublishProPrice(ctx context.Context, spec ProPriceSpec) (PriceTerms, error) {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return PriceTerms{}, err
	}
	defer tx.Rollback()
	terms, err := l.publishProPriceTx(ctx, tx, l.now().UTC(), spec)
	if err != nil {
		return PriceTerms{}, err
	}
	return terms, tx.Commit()
}

func (l *Lab) publishProPriceTx(ctx context.Context, tx *sql.Tx, at time.Time, spec ProPriceSpec) (PriceTerms, error) {
	if spec.ID == "" || spec.Version <= 0 || spec.SeatMinor <= 0 || !minimumUpfrontFits(spec.FixedMinor, spec.SeatMinor) || spec.IncludedTasks < 0 || spec.UsageRateNum < 0 || spec.UsageRateDen <= 0 || spec.EffectiveFrom.IsZero() {
		return PriceTerms{}, ErrConflict
	}
	checksum := hash(spec.ID, spec.Version, "USD", spec.FixedMinor, "seats", spec.SeatMinor, "tasks", spec.IncludedTasks, spec.UsageRateNum, spec.UsageRateDen, spec.EffectiveFrom.UTC().UnixNano())
	var existingChecksum, state string
	err := tx.QueryRowContext(ctx, `SELECT checksum,publication_state FROM price_versions WHERE id=?`, spec.ID).Scan(&existingChecksum, &state)
	if err == nil {
		if state != "published" || existingChecksum != checksum {
			return PriceTerms{}, ErrConflict
		}
		return loadPriceTerms(ctx, tx, spec.ID)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return PriceTerms{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO price_versions(id,plan_id,version,currency,fixed_amount_minor,published_at,checksum,publication_state,effective_from) VALUES(?,'pro',?,'USD',?,0,'','draft',?)`, spec.ID, spec.Version, spec.FixedMinor, spec.EffectiveFrom.UTC().UnixNano()); err != nil {
		return PriceTerms{}, err
	}
	for _, component := range []struct {
		code, kind, meter          string
		amount, quantity, num, den int64
	}{
		{"seats", "per_seat", "", spec.SeatMinor, 0, 0, 1},
		{"tasks_included", "included_quantity", "tasks", 0, spec.IncludedTasks, 0, 1},
		{"tasks_overage", "usage_overage", "tasks", 0, 0, spec.UsageRateNum, spec.UsageRateDen},
	} {
		if _, err := tx.ExecContext(ctx, `INSERT INTO price_components(price_version_id,component_code,kind,amount_minor,quantity,rate_num,rate_den,meter_id) VALUES(?,?,?,?,?,?,?,?)`, spec.ID, component.code, component.kind, component.amount, component.quantity, component.num, component.den, component.meter); err != nil {
			return PriceTerms{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE price_versions SET publication_state='published',published_at=?,checksum=? WHERE id=? AND publication_state='draft'`, at.UnixNano(), checksum, spec.ID); err != nil {
		return PriceTerms{}, err
	}
	return loadPriceTerms(ctx, tx, spec.ID)
}

func (l *Lab) SelectCatalogPrice(ctx context.Context, planID, cohort string, effectiveAt time.Time, priceVersionID string) error {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := l.selectCatalogPriceTx(ctx, tx, planID, cohort, effectiveAt, priceVersionID); err != nil {
		return err
	}
	return tx.Commit()
}

func (l *Lab) selectCatalogPriceTx(ctx context.Context, tx *sql.Tx, planID, cohort string, effectiveAt time.Time, priceVersionID string) error {
	if planID == "" || cohort == "" || priceVersionID == "" || effectiveAt.IsZero() {
		return ErrConflict
	}
	var pricePlan, state string
	var from int64
	var to sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT plan_id,publication_state,effective_from,effective_to FROM price_versions WHERE id=?`, priceVersionID).Scan(&pricePlan, &state, &from, &to); err != nil {
		return err
	}
	at := effectiveAt.UTC().UnixNano()
	if pricePlan != planID || state != "published" || at < from || (to.Valid && at >= to.Int64) {
		return ErrConflict
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO catalog_selection(plan_id,cohort,effective_at,price_version_id) VALUES(?,?,?,?) ON CONFLICT(plan_id,cohort,effective_at) DO NOTHING`, planID, cohort, at, priceVersionID); err != nil {
		return err
	}
	var saved string
	if err := tx.QueryRowContext(ctx, `SELECT price_version_id FROM catalog_selection WHERE plan_id=? AND cohort=? AND effective_at=?`, planID, cohort, at).Scan(&saved); err != nil {
		return err
	}
	if saved != priceVersionID {
		return ErrConflict
	}
	return nil
}
