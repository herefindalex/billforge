package lab

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

const adminSchemaVersion = 1

var adminSchemaV2Statements = []string{
	`ALTER TABLE admin_previews ADD COLUMN clock_revision INTEGER NOT NULL DEFAULT 0`,
	`CREATE TRIGGER admin_preview_clock_revision AFTER INSERT ON admin_previews
	BEGIN UPDATE admin_previews SET clock_revision=COALESCE((SELECT revision FROM admin_lab_clock WHERE id=1),0) WHERE id=NEW.id; END`,
}

var adminSchemaV3Statements = []string{
	`CREATE INDEX admin_quotes_customer_idx ON quotes(customer_id,id)`,
	`CREATE INDEX admin_subscriptions_customer_idx ON subscriptions(customer_id,id)`,
	`CREATE INDEX admin_invoices_subscription_idx ON invoices(subscription_id,id)`,
	`CREATE INDEX admin_credit_grants_invoice_idx ON credit_grants(source_invoice_id,id)`,
	`CREATE INDEX admin_schedules_subscription_status_idx ON subscription_schedules(subscription_id,status,effective_at)`,
}

var adminSchemaV4Statements = []string{
	`CREATE INDEX admin_audit_events_object_kind_at_idx ON audit_events(object_id,kind,at)`,
	`CREATE INDEX admin_immediate_changes_subscription_requested_idx ON immediate_changes(subscription_id,requested_at)`,
	`CREATE INDEX admin_payment_operations_invoice_idx ON payment_operations(invoice_id,id)`,
}

var adminSchemaV5Statements = []string{
	`CREATE INDEX admin_reconciliation_findings_discrepancy_idx ON reconciliation_findings(discrepancy_id,run_id)`,
	`CREATE INDEX admin_repair_operations_discrepancy_created_idx ON repair_operations(discrepancy_id,created_at,id)`,
	`CREATE INDEX admin_manual_decisions_discrepancy_created_idx ON manual_decisions(discrepancy_id,created_at,id)`,
}

var adminSchemaV6Statements = []string{
	`CREATE INDEX admin_migration_shadows_account_observed_idx ON migration_shadows(legacy_account_id,observed_at DESC,id DESC)`,
	`CREATE INDEX admin_legacy_provenance_account_invoice_idx ON legacy_provenance(legacy_account_id,legacy_invoice_id)`,
	`CREATE INDEX admin_account_migration_events_account_at_idx ON account_migration_events(legacy_account_id,at DESC,id DESC)`,
}

var adminSchemaV7Statements = []string{
	`CREATE INDEX admin_corrections_invoice_created_idx ON corrections(invoice_id,created_at DESC,id DESC)`,
	`CREATE INDEX admin_credit_applications_invoice_created_idx ON credit_applications(invoice_id,created_at DESC,id DESC)`,
	`CREATE INDEX admin_credit_grants_invoice_created_idx ON credit_grants(source_invoice_id,created_at DESC,id DESC)`,
	`CREATE INDEX admin_refund_operations_grant_created_idx ON refund_operations(grant_id,created_at DESC,id DESC)`,
}

var adminSchemaV8Statements = []string{
	`ALTER TABLE admin_commands ADD COLUMN request_id TEXT`,
	`ALTER TABLE admin_audit ADD COLUMN request_id TEXT`,
	`CREATE INDEX admin_commands_request_id_idx ON admin_commands(request_id)`,
	`CREATE INDEX admin_audit_request_id_idx ON admin_audit(request_id)`,
}

