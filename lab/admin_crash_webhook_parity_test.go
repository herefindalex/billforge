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

func TestAdminCrashWebhookAndProjectionRecoveryMatchesDomain(t *testing.T) {
	ctx := context.Background()
	type instance struct {
		lab      *Lab
		commerce string
		provider string
	}
	open := func() *instance {
		t.Helper()
		dir := t.TempDir()
		item := &instance{commerce: filepath.Join(dir, "commerce.db"), provider: filepath.Join(dir, "provider.db")}
		var err error
		item.lab, err = Open(item.commerce, item.provider, func() time.Time { return fixedNow })
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = item.lab.Close() })
		return item
	}
	reopen := func(item *instance) {
		t.Helper()
		if err := item.lab.Close(); err != nil {
			t.Fatal(err)
		}
		var err error
		item.lab, err = Open(item.commerce, item.provider, func() time.Time { return fixedNow })
		if err != nil {
			t.Fatal(err)
		}
	}
	domain, admin := open(), open()
	if err := admin.lab.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	domainReceipt, adminReceipt := purchase(t, domain.lab), purchase(t, admin.lab)
	compare := func(label, operation, entitlement string, allocated int64) {
		t.Helper()
		d, err := domain.lab.Snapshot(ctx, domainReceipt.SubscriptionID)
		if err != nil {
			t.Fatal(err)
		}
		a, err := admin.lab.Snapshot(ctx, adminReceipt.SubscriptionID)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(d, a) || a.OperationStatus != operation || a.EntitlementStatus != entitlement || a.AllocatedMinor != allocated {
			t.Fatalf("%s differs: domain=%+v admin=%+v", label, d, a)
		}
		if dc, ac := captureCount(t, domain.lab), captureCount(t, admin.lab); dc != 1 || ac != 1 {
			t.Fatalf("%s provider captures: domain=%d admin=%d", label, dc, ac)
		}
	}
	if _, err := domain.lab.DispatchNext(ctx, "crash_after_provider"); !errors.Is(err, ErrInjectedCrash) {
		t.Fatalf("domain crash: %v", err)
	}
	submitControl(t, admin.lab, "crash-webhook-fault", "C49", adminReceipt.OperationID, json.RawMessage(`{"operation_kind":"payment","mode":"crash_after_provider"}`))
	payload := json.RawMessage(`{}`)
	preview, err := admin.lab.AdminCreatePreview(ctx, "local-admin", "C09", adminReceipt.OperationID, payload)
	if err != nil {
		t.Fatal(err)
	}
	command, _, err := admin.lab.AdminSubmitCommand(ctx, "local-admin", "crash-webhook-dispatch", "C09", adminReceipt.OperationID, payload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.lab.AdminExecuteCommand(ctx, command.ID); !errors.Is(err, ErrInjectedCrash) {
		t.Fatalf("admin crash: %v", err)
	}
	compare("after provider commit", "submitted", "pending", 0)

	reopen(domain)
	reopen(admin)
	for _, item := range []struct {
		lab     *Lab
		invoice string
	}{{domain.lab, domainReceipt.InvoiceID}, {admin.lab, adminReceipt.InvoiceID}} {
		event, found, err := item.lab.provider.Lookup(ctx, "capture:"+item.invoice)
		if err != nil || !found {
			t.Fatalf("persistent provider evidence: found=%v err=%v", found, err)
		}
		event.ID = "webhook:success"
		if err := item.lab.HandleWebhook(ctx, event); err != nil {
			t.Fatal(err)
		}
		if err := item.lab.HandleWebhook(ctx, event); err != nil {
			t.Fatal(err)
		}
		old := event
		old.ID, old.Status = "webhook:old-pending", "pending"
		if err := item.lab.HandleWebhook(ctx, old); err != nil {
			t.Fatal(err)
		}
	}
	compare("duplicate and stale webhooks", "succeeded", "pending", 2000)
	reopen(domain)
	reopen(admin)
	if err := domain.lab.RebuildEntitlements(ctx); err != nil {
		t.Fatal(err)
	}
	if err := domain.lab.RebuildEntitlements(ctx); err != nil {
		t.Fatal(err)
	}
	refreshPreview, err := admin.lab.AdminCreatePreview(ctx, "local-admin", "C45", "", payload)
	if err != nil {
		t.Fatal(err)
	}
	refresh, _, err := admin.lab.AdminSubmitCommand(ctx, "local-admin", "crash-webhook-refresh", "C45", "", payload, refreshPreview.ID)
	if err != nil {
		t.Fatal(err)
	}
	refresh, err = admin.lab.AdminExecuteCommand(ctx, refresh.ID)
	if err != nil || refresh.Status != "succeeded" {
		t.Fatalf("admin entitlement refresh: %+v err=%v", refresh, err)
	}
	compare("projection rebuilt", "succeeded", "active", 2000)
	command, err = admin.lab.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "succeeded" {
		t.Fatalf("original dispatch recovery: %+v err=%v", command, err)
	}
	compare("dispatch receipt recovered", "succeeded", "active", 2000)
	var receipts int
	if err := admin.lab.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil || receipts != 1 {
		t.Fatalf("original command receipts=%d err=%v", receipts, err)
	}
}
