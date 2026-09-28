package lab

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestAdminCompetingRefundDispatchesAcrossProcesses(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	dir := t.TempDir()
	commercePath := filepath.Join(dir, "commerce.db")
	providerPath := filepath.Join(dir, "provider.db")
	l, err := Open(commercePath, providerPath, func() time.Time { return fixedNow })
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	paid := paidBasicSubscription(t, l, "cross-process-refund-dispatch")
	correction, err := l.PostReduction(ctx, paid.InvoiceID, 1000, "service credit", "cross-process-refund-reduction")
	if err != nil || len(correction.GrantIDs) != 1 {
		t.Fatalf("funded credit grant: %+v err=%v", correction, err)
	}
	refundID, err := l.ReserveRefund(ctx, correction.GrantIDs[0], 400, "cross-process-refund-reservation")
	if err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{}`)
	create := func(key string) AdminCommand {
		t.Helper()
		preview, err := l.AdminCreatePreview(ctx, "local-admin", "C16", refundID, payload)
		if err != nil {
			t.Fatal(err)
		}
		command, _, err := l.AdminSubmitCommand(ctx, "local-admin", key, "C16", refundID, payload, preview.ID)
		if err != nil {
			t.Fatal(err)
		}
		return command
	}
	a := create("cross-process-refund-dispatch-a")
	b := create("cross-process-refund-dispatch-b")
	executeAdminCommandsAcrossProcesses(t, ctx, commercePath, providerPath, fixedNow, a.ID, b.ID)

	var succeeded, stale, receipts, providerRefunds int
	for _, check := range []struct {
		query string
		args  []any
		out   *int
	}{
		{`SELECT COUNT(*) FROM admin_commands WHERE id IN (?,?) AND status='succeeded'`, []any{a.ID, b.ID}, &succeeded},
		{`SELECT COUNT(*) FROM admin_commands WHERE id IN (?,?) AND status='failed' AND error_code='PREVIEW_STALE'`, []any{a.ID, b.ID}, &stale},
		{`SELECT COUNT(*) FROM admin_command_receipts WHERE command_id IN (?,?)`, []any{a.ID, b.ID}, &receipts},
	} {
		if err := l.db.QueryRowContext(ctx, check.query, check.args...).Scan(check.out); err != nil {
			t.Fatal(err)
		}
	}
	if err := l.provider.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM refunds`).Scan(&providerRefunds); err != nil {
		t.Fatal(err)
	}
	refund, err := l.Refund(ctx, refundID)
	if err != nil {
		t.Fatal(err)
	}
	if succeeded != 1 || stale != 1 || receipts != 1 || providerRefunds != 1 || refund.Status != "succeeded" {
		t.Fatalf("cross-process refund dispatch: succeeded=%d stale=%d receipts=%d provider_refunds=%d refund=%+v", succeeded, stale, receipts, providerRefunds, refund)
	}
	for _, attempt := range []struct{ key, id string }{
		{key: "cross-process-refund-dispatch-a", id: a.ID},
		{key: "cross-process-refund-dispatch-b", id: b.ID},
	} {
		var previewID string
		if err := l.db.QueryRowContext(ctx, `SELECT preview_id FROM admin_commands WHERE id=?`, attempt.id).Scan(&previewID); err != nil {
			t.Fatal(err)
		}
		replayed, replay, err := l.AdminSubmitCommand(ctx, "local-admin", attempt.key, "C16", refundID, payload, previewID)
		if err != nil || !replay || replayed.ID != attempt.id {
			t.Fatalf("cross-process dispatch replay changed command: %+v replay=%t err=%v", replayed, replay, err)
		}
	}
}

func TestAdminCompetingPaymentDispatchesAcrossProcesses(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	dir := t.TempDir()
	commercePath := filepath.Join(dir, "commerce.db")
	providerPath := filepath.Join(dir, "provider.db")
	l, err := Open(commercePath, providerPath, func() time.Time { return fixedNow })
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	quote, err := l.CreateQuote(ctx, "cross-process-payment-dispatch", "basic")
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "cross-process-payment-checkout")
	if err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{}`)
	create := func(key string) AdminCommand {
		t.Helper()
		preview, err := l.AdminCreatePreview(ctx, "local-admin", "C09", accepted.OperationID, payload)
		if err != nil {
			t.Fatal(err)
		}
		command, _, err := l.AdminSubmitCommand(ctx, "local-admin", key, "C09", accepted.OperationID, payload, preview.ID)
		if err != nil {
			t.Fatal(err)
		}
		return command
	}
	a := create("cross-process-payment-dispatch-a")
	b := create("cross-process-payment-dispatch-b")
	executeAdminCommandsAcrossProcesses(t, ctx, commercePath, providerPath, fixedNow, a.ID, b.ID)

	var succeeded, stale, receipts, allocations, providerCaptures int
	for _, check := range []struct {
		query string
		args  []any
		out   *int
	}{
		{`SELECT COUNT(*) FROM admin_commands WHERE id IN (?,?) AND status='succeeded'`, []any{a.ID, b.ID}, &succeeded},
		{`SELECT COUNT(*) FROM admin_commands WHERE id IN (?,?) AND status='failed' AND error_code='PREVIEW_STALE'`, []any{a.ID, b.ID}, &stale},
		{`SELECT COUNT(*) FROM admin_command_receipts WHERE command_id IN (?,?)`, []any{a.ID, b.ID}, &receipts},
		{`SELECT COUNT(*) FROM allocations WHERE operation_id=?`, []any{accepted.OperationID}, &allocations},
	} {
		if err := l.db.QueryRowContext(ctx, check.query, check.args...).Scan(check.out); err != nil {
			t.Fatal(err)
		}
	}
	if err := l.provider.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM captures`).Scan(&providerCaptures); err != nil {
		t.Fatal(err)
	}
	if succeeded != 1 || stale != 1 || receipts != 1 || allocations != 1 || providerCaptures != 1 {
		t.Fatalf("cross-process payment dispatch: succeeded=%d stale=%d receipts=%d allocations=%d provider_captures=%d", succeeded, stale, receipts, allocations, providerCaptures)
	}
	for _, attempt := range []struct{ key, id string }{
		{key: "cross-process-payment-dispatch-a", id: a.ID},
		{key: "cross-process-payment-dispatch-b", id: b.ID},
	} {
		var previewID string
		if err := l.db.QueryRowContext(ctx, `SELECT preview_id FROM admin_commands WHERE id=?`, attempt.id).Scan(&previewID); err != nil {
			t.Fatal(err)
		}
		replayed, replay, err := l.AdminSubmitCommand(ctx, "local-admin", attempt.key, "C09", accepted.OperationID, payload, previewID)
		if err != nil || !replay || replayed.ID != attempt.id {
			t.Fatalf("cross-process dispatch replay changed command: %+v replay=%t err=%v", replayed, replay, err)
		}
	}
}