var adminSchemaStatements = []string{
	`CREATE TABLE admin_commands (
		id TEXT PRIMARY KEY,
		actor_id TEXT NOT NULL,
		idempotency_key TEXT NOT NULL,
		action_id TEXT NOT NULL,
		target_id TEXT NOT NULL DEFAULT '',
		payload_json TEXT NOT NULL,
		payload_hash TEXT NOT NULL,
		preview_id TEXT,
		status TEXT NOT NULL CHECK(status IN ('accepted','running','succeeded','failed','waiting_verification')),
		business_time TEXT NOT NULL,
		clock_revision INTEGER NOT NULL DEFAULT 0,
		lease_owner TEXT,
		lease_generation INTEGER NOT NULL DEFAULT 0,
		lease_until TEXT,
		result_refs_json TEXT,
		error_code TEXT,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		UNIQUE(actor_id, idempotency_key)
	)`,
	`CREATE INDEX admin_commands_status_idx ON admin_commands(status, updated_at)`,
	`CREATE TABLE admin_previews (
		id TEXT PRIMARY KEY,
		actor_id TEXT NOT NULL,
		action_id TEXT NOT NULL,
		target_id TEXT NOT NULL DEFAULT '',
		intent_hash TEXT NOT NULL,
		intent_json TEXT NOT NULL,
		source_versions_json TEXT NOT NULL,
		impact_json TEXT NOT NULL,
		expires_at TEXT NOT NULL,
		claimed_command_id TEXT UNIQUE REFERENCES admin_commands(id),
		created_at TEXT NOT NULL
	)`,
	`CREATE INDEX admin_previews_expiry_idx ON admin_previews(expires_at)`,
	`CREATE TABLE admin_command_receipts (
		command_id TEXT PRIMARY KEY REFERENCES admin_commands(id),
		domain_request_key TEXT NOT NULL UNIQUE,
		result_refs_json TEXT NOT NULL,
		committed_at TEXT NOT NULL
	)`,
	`CREATE TABLE admin_jobs (
		id TEXT PRIMARY KEY,
		command_id TEXT NOT NULL UNIQUE REFERENCES admin_commands(id),
		kind TEXT NOT NULL,
		status TEXT NOT NULL CHECK(status IN ('accepted','running','succeeded','failed','partial','paused')),
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`,
	`CREATE TABLE admin_job_items (
		id TEXT PRIMARY KEY,
		job_id TEXT NOT NULL REFERENCES admin_jobs(id),
		target_type TEXT NOT NULL,
		target_id TEXT NOT NULL,
		period_key TEXT NOT NULL DEFAULT '',
		payload_hash TEXT NOT NULL,
		status TEXT NOT NULL CHECK(status IN ('accepted','running','succeeded','failed','conflicted','waiting_verification','skipped')),
		result_refs_json TEXT,
		error_code TEXT,
		updated_at TEXT NOT NULL,
		UNIQUE(job_id,target_type,target_id,period_key)
	)`,
	`CREATE TABLE admin_audit (
		sequence INTEGER PRIMARY KEY AUTOINCREMENT,
		command_id TEXT,
		actor_id TEXT NOT NULL,
		action_id TEXT NOT NULL,
		target_id TEXT NOT NULL DEFAULT '',
		reason TEXT NOT NULL DEFAULT '',
		before_json TEXT,
		after_json TEXT,
		recorded_at TEXT NOT NULL
	)`,
	`CREATE TRIGGER admin_audit_no_update BEFORE UPDATE ON admin_audit BEGIN SELECT RAISE(ABORT,'admin audit is append only'); END`,
	`CREATE TRIGGER admin_audit_no_delete BEFORE DELETE ON admin_audit BEGIN SELECT RAISE(ABORT,'admin audit is append only'); END`,
	`CREATE TABLE admin_lab_clock (
		id INTEGER PRIMARY KEY CHECK(id=1),
		mode TEXT NOT NULL CHECK(mode IN ('real','fixed')),
		value_utc TEXT,
		revision INTEGER NOT NULL,
		updated_at TEXT NOT NULL
	)`,
	`CREATE TABLE admin_fault_tickets (
		id TEXT PRIMARY KEY,
		operation_kind TEXT NOT NULL,
		operation_id TEXT NOT NULL,
		mode TEXT NOT NULL CHECK(mode IN ('lost_response','crash_after_provider')),
		claimed_command_id TEXT REFERENCES admin_commands(id),
		created_at TEXT NOT NULL,
		UNIQUE(operation_kind,operation_id)
	)`,
}

