package lab

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"
)

func TestReconciliationRunRetainsOriginalFindingSnapshotAcrossLaterRuns(t *testing.T) {
	l, _, _ := openTestLab(t)
	ctx := context.Background()
	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	persist := func(at time.Time, findings ...Discrepancy) string {
		t.Helper()
		tx, err := l.db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		run, err := l.persistReconciliationTx(ctx, tx, ReconciliationRun{CutoffAt: at, LocalObservedAt: at, ProviderObservedAt: at, Findings: findings})
		if err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		return run.ID
	}
	first := finding("snapshot_probe", "same-object", "MANUAL_REVIEW", "expected-old", "actual-old", "evidence-old", 7)
	second := finding("snapshot_probe", "second-object", "MANUAL_REVIEW", "expected-two", "actual-two", "evidence-two", 2)
	runID := persist(base, first, second)
	updated := finding("snapshot_probe", "same-object", "MANUAL_REVIEW", "expected-new", "actual-new", "evidence-new", 8)
	newRunID := persist(base.Add(time.Second), updated)
	pageOne, err := l.AdminReconciliationRunPage(ctx, runID, "", 1)
	if err != nil || len(pageOne.Findings) != 1 || pageOne.NextID == "" || pageOne.Run.FindingCount != 2 {
		t.Fatalf("first page=%+v err=%v", pageOne, err)
	}
	pageTwo, err := l.AdminReconciliationRunPage(ctx, runID, pageOne.NextID, 1)
	if err != nil || len(pageTwo.Findings) != 1 || pageTwo.NextID != "" || pageTwo.Findings[0].DiscrepancyID == pageOne.Findings[0].DiscrepancyID {
		t.Fatalf("second page=%+v err=%v", pageTwo, err)
	}
	var original AdminReconciliationFinding
	for _, item := range append(pageOne.Findings, pageTwo.Findings...) {
		if item.DiscrepancyID == first.ID {
			original = item
		}
	}
	if original.Actual != "actual-old" || original.Expected != "expected-old" || original.Evidence != "evidence-old" || original.SourceRevision != 7 || original.SnapshotQuality != "recorded" {
		t.Fatalf("original run lost its finding snapshot: %+v", original)
	}
	newPage, err := l.AdminReconciliationRunPage(ctx, newRunID, "", 10)
	if err != nil || len(newPage.Findings) != 1 || newPage.Findings[0].Actual != "actual-new" {
		t.Fatalf("new run snapshot=%+v err=%v", newPage, err)
	}
	if _, err := l.db.ExecContext(ctx, `UPDATE reconciliation_finding_snapshots SET actual='tampered' WHERE run_id=?`, runID); err == nil {
		t.Fatal("reconciliation snapshot was mutable")
	}
	if _, err := l.AdminReconciliationRunPage(ctx, "missing-run", "", 10); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing run error=%v", err)
	}
}

func TestReconciliationRunLegacyFindingIsMarkedAsCurrentValueBackfill(t *testing.T) {
	l, _, _ := openTestLab(t)
	ctx := context.Background()
	at := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC).UnixNano()
	if _, err := l.db.ExecContext(ctx, `INSERT INTO reconciliation_runs(id,cutoff_at,local_observed_at,provider_observed_at,created_at) VALUES('legacy-run',?,?,?,?)`, at, at, at, at); err != nil {
		t.Fatal(err)
	}
	if _, err := l.db.ExecContext(ctx, `INSERT INTO discrepancies(id,kind,object_id,classification,expected,actual,evidence,source_revision,status,first_seen_at,last_seen_at) VALUES('disc:legacy','legacy','object','MANUAL_REVIEW','expected','latest-known','legacy evidence',3,'open',?,?)`, at, at); err != nil {
		t.Fatal(err)
	}
	if _, err := l.db.ExecContext(ctx, `INSERT INTO reconciliation_findings(run_id,discrepancy_id) VALUES('legacy-run','disc:legacy')`); err != nil {
		t.Fatal(err)
	}
	if err := migrateReconciliation(l.db); err != nil {
		t.Fatal(err)
	}
	page, err := l.AdminReconciliationRunPage(ctx, "legacy-run", "", 10)
	if err != nil || len(page.Findings) != 1 || page.Findings[0].SnapshotQuality != "backfilled_current" || page.Findings[0].Actual != "latest-known" {
		t.Fatalf("legacy provenance=%+v err=%v", page, err)
	}
}
