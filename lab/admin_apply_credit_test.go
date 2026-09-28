package lab

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestAdminApplyCreditUsesGrantAndInvoiceBalanceAtomically(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	now := fixedNow
	l, err := Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	paid := paidBasicSubscription(t, l, "admin-credit-customer")
	correction, err := l.PostReduction(ctx, paid.InvoiceID, 1000, "service credit", "admin-credit-reduction")
	if err != nil || len(correction.GrantIDs) != 1 {
		t.Fatalf("correction: %+v %v", correction, err)
	}
	now = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	renewals, err := l.RunRenewals(ctx)
	if err != nil || len(renewals) != 1 {
		t.Fatalf("renewals: %+v %v", renewals, err)
	}
	grantID := correction.GrantIDs[0]
	payload, _ := json.Marshal(AdminApplyCreditPayload{InvoiceID: renewals[0].InvoiceID, AmountMinor: "500"})
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C12", grantID, payload)
	if err != nil {
		t.Fatal(err)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "apply-credit-001", "C12", grantID, payload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	command, err = l.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "succeeded" {
		t.Fatalf("execute: %+v %v", command, err)
	}
	var refs map[string]string
	if err := json.Unmarshal(command.ResultRefs, &refs); err != nil {
		t.Fatal(err)
	}
	if refs["application_id"] == "" || refs["amount_minor"] != "500" {
		t.Fatalf("wrong refs: %+v", refs)
	}
	var applications, receipts int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM credit_applications WHERE id=? AND amount_minor=500`, refs["application_id"]).Scan(&applications); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if applications != 1 || receipts != 1 {
		t.Fatalf("applications=%d receipts=%d", applications, receipts)
	}

	stalePayload, _ := json.Marshal(AdminApplyCreditPayload{InvoiceID: renewals[0].InvoiceID, AmountMinor: "200"})
	stalePreview, err := l.AdminCreatePreview(ctx, "local-admin", "C12", grantID, stalePayload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.ReserveRefund(ctx, grantID, 100, "reserve-after-credit-preview"); err != nil {
		t.Fatal(err)
	}
	staleCommand, _, err := l.AdminSubmitCommand(ctx, "local-admin", "apply-credit-stale-001", "C12", grantID, stalePayload, stalePreview.ID)
	if err != nil {
		t.Fatal(err)
	}
	staleCommand, err = l.AdminExecuteCommand(ctx, staleCommand.ID)
	if err != nil || staleCommand.Status != "failed" || staleCommand.ErrorCode != "PREVIEW_STALE" {
		t.Fatalf("stale command: %+v %v", staleCommand, err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM credit_applications WHERE grant_id=?`, grantID).Scan(&applications); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, staleCommand.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if applications != 1 || receipts != 0 {
		t.Fatalf("stale preview changed credit: applications=%d receipts=%d", applications, receipts)
	}
	replayed, replay, err := l.AdminSubmitCommand(ctx, "local-admin", "apply-credit-stale-001", "C12", grantID, stalePayload, stalePreview.ID)
	if err != nil || !replay || replayed.ID != staleCommand.ID || replayed.ErrorCode != "PREVIEW_STALE" {
		t.Fatalf("stale command replay changed result: %+v replay=%v err=%v", replayed, replay, err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, staleCommand.ID).Scan(&receipts); err != nil || receipts != 0 {
		t.Fatalf("stale replay wrote receipt: count=%d err=%v", receipts, err)
	}
}

func TestAdminApplyCreditCancelsOldCollectionAndCapturesOnlyRemainder(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	now := fixedNow
	l, err := Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}

	paid := paidBasicSubscription(t, l, "credit-remaining-collection")
	correction, err := l.PostReduction(ctx, paid.InvoiceID, 1000, "funded credit", "credit-remaining-reduction")
	if err != nil || len(correction.GrantIDs) != 1 {
		t.Fatalf("source credit: %+v err=%v", correction, err)
	}
	now = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	renewals, err := l.RunRenewals(ctx)
	if err != nil || len(renewals) != 1 {
		t.Fatalf("renewal: %+v err=%v", renewals, err)
	}
	renewal := renewals[0]
	var oldProviderKey string
	if err := l.db.QueryRowContext(ctx, `SELECT provider_key FROM payment_operations WHERE id=?`, renewal.OperationID).Scan(&oldProviderKey); err != nil {
		t.Fatal(err)
	}
	providerCaptures := captureCount(t, l)
	creditPayload, _ := json.Marshal(AdminApplyCreditPayload{InvoiceID: renewal.InvoiceID, AmountMinor: "500"})
	creditPreview, err := l.AdminCreatePreview(ctx, "local-admin", "C12", correction.GrantIDs[0], creditPayload)
	if err != nil {
		t.Fatal(err)
	}
	creditCommand, _, err := l.AdminSubmitCommand(ctx, "local-admin", "credit-remaining-apply", "C12", correction.GrantIDs[0], creditPayload, creditPreview.ID)
	if err != nil {
		t.Fatal(err)
	}
	creditCommand, err = l.AdminExecuteCommand(ctx, creditCommand.ID)
	if err != nil || creditCommand.Status != "succeeded" {
		t.Fatalf("apply credit: %+v err=%v", creditCommand, err)
	}
	var oldStatus, oldOutboxStatus string
	if err := l.db.QueryRowContext(ctx, `SELECT status FROM payment_operations WHERE id=?`, renewal.OperationID).Scan(&oldStatus); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT status FROM outbox WHERE id=?`, "capture:"+renewal.OperationID).Scan(&oldOutboxStatus); err != nil {
		t.Fatal(err)
	}
	if oldStatus != "cancelled" || oldOutboxStatus != "done" {
		t.Fatalf("old full collection remains dispatchable: operation=%s outbox=%s", oldStatus, oldOutboxStatus)
	}
	if _, found, err := l.provider.Lookup(ctx, oldProviderKey); err != nil || found {
		t.Fatalf("old full collection reached provider: found=%v err=%v", found, err)
	}
	remaining, err := loadInvoiceBalance(ctx, l.db, renewal.InvoiceID)
	if err != nil || remaining.OutstandingMinor != 1500 {
		t.Fatalf("remaining due after credit: %+v err=%v", remaining, err)
	}

	paymentPayload, _ := json.Marshal(AdminAmountPayload{AmountMinor: "1500"})
	paymentPreview, err := l.AdminCreatePreview(ctx, "local-admin", "C07", renewal.InvoiceID, paymentPayload)
	if err != nil {
		t.Fatal(err)
	}
	paymentCommand, _, err := l.AdminSubmitCommand(ctx, "local-admin", "credit-remaining-payment", "C07", renewal.InvoiceID, paymentPayload, paymentPreview.ID)
	if err != nil {
		t.Fatal(err)
	}
	paymentCommand, err = l.AdminExecuteCommand(ctx, paymentCommand.ID)
	if err != nil || paymentCommand.Status != "succeeded" {
		t.Fatalf("create remaining payment: %+v err=%v", paymentCommand, err)
	}
	var paymentRefs map[string]string
	if err := json.Unmarshal(paymentCommand.ResultRefs, &paymentRefs); err != nil {
		t.Fatal(err)
	}
	newOperationID := paymentRefs["operation_id"]
	if newOperationID == "" || newOperationID == renewal.OperationID {
		t.Fatalf("new collection identity invalid: %q", newOperationID)
	}
	if _, err := l.DispatchCapture(ctx, newOperationID, ""); err != nil {
		t.Fatal(err)
	}
	var newProviderKey string
	if err := l.db.QueryRowContext(ctx, `SELECT provider_key FROM payment_operations WHERE id=?`, newOperationID).Scan(&newProviderKey); err != nil {
		t.Fatal(err)
	}
	capture, found, err := l.provider.Lookup(ctx, newProviderKey)
	if err != nil || !found || capture.Amount != 1500 || capture.Currency != "USD" {
		t.Fatalf("remaining capture: %+v found=%v err=%v", capture, found, err)
	}
	if got := captureCount(t, l); got != providerCaptures+1 {
		t.Fatalf("provider capture count=%d want=%d", got, providerCaptures+1)
	}
	remaining, err = loadInvoiceBalance(ctx, l.db, renewal.InvoiceID)
	if err != nil || remaining.OutstandingMinor != 0 {
		t.Fatalf("invoice not settled by credit and remaining capture: %+v err=%v", remaining, err)
	}
}
