package lab

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
)

func TestAdminReductionClosingInitialReceivableActivatesLikeDomain(t *testing.T) {
	ctx := context.Background()
	domain, _, _ := openTestLab(t)
	admin, _, _ := openTestLab(t)
	if err := admin.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	domainPurchase := purchaseHundred(t, domain, "reduction-activation", "domain-activation-checkout")
	adminPurchase := purchaseHundred(t, admin, "reduction-activation", "admin-activation-checkout")
	for _, path := range []struct {
		lab       *Lab
		invoiceID string
	}{
		{domain, domainPurchase.InvoiceID},
		{admin, adminPurchase.InvoiceID},
	} {
		if _, err := path.lab.CreatePayment(ctx, path.invoiceID, 6000, "activation-pay-sixty"); err != nil {
			t.Fatal(err)
		}
		if _, err := path.lab.DispatchNext(ctx, ""); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []struct {
		lab            *Lab
		subscriptionID string
	}{
		{domain, domainPurchase.SubscriptionID},
		{admin, adminPurchase.SubscriptionID},
	} {
		if got := snapshot(t, path.lab, path.subscriptionID); got.SubscriptionStatus != "pending" || got.EntitlementStatus == "active" {
			t.Fatalf("partial payment activated service: %+v", got)
		}
	}

	if correction, err := domain.PostReduction(ctx, domainPurchase.InvoiceID, 4000, "service correction", "domain-close-receivable"); err != nil || len(correction.GrantIDs) != 0 {
		t.Fatalf("direct reduction %+v %v", correction, err)
	}
	payload := json.RawMessage(`{"reduction_minor":"4000","reason":"service correction"}`)
	preview, err := admin.AdminCreatePreview(ctx, "local-admin", "C11", adminPurchase.InvoiceID, payload)
	if err != nil {
		t.Fatal(err)
	}
	command, _, err := admin.AdminSubmitCommand(ctx, "local-admin", "admin-close-receivable", "C11", adminPurchase.InvoiceID, payload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	command, err = admin.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "succeeded" {
		t.Fatalf("admin reduction %+v %v", command, err)
	}
	for _, path := range []*Lab{domain, admin} {
		if err := path.RefreshEntitlements(ctx); err != nil {
			t.Fatal(err)
		}
	}
	domainBalance, err := domain.Balance(ctx, domainPurchase.InvoiceID)
	if err != nil {
		t.Fatal(err)
	}
	adminBalance, err := admin.Balance(ctx, adminPurchase.InvoiceID)
	if err != nil {
		t.Fatal(err)
	}
	domainBalance.InvoiceID = ""
	adminBalance.InvoiceID = ""
	if !reflect.DeepEqual(domainBalance, adminBalance) || adminBalance.ObligationMinor != 6000 || adminBalance.NetAppliedMinor != 6000 || adminBalance.OutstandingMinor != 0 {
		t.Fatalf("closed receivable differs: domain=%+v admin=%+v", domainBalance, adminBalance)
	}
	domainState := snapshot(t, domain, domainPurchase.SubscriptionID)
	adminState := snapshot(t, admin, adminPurchase.SubscriptionID)
	if !reflect.DeepEqual(domainState, adminState) || adminState.SubscriptionStatus != "active" || adminState.EntitlementStatus != "active" {
		t.Fatalf("activation differs: domain=%+v admin=%+v", domainState, adminState)
	}
	for _, path := range []struct {
		name      string
		lab       *Lab
		invoiceID string
	}{
		{"direct", domain, domainPurchase.InvoiceID},
		{"admin", admin, adminPurchase.InvoiceID},
	} {
		var grants int
		if err := path.lab.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM credit_grants WHERE source_invoice_id=?`, path.invoiceID).Scan(&grants); err != nil || grants != 0 {
			t.Fatalf("%s unfunded grant count=%d err=%v", path.name, grants, err)
		}
		if captures := captureCount(t, path.lab); captures != 1 {
			t.Fatalf("%s provider capture count=%d", path.name, captures)
		}
	}
}
