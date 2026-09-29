package lab

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestAdminCapabilityRevocationPreservesExternalObligations(t *testing.T) {
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

	local, _, err := l.AdminSubmitCommand(ctx, "local-admin", "revoke-local-001", "C01", "", json.RawMessage(`{"customer_id":"revoke-local","plan_id":"basic","seats":"0"}`), "")
	if err != nil {
		t.Fatal(err)
	}
	newDispatch := func(customer, key string) (AdminCommand, string) {
		t.Helper()
		quote, err := l.CreateQuote(ctx, customer, "basic")
		if err != nil {
			t.Fatal(err)
		}
		accepted, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, key+"-checkout")
		if err != nil {
			t.Fatal(err)
		}
		payload := json.RawMessage(`{}`)
		preview, err := l.AdminCreatePreview(ctx, "local-admin", "C09", accepted.OperationID, payload)
		if err != nil {
			t.Fatal(err)
		}
		command, _, err := l.AdminSubmitCommand(ctx, "local-admin", key, "C09", accepted.OperationID, payload, preview.ID)
		if err != nil {
			t.Fatal(err)
		}
		return command, accepted.OperationID
	}
	unstarted, unstartedOperation := newDispatch("revoke-unstarted", "revoke-dispatch-001")
	unknown, unknownOperation := newDispatch("revoke-unknown", "revoke-dispatch-002")
	inconsistent, inconsistentOperation := newDispatch("revoke-inconsistent", "revoke-dispatch-003")
	if _, err := l.db.ExecContext(ctx, `UPDATE admin_commands SET status='waiting_verification' WHERE id=?`, inconsistent.ID); err != nil {
		t.Fatal(err)
	}
	controlPayload := json.RawMessage(`{"status":"definitively_failed"}`)
	controlPreview, err := l.AdminCreatePreview(ctx, "local-admin", "C47", unstartedOperation, controlPayload)
	if err != nil {
		t.Fatal(err)
	}
	control, _, err := l.AdminSubmitCommand(ctx, "local-admin", "revoke-control-001", "C47", unstartedOperation, controlPayload, controlPreview.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.db.ExecContext(ctx, `UPDATE payment_operations SET status='submitted' WHERE id=? AND status='created'`, unknownOperation); err != nil {
		t.Fatal(err)
	}

	if err := l.AdminResumeWithPolicy(ctx, func(string) bool { return false }); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{local.ID, unstarted.ID} {
		command, err := l.AdminCommand(ctx, id)
		if err != nil || command.Status != "failed" || command.ErrorCode != "PERMISSION_REVOKED" {
			t.Fatalf("unstarted command %s: %+v, %v", id, command, err)
		}
		var receipts int
		if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, id).Scan(&receipts); err != nil || receipts != 0 {
			t.Fatalf("unexpected receipt for revoked command %s: %d, %v", id, receipts, err)
		}
	}
	replayed, replay, err := l.AdminSubmitCommand(ctx, "local-admin", "revoke-local-001", "C01", "", json.RawMessage(`{"customer_id":"revoke-local","plan_id":"basic","seats":"0"}`), "")
	if err != nil || !replay || replayed.ID != local.ID || replayed.ErrorCode != "PERMISSION_REVOKED" {
		t.Fatalf("revoked command replay must return original result: %+v, %v, %v", replayed, replay, err)
	}
	if _, _, err := l.AdminSubmitCommand(ctx, "local-admin", "revoke-local-001", "C01", "", json.RawMessage(`{"customer_id":"different","plan_id":"basic","seats":"0"}`), ""); !errors.Is(err, ErrAdminIdempotencyConflict) {
		t.Fatalf("changed payload must conflict with revoked command key: %v", err)
	}
	var operationStatus string
	if err := l.db.QueryRowContext(ctx, `SELECT status FROM payment_operations WHERE id=?`, unstartedOperation).Scan(&operationStatus); err != nil || operationStatus != "created" {
		t.Fatalf("unstarted operation: %s, %v", operationStatus, err)
	}
	result, err := l.AdminCommand(ctx, unknown.ID)
	if err != nil || result.Status != "waiting_verification" {
		t.Fatalf("submitted operation must remain under verification: %+v, %v", result, err)
	}
	result, err = l.AdminCommand(ctx, inconsistent.ID)
	if err != nil || result.Status != "waiting_verification" {
		t.Fatalf("inconsistent waiting command must not dispatch: %+v, %v", result, err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT status FROM payment_operations WHERE id=?`, inconsistentOperation).Scan(&operationStatus); err != nil || operationStatus != "created" {
		t.Fatalf("inconsistent operation was dispatched: %s, %v", operationStatus, err)
	}
	result, err = l.AdminCommand(ctx, control.ID)
	if err != nil || result.Status != "accepted" || result.ErrorCode != "PERMISSION_REVOKED_REVIEW" {
		t.Fatalf("separate-database control must be held without a receipt: %+v, %v", result, err)
	}
	if err := l.AdminResumeWithPolicy(ctx, func(string) bool { return false }); err != nil {
		t.Fatal(err)
	}
	var holdAudits int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_audit WHERE command_id=? AND reason='PERMISSION_REVOKED_REVIEW'`, control.ID).Scan(&holdAudits); err != nil || holdAudits != 1 {
		t.Fatalf("hold audit must be idempotent: %d, %v", holdAudits, err)
	}
	result, err = l.AdminVerifyExistingObligation(ctx, control.ID)
	if err != nil || result.Status != "accepted" || result.ErrorCode != "PERMISSION_REVOKED_REVIEW" {
		t.Fatalf("verification without receipt must not start provider work: %+v, %v", result, err)
	}
	var providerKey string
	if err := l.db.QueryRowContext(ctx, `SELECT provider_key FROM payment_operations WHERE id=?`, unstartedOperation).Scan(&providerKey); err != nil {
		t.Fatal(err)
	}
	if err := l.provider.adminSetDecision(ctx, control.ID, "payment", providerKey, "definitively_failed"); err != nil {
		t.Fatal(err)
	}
	result, err = l.AdminVerifyExistingObligation(ctx, control.ID)
	if err != nil || result.Status != "succeeded" || result.ErrorCode != "" {
		t.Fatalf("existing provider receipt must be recovered: %+v, %v", result, err)
	}
}

