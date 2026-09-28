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
