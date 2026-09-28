package lab

import (
	"context"
	"encoding/json"
	"testing"
)

func TestAdminLinkAccountPreviewAndReceipt(t *testing.T) {
	l, _ := openChangingLab(t)
	ctx := context.Background()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{"legacy_account_id":"legacy-admin-1","customer_id":"commerce-admin-1","beneficiary_id":"beneficiary-admin-1","cohort":"default","has_history":"false"}`)
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C36", "", payload)
	if err != nil {
		t.Fatal(err)
	}
	var before int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM account_links WHERE legacy_account_id='legacy-admin-1'`).Scan(&before); err != nil || before != 0 {
		t.Fatalf("preview wrote account link: %d, %v", before, err)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "link-account-001", "C36", "", payload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	command, err = l.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "succeeded" {
		t.Fatalf("execute: %+v err=%v", command, err)
	}
	var links, receipts int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM account_links WHERE legacy_account_id='legacy-admin-1' AND read_owner='legacy' AND writer_owner='legacy'`).Scan(&links); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if links != 1 || receipts != 1 {
		t.Fatalf("links=%d receipts=%d", links, receipts)
	}
}

func TestAdminShadowCommandsStoreComparisonsWithReceipts(t *testing.T) {
	l, _ := openChangingLab(t)
	ctx := context.Background()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	paid := paidProSubscription(t, l, "admin-shadow-customer")
	if _, err := l.LinkLegacyAccount(ctx, AccountLink{LegacyAccountID: "admin-shadow-legacy", CustomerID: "admin-shadow-customer", BeneficiaryID: "admin-shadow-beneficiary", Cohort: "default", HasHistory: true}); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		action, key string
		payload     json.RawMessage
	}{
		{"C37", "admin-shadow-quote-001", json.RawMessage(`{"plan_id":"pro","seats":"5","legacy_amount_minor":"10000","legacy_currency":"USD"}`)},
		{"C38", "admin-shadow-entitlement-001", json.RawMessage(`{"subscription_id":"` + paid.SubscriptionID + `","legacy_status":"active"}`)},
	}
	for _, tc := range cases {
		command, _, err := l.AdminSubmitCommand(ctx, "local-admin", tc.key, tc.action, "admin-shadow-legacy", tc.payload, "")
		if err != nil {
			t.Fatal(err)
		}
		command, err = l.AdminExecuteCommand(ctx, command.ID)
		if err != nil || command.Status != "succeeded" {
			t.Fatalf("%s: %+v err=%v", tc.action, command, err)
		}
	}
	var shadows, receipts int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM migration_shadows WHERE legacy_account_id='admin-shadow-legacy'`).Scan(&shadows); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts r JOIN admin_commands c ON c.id=r.command_id WHERE c.action_id IN ('C37','C38')`).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if shadows != 2 || receipts != 2 {
		t.Fatalf("shadows=%d receipts=%d", shadows, receipts)
	}
}

func TestAdminProvenanceBackfillAndReview(t *testing.T) {
	l, _, _ := openTestLab(t)
	ctx := context.Background()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	quote, err := l.CreateQuote(ctx, "admin-provenance-customer", "basic")
	if err != nil {
		t.Fatal(err)
	}
	purchased, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "admin-provenance-buy")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.LinkLegacyAccount(ctx, AccountLink{LegacyAccountID: "admin-provenance-legacy", CustomerID: "admin-provenance-customer", BeneficiaryID: "admin-provenance-beneficiary", Cohort: "default", HasHistory: true}); err != nil {
		t.Fatal(err)
	}
	backfill, err := json.Marshal(adminBackfillProvenancePayload{LegacyInvoiceID: "admin-legacy-invoice", LegacySubscriptionID: "admin-legacy-sub", CommerceSubscriptionID: purchased.SubscriptionID, CommerceInvoiceID: purchased.InvoiceID, PriceVersionID: "pro-v1"})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C39", "admin-provenance-legacy", backfill)
	if err != nil {
		t.Fatal(err)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "admin-provenance-backfill-001", "C39", "admin-provenance-legacy", backfill, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	command, err = l.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "succeeded" {
		t.Fatalf("backfill: %+v err=%v", command, err)
	}
	var status string
	if err := l.db.QueryRowContext(ctx, `SELECT status FROM legacy_provenance WHERE legacy_invoice_id='admin-legacy-invoice'`).Scan(&status); err != nil || status != "manual_review" {
		t.Fatalf("status=%s err=%v", status, err)
	}
	resolve, err := json.Marshal(adminResolveProvenancePayload{CommerceSubscriptionID: purchased.SubscriptionID, CommerceInvoiceID: purchased.InvoiceID, PriceVersionID: "basic-v1", Decision: "matched immutable invoice"})
	if err != nil {
		t.Fatal(err)
	}
	preview, err = l.AdminCreatePreview(ctx, "local-admin", "C40", "admin-legacy-invoice", resolve)
	if err != nil {
		t.Fatal(err)
	}
	command, _, err = l.AdminSubmitCommand(ctx, "local-admin", "admin-provenance-review-001", "C40", "admin-legacy-invoice", resolve, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	command, err = l.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "succeeded" {
		t.Fatalf("review: %+v err=%v", command, err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT status FROM legacy_provenance WHERE legacy_invoice_id='admin-legacy-invoice'`).Scan(&status); err != nil || status != "complete" {
		t.Fatalf("status=%s err=%v", status, err)
	}
	var receipts int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts r JOIN admin_commands c ON c.id=r.command_id WHERE c.action_id IN ('C39','C40')`).Scan(&receipts); err != nil || receipts != 2 {
		t.Fatalf("receipts=%d err=%v", receipts, err)
	}
}

func TestAdminCutoverReadWriterAndStop(t *testing.T) {
	l, _, _ := openTestLab(t)
	ctx := context.Background()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	accountID := "admin-cutover-legacy"
	if _, err := l.LinkLegacyAccount(ctx, AccountLink{LegacyAccountID: accountID, CustomerID: "admin-cutover-customer", BeneficiaryID: "admin-cutover-beneficiary", Cohort: "default"}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.ShadowQuote(ctx, accountID, "basic", 0, 2000, "USD"); err != nil {
		t.Fatal(err)
	}
	if _, err := l.ShadowEntitlement(ctx, accountID, "future-sub", "missing"); err != nil {
		t.Fatal(err)
	}
	if _, err := l.RunReconciliation(ctx, fixedNow); err != nil {
		t.Fatal(err)
	}
	thresholds := json.RawMessage(`{"max_quote_p95_millis":"5000","max_unknown_payments":"0","max_open_discrepancies":"0"}`)
	cases := []struct {
		action, key string
		payload     json.RawMessage
	}{
		{"C41", "admin-cutover-read-001", thresholds},
		{"C42", "admin-cutover-writer-001", thresholds},
		{"C43", "admin-cutover-stop-001", json.RawMessage(`{"reason":"shadow alert"}`)},
	}
	for _, tc := range cases {
		preview, err := l.AdminCreatePreview(ctx, "local-admin", tc.action, accountID, tc.payload)
		if err != nil {
			t.Fatalf("preview %s: %v", tc.action, err)
		}
		command, _, err := l.AdminSubmitCommand(ctx, "local-admin", tc.key, tc.action, accountID, tc.payload, preview.ID)
		if err != nil {
			t.Fatalf("submit %s: %v", tc.action, err)
		}
		command, err = l.AdminExecuteCommand(ctx, command.ID)
		if err != nil || command.Status != "succeeded" {
			t.Fatalf("execute %s: %+v err=%v", tc.action, command, err)
		}
	}
	link, err := l.AccountLink(ctx, accountID)
	if err != nil || link.ReadOwner != "commerce" || link.WriterOwner != "commerce" || !link.Stopped {
		t.Fatalf("link: %+v err=%v", link, err)
	}
	var receipts int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts r JOIN admin_commands c ON c.id=r.command_id WHERE c.action_id IN ('C41','C42','C43')`).Scan(&receipts); err != nil || receipts != 3 {
		t.Fatalf("receipts=%d err=%v", receipts, err)
	}
}
