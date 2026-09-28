package lab

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestAdminFailedRenewalGraceAndRetryMatchesDomain(t *testing.T) {
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
	runAdmin := func(action, target, key string) AdminCommand {
		t.Helper()
		payload := json.RawMessage(`{}`)
		preview, err := admin.AdminCreatePreview(ctx, "local-admin", action, target, payload)
		if err != nil {
			t.Fatalf("%s preview: %v", action, err)
		}
		command, _, err := admin.AdminSubmitCommand(ctx, "local-admin", key, action, target, payload, preview.ID)
		if err != nil {
			t.Fatalf("%s submit: %v", action, err)
		}
		command, err = admin.AdminExecuteCommand(ctx, command.ID)
		if err != nil || command.Status != "succeeded" {
			t.Fatalf("%s execute: %+v err=%v", action, command, err)
		}
		return command
	}
	compareSnapshot := func(label, domainSub, adminSub, entitlement, operation string, allocated int64) {
		t.Helper()
		d, err := domain.Snapshot(ctx, domainSub)
		if err != nil {
			t.Fatal(err)
		}
		a, err := admin.Snapshot(ctx, adminSub)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(d, a) || a.EntitlementStatus != entitlement || a.OperationStatus != operation || a.AllocatedMinor != allocated {
			t.Fatalf("%s snapshot differs: domain=%+v admin=%+v", label, d, a)
		}
	}
	compareOperations := func(domainInvoice, adminInvoice string) {
		t.Helper()
		read := func(l *Lab, invoice string) []string {
			t.Helper()
			rows, err := l.db.QueryContext(ctx, `SELECT status || ':' || amount_minor FROM payment_operations WHERE invoice_id=? ORDER BY status,amount_minor`, invoice)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			var result []string
			for rows.Next() {
				var fact string
				if err := rows.Scan(&fact); err != nil {
					t.Fatal(err)
				}
				result = append(result, fact)
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			return result
		}
		d, a := read(domain, domainInvoice), read(admin, adminInvoice)
		if !reflect.DeepEqual(d, a) {
			t.Fatalf("payment obligations differ: domain=%v admin=%v", d, a)
		}
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
	compareSnapshot("initial purchase", domainReceipt.SubscriptionID, adminReceipt.SubscriptionID, "active", "succeeded", 2000)

	now = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	domainRenewals, err := domain.RunRenewals(ctx)
	if err != nil || len(domainRenewals) != 1 {
		t.Fatalf("domain renewal: %+v err=%v", domainRenewals, err)
	}
	runAdmin("C44", "", "failed-renewal-create")
	domainRenewal := domainRenewals[0]
	var adminOperation, adminInvoice string
	if err := admin.db.QueryRowContext(ctx, `SELECT o.id,i.id FROM billing_periods p JOIN invoices i ON i.id=p.invoice_id JOIN payment_operations o ON o.invoice_id=i.id WHERE p.subscription_id=? AND p.period_index=1`, adminReceipt.SubscriptionID).Scan(&adminOperation, &adminInvoice); err != nil {
		t.Fatal(err)
	}
	if d, a := loadRenewalFacts(t, domain, domainReceipt.SubscriptionID), loadRenewalFacts(t, admin, adminReceipt.SubscriptionID); !reflect.DeepEqual(d, a) || a.InvoiceMinor != 2000 {
		t.Fatalf("renewal facts differ: domain=%+v admin=%+v", d, a)
	}
	for _, item := range []struct {
		l  *Lab
		op string
	}{{domain, domainRenewal.OperationID}, {admin, adminOperation}} {
		if err := item.l.SetFakePaymentDecision(ctx, item.op, "definitively_failed"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := domain.DispatchNext(ctx, ""); err != nil {
		t.Fatal(err)
	}
	runAdmin("C09", adminOperation, "failed-renewal-dispatch")
	if err := domain.RefreshEntitlements(ctx); err != nil {
		t.Fatal(err)
	}
	runAdmin("C45", "", "failed-renewal-grace")
	compareSnapshot("failed renewal grace", domainReceipt.SubscriptionID, adminReceipt.SubscriptionID, "grace", "definitively_failed", 0)
	compareOperations(domainRenewal.InvoiceID, adminInvoice)

	now = now.Add(7 * 24 * time.Hour)
	if err := domain.RefreshEntitlements(ctx); err != nil {
		t.Fatal(err)
	}
	runAdmin("C45", "", "failed-renewal-suspend")
	compareSnapshot("grace deadline", domainReceipt.SubscriptionID, adminReceipt.SubscriptionID, "suspended", "definitively_failed", 0)

	domainRetry, err := domain.RetryFailedPayment(ctx, domainRenewal.InvoiceID, "failed-renewal-domain-retry")
	if err != nil || domainRetry == domainRenewal.OperationID {
		t.Fatalf("domain retry: %s err=%v", domainRetry, err)
	}
	retryCommand := runAdmin("C08", adminOperation, "failed-renewal-admin-retry")
	var retryRefs map[string]string
	if err := json.Unmarshal(retryCommand.ResultRefs, &retryRefs); err != nil {
		t.Fatal(err)
	}
	adminRetry := retryRefs["operation_id"]
	if adminRetry == "" || adminRetry == adminOperation {
		t.Fatalf("admin retry did not create a new operation: %+v", retryRefs)
	}
	if replay, err := domain.RetryFailedPayment(ctx, domainRenewal.InvoiceID, "failed-renewal-domain-retry"); err != nil || replay != domainRetry {
		t.Fatalf("domain retry replay: %s err=%v", replay, err)
	}
	var retryPreviewID string
	if err := admin.db.QueryRowContext(ctx, `SELECT preview_id FROM admin_commands WHERE id=?`, retryCommand.ID).Scan(&retryPreviewID); err != nil {
		t.Fatal(err)
	}
	if replay, reused, err := admin.AdminSubmitCommand(ctx, "local-admin", "failed-renewal-admin-retry", "C08", adminOperation, json.RawMessage(`{}`), retryPreviewID); err != nil || !reused || replay.ID != retryCommand.ID {
		t.Fatalf("admin retry replay: %+v reused=%v err=%v", replay, reused, err)
	}
	compareOperations(domainRenewal.InvoiceID, adminInvoice)
	if _, err := domain.DispatchNext(ctx, ""); err != nil {
		t.Fatal(err)
	}
	runAdmin("C09", adminRetry, "failed-renewal-retry-dispatch")
	if err := domain.RefreshEntitlements(ctx); err != nil {
		t.Fatal(err)
	}
	runAdmin("C45", "", "failed-renewal-recovered")
	compareSnapshot("recovered renewal", domainReceipt.SubscriptionID, adminReceipt.SubscriptionID, "active", "succeeded", 2000)
	compareOperations(domainRenewal.InvoiceID, adminInvoice)
	if got, want := captureCount(t, admin), captureCount(t, domain); got != want || got != 3 {
		t.Fatalf("capture count differs: admin=%d domain=%d", got, want)
	}
}
