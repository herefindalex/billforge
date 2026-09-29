package lab

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
)

func TestAdminControlCommandsRequirePreviewButLegacyKeysRemainRecoverable(t *testing.T) {
	ctx := context.Background()
	l, _, _ := openTestLab(t)
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	quote, err := l.CreateQuote(ctx, "control-preview-required", "basic")
	if err != nil {
		t.Fatal(err)
	}
	paid, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "control-preview-required-checkout")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		action, target, payload string
	}{
		{"C46", "", `{"mode":"real"}`},
		{"C47", paid.OperationID, `{"status":"succeeded"}`},
		{"C49", paid.OperationID, `{"operation_kind":"payment","mode":"lost_response"}`},
	} {
		if _, _, err := l.AdminSubmitCommand(ctx, "local-admin", "no-preview-"+tc.action, tc.action, tc.target, json.RawMessage(tc.payload), ""); !errors.Is(err, ErrAdminInvalidCommand) {
			t.Fatalf("%s accepted without preview: %v", tc.action, err)
		}
	}
	clockPayload := json.RawMessage(`{"mode":"real"}`)
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C46", "", clockPayload)
	if err != nil {
		t.Fatal(err)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "legacy-clock-control", "C46", "", clockPayload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := canonicalAdminPayload("C46", clockPayload)
	if err != nil {
		t.Fatal(err)
	}
	input, err := json.Marshal([]any{"C46", "", "", json.RawMessage(canonical)})
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(input)
	if _, err := l.db.ExecContext(ctx, `UPDATE admin_commands SET preview_id=NULL,payload_hash=? WHERE id=?`, hex.EncodeToString(hash[:]), command.ID); err != nil {
		t.Fatal(err)
	}
	replayed, found, err := l.AdminSubmitCommand(ctx, "local-admin", "legacy-clock-control", "C46", "", clockPayload, "")
	if err != nil || !found || replayed.ID != command.ID {
		t.Fatalf("legacy key recovery: %+v found=%t err=%v", replayed, found, err)
	}
}

func TestAdminControlPreviewsShowSourcesWithoutChangingLabState(t *testing.T) {
	ctx := context.Background()
	l, _, _ := openTestLab(t)
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	quote, err := l.CreateQuote(ctx, "control-preview-customer", "basic")
	if err != nil {
		t.Fatal(err)
	}
	paid, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "control-preview-checkout")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.DispatchNext(ctx, ""); err != nil {
		t.Fatal(err)
	}
	correction, err := l.PostReduction(ctx, paid.InvoiceID, 1000, "service credit", "control-preview-reduction")
	if err != nil || len(correction.GrantIDs) != 1 {
		t.Fatalf("correction: %+v err=%v", correction, err)
	}
	refundID, err := l.ReserveRefund(ctx, correction.GrantIDs[0], 400, "control-preview-refund")
	if err != nil {
		t.Fatal(err)
	}
	secondQuote, err := l.CreateQuote(ctx, "control-preview-second-customer", "basic")
	if err != nil {
		t.Fatal(err)
	}
	second, err := l.AcceptQuote(ctx, secondQuote.ID, secondQuote.Fingerprint, "control-preview-second-checkout")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		action, target, payload, impactKey, impactValue string
	}{
		{"C46", "", `{"mode":"fixed","value_utc":"2026-10-01T12:00:00Z"}`, "proposed_mode", "fixed"},
		{"C47", second.OperationID, `{"status":"definitively_failed"}`, "proposed_decision", "definitively_failed"},
		{"C48", refundID, `{"status":"succeeded"}`, "proposed_decision", "succeeded"},
		{"C49", second.OperationID, `{"operation_kind":"payment","mode":"lost_response"}`, "fault_mode", "lost_response"},
	} {
		t.Run(tc.action, func(t *testing.T) {
			preview, err := l.AdminCreatePreview(ctx, "local-admin", tc.action, tc.target, json.RawMessage(tc.payload))
			if err != nil {
				t.Fatal(err)
			}
			if preview.ID == "" || preview.ActionID != tc.action || preview.TargetID != tc.target || len(preview.SourceVersions) == 0 {
				t.Fatalf("incomplete control preview: %+v", preview)
			}
			var impact map[string]string
			if err := json.Unmarshal(preview.Impact, &impact); err != nil {
				t.Fatal(err)
			}
			if impact[tc.impactKey] != tc.impactValue {
				t.Fatalf("wrong impact: %+v", impact)
			}
		})
	}
	clock, err := l.AdminClock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if clock.Mode != "real" || clock.Revision != 0 {
		t.Fatalf("preview changed clock: %+v", clock)
	}
	var commands, tickets int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_commands WHERE action_id IN ('C46','C47','C48','C49')`).Scan(&commands); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_fault_tickets`).Scan(&tickets); err != nil {
		t.Fatal(err)
	}
	if commands != 0 || tickets != 0 {
		t.Fatalf("previews mutated controls: commands=%d tickets=%d", commands, tickets)
	}
	if err := l.SetFakePaymentDecision(ctx, second.OperationID, "succeeded"); err != nil {
		t.Fatal(err)
	}
	if _, err := l.AdminCreatePreview(ctx, "local-admin", "C47", second.OperationID, json.RawMessage(`{"status":"definitively_failed"}`)); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflicting provider decision preview: %v", err)
	}
}

