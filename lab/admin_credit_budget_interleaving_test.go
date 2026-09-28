package lab

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestAdminCreditApplicationAndRefundReservationCompeteAcrossInstances(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	clock := fixedNow
	dir := t.TempDir()
	commercePath := filepath.Join(dir, "commerce.db")
	providerPath := filepath.Join(dir, "provider.db")
	first, err := Open(commercePath, providerPath, func() time.Time { return clock })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Close() })
	if err := first.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	paid := paidBasicSubscription(t, first, "admin-credit-budget-race")
	correction, err := first.PostReduction(ctx, paid.InvoiceID, 1000, "service credit", "admin-credit-budget-race-reduction")
	if err != nil || len(correction.GrantIDs) != 1 {
		t.Fatalf("correction: %+v err=%v", correction, err)
	}
	grantID := correction.GrantIDs[0]
	clock = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	renewals, err := first.RunRenewals(ctx)
	if err != nil || len(renewals) != 1 {
		t.Fatalf("renewals: %+v err=%v", renewals, err)
	}
	invoiceID := renewals[0].InvoiceID
	second, err := Open(commercePath, providerPath, func() time.Time { return clock })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	if err := second.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}

	applyPayload, err := json.Marshal(AdminApplyCreditPayload{InvoiceID: invoiceID, AmountMinor: "700"})
	if err != nil {
		t.Fatal(err)
	}
	reservePayload := json.RawMessage(`{"amount_minor":"700"}`)
	applyPreview, err := first.AdminCreatePreview(ctx, "local-admin", "C12", grantID, applyPayload)
	if err != nil {
		t.Fatal(err)
	}
	reservePreview, err := second.AdminCreatePreview(ctx, "local-admin", "C15", grantID, reservePayload)
	if err != nil {
		t.Fatal(err)
	}
	apply, _, err := first.AdminSubmitCommand(ctx, "local-admin", "admin-credit-budget-race-apply", "C12", grantID, applyPayload, applyPreview.ID)
	if err != nil {
		t.Fatal(err)
	}
	reserve, _, err := second.AdminSubmitCommand(ctx, "local-admin", "admin-credit-budget-race-reserve", "C15", grantID, reservePayload, reservePreview.ID)
	if err != nil {
		t.Fatal(err)
	}

	type result struct {
		command AdminCommand
		err     error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	for _, item := range []struct {
		lab     *Lab
		command AdminCommand
	}{{first, apply}, {second, reserve}} {
		go func(l *Lab, command AdminCommand) {
			ready.Done()
			<-start
			updated, err := l.AdminExecuteCommand(ctx, command.ID)
			results <- result{updated, err}
		}(item.lab, item.command)
	}
	ready.Wait()
	close(start)
	var succeeded, stale int
	for range 2 {
		select {
		case outcome := <-results:
			if outcome.err != nil {
				t.Fatalf("execute %s: %v", outcome.command.ID, outcome.err)
			}
			switch {
			case outcome.command.Status == "succeeded":
				succeeded++
			case outcome.command.Status == "failed" && outcome.command.ErrorCode == "PREVIEW_STALE":
				stale++
			default:
				t.Fatalf("unexpected competing command: %+v", outcome.command)
			}
		case <-ctx.Done():
			t.Fatalf("competing commands timed out: %v", ctx.Err())
		}
	}
	if succeeded != 1 || stale != 1 {
		t.Fatalf("competing results: succeeded=%d stale=%d", succeeded, stale)
	}
	credit, err := first.CreditBalance(ctx, grantID)
	if err != nil {
		t.Fatal(err)
	}
	if credit.GrantedMinor != 1000 || credit.AppliedMinor+credit.ReservedMinor != 700 || credit.AvailableMinor != 300 || credit.RefundedMinor != 0 {
		t.Fatalf("credit overcommitted or lost: %+v", credit)
	}
	invoice, err := first.Balance(ctx, invoiceID)
	if err != nil {
		t.Fatal(err)
	}
	if invoice.CreditAppliedMinor != credit.AppliedMinor || invoice.OutstandingMinor != 2000-credit.AppliedMinor {
		t.Fatalf("invoice and credit disagree: invoice=%+v credit=%+v", invoice, credit)
	}
	var receipts, applications, refunds, providerRefunds int
	for _, check := range []struct {
		query string
		args  []any
		out   *int
	}{
		{`SELECT COUNT(*) FROM admin_command_receipts WHERE command_id IN (?,?)`, []any{apply.ID, reserve.ID}, &receipts},
		{`SELECT COUNT(*) FROM credit_applications WHERE grant_id=?`, []any{grantID}, &applications},
		{`SELECT COUNT(*) FROM refund_operations WHERE grant_id=?`, []any{grantID}, &refunds},
	} {
		if err := first.db.QueryRowContext(ctx, check.query, check.args...).Scan(check.out); err != nil {
			t.Fatal(err)
		}
	}
	if err := first.provider.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM refunds`).Scan(&providerRefunds); err != nil {
		t.Fatal(err)
	}
	if receipts != 1 || applications+refunds != 1 || providerRefunds != 0 {
		t.Fatalf("competing facts: receipts=%d applications=%d refunds=%d provider_refunds=%d", receipts, applications, refunds, providerRefunds)
	}
}
