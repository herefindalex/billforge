package lab

import (
	"context"
	"database/sql"
)

// lastCommittedBatchSource advances selection only after a preview becomes a job.
// Repeated previews therefore keep the same membership until a command is accepted.
func lastCommittedBatchSource(ctx context.Context, tx *sql.Tx, actionID string) (string, error) {
	var source string
	err := tx.QueryRowContext(ctx, `SELECT p.source_versions_json
		FROM admin_jobs j
		JOIN admin_commands c ON c.id=j.command_id
		JOIN admin_previews p ON p.id=c.preview_id
		WHERE j.kind=?
		ORDER BY j.created_at DESC,j.id DESC LIMIT 1`, actionID).Scan(&source)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return source, err
}
