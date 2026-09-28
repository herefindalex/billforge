package lab

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestAdminUnknownRenewalKeepsVerificationGraceMatchesDomain(t *testing.T) {
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
	domainReceipt, adminReceipt := purchase(t, domain), purchase(t, admin)
	for _, l := range []*Lab{domain, admin} {
		if _, err := l.DispatchNext(ctx, ""); err != nil {
			t.Fatal(err)
		}
		if err := l.RefreshEntitlements(ctx); err != nil {
			t.Fatal(err)
		}
	}
	runAdmin := func(action, target, key string, previewRequired bool) AdminCommand {
		t.Helper()
		payload := json.RawMessage(`{}`)
		previewID := ""
		if previewRequired {
			preview, err := admin.AdminCreatePreview(ctx, "local-admin", action, target, payload)
			if err != nil {
				t.Fatal(err)
			}
			previewID = preview.ID
		}
		command, _, err := admin.AdminSubmitCommand(ctx, "local-admin", key, action, target, payload, previewID)
		if err != nil {
			t.Fatal(err)
		}
		command, err = admin.AdminExecuteCommand(ctx, command.ID)
		if err != nil || command.Status != "succeeded" {
			t.Fatalf("%s execute: %+v err=%v", action, command, err)
		}
		return command
	}
	compare := func(label, entitlement, operation string, allocation int64) {
		t.Helper()
		d, err := domain.Snapshot(ctx, domainReceipt.SubscriptionID)
		if err != nil {
			t.Fatal(err)
		}
		a, err := admin.Snapshot(ctx, adminReceipt.SubscriptionID)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(d, a) || a.EntitlementStatus != entitlement || a.OperationStatus != operation || a.AllocatedMinor != allocation {
			t.Fatalf("%s snapshot differs: domain=%+v admin=%+v", label, d, a)
		}
		if df, af := loadRenewalFacts(t, domain, domainReceipt.SubscriptionID), loadRenewalFacts(t, admin, adminReceipt.SubscriptionID); !reflect.DeepEqual(df, af) {
			t.Fatalf("%s renewal facts differ: domain=%+v admin=%+v", label, df, af)
		}
	}

	now = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	domainRenewals, err := domain.RunRenewals(ctx)
	if err != nil || len(domainRenewals) != 1 {
		t.Fatalf("domain renewal: %+v err=%v", domainRenewals, err)
	}
	runAdmin("C44", "", "unknown-renewal-create", true)
	var adminOperation string
	if err := admin.db.QueryRowContext(ctx, `SELECT o.id FROM billing_periods p JOIN payment_operations o ON o.invoice_id=p.invoice_id WHERE p.subscription_id=? AND p.period_index=1`, adminReceipt.SubscriptionID).Scan(&adminOperation); err != nil {
		t.Fatal(err)
	}
	if _, err := domain.DispatchNext(ctx, "lost_response"); !errors.Is(err, ErrPaymentUnknown) {
		t.Fatalf("domain lost response: %v", err)
	}
	submitControl(t, admin, "unknown-renewal-fault", "C49", adminOperation, json.RawMessage(`{"operation_kind":"payment","mode":"lost_response"}`))
	payload := json.RawMessage(`{}`)
	preview, err := admin.AdminCreatePreview(ctx, "local-admin", "C09", adminOperation, payload)
	if err != nil {
		t.Fatal(err)
	}
	dispatch, _, err := admin.AdminSubmitCommand(ctx, "local-admin", "unknown-renewal-dispatch", "C09", adminOperation, payload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	dispatch, err = admin.AdminExecuteCommand(ctx, dispatch.ID)
	if err != nil || dispatch.Status != "waiting_verification" {
		t.Fatalf("admin unknown renewal dispatch: %+v err=%v", dispatch, err)
	}
	if err := domain.RefreshEntitlements(ctx); err != nil {
		t.Fatal(err)
	}
	runAdmin("C45", "", "unknown-renewal-grace", true)
	compare("unknown renewal", "grace", "unknown", 0)

	now = now.Add(8 * 24 * time.Hour)
	if err := domain.RefreshEntitlements(ctx); err != nil {
		t.Fatal(err)
	}
	runAdmin("C45", "", "unknown-renewal-after-deadline", true)
	compare("verification grace after deadline", "grace", "unknown", 0)
	if found, err := domain.ReconcilePayment(ctx, domainRenewals[0].OperationID); err != nil || !found {
		t.Fatalf("domain lookup: found=%v err=%v", found, err)
	}
	runAdmin("C10", adminOperation, "unknown-renewal-reconcile", false)
	dispatch, err = admin.AdminExecuteCommand(ctx, dispatch.ID)
	if err != nil || dispatch.Status != "succeeded" {
		t.Fatalf("original dispatch recovery: %+v err=%v", dispatch, err)
	}
	if err := domain.RefreshEntitlements(ctx); err != nil {
		t.Fatal(err)
	}
	runAdmin("C45", "", "unknown-renewal-recovered", true)
	compare("confirmed renewal", "active", "succeeded", 2000)
	if dc, ac := captureCount(t, domain), captureCount(t, admin); dc != 2 || ac != 2 {
		t.Fatalf("provider captures: domain=%d admin=%d", dc, ac)
	}
}
