package lab

import (
	"context"
	"database/sql"
	"time"
)

type AdminShadowCursor struct {
	AtNano int64  `json:"at_nano"`
	ID     string `json:"id"`
}

type AdminShadowPage struct {
	Items []ShadowComparison
	Next  *AdminShadowCursor
}

type AdminProvenancePage struct {
	Items  []LegacyProvenance
	NextID string
}

func (l *Lab) AdminAccountMigrationShadows(ctx context.Context, accountID string, after *AdminShadowCursor, limit int) (AdminShadowPage, error) {
	if limit < 1 || limit > 100 || (after != nil && (after.AtNano == 0 || after.ID == "")) {
		return AdminShadowPage{}, ErrAdminInvalidCommand
	}
	tx, err := l.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return AdminShadowPage{}, err
	}
	defer tx.Rollback()
	if _, err := loadAccountLink(ctx, tx, accountID); err != nil {
		return AdminShadowPage{}, err
	}
	var cursorAt any
	var cursorID string
	if after != nil {
		cursorAt, cursorID = after.AtNano, after.ID
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,kind,object_id,expected,actual,matched,latency_millis,observed_at FROM migration_shadows WHERE legacy_account_id=? AND (? IS NULL OR observed_at<? OR (observed_at=? AND id<?)) ORDER BY observed_at DESC,id DESC LIMIT ?`, accountID, cursorAt, cursorAt, cursorAt, cursorID, limit+1)
	if err != nil {
		return AdminShadowPage{}, err
	}
	page := AdminShadowPage{Items: make([]ShadowComparison, 0, limit)}
	for rows.Next() {
		var item ShadowComparison
		var matched int
		var observed int64
		if err := rows.Scan(&item.ID, &item.Kind, &item.ObjectID, &item.Expected, &item.Actual, &matched, &item.LatencyMillis, &observed); err != nil {
			rows.Close()
			return AdminShadowPage{}, err
		}
		item.LegacyAccountID = accountID
		item.Matched = matched != 0
		item.ObservedAt = time.Unix(0, observed).UTC()
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return AdminShadowPage{}, err
	}
	rows.Close()
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		last := page.Items[len(page.Items)-1]
		page.Next = &AdminShadowCursor{AtNano: last.ObservedAt.UnixNano(), ID: last.ID}
	}
	if err := tx.Commit(); err != nil {
		return AdminShadowPage{}, err
	}
	return page, nil
}

func (l *Lab) AdminAccountMigrationProvenance(ctx context.Context, accountID, afterID string, limit int) (AdminProvenancePage, error) {
	if limit < 1 || limit > 100 {
		return AdminProvenancePage{}, ErrAdminInvalidCommand
	}
	tx, err := l.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return AdminProvenancePage{}, err
	}
	defer tx.Rollback()
	if _, err := loadAccountLink(ctx, tx, accountID); err != nil {
		return AdminProvenancePage{}, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT legacy_invoice_id,legacy_subscription_id,commerce_subscription_id,commerce_invoice_id,price_version_id,status,evidence FROM legacy_provenance WHERE legacy_account_id=? AND legacy_invoice_id>? ORDER BY legacy_invoice_id LIMIT ?`, accountID, afterID, limit+1)
	if err != nil {
		return AdminProvenancePage{}, err
	}
	page := AdminProvenancePage{Items: make([]LegacyProvenance, 0, limit)}
	for rows.Next() {
		var item LegacyProvenance
		if err := rows.Scan(&item.LegacyInvoiceID, &item.LegacySubscriptionID, &item.CommerceSubscriptionID, &item.CommerceInvoiceID, &item.PriceVersionID, &item.Status, &item.Evidence); err != nil {
			rows.Close()
			return AdminProvenancePage{}, err
		}
		item.LegacyAccountID = accountID
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return AdminProvenancePage{}, err
	}
	rows.Close()
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		page.NextID = page.Items[len(page.Items)-1].LegacyInvoiceID
	}
	if err := tx.Commit(); err != nil {
		return AdminProvenancePage{}, err
	}
	return page, nil
}
