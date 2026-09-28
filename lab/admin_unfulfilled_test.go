package lab

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestAdminResolveUnfulfilledChangeCommitsCorrectionAndResolution(t *testing.T) {
	l, clock := openChangingLab(t)
	ctx := context.Background()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	paid := paidBasicSubscription(t, l, "admin-unfulfilled-customer")
	*clock = time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)
	change, err := l.RequestImmediateProUpgrade(ctx, paid.SubscriptionID, 5, 1, "admin-unfulfilled-upgrade")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.DispatchNext(ctx, "lost_response"); !errors.Is(err, ErrPaymentUnknown) {
		t.Fatalf("expected unknown payment: %v", err)
	}
	*clock = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if _, err := l.RunRenewals(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := l.ReconcilePayment(ctx, change.OperationID); err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{}`)
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C14", change.ID, payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := l.AdminSubmitCommand(ctx, "other-admin", "foreign-c14-001", "C14", change.ID, payload, preview.ID); !errors.Is(err, ErrAdminPreviewStale) {
		t.Fatalf("other actor used C14 preview: %v", err)
	}
	var foreignCommands int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_commands WHERE actor_id='other-admin' AND action_id='C14'`).Scan(&foreignCommands); err != nil {
		t.Fatal(err)
	}
	if foreignCommands != 0 {
		t.Fatalf("other actor wrote %d C14 commands", foreignCommands)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "resolve-unfulfilled-001", "C14", change.ID, payload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	command, err = l.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "succeeded" {
		t.Fatalf("execute: %+v %v", command, err)
	}
	var refs struct {
		CorrectionID string `json:"correction_id"`
	}
	if err := json.Unmarshal(command.ResultRefs, &refs); err != nil {
		t.Fatal(err)
	}
	if refs.CorrectionID == "" {
		t.Fatal("missing correction")
	}
	var resolutions, receipts int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM immediate_change_resolutions WHERE change_id=? AND correction_id=?`, change.ID, refs.CorrectionID).Scan(&resolutions); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if resolutions != 1 || receipts != 1 {
		t.Fatalf("resolutions=%d receipts=%d", resolutions, receipts)
	}
	detail, err := l.AdminInvoiceDetail(ctx, change.InvoiceID)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Corrections) != 1 || detail.Corrections[0].OriginKind != "unfulfilled_change" || detail.Corrections[0].OriginID != change.ID {
		t.Fatalf("unfulfilled change origin: %+v", detail.Corrections)
	}
	replayed, replay, err := l.AdminSubmitCommand(ctx, "local-admin", "resolve-unfulfilled-001", "C14", change.ID, payload, preview.ID)
	if err != nil || !replay || replayed.ID != command.ID {
		t.Fatalf("resolution replay: %+v replay=%v err=%v", replayed, replay, err)
	}
	replayed, err = l.AdminExecuteCommand(ctx, replayed.ID)
	if err != nil || replayed.Status != "succeeded" {
		t.Fatalf("execute replay: %+v %v", replayed, err)
	}
	var corrections, grants int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM corrections WHERE invoice_id=?`, change.InvoiceID).Scan(&corrections); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM credit_grants WHERE source_invoice_id=?`, change.InvoiceID).Scan(&grants); err != nil {
		t.Fatal(err)
	}
	if corrections != 1 || grants != 1 {
		t.Fatalf("resolution replay duplicated financial facts: corrections=%d grants=%d", corrections, grants)
	}
}

func TestAdminResolveUnfulfilledRollsBackFinancialFactsWhenReceiptFails(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	commercePath := filepath.Join(dir, "commerce.db")
	providerPath := filepath.Join(dir, "provider.db")
	now := fixedNow
	l, err := Open(commercePath, providerPath, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}

	paid := paidBasicSubscription(t, l, "unfulfilled-receipt-failure")
	now = time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)
	change, err := l.RequestImmediateProUpgrade(ctx, paid.SubscriptionID, 5, 1, "unfulfilled-receipt-failure-upgrade")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.DispatchNext(ctx, "lost_response"); !errors.Is(err, ErrPaymentUnknown) {
		t.Fatalf("expected unknown upgrade payment: %v", err)
	}
	now = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if _, err := l.RunRenewals(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := l.ReconcilePayment(ctx, change.OperationID); err != nil {
		t.Fatal(err)
	}
	providerCaptures := captureCount(t, l)
	payload := json.RawMessage(`{}`)
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C14", change.ID, payload)
	if err != nil {
		t.Fatal(err)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "unfulfilled-receipt-failure-command", "C14", change.ID, payload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertFacts := func(want int) {
		t.Helper()
		checks := []struct {
			name  string
			query string
			arg   string
		}{
			{"corrections", `SELECT COUNT(*) FROM corrections WHERE invoice_id=?`, change.InvoiceID},
			{"credit_grants", `SELECT COUNT(*) FROM credit_grants WHERE source_invoice_id=?`, change.InvoiceID},
			{"resolutions", `SELECT COUNT(*) FROM immediate_change_resolutions WHERE change_id=?`, change.ID},
			{"receipts", `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID},
		}
		for _, check := range checks {
			var got int
			if err := l.db.QueryRowContext(ctx, check.query, check.arg).Scan(&got); err != nil || got != want {
				t.Fatalf("%s: got=%d want=%d err=%v", check.name, got, want, err)
			}
		}
		if got := captureCount(t, l); got != providerCaptures {
			t.Fatalf("resolution called provider: captures=%d want=%d", got, providerCaptures)
		}
	}

	if _, err := l.db.ExecContext(ctx, `CREATE TEMP TRIGGER fail_unfulfilled_receipt BEFORE INSERT ON admin_command_receipts BEGIN SELECT RAISE(ABORT, 'injected receipt failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := l.AdminExecuteCommand(ctx, command.ID); err == nil {
		t.Fatal("receipt failure must abort unfulfilled resolution")
	}
	assertFacts(0)
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	l, err = Open(commercePath, providerPath, func() time.Time { return now })
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
	recovered, err := l.AdminCommand(ctx, command.ID)
	if err != nil || recovered.Status != "succeeded" {
		t.Fatalf("resolution did not recover: %+v err=%v", recovered, err)
	}
	replayed, replay, err := l.AdminSubmitCommand(ctx, "local-admin", "unfulfilled-receipt-failure-command", "C14", change.ID, payload, preview.ID)
	if err != nil || !replay || replayed.ID != command.ID {
		t.Fatalf("replay changed command: %+v replay=%v err=%v", replayed, replay, err)
	}
	assertFacts(1)
}
