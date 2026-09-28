package lab

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestAdminCompetingPaymentCommandsAcrossLabInstances(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	dir := t.TempDir()
	commercePath := filepath.Join(dir, "commerce.db")
	providerPath := filepath.Join(dir, "provider.db")
	first, err := Open(commercePath, providerPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Close() })
	if err := first.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	purchase := purchaseHundred(t, first, "cross-instance-payment", "cross-instance-checkout")
	create := func(key, amount string) AdminCommand {
		t.Helper()
		payload := json.RawMessage(`{"amount_minor":"` + amount + `"}`)
		preview, err := first.AdminCreatePreview(ctx, "local-admin", "C07", purchase.InvoiceID, payload)
		if err != nil {
			t.Fatal(err)
		}
		command, _, err := first.AdminSubmitCommand(ctx, "local-admin", key, "C07", purchase.InvoiceID, payload, preview.ID)
		if err != nil {
			t.Fatal(err)
		}
		return command
	}
	a := create("cross-instance-six", "6000")
	b := create("cross-instance-seven", "7000")
	second, err := Open(commercePath, providerPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	if err := second.InitAdmin(ctx); err != nil {
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
	}{{first, a}, {second, b}} {
		go func(l *Lab, command AdminCommand) {
			ready <- struct{}{}
			<-start
			settled, err := l.AdminExecuteCommand(ctx, command.ID)
			results <- result{command.ID, settled, err}
		}(item.lab, item.command)
	}
	<-ready
	<-ready
	close(start)
	var executionErr error
	var failedID string
	for range 2 {
		select {
		case outcome := <-results:
			if outcome.err != nil {
				executionErr = outcome.err
				failedID = outcome.id
			}
		case <-ctx.Done():
			t.Fatalf("concurrent commands timed out: %v", ctx.Err())
		}
	}
	if executionErr != nil {
		var aStatus, bStatus, aLease, bLease string
		_ = first.db.QueryRowContext(ctx, `SELECT status,COALESCE(lease_until,'') FROM admin_commands WHERE id=?`, a.ID).Scan(&aStatus, &aLease)
		_ = first.db.QueryRowContext(ctx, `SELECT status,COALESCE(lease_until,'') FROM admin_commands WHERE id=?`, b.ID).Scan(&bStatus, &bLease)
		t.Fatalf("concurrent command %s: %v; a=%s lease=%s b=%s lease=%s", failedID, executionErr, aStatus, aLease, bStatus, bLease)
	}
	var succeeded, stale, receipts, operations, created, oldCancelled int
	if err := first.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_commands WHERE id IN (?,?) AND status='succeeded'`, a.ID, b.ID).Scan(&succeeded); err != nil {
		t.Fatal(err)
	}
	if err := first.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_commands WHERE id IN (?,?) AND status='failed' AND error_code='PREVIEW_STALE'`, a.ID, b.ID).Scan(&stale); err != nil {
		t.Fatal(err)
	}
	if err := first.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id IN (?,?)`, a.ID, b.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if err := first.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM payment_operations WHERE invoice_id=?`, purchase.InvoiceID).Scan(&operations); err != nil {
		t.Fatal(err)
	}
	if err := first.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM payment_operations WHERE invoice_id=? AND status='created' AND amount_minor IN (6000,7000)`, purchase.InvoiceID).Scan(&created); err != nil {
		t.Fatal(err)
	}
	if err := first.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM payment_operations WHERE id=? AND status='cancelled'`, purchase.OperationID).Scan(&oldCancelled); err != nil {
		t.Fatal(err)
	}
	if succeeded != 1 || stale != 1 || receipts != 1 || operations != 2 || created != 1 || oldCancelled != 1 {
		t.Fatalf("cross-instance outcome succeeded=%d stale=%d receipts=%d operations=%d created=%d oldCancelled=%d", succeeded, stale, receipts, operations, created, oldCancelled)
	}
	if captures := captureCount(t, first); captures != 0 {
		t.Fatalf("C07 dispatched provider unexpectedly: %d", captures)
	}
}

