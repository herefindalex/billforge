package lab

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

func TestPaymentPreviewAdmissionRejectsWrongActorActionTargetAndIntent(t *testing.T) {
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
	quote, err := l.CreateQuote(ctx, "preview-admission-customer", "basic")
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "preview-admission-checkout")
	if err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{"amount_minor":"1000"}`)
	preview, err := l.AdminCreatePreview(ctx, "owner", "C07", accepted.InvoiceID, payload)
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name, actorID, actionID, targetID string
		payload                           json.RawMessage
	}{
		{"other-actor", "other", "C07", accepted.InvoiceID, payload},
		{"other-action", "owner", "C15", accepted.InvoiceID, payload},
		{"other-target", "owner", "C07", "different-invoice", payload},
		{"other-intent", "owner", "C07", accepted.InvoiceID, json.RawMessage(`{"amount_minor":"999"}`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := l.AdminSubmitCommand(ctx, tc.actorID, "preview-rejected-"+tc.name, tc.actionID, tc.targetID, tc.payload, preview.ID)
			if !errors.Is(err, ErrAdminPreviewStale) {
				t.Fatalf("admission error = %v; want PREVIEW_STALE", err)
			}
		})
	}
	var commandCount int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_commands`).Scan(&commandCount); err != nil {
		t.Fatal(err)
	}
	if commandCount != 0 {
		t.Fatalf("mismatched preview admitted %d command(s)", commandCount)
	}

	command, replay, err := l.AdminSubmitCommand(ctx, "owner", "preview-valid-payment", "C07", accepted.InvoiceID, payload, preview.ID)
	if err != nil || replay || command.ID == "" {
		t.Fatalf("valid preview was rejected: command=%+v replay=%v err=%v", command, replay, err)
	}
	if _, _, err := l.AdminSubmitCommand(ctx, "owner", "preview-claimed-payment", "C07", accepted.InvoiceID, payload, preview.ID); !errors.Is(err, ErrAdminPreviewStale) {
		t.Fatalf("claimed preview accepted by a second command: %v", err)
	}
	if _, err := l.db.ExecContext(ctx, `UPDATE admin_previews SET expires_at='2020-01-01T00:00:00Z' WHERE id=?`, preview.ID); err != nil {
		t.Fatal(err)
	}
	replayed, same, err := l.AdminSubmitCommand(ctx, "owner", "preview-valid-payment", "C07", accepted.InvoiceID, payload, preview.ID)
	if err != nil || !same || replayed.ID != command.ID {
		t.Fatalf("expired preview hid original command: replay=%+v same=%v err=%v", replayed, same, err)
	}
	if _, _, err := l.AdminSubmitCommand(ctx, "owner", "preview-valid-payment", "C07", accepted.InvoiceID, json.RawMessage(`{"amount_minor":"999"}`), preview.ID); !errors.Is(err, ErrAdminIdempotencyConflict) {
		t.Fatalf("changed payload reused command key: %v", err)
	}
	expired, err := l.AdminCreatePreview(ctx, "owner", "C07", accepted.InvoiceID, payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.db.ExecContext(ctx, `UPDATE admin_previews SET expires_at='2020-01-01T00:00:00Z' WHERE id=?`, expired.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := l.AdminSubmitCommand(ctx, "owner", "preview-expired-payment", "C07", accepted.InvoiceID, payload, expired.ID); !errors.Is(err, ErrAdminPreviewStale) {
		t.Fatalf("expired preview accepted: %v", err)
	}
}
