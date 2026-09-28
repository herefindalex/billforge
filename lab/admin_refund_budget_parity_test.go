package lab

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestAdminCreditApplicationAndUnknownRefundShareFundedBudget(t *testing.T) {
	ctx := context.Background()
	now := fixedNow
	open := func() *Lab {
		t.Helper()
		dir := t.TempDir()
		l, err := Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), func() time.Time { return now })
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = l.Close() })
		return l
	}
	domain, admin := open(), open()
	if err := admin.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	runAdmin := func(action, target, key string, payload json.RawMessage, previewRequired bool) AdminCommand {
		t.Helper()
		previewID := ""
		if previewRequired {
			preview, err := admin.AdminCreatePreview(ctx, "local-admin", action, target, payload)
			if err != nil {
				t.Fatalf("%s preview: %v", action, err)
			}
			previewID = preview.ID
		}
		command, _, err := admin.AdminSubmitCommand(ctx, "local-admin", key, action, target, payload, previewID)
		if err != nil {
			t.Fatalf("%s submit: %v", action, err)
		}
		command, err = admin.AdminExecuteCommand(ctx, command.ID)
		if err != nil || command.Status != "succeeded" {
			t.Fatalf("%s execute: %+v err=%v", action, command, err)
		}
		return command
	}
	refs := func(command AdminCommand) map[string]string {
		t.Helper()
		var result map[string]string
		if err := json.Unmarshal(command.ResultRefs, &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	domainPaid := paidBasicSubscription(t, domain, "refund-budget-parity")
	adminPaid := paidBasicSubscription(t, admin, "refund-budget-parity")
	domainCorrection, err := domain.PostReduction(ctx, domainPaid.InvoiceID, 1000, "service credit", "refund-budget-domain-reduction")
	if err != nil || len(domainCorrection.GrantIDs) != 1 {
		t.Fatalf("domain funded credit: %+v err=%v", domainCorrection, err)
	}
	adminReduction := runAdmin("C11", adminPaid.InvoiceID, "refund-budget-admin-reduction", json.RawMessage(`{"reduction_minor":"1000","reason":"service credit"}`), true)
	var reductionRefs struct {
		GrantIDs []string `json:"grant_ids"`
	}
	if err := json.Unmarshal(adminReduction.ResultRefs, &reductionRefs); err != nil || len(reductionRefs.GrantIDs) != 1 {
		t.Fatalf("admin funded credit refs: %+v err=%v", reductionRefs, err)
	}
	domainGrant, adminGrant := domainCorrection.GrantIDs[0], reductionRefs.GrantIDs[0]
	compareCredit := func(label string, applied, reserved, refunded, available int64) {
		t.Helper()
		d, err := domain.CreditBalance(ctx, domainGrant)
		if err != nil {
			t.Fatal(err)
		}
		a, err := admin.CreditBalance(ctx, adminGrant)
		if err != nil {
			t.Fatal(err)
		}
		d.GrantID, a.GrantID = "", ""
		if !reflect.DeepEqual(d, a) || a.GrantedMinor != 1000 || a.AppliedMinor != applied || a.ReservedMinor != reserved || a.RefundedMinor != refunded || a.AvailableMinor != available {
			t.Fatalf("%s credit budget differs: domain=%+v admin=%+v", label, d, a)
		}
	}
	compareCredit("funded grant", 0, 0, 0, 1000)
	now = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	domainRenewals, err := domain.RunRenewals(ctx)
	if err != nil || len(domainRenewals) != 1 || domainRenewals[0].AmountMinor != 2000 {
		t.Fatalf("domain renewal: %+v err=%v", domainRenewals, err)
	}
	runAdmin("C44", "", "refund-budget-renewal", json.RawMessage(`{}`), true)
	var adminRenewalInvoice string
	if err := admin.db.QueryRowContext(ctx, `SELECT invoice_id FROM billing_periods WHERE subscription_id=? AND period_index=1`, adminPaid.SubscriptionID).Scan(&adminRenewalInvoice); err != nil {
		t.Fatal(err)
	}
	if _, err := domain.ApplyCredit(ctx, domainGrant, domainRenewals[0].InvoiceID, 500, "refund-budget-domain-apply"); err != nil {
		t.Fatal(err)
	}
	applyPayload, err := json.Marshal(AdminApplyCreditPayload{InvoiceID: adminRenewalInvoice, AmountMinor: "500"})
	if err != nil {
		t.Fatal(err)
	}
	runAdmin("C12", adminGrant, "refund-budget-admin-apply", applyPayload, true)
	compareCredit("after application", 500, 0, 0, 500)
	for _, item := range []struct {
		l       *Lab
		invoice string
	}{{domain, domainRenewals[0].InvoiceID}, {admin, adminRenewalInvoice}} {
		balance, err := item.l.Balance(ctx, item.invoice)
		if err != nil || balance.CreditAppliedMinor != 500 || balance.OutstandingMinor != 1500 {
			t.Fatalf("applied credit balance: %+v err=%v", balance, err)
		}
		if _, err := item.l.DispatchNext(ctx, ""); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("stale full-price renewal capture stayed dispatchable: %v", err)
		}
	}
	domainRefund, err := domain.ReserveRefund(ctx, domainGrant, 500, "refund-budget-domain-reserve")
	if err != nil {
		t.Fatal(err)
	}
	adminRefund := refs(runAdmin("C15", adminGrant, "refund-budget-admin-reserve", json.RawMessage(`{"amount_minor":"500"}`), true))["refund_id"]
	if adminRefund == "" {
		t.Fatal("admin refund reservation returned no refund ID")
	}
	compareCredit("reserved refund", 500, 500, 0, 0)
	if _, err := domain.DispatchRefundNext(ctx, "lost_response"); !errors.Is(err, ErrPaymentUnknown) {
		t.Fatalf("domain refund lost response: %v", err)
	}
	submitControl(t, admin, "refund-budget-fault", "C49", adminRefund, json.RawMessage(`{"operation_kind":"refund","mode":"lost_response"}`))
	payload := json.RawMessage(`{}`)
	preview, err := admin.AdminCreatePreview(ctx, "local-admin", "C16", adminRefund, payload)
	if err != nil {
		t.Fatal(err)
	}
	dispatch, _, err := admin.AdminSubmitCommand(ctx, "local-admin", "refund-budget-admin-dispatch", "C16", adminRefund, payload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	dispatch, err = admin.AdminExecuteCommand(ctx, dispatch.ID)
	if err != nil || dispatch.Status != "waiting_verification" {
		t.Fatalf("admin refund lost response: %+v err=%v", dispatch, err)
	}
	compareCredit("unknown refund keeps reservation", 500, 500, 0, 0)
	if _, err := domain.ReserveRefund(ctx, domainGrant, 1, "refund-budget-domain-overcommit"); !errors.Is(err, ErrConflict) {
		t.Fatalf("domain admitted overcommitted refund: %v", err)
	}
	if found, err := domain.ReconcileRefund(ctx, domainRefund); err != nil || !found {
		t.Fatalf("domain refund lookup: found=%v err=%v", found, err)
	}
	runAdmin("C17", adminRefund, "refund-budget-admin-reconcile", payload, false)
	dispatch, err = admin.AdminExecuteCommand(ctx, dispatch.ID)
	if err != nil || dispatch.Status != "succeeded" {
		t.Fatalf("original admin refund command did not finish: %+v err=%v", dispatch, err)
	}
	compareCredit("settled refund", 500, 0, 500, 0)
	for _, item := range []struct {
		l       *Lab
		refund  string
		invoice string
	}{{domain, domainRefund, domainPaid.InvoiceID}, {admin, adminRefund, adminPaid.InvoiceID}} {
		var providerKey, source string
		if err := item.l.db.QueryRowContext(ctx, `SELECT provider_key FROM refund_operations WHERE id=?`, item.refund).Scan(&providerKey); err != nil {
			t.Fatal(err)
		}
		if err := item.l.provider.db.QueryRowContext(ctx, `SELECT source_capture_key FROM refunds WHERE provider_key=?`, providerKey).Scan(&source); err != nil || source != "capture:"+item.invoice {
			t.Fatalf("refund source capture=%q err=%v", source, err)
		}
		var refunds int
		if err := item.l.provider.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM refunds`).Scan(&refunds); err != nil || refunds != 1 {
			t.Fatalf("provider refund count=%d err=%v", refunds, err)
		}
	}
}
