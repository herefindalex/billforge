package lab

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAdminSchemaUpgradesExistingV1(t *testing.T) {
	dir := t.TempDir()
	l, err := Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	ctx := context.Background()
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE admin_schema_versions(version INTEGER PRIMARY KEY,checksum TEXT NOT NULL,applied_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for _, statement := range adminSchemaStatements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	sum := sha256.Sum256([]byte(strings.Join(adminSchemaStatements, ";\n")))
	if _, err := tx.ExecContext(ctx, `INSERT INTO admin_schema_versions(version,checksum,applied_at) VALUES(1,?,?)`, hex.EncodeToString(sum[:]), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `INSERT INTO admin_commands(id,actor_id,idempotency_key,action_id,target_id,payload_json,payload_hash,status,business_time,created_at,updated_at) VALUES('legacy-request-command','local-admin','legacy-request-key','C01','','{}','legacy-hash','succeeded',?,?,?)`, stamp, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO admin_audit(command_id,actor_id,action_id,target_id,reason,recorded_at) VALUES('legacy-request-command','local-admin','C01','','succeeded',?)`, stamp); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	var versions, previewClockColumns, commandRequestColumns, auditRequestColumns int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_schema_versions`).Scan(&versions); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('admin_previews') WHERE name='clock_revision'`).Scan(&previewClockColumns); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('admin_commands') WHERE name='request_id'`).Scan(&commandRequestColumns); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('admin_audit') WHERE name='request_id'`).Scan(&auditRequestColumns); err != nil {
		t.Fatal(err)
	}
	if versions != 8 || previewClockColumns != 1 || commandRequestColumns != 1 || auditRequestColumns != 1 {
		t.Fatalf("v1 upgrade: versions=%d clock_columns=%d command_request=%d audit_request=%d", versions, previewClockColumns, commandRequestColumns, auditRequestColumns)
	}
	var legacyCommandRequest, legacyAuditRequest sql.NullString
	if err := l.db.QueryRowContext(ctx, `SELECT request_id FROM admin_commands WHERE id='legacy-request-command'`).Scan(&legacyCommandRequest); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT request_id FROM admin_audit WHERE command_id='legacy-request-command'`).Scan(&legacyAuditRequest); err != nil {
		t.Fatal(err)
	}
	if legacyCommandRequest.Valid || legacyAuditRequest.Valid {
		t.Fatalf("migration fabricated legacy request ids: command=%v audit=%v", legacyCommandRequest, legacyAuditRequest)
	}
	var customerIndexes int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name IN ('admin_quotes_customer_idx','admin_subscriptions_customer_idx')`).Scan(&customerIndexes); err != nil {
		t.Fatal(err)
	}
	if customerIndexes != 2 {
		t.Fatalf("customer read indexes=%d", customerIndexes)
	}
	var historyIndexes int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name IN ('admin_audit_events_object_kind_at_idx','admin_immediate_changes_subscription_requested_idx','admin_payment_operations_invoice_idx')`).Scan(&historyIndexes); err != nil {
		t.Fatal(err)
	}
	if historyIndexes != 3 {
		t.Fatalf("subscription history indexes=%d", historyIndexes)
	}
	var discrepancyIndexes int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name IN ('admin_reconciliation_findings_discrepancy_idx','admin_repair_operations_discrepancy_created_idx','admin_manual_decisions_discrepancy_created_idx')`).Scan(&discrepancyIndexes); err != nil {
		t.Fatal(err)
	}
	if discrepancyIndexes != 3 {
		t.Fatalf("discrepancy detail indexes=%d", discrepancyIndexes)
	}
	var migrationIndexes int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name IN ('admin_migration_shadows_account_observed_idx','admin_legacy_provenance_account_invoice_idx','admin_account_migration_events_account_at_idx')`).Scan(&migrationIndexes); err != nil {
		t.Fatal(err)
	}
	if migrationIndexes != 3 {
		t.Fatalf("account migration detail indexes=%d", migrationIndexes)
	}
	var invoiceIndexes int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name IN ('admin_corrections_invoice_created_idx','admin_credit_applications_invoice_created_idx','admin_credit_grants_invoice_created_idx','admin_refund_operations_grant_created_idx')`).Scan(&invoiceIndexes); err != nil {
		t.Fatal(err)
	}
	if invoiceIndexes != 4 {
		t.Fatalf("invoice provenance indexes=%d", invoiceIndexes)
	}
}

func TestAdminSchemaUpgradeIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	l, err := Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if err := l.InitAdmin(ctx); err != nil {
			t.Fatalf("upgrade %d: %v", i+1, err)
		}
	}
	var count int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_schema_versions`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 8 {
		t.Fatalf("schema ledger has %d rows", count)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM price_versions`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		t.Fatal("existing price facts disappeared")
	}
}

func TestAdminSchemaUpgradesExistingV4WithoutChangingEarlierChecksums(t *testing.T) {
	dir := t.TempDir()
	l, err := Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	ctx := context.Background()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	paid := purchase(t, l)
	if _, err := l.DispatchCapture(ctx, paid.OperationID, ""); err != nil {
		t.Fatal(err)
	}
	var v4Before string
	if err := l.db.QueryRowContext(ctx, `SELECT checksum FROM admin_schema_versions WHERE version=4`).Scan(&v4Before); err != nil {
		t.Fatal(err)
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for _, index := range []string{
		"admin_reconciliation_findings_discrepancy_idx", "admin_repair_operations_discrepancy_created_idx", "admin_manual_decisions_discrepancy_created_idx",
		"admin_migration_shadows_account_observed_idx", "admin_legacy_provenance_account_invoice_idx", "admin_account_migration_events_account_at_idx",
		"admin_corrections_invoice_created_idx", "admin_credit_applications_invoice_created_idx", "admin_credit_grants_invoice_created_idx", "admin_refund_operations_grant_created_idx",
	} {
		if _, err := tx.ExecContext(ctx, `DROP INDEX `+index); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM admin_schema_versions WHERE version IN (5,6,7)`); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	var v4After string
	if err := l.db.QueryRowContext(ctx, `SELECT checksum FROM admin_schema_versions WHERE version=4`).Scan(&v4After); err != nil {
		t.Fatal(err)
	}
	if v4After != v4Before {
		t.Fatal("v4 checksum changed during later upgrades")
	}
	var indexes int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name IN ('admin_reconciliation_findings_discrepancy_idx','admin_repair_operations_discrepancy_created_idx','admin_manual_decisions_discrepancy_created_idx')`).Scan(&indexes); err != nil {
		t.Fatal(err)
	}
	if indexes != 3 {
		t.Fatalf("v5 indexes = %d", indexes)
	}
	var total int64
	var operationStatus string
	if err := l.db.QueryRowContext(ctx, `SELECT total_minor FROM invoices WHERE id=?`, paid.InvoiceID).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT status FROM payment_operations WHERE id=?`, paid.OperationID).Scan(&operationStatus); err != nil {
		t.Fatal(err)
	}
	if total != 2000 || operationStatus != "succeeded" || captureCount(t, l) != 1 {
		t.Fatalf("v4-v6 upgrade changed paid invoice: total=%d operation=%s captures=%d", total, operationStatus, captureCount(t, l))
	}
}

