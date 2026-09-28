package lab

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

func TestAdminDiscrepancyDetailKeepsEvidenceAndRepairHistory(t *testing.T) {
	l, _, _ := openTestLab(t)
	ctx := context.Background()
	purchase(t, l)
	run := reconcileNow(t, l)
	finding := findingFor(t, run, "pending_outbox", "")
	detail, err := l.AdminDiscrepancyDetail(ctx, finding.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Discrepancy.ID != finding.ID || detail.Discrepancy.Expected != finding.Expected || detail.Discrepancy.Actual != finding.Actual || detail.Discrepancy.Evidence != finding.Evidence || detail.Discrepancy.SourceRevision != finding.SourceRevision {
		t.Fatalf("discrepancy evidence changed: %+v", detail)
	}
	if len(detail.Runs) != 1 || detail.Runs[0].ID != run.ID || len(detail.Repairs) != 0 || len(detail.Decisions) != 0 {
		t.Fatalf("unexpected initial provenance: %+v", detail)
	}
	repair, err := l.RepairDiscrepancy(ctx, finding.ID, "detail-repair")
	if err != nil {
		t.Fatal(err)
	}
	detail, err = l.AdminDiscrepancyDetail(ctx, finding.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Repairs) != 1 || detail.Repairs[0].ID != repair.ID || detail.Repairs[0].Status != repair.Status || detail.Repairs[0].Action != repair.Action {
		t.Fatalf("repair provenance missing: %+v", detail.Repairs)
	}
	if _, err := l.AdminDiscrepancyDetail(ctx, "missing-discrepancy"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing discrepancy error = %v", err)
	}
}
