package lab

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestAdminApplyCreditReceiptFailureRollsBackAndRecoversOriginalCommand(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	commerce, provider := filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db")
	now := fixedNow
	l, err := Open(commerce, provider, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}

	paid := paidBasicSubscription(t, l, "credit-receipt-failure-customer")
	correction, err := l.PostReduction(ctx, paid.InvoiceID, 1000, "service credit", "credit-receipt-reduction")
	if err != nil || len(correction.GrantIDs) != 1 {
		t.Fatalf("funded credit grant: %+v err=%v", correction, err)
	}
	grantID := correction.GrantIDs[0]
	now = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	renewals, err := l.RunRenewals(ctx)
	if err != nil || len(renewals) != 1 {
		t.Fatalf("renewal invoice: %+v err=%v", renewals, err)
	}
	invoiceID := renewals[0].InvoiceID
	beforeCredit, err := l.CreditBalance(ctx, grantID)
	if err != nil {
		t.Fatal(err)
	}
	beforeInvoice, err := l.Balance(ctx, invoiceID)
	if err != nil {
		t.Fatal(err)
	}
	var beforeCollections, beforeOutbox string
	if err := l.db.QueryRowContext(ctx, `SELECT COALESCE(GROUP_CONCAT(id||':'||status,'|'),'') FROM (SELECT id,status FROM payment_operations WHERE invoice_id=? ORDER BY id)`, invoiceID).Scan(&beforeCollections); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COALESCE(GROUP_CONCAT(id||':'||status,'|'),'') FROM (SELECT id,status FROM outbox ORDER BY id)`).Scan(&beforeOutbox); err != nil {
		t.Fatal(err)
	}
	var beforeCaptures int
	if err := l.provider.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM captures`).Scan(&beforeCaptures); err != nil {
		t.Fatal(err)
	}

	payload, err := json.Marshal(AdminApplyCreditPayload{InvoiceID: invoiceID, AmountMinor: "500"})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C12", grantID, payload)
	if err != nil {
		t.Fatal(err)
	}
	const key = "credit-receipt-failure-001"
	command, replay, err := l.AdminSubmitCommand(ctx, "local-admin", key, "C12", grantID, payload, preview.ID)
	if err != nil || replay {
		t.Fatalf("submit credit command: command=%+v replay=%v err=%v", command, replay, err)
	}
	if _, err := l.db.ExecContext(ctx, `CREATE TEMP TRIGGER fail_credit_receipt BEFORE INSERT ON admin_command_receipts BEGIN SELECT RAISE(ABORT, 'injected receipt failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := l.AdminExecuteCommand(ctx, command.ID); err == nil {
		t.Fatal("receipt failure must abort the credit application")
	}
	for _, check := range []struct {
		query string
		args  []any
	}{
		{`SELECT COUNT(*) FROM credit_applications WHERE grant_id=?`, []any{grantID}},
		{`SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, []any{command.ID}},
	} {
		var count int
		if err := l.db.QueryRowContext(ctx, check.query, check.args...).Scan(&count); err != nil || count != 0 {
			t.Fatalf("failed receipt left a financial fact: query=%s count=%d err=%v", check.query, count, err)
		}
	}
	creditAfterFailure, err := l.CreditBalance(ctx, grantID)
	if err != nil || !reflect.DeepEqual(creditAfterFailure, beforeCredit) {
		t.Fatalf("failed receipt changed credit balance: before=%+v after=%+v err=%v", beforeCredit, creditAfterFailure, err)
	}
	invoiceAfterFailure, err := l.Balance(ctx, invoiceID)
	if err != nil || !reflect.DeepEqual(invoiceAfterFailure, beforeInvoice) {
		t.Fatalf("failed receipt changed invoice balance: before=%+v after=%+v err=%v", beforeInvoice, invoiceAfterFailure, err)
	}
	var afterCollections, afterOutbox string
	if err := l.db.QueryRowContext(ctx, `SELECT COALESCE(GROUP_CONCAT(id||':'||status,'|'),'') FROM (SELECT id,status FROM payment_operations WHERE invoice_id=? ORDER BY id)`, invoiceID).Scan(&afterCollections); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COALESCE(GROUP_CONCAT(id||':'||status,'|'),'') FROM (SELECT id,status FROM outbox ORDER BY id)`).Scan(&afterOutbox); err != nil {
		t.Fatal(err)
	}
	if afterCollections != beforeCollections || afterOutbox != beforeOutbox {
		t.Fatalf("failed receipt changed pending collection: operations=%q/%q outbox=%q/%q", beforeCollections, afterCollections, beforeOutbox, afterOutbox)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	l, err = Open(commerce, provider, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := l.AdminResumeAccepted(ctx); err != nil {
			t.Fatal(err)
		}
	}
	command, err = l.AdminCommand(ctx, command.ID)
	if err != nil || command.Status != "succeeded" {
		t.Fatalf("credit application did not recover: command=%+v err=%v", command, err)
	}
	again, replay, err := l.AdminSubmitCommand(ctx, "local-admin", key, "C12", grantID, payload, preview.ID)
	if err != nil || !replay || again.ID != command.ID {
		t.Fatalf("original request key changed recovered command: command=%+v replay=%v err=%v", again, replay, err)
	}
	var applications, amount, receipts, afterCaptures int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(amount_minor),0) FROM credit_applications WHERE grant_id=? AND invoice_id=?`, grantID, invoiceID).Scan(&applications, &amount); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if err := l.provider.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM captures`).Scan(&afterCaptures); err != nil {
		t.Fatal(err)
	}
	creditAfterRecovery, err := l.CreditBalance(ctx, grantID)
	if err != nil {
		t.Fatal(err)
	}
	invoiceAfterRecovery, err := l.Balance(ctx, invoiceID)
	if err != nil {
		t.Fatal(err)
	}
	if applications != 1 || amount != 500 || receipts != 1 || afterCaptures != beforeCaptures || creditAfterRecovery.AppliedMinor != beforeCredit.AppliedMinor+500 || creditAfterRecovery.AvailableMinor != beforeCredit.AvailableMinor-500 || invoiceAfterRecovery.OutstandingMinor != beforeInvoice.OutstandingMinor-500 {
		t.Fatalf("recovered credit state: applications=%d amount=%d receipts=%d captures=%d/%d credit=%+v invoice=%+v", applications, amount, receipts, beforeCaptures, afterCaptures, creditAfterRecovery, invoiceAfterRecovery)
	}
}
