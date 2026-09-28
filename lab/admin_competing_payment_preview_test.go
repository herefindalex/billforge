package lab

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestAdminCompetingPaymentPreviewsKeepSingleReplacementOperation(t *testing.T) {
	ctx := context.Background()
	l, _, _ := openTestLab(t)
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	purchase := purchaseHundred(t, l, "competing-payment", "competing-payment-checkout")
	create := func(key, amount string) AdminCommand {
		t.Helper()
		payload := json.RawMessage(`{"amount_minor":"` + amount + `"}`)
		preview, err := l.AdminCreatePreview(ctx, "local-admin", "C07", purchase.InvoiceID, payload)
		if err != nil {
			t.Fatal(err)
		}
		command, _, err := l.AdminSubmitCommand(ctx, "local-admin", key, "C07", purchase.InvoiceID, payload, preview.ID)
		if err != nil {
			t.Fatal(err)
		}
		return command
	}
	first := create("payment-six-thousand", "6000")
	second := create("payment-seven-thousand", "7000")
	second, err := l.AdminExecuteCommand(ctx, second.ID)
	if err != nil || second.Status != "succeeded" {
		t.Fatalf("second preview command %+v %v", second, err)
	}
	first, err = l.AdminExecuteCommand(ctx, first.ID)
	if err != nil || first.Status != "failed" || first.ErrorCode != "PREVIEW_STALE" {
		t.Fatalf("stale first preview command %+v %v", first, err)
	}
	for _, tc := range []struct {
		command AdminCommand
		key     string
		amount  string
	}{
		{first, "payment-six-thousand", "6000"},
		{second, "payment-seven-thousand", "7000"},
	} {
		payload := json.RawMessage(`{"amount_minor":"` + tc.amount + `"}`)
		var previewID string
		if err := l.db.QueryRowContext(ctx, `SELECT preview_id FROM admin_commands WHERE id=?`, tc.command.ID).Scan(&previewID); err != nil {
			t.Fatal(err)
		}
		replayed, same, err := l.AdminSubmitCommand(ctx, "local-admin", tc.key, "C07", purchase.InvoiceID, payload, previewID)
		if err != nil || !same || replayed.ID != tc.command.ID || replayed.Status != tc.command.Status {
			t.Fatalf("replay %s: %+v same=%v err=%v", tc.key, replayed, same, err)
		}
		if _, _, err := l.AdminSubmitCommand(ctx, "local-admin", tc.key, "C07", purchase.InvoiceID, json.RawMessage(`{"amount_minor":"5000"}`), previewID); !errors.Is(err, ErrAdminIdempotencyConflict) {
			t.Fatalf("changed intent for %s: %v", tc.key, err)
		}
	}
	var oldStatus string
	if err := l.db.QueryRowContext(ctx, `SELECT status FROM payment_operations WHERE id=?`, purchase.OperationID).Scan(&oldStatus); err != nil || oldStatus != "cancelled" {
		t.Fatalf("original operation status=%s err=%v", oldStatus, err)
	}
	var operations, createdAmount, oldOutboxDone, firstReceipts, secondReceipts int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM payment_operations WHERE invoice_id=?`, purchase.InvoiceID).Scan(&operations); err != nil || operations != 2 {
		t.Fatalf("payment operation count=%d err=%v", operations, err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT amount_minor FROM payment_operations WHERE invoice_id=? AND status='created'`, purchase.InvoiceID).Scan(&createdAmount); err != nil || createdAmount != 7000 {
		t.Fatalf("winner amount=%d err=%v", createdAmount, err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox WHERE id=? AND status='done'`, "capture:"+purchase.OperationID).Scan(&oldOutboxDone); err != nil || oldOutboxDone != 1 {
		t.Fatalf("old collection outbox done=%d err=%v", oldOutboxDone, err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, first.ID).Scan(&firstReceipts); err != nil || firstReceipts != 0 {
		t.Fatalf("stale command receipts=%d err=%v", firstReceipts, err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, second.ID).Scan(&secondReceipts); err != nil || secondReceipts != 1 {
		t.Fatalf("winning command receipts=%d err=%v", secondReceipts, err)
	}
	if captures := captureCount(t, l); captures != 0 {
		t.Fatalf("C07 dispatched provider unexpectedly: %d", captures)
	}
}

func TestAdminCompetingRetryPreviewsKeepSingleNewObligation(t *testing.T) {
	ctx := context.Background()
	l, _, _ := openTestLab(t)
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	purchase := purchaseHundred(t, l, "competing-retry", "competing-retry-checkout")
	if err := l.SetFakePaymentDecision(ctx, purchase.OperationID, "definitively_failed"); err != nil {
		t.Fatal(err)
	}
	if _, err := l.DispatchNext(ctx, ""); err != nil {
		t.Fatal(err)
	}
	create := func(key string) AdminCommand {
		t.Helper()
		preview, err := l.AdminCreatePreview(ctx, "local-admin", "C08", purchase.OperationID, json.RawMessage(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		command, _, err := l.AdminSubmitCommand(ctx, "local-admin", key, "C08", purchase.OperationID, json.RawMessage(`{}`), preview.ID)
		if err != nil {
			t.Fatal(err)
		}
		return command
	}
	first := create("retry-first-preview")
	second := create("retry-second-preview")
	first, err := l.AdminExecuteCommand(ctx, first.ID)
	if err != nil || first.Status != "succeeded" {
		t.Fatalf("first retry command %+v %v", first, err)
	}
	second, err = l.AdminExecuteCommand(ctx, second.ID)
	if err != nil || second.Status != "failed" || second.ErrorCode != "PREVIEW_STALE" {
		t.Fatalf("stale retry command %+v %v", second, err)
	}
	var refs struct {
		OperationID string `json:"operation_id"`
	}
	if err := json.Unmarshal(first.ResultRefs, &refs); err != nil || refs.OperationID == "" || refs.OperationID == purchase.OperationID {
		t.Fatalf("retry result refs %+v %v", refs, err)
	}
	var retryRequests, operations, firstReceipts, secondReceipts int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM payment_retry_requests WHERE operation_id=?`, refs.OperationID).Scan(&retryRequests); err != nil || retryRequests != 1 {
		t.Fatalf("retry requests=%d err=%v", retryRequests, err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM payment_operations WHERE invoice_id=?`, purchase.InvoiceID).Scan(&operations); err != nil || operations != 2 {
		t.Fatalf("payment operations=%d err=%v", operations, err)
	}
	var amount int64
	if err := l.db.QueryRowContext(ctx, `SELECT amount_minor FROM payment_operations WHERE id=? AND status='created'`, refs.OperationID).Scan(&amount); err != nil || amount != 10000 {
		t.Fatalf("new retry amount=%d err=%v", amount, err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, first.ID).Scan(&firstReceipts); err != nil || firstReceipts != 1 {
		t.Fatalf("first retry receipts=%d err=%v", firstReceipts, err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, second.ID).Scan(&secondReceipts); err != nil || secondReceipts != 0 {
		t.Fatalf("stale retry receipts=%d err=%v", secondReceipts, err)
	}
	for _, command := range []AdminCommand{first, second} {
		var previewID string
		if err := l.db.QueryRowContext(ctx, `SELECT preview_id FROM admin_commands WHERE id=?`, command.ID).Scan(&previewID); err != nil {
			t.Fatal(err)
		}
		replayed, same, err := l.AdminSubmitCommand(ctx, "local-admin", command.IdempotencyKey, "C08", purchase.OperationID, json.RawMessage(`{}`), previewID)
		if err != nil || !same || replayed.ID != command.ID || replayed.Status != command.Status {
			t.Fatalf("retry replay %+v same=%v err=%v", replayed, same, err)
		}
	}
	var succeeded, newKeyRows int
	if err := l.provider.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM captures WHERE status='succeeded'`).Scan(&succeeded); err != nil || succeeded != 0 {
		t.Fatalf("unexpected successful provider capture=%d err=%v", succeeded, err)
	}
	var newProviderKey string
	if err := l.db.QueryRowContext(ctx, `SELECT provider_key FROM payment_operations WHERE id=?`, refs.OperationID).Scan(&newProviderKey); err != nil {
		t.Fatal(err)
	}
	if err := l.provider.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM captures WHERE provider_key=?`, newProviderKey).Scan(&newKeyRows); err != nil || newKeyRows != 0 {
		t.Fatalf("retry provider key dispatched=%d err=%v", newKeyRows, err)
	}
}
