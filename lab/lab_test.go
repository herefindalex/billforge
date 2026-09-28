package lab

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

var fixedNow = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

func openTestLab(t *testing.T) (*Lab, string, string) {
	t.Helper()
	dir := t.TempDir()
	local, provider := filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db")
	l, err := Open(local, provider, func() time.Time { return fixedNow })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	return l, local, provider
}

func purchase(t *testing.T, l *Lab) Receipt {
	t.Helper()
	ctx := context.Background()
	q, err := l.CreateQuote(ctx, "customer-1", "basic")
	if err != nil {
		t.Fatal(err)
	}
	if q.AmountMinor != 2000 || q.Currency != "USD" || q.PriceVersionID != "basic-v1" {
		t.Fatalf("wrong quote: %+v", q)
	}
	r, err := l.AcceptQuote(ctx, q.ID, q.Fingerprint, "checkout-1")
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func snapshot(t *testing.T, l *Lab, subID string) Snapshot {
	t.Helper()
	s, err := l.Snapshot(context.Background(), subID)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func captureCount(t *testing.T, l *Lab) int {
	t.Helper()
	var n int
	if err := l.provider.db.QueryRow(`SELECT COUNT(*) FROM captures`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestS01NewBasicPurchase(t *testing.T) {
	l, _, _ := openTestLab(t)
	ctx := context.Background()
	r := purchase(t, l)
	before := snapshot(t, l, r.SubscriptionID)
	if before.SubscriptionStatus != "pending" || before.InvoiceTotalMinor != 2000 || before.AllocationCount != 0 || before.EntitlementStatus != "pending" {
		t.Fatalf("unpaid purchase activated: %+v", before)
	}
	opID, err := l.DispatchNext(ctx, "")
	if err != nil || opID != r.OperationID {
		t.Fatalf("dispatch %q: %v", opID, err)
	}
	after := snapshot(t, l, r.SubscriptionID)
	if after.SubscriptionStatus != "active" || after.OperationStatus != "succeeded" || after.AllocationCount != 1 || after.AllocatedMinor != 2000 || after.EntitlementStatus != "pending" {
		t.Fatalf("wrong committed source facts: %+v", after)
	}
	if err := l.RebuildEntitlements(ctx); err != nil {
		t.Fatal(err)
	}
	if got := snapshot(t, l, r.SubscriptionID).EntitlementStatus; got != "active" {
		t.Fatalf("entitlement = %q", got)
	}
	if captureCount(t, l) != 1 {
		t.Fatal("expected exactly one provider capture")
	}
}

func TestS04LostResponseKeepsOriginalOperation(t *testing.T) {
	l, _, _ := openTestLab(t)
	ctx := context.Background()
	r := purchase(t, l)
	_, err := l.DispatchNext(ctx, "lost_response")
	if !errors.Is(err, ErrPaymentUnknown) {
		t.Fatalf("expected unknown, got %v", err)
	}
	s := snapshot(t, l, r.SubscriptionID)
	if s.OperationStatus != "unknown" || s.SubscriptionStatus != "pending" || s.AllocationCount != 0 {
		t.Fatalf("unknown incorrectly settled: %+v", s)
	}
	if captureCount(t, l) != 1 {
		t.Fatal("provider should already have one capture")
	}
	retriedOp, err := l.DispatchNext(ctx, "")
	if err != nil || retriedOp != r.OperationID {
		t.Fatalf("retry should query original operation: id=%q err=%v", retriedOp, err)
	}
	_, err = l.DispatchNext(ctx, "")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("capture outbox should be complete: %v", err)
	}
	found, err := l.ReconcilePayment(ctx, r.OperationID)
	if err != nil || !found {
		t.Fatalf("repeat lookup failed: %v", err)
	}
	s = snapshot(t, l, r.SubscriptionID)
	if s.AllocationCount != 1 || s.AllocatedMinor != 2000 || captureCount(t, l) != 1 {
		t.Fatalf("duplicate financial effect: %+v", s)
	}
}

func TestS05CrashWebhookOrderAndProjectionRecovery(t *testing.T) {
	l, local, provider := openTestLab(t)
	ctx := context.Background()
	r := purchase(t, l)
	_, err := l.DispatchNext(ctx, "crash_after_provider")
	if !errors.Is(err, ErrInjectedCrash) {
		t.Fatalf("expected crash, got %v", err)
	}
	if got := snapshot(t, l, r.SubscriptionID); got.OperationStatus != "submitted" || got.AllocationCount != 0 {
		t.Fatalf("local effects should be missing: %+v", got)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	l, err = Open(local, provider, func() time.Time { return fixedNow })
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	e, found, err := l.provider.Lookup(ctx, "capture:"+r.InvoiceID)
	if err != nil || !found {
		t.Fatalf("provider capture not persistent: %v", err)
	}
	e.ID = "webhook:success"
	if err := l.HandleWebhook(ctx, e); err != nil {
		t.Fatal(err)
	}
	if err := l.HandleWebhook(ctx, e); err != nil {
		t.Fatal(err)
	}
	pending := e
	pending.ID, pending.Status = "webhook:old-pending", "pending"
	if err := l.HandleWebhook(ctx, pending); err != nil {
		t.Fatal(err)
	}
	s := snapshot(t, l, r.SubscriptionID)
	if s.OperationStatus != "succeeded" || s.AllocationCount != 1 || s.EntitlementStatus != "pending" {
		t.Fatalf("webhook ordering or projection boundary wrong: %+v", s)
	}
	// A second process restart occurs after allocation but before projection.
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	l, err = Open(local, provider, func() time.Time { return fixedNow })
	if err != nil {
		t.Fatal(err)
	}
	if err := l.RebuildEntitlements(ctx); err != nil {
		t.Fatal(err)
	}
	if err := l.RebuildEntitlements(ctx); err != nil {
		t.Fatal(err)
	}
	s = snapshot(t, l, r.SubscriptionID)
	if s.EntitlementStatus != "active" || s.AllocationCount != 1 || captureCount(t, l) != 1 {
		t.Fatalf("recovery duplicated or lost facts: %+v", s)
	}
}

func TestQuoteExpiryIdentityAndPriceImmutability(t *testing.T) {
	l, _, _ := openTestLab(t)
	ctx := context.Background()
	q, err := l.CreateQuote(ctx, "customer-1", "basic")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.AcceptQuote(ctx, q.ID, "wrong", "key-1"); !errors.Is(err, ErrConflict) {
		t.Fatalf("fingerprint mismatch: %v", err)
	}
	r, err := l.AcceptQuote(ctx, q.ID, q.Fingerprint, "key-1")
	if err != nil {
		t.Fatal(err)
	}
	again, err := l.AcceptQuote(ctx, q.ID, q.Fingerprint, "key-1")
	if err != nil || again != r {
		t.Fatalf("idempotency failed: %+v, %v", again, err)
	}
	if _, err := l.AcceptQuote(ctx, q.ID, q.Fingerprint, "different-key"); !errors.Is(err, ErrConflict) {
		t.Fatalf("same quote must not create a second obligation: %v", err)
	}
	q2, err := l.CreateQuote(ctx, "customer-2", "basic")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.AcceptQuote(ctx, q2.ID, q2.Fingerprint, "key-1"); !errors.Is(err, ErrConflict) {
		t.Fatalf("same key with new payload must conflict: %v", err)
	}
	if _, err := l.db.Exec(`UPDATE price_versions SET fixed_amount_minor=3000 WHERE id='basic-v1'`); err == nil {
		t.Fatal("published price was mutable")
	}
	if _, err := l.db.Exec(`UPDATE payment_operations SET amount_minor=3000 WHERE id=?`, r.OperationID); err == nil {
		t.Fatal("payment payload was mutable")
	}
	if _, err := l.db.Exec(`INSERT INTO invoice_lines(invoice_id,price_version_id,component_code,amount_minor)
VALUES(?,'basic-v1','extra',100)`, r.InvoiceID); err == nil {
		t.Fatal("finalized invoice accepted another line")
	}
	if _, err := l.db.Exec(`UPDATE invoices SET total_minor=3000 WHERE id=?`, r.InvoiceID); err == nil {
		t.Fatal("finalized invoice total was mutable")
	}
	if _, err := l.DispatchNext(ctx, "bad_fault"); err == nil || captureCount(t, l) != 0 {
		t.Fatalf("invalid fault caused a charge: %v", err)
	}
}

func TestQuoteExpiresAndAbsentProviderResultIsNotFailure(t *testing.T) {
	dir := t.TempDir()
	now := fixedNow
	l, err := Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	ctx := context.Background()
	q, err := l.CreateQuote(ctx, "customer-1", "basic")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(16 * time.Minute)
	if _, err := l.AcceptQuote(ctx, q.ID, q.Fingerprint, "expired"); !errors.Is(err, ErrExpired) {
		t.Fatalf("expired quote accepted: %v", err)
	}
	now = fixedNow
	r := purchase(t, l)
	found, err := l.ReconcilePayment(ctx, r.OperationID)
	if err != nil || found {
		t.Fatalf("unattempted provider lookup found an outcome: found=%v err=%v", found, err)
	}
	if s := snapshot(t, l, r.SubscriptionID); s.OperationStatus != "created" || s.AllocationCount != 0 {
		t.Fatalf("absence was treated as failure or success: %+v", s)
	}
}
