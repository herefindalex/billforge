package lab

import (
	"context"
	"database/sql"
	"time"
)

type AdminDiscrepancyDetail struct {
	Discrepancy        Discrepancy
	FirstSeenAt        time.Time
	LastSeenAt         time.Time
	Resolution         string
	Runs               []AdminDiscrepancyRun
	Repairs            []AdminDiscrepancyRepair
	Decisions          []AdminDiscrepancyDecision
	RunsTruncated      bool
	RepairsTruncated   bool
	DecisionsTruncated bool
}

type AdminDiscrepancyRun struct {
	ID                 string
	CutoffAt           time.Time
	LocalObservedAt    time.Time
	ProviderObservedAt time.Time
}

type AdminDiscrepancyRepair struct {
	ID                   string
	Action               string
	Expected             string
	Actual               string
	PreconditionRevision int64
	Status               string
	Verification         string
	CreatedAt            time.Time
	ExecutedAt           *time.Time
}

type AdminDiscrepancyDecision struct {
	ID        string
	Reviewer  string
	Decision  string
	Reason    string
	CreatedAt time.Time
}

func (l *Lab) AdminDiscrepancyDetail(ctx context.Context, id string) (AdminDiscrepancyDetail, error) {
	tx, err := l.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return AdminDiscrepancyDetail{}, err
	}
	defer tx.Rollback()
	d := AdminDiscrepancyDetail{
		Runs:      []AdminDiscrepancyRun{},
		Repairs:   []AdminDiscrepancyRepair{},
		Decisions: []AdminDiscrepancyDecision{},
	}
	var firstSeen, lastSeen int64
	err = tx.QueryRowContext(ctx, `SELECT id,kind,object_id,classification,expected,actual,evidence,source_revision,status,first_seen_at,last_seen_at,resolution FROM discrepancies WHERE id=?`, id).
		Scan(&d.Discrepancy.ID, &d.Discrepancy.Kind, &d.Discrepancy.ObjectID, &d.Discrepancy.Classification, &d.Discrepancy.Expected, &d.Discrepancy.Actual, &d.Discrepancy.Evidence, &d.Discrepancy.SourceRevision, &d.Discrepancy.Status, &firstSeen, &lastSeen, &d.Resolution)
	if err != nil {
		return AdminDiscrepancyDetail{}, err
	}
	d.FirstSeenAt = time.Unix(0, firstSeen).UTC()
	d.LastSeenAt = time.Unix(0, lastSeen).UTC()
	runs, err := tx.QueryContext(ctx, `SELECT r.id,r.cutoff_at,r.local_observed_at,r.provider_observed_at FROM reconciliation_findings f JOIN reconciliation_runs r ON r.id=f.run_id WHERE f.discrepancy_id=? ORDER BY r.created_at DESC,r.id DESC LIMIT 101`, id)
	if err != nil {
		return AdminDiscrepancyDetail{}, err
	}
	for runs.Next() {
		var item AdminDiscrepancyRun
		var cutoff, local, provider int64
		if err := runs.Scan(&item.ID, &cutoff, &local, &provider); err != nil {
			runs.Close()
			return AdminDiscrepancyDetail{}, err
		}
		item.CutoffAt = time.Unix(0, cutoff).UTC()
		item.LocalObservedAt = time.Unix(0, local).UTC()
		item.ProviderObservedAt = time.Unix(0, provider).UTC()
		d.Runs = append(d.Runs, item)
	}
	if err := runs.Err(); err != nil {
		runs.Close()
		return AdminDiscrepancyDetail{}, err
	}
	runs.Close()
	if len(d.Runs) > 100 {
		d.Runs = d.Runs[:100]
		d.RunsTruncated = true
	}
	repairs, err := tx.QueryContext(ctx, `SELECT id,action,expected,actual,precondition_revision,status,verification,created_at,executed_at FROM repair_operations WHERE discrepancy_id=? ORDER BY created_at DESC,id DESC LIMIT 101`, id)
	if err != nil {
		return AdminDiscrepancyDetail{}, err
	}
	for repairs.Next() {
		var item AdminDiscrepancyRepair
		var created int64
		var executed sql.NullInt64
		if err := repairs.Scan(&item.ID, &item.Action, &item.Expected, &item.Actual, &item.PreconditionRevision, &item.Status, &item.Verification, &created, &executed); err != nil {
			repairs.Close()
			return AdminDiscrepancyDetail{}, err
		}
		item.CreatedAt = time.Unix(0, created).UTC()
		if executed.Valid {
			at := time.Unix(0, executed.Int64).UTC()
			item.ExecutedAt = &at
		}
		d.Repairs = append(d.Repairs, item)
	}
	if err := repairs.Err(); err != nil {
		repairs.Close()
		return AdminDiscrepancyDetail{}, err
	}
	repairs.Close()
	if len(d.Repairs) > 100 {
		d.Repairs = d.Repairs[:100]
		d.RepairsTruncated = true
	}
	decisions, err := tx.QueryContext(ctx, `SELECT id,reviewer,decision,reason,created_at FROM manual_decisions WHERE discrepancy_id=? ORDER BY created_at DESC,id DESC LIMIT 101`, id)
	if err != nil {
		return AdminDiscrepancyDetail{}, err
	}
	for decisions.Next() {
		var item AdminDiscrepancyDecision
		var created int64
		if err := decisions.Scan(&item.ID, &item.Reviewer, &item.Decision, &item.Reason, &created); err != nil {
			decisions.Close()
			return AdminDiscrepancyDetail{}, err
		}
		item.CreatedAt = time.Unix(0, created).UTC()
		d.Decisions = append(d.Decisions, item)
	}
	if err := decisions.Err(); err != nil {
		decisions.Close()
		return AdminDiscrepancyDetail{}, err
	}
	decisions.Close()
	if len(d.Decisions) > 100 {
		d.Decisions = d.Decisions[:100]
		d.DecisionsTruncated = true
	}
	if err := tx.Commit(); err != nil {
		return AdminDiscrepancyDetail{}, err
	}
	return d, nil
}