func TestAdminSchemaFailureRollsBack(t *testing.T) {
	dir := t.TempDir()
	l, err := Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	ctx := context.Background()
	paid := purchase(t, l)
	if _, err := l.DispatchCapture(ctx, paid.OperationID, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := l.db.ExecContext(ctx, `CREATE VIEW admin_commands AS SELECT 1 AS id`); err != nil {
		t.Fatal(err)
	}
	if err := l.InitAdmin(ctx); err == nil {
		t.Fatal("upgrade unexpectedly succeeded")
	}
	var count int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE name='admin_schema_versions'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("failed upgrade left a schema ledger")
	}
	var total int64
	if err := l.db.QueryRowContext(ctx, `SELECT total_minor FROM invoices WHERE id=?`, paid.InvoiceID).Scan(&total); err != nil || total != 2000 || captureCount(t, l) != 1 {
		t.Fatalf("failed admin upgrade changed existing financial facts: total=%d captures=%d err=%v", total, captureCount(t, l), err)
	}
	quote, err := l.CreateQuote(ctx, "cli-after-failed-admin-upgrade", "basic")
	if err != nil {
		t.Fatalf("legacy quote write after failed admin upgrade: %v", err)
	}
	if _, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "cli-after-failed-admin-upgrade-001"); err != nil {
		t.Fatalf("legacy acceptance after failed admin upgrade: %v", err)
	}
	if _, err := l.db.ExecContext(ctx, `DROP VIEW admin_commands`); err != nil {
		t.Fatal(err)
	}
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatalf("retry after removing blocker: %v", err)
	}
}
