package lab

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// AdminUsagePeriodDetail keeps the current monetary state and its latest
// persisted rating in one read snapshot. Open periods carry a read-only estimate.
type AdminUsagePeriodDetail struct {
	SubscriptionID  string
	PeriodIndex     int
	PeriodStart     time.Time
	PeriodEnd       time.Time
	PriceVersionID  string
	Currency        string
	Status          string
	CutoffAt        *time.Time
	ClosedAt        *time.Time
	RatedMinor      int64
	BilledMinor     int64
	CreditedMinor   int64
	AppliedCount    int
	CreditNoteCount int
	CreditNoteMinor int64
	Estimate        *UsageRating
	LatestRating    *UsageRating
}

type AdminUsageRatingCursor struct {
	SubscriptionID string `json:"subscription_id"`
	PeriodIndex    int    `json:"period_index"`
	Revision       int    `json:"revision"`
}

type AdminUsageRatingPage struct {
	Items []UsageRating
	Next  *AdminUsageRatingCursor
}

func (l *Lab) AdminUsagePeriodDetail(ctx context.Context, subID string, index int) (AdminUsagePeriodDetail, error) {
	if subID == "" || index < 0 {
		return AdminUsagePeriodDetail{}, ErrConflict
	}
	tx, err := l.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return AdminUsagePeriodDetail{}, err
	}
	defer tx.Rollback()
	detail := AdminUsagePeriodDetail{SubscriptionID: subID, PeriodIndex: index, Status: "estimated"}
	var start, end int64
	if err := tx.QueryRowContext(ctx, `SELECT period_start,period_end FROM billing_periods WHERE subscription_id=? AND period_index=?`, subID, index).Scan(&start, &end); err != nil {
		return AdminUsagePeriodDetail{}, err
	}
	detail.PeriodStart, detail.PeriodEnd = time.Unix(0, start).UTC(), time.Unix(0, end).UTC()
	var cutoff, closed int64
	err = tx.QueryRowContext(ctx, `SELECT price_version_id,cutoff_at,closed_at,rated_minor,billed_minor,credited_minor,applied_count FROM usage_periods WHERE subscription_id=? AND period_index=?`, subID, index).Scan(
		&detail.PriceVersionID, &cutoff, &closed, &detail.RatedMinor, &detail.BilledMinor, &detail.CreditedMinor, &detail.AppliedCount,
	)
	if err == nil {
		cutoffAt, closedAt := time.Unix(0, cutoff).UTC(), time.Unix(0, closed).UTC()
		detail.CutoffAt, detail.ClosedAt = &cutoffAt, &closedAt
		var ratingID string
		if err := tx.QueryRowContext(ctx, `SELECT id FROM usage_ratings WHERE subscription_id=? AND period_index=? ORDER BY revision DESC LIMIT 1`, subID, index).Scan(&ratingID); err != nil {
			return AdminUsagePeriodDetail{}, err
		}
		rating, err := loadUsageRating(ctx, tx, ratingID)
		if err != nil {
			return AdminUsagePeriodDetail{}, err
		}
		detail.LatestRating = &rating
		detail.Status = "finalized"
		if rating.Revision > 1 {
			detail.Status = "rerated"
		}
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(amount_minor),0) FROM usage_credit_notes WHERE subscription_id=? AND period_index=?`, subID, index).Scan(&detail.CreditNoteCount, &detail.CreditNoteMinor); err != nil {
			return AdminUsagePeriodDetail{}, err
		}
	} else if errors.Is(err, sql.ErrNoRows) {
		err = tx.QueryRowContext(ctx, `SELECT price_version_id FROM usage_events WHERE subscription_id=? AND period_index=? ORDER BY rowid LIMIT 1`, subID, index).Scan(&detail.PriceVersionID)
		if errors.Is(err, sql.ErrNoRows) {
			err = tx.QueryRowContext(ctx, `SELECT price_version_id FROM pricing_assignments WHERE subscription_id=? AND effective_start<? AND (effective_end IS NULL OR effective_end>?) ORDER BY assignment_index DESC LIMIT 1`, subID, end, end-1).Scan(&detail.PriceVersionID)
		}
		if err != nil {
			return AdminUsagePeriodDetail{}, err
		}
		terms, err := loadPriceTerms(ctx, tx, detail.PriceVersionID)
		if err != nil {
			return AdminUsagePeriodDetail{}, err
		}
		if terms.MeterID != "" {
			estimate, err := rateUsage(ctx, tx, subID, index, detail.PriceVersionID, l.now().UTC().UnixNano())
			if err != nil {
				return AdminUsagePeriodDetail{}, err
			}
			detail.Estimate = &estimate
		}
	} else {
		return AdminUsagePeriodDetail{}, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT currency FROM price_versions WHERE id=?`, detail.PriceVersionID).Scan(&detail.Currency); err != nil {
		return AdminUsagePeriodDetail{}, err
	}
	if err := tx.Commit(); err != nil {
		return AdminUsagePeriodDetail{}, err
	}
	return detail, nil
}

func (l *Lab) AdminUsagePeriodRatings(ctx context.Context, subID string, index int, after *AdminUsageRatingCursor, limit int) (AdminUsageRatingPage, error) {
	if subID == "" || index < 0 || limit < 1 || limit > 100 || after != nil && (after.SubscriptionID != subID || after.PeriodIndex != index || after.Revision < 1) {
		return AdminUsageRatingPage{}, ErrConflict
	}
	tx, err := l.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return AdminUsageRatingPage{}, err
	}
	defer tx.Rollback()
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM billing_periods WHERE subscription_id=? AND period_index=?`, subID, index).Scan(&exists); err != nil {
		return AdminUsageRatingPage{}, err
	}
	query := `SELECT id,revision,price_version_id,quantity,included_quantity,overage_quantity,exact_minor_numerator,exact_minor_denominator,rounded_minor,delta_minor,rated_at FROM usage_ratings WHERE subscription_id=? AND period_index=?`
	args := []any{subID, index}
	if after != nil {
		query += ` AND revision<?`
		args = append(args, after.Revision)
	}
	query += ` ORDER BY revision DESC LIMIT ?`
	args = append(args, limit+1)
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return AdminUsageRatingPage{}, err
	}
	page := AdminUsageRatingPage{Items: []UsageRating{}}
	for rows.Next() {
		item := UsageRating{SubscriptionID: subID, PeriodIndex: index}
		var at int64
		if err := rows.Scan(&item.ID, &item.Revision, &item.PriceVersionID, &item.Quantity, &item.IncludedQuantity, &item.OverageQuantity, &item.ExactMinorNumerator, &item.ExactMinorDenominator, &item.RoundedMinor, &item.DeltaMinor, &at); err != nil {
			rows.Close()
			return AdminUsageRatingPage{}, err
		}
		item.RatedAt = time.Unix(0, at).UTC()
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return AdminUsageRatingPage{}, err
	}
	rows.Close()
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		page.Next = &AdminUsageRatingCursor{SubscriptionID: subID, PeriodIndex: index, Revision: page.Items[len(page.Items)-1].Revision}
	}
	if err := tx.Commit(); err != nil {
		return AdminUsageRatingPage{}, err
	}
	return page, nil
}
