package lab

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
)

func TestAdminUnpaidReductionAndReplacementCollectionMatchDomain(t *testing.T) {
	ctx := context.Background()
	domain, _, _ := openTestLab(t)
	admin, _, _ := openTestLab(t)
	if err := admin.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	domainPurchase := purchaseHundred(t, domain, "unpaid-reduction", "domain-unpaid-checkout")
	adminPurchase := purchaseHundred(t, admin, "unpaid-reduction", "admin-unpaid-checkout")
	domainCorrection, err := domain.PostReduction(ctx, domainPurchase.InvoiceID, 2000, "service correction", "domain-unpaid-reduction")
	if err != nil || len(domainCorrection.GrantIDs) != 0 {
		t.Fatalf("direct unpaid reduction %+v %v", domainCorrection, err)
	}
	reductionPayload := json.RawMessage(`{"reduction_minor":"2000","reason":"service correction"}`)
	reductionPreview, err := admin.AdminCreatePreview(ctx, "local-admin", "C11", adminPurchase.InvoiceID, reductionPayload)
	if err != nil {
		t.Fatal(err)
	}
	reduction, _, err := admin.AdminSubmitCommand(ctx, "local-admin", "admin-unpaid-reduction", "C11", adminPurchase.InvoiceID, reductionPayload, reductionPreview.ID)
	if err != nil {
		t.Fatal(err)
	}
	reduction, err = admin.AdminExecuteCommand(ctx, reduction.ID)
	if err != nil || reduction.Status != "succeeded" {
		t.Fatalf("admin unpaid reduction %+v %v", reduction, err)
	}
	compare := func(stage string, wantOutstanding, wantNetApplied int64) {
		t.Helper()
		left, err := domain.Balance(ctx, domainPurchase.InvoiceID)
		if err != nil {
			t.Fatal(err)
		}
		right, err := admin.Balance(ctx, adminPurchase.InvoiceID)
		if err != nil {
			t.Fatal(err)
		}
		left.InvoiceID = ""
		right.InvoiceID = ""
		if !reflect.DeepEqual(left, right) || right.OriginalMinor != 10000 || right.ReductionsMinor != 2000 || right.ObligationMinor != 8000 || right.OutstandingMinor != wantOutstanding || right.NetAppliedMinor != wantNetApplied {
			t.Fatalf("%s balances differ: direct=%+v admin=%+v", stage, left, right)
		}
	}
	compare("after reduction", 8000, 0)
	for _, path := range []struct {
		name        string
		lab         *Lab
		invoiceID   string
		operationID string
	}{
		{"direct", domain, domainPurchase.InvoiceID, domainPurchase.OperationID},
		{"admin", admin, adminPurchase.InvoiceID, adminPurchase.OperationID},
	} {
		var status, outboxStatus string
		if err := path.lab.db.QueryRowContext(ctx, `SELECT status FROM payment_operations WHERE id=?`, path.operationID).Scan(&status); err != nil || status != "cancelled" {
			t.Fatalf("%s original operation status=%s err=%v", path.name, status, err)
		}
		if err := path.lab.db.QueryRowContext(ctx, `SELECT status FROM outbox WHERE id=?`, "capture:"+path.operationID).Scan(&outboxStatus); err != nil || outboxStatus != "done" {
			t.Fatalf("%s original outbox status=%s err=%v", path.name, outboxStatus, err)
		}
		var grants int
		if err := path.lab.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM credit_grants WHERE source_invoice_id=?`, path.invoiceID).Scan(&grants); err != nil || grants != 0 {
			t.Fatalf("%s unfunded grants=%d err=%v", path.name, grants, err)
		}
		if captures := captureCount(t, path.lab); captures != 0 {
			t.Fatalf("%s provider captured cancelled operation: %d", path.name, captures)
		}
	}
	domainOperation, err := domain.CreatePayment(ctx, domainPurchase.InvoiceID, 8000, "domain-replacement-payment")
	if err != nil {
		t.Fatal(err)
	}
	paymentPayload := json.RawMessage(`{"amount_minor":"8000"}`)
	paymentPreview, err := admin.AdminCreatePreview(ctx, "local-admin", "C07", adminPurchase.InvoiceID, paymentPayload)
	if err != nil {
		t.Fatal(err)
	}
	payment, _, err := admin.AdminSubmitCommand(ctx, "local-admin", "admin-replacement-payment", "C07", adminPurchase.InvoiceID, paymentPayload, paymentPreview.ID)
	if err != nil {
		t.Fatal(err)
	}
	payment, err = admin.AdminExecuteCommand(ctx, payment.ID)
	if err != nil || payment.Status != "succeeded" {
		t.Fatalf("admin replacement payment %+v %v", payment, err)
	}
	var paymentRefs struct {
		OperationID string `json:"operation_id"`
	}
	if err := json.Unmarshal(payment.ResultRefs, &paymentRefs); err != nil || paymentRefs.OperationID == "" || paymentRefs.OperationID == adminPurchase.OperationID {
		t.Fatalf("admin replacement operation %+v %v", paymentRefs, err)
	}
	if _, err := domain.DispatchCapture(ctx, domainOperation, ""); err != nil {
		t.Fatal(err)
	}
	dispatchPreview, err := admin.AdminCreatePreview(ctx, "local-admin", "C09", paymentRefs.OperationID, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	dispatch, _, err := admin.AdminSubmitCommand(ctx, "local-admin", "admin-replacement-dispatch", "C09", paymentRefs.OperationID, json.RawMessage(`{}`), dispatchPreview.ID)
	if err != nil {
		t.Fatal(err)
	}
	dispatch, err = admin.AdminExecuteCommand(ctx, dispatch.ID)
	if err != nil || dispatch.Status != "succeeded" {
		t.Fatalf("admin replacement dispatch %+v %v", dispatch, err)
	}
	compare("after replacement capture", 0, 8000)
	for _, path := range []*Lab{domain, admin} {
		if err := path.RefreshEntitlements(ctx); err != nil {
			t.Fatal(err)
		}
	}
	left := snapshot(t, domain, domainPurchase.SubscriptionID)
	right := snapshot(t, admin, adminPurchase.SubscriptionID)
	if !reflect.DeepEqual(left, right) || right.SubscriptionStatus != "active" || right.EntitlementStatus != "active" {
		t.Fatalf("replacement activation differs: direct=%+v admin=%+v", left, right)
	}
	if directCaptures, adminCaptures := captureCount(t, domain), captureCount(t, admin); directCaptures != 1 || adminCaptures != 1 {
		t.Fatalf("replacement captures direct=%d admin=%d", directCaptures, adminCaptures)
	}
	for _, path := range []struct {
		name           string
		lab            *Lab
		oldOperationID string
		newOperationID string
	}{
		{"direct", domain, domainPurchase.OperationID, domainOperation},
		{"admin", admin, adminPurchase.OperationID, paymentRefs.OperationID},
	} {
		var oldKey, newKey string
		if err := path.lab.db.QueryRowContext(ctx, `SELECT provider_key FROM payment_operations WHERE id=?`, path.oldOperationID).Scan(&oldKey); err != nil {
			t.Fatal(err)
		}
		if err := path.lab.db.QueryRowContext(ctx, `SELECT provider_key FROM payment_operations WHERE id=?`, path.newOperationID).Scan(&newKey); err != nil {
			t.Fatal(err)
		}
		var oldCaptures, newAmount int64
		if err := path.lab.provider.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM captures WHERE provider_key=?`, oldKey).Scan(&oldCaptures); err != nil || oldCaptures != 0 {
			t.Fatalf("%s cancelled provider key captures=%d err=%v", path.name, oldCaptures, err)
		}
		if err := path.lab.provider.db.QueryRowContext(ctx, `SELECT amount_minor FROM captures WHERE provider_key=?`, newKey).Scan(&newAmount); err != nil || newAmount != 8000 {
			t.Fatalf("%s replacement provider amount=%d err=%v", path.name, newAmount, err)
		}
	}
}
