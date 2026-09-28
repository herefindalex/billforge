package lab

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestQuoteExpiryStaysWithinUnixNanoRange(t *testing.T) {
	ctx := context.Background()
	now := maxUnixNanoTime.Add(-15 * time.Minute)
	dir := t.TempDir()
	l, err := Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	quote, err := l.CreateQuote(ctx, "boundary-customer", "basic")
	if err != nil || !quote.ExpiresAt.Equal(maxUnixNanoTime) {
		t.Fatalf("last representable expiry: %+v, %v", quote, err)
	}
	if _, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "boundary-accept-001"); !errors.Is(err, ErrConflict) {
		t.Fatalf("unrepresentable next billing boundary must reject acceptance, got %v", err)
	}
	var subscriptions int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM subscriptions`).Scan(&subscriptions); err != nil || subscriptions != 0 {
		t.Fatalf("rejected acceptance left subscription: count=%d err=%v", subscriptions, err)
	}

	now = maxUnixNanoTime.Add(-5 * time.Minute)
	if _, err := l.CreateQuote(ctx, "overflow-customer", "basic"); !errors.Is(err, ErrConflict) {
		t.Fatalf("overflowing quote expiry must be rejected, got %v", err)
	}
	var quotes int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM quotes`).Scan(&quotes); err != nil || quotes != 1 {
		t.Fatalf("overflow created a quote: count=%d err=%v", quotes, err)
	}
}

func TestQuoteExpiryRejectsClockOutsideUnixNanoRange(t *testing.T) {
	for _, at := range []time.Time{minUnixNanoTime.Add(-time.Nanosecond), maxUnixNanoTime.Add(time.Nanosecond)} {
		if _, err := quoteExpiryAt(at); !errors.Is(err, ErrConflict) {
			t.Fatalf("clock %s outside storage range: %v", at, err)
		}
	}
	if expires, err := quoteExpiryAt(minUnixNanoTime); err != nil || !expires.Equal(minUnixNanoTime.Add(15*time.Minute)) {
		t.Fatalf("minimum representable clock: %s, %v", expires, err)
	}
}

func TestAdminQuoteWithOverflowingExpiryFailsWithoutReceipt(t *testing.T) {
	ctx := context.Background()
	l, _, _ := openTestLab(t)
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	nearMax := maxUnixNanoTime.Add(-5 * time.Minute).Format(time.RFC3339Nano)
	clockPayload, err := json.Marshal(map[string]string{"mode": "fixed", "value_utc": nearMax})
	if err != nil {
		t.Fatal(err)
	}
	submitControl(t, l, "near-max-clock-001", "C46", "", clockPayload)
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "near-max-quote-001", "C01", "", json.RawMessage(`{"customer_id":"overflow-customer","plan_id":"basic","cohort":"default","seats":"0"}`), "")
	if err != nil {
		t.Fatal(err)
	}
	command, err = l.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "failed" || command.ErrorCode != "DOMAIN_REJECTED" {
		t.Fatalf("overflow command: %+v, %v", command, err)
	}
	var quotes, receipts int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM quotes`).Scan(&quotes); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if quotes != 0 || receipts != 0 {
		t.Fatalf("rejected quote left domain facts: quotes=%d receipts=%d", quotes, receipts)
	}
}

func TestCaptureRejectsUnrepresentableActivationBeforeProviderCall(t *testing.T) {
	ctx := context.Background()
	now := maxUnixNanoTime.AddDate(0, -1, 0)
	if !cycleBoundary(now, 1).Equal(maxUnixNanoTime) {
		t.Fatalf("test anchor does not end at storage boundary: %s", now)
	}
	dir := t.TempDir()
	l, err := Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	quote, err := l.CreateQuote(ctx, "activation-boundary-customer", "basic")
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "activation-boundary-checkout")
	if err != nil {
		t.Fatal(err)
	}
	now = maxUnixNanoTime.Add(-5 * time.Minute)
	if _, err := l.DispatchCapture(ctx, accepted.OperationID, ""); !errors.Is(err, ErrConflict) {
		t.Fatalf("unrepresentable activation must reject before provider call: %v", err)
	}
	var status string
	if err := l.db.QueryRowContext(ctx, `SELECT status FROM payment_operations WHERE id=?`, accepted.OperationID).Scan(&status); err != nil || status != "created" {
		t.Fatalf("rejected payment changed local status: %s, %v", status, err)
	}
	if got := captureCount(t, l); got != 0 {
		t.Fatalf("rejected activation charged provider %d times", got)
	}
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C09", accepted.OperationID, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "activation-boundary-dispatch-001", "C09", accepted.OperationID, json.RawMessage(`{}`), preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	command, err = l.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "failed" || command.ErrorCode != "DOMAIN_REJECTED" {
		t.Fatalf("admin dispatch at boundary: %+v, %v", command, err)
	}
	var receipts int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if got := captureCount(t, l); got != 0 || receipts != 0 {
		t.Fatalf("admin rejection created provider or command facts: captures=%d receipts=%d", got, receipts)
	}
}

func TestExistingProviderCaptureCannotOverflowActivationPeriod(t *testing.T) {
	ctx := context.Background()
	now := maxUnixNanoTime.AddDate(0, -1, 0)
	dir := t.TempDir()
	l, err := Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	quote, err := l.CreateQuote(ctx, "existing-capture-boundary", "basic")
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "existing-capture-boundary-checkout")
	if err != nil {
		t.Fatal(err)
	}
	var key string
	if err := l.db.QueryRowContext(ctx, `SELECT provider_key FROM payment_operations WHERE id=?`, accepted.OperationID).Scan(&key); err != nil {
		t.Fatal(err)
	}
	event, err := l.provider.Capture(ctx, key, accepted.AmountMinor, accepted.Currency)
	if err != nil {
		t.Fatal(err)
	}
	now = maxUnixNanoTime.Add(-5 * time.Minute)
	if err := l.applyObservationAt(ctx, event, now); !errors.Is(err, ErrConflict) {
		t.Fatalf("unrepresentable activation must return a domain conflict: %v", err)
	}
	var inbox, allocations int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM inbox WHERE provider_key=?`, key).Scan(&inbox); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM allocations WHERE operation_id=?`, accepted.OperationID).Scan(&allocations); err != nil {
		t.Fatal(err)
	}
	if inbox != 0 || allocations != 0 || captureCount(t, l) != 1 {
		t.Fatalf("provider evidence or local facts changed unexpectedly: inbox=%d allocations=%d captures=%d", inbox, allocations, captureCount(t, l))
	}
}
