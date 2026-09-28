package lab

import (
	"context"
	"database/sql"
	"time"
)

// AdminCreditDetail reads the grant provenance and current money budget from
// the same SQLite snapshot. Submitted and unknown refunds remain reserved.
type AdminCreditDetail struct {
	ID                 string
	ReleaseID          string
	SourceCorrectionID string
	SourceOperationID  string
	SourceInvoiceID    string
	CreatedAt          time.Time
	Balance            CreditBalance
}

func (l *Lab) AdminCreditDetail(ctx context.Context, grantID string) (AdminCreditDetail, error) {
	tx, err := l.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return AdminCreditDetail{}, err
	}
	defer tx.Rollback()

	var detail AdminCreditDetail
	var createdAt int64
	if err := tx.QueryRowContext(ctx, `SELECT g.id,g.release_id,r.correction_id,g.source_operation_id,g.source_invoice_id,g.created_at FROM credit_grants g JOIN allocation_releases r ON r.id=g.release_id WHERE g.id=?`, grantID).
		Scan(&detail.ID, &detail.ReleaseID, &detail.SourceCorrectionID, &detail.SourceOperationID, &detail.SourceInvoiceID, &createdAt); err != nil {
		return AdminCreditDetail{}, err
	}
	detail.CreatedAt = time.Unix(0, createdAt).UTC()
	detail.Balance, err = loadCreditBalance(ctx, tx, grantID)
	if err != nil {
		return AdminCreditDetail{}, err
	}
	if err := tx.Commit(); err != nil {
		return AdminCreditDetail{}, err
	}
	return detail, nil
}