func TestAdminCompetingRetryCommandsAcrossLabInstances(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	dir := t.TempDir()
	commercePath := filepath.Join(dir, "commerce.db")
	providerPath := filepath.Join(dir, "provider.db")
	first, err := Open(commercePath, providerPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Close() })
	if err := first.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	purchase := purchaseHundred(t, first, "cross-instance-retry", "cross-instance-retry-checkout")
	if err := first.SetFakePaymentDecision(ctx, purchase.OperationID, "definitively_failed"); err != nil {
		t.Fatal(err)
	}
	if _, err := first.DispatchNext(ctx, ""); err != nil {
		t.Fatal(err)
	}
	create := func(key string) AdminCommand {
		t.Helper()
		preview, err := first.AdminCreatePreview(ctx, "local-admin", "C08", purchase.OperationID, json.RawMessage(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		command, _, err := first.AdminSubmitCommand(ctx, "local-admin", key, "C08", purchase.OperationID, json.RawMessage(`{}`), preview.ID)
		if err != nil {
			t.Fatal(err)
		}
		return command
	}
	a := create("cross-instance-retry-a")
	b := create("cross-instance-retry-b")
	second, err := Open(commercePath, providerPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	if err := second.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	type result struct {
		id  string
		err error
	}
	start := make(chan struct{})
	ready := make(chan struct{}, 2)
	results := make(chan result, 2)
	for _, item := range []struct {
		lab     *Lab
		command AdminCommand
	}{{first, a}, {second, b}} {
		go func(l *Lab, command AdminCommand) {
			ready <- struct{}{}
			<-start
			_, err := l.AdminExecuteCommand(ctx, command.ID)
			results <- result{command.ID, err}
		}(item.lab, item.command)
	}
	<-ready
	<-ready
	close(start)
	for range 2 {
		select {
		case outcome := <-results:
			if outcome.err != nil {
				t.Fatalf("concurrent retry command %s: %v", outcome.id, outcome.err)
			}
		case <-ctx.Done():
			t.Fatalf("concurrent retry commands timed out: %v", ctx.Err())
		}
	}
	var succeeded, stale, receipts, retries, operations int
	if err := first.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_commands WHERE id IN (?,?) AND status='succeeded'`, a.ID, b.ID).Scan(&succeeded); err != nil {
		t.Fatal(err)
	}
	if err := first.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_commands WHERE id IN (?,?) AND status='failed' AND error_code='PREVIEW_STALE'`, a.ID, b.ID).Scan(&stale); err != nil {
		t.Fatal(err)
	}
	if err := first.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id IN (?,?)`, a.ID, b.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if err := first.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM payment_retry_requests WHERE invoice_id=?`, purchase.InvoiceID).Scan(&retries); err != nil {
		t.Fatal(err)
	}
	if err := first.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM payment_operations WHERE invoice_id=?`, purchase.InvoiceID).Scan(&operations); err != nil {
		t.Fatal(err)
	}
	if succeeded != 1 || stale != 1 || receipts != 1 || retries != 1 || operations != 2 {
		t.Fatalf("cross-instance retry outcome succeeded=%d stale=%d receipts=%d retries=%d operations=%d", succeeded, stale, receipts, retries, operations)
	}
	var newProviderKey string
	if err := first.db.QueryRowContext(ctx, `SELECT provider_key FROM payment_operations WHERE invoice_id=? AND status='created'`, purchase.InvoiceID).Scan(&newProviderKey); err != nil {
		t.Fatal(err)
	}
	var providerRows int
	if err := first.provider.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM captures WHERE provider_key=?`, newProviderKey).Scan(&providerRows); err != nil || providerRows != 0 {
		t.Fatalf("new retry key dispatched=%d err=%v", providerRows, err)
	}
}
