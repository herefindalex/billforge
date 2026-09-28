package lab

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestAdminReplacementInvalidatesOldDispatchAndCapturesOnlyNewOperation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	dir := t.TempDir()
	commercePath, providerPath := filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db")
	first, err := Open(commercePath, providerPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Close() })
	if err := first.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	purchase := purchaseHundred(t, first, "replacement-dispatch", "replacement-dispatch-checkout")
	second, err := Open(commercePath, providerPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	if err := second.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}

	empty := json.RawMessage(`{}`)
	oldPreview, err := first.AdminCreatePreview(ctx, "local-admin", "C09", purchase.OperationID, empty)
	if err != nil {
		t.Fatal(err)
	}
	oldDispatch, _, err := first.AdminSubmitCommand(ctx, "local-admin", "replacement-old-dispatch", "C09", purchase.OperationID, empty, oldPreview.ID)
	if err != nil {
		t.Fatal(err)
	}
	amount := json.RawMessage(`{"amount_minor":"6000"}`)
	replacementPreview, err := first.AdminCreatePreview(ctx, "local-admin", "C07", purchase.InvoiceID, amount)
	if err != nil {
		t.Fatal(err)
	}
	replacement, _, err := first.AdminSubmitCommand(ctx, "local-admin", "replacement-create-payment", "C07", purchase.InvoiceID, amount, replacementPreview.ID)
	if err != nil {
		t.Fatal(err)
	}
	replacement, err = second.AdminExecuteCommand(ctx, replacement.ID)
	if err != nil || replacement.Status != "succeeded" {
		t.Fatalf("replacement: %+v %v", replacement, err)
	}
	var refs map[string]string
	if err := json.Unmarshal(replacement.ResultRefs, &refs); err != nil {
		t.Fatal(err)
	}
	newOperationID := refs["operation_id"]
	if newOperationID == "" || newOperationID == purchase.OperationID || refs["invoice_id"] != purchase.InvoiceID || refs["amount_minor"] != "6000" {
		t.Fatalf("wrong replacement refs: %+v", refs)
	}

	oldDispatch, err = first.AdminExecuteCommand(ctx, oldDispatch.ID)
	if err != nil || oldDispatch.Status != "failed" || oldDispatch.ErrorCode != "PREVIEW_STALE" {
		t.Fatalf("old dispatch must be stale after replacement: %+v %v", oldDispatch, err)
	}
	preview, err := second.AdminCreatePreview(ctx, "local-admin", "C09", newOperationID, empty)
	if err != nil {
		t.Fatal(err)
	}
	dispatch, _, err := second.AdminSubmitCommand(ctx, "local-admin", "replacement-new-dispatch", "C09", newOperationID, empty, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	dispatch, err = first.AdminExecuteCommand(ctx, dispatch.ID)
	if err != nil || dispatch.Status != "succeeded" {
		t.Fatalf("new dispatch: %+v %v", dispatch, err)
	}
	if _, err := second.AdminExecuteCommand(ctx, dispatch.ID); err != nil {
		t.Fatal(err)
	}
	var captures, oldCaptures, receipts, cancelled, succeeded int
	var oldProviderKey, newProviderKey string
	if err := first.db.QueryRowContext(ctx, `SELECT provider_key FROM payment_operations WHERE id=?`, purchase.OperationID).Scan(&oldProviderKey); err != nil {
		t.Fatal(err)
	}
	if err := first.db.QueryRowContext(ctx, `SELECT provider_key FROM payment_operations WHERE id=?`, newOperationID).Scan(&newProviderKey); err != nil {
		t.Fatal(err)
	}
	if err := first.provider.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM captures`).Scan(&captures); err != nil {
		t.Fatal(err)
	}
	if err := first.provider.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM captures WHERE provider_key=?`, oldProviderKey).Scan(&oldCaptures); err != nil {
		t.Fatal(err)
	}
	var capturedAmount int64
	var capturedCurrency, capturedStatus string
	if err := first.provider.db.QueryRowContext(ctx, `SELECT amount_minor,currency,status FROM captures WHERE provider_key=?`, newProviderKey).Scan(&capturedAmount, &capturedCurrency, &capturedStatus); err != nil {
		t.Fatal(err)
	}
	if err := first.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id IN (?,?)`, replacement.ID, dispatch.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if err := first.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM payment_operations WHERE id=? AND invoice_id=? AND status='cancelled'`, purchase.OperationID, purchase.InvoiceID).Scan(&cancelled); err != nil {
		t.Fatal(err)
	}
	if err := first.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM payment_operations WHERE id=? AND invoice_id=? AND amount_minor=6000 AND status='succeeded'`, newOperationID, purchase.InvoiceID).Scan(&succeeded); err != nil {
		t.Fatal(err)
	}
	if captures != 1 || oldCaptures != 0 || capturedAmount != 6000 || capturedCurrency != "USD" || capturedStatus != "succeeded" || receipts != 2 || cancelled != 1 || succeeded != 1 {
		t.Fatalf("wrong financial effect: captures=%d old=%d new=%d/%s/%s receipts=%d cancelled=%d succeeded=%d", captures, oldCaptures, capturedAmount, capturedCurrency, capturedStatus, receipts, cancelled, succeeded)
	}
	balance, err := first.Balance(ctx, purchase.InvoiceID)
	if err != nil || balance.OutstandingMinor != 4000 {
		t.Fatalf("invoice balance: %+v %v", balance, err)
	}
}

func TestAdminRetryDispatchMakesCompetingRetryStaleWithoutDuplicateCapture(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	dir := t.TempDir()
	commercePath, providerPath := filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db")
	first, err := Open(commercePath, providerPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Close() })
	if err := first.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	purchase := purchaseHundred(t, first, "retry-dispatch", "retry-dispatch-checkout")
	if err := first.SetFakePaymentDecision(ctx, purchase.OperationID, "definitively_failed"); err != nil {
		t.Fatal(err)
	}
	if _, err := first.DispatchNext(ctx, ""); err != nil {
		t.Fatal(err)
	}
	second, err := Open(commercePath, providerPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	if err := second.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	empty := json.RawMessage(`{}`)
	createRetry := func(l *Lab, key string) AdminCommand {
		t.Helper()
		preview, err := l.AdminCreatePreview(ctx, "local-admin", "C08", purchase.OperationID, empty)
		if err != nil {
			t.Fatal(err)
		}
		command, _, err := l.AdminSubmitCommand(ctx, "local-admin", key, "C08", purchase.OperationID, empty, preview.ID)
		if err != nil {
			t.Fatal(err)
		}
		return command
	}
	winner := createRetry(first, "retry-dispatch-winner")
	loser := createRetry(second, "retry-dispatch-loser")
	winner, err = second.AdminExecuteCommand(ctx, winner.ID)
	if err != nil || winner.Status != "succeeded" {
		t.Fatalf("retry: %+v %v", winner, err)
	}
	var refs map[string]string
	if err := json.Unmarshal(winner.ResultRefs, &refs); err != nil {
		t.Fatal(err)
	}
	newOperationID := refs["operation_id"]
	if newOperationID == "" || newOperationID == purchase.OperationID || refs["failed_operation_id"] != purchase.OperationID || refs["invoice_id"] != purchase.InvoiceID || refs["amount_minor"] != "10000" {
		t.Fatalf("wrong retry refs: %+v", refs)
	}
	preview, err := first.AdminCreatePreview(ctx, "local-admin", "C09", newOperationID, empty)
	if err != nil {
		t.Fatal(err)
	}
	dispatch, _, err := first.AdminSubmitCommand(ctx, "local-admin", "retry-dispatch-new", "C09", newOperationID, empty, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	dispatch, err = second.AdminExecuteCommand(ctx, dispatch.ID)
	if err != nil || dispatch.Status != "succeeded" {
		t.Fatalf("retry dispatch: %+v %v", dispatch, err)
	}
	loser, err = first.AdminExecuteCommand(ctx, loser.ID)
	if err != nil || loser.Status != "failed" || loser.ErrorCode != "PREVIEW_STALE" {
		t.Fatalf("competing retry must be stale: %+v %v", loser, err)
	}
	if _, err := first.AdminExecuteCommand(ctx, dispatch.ID); err != nil {
		t.Fatal(err)
	}
	var captures, retryRequests, receipts, paid int
	var newProviderKey string
	if err := first.db.QueryRowContext(ctx, `SELECT provider_key FROM payment_operations WHERE id=?`, newOperationID).Scan(&newProviderKey); err != nil {
		t.Fatal(err)
	}
	if err := first.provider.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM captures`).Scan(&captures); err != nil {
		t.Fatal(err)
	}
	var capturedAmount int64
	var capturedCurrency, capturedStatus string
	if err := first.provider.db.QueryRowContext(ctx, `SELECT amount_minor,currency,status FROM captures WHERE provider_key=?`, newProviderKey).Scan(&capturedAmount, &capturedCurrency, &capturedStatus); err != nil {
		t.Fatal(err)
	}
	if err := first.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM payment_retry_requests WHERE invoice_id=?`, purchase.InvoiceID).Scan(&retryRequests); err != nil {
		t.Fatal(err)
	}
	if err := first.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id IN (?,?)`, winner.ID, dispatch.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if err := first.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM payment_operations WHERE id=? AND invoice_id=? AND amount_minor=10000 AND status='succeeded'`, newOperationID, purchase.InvoiceID).Scan(&paid); err != nil {
		t.Fatal(err)
	}
	if captures != 2 || capturedAmount != 10000 || capturedCurrency != "USD" || capturedStatus != "succeeded" || retryRequests != 1 || receipts != 2 || paid != 1 {
		t.Fatalf("wrong retry financial effect: captures=%d new=%d/%s/%s retry_requests=%d receipts=%d paid=%d", captures, capturedAmount, capturedCurrency, capturedStatus, retryRequests, receipts, paid)
	}
	balance, err := first.Balance(ctx, purchase.InvoiceID)
	if err != nil || balance.OutstandingMinor != 0 {
		t.Fatalf("invoice balance: %+v %v", balance, err)
	}
}

func TestAdminDispatchInvalidatesEarlierReplacementPreview(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	dir := t.TempDir()
	commercePath, providerPath := filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db")
	first, err := Open(commercePath, providerPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Close() })
	if err := first.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	purchase := purchaseHundred(t, first, "dispatch-before-replacement", "dispatch-before-replacement-checkout")
	second, err := Open(commercePath, providerPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	if err := second.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	amount, empty := json.RawMessage(`{"amount_minor":"6000"}`), json.RawMessage(`{}`)
	replacementPreview, err := first.AdminCreatePreview(ctx, "local-admin", "C07", purchase.InvoiceID, amount)
	if err != nil {
		t.Fatal(err)
	}
	replacement, _, err := first.AdminSubmitCommand(ctx, "local-admin", "dispatch-first-replacement", "C07", purchase.InvoiceID, amount, replacementPreview.ID)
	if err != nil {
		t.Fatal(err)
	}
	dispatchPreview, err := second.AdminCreatePreview(ctx, "local-admin", "C09", purchase.OperationID, empty)
	if err != nil {
		t.Fatal(err)
	}
	dispatch, _, err := second.AdminSubmitCommand(ctx, "local-admin", "dispatch-first-capture", "C09", purchase.OperationID, empty, dispatchPreview.ID)
	if err != nil {
		t.Fatal(err)
	}
	dispatch, err = second.AdminExecuteCommand(ctx, dispatch.ID)
	if err != nil || dispatch.Status != "succeeded" {
		t.Fatalf("dispatch: %+v %v", dispatch, err)
	}
	replacement, err = first.AdminExecuteCommand(ctx, replacement.ID)
	if err != nil || replacement.Status != "failed" || replacement.ErrorCode != "PREVIEW_STALE" {
		t.Fatalf("replacement after capture must be stale: %+v %v", replacement, err)
	}
	var operations, receipts, captures int
	if err := first.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM payment_operations WHERE invoice_id=?`, purchase.InvoiceID).Scan(&operations); err != nil {
		t.Fatal(err)
	}
	if err := first.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id IN (?,?)`, dispatch.ID, replacement.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if err := first.provider.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM captures`).Scan(&captures); err != nil {
		t.Fatal(err)
	}
	if operations != 1 || receipts != 1 || captures != 1 {
		t.Fatalf("dispatch-first effect: operations=%d receipts=%d captures=%d", operations, receipts, captures)
	}
	balance, err := first.Balance(ctx, purchase.InvoiceID)
	if err != nil || balance.OutstandingMinor != 0 {
		t.Fatalf("invoice balance: %+v %v", balance, err)
	}
}

