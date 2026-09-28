package lab

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type AdminReconciliationFinding struct {
	DiscrepancyID   string
	Kind            string
	ObjectID        string
	Classification  string
	Expected        string
	Actual          string
	Evidence        string
	SourceRevision  int64
	SnapshotQuality string
	CurrentStatus   string
}

type AdminReconciliationRunPage struct {
	Run      ReconciliationRunState
	Findings []AdminReconciliationFinding
	NextID   string
}

func (l *Lab) AdminReconciliationRunPage(ctx context.Context, id, after string, limit int) (AdminReconciliationRunPage, error) {
	if limit < 1 || limit > 100 {
		return AdminReconciliationRunPage{}, ErrAdminInvalidCommand
	}
	tx, err := l.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return AdminReconciliationRunPage{}, err
	}
	defer tx.Rollback()
	page := AdminReconciliationRunPage{Findings: []AdminReconciliationFinding{}}
	var cutoff, local, provider int64
	err = tx.QueryRowContext(ctx, `SELECT id,cutoff_at,local_observed_at,provider_observed_at FROM reconciliation_runs WHERE id=?`, id).
		Scan(&page.Run.ID, &cutoff, &local, &provider)
	if err != nil {
		return AdminReconciliationRunPage{}, err
	}
	page.Run.CutoffAt = time.Unix(0, cutoff).UTC()
	page.Run.LocalObservedAt = time.Unix(0, local).UTC()
	page.Run.ProviderObservedAt = time.Unix(0, provider).UTC()
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM reconciliation_findings WHERE run_id=?`, id).Scan(&page.Run.FindingCount); err != nil {
		return AdminReconciliationRunPage{}, err
	}
	var snapshotCount int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM reconciliation_finding_snapshots WHERE run_id=?`, id).Scan(&snapshotCount); err != nil {
		return AdminReconciliationRunPage{}, err
	}
	if snapshotCount != page.Run.FindingCount {
		return AdminReconciliationRunPage{}, errors.New("reconciliation finding snapshot count mismatch")
	}
	rows, err := tx.QueryContext(ctx, `SELECT f.discrepancy_id,f.kind,f.object_id,f.classification,f.expected,f.actual,f.evidence,f.source_revision,f.snapshot_quality,d.status
		FROM reconciliation_finding_snapshots f JOIN discrepancies d ON d.id=f.discrepancy_id
		WHERE f.run_id=? AND f.discrepancy_id>? ORDER BY f.discrepancy_id LIMIT ?`, id, after, limit+1)
	if err != nil {
		return AdminReconciliationRunPage{}, err
	}
	for rows.Next() {
		var item AdminReconciliationFinding
		if err := rows.Scan(&item.DiscrepancyID, &item.Kind, &item.ObjectID, &item.Classification, &item.Expected, &item.Actual, &item.Evidence, &item.SourceRevision, &item.SnapshotQuality, &item.CurrentStatus); err != nil {
			rows.Close()
			return AdminReconciliationRunPage{}, err
		}
		page.Findings = append(page.Findings, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return AdminReconciliationRunPage{}, err
	}
	rows.Close()
	if len(page.Findings) > limit {
		page.Findings = page.Findings[:limit]
		page.NextID = page.Findings[len(page.Findings)-1].DiscrepancyID
	}
	if err := tx.Commit(); err != nil {
		return AdminReconciliationRunPage{}, err
	}
	return page, nil
}