// InitAdmin adds admin tables and read indexes. Existing commerce facts are left intact.
// The schema and version row commit together, so a failed upgrade can be retried.
func (l *Lab) InitAdmin(ctx context.Context) error {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS admin_schema_versions (
		version INTEGER PRIMARY KEY,
		checksum TEXT NOT NULL,
		applied_at TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("create admin schema ledger: %w", err)
	}
	sum := sha256.Sum256([]byte(strings.Join(adminSchemaStatements, ";\n")))
	want := hex.EncodeToString(sum[:])
	var got string
	err = tx.QueryRowContext(ctx, `SELECT checksum FROM admin_schema_versions WHERE version=?`, adminSchemaVersion).Scan(&got)
	if err == nil {
		if got != want {
			return errors.New("admin schema checksum mismatch")
		}
	} else if errors.Is(err, sql.ErrNoRows) {
		for i, statement := range adminSchemaStatements {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("admin schema statement %d: %w", i+1, err)
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO admin_schema_versions(version,checksum,applied_at) VALUES(?,?,?)`, adminSchemaVersion, want, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
	} else {
		return err
	}
	v2Sum := sha256.Sum256([]byte(strings.Join(adminSchemaV2Statements, ";\n")))
	v2Want := hex.EncodeToString(v2Sum[:])
	err = tx.QueryRowContext(ctx, `SELECT checksum FROM admin_schema_versions WHERE version=2`).Scan(&got)
	if err == nil {
		if got != v2Want {
			return errors.New("admin schema v2 checksum mismatch")
		}
	} else if errors.Is(err, sql.ErrNoRows) {
		for i, statement := range adminSchemaV2Statements {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("admin schema v2 statement %d: %w", i+1, err)
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO admin_schema_versions(version,checksum,applied_at) VALUES(2,?,?)`, v2Want, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
	} else {
		return err
	}
	v3Sum := sha256.Sum256([]byte(strings.Join(adminSchemaV3Statements, ";\n")))
	v3Want := hex.EncodeToString(v3Sum[:])
	err = tx.QueryRowContext(ctx, `SELECT checksum FROM admin_schema_versions WHERE version=3`).Scan(&got)
	if err == nil {
		if got != v3Want {
			return errors.New("admin schema v3 checksum mismatch")
		}
	} else if errors.Is(err, sql.ErrNoRows) {
		for i, statement := range adminSchemaV3Statements {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("admin schema v3 statement %d: %w", i+1, err)
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO admin_schema_versions(version,checksum,applied_at) VALUES(3,?,?)`, v3Want, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
	} else {
		return err
	}
	v4Sum := sha256.Sum256([]byte(strings.Join(adminSchemaV4Statements, ";\n")))
	v4Want := hex.EncodeToString(v4Sum[:])
	err = tx.QueryRowContext(ctx, `SELECT checksum FROM admin_schema_versions WHERE version=4`).Scan(&got)
	if err == nil {
		if got != v4Want {
			return errors.New("admin schema v4 checksum mismatch")
		}
	} else if errors.Is(err, sql.ErrNoRows) {
		for i, statement := range adminSchemaV4Statements {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("admin schema v4 statement %d: %w", i+1, err)
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO admin_schema_versions(version,checksum,applied_at) VALUES(4,?,?)`, v4Want, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
	} else {
		return err
	}
	v5Sum := sha256.Sum256([]byte(strings.Join(adminSchemaV5Statements, ";\n")))
	v5Want := hex.EncodeToString(v5Sum[:])
	err = tx.QueryRowContext(ctx, `SELECT checksum FROM admin_schema_versions WHERE version=5`).Scan(&got)
	if err == nil {
		if got != v5Want {
			return errors.New("admin schema v5 checksum mismatch")
		}
	} else if errors.Is(err, sql.ErrNoRows) {
		for i, statement := range adminSchemaV5Statements {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("admin schema v5 statement %d: %w", i+1, err)
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO admin_schema_versions(version,checksum,applied_at) VALUES(5,?,?)`, v5Want, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
	} else {
		return err
	}
	v6Sum := sha256.Sum256([]byte(strings.Join(adminSchemaV6Statements, ";\n")))
	v6Want := hex.EncodeToString(v6Sum[:])
	err = tx.QueryRowContext(ctx, `SELECT checksum FROM admin_schema_versions WHERE version=6`).Scan(&got)
	if err == nil {
		if got != v6Want {
			return errors.New("admin schema v6 checksum mismatch")
		}
	} else if errors.Is(err, sql.ErrNoRows) {
		for i, statement := range adminSchemaV6Statements {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("admin schema v6 statement %d: %w", i+1, err)
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO admin_schema_versions(version,checksum,applied_at) VALUES(6,?,?)`, v6Want, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
	} else {
		return err
	}
	v7Sum := sha256.Sum256([]byte(strings.Join(adminSchemaV7Statements, ";\n")))
	v7Want := hex.EncodeToString(v7Sum[:])
	err = tx.QueryRowContext(ctx, `SELECT checksum FROM admin_schema_versions WHERE version=7`).Scan(&got)
	if err == nil {
		if got != v7Want {
			return errors.New("admin schema v7 checksum mismatch")
		}
	} else if errors.Is(err, sql.ErrNoRows) {
		for i, statement := range adminSchemaV7Statements {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("admin schema v7 statement %d: %w", i+1, err)
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO admin_schema_versions(version,checksum,applied_at) VALUES(7,?,?)`, v7Want, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
	} else {
		return err
	}
	v8Sum := sha256.Sum256([]byte(strings.Join(adminSchemaV8Statements, ";\n")))
	v8Want := hex.EncodeToString(v8Sum[:])
	err = tx.QueryRowContext(ctx, `SELECT checksum FROM admin_schema_versions WHERE version=8`).Scan(&got)
	if err == nil {
		if got != v8Want {
			return errors.New("admin schema v8 checksum mismatch")
		}
	} else if errors.Is(err, sql.ErrNoRows) {
		for i, statement := range adminSchemaV8Statements {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("admin schema v8 statement %d: %w", i+1, err)
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO admin_schema_versions(version,checksum,applied_at) VALUES(8,?,?)`, v8Want, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
	} else {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return l.loadAdminClock(ctx)
}
