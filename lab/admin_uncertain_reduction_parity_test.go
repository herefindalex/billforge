package lab

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestAdminUncertainCaptureBlocksReductionUntilVerifiedLikeDomain(t *testing.T) {
	ctx := context.Background()
	domain, _, _ := openTestLab(t)
	admin, _, _ := openTestLab(t)
	if err := admin.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	domainPurchase := purchaseHundred(t, domain, "uncertain-parity", "uncertain-domain-checkout")
	adminPurchase := purchaseHundred(t, admin, "uncertain-parity", "uncertain-admin-checkout")
	if _, err := domain.DispatchCapture(ctx, domainPurchase.OperationID, "lost_response"); !errors.Is(err, ErrPaymentUnknown) {
		t.Fatalf("direct capture uncertainty: %v", err)
	}
	submitControl(t, admin, "uncertain-admin-fault", "C49", adminPurchase.OperationID, json.RawMessage(`{"operation_kind":"payment","mode":"lost_response"}`))
	dispatchPreview, err := admin.AdminCreatePreview(ctx, "local-admin", "C09", adminPurchase.OperationID, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	dispatch, _, err := admin.AdminSubmitCommand(ctx, "local-admin", "uncertain-admin-dispatch", "C09", adminPurchase.OperationID, json.RawMessage(`{}`), dispatchPreview.ID)
	if err != nil {
		t.Fatal(err)
	}
	dispatch, err = admin.AdminExecuteCommand(ctx, dispatch.ID)
	if err != nil || dispatch.Status != "waiting_verification" {
		t.Fatalf("admin capture uncertainty %+v %v", dispatch, err)
	}
	if _, err := domain.PostReduction(ctx, domainPurchase.InvoiceID, 2000, "service correction", "uncertain-domain-reduction"); !errors.Is(err, ErrConflict) {
		t.Fatalf("direct reduction before verification: %v", err)
	}
	reductionPayload := json.RawMessage(`{"reduction_minor":"2000","reason":"service correction"}`)
	if _, err := admin.AdminCreatePreview(ctx, "local-admin", "C11", adminPurchase.InvoiceID, reductionPayload); !errors.Is(err, ErrConflict) {
		t.Fatalf("admin reduction preview before verification: %v", err)
	}
	compareBalances := func(stage string, obligation, captured int64) {
		t.Helper()
		left, err := domain.Balance(ctx, domainPurchase.InvoiceID)
		if err != nil {
			t.Fatal(err)
		}
		right, err := admin.Balance(ctx, adminPurchase.InvoiceID)
		if err != nil {
			t.Fatal(err)
		}
		left.InvoiceID, right.InvoiceID = "", ""
		if !reflect.DeepEqual(left, right) || left.ObligationMinor != obligation || left.GrossCapturedMinor != captured {
			t.Fatalf("%s balances direct=%+v admin=%+v", stage, left, right)
		}
	}
	compareBalances("unknown capture", 10000, 0)
	var corrections, grants, receipts int
	if err := admin.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM corrections WHERE invoice_id=?`, adminPurchase.InvoiceID).Scan(&corrections); err != nil || corrections != 0 {
		t.Fatalf("correction before verification %d %v", corrections, err)
	}
	if err := admin.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM credit_grants WHERE source_invoice_id=?`, adminPurchase.InvoiceID).Scan(&grants); err != nil || grants != 0 {
		t.Fatalf("funded grant before verification %d %v", grants, err)
	}
	if err := admin.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_commands WHERE action_id='C11' AND target_id=?`, adminPurchase.InvoiceID).Scan(&corrections); err != nil || corrections != 0 {
		t.Fatalf("admin reduction admitted before verification %d %v", corrections, err)
	}
	if found, err := domain.ReconcilePayment(ctx, domainPurchase.OperationID); err != nil || !found {
		t.Fatalf("direct payment verification found=%v err=%v", found, err)
	}
	verify, _, err := admin.AdminSubmitCommand(ctx, "local-admin", "uncertain-admin-verify", "C10", adminPurchase.OperationID, json.RawMessage(`{}`), "")
	if err != nil {
		t.Fatal(err)
	}
	verify, err = admin.AdminExecuteCommand(ctx, verify.ID)
	if err != nil || verify.Status != "succeeded" {
		t.Fatalf("admin payment verification %+v %v", verify, err)
	}
	dispatch, err = admin.AdminExecuteCommand(ctx, dispatch.ID)
	if err != nil || dispatch.Status != "succeeded" {
		t.Fatalf("original dispatch recovery %+v %v", dispatch, err)
	}
	compareBalances("verified capture", 10000, 10000)
	domainCorrection, err := domain.PostReduction(ctx, domainPurchase.InvoiceID, 2000, "service correction", "uncertain-domain-reduction")
	if err != nil || len(domainCorrection.GrantIDs) != 1 {
		t.Fatalf("direct reduction after verification %+v %v", domainCorrection, err)
	}
	reductionPreview, err := admin.AdminCreatePreview(ctx, "local-admin", "C11", adminPurchase.InvoiceID, reductionPayload)
	if err != nil {
		t.Fatal(err)
	}
	reduction, _, err := admin.AdminSubmitCommand(ctx, "local-admin", "uncertain-admin-reduction", "C11", adminPurchase.InvoiceID, reductionPayload, reductionPreview.ID)
	if err != nil {
		t.Fatal(err)
	}
	reduction, err = admin.AdminExecuteCommand(ctx, reduction.ID)
	if err != nil || reduction.Status != "succeeded" {
		t.Fatalf("admin reduction after verification %+v %v", reduction, err)
	}
	compareBalances("reduction", 8000, 10000)
	for _, path := range []struct {
		name string
		lab  *Lab
		id   string
	}{
		{name: "direct", lab: domain, id: domainPurchase.InvoiceID},
		{name: "admin", lab: admin, id: adminPurchase.InvoiceID},
	} {
		var amount, count int
		if err := path.lab.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount_minor),0),COUNT(*) FROM credit_grants WHERE source_invoice_id=?`, path.id).Scan(&amount, &count); err != nil || amount != 2000 || count != 1 {
			t.Fatalf("%s funded credit amount=%d count=%d err=%v", path.name, amount, count, err)
		}
		if err := path.lab.provider.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM captures`).Scan(&count); err != nil || count != 1 {
			t.Fatalf("%s provider captures %d %v", path.name, count, err)
		}
	}
	if err := admin.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id IN (?,?,?)`, dispatch.ID, verify.ID, reduction.ID).Scan(&receipts); err != nil || receipts != 3 {
		t.Fatalf("admin financial command receipts %d %v", receipts, err)
	}
}
