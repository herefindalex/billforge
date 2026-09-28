package lab

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

var (
	ErrAdminLeaseBusy = errors.New("admin command is owned by another worker")
	ErrAdminLeaseLost = errors.New("admin command lease generation changed")
)

type adminLeaseContextKey struct{}
type adminLeaseCommandContextKey struct{}

const adminLeaseDuration = 3 * time.Second

func adminLeaseGeneration(ctx context.Context) int64 {
	value, _ := ctx.Value(adminLeaseContextKey{}).(int64)
	return value
}

// adminMarkSubmitted fences a managed worker before a provider call. The
// operation transition and lease check commit together, so a replacement
// worker cannot revoke an operation that an old worker is about to send.
func (l *Lab) adminMarkSubmitted(ctx context.Context, table, operationID string) error {
	if table != "payment_operations" && table != "refund_operations" {
		return ErrConflict
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if commandID, _ := ctx.Value(adminLeaseCommandContextKey{}).(string); commandID != "" {
		if err := adminReserveWriterTx(ctx, tx, commandID); err != nil {
			return err
		}
		if err := adminCheckLeaseTx(ctx, tx, commandID); err != nil {
			return err
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE `+table+` SET status='submitted' WHERE id=? AND status='created'`, operationID)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return ErrConflict
	}
	return tx.Commit()
}

// Reserve SQLite's writer slot before reading command or domain state in a
// transaction that may later write. This avoids a read-to-write lock upgrade
// failing when a different command worker writes concurrently.
func adminReserveWriterTx(ctx context.Context, tx *sql.Tx, commandID string) error {
	_, err := tx.ExecContext(ctx, `UPDATE admin_commands SET lease_generation=lease_generation WHERE id=? AND status NOT IN ('succeeded','failed')`, commandID)
	return err
}

func adminCheckLeaseTx(ctx context.Context, tx *sql.Tx, commandID string) error {
	expected := adminLeaseGeneration(ctx)
	if expected == 0 {
		return nil
	} // domain-only callers do not own an admin worker lease
	var current int64
	var until string
	if err := tx.QueryRowContext(ctx, `SELECT lease_generation,COALESCE(lease_until,'') FROM admin_commands WHERE id=?`, commandID).Scan(&current, &until); err != nil {
		return err
	}
	if current != expected {
		return ErrAdminLeaseLost
	}
	deadline, err := time.Parse(time.RFC3339Nano, until)
	if err != nil || !time.Now().UTC().Before(deadline) {
		return ErrAdminLeaseLost
	}
	return nil
}

func (l *Lab) adminAcquireLease(ctx context.Context, commandID string) (int64, error) {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	// Acquire the SQLite writer reservation before reading lease state. A
	// deferred read followed by UPDATE can fail with SQLITE_BUSY_SNAPSHOT when
	// another process updates a different command between those statements.
	if _, err := tx.ExecContext(ctx, `UPDATE admin_commands SET lease_generation=lease_generation WHERE id=? AND status NOT IN ('succeeded','failed')`, commandID); err != nil {
		return 0, err
	}
	var status, owner, until string
	var generation int64
	if err := tx.QueryRowContext(ctx, `SELECT status,COALESCE(lease_owner,''),lease_generation,COALESCE(lease_until,'') FROM admin_commands WHERE id=?`, commandID).Scan(&status, &owner, &generation, &until); err != nil {
		return 0, err
	}
	if status == "succeeded" || status == "failed" {
		return 0, tx.Commit()
	}
	now := time.Now().UTC()
	if owner != "" && owner != l.workerID && until != "" {
		deadline, err := time.Parse(time.RFC3339Nano, until)
		if err != nil {
			return 0, err
		}
		if now.Before(deadline) {
			return 0, ErrAdminLeaseBusy
		}
	}
	generation++
	if _, err := tx.ExecContext(ctx, `UPDATE admin_commands SET lease_owner=?,lease_generation=?,lease_until=? WHERE id=?`, l.workerID, generation, now.Add(adminLeaseDuration).Format(time.RFC3339Nano), commandID); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return generation, nil
}

func (l *Lab) adminRenewLease(ctx context.Context, commandID string) error {
	generation := adminLeaseGeneration(ctx)
	if generation == 0 {
		return nil
	}
	result, err := l.db.ExecContext(ctx, `UPDATE admin_commands SET lease_until=? WHERE id=? AND lease_owner=? AND lease_generation=?`, time.Now().UTC().Add(adminLeaseDuration).Format(time.RFC3339Nano), commandID, l.workerID, generation)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrAdminLeaseLost
	}
	return nil
}

func (l *Lab) adminReleaseLease(commandID string, generation int64) {
	if generation == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, _ = l.db.ExecContext(ctx, `UPDATE admin_commands SET lease_owner=NULL,lease_until=NULL WHERE id=? AND lease_owner=? AND lease_generation=?`, commandID, l.workerID, generation)
}
