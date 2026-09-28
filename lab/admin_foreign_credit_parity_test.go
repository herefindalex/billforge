package lab

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestAdminCreditRejectsForeignCustomerLikeDomain(t *testing.T) {
	ctx := context.Background()
	domain, _, _ := openTestLab(t)
	admin, _, _ := openTestLab(t)
	if err := admin.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	domainPaid := paidBasicSubscription(t, domain, "credit-owner")
	adminPaid := paidBasicSubscription(t, admin, "credit-owner")
	domainForeign := purchaseHundred(t, domain, "foreign-customer", "domain-foreign-checkout")
	adminForeign := purchaseHundred(t, admin, "foreign-customer", "admin-foreign-checkout")

	domainCorrection, err := domain.PostReduction(ctx, domainPaid.InvoiceID, 1000, "service credit", "domain-foreign-credit-source")
	if err != nil || len(domainCorrection.GrantIDs) != 1 {
		t.Fatalf("direct funded credit %+v %v", domainCorrection, err)
	}
	reductionPayload := json.RawMessage(`{"reduction_minor":"1000","reason":"service credit"}`)
	reductionPreview, err := admin.AdminCreatePreview(ctx, "local-admin", "C11", adminPaid.InvoiceID, reductionPayload)
	if err != nil {
		t.Fatal(err)
	}
	reduction, _, err := admin.AdminSubmitCommand(ctx, "local-admin", "admin-foreign-credit-source", "C11", adminPaid.InvoiceID, reductionPayload, reductionPreview.ID)
	if err != nil {
		t.Fatal(err)
	}
	reduction, err = admin.AdminExecuteCommand(ctx, reduction.ID)
	if err != nil || reduction.Status != "succeeded" {
		t.Fatalf("admin funded credit %+v %v", reduction, err)
	}
	var refs struct {
		GrantIDs []string `json:"grant_ids"`
	}
	if err := json.Unmarshal(reduction.ResultRefs, &refs); err != nil || len(refs.GrantIDs) != 1 {
		t.Fatalf("admin grant refs %+v %v", refs, err)
	}
	if _, err := domain.ApplyCredit(ctx, domainCorrection.GrantIDs[0], domainForeign.InvoiceID, 500, "domain-foreign-credit"); !errors.Is(err, ErrConflict) {
		t.Fatalf("direct foreign credit accepted: %v", err)
	}
	applyPayload, err := json.Marshal(AdminApplyCreditPayload{InvoiceID: adminForeign.InvoiceID, AmountMinor: "500"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.AdminCreatePreview(ctx, "local-admin", "C12", refs.GrantIDs[0], applyPayload); !errors.Is(err, ErrConflict) {
		t.Fatalf("admin foreign credit preview accepted: %v", err)
	}
	for _, path := range []struct {
		name      string
		lab       *Lab
		grantID   string
		invoiceID string
	}{
		{"direct", domain, domainCorrection.GrantIDs[0], domainForeign.InvoiceID},
		{"admin", admin, refs.GrantIDs[0], adminForeign.InvoiceID},
	} {
		credit, err := path.lab.CreditBalance(ctx, path.grantID)
		if err != nil || credit.AvailableMinor != 1000 || credit.AppliedMinor != 0 {
			t.Fatalf("%s credit balance %+v %v", path.name, credit, err)
		}
		invoice, err := path.lab.Balance(ctx, path.invoiceID)
		if err != nil || invoice.CreditAppliedMinor != 0 || invoice.OutstandingMinor != 10000 {
			t.Fatalf("%s foreign invoice %+v %v", path.name, invoice, err)
		}
		var applications int
		if err := path.lab.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM credit_applications WHERE grant_id=?`, path.grantID).Scan(&applications); err != nil || applications != 0 {
			t.Fatalf("%s foreign applications count=%d err=%v", path.name, applications, err)
		}
	}
	var commands int
	if err := admin.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_commands WHERE action_id='C12' AND target_id=?`, refs.GrantIDs[0]).Scan(&commands); err != nil || commands != 0 {
		t.Fatalf("foreign customer credit command count=%d err=%v", commands, err)
	}
}