func TestAdminUnstartedDispatchClaimIsReleasedOnRevocation(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	l, err := Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), func() time.Time { return fixedNow })
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	quote, err := l.CreateQuote(ctx, "revoked-dispatch-claim", "basic")
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "revoked-dispatch-checkout")
	if err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{}`)
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C09", accepted.OperationID, payload)
	if err != nil {
		t.Fatal(err)
	}
	first, _, err := l.AdminSubmitCommand(ctx, "local-admin", "revoked-dispatch-first", "C09", accepted.OperationID, payload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	owned, err := l.adminClaimExternalDispatch(ctx, first.ID, "C09", accepted.OperationID)
	if err != nil || !owned {
		t.Fatalf("claim before provider call: owned=%t err=%v", owned, err)
	}
	revoked, err := l.AdminRevokeUnstarted(ctx, first.ID)
	if err != nil || !revoked {
		t.Fatalf("revoke before provider call: revoked=%t err=%v", revoked, err)
	}
	owner, err := l.adminExternalDispatchOwner(ctx, "C09", accepted.OperationID)
	if err != nil || owner != "" {
		t.Fatalf("unstarted claim retained after revocation: owner=%q err=%v", owner, err)
	}
	preview, err = l.AdminCreatePreview(ctx, "local-admin", "C09", accepted.OperationID, payload)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := l.AdminSubmitCommand(ctx, "local-admin", "revoked-dispatch-second", "C09", accepted.OperationID, payload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	second, err = l.AdminExecuteCommand(ctx, second.ID)
	if err != nil || second.Status != "succeeded" {
		t.Fatalf("new command after revoked unstarted claim: %+v err=%v", second, err)
	}
	var providerCaptures int
	if err := l.provider.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM captures`).Scan(&providerCaptures); err != nil {
		t.Fatal(err)
	}
	if providerCaptures != 1 {
		t.Fatalf("provider captures after revocation=%d", providerCaptures)
	}
}

func TestAdminUnstartedRefundDispatchClaimIsReleasedOnRevocation(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	l, err := Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), func() time.Time { return fixedNow })
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	paid := paidBasicSubscription(t, l, "revoked-refund-dispatch-claim")
	correction, err := l.PostReduction(ctx, paid.InvoiceID, 1000, "service credit", "revoked-refund-claim-reduction")
	if err != nil || len(correction.GrantIDs) != 1 {
		t.Fatalf("funded credit grant: %+v err=%v", correction, err)
	}
	refundID, err := l.ReserveRefund(ctx, correction.GrantIDs[0], 400, "revoked-refund-claim-reservation")
	if err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{}`)
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C16", refundID, payload)
	if err != nil {
		t.Fatal(err)
	}
	first, _, err := l.AdminSubmitCommand(ctx, "local-admin", "revoked-refund-dispatch-first", "C16", refundID, payload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	owned, err := l.adminClaimExternalDispatch(ctx, first.ID, "C16", refundID)
	if err != nil || !owned {
		t.Fatalf("claim before provider call: owned=%t err=%v", owned, err)
	}
	revoked, err := l.AdminRevokeUnstarted(ctx, first.ID)
	if err != nil || !revoked {
		t.Fatalf("revoke before provider call: revoked=%t err=%v", revoked, err)
	}
	owner, err := l.adminExternalDispatchOwner(ctx, "C16", refundID)
	if err != nil || owner != "" {
		t.Fatalf("unstarted refund claim retained after revocation: owner=%q err=%v", owner, err)
	}
	preview, err = l.AdminCreatePreview(ctx, "local-admin", "C16", refundID, payload)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := l.AdminSubmitCommand(ctx, "local-admin", "revoked-refund-dispatch-second", "C16", refundID, payload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	second, err = l.AdminExecuteCommand(ctx, second.ID)
	if err != nil || second.Status != "succeeded" {
		t.Fatalf("new refund command after revoked unstarted claim: %+v err=%v", second, err)
	}
	var providerRefunds int
	if err := l.provider.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM refunds`).Scan(&providerRefunds); err != nil {
		t.Fatal(err)
	}
	if providerRefunds != 1 {
		t.Fatalf("provider refunds after revocation=%d", providerRefunds)
	}
}
