package lab

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// migrateLegacy removes the E-stage one-invoice/one-operation constraints.
// It runs before the current schema is installed, preserving old IDs and facts.
func migrateLegacy(db *sql.DB) error {
	var invoiceSQL, operationSQL string
	err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='invoices'`).Scan(&invoiceSQL)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='payment_operations'`).Scan(&operationSQL); err != nil {
		return err
	}
	oldInvoice := strings.Contains(strings.ToLower(invoiceSQL), "subscription_id text not null unique")
	oldOperation := strings.Contains(strings.ToLower(operationSQL), "invoice_id text not null unique") ||
		!strings.Contains(strings.ToLower(operationSQL), "'cancelled'")
	if !oldInvoice && !oldOperation {
		return nil
	}
	if _, err := db.Exec(`PRAGMA foreign_keys=OFF`); err != nil {
		return err
	}
	defer db.Exec(`PRAGMA foreign_keys=ON`)
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if oldInvoice {
		if _, err = tx.Exec(`CREATE TABLE invoices_new (
id TEXT PRIMARY KEY, subscription_id TEXT NOT NULL REFERENCES subscriptions(id),
total_minor INTEGER NOT NULL CHECK(total_minor>0), currency TEXT NOT NULL, finalized_at INTEGER);
INSERT INTO invoices_new SELECT id,subscription_id,total_minor,currency,finalized_at FROM invoices;
DROP TABLE invoices;
ALTER TABLE invoices_new RENAME TO invoices;`); err != nil {
			return fmt.Errorf("migrate invoices: %w", err)
		}
	}
	if oldOperation {
		if _, err = tx.Exec(`CREATE TABLE payment_operations_new (
id TEXT PRIMARY KEY, invoice_id TEXT NOT NULL REFERENCES invoices(id),
provider_key TEXT NOT NULL UNIQUE, amount_minor INTEGER NOT NULL CHECK(amount_minor>0),
currency TEXT NOT NULL, status TEXT NOT NULL CHECK(status IN ('created','submitted','unknown','succeeded','definitively_failed','cancelled')));
INSERT INTO payment_operations_new SELECT id,invoice_id,provider_key,amount_minor,currency,status FROM payment_operations;
DROP TABLE payment_operations;
ALTER TABLE payment_operations_new RENAME TO payment_operations;`); err != nil {
			return fmt.Errorf("migrate payment operations: %w", err)
		}
	}
	return tx.Commit()
}

func ensureEntitlementColumns(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(entitlements)`)
	if err != nil {
		return err
	}
	columns := map[string]bool{}
	for rows.Next() {
		var cid, notNull, pk int
		var name, kind string
		var defaultValue sql.NullString
		if err = rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &pk); err != nil {
			rows.Close()
			return err
		}
		columns[name] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, column := range []struct{ name, ddl string }{
		{"reason", `ALTER TABLE entitlements ADD COLUMN reason TEXT NOT NULL DEFAULT 'payment_confirmed'`},
		{"source_invoice_id", `ALTER TABLE entitlements ADD COLUMN source_invoice_id TEXT REFERENCES invoices(id)`},
		{"grace_deadline", `ALTER TABLE entitlements ADD COLUMN grace_deadline INTEGER`},
	} {
		if !columns[column.name] {
			if _, err := db.Exec(column.ddl); err != nil {
				return err
			}
		}
	}
	return nil
}

func backfillInitialPeriods(db *sql.DB) error {
	rows, err := db.Query(`SELECT s.id,s.created_at,i.id FROM subscriptions s
JOIN invoices i ON i.subscription_id=s.id
WHERE NOT EXISTS (SELECT 1 FROM billing_periods p WHERE p.subscription_id=s.id)
ORDER BY i.finalized_at,i.id`)
	if err != nil {
		return err
	}
	type initial struct {
		sub, invoice string
		start        int64
	}
	var pending []initial
	seen := map[string]bool{}
	for rows.Next() {
		var x initial
		if err = rows.Scan(&x.sub, &x.start, &x.invoice); err != nil {
			rows.Close()
			return err
		}
		if !seen[x.sub] {
			pending = append(pending, x)
			seen[x.sub] = true
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, x := range pending {
		start := time.Unix(0, x.start).UTC()
		end := cycleBoundary(start, 1)
		if _, err := db.Exec(`INSERT INTO billing_periods(subscription_id,period_index,period_start,period_end,due_at,invoice_id)
VALUES(?,0,?,?,?,?) ON CONFLICT(subscription_id,period_index) DO NOTHING`, x.sub, x.start, end.UnixNano(), x.start, x.invoice); err != nil {
			return err
		}
	}
	return nil
}
