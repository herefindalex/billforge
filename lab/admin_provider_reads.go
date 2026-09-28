package lab

import (
	"context"
	"database/sql"
	"time"
)

type AdminProviderCounts struct {
	Captures         int64 `json:"captures"`
	Refunds          int64 `json:"refunds"`
	CaptureDecisions int64 `json:"capture_decisions"`
	RefundDecisions  int64 `json:"refund_decisions"`
}

type AdminProviderOperation struct {
	ProviderKey      string `json:"provider_key"`
	SourceCaptureKey string `json:"source_capture_key,omitempty"`
	AmountMinor      int64  `json:"amount_minor"`
	Currency         string `json:"currency"`
	Status           string `json:"status"`
}

func (l *Lab) AdminPendingFaultCount(ctx context.Context) (int64, error) {
	var count int64
	err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_fault_tickets WHERE claimed_command_id IS NULL`).Scan(&count)
	return count, err
}

// AdminProviderCounts observes only the fake provider database. It cannot be
// part of the commerce database snapshot used by other admin reads.
func (l *Lab) AdminProviderCounts(ctx context.Context) (AdminProviderCounts, time.Time, error) {
	tx, err := l.provider.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return AdminProviderCounts{}, time.Time{}, err
	}
	defer tx.Rollback()
	var counts AdminProviderCounts
	for _, item := range []struct {
		table string
		value *int64
	}{
		{"captures", &counts.Captures},
		{"refunds", &counts.Refunds},
		{"decisions", &counts.CaptureDecisions},
		{"refund_decisions", &counts.RefundDecisions},
	} {
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+item.table).Scan(item.value); err != nil {
			return AdminProviderCounts{}, time.Time{}, err
		}
	}
	observedAt := time.Now().UTC()
	if err := tx.Commit(); err != nil {
		return AdminProviderCounts{}, time.Time{}, err
	}
	return counts, observedAt, nil
}

// AdminProviderOperationsPage reads a bounded page in provider insertion order.
// A new provider event cannot shift an older page addressed by its rowid cursor.
func (l *Lab) AdminProviderOperationsPage(ctx context.Context, kind, status string, before int64, limit int) ([]AdminProviderOperation, int64, time.Time, error) {
	if before < 0 || limit < 1 || limit > 100 || (status != "" && status != "succeeded" && status != "definitively_failed") {
		return nil, 0, time.Time{}, ErrAdminInvalidCommand
	}
	var selectSQL string
	switch kind {
	case "captures":
		selectSQL = `SELECT rowid,provider_key,'' AS source_capture_key,amount_minor,currency,status FROM captures`
	case "refunds":
		selectSQL = `SELECT rowid,provider_key,source_capture_key,amount_minor,currency,status FROM refunds`
	default:
		return nil, 0, time.Time{}, ErrAdminUnsupportedAction
	}
	tx, err := l.provider.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, 0, time.Time{}, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, selectSQL+` WHERE (?=0 OR rowid<?) AND (?='' OR status=?) ORDER BY rowid DESC LIMIT ?`, before, before, status, status, limit+1)
	if err != nil {
		return nil, 0, time.Time{}, err
	}
	items := make([]AdminProviderOperation, 0, limit)
	var lastRowID int64
	hasMore := false
	for rows.Next() {
		var rowID int64
		var item AdminProviderOperation
		if err := rows.Scan(&rowID, &item.ProviderKey, &item.SourceCaptureKey, &item.AmountMinor, &item.Currency, &item.Status); err != nil {
			rows.Close()
			return nil, 0, time.Time{}, err
		}
		if len(items) == limit {
			hasMore = true
			break
		}
		items = append(items, item)
		lastRowID = rowID
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, 0, time.Time{}, err
	}
	if err := rows.Close(); err != nil {
		return nil, 0, time.Time{}, err
	}
	observedAt := time.Now().UTC()
	if err := tx.Commit(); err != nil {
		return nil, 0, time.Time{}, err
	}
	if hasMore {
		return items, lastRowID, observedAt, nil
	}
	return items, 0, observedAt, nil
}