func TestExpiredAdminLeaseCannotStartProviderDispatch(t *testing.T) {
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
	quote, err := l.CreateQuote(ctx, "lease-dispatch", "basic")
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "lease-dispatch-checkout")
	if err != nil {
		t.Fatal(err)
	}
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C09", accepted.OperationID, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "lease-dispatch-001", "C09", accepted.OperationID, json.RawMessage(`{}`), preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	oldGeneration, err := l.adminAcquireLease(ctx, command.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.db.ExecContext(ctx, `UPDATE admin_commands SET lease_until=? WHERE id=?`, time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano), command.ID); err != nil {
		t.Fatal(err)
	}
	newGeneration, err := l.adminAcquireLease(ctx, command.ID)
	if err != nil {
		t.Fatal(err)
	}
	stale := context.WithValue(ctx, adminLeaseContextKey{}, oldGeneration)
	stale = context.WithValue(stale, adminLeaseCommandContextKey{}, command.ID)
	if _, err := l.dispatchCaptureAt(stale, accepted.OperationID, "", time.Now().UTC()); !errors.Is(err, ErrAdminLeaseLost) {
		t.Fatalf("stale worker dispatch: %v", err)
	}
	var status string
	if err := l.db.QueryRowContext(ctx, `SELECT status FROM payment_operations WHERE id=?`, accepted.OperationID).Scan(&status); err != nil || status != "created" {
		t.Fatalf("operation changed by stale worker: %s, %v", status, err)
	}
	l.adminReleaseLease(command.ID, newGeneration)
	revoked, err := l.AdminRevokeUnstarted(ctx, command.ID)
	if err != nil || !revoked {
		t.Fatalf("revoke after stale dispatch: %v, %v", revoked, err)
	}
}

