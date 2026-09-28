package lab

import (
	"context"
	"time"
)

type ReconciliationRunState struct {
	ID                 string
	CutoffAt           time.Time
	LocalObservedAt    time.Time
	ProviderObservedAt time.Time
	FindingCount       int
}

func (l *Lab) ReconciliationRuns(ctx context.Context) ([]ReconciliationRunState, error) {
	rows, err := l.db.QueryContext(ctx, `SELECT r.id,r.cutoff_at,r.local_observed_at,r.provider_observed_at,COUNT(f.discrepancy_id)
		FROM reconciliation_runs r LEFT JOIN reconciliation_findings f ON f.run_id=r.id
		GROUP BY r.id ORDER BY r.created_at DESC,r.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]ReconciliationRunState, 0)
	for rows.Next() {
		var item ReconciliationRunState
		var cutoff, local, provider int64
		if err := rows.Scan(&item.ID, &cutoff, &local, &provider, &item.FindingCount); err != nil {
			return nil, err
		}
		item.CutoffAt = time.Unix(0, cutoff).UTC()
		item.LocalObservedAt = time.Unix(0, local).UTC()
		item.ProviderObservedAt = time.Unix(0, provider).UTC()
		result = append(result, item)
	}
	return result, rows.Err()
}
