package lab

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestAdminCreditAndRefundCompeteAcrossProcesses(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	dir := t.TempDir()
	commercePath := filepath.Join(dir, "commerce.db")
	providerPath := filepath.Join(dir, "provider.db")
	clock := fixedNow
	l, err := Open(commercePath, providerPath, func() time.Time { return clock })
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	paid := paidBasicSubscription(t, l, "cross-process-credit-customer")
	correction, err := l.PostReduction(ctx, paid.InvoiceID, 1000, "service credit", "cross-process-credit-reduction")
	if err != nil || len(correction.GrantIDs) != 1 {
		t.Fatalf("funded credit grant: %+v err=%v", correction, err)
	}
	grantID := correction.GrantIDs[0]
	clock = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	renewals, err := l.RunRenewals(ctx)
	if err != nil || len(renewals) != 1 {
		t.Fatalf("renewal invoice: %+v err=%v", renewals, err)
	}
	invoiceID := renewals[0].InvoiceID
	applyPayload, err := json.Marshal(AdminApplyCreditPayload{InvoiceID: invoiceID, AmountMinor: "700"})
	if err != nil {
		t.Fatal(err)
	}
	reservePayload := json.RawMessage(`{"amount_minor":"700"}`)
	applyPreview, err := l.AdminCreatePreview(ctx, "local-admin", "C12", grantID, applyPayload)
	if err != nil {
		t.Fatal(err)
	}
	reservePreview, err := l.AdminCreatePreview(ctx, "local-admin", "C15", grantID, reservePayload)
	if err != nil {
		t.Fatal(err)
	}
	const applyKey = "cross-process-credit-apply"
	const reserveKey = "cross-process-credit-refund"
	apply, _, err := l.AdminSubmitCommand(ctx, "local-admin", applyKey, "C12", grantID, applyPayload, applyPreview.ID)
	if err != nil {
		t.Fatal(err)
	}
	reserve, _, err := l.AdminSubmitCommand(ctx, "local-admin", reserveKey, "C15", grantID, reservePayload, reservePreview.ID)
	if err != nil {
		t.Fatal(err)
	}

	type worker struct {
		cmd       *exec.Cmd
		output    bytes.Buffer
		readyPath string
	}
	gatePath := filepath.Join(dir, "start")
	workers := []*worker{
		{readyPath: filepath.Join(dir, "ready-apply")},
		{readyPath: filepath.Join(dir, "ready-refund")},
	}
	for i, command := range []AdminCommand{apply, reserve} {
		worker := workers[i]
		worker.cmd = exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAdminCrossProcessPaymentWorker$")
		worker.cmd.Env = append(os.Environ(),
			"BILLFORGE_TEST_ADMIN_WORKER=1",
			"BILLFORGE_TEST_COMMERCE_PATH="+commercePath,
			"BILLFORGE_TEST_PROVIDER_PATH="+providerPath,
			"BILLFORGE_TEST_COMMAND_ID="+command.ID,
			"BILLFORGE_TEST_READY_PATH="+worker.readyPath,
			"BILLFORGE_TEST_GATE_PATH="+gatePath,
			"BILLFORGE_TEST_BUSINESS_TIME="+clock.Format(time.RFC3339Nano),
		)
		worker.cmd.Stdout = &worker.output
		worker.cmd.Stderr = &worker.output
		if err := worker.cmd.Start(); err != nil {
			t.Fatal(err)
		}
	}
	for {
		allReady := true
		for _, worker := range workers {
			if _, err := os.Stat(worker.readyPath); os.IsNotExist(err) {
				allReady = false
			} else if err != nil {
				t.Fatal(err)
			}
		}
		if allReady {
			break
		}
		if ctx.Err() != nil {
			t.Fatalf("workers did not become ready: %v", ctx.Err())
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := os.WriteFile(gatePath, []byte("go"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, worker := range workers {
		if err := worker.cmd.Wait(); err != nil {
			t.Fatalf("worker failed: %v\n%s", err, worker.output.String())
		}
	}

	var succeeded, stale, receipts, applications, refunds, providerRefunds int
	for _, check := range []struct {
		query string
		args  []any
		out   *int
	}{
		{`SELECT COUNT(*) FROM admin_commands WHERE id IN (?,?) AND status='succeeded'`, []any{apply.ID, reserve.ID}, &succeeded},
		{`SELECT COUNT(*) FROM admin_commands WHERE id IN (?,?) AND status='failed' AND error_code='PREVIEW_STALE'`, []any{apply.ID, reserve.ID}, &stale},
		{`SELECT COUNT(*) FROM admin_command_receipts WHERE command_id IN (?,?)`, []any{apply.ID, reserve.ID}, &receipts},
		{`SELECT COUNT(*) FROM credit_applications WHERE grant_id=?`, []any{grantID}, &applications},
		{`SELECT COUNT(*) FROM refund_operations WHERE grant_id=?`, []any{grantID}, &refunds},
	} {
		if err := l.db.QueryRowContext(ctx, check.query, check.args...).Scan(check.out); err != nil {
			t.Fatal(err)
		}
	}
	if err := l.provider.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM refunds`).Scan(&providerRefunds); err != nil {
		t.Fatal(err)
	}
	credit, err := l.CreditBalance(ctx, grantID)
	if err != nil {
		t.Fatal(err)
	}
	invoice, err := l.Balance(ctx, invoiceID)
	if err != nil {
		t.Fatal(err)
	}
	if succeeded != 1 || stale != 1 || receipts != 1 || applications+refunds != 1 || providerRefunds != 0 || credit.GrantedMinor != 1000 || credit.AppliedMinor+credit.ReservedMinor != 700 || credit.AvailableMinor != 300 || invoice.CreditAppliedMinor != credit.AppliedMinor || invoice.OutstandingMinor != 2000-credit.AppliedMinor {
		t.Fatalf("cross-process credit state: succeeded=%d stale=%d receipts=%d applications=%d refunds=%d provider_refunds=%d credit=%+v invoice=%+v", succeeded, stale, receipts, applications, refunds, providerRefunds, credit, invoice)
	}
	for _, item := range []struct {
		key       string
		action    string
		payload   json.RawMessage
		previewID string
		commandID string
	}{
		{applyKey, "C12", applyPayload, applyPreview.ID, apply.ID},
		{reserveKey, "C15", reservePayload, reservePreview.ID, reserve.ID},
	} {
		replayed, replay, err := l.AdminSubmitCommand(ctx, "local-admin", item.key, item.action, grantID, item.payload, item.previewID)
		if err != nil || !replay || replayed.ID != item.commandID {
			t.Fatalf("cross-process original key changed: action=%s command=%+v replay=%v err=%v", item.action, replayed, replay, err)
		}
	}
}