func TestExpiredAdminLeaseCannotStartRefundDispatch(t *testing.T) {
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
	quote, err := l.CreateQuote(ctx, "lease-refund", "basic")
	if err != nil {
		t.Fatal(err)
	}
	paid, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "lease-refund-checkout")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.DispatchCapture(ctx, paid.OperationID, ""); err != nil {
		t.Fatal(err)
	}
	correction, err := l.PostReduction(ctx, paid.InvoiceID, 1000, "service credit", "lease-refund-reduction")
	if err != nil || len(correction.GrantIDs) != 1 {
		t.Fatalf("correction: %+v, %v", correction, err)
	}
	refundID, err := l.ReserveRefund(ctx, correction.GrantIDs[0], 500, "lease-refund-reserve")
	if err != nil {
		t.Fatal(err)
	}
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C16", refundID, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "lease-refund-dispatch-001", "C16", refundID, json.RawMessage(`{}`), preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	oldGeneration, err := l.adminAcquireLease(ctx, command.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.db.ExecContext(ctx, `UPDATE admin_commands SET lease_until=? WHERE id=?`, time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano), command.ID); err != nil {
		t.Fatal(err)
	}
	newGeneration, err := l.adminAcquireLease(ctx, command.ID)
	if err != nil {
		t.Fatal(err)
	}
	stale := context.WithValue(ctx, adminLeaseContextKey{}, oldGeneration)
	stale = context.WithValue(stale, adminLeaseCommandContextKey{}, command.ID)
	if _, err := l.dispatchRefundAt(stale, refundID, "", time.Now().UTC()); !errors.Is(err, ErrAdminLeaseLost) {
		t.Fatalf("stale worker refund dispatch: %v", err)
	}
	var status string
	if err := l.db.QueryRowContext(ctx, `SELECT status FROM refund_operations WHERE id=?`, refundID).Scan(&status); err != nil || status != "created" {
		t.Fatalf("refund changed by stale worker: %s, %v", status, err)
	}
	l.adminReleaseLease(command.ID, newGeneration)
	revoked, err := l.AdminRevokeUnstarted(ctx, command.ID)
	if err != nil || !revoked {
		t.Fatalf("revoke after stale refund dispatch: %v, %v", revoked, err)
	}
	result, err := l.AdminCommand(ctx, command.ID)
	if err != nil || result.Status != "failed" || result.ErrorCode != "PERMISSION_REVOKED" {
		t.Fatalf("revoked refund command: %+v, %v", result, err)
	}
	if _, err := l.AdminVerifyExistingObligation(ctx, command.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("revoked unstarted refund must not resume: %v", err)
	}
	var refunds, receipts int
	if err := l.provider.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM refunds`).Scan(&refunds); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if refunds != 0 || receipts != 0 {
		t.Fatalf("revoked refund left provider or command facts: refunds=%d receipts=%d", refunds, receipts)
	}
}

func TestRevokedRefundDispatchVerifiesExistingProviderResult(t *testing.T) {
	ctx := context.Background()
	l, _, _ := openTestLab(t)
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	quote, err := l.CreateQuote(ctx, "revoked-refund", "basic")
	if err != nil {
		t.Fatal(err)
	}
	paid, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "revoked-refund-checkout")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.DispatchCapture(ctx, paid.OperationID, ""); err != nil {
		t.Fatal(err)
	}
	correction, err := l.PostReduction(ctx, paid.InvoiceID, 1000, "service credit", "revoked-refund-reduction")
	if err != nil || len(correction.GrantIDs) != 1 {
		t.Fatalf("correction: %+v, %v", correction, err)
	}
	refundID, err := l.ReserveRefund(ctx, correction.GrantIDs[0], 500, "revoked-refund-reserve")
	if err != nil {
		t.Fatal(err)
	}
	submitControl(t, l, "revoked-refund-fault-001", "C49", refundID, json.RawMessage(`{"operation_kind":"refund","mode":"lost_response"}`))
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C16", refundID, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "revoked-refund-dispatch-001", "C16", refundID, json.RawMessage(`{}`), preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	command, err = l.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "waiting_verification" {
		t.Fatalf("lost refund response: %+v, %v", command, err)
	}
	if err := l.AdminResumeWithPolicy(ctx, func(string) bool { return false }); err != nil {
		t.Fatal(err)
	}
	command, err = l.AdminCommand(ctx, command.ID)
	if err != nil || command.Status != "succeeded" {
		t.Fatalf("revoked recovery did not verify existing refund: %+v, %v", command, err)
	}
	var refunds, receipts int
	if err := l.provider.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM refunds`).Scan(&refunds); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if refunds != 1 || receipts != 1 {
		t.Fatalf("refund verification duplicated or lost facts: refunds=%d receipts=%d", refunds, receipts)
	}
}

func TestRevokedRefundWithoutProviderEvidenceRemainsWaiting(t *testing.T) {
	ctx := context.Background()
	l, _, _ := openTestLab(t)
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	quote, err := l.CreateQuote(ctx, "revoked-refund-absent", "basic")
	if err != nil {
		t.Fatal(err)
	}
	paid, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "revoked-refund-absent-checkout")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.DispatchCapture(ctx, paid.OperationID, ""); err != nil {
		t.Fatal(err)
	}
	correction, err := l.PostReduction(ctx, paid.InvoiceID, 1000, "service credit", "revoked-refund-absent-reduction")
	if err != nil || len(correction.GrantIDs) != 1 {
		t.Fatalf("correction: %+v, %v", correction, err)
	}
	refundID, err := l.ReserveRefund(ctx, correction.GrantIDs[0], 500, "revoked-refund-absent-reserve")
	if err != nil {
		t.Fatal(err)
	}
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C16", refundID, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "revoked-refund-absent-dispatch-001", "C16", refundID, json.RawMessage(`{}`), preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Model a crash after marking the provider request as submitted, before
	// the provider receives it. Recovery must query the original key only.
	if _, err := l.db.ExecContext(ctx, `UPDATE refund_operations SET status='submitted' WHERE id=? AND status='created'`, refundID); err != nil {
		t.Fatal(err)
	}
	if _, err := l.db.ExecContext(ctx, `UPDATE admin_commands SET status='waiting_verification' WHERE id=? AND status='accepted'`, command.ID); err != nil {
		t.Fatal(err)
	}
	if err := l.AdminResumeWithPolicy(ctx, func(string) bool { return false }); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		command, err = l.AdminVerifyExistingObligation(ctx, command.ID)
		if err != nil || command.Status != "waiting_verification" {
			t.Fatalf("verification %d invented a result: %+v, %v", i, command, err)
		}
	}
	var refunds, receipts int
	if err := l.provider.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM refunds`).Scan(&refunds); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if refunds != 0 || receipts != 0 {
		t.Fatalf("missing provider evidence created facts: refunds=%d receipts=%d", refunds, receipts)
	}
}
