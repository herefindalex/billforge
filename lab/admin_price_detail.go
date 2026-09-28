package lab

import (
	"context"
	"database/sql"
	"time"
)

type AdminPriceComponent struct {
	Code        string
	Kind        string
	AmountMinor int64
	Quantity    int64
	RateNum     int64
	RateDen     int64
	MeterID     string
}

type AdminPriceDetail struct {
	ID                  string
	PlanID              string
	Version             int64
	Currency            string
	FixedMinor          int64
	Checksum            string
	PublicationState    string
	PublishedAt         time.Time
	EffectiveFrom       time.Time
	EffectiveTo         *time.Time
	SelectionCount      int64
	Components          []AdminPriceComponent
	ComponentsTruncated bool
}

// AdminPriceVersionDetail reads the version, its components, and selection count
// from one SQLite snapshot. Component results are bounded for an invalid draft.
func (l *Lab) AdminPriceVersionDetail(ctx context.Context, id string) (AdminPriceDetail, error) {
	tx, err := l.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return AdminPriceDetail{}, err
	}
	defer tx.Rollback()

	var detail AdminPriceDetail
	var publishedAt, effectiveFrom int64
	var effectiveTo sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT id,plan_id,version,currency,fixed_amount_minor,checksum,publication_state,published_at,effective_from,effective_to FROM price_versions WHERE id=?`, id).
		Scan(&detail.ID, &detail.PlanID, &detail.Version, &detail.Currency, &detail.FixedMinor, &detail.Checksum, &detail.PublicationState, &publishedAt, &effectiveFrom, &effectiveTo)
	if err != nil {
		return AdminPriceDetail{}, err
	}
	detail.PublishedAt = time.Unix(0, publishedAt).UTC()
	detail.EffectiveFrom = time.Unix(0, effectiveFrom).UTC()
	if effectiveTo.Valid {
		value := time.Unix(0, effectiveTo.Int64).UTC()
		detail.EffectiveTo = &value
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM catalog_selection WHERE price_version_id=?`, id).Scan(&detail.SelectionCount); err != nil {
		return AdminPriceDetail{}, err
	}

	rows, err := tx.QueryContext(ctx, `SELECT component_code,kind,amount_minor,quantity,rate_num,rate_den,meter_id FROM price_components WHERE price_version_id=? ORDER BY component_code LIMIT 51`, id)
	if err != nil {
		return AdminPriceDetail{}, err
	}
	detail.Components = make([]AdminPriceComponent, 0)
	for rows.Next() {
		var component AdminPriceComponent
		if err := rows.Scan(&component.Code, &component.Kind, &component.AmountMinor, &component.Quantity, &component.RateNum, &component.RateDen, &component.MeterID); err != nil {
			rows.Close()
			return AdminPriceDetail{}, err
		}
		if len(detail.Components) == 50 {
			detail.ComponentsTruncated = true
			break
		}
		detail.Components = append(detail.Components, component)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return AdminPriceDetail{}, err
	}
	if err := rows.Close(); err != nil {
		return AdminPriceDetail{}, err
	}
	if err := tx.Commit(); err != nil {
		return AdminPriceDetail{}, err
	}
	return detail, nil
}