func TestAdminControlCommandsRejectChangedPreviewSources(t *testing.T) {
	ctx := context.Background()
	t.Run("clock", func(t *testing.T) {
		l, _, _ := openTestLab(t)
		if err := l.InitAdmin(ctx); err != nil {
			t.Fatal(err)
		}
		payload := json.RawMessage(`{"mode":"fixed","value_utc":"2026-10-01T12:00:00Z"}`)
		preview, err := l.AdminCreatePreview(ctx, "local-admin", "C46", "", payload)
		if err != nil {
			t.Fatal(err)
		}
		command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "clock-stale-control", "C46", "", payload, preview.ID)
		if err != nil {
			t.Fatal(err)
		}
		otherPayload := json.RawMessage(`{"mode":"real"}`)
		submitControl(t, l, "clock-new-control", "C46", "", otherPayload)
		command, err = l.AdminExecuteCommand(ctx, command.ID)
		if err != nil || command.Status != "failed" || command.ErrorCode != "PREVIEW_STALE" {
			t.Fatalf("clock source change did not reject preview: %+v err=%v", command, err)
		}
		clock, err := l.AdminClock(ctx)
		if err != nil || clock.Mode != "real" || clock.Revision != 1 {
			t.Fatalf("stale command changed clock: %+v err=%v", clock, err)
		}
	})
	t.Run("provider decision", func(t *testing.T) {
		l, _, _ := openTestLab(t)
		if err := l.InitAdmin(ctx); err != nil {
			t.Fatal(err)
		}
		quote, err := l.CreateQuote(ctx, "control-stale-decision", "basic")
		if err != nil {
			t.Fatal(err)
		}
		accepted, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "control-stale-decision-checkout")
		if err != nil {
			t.Fatal(err)
		}
		payload := json.RawMessage(`{"status":"succeeded"}`)
		preview, err := l.AdminCreatePreview(ctx, "local-admin", "C47", accepted.OperationID, payload)
		if err != nil {
			t.Fatal(err)
		}
		command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "decision-stale-control", "C47", accepted.OperationID, payload, preview.ID)
		if err != nil {
			t.Fatal(err)
		}
		if err := l.SetFakePaymentDecision(ctx, accepted.OperationID, "definitively_failed"); err != nil {
			t.Fatal(err)
		}
		command, err = l.AdminExecuteCommand(ctx, command.ID)
		if err != nil || command.Status != "failed" || command.ErrorCode != "PREVIEW_STALE" {
			t.Fatalf("provider source change did not reject preview: %+v err=%v", command, err)
		}
		var receipts int
		if err := l.provider.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM provider_control_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil {
			t.Fatal(err)
		}
		if receipts != 0 {
			t.Fatalf("stale provider command wrote %d external receipts", receipts)
		}
	})
	t.Run("fault ticket", func(t *testing.T) {
		l, _, _ := openTestLab(t)
		if err := l.InitAdmin(ctx); err != nil {
			t.Fatal(err)
		}
		quote, err := l.CreateQuote(ctx, "control-stale-fault", "basic")
		if err != nil {
			t.Fatal(err)
		}
		accepted, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "control-stale-fault-checkout")
		if err != nil {
			t.Fatal(err)
		}
		payload := json.RawMessage(`{"operation_kind":"payment","mode":"lost_response"}`)
		preview, err := l.AdminCreatePreview(ctx, "local-admin", "C49", accepted.OperationID, payload)
		if err != nil {
			t.Fatal(err)
		}
		command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "fault-stale-control", "C49", accepted.OperationID, payload, preview.ID)
		if err != nil {
			t.Fatal(err)
		}
		submitControl(t, l, "fault-new-control", "C49", accepted.OperationID, payload)
		command, err = l.AdminExecuteCommand(ctx, command.ID)
		if err != nil || command.Status != "failed" || command.ErrorCode != "PREVIEW_STALE" {
			t.Fatalf("fault source change did not reject preview: %+v err=%v", command, err)
		}
		var tickets int
		if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_fault_tickets WHERE operation_kind='payment' AND operation_id=?`, accepted.OperationID).Scan(&tickets); err != nil {
			t.Fatal(err)
		}
		if tickets != 1 {
			t.Fatalf("stale fault command left %d tickets", tickets)
		}
	})
}

func TestAdminControlCommandsRejectStaleSourcesBeforeAdmission(t *testing.T) {
	ctx := context.Background()
	assertStale := func(t *testing.T, l *Lab, actionID, targetID string, payload json.RawMessage, previewID string) {
		t.Helper()
		key := "stale-admission-" + actionID
		command, found, err := l.AdminSubmitCommand(ctx, "local-admin", key, actionID, targetID, payload, previewID)
		if !errors.Is(err, ErrAdminPreviewStale) || found || command.ID != "" {
			t.Fatalf("stale %s admission: %+v found=%t err=%v", actionID, command, found, err)
		}
		var commands int
		if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_commands WHERE idempotency_key=?`, key).Scan(&commands); err != nil || commands != 0 {
			t.Fatalf("stale %s created %d commands: %v", actionID, commands, err)
		}
		var claimed string
		if err := l.db.QueryRowContext(ctx, `SELECT COALESCE(claimed_command_id,'') FROM admin_previews WHERE id=?`, previewID).Scan(&claimed); err != nil || claimed != "" {
			t.Fatalf("stale %s claimed preview %q: %v", actionID, claimed, err)
		}
	}

	t.Run("payment decision", func(t *testing.T) {
		l, _, _ := openTestLab(t)
		if err := l.InitAdmin(ctx); err != nil {
			t.Fatal(err)
		}
		quote, err := l.CreateQuote(ctx, "stale-admission-payment", "basic")
		if err != nil {
			t.Fatal(err)
		}
		paid, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "stale-admission-payment-checkout")
		if err != nil {
			t.Fatal(err)
		}
		payload := json.RawMessage(`{"status":"succeeded"}`)
		preview, err := l.AdminCreatePreview(ctx, "local-admin", "C47", paid.OperationID, payload)
		if err != nil {
			t.Fatal(err)
		}
		if err := l.SetFakePaymentDecision(ctx, paid.OperationID, "definitively_failed"); err != nil {
			t.Fatal(err)
		}
		assertStale(t, l, "C47", paid.OperationID, payload, preview.ID)
	})

	t.Run("refund decision", func(t *testing.T) {
		l, _, _ := openTestLab(t)
		if err := l.InitAdmin(ctx); err != nil {
			t.Fatal(err)
		}
		quote, err := l.CreateQuote(ctx, "stale-admission-refund", "basic")
		if err != nil {
			t.Fatal(err)
		}
		paid, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "stale-admission-refund-checkout")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := l.DispatchCapture(ctx, paid.OperationID, ""); err != nil {
			t.Fatal(err)
		}
		credit, err := l.PostReduction(ctx, paid.InvoiceID, 1000, "refund source change", "stale-admission-refund-reduction")
		if err != nil || len(credit.GrantIDs) != 1 {
			t.Fatalf("reduction: %+v err=%v", credit, err)
		}
		refundID, err := l.ReserveRefund(ctx, credit.GrantIDs[0], 500, "stale-admission-refund-reserve")
		if err != nil {
			t.Fatal(err)
		}
		payload := json.RawMessage(`{"status":"succeeded"}`)
		preview, err := l.AdminCreatePreview(ctx, "local-admin", "C48", refundID, payload)
		if err != nil {
			t.Fatal(err)
		}
		if err := l.SetFakeRefundDecision(ctx, refundID, "definitively_failed"); err != nil {
			t.Fatal(err)
		}
		assertStale(t, l, "C48", refundID, payload, preview.ID)
	})

	t.Run("fault ticket", func(t *testing.T) {
		l, _, _ := openTestLab(t)
		if err := l.InitAdmin(ctx); err != nil {
			t.Fatal(err)
		}
		quote, err := l.CreateQuote(ctx, "stale-admission-fault", "basic")
		if err != nil {
			t.Fatal(err)
		}
		paid, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "stale-admission-fault-checkout")
		if err != nil {
			t.Fatal(err)
		}
		payload := json.RawMessage(`{"operation_kind":"payment","mode":"lost_response"}`)
		preview, err := l.AdminCreatePreview(ctx, "local-admin", "C49", paid.OperationID, payload)
		if err != nil {
			t.Fatal(err)
		}
		submitControl(t, l, "stale-admission-other-fault", "C49", paid.OperationID, json.RawMessage(`{"operation_kind":"payment","mode":"crash_after_provider"}`))
		assertStale(t, l, "C49", paid.OperationID, payload, preview.ID)
	})
}
