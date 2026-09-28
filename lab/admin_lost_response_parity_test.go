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

func TestAdminLostPaymentResponseKeepsOriginalFinancialEffect(t *testing.T) {
	ctx := context.Background()
	open := func() *Lab {
		t.Helper()
		dir := t.TempDir()
		l, err := Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), func() time.Time { return fixedNow })
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
	submitAdmin := func(action, target, key string, needsPreview bool) AdminCommand {
		t.Helper()
		payload := json.RawMessage(`{}`)
		previewID := ""
		if needsPreview {
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
		return command
	}
	compare := func(label, expectedStatus string, expectedAllocated int64) {
		t.Helper()
		d, err := domain.Snapshot(ctx, domainReceipt.SubscriptionID)
		if err != nil {
			t.Fatal(err)
		}
		a, err := admin.Snapshot(ctx, adminReceipt.SubscriptionID)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(d, a) || a.OperationStatus != expectedStatus || a.AllocatedMinor != expectedAllocated {
			t.Fatalf("%s snapshot differs: domain=%+v admin=%+v", label, d, a)
		}
		if dc, ac := captureCount(t, domain), captureCount(t, admin); dc != 1 || ac != 1 {
			t.Fatalf("%s provider captures: domain=%d admin=%d", label, dc, ac)
		}
	}

	if _, err := domain.DispatchNext(ctx, "lost_response"); !errors.Is(err, ErrPaymentUnknown) {
		t.Fatalf("domain lost response: %v", err)
	}
	submitControl(t, admin, "lost-response-fault", "C49", adminReceipt.OperationID, json.RawMessage(`{"operation_kind":"payment","mode":"lost_response"}`))
	dispatch := submitAdmin("C09", adminReceipt.OperationID, "lost-response-dispatch", true)
	dispatch, err := admin.AdminExecuteCommand(ctx, dispatch.ID)
	if err != nil || dispatch.Status != "waiting_verification" {
		t.Fatalf("admin lost response: %+v err=%v", dispatch, err)
	}
	compare("unknown payment", "unknown", 0)
	if found, err := domain.ReconcilePayment(ctx, domainReceipt.OperationID); err != nil || !found {
		t.Fatalf("domain lookup: found=%v err=%v", found, err)
	}
	reconcile := submitAdmin("C10", adminReceipt.OperationID, "lost-response-reconcile", false)
	reconcile, err = admin.AdminExecuteCommand(ctx, reconcile.ID)
	if err != nil || reconcile.Status != "succeeded" {
		t.Fatalf("admin lookup: %+v err=%v", reconcile, err)
	}
	dispatch, err = admin.AdminExecuteCommand(ctx, dispatch.ID)
	if err != nil || dispatch.Status != "succeeded" {
		t.Fatalf("original dispatch did not finish: %+v err=%v", dispatch, err)
	}
	compare("reconciled payment", "succeeded", 2000)
	if found, err := domain.ReconcilePayment(ctx, domainReceipt.OperationID); err != nil || !found {
		t.Fatalf("domain repeated lookup: found=%v err=%v", found, err)
	}
	if replay, err := admin.AdminExecuteCommand(ctx, reconcile.ID); err != nil || replay.ID != reconcile.ID || replay.Status != "succeeded" {
		t.Fatalf("admin repeated lookup: %+v err=%v", replay, err)
	}
	compare("replayed lookup", "succeeded", 2000)
	var adminReceipts int
	if err := admin.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id IN (?,?)`, dispatch.ID, reconcile.ID).Scan(&adminReceipts); err != nil || adminReceipts != 2 {
		t.Fatalf("admin dispatch and lookup receipts=%d err=%v", adminReceipts, err)
	}
}
