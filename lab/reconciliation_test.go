package lab

import (
	"context"
	"strings"
	"testing"
)

func findingFor(t *testing.T, run ReconciliationRun, kind, objectID string) Discrepancy {
	t.Helper()
	for _, d := range run.Findings {
		if d.Kind == kind && (objectID == "" || d.ObjectID == objectID) {
			return d
		}
	}
	t.Fatalf("missing finding kind=%s object=%s: %+v", kind, objectID, run.Findings)
	return Discrepancy{}
}

func reconcileNow(t *testing.T, l *Lab) ReconciliationRun {
	t.Helper()
	r, err := l.RunReconciliation(context.Background(), fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestS12RebuildEntitlementAndReplayRepair(t *testing.T) {
	l, _, _ := openTestLab(t)
	ctx := context.Background()
	r := purchase(t, l)
	if _, err := l.DispatchNext(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if err := l.RebuildEntitlements(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := l.db.ExecContext(ctx, `DELETE FROM entitlements WHERE subscription_id=?`, r.SubscriptionID); err != nil {
		t.Fatal(err)
	}
	d := findingFor(t, reconcileNow(t, l), "entitlement_projection", r.SubscriptionID)
	if d.Classification != "SAFE_AUTO_REPAIR" || d.SourceRevision == 0 {
		t.Fatalf("unsafe classification or missing revision: %+v", d)
	}
	op, err := l.RepairDiscrepancy(ctx, d.ID, "repair:entitlement")
	if err != nil || op.Status != "verified" {
		t.Fatalf("repair: %+v %v", op, err)
	}
	if got := snapshot(t, l, r.SubscriptionID).EntitlementStatus; got != "active" {
		t.Fatalf("entitlement = %s", got)
	}
	replay, err := l.RepairDiscrepancy(ctx, d.ID, "repair:entitlement")
	if err != nil || replay.ID != op.ID || replay.Status != "verified" {
		t.Fatalf("replay: %+v %v", replay, err)
	}
	if _, err := l.RepairDiscrepancy(ctx, "other", "repair:entitlement"); err != ErrConflict {
		t.Fatalf("same request key accepted for another finding: %v", err)
	}
}

func TestS12RepairBlocksChangedRevision(t *testing.T) {
	l, _, _ := openTestLab(t)
	ctx := context.Background()
	r := purchase(t, l)
	if _, err := l.DispatchNext(ctx, ""); err != nil {
		t.Fatal(err)
	}
	d := findingFor(t, reconcileNow(t, l), "entitlement_projection", r.SubscriptionID)
	if _, err := l.db.ExecContext(ctx, `UPDATE subscriptions SET revision=revision+1 WHERE id=?`, r.SubscriptionID); err != nil {
		t.Fatal(err)
	}
	op, err := l.RepairDiscrepancy(ctx, d.ID, "repair:stale")
	if err != nil || op.Status != "blocked" || !strings.Contains(op.Verification, "changed") {
		t.Fatalf("stale repair: %+v %v", op, err)
	}
}

func TestS12RetryOriginalCaptureAndUnknownLookup(t *testing.T) {
	l, _, _ := openTestLab(t)
	ctx := context.Background()
	r := purchase(t, l)
	d := findingFor(t, reconcileNow(t, l), "pending_outbox", "")
	if d.Classification != "RETRY_REQUIRED" {
		t.Fatalf("unexpected classification: %+v", d)
	}
	op, err := l.RepairDiscrepancy(ctx, d.ID, "repair:capture")
	if err != nil || op.Status != "verified" || captureCount(t, l) != 1 {
		t.Fatalf("capture repair: %+v %v", op, err)
	}
	if snapshot(t, l, r.SubscriptionID).OperationStatus != "succeeded" {
		t.Fatal("original payment did not settle")
	}

	l2, _, _ := openTestLab(t)
	r2 := purchase(t, l2)
	if _, err := l2.DispatchNext(ctx, "lost_response"); err != ErrPaymentUnknown {
		t.Fatalf("expected unknown response, got %v", err)
	}
	d2 := findingFor(t, reconcileNow(t, l2), "provider_success_unobserved", r2.OperationID)
	if d2.Classification != "EXTERNAL_LOOKUP_REQUIRED" {
		t.Fatalf("unexpected unknown classification: %+v", d2)
	}
	op2, err := l2.RepairDiscrepancy(ctx, d2.ID, "repair:lookup")
	if err != nil || op2.Status != "verified" || captureCount(t, l2) != 1 {
		t.Fatalf("lookup repair: %+v %v", op2, err)
	}
}

func TestS12ManualReviewForUnknownAndMismatchedProviderCapture(t *testing.T) {
	l, _, _ := openTestLab(t)
	ctx := context.Background()
	r := purchase(t, l)
	if _, err := l.provider.db.ExecContext(ctx, `INSERT INTO captures(provider_key,amount_minor,currency,status) VALUES('capture:foreign',100,'USD','succeeded')`); err != nil {
		t.Fatal(err)
	}
	if _, err := l.provider.db.ExecContext(ctx, `INSERT INTO captures(provider_key,amount_minor,currency,status) VALUES(?,2100,'USD','succeeded')`, "capture:"+r.InvoiceID); err != nil {
		t.Fatal(err)
	}
	run := reconcileNow(t, l)
	foreign := findingFor(t, run, "unknown_provider_capture", "capture:foreign")
	mismatch := findingFor(t, run, "provider_amount_mismatch", r.OperationID)
	if foreign.Classification != "MANUAL_REVIEW" || mismatch.Classification != "MANUAL_REVIEW" {
		t.Fatalf("unsafe provider records: %+v %+v", foreign, mismatch)
	}
	op, err := l.RepairDiscrepancy(ctx, mismatch.ID, "repair:mismatch")
	if err != nil || op.Status != "blocked" || captureCount(t, l) != 2 {
		t.Fatalf("unsafe repair: %+v %v", op, err)
	}
	if _, err := l.RecordManualDecision(ctx, mismatch.ID, "reviewer-1", "investigate provider amount"); err != nil {
		t.Fatal(err)
	}
}
