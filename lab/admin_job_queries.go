package lab

import (
	"context"
	"database/sql"
	"encoding/json"
)

type AdminJobItem struct {
	ID         string          `json:"id"`
	TargetType string          `json:"target_type"`
	TargetID   string          `json:"target_id"`
	PeriodKey  string          `json:"period_key"`
	Status     string          `json:"status"`
	ResultRefs json.RawMessage `json:"result_refs,omitempty"`
	ErrorCode  string          `json:"error_code,omitempty"`
	UpdatedAt  string          `json:"updated_at"`
}

type AdminJob struct {
	ID        string         `json:"id"`
	CommandID string         `json:"command_id"`
	Kind      string         `json:"kind"`
	Status    string         `json:"status"`
	CreatedAt string         `json:"created_at"`
	UpdatedAt string         `json:"updated_at"`
	Items     []AdminJobItem `json:"items"`
}

type AdminJobSummary struct {
	ID             string `json:"id"`
	CommandID      string `json:"command_id"`
	Kind           string `json:"kind"`
	Status         string `json:"status"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
	TotalItems     int64  `json:"total_items"`
	SucceededItems int64  `json:"succeeded_items"`
	AttentionItems int64  `json:"attention_items"`
}

// AdminJobsPage lists the newest jobs first. A rowid cursor keeps new jobs
// from shifting a page that an administrator has already read.
func (l *Lab) AdminJobsPage(ctx context.Context, before int64, limit int) ([]AdminJobSummary, int64, error) {
	if before < 0 || limit < 1 || limit > 100 {
		return nil, 0, ErrAdminInvalidCommand
	}
	tx, err := l.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT j.rowid,j.id,j.command_id,j.kind,j.status,j.created_at,j.updated_at,
		(SELECT COUNT(*) FROM admin_job_items i WHERE i.job_id=j.id),
		(SELECT COUNT(*) FROM admin_job_items i WHERE i.job_id=j.id AND i.status='succeeded'),
		(SELECT COUNT(*) FROM admin_job_items i WHERE i.job_id=j.id AND i.status IN ('failed','conflicted','waiting_verification'))
		FROM admin_jobs j WHERE (?=0 OR j.rowid<?) ORDER BY j.rowid DESC LIMIT ?`, before, before, limit+1)
	if err != nil {
		return nil, 0, err
	}
	jobs := make([]AdminJobSummary, 0, limit)
	var lastRowID int64
	hasMore := false
	for rows.Next() {
		var rowID int64
		var job AdminJobSummary
		if err := rows.Scan(&rowID, &job.ID, &job.CommandID, &job.Kind, &job.Status, &job.CreatedAt, &job.UpdatedAt, &job.TotalItems, &job.SucceededItems, &job.AttentionItems); err != nil {
			rows.Close()
			return nil, 0, err
		}
		if len(jobs) == limit {
			hasMore = true
			break
		}
		jobs = append(jobs, job)
		lastRowID = rowID
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, 0, err
	}
	if err := rows.Close(); err != nil {
		return nil, 0, err
	}
	if err := tx.Commit(); err != nil {
		return nil, 0, err
	}
	if hasMore {
		return jobs, lastRowID, nil
	}
	return jobs, 0, nil
}

func (l *Lab) AdminJob(ctx context.Context, id string) (AdminJob, error) {
	tx, err := l.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return AdminJob{}, err
	}
	defer tx.Rollback()
	job := AdminJob{Items: []AdminJobItem{}}
	if err := tx.QueryRowContext(ctx, `SELECT id,command_id,kind,status,created_at,updated_at FROM admin_jobs WHERE id=?`, id).Scan(&job.ID, &job.CommandID, &job.Kind, &job.Status, &job.CreatedAt, &job.UpdatedAt); err != nil {
		return AdminJob{}, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,target_type,target_id,period_key,status,COALESCE(result_refs_json,''),COALESCE(error_code,''),updated_at FROM admin_job_items WHERE job_id=? ORDER BY rowid`, id)
	if err != nil {
		return AdminJob{}, err
	}
	for rows.Next() {
		var item AdminJobItem
		var refs string
		if err := rows.Scan(&item.ID, &item.TargetType, &item.TargetID, &item.PeriodKey, &item.Status, &refs, &item.ErrorCode, &item.UpdatedAt); err != nil {
			rows.Close()
			return AdminJob{}, err
		}
		if refs != "" {
			item.ResultRefs = json.RawMessage(refs)
		}
		job.Items = append(job.Items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return AdminJob{}, err
	}
	if err := rows.Close(); err != nil {
		return AdminJob{}, err
	}
	if err := tx.Commit(); err != nil {
		return AdminJob{}, err
	}
	return job, nil
}
