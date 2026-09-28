package lab

import (
	"context"
	"encoding/json"
	"reflect"
	"strconv"
	"testing"
	"time"
)

func runParityReconciliationCommand(t *testing.T, l *Lab, action, target, key string, payload json.RawMessage, previewRequired bool) AdminCommand {
	t.Helper()
	ctx := context.Background()
	previewID := ""
	if previewRequired {
		preview, err := l.AdminCreatePreview(ctx, "local-admin", action, target, payload)
		if err != nil {
			t.Fatalf("%s preview: %v", action, err)
		}
		previewID = preview.ID
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", key, action, target, payload, previewID)
	if err != nil {
		t.Fatalf("%s submit: %v", action, err)
	}
	command, err = l.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "succeeded" {
		t.Fatalf("%s execute: %+v err=%v", action, command, err)
	}
	return command
}

func adminParityFinding(t *testing.T, l *Lab, kind, objectID string) Discrepancy {
	t.Helper()
	findings, err := l.Discrepancies(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range findings {
		if d.Kind == kind && (objectID == "" || d.ObjectID == objectID) {
			return d
		}
	}
	t.Fatalf("missing admin finding kind=%s object=%s: %+v", kind, objectID, findings)
	return Discrepancy{}
}

func TestAdminSafeEntitlementRepairMatchesDomain(t *testing.T) {
	ctx := context.Background()
	domain, _, _ := openTestLab(t)
	admin, _, _ := openTestLab(t)
	if err := admin.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	domainReceipt, adminReceipt := purchase(t, domain), purchase(t, admin)
	for _, l := range []*Lab{domain, admin} {
		if _, err := l.DispatchNext(ctx, ""); err != nil {
			t.Fatal(err)
		}
		if err := l.RebuildEntitlements(ctx); err != nil {
			t.Fatal(err)
		}
	}
	for _, item := range []struct {
		l   *Lab
		sub string
	}{{domain, domainReceipt.SubscriptionID}, {admin, adminReceipt.SubscriptionID}} {
		if _, err := item.l.db.ExecContext(ctx, `DELETE FROM entitlements WHERE subscription_id=?`, item.sub); err != nil {
			t.Fatal(err)
		}
	}
	domainFinding := findingFor(t, reconcileNow(t, domain), "entitlement_projection", domainReceipt.SubscriptionID)
	reconcilePayload, err := json.Marshal(AdminReconciliationPayload{AsOf: fixedNow.Format(time.RFC3339Nano)})
	if err != nil {
		t.Fatal(err)
	}
	runParityReconciliationCommand(t, admin, "C33", "", "parity-entitlement-reconciliation", reconcilePayload, false)
	adminFinding := adminParityFinding(t, admin, "entitlement_projection", adminReceipt.SubscriptionID)
	if domainFinding.Classification != "SAFE_AUTO_REPAIR" || adminFinding.Classification != "SAFE_AUTO_REPAIR" || domainFinding.SourceRevision != adminFinding.SourceRevision || domainFinding.SourceRevision == 0 {
		t.Fatalf("repair classification differs: domain=%+v admin=%+v", domainFinding, adminFinding)
	}
	domainRepair, err := domain.RepairDiscrepancy(ctx, domainFinding.ID, "parity-domain-entitlement-repair")
	if err != nil || domainRepair.Status != "verified" {
		t.Fatalf("domain repair: %+v err=%v", domainRepair, err)
	}
	repairPayload, err := json.Marshal(adminRepairPayload{SourceRevision: strconv.FormatInt(adminFinding.SourceRevision, 10), Evidence: adminFinding.Evidence})
	if err != nil {
		t.Fatal(err)
	}
	command := runParityReconciliationCommand(t, admin, "C34", adminFinding.ID, "parity-admin-entitlement-repair", repairPayload, true)
	for _, item := range []struct {
		l       *Lab
		receipt Receipt
	}{{domain, domainReceipt}, {admin, adminReceipt}} {
		state, err := item.l.Snapshot(ctx, item.receipt.SubscriptionID)
		if err != nil || state.EntitlementStatus != "active" || state.AllocatedMinor != 2000 || state.AllocationCount != 1 {
			t.Fatalf("repaired state: %+v err=%v", state, err)
		}
		balance, err := item.l.Balance(ctx, item.receipt.InvoiceID)
		if err != nil || balance.NetAppliedMinor != 2000 || balance.OutstandingMinor != 0 {
			t.Fatalf("repaired balance: %+v err=%v", balance, err)
		}
		if count := captureCount(t, item.l); count != 1 {
			t.Fatalf("repair changed provider captures: %d", count)
		}
	}
	if replay, err := domain.RepairDiscrepancy(ctx, domainFinding.ID, "parity-domain-entitlement-repair"); err != nil || replay.ID != domainRepair.ID || replay.Status != "verified" {
		t.Fatalf("domain repair replay: %+v err=%v", replay, err)
	}
	if replay, err := admin.AdminExecuteCommand(ctx, command.ID); err != nil || replay.ID != command.ID || replay.Status != "succeeded" {
		t.Fatalf("admin repair replay: %+v err=%v", replay, err)
	}
	var repairs, receipts int
	if err := admin.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM repair_operations WHERE request_key=?`, "admin:"+command.ID).Scan(&repairs); err != nil {
		t.Fatal(err)
	}
	if err := admin.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if repairs != 1 || receipts != 1 {
		t.Fatalf("admin repair replay duplicated facts: operations=%d receipts=%d", repairs, receipts)
	}
	domainState, err := domain.Snapshot(ctx, domainReceipt.SubscriptionID)
	if err != nil {
		t.Fatal(err)
	}
	adminState, err := admin.Snapshot(ctx, adminReceipt.SubscriptionID)
	if err != nil || !reflect.DeepEqual(domainState, adminState) {
		t.Fatalf("repaired projection differs: domain=%+v admin=%+v err=%v", domainState, adminState, err)
	}
}

func TestAdminUnsafeProviderCaptureStaysManualReviewMatchesDomain(t *testing.T) {
	ctx := context.Background()
	domain, _, _ := openTestLab(t)
	admin, _, _ := openTestLab(t)
	if err := admin.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	domainReceipt, adminReceipt := purchase(t, domain), purchase(t, admin)
	for _, item := range []struct {
		l       *Lab
		receipt Receipt
	}{{domain, domainReceipt}, {admin, adminReceipt}} {
		if _, err := item.l.provider.db.ExecContext(ctx, `INSERT INTO captures(provider_key,amount_minor,currency,status) VALUES('capture:foreign',100,'USD','succeeded')`); err != nil {
			t.Fatal(err)
		}
		if _, err := item.l.provider.db.ExecContext(ctx, `INSERT INTO captures(provider_key,amount_minor,currency,status) VALUES(?,2100,'USD','succeeded')`, "capture:"+item.receipt.InvoiceID); err != nil {
			t.Fatal(err)
		}
	}
	domainRun := reconcileNow(t, domain)
	payload, err := json.Marshal(AdminReconciliationPayload{AsOf: fixedNow.Format(time.RFC3339Nano)})
	if err != nil {
		t.Fatal(err)
	}
	runParityReconciliationCommand(t, admin, "C33", "", "parity-provider-mismatch-run", payload, false)
	domainForeign := findingFor(t, domainRun, "unknown_provider_capture", "capture:foreign")
	domainMismatch := findingFor(t, domainRun, "provider_amount_mismatch", domainReceipt.OperationID)
	adminForeign := adminParityFinding(t, admin, "unknown_provider_capture", "capture:foreign")
	adminMismatch := adminParityFinding(t, admin, "provider_amount_mismatch", adminReceipt.OperationID)
	for _, finding := range []Discrepancy{domainForeign, domainMismatch, adminForeign, adminMismatch} {
		if finding.Classification != "MANUAL_REVIEW" {
			t.Fatalf("unsafe provider fact classified for automatic repair: %+v", finding)
		}
	}
	domainRepair, err := domain.RepairDiscrepancy(ctx, domainMismatch.ID, "parity-domain-mismatch-repair")
	if err != nil || domainRepair.Status != "blocked" {
		t.Fatalf("domain mismatch repair: %+v err=%v", domainRepair, err)
	}
	repairPayload, err := json.Marshal(adminRepairPayload{SourceRevision: strconv.FormatInt(adminMismatch.SourceRevision, 10), Evidence: adminMismatch.Evidence})
	if err != nil {
		t.Fatal(err)
	}
	repairCommand := runParityReconciliationCommand(t, admin, "C34", adminMismatch.ID, "parity-admin-mismatch-repair", repairPayload, true)
	var repairRefs map[string]string
	if err := json.Unmarshal(repairCommand.ResultRefs, &repairRefs); err != nil || repairRefs["repair_status"] != "blocked" {
		t.Fatalf("admin mismatch repair refs=%v err=%v", repairRefs, err)
	}
	for _, item := range []struct {
		l       *Lab
		receipt Receipt
	}{{domain, domainReceipt}, {admin, adminReceipt}} {
		state, err := item.l.Snapshot(ctx, item.receipt.SubscriptionID)
		if err != nil || state.OperationStatus != "created" || state.AllocationCount != 0 || state.AllocatedMinor != 0 {
			t.Fatalf("unsafe repair changed local payment: %+v err=%v", state, err)
		}
		balance, err := item.l.Balance(ctx, item.receipt.InvoiceID)
		if err != nil || balance.OutstandingMinor != 2000 || balance.NetAppliedMinor != 0 {
			t.Fatalf("unsafe repair changed invoice balance: %+v err=%v", balance, err)
		}
		if count := captureCount(t, item.l); count != 2 {
			t.Fatalf("unsafe repair changed provider captures: %d", count)
		}
	}
	if _, err := domain.RecordManualDecision(ctx, domainMismatch.ID, "local-admin", "investigate_provider"); err != nil {
		t.Fatal(err)
	}
	decisionPayload, err := json.Marshal(adminManualDecisionPayload{Decision: "investigate_provider", Reason: "provider amount mismatch"})
	if err != nil {
		t.Fatal(err)
	}
	runParityReconciliationCommand(t, admin, "C35", adminMismatch.ID, "parity-admin-mismatch-decision", decisionPayload, true)
	for _, item := range []struct {
		l         *Lab
		findingID string
	}{{domain, domainMismatch.ID}, {admin, adminMismatch.ID}} {
		var status, reviewer, decision string
		if err := item.l.db.QueryRowContext(ctx, `SELECT status FROM discrepancies WHERE id=?`, item.findingID).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if err := item.l.db.QueryRowContext(ctx, `SELECT reviewer,decision FROM manual_decisions WHERE discrepancy_id=?`, item.findingID).Scan(&reviewer, &decision); err != nil {
			t.Fatal(err)
		}
		if status != "investigating" || reviewer != "local-admin" || decision != "investigate_provider" {
			t.Fatalf("manual review state=%s reviewer=%s decision=%s", status, reviewer, decision)
		}
	}
	var reason string
	if err := admin.db.QueryRowContext(ctx, `SELECT reason FROM manual_decisions WHERE discrepancy_id=?`, adminMismatch.ID).Scan(&reason); err != nil || reason != "provider amount mismatch" {
		t.Fatalf("admin manual review reason=%q err=%v", reason, err)
	}
}

func TestAdminOriginalCaptureRepairMatchesDomain(t *testing.T) {
	for _, tc := range []struct {
		name           string
		kind           string
		classification string
		lostResponse   bool
	}{
		{name: "pending outbox", kind: "pending_outbox", classification: "RETRY_REQUIRED"},
		{name: "unknown provider response", kind: "provider_success_unobserved", classification: "EXTERNAL_LOOKUP_REQUIRED", lostResponse: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			domain, _, _ := openTestLab(t)
			admin, _, _ := openTestLab(t)
			if err := admin.InitAdmin(ctx); err != nil {
				t.Fatal(err)
			}
			domainReceipt, adminReceipt := purchase(t, domain), purchase(t, admin)
			if tc.lostResponse {
				for _, l := range []*Lab{domain, admin} {
					if _, err := l.DispatchNext(ctx, "lost_response"); err != ErrPaymentUnknown {
						t.Fatalf("lost response: %v", err)
					}
				}
			}
			domainObject, adminObject := "", ""
			if tc.lostResponse {
				domainObject, adminObject = domainReceipt.OperationID, adminReceipt.OperationID
			}
			domainFinding := findingFor(t, reconcileNow(t, domain), tc.kind, domainObject)
			reconcilePayload, err := json.Marshal(AdminReconciliationPayload{AsOf: fixedNow.Format(time.RFC3339Nano)})
			if err != nil {
				t.Fatal(err)
			}
			runParityReconciliationCommand(t, admin, "C33", "", "parity-reconcile-"+tc.kind, reconcilePayload, false)
			adminFinding := adminParityFinding(t, admin, tc.kind, adminObject)
			if domainFinding.Classification != tc.classification || adminFinding.Classification != tc.classification {
				t.Fatalf("repair classification differs: domain=%+v admin=%+v", domainFinding, adminFinding)
			}
			domainRepair, err := domain.RepairDiscrepancy(ctx, domainFinding.ID, "parity-domain-repair-"+tc.kind)
			if err != nil || domainRepair.Status != "verified" {
				t.Fatalf("domain original capture repair: %+v err=%v", domainRepair, err)
			}
			repairPayload, err := json.Marshal(adminRepairPayload{SourceRevision: strconv.FormatInt(adminFinding.SourceRevision, 10), Evidence: adminFinding.Evidence})
			if err != nil {
				t.Fatal(err)
			}
			command := runParityReconciliationCommand(t, admin, "C34", adminFinding.ID, "parity-admin-repair-"+tc.kind, repairPayload, true)
			var refs map[string]string
			if err := json.Unmarshal(command.ResultRefs, &refs); err != nil || refs["repair_status"] != "verified" {
				t.Fatalf("admin original capture repair refs=%v err=%v", refs, err)
			}
			for _, item := range []struct {
				l       *Lab
				receipt Receipt
			}{{domain, domainReceipt}, {admin, adminReceipt}} {
				state, err := item.l.Snapshot(ctx, item.receipt.SubscriptionID)
				if err != nil || state.OperationStatus != "succeeded" || state.AllocationCount != 1 || state.AllocatedMinor != 2000 {
					t.Fatalf("original operation did not settle exactly once: %+v err=%v", state, err)
				}
				balance, err := item.l.Balance(ctx, item.receipt.InvoiceID)
				if err != nil || balance.NetAppliedMinor != 2000 || balance.OutstandingMinor != 0 {
					t.Fatalf("original invoice balance: %+v err=%v", balance, err)
				}
				if count := captureCount(t, item.l); count != 1 {
					t.Fatalf("repair recaptured original payment: %d", count)
				}
			}
			if replay, err := domain.RepairDiscrepancy(ctx, domainFinding.ID, "parity-domain-repair-"+tc.kind); err != nil || replay.ID != domainRepair.ID || replay.Status != "verified" {
				t.Fatalf("domain repair replay: %+v err=%v", replay, err)
			}
			if replay, err := admin.AdminExecuteCommand(ctx, command.ID); err != nil || replay.ID != command.ID || replay.Status != "succeeded" {
				t.Fatalf("admin repair replay: %+v err=%v", replay, err)
			}
			if dc, ac := captureCount(t, domain), captureCount(t, admin); dc != 1 || ac != 1 {
				t.Fatalf("repair replay recaptured: domain=%d admin=%d", dc, ac)
			}
		})
	}
}

func TestAdminRepairRejectsChangedSourceRevisionMatchesDomain(t *testing.T) {
	ctx := context.Background()
	domain, _, _ := openTestLab(t)
	admin, _, _ := openTestLab(t)
	if err := admin.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	domainReceipt, adminReceipt := purchase(t, domain), purchase(t, admin)
	for _, l := range []*Lab{domain, admin} {
		if _, err := l.DispatchNext(ctx, ""); err != nil {
			t.Fatal(err)
		}
	}
	domainFinding := findingFor(t, reconcileNow(t, domain), "entitlement_projection", domainReceipt.SubscriptionID)
	reconcilePayload, err := json.Marshal(AdminReconciliationPayload{AsOf: fixedNow.Format(time.RFC3339Nano)})
	if err != nil {
		t.Fatal(err)
	}
	runParityReconciliationCommand(t, admin, "C33", "", "parity-stale-reconciliation", reconcilePayload, false)
	adminFinding := adminParityFinding(t, admin, "entitlement_projection", adminReceipt.SubscriptionID)
	for _, item := range []struct {
		l   *Lab
		sub string
	}{{domain, domainReceipt.SubscriptionID}, {admin, adminReceipt.SubscriptionID}} {
		if _, err := item.l.db.ExecContext(ctx, `UPDATE subscriptions SET revision=revision+1 WHERE id=?`, item.sub); err != nil {
			t.Fatal(err)
		}
	}
	domainRepair, err := domain.RepairDiscrepancy(ctx, domainFinding.ID, "parity-domain-stale-repair")
	if err != nil || domainRepair.Status != "blocked" {
		t.Fatalf("domain stale repair: %+v err=%v", domainRepair, err)
	}
	repairPayload, err := json.Marshal(adminRepairPayload{SourceRevision: strconv.FormatInt(adminFinding.SourceRevision, 10), Evidence: adminFinding.Evidence})
	if err != nil {
		t.Fatal(err)
	}
	command := runParityReconciliationCommand(t, admin, "C34", adminFinding.ID, "parity-admin-stale-repair", repairPayload, true)
	var refs map[string]string
	if err := json.Unmarshal(command.ResultRefs, &refs); err != nil || refs["repair_status"] != "blocked" {
		t.Fatalf("admin stale repair refs=%v err=%v", refs, err)
	}
	for _, item := range []struct {
		l       *Lab
		receipt Receipt
	}{{domain, domainReceipt}, {admin, adminReceipt}} {
		var entitlements int
		if err := item.l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM entitlements WHERE subscription_id=?`, item.receipt.SubscriptionID).Scan(&entitlements); err != nil || entitlements != 0 {
			t.Fatalf("stale repair wrote entitlement: count=%d err=%v", entitlements, err)
		}
		balance, err := item.l.Balance(ctx, item.receipt.InvoiceID)
		if err != nil || balance.NetAppliedMinor != 2000 || balance.OutstandingMinor != 0 {
			t.Fatalf("stale repair changed money: %+v err=%v", balance, err)
		}
		if count := captureCount(t, item.l); count != 1 {
			t.Fatalf("stale repair captured payment: %d", count)
		}
	}
}
