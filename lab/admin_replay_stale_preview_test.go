package lab

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestAdminSuccessfulCommandReplayPrecedesExpiredPreviewAndClockRevision(t *testing.T) {
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

	quote, err := l.CreateQuote(ctx, "replay-customer", "basic")
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(AdminAcceptQuotePayload{Fingerprint: quote.Fingerprint})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C02", quote.ID, payload)
	if err != nil {
		t.Fatal(err)
	}
	const key = "accept-quote-after-preview-expiry"
	command, replay, err := l.AdminSubmitCommand(ctx, "local-admin", key, "C02", quote.ID, payload, preview.ID)
	if err != nil || replay {
		t.Fatalf("submit: command=%+v replay=%v err=%v", command, replay, err)
	}
	command, err = l.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "succeeded" {
		t.Fatalf("execute: command=%+v err=%v", command, err)
	}

	before, err := l.AdminClock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	fixed := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	clockPayload, err := json.Marshal(map[string]string{"mode": "fixed", "value_utc": fixed.Format(time.RFC3339Nano)})
	if err != nil {
		t.Fatal(err)
	}
	submitControl(t, l, "advance-clock-after-accepted-quote", "C46", "", clockPayload)
	after, err := l.AdminClock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if after.Revision <= before.Revision {
		t.Fatalf("clock revision did not advance: before=%d after=%d", before.Revision, after.Revision)
	}
	if _, err := l.db.ExecContext(ctx, `UPDATE admin_previews SET expires_at=? WHERE id=?`, time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano), preview.ID); err != nil {
		t.Fatal(err)
	}

	again, replay, err := l.AdminSubmitCommand(ctx, "local-admin", key, "C02", quote.ID, payload, preview.ID)
	if err != nil || !replay || again.ID != command.ID || again.Status != "succeeded" || !bytes.Equal(again.ResultRefs, command.ResultRefs) {
		t.Fatalf("original key did not recover succeeded command: command=%+v replay=%v err=%v", again, replay, err)
	}
	changedPayload, err := json.Marshal(AdminAcceptQuotePayload{Fingerprint: quote.Fingerprint + "-changed"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := l.AdminSubmitCommand(ctx, "local-admin", key, "C02", quote.ID, changedPayload, preview.ID); !errors.Is(err, ErrAdminIdempotencyConflict) {
		t.Fatalf("same key with changed payload: %v", err)
	}
	if _, _, err := l.AdminSubmitCommand(ctx, "local-admin", "new-key-after-preview-expiry", "C02", quote.ID, payload, preview.ID); !errors.Is(err, ErrAdminPreviewStale) {
		t.Fatalf("new key with expired preview: %v", err)
	}

	var subscriptions, receipts int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM subscriptions WHERE quote_id=?`, quote.ID).Scan(&subscriptions); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if subscriptions != 1 || receipts != 1 {
		t.Fatalf("replay created duplicate effects: subscriptions=%d receipts=%d", subscriptions, receipts)
	}
}
