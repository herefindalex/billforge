package lab

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"
)

type Discrepancy struct {
	ID             string
	Kind           string
	ObjectID       string
	Classification string
	Expected       string
	Actual         string
	Evidence       string
	SourceRevision int64
	Status         string
}

type ReconciliationRun struct {
	ID                 string
	CutoffAt           time.Time
	LocalObservedAt    time.Time
	ProviderObservedAt time.Time
	Findings           []Discrepancy
}

type RepairOperation struct {
	ID                   string
	DiscrepancyID        string
	Action               string
	Expected             string
	Actual               string
	PreconditionRevision int64
	Status               string
	Verification         string
}

func migrateReconciliation(db *sql.DB) error {
	if err := ensureCatalogColumn(db, "entitlements", "source_revision", `ALTER TABLE entitlements ADD COLUMN source_revision INTEGER NOT NULL DEFAULT 0`); err != nil {
		return err
	}
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS reconciliation_runs (
id TEXT PRIMARY KEY,
cutoff_at INTEGER NOT NULL,
local_observed_at INTEGER NOT NULL,
provider_observed_at INTEGER NOT NULL,
created_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS discrepancies (
id TEXT PRIMARY KEY,
kind TEXT NOT NULL,
object_id TEXT NOT NULL,
classification TEXT NOT NULL CHECK(classification IN ('SAFE_AUTO_REPAIR','RETRY_REQUIRED','EXTERNAL_LOOKUP_REQUIRED','MANUAL_REVIEW','UNSAFE_TO_REPAIR')),
expected TEXT NOT NULL,
actual TEXT NOT NULL,
evidence TEXT NOT NULL,
source_revision INTEGER NOT NULL DEFAULT 0,
status TEXT NOT NULL CHECK(status IN ('open','investigating','resolved')),
first_seen_at INTEGER NOT NULL,
last_seen_at INTEGER NOT NULL,
resolution TEXT NOT NULL DEFAULT '');
CREATE TABLE IF NOT EXISTS reconciliation_findings (
run_id TEXT NOT NULL REFERENCES reconciliation_runs(id),
discrepancy_id TEXT NOT NULL REFERENCES discrepancies(id),
PRIMARY KEY(run_id,discrepancy_id));
CREATE TABLE IF NOT EXISTS reconciliation_finding_snapshots (
run_id TEXT NOT NULL REFERENCES reconciliation_runs(id),
discrepancy_id TEXT NOT NULL REFERENCES discrepancies(id),
kind TEXT NOT NULL, object_id TEXT NOT NULL, classification TEXT NOT NULL,
expected TEXT NOT NULL, actual TEXT NOT NULL, evidence TEXT NOT NULL,
source_revision INTEGER NOT NULL,
snapshot_quality TEXT NOT NULL CHECK(snapshot_quality IN ('recorded','backfilled_current')),
PRIMARY KEY(run_id,discrepancy_id));
CREATE TRIGGER IF NOT EXISTS reconciliation_finding_snapshots_no_update BEFORE UPDATE ON reconciliation_finding_snapshots BEGIN SELECT RAISE(ABORT,'reconciliation finding snapshot immutable'); END;
CREATE TRIGGER IF NOT EXISTS reconciliation_finding_snapshots_no_delete BEFORE DELETE ON reconciliation_finding_snapshots BEGIN SELECT RAISE(ABORT,'reconciliation finding snapshot immutable'); END;
CREATE TABLE IF NOT EXISTS repair_operations (
id TEXT PRIMARY KEY,
request_key TEXT NOT NULL UNIQUE,
discrepancy_id TEXT NOT NULL REFERENCES discrepancies(id),
action TEXT NOT NULL,
expected TEXT NOT NULL,
actual TEXT NOT NULL,
precondition_revision INTEGER NOT NULL,
status TEXT NOT NULL CHECK(status IN ('planned','executed','waiting','verified','blocked')),
verification TEXT NOT NULL DEFAULT '',
created_at INTEGER NOT NULL,
executed_at INTEGER);
CREATE TABLE IF NOT EXISTS manual_decisions (
id TEXT PRIMARY KEY,
discrepancy_id TEXT NOT NULL REFERENCES discrepancies(id),
reviewer TEXT NOT NULL,
decision TEXT NOT NULL,
reason TEXT NOT NULL DEFAULT '',
created_at INTEGER NOT NULL);`)
	if err != nil {
		return err
	}
	if err := ensureCatalogColumn(db, "manual_decisions", "reason", `ALTER TABLE manual_decisions ADD COLUMN reason TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	_, err = db.Exec(`INSERT OR IGNORE INTO reconciliation_finding_snapshots(run_id,discrepancy_id,kind,object_id,classification,expected,actual,evidence,source_revision,snapshot_quality)
		SELECT f.run_id,f.discrepancy_id,d.kind,d.object_id,d.classification,d.expected,d.actual,d.evidence,d.source_revision,'backfilled_current'
		FROM reconciliation_findings f JOIN discrepancies d ON d.id=f.discrepancy_id`)
	return err
}

func finding(kind, objectID, class, expected, actual, evidence string, revision int64) Discrepancy {
	return Discrepancy{ID: "disc:" + hash(kind, objectID), Kind: kind, ObjectID: objectID, Classification: class, Expected: expected, Actual: actual, Evidence: evidence, SourceRevision: revision, Status: "open"}
}

func loadDiscrepancy(ctx context.Context, q rowQuerier, id string) (Discrepancy, error) {
	var d Discrepancy
	err := q.QueryRowContext(ctx, `SELECT id,kind,object_id,classification,expected,actual,evidence,source_revision,status FROM discrepancies WHERE id=?`, id).Scan(&d.ID, &d.Kind, &d.ObjectID, &d.Classification, &d.Expected, &d.Actual, &d.Evidence, &d.SourceRevision, &d.Status)
	return d, err
}

func (l *Lab) Discrepancies(ctx context.Context) ([]Discrepancy, error) {
	rows, err := l.db.QueryContext(ctx, `SELECT id FROM discrepancies ORDER BY last_seen_at DESC,id`)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	var out []Discrepancy
	for _, id := range ids {
		d, err := loadDiscrepancy(ctx, l.db, id)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

func (l *Lab) reconciliationSnapshot(ctx context.Context, cutoff time.Time) (ReconciliationRun, error) {
	if cutoff.IsZero() || cutoff.After(l.now().UTC()) {
		return ReconciliationRun{}, ErrConflict
	}
	localAt := l.now().UTC()
	state, err := l.State(ctx)
	if err != nil {
		return ReconciliationRun{}, err
	}
	providerAt := l.now().UTC()
	provider, err := l.ProviderState(ctx)
	if err != nil {
		return ReconciliationRun{}, err
	}
	var findings []Discrepancy
	payments := map[string]PaymentState{}
	for _, p := range state.Payments {
		payments[p.ProviderKey] = p
	}
	providerCaptures := map[string]ProviderCaptureState{}
	for _, p := range provider.Captures {
		providerCaptures[p.ProviderKey] = p
		local, ok := payments[p.ProviderKey]
		if !ok {
			findings = append(findings, finding("unknown_provider_capture", p.ProviderKey, "MANUAL_REVIEW", "known payment operation", fmt.Sprintf("%d %s %s", p.AmountMinor, p.Currency, p.Status), "provider capture list", 0))
			continue
		}
		if local.AmountMinor != p.AmountMinor || local.Currency != p.Currency {
			findings = append(findings, finding("provider_amount_mismatch", local.ID, "MANUAL_REVIEW", fmt.Sprintf("%d %s", local.AmountMinor, local.Currency), fmt.Sprintf("%d %s", p.AmountMinor, p.Currency), "payment operation and provider capture", 0))
		}
		if p.Status == "succeeded" && local.Status != "succeeded" {
			findings = append(findings, finding("provider_success_unobserved", local.ID, "EXTERNAL_LOOKUP_REQUIRED", "succeeded allocation", local.Status, "provider capture succeeded; lookup original key", 0))
		}
	}
	for _, p := range state.Payments {
		if p.Status == "unknown" || p.Status == "submitted" {
			if _, ok := providerCaptures[p.ProviderKey]; !ok {
				findings = append(findings, finding("payment_unknown", p.ID, "EXTERNAL_LOOKUP_REQUIRED", "terminal provider observation", p.Status, "original provider key lookup", 0))
			}
		}
	}
	for _, o := range state.PendingOutbox {
		class := "RETRY_REQUIRED"
		if o.Kind == "entitlement" {
			class = "SAFE_AUTO_REPAIR"
		}
		if o.Kind == "capture" {
			for _, p := range state.Payments {
				if p.ID == o.ObjectID && (p.Status == "unknown" || p.Status == "submitted") {
					class = "EXTERNAL_LOOKUP_REQUIRED"
					break
				}
			}
		}
		findings = append(findings, finding("pending_outbox", o.ID, class, "done", "pending", "local outbox", 0))
	}
	for _, s := range state.Subscriptions {
		if s.Status == "active" && (s.EntitlementStatus == "" || s.EntitlementSourceRevision != s.Revision) {
			findings = append(findings, finding("entitlement_projection", s.ID, "SAFE_AUTO_REPAIR", fmt.Sprintf("revision=%d", s.Revision), fmt.Sprintf("status=%s revision=%d", s.EntitlementStatus, s.EntitlementSourceRevision), "subscription and entitlement projection", s.Revision))
		}
		if s.Status == "active" {
			var active []PricingAssignmentState
			for _, a := range state.Assignments {
				if a.SubscriptionID == s.ID && a.EffectiveEnd == nil {
					active = append(active, a)
				}
			}
			if len(active) != 1 || active[0].PriceVersionID != s.PriceVersionID {
				findings = append(findings, finding("assignment_mismatch", s.ID, "UNSAFE_TO_REPAIR", s.PriceVersionID, fmt.Sprintf("open_assignments=%d", len(active)), "subscription and assignment history", s.Revision))
			}
		}
	}
	for _, i := range state.Invoices {
		var lineSum int64
		if err := l.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount_minor),0) FROM invoice_lines WHERE invoice_id=?`, i.ID).Scan(&lineSum); err != nil {
			return ReconciliationRun{}, err
		}
		if lineSum != i.Balance.OriginalMinor {
			findings = append(findings, finding("invoice_line_mismatch", i.ID, "UNSAFE_TO_REPAIR", fmt.Sprint(i.Balance.OriginalMinor), fmt.Sprint(lineSum), "finalized invoice and immutable lines", 0))
		}
	}
	rows, err := l.db.QueryContext(ctx, `SELECT subscription_id,period_index,reason FROM renewal_holds WHERE reason='contract_next_price_missing'`)
	if err != nil {
		return ReconciliationRun{}, err
	}
	for rows.Next() {
		var sub, reason string
		var index int
		if err := rows.Scan(&sub, &index, &reason); err != nil {
			rows.Close()
			return ReconciliationRun{}, err
		}
		findings = append(findings, finding("contract_next_price_missing", sub, "MANUAL_REVIEW", "explicit post-contract price", fmt.Sprintf("hold period=%d", index), "renewal hold and contract version", 0))
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return ReconciliationRun{}, err
	}
	sort.Slice(findings, func(i, j int) bool { return findings[i].ID < findings[j].ID })
	return ReconciliationRun{CutoffAt: cutoff.UTC(), LocalObservedAt: localAt, ProviderObservedAt: providerAt, Findings: findings}, nil
}

func (l *Lab) persistReconciliationTx(ctx context.Context, tx *sql.Tx, snapshot ReconciliationRun) (ReconciliationRun, error) {
	findings := snapshot.Findings
	cutoff := snapshot.CutoffAt
	localAt := snapshot.LocalObservedAt
	providerAt := snapshot.ProviderObservedAt
	runID, err := newID("recon_")
	if err != nil {
		return ReconciliationRun{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO reconciliation_runs(id,cutoff_at,local_observed_at,provider_observed_at,created_at) VALUES(?,?,?,?,?)`, runID, cutoff.UTC().UnixNano(), localAt.UnixNano(), providerAt.UnixNano(), l.now().UTC().UnixNano()); err != nil {
		return ReconciliationRun{}, err
	}
	seen := map[string]bool{}
	for _, d := range findings {
		seen[d.ID] = true
		if _, err := tx.ExecContext(ctx, `INSERT INTO discrepancies(id,kind,object_id,classification,expected,actual,evidence,source_revision,status,first_seen_at,last_seen_at) VALUES(?,?,?,?,?,?,?,?,'open',?,?) ON CONFLICT(id) DO UPDATE SET classification=excluded.classification,expected=excluded.expected,actual=excluded.actual,evidence=excluded.evidence,source_revision=excluded.source_revision,status='open',last_seen_at=excluded.last_seen_at`, d.ID, d.Kind, d.ObjectID, d.Classification, d.Expected, d.Actual, d.Evidence, d.SourceRevision, localAt.UnixNano(), localAt.UnixNano()); err != nil {
			return ReconciliationRun{}, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO reconciliation_findings(run_id,discrepancy_id) VALUES(?,?)`, runID, d.ID); err != nil {
			return ReconciliationRun{}, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO reconciliation_finding_snapshots(run_id,discrepancy_id,kind,object_id,classification,expected,actual,evidence,source_revision,snapshot_quality) VALUES(?,?,?,?,?,?,?,?,?,'recorded')`, runID, d.ID, d.Kind, d.ObjectID, d.Classification, d.Expected, d.Actual, d.Evidence, d.SourceRevision); err != nil {
			return ReconciliationRun{}, err
		}
	}
	// Only a fresh comparison can resolve an older finding. Evidence remains.
	openRows, err := tx.QueryContext(ctx, `SELECT id FROM discrepancies WHERE status!='resolved'`)
	if err != nil {
		return ReconciliationRun{}, err
	}
	var old []string
	for openRows.Next() {
		var id string
		if err := openRows.Scan(&id); err != nil {
			openRows.Close()
			return ReconciliationRun{}, err
		}
		if !seen[id] {
			old = append(old, id)
		}
	}
	err = openRows.Err()
	openRows.Close()
	if err != nil {
		return ReconciliationRun{}, err
	}
	for _, id := range old {
		if _, err := tx.ExecContext(ctx, `UPDATE discrepancies SET status='resolved',resolution='absent in reconciliation '||? WHERE id=?`, runID, id); err != nil {
			return ReconciliationRun{}, err
		}
	}
	snapshot.ID = runID
	return snapshot, nil
}

func (l *Lab) RunReconciliation(ctx context.Context, cutoff time.Time) (ReconciliationRun, error) {
	snapshot, err := l.reconciliationSnapshot(ctx, cutoff)
	if err != nil {
		return ReconciliationRun{}, err
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return ReconciliationRun{}, err
	}
	defer tx.Rollback()
	run, err := l.persistReconciliationTx(ctx, tx, snapshot)
	if err != nil {
		return ReconciliationRun{}, err
	}
	if err := tx.Commit(); err != nil {
		return ReconciliationRun{}, err
	}
	return run, nil
}

func (l *Lab) recordManualDecisionTx(ctx context.Context, tx *sql.Tx, discrepancyID, reviewer, decision, reason string) (string, error) {
	if discrepancyID == "" || reviewer == "" || decision == "" {
		return "", ErrConflict
	}
	d, err := loadDiscrepancy(ctx, tx, discrepancyID)
	if err != nil {
		return "", err
	}
	if d.Classification != "MANUAL_REVIEW" && d.Classification != "UNSAFE_TO_REPAIR" {
		return "", ErrConflict
	}
	id, err := newID("decision_")
	if err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO manual_decisions(id,discrepancy_id,reviewer,decision,reason,created_at) VALUES(?,?,?,?,?,?)`, id, discrepancyID, reviewer, decision, reason, l.now().UTC().UnixNano()); err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE discrepancies SET status='investigating' WHERE id=?`, discrepancyID); err != nil {
		return "", err
	}
	return id, nil
}

func (l *Lab) RecordManualDecision(ctx context.Context, discrepancyID, reviewer, decision string) (string, error) {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	id, err := l.recordManualDecisionTx(ctx, tx, discrepancyID, reviewer, decision, "")
	if err != nil {
		return "", err
	}
	return id, tx.Commit()
}
