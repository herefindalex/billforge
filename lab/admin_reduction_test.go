package lab

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestAdminReductionCreatesExactCreditAndReceipt(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	l, err := Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	quote, err := l.CreateQuote(ctx, "admin-reduction-customer", "basic")
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "admin-reduction-checkout")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.DispatchNext(ctx, ""); err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{"reduction_minor":"1000","reason":"service credit"}`)
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C11", accepted.InvoiceID, payload)
	if err != nil {
		t.Fatal(err)
	}
	var impact map[string]string
	if err := json.Unmarshal(preview.Impact, &impact); err != nil {
		t.Fatal(err)
	}
	if impact["credit_grant_minor"] != "1000" || impact["new_obligation_minor"] != "1000" ||
		impact["invoice_outstanding_after_minor"] != "0" || impact["created_payments_to_cancel"] != "0" {
		t.Fatalf("wrong impact: %+v", impact)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "reduction-001", "C11", accepted.InvoiceID, payload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	command, err = l.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "succeeded" {
		t.Fatalf("execute: %+v %v", command, err)
	}
	var refs struct {
		CorrectionID string   `json:"correction_id"`
		GrantIDs     []string `json:"grant_ids"`
	}
	if err := json.Unmarshal(command.ResultRefs, &refs); err != nil {
		t.Fatal(err)
	}
	if refs.CorrectionID == "" || len(refs.GrantIDs) != 1 {
		t.Fatalf("wrong refs: %+v", refs)
	}
	var grantAmount int64
	if err := l.db.QueryRowContext(ctx, `SELECT amount_minor FROM credit_grants WHERE id=?`, refs.GrantIDs[0]).Scan(&grantAmount); err != nil {
		t.Fatal(err)
	}
	var receipt int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipt); err != nil {
		t.Fatal(err)
	}
	if grantAmount != 1000 || receipt != 1 {
		t.Fatalf("grant=%d receipt=%d", grantAmount, receipt)
	}
	detail, err := l.AdminInvoiceDetail(ctx, accepted.InvoiceID)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Corrections) != 1 || detail.Corrections[0].OriginKind != "admin_command" || detail.Corrections[0].OriginID != command.ID {
		t.Fatalf("admin reduction origin: %+v", detail.Corrections)
	}
	replayed, replay, err := l.AdminSubmitCommand(ctx, "local-admin", "reduction-001", "C11", accepted.InvoiceID, payload, preview.ID)
	if err != nil || !replay || replayed.ID != command.ID {
		t.Fatalf("reduction replay: %+v replay=%v err=%v", replayed, replay, err)
	}
	replayed, err = l.AdminExecuteCommand(ctx, replayed.ID)
	if err != nil || replayed.Status != "succeeded" {
		t.Fatalf("execute replay: %+v %v", replayed, err)
	}
	var corrections, grants int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM corrections WHERE invoice_id=?`, accepted.InvoiceID).Scan(&corrections); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM credit_grants WHERE source_invoice_id=?`, accepted.InvoiceID).Scan(&grants); err != nil {
		t.Fatal(err)
	}
	if corrections != 1 || grants != 1 {
		t.Fatalf("reduction replay duplicated financial facts: corrections=%d grants=%d", corrections, grants)
	}
}

func TestAdminReductionCancelsOldCollectionAndCapturesOnlyRemainder(t *testing.T) {
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

	paid := paidBasicSubscription(t, l, "reduction-remaining-collection")
	now = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	renewals, err := l.RunRenewals(ctx)
	if err != nil || len(renewals) != 1 {
		t.Fatalf("renewal: %+v err=%v", renewals, err)
	}
	renewal := renewals[0]
	if renewal.SubscriptionID != paid.SubscriptionID {
		t.Fatalf("renewal subscription=%s want=%s", renewal.SubscriptionID, paid.SubscriptionID)
	}
	providerCaptures := captureCount(t, l)
	var oldProviderKey string
	if err := l.db.QueryRowContext(ctx, `SELECT provider_key FROM payment_operations WHERE id=?`, renewal.OperationID).Scan(&oldProviderKey); err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{"reduction_minor":"500","reason":"billing correction"}`)
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C11", renewal.InvoiceID, payload)
	if err != nil {
		t.Fatal(err)
	}
	var impact map[string]string
	if err := json.Unmarshal(preview.Impact, &impact); err != nil {
		t.Fatal(err)
	}
	if impact["new_obligation_minor"] != "1500" || impact["credit_grant_minor"] != "0" ||
		impact["invoice_outstanding_after_minor"] != "1500" || impact["created_payments_to_cancel"] != "1" {
		t.Fatalf("reduction preview hides collection change: %+v", impact)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "reduction-remaining-command", "C11", renewal.InvoiceID, payload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	command, err = l.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "succeeded" {
		t.Fatalf("reduce invoice: %+v err=%v", command, err)
	}
	var oldStatus, oldOutboxStatus string
	if err := l.db.QueryRowContext(ctx, `SELECT status FROM payment_operations WHERE id=?`, renewal.OperationID).Scan(&oldStatus); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT status FROM outbox WHERE id=?`, "capture:"+renewal.OperationID).Scan(&oldOutboxStatus); err != nil {
		t.Fatal(err)
	}
	if oldStatus != "cancelled" || oldOutboxStatus != "done" {
		t.Fatalf("original collection remains dispatchable: operation=%s outbox=%s", oldStatus, oldOutboxStatus)
	}
	if _, found, err := l.provider.Lookup(ctx, oldProviderKey); err != nil || found {
		t.Fatalf("old full collection reached provider: found=%v err=%v", found, err)
	}
	var grants int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM credit_grants WHERE source_invoice_id=?`, renewal.InvoiceID).Scan(&grants); err != nil || grants != 0 {
		t.Fatalf("unpaid reduction issued credit: grants=%d err=%v", grants, err)
	}

	newOperationID, err := l.CreatePayment(ctx, renewal.InvoiceID, 1500, "reduction-remaining-payment")
	if err != nil {
		t.Fatal(err)
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
	balance, err := loadInvoiceBalance(ctx, l.db, renewal.InvoiceID)
	if err != nil || balance.OutstandingMinor != 0 {
		t.Fatalf("reduced invoice not settled: %+v err=%v", balance, err)
	}
}