func TestAdminReplacementAndDispatchCompeteAcrossLabInstances(t *testing.T) {
	for attempt := 0; attempt < 5; attempt++ {
		t.Run("attempt-"+strconv.Itoa(attempt), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			dir := t.TempDir()
			commercePath, providerPath := filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db")
			first, err := Open(commercePath, providerPath, nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = first.Close() })
			if err := first.InitAdmin(ctx); err != nil {
				t.Fatal(err)
			}
			purchase := purchaseHundred(t, first, "competing-replacement-dispatch", "competing-replacement-dispatch-checkout")
			second, err := Open(commercePath, providerPath, nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = second.Close() })
			if err := second.InitAdmin(ctx); err != nil {
				t.Fatal(err)
			}
			amount, empty := json.RawMessage(`{"amount_minor":"6000"}`), json.RawMessage(`{}`)
			replacementPreview, err := first.AdminCreatePreview(ctx, "local-admin", "C07", purchase.InvoiceID, amount)
			if err != nil {
				t.Fatal(err)
			}
			replacement, _, err := first.AdminSubmitCommand(ctx, "local-admin", "competing-replacement", "C07", purchase.InvoiceID, amount, replacementPreview.ID)
			if err != nil {
				t.Fatal(err)
			}
			dispatchPreview, err := second.AdminCreatePreview(ctx, "local-admin", "C09", purchase.OperationID, empty)
			if err != nil {
				t.Fatal(err)
			}
			dispatch, _, err := second.AdminSubmitCommand(ctx, "local-admin", "competing-dispatch", "C09", purchase.OperationID, empty, dispatchPreview.ID)
			if err != nil {
				t.Fatal(err)
			}
			type result struct {
				id      string
				command AdminCommand
				err     error
			}
			start := make(chan struct{})
			ready := make(chan struct{}, 2)
			results := make(chan result, 2)
			for _, item := range []struct {
				lab     *Lab
				command AdminCommand
			}{{first, replacement}, {second, dispatch}} {
				go func(l *Lab, c AdminCommand) {
					ready <- struct{}{}
					<-start
					settled, err := l.AdminExecuteCommand(ctx, c.ID)
					results <- result{c.ID, settled, err}
				}(item.lab, item.command)
			}
			<-ready
			<-ready
			close(start)
			for range 2 {
				select {
				case outcome := <-results:
					if outcome.err != nil {
						var operationStatus, commandStatus string
						_ = first.db.QueryRowContext(ctx, `SELECT status FROM payment_operations WHERE id=?`, purchase.OperationID).Scan(&operationStatus)
						_ = first.db.QueryRowContext(ctx, `SELECT status FROM admin_commands WHERE id=?`, outcome.id).Scan(&commandStatus)
						t.Fatalf("concurrent command %s (replacement=%t, operation=%s, command=%s): %v", outcome.id, outcome.id == replacement.ID, operationStatus, commandStatus, outcome.err)
					}
					if outcome.command.ID == replacement.ID {
						replacement = outcome.command
					} else {
						dispatch = outcome.command
					}
				case <-ctx.Done():
					t.Fatalf("concurrent commands timed out: %v", ctx.Err())
				}
			}
			var captures, operations, receipts int
			if err := first.provider.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM captures`).Scan(&captures); err != nil {
				t.Fatal(err)
			}
			if err := first.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM payment_operations WHERE invoice_id=?`, purchase.InvoiceID).Scan(&operations); err != nil {
				t.Fatal(err)
			}
			if err := first.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id IN (?,?)`, replacement.ID, dispatch.ID).Scan(&receipts); err != nil {
				t.Fatal(err)
			}
			if replacement.Status == "succeeded" {
				if dispatch.Status != "failed" || dispatch.ErrorCode != "PREVIEW_STALE" || captures != 0 || operations != 2 || receipts != 1 {
					t.Fatalf("replacement first: replacement=%+v dispatch=%+v captures=%d operations=%d receipts=%d", replacement, dispatch, captures, operations, receipts)
				}
			} else if dispatch.Status == "succeeded" {
				if replacement.Status != "failed" || replacement.ErrorCode != "PREVIEW_STALE" || captures != 1 || operations != 1 || receipts != 1 {
					t.Fatalf("dispatch first: replacement=%+v dispatch=%+v captures=%d operations=%d receipts=%d", replacement, dispatch, captures, operations, receipts)
				}
			} else {
				t.Fatalf("neither command succeeded: replacement=%+v dispatch=%+v", replacement, dispatch)
			}
		})
	}
}
