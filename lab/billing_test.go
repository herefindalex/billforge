package lab

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestS02MonthEndAnchorAndIdempotentRenewal(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2027, 1, 31, 9, 30, 0, 0, time.UTC)
	l, err := Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	ctx := context.Background()
	r := purchase(t, l)
	if _, err := l.DispatchNext(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if err := l.RebuildEntitlements(ctx); err != nil {
		t.Fatal(err)
	}
	periods, err := l.Periods(ctx, r.SubscriptionID)
	if err != nil || len(periods) != 1 || periods[0].End.Day() != 28 || periods[0].End.Month() != time.February {
		t.Fatalf("initial anchored period: %+v, %v", periods, err)
	}
	now = periods[0].End
	renewed, err := l.RunRenewals(ctx)
	if err != nil || len(renewed) != 1 || renewed[0].AmountMinor != 2000 {
		t.Fatalf("first renewal: %+v, %v", renewed, err)
	}
	if again, err := l.RunRenewals(ctx); err != nil || len(again) != 0 {
		t.Fatalf("duplicate renewal: %+v, %v", again, err)
	}
	var originalQuoteID, originalFingerprint string
	if err := l.db.QueryRow(`SELECT q.id,q.fingerprint FROM subscriptions s JOIN quotes q ON q.id=s.quote_id WHERE s.id=?`, r.SubscriptionID).
		Scan(&originalQuoteID, &originalFingerprint); err != nil {
		t.Fatal(err)
	}
	if replay, err := l.AcceptQuote(ctx, originalQuoteID, originalFingerprint, "checkout-1"); err != nil || replay != r {
		t.Fatalf("original checkout replay selected renewal invoice: %+v, %v", replay, err)
	}
	periods, err = l.Periods(ctx, r.SubscriptionID)
	if err != nil || len(periods) != 2 || periods[1].Start != now || periods[1].End.Day() != 31 || periods[1].End.Month() != time.March {
		t.Fatalf("February clamp did not return to 31st: %+v, %v", periods, err)
	}
	if err := l.RefreshEntitlements(ctx); err != nil {
		t.Fatal(err)
	}
	if got := snapshot(t, l, r.SubscriptionID).EntitlementStatus; got != "grace" {
		t.Fatalf("unpaid renewal should be grace, got %q", got)
	}
	if _, err := l.DispatchNext(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if err := l.RefreshEntitlements(ctx); err != nil {
		t.Fatal(err)
	}
	if got := snapshot(t, l, r.SubscriptionID); got.EntitlementStatus != "active" || got.AllocatedMinor != 2000 {
		t.Fatalf("paid renewal did not activate: %+v", got)
	}
	now = periods[1].End
	if renewed, err := l.RunRenewals(ctx); err != nil || len(renewed) != 1 {
		t.Fatalf("March renewal: %+v, %v", renewed, err)
	}
	periods, _ = l.Periods(ctx, r.SubscriptionID)
	if len(periods) != 3 || periods[2].End.Month() != time.April || periods[2].End.Day() != 30 {
		t.Fatalf("April clamp wrong: %+v", periods)
	}
}

func TestDelayedInitialConfirmationMovesServiceAnchor(t *testing.T) {
	dir := t.TempDir()
	now := fixedNow
	l, err := Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	ctx := context.Background()
	r := purchase(t, l)
	if _, err := l.DispatchNext(ctx, "lost_response"); !errors.Is(err, ErrPaymentUnknown) {
		t.Fatalf("expected unknown: %v", err)
	}
	now = time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC)
	if found, err := l.ReconcilePayment(ctx, r.OperationID); err != nil || !found {
		t.Fatalf("payment lookup: found=%v err=%v", found, err)
	}
	if err := l.RefreshEntitlements(ctx); err != nil {
		t.Fatal(err)
	}
	periods, err := l.Periods(ctx, r.SubscriptionID)
	if err != nil || len(periods) != 1 || periods[0].Start != now || periods[0].End != time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC) {
		t.Fatalf("service month began before activation: %+v, %v", periods, err)
	}
	if _, err := l.db.Exec(`UPDATE billing_periods SET period_start=period_start+1 WHERE subscription_id=? AND period_index=0`, r.SubscriptionID); err == nil {
		t.Fatal("effective service period was mutable")
	}
	now = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if renewed, err := l.RunRenewals(ctx); err != nil || len(renewed) != 0 {
		t.Fatalf("renewed at quote acceptance anchor: %+v, %v", renewed, err)
	}
	now = periods[0].End
	if renewed, err := l.RunRenewals(ctx); err != nil || len(renewed) != 1 {
		t.Fatalf("did not renew at activation anchor: %+v, %v", renewed, err)
	}
}

func TestS03FailedRenewalGraceSuspensionAndRecovery(t *testing.T) {
	dir := t.TempDir()
	now := fixedNow
	local, provider := filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db")
	l, err := Open(local, provider, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	ctx := context.Background()
	r := purchase(t, l)
	if _, err := l.DispatchNext(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if err := l.RebuildEntitlements(ctx); err != nil {
		t.Fatal(err)
	}
	now = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	renewed, err := l.RunRenewals(ctx)
	if err != nil || len(renewed) != 1 {
		t.Fatalf("renewal: %+v, %v", renewed, err)
	}
	if err := l.SetFakePaymentDecision(ctx, renewed[0].OperationID, "definitively_failed"); err != nil {
		t.Fatal(err)
	}
	if _, err := l.DispatchNext(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if err := l.RefreshEntitlements(ctx); err != nil {
		t.Fatal(err)
	}
	s := snapshot(t, l, r.SubscriptionID)
	if s.SubscriptionStatus != "active" || s.OperationStatus != "definitively_failed" || s.AllocationCount != 0 || s.EntitlementStatus != "grace" {
		t.Fatalf("failed renewal state: %+v", s)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	l, err = Open(local, provider, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	now = now.Add(7 * 24 * time.Hour)
	if err := l.RefreshEntitlements(ctx); err != nil {
		t.Fatal(err)
	}
	if got := snapshot(t, l, r.SubscriptionID).EntitlementStatus; got != "suspended" {
		t.Fatalf("grace deadline should suspend, got %q", got)
	}
	retryID, err := l.RetryFailedPayment(ctx, renewed[0].InvoiceID, "retry-october")
	if err != nil || retryID == renewed[0].OperationID {
		t.Fatalf("new operation required: %q, %v", retryID, err)
	}
	if replay, err := l.RetryFailedPayment(ctx, renewed[0].InvoiceID, "retry-october"); err != nil || replay != retryID {
		t.Fatalf("retry command replay changed operation: %q, %v", replay, err)
	}
	if _, err := l.DispatchNext(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if err := l.RefreshEntitlements(ctx); err != nil {
		t.Fatal(err)
	}
	s = snapshot(t, l, r.SubscriptionID)
	if s.EntitlementStatus != "active" || s.AllocatedMinor != 2000 || s.AllocationCount != 1 {
		t.Fatalf("recovery state: %+v", s)
	}
	if _, err := l.RetryFailedPayment(ctx, renewed[0].InvoiceID, "another-retry"); !errors.Is(err, ErrConflict) {
		t.Fatalf("paid invoice retried: %v", err)
	}
	if later, err := l.RunRenewals(ctx); err != nil || len(later) != 0 {
		t.Fatalf("same period reissued: %+v, %v", later, err)
	}
}

func TestUnknownRenewalRetainsVerificationGrace(t *testing.T) {
	dir := t.TempDir()
	now := fixedNow
	l, err := Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	ctx := context.Background()
	r := purchase(t, l)
	if _, err := l.DispatchNext(ctx, ""); err != nil {
		t.Fatal(err)
	}
	now = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	renewed, err := l.RunRenewals(ctx)
	if err != nil || len(renewed) != 1 {
		t.Fatalf("renewal: %+v, %v", renewed, err)
	}
	if _, err := l.DispatchNext(ctx, "lost_response"); !errors.Is(err, ErrPaymentUnknown) {
		t.Fatalf("expected unknown: %v", err)
	}
	now = now.Add(8 * 24 * time.Hour)
	if err := l.RefreshEntitlements(ctx); err != nil {
		t.Fatal(err)
	}
	if got := snapshot(t, l, r.SubscriptionID).EntitlementStatus; got != "grace" {
		t.Fatalf("unknown provider outcome suspended prematurely: %q", got)
	}
	if found, err := l.ReconcilePayment(ctx, renewed[0].OperationID); err != nil || !found {
		t.Fatalf("lookup: found=%v err=%v", found, err)
	}
	if err := l.RefreshEntitlements(ctx); err != nil {
		t.Fatal(err)
	}
	if got := snapshot(t, l, r.SubscriptionID).EntitlementStatus; got != "active" {
		t.Fatalf("confirmed payment did not activate: %q", got)
	}
}

func TestMissedRenewalGraceCreatesHoldInsteadOfBackBilling(t *testing.T) {
	dir := t.TempDir()
	now := fixedNow
	l, err := Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	ctx := context.Background()
	r := purchase(t, l)
	if _, err := l.DispatchNext(ctx, ""); err != nil {
		t.Fatal(err)
	}
	now = time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 2; i++ {
		if renewed, err := l.RunRenewals(ctx); err != nil || len(renewed) != 0 {
			t.Fatalf("late worker back-billed service: %+v, %v", renewed, err)
		}
	}
	periods, err := l.Periods(ctx, r.SubscriptionID)
	if err != nil || len(periods) != 1 {
		t.Fatalf("unexpected period count: %+v, %v", periods, err)
	}
	var holds int
	if err := l.db.QueryRow(`SELECT COUNT(*) FROM renewal_holds WHERE subscription_id=?`, r.SubscriptionID).Scan(&holds); err != nil || holds != 1 {
		t.Fatalf("missing stable operational hold: %d, %v", holds, err)
	}
	if err := l.RefreshEntitlements(ctx); err != nil {
		t.Fatal(err)
	}
	if got := snapshot(t, l, r.SubscriptionID).EntitlementStatus; got != "suspended" {
		t.Fatalf("no current paid period should suspend: %q", got)
	}
}

func TestLateFullAmountDispatchRequiresReview(t *testing.T) {
	dir := t.TempDir()
	now := fixedNow
	l, err := Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	ctx := context.Background()
	purchase(t, l)
	if _, err := l.DispatchNext(ctx, ""); err != nil {
		t.Fatal(err)
	}
	now = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if renewed, err := l.RunRenewals(ctx); err != nil || len(renewed) != 1 {
		t.Fatalf("renewal: %+v, %v", renewed, err)
	}
	now = time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	if _, err := l.DispatchNext(ctx, ""); !errors.Is(err, ErrLateNeedsReview) {
		t.Fatalf("late full charge was dispatched: %v", err)
	}
	if captureCount(t, l) != 1 {
		t.Fatal("late renewal caused a second provider capture")
	}
}

func TestFailedPeriodCannotBeChargedAfterItEnds(t *testing.T) {
	dir := t.TempDir()
	now := fixedNow
	l, err := Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	ctx := context.Background()
	purchase(t, l)
	if _, err := l.DispatchNext(ctx, ""); err != nil {
		t.Fatal(err)
	}
	now = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	renewed, err := l.RunRenewals(ctx)
	if err != nil || len(renewed) != 1 {
		t.Fatalf("renewal: %+v, %v", renewed, err)
	}
	if err := l.SetFakePaymentDecision(ctx, renewed[0].OperationID, "definitively_failed"); err != nil {
		t.Fatal(err)
	}
	if _, err := l.DispatchNext(ctx, ""); err != nil {
		t.Fatal(err)
	}
	now = time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	if _, err := l.RetryFailedPayment(ctx, renewed[0].InvoiceID, "needs-correction"); !errors.Is(err, ErrLateNeedsReview) {
		t.Fatalf("late payment did not require correction review: %v", err)
	}
	now = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	if _, err := l.RetryFailedPayment(ctx, renewed[0].InvoiceID, "late-retry"); !errors.Is(err, ErrPeriodEnded) {
		t.Fatalf("expired service charged in full: %v", err)
	}
}

func TestUpgradeStageEDatabasesPreservesFacts(t *testing.T) {
	dir := t.TempDir()
	local, provider := filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db")
	db, err := sql.Open("sqlite3", local)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE quotes(id TEXT PRIMARY KEY,customer_id TEXT,price_version_id TEXT,amount_minor INTEGER,currency TEXT,expires_at INTEGER,fingerprint TEXT);
CREATE TABLE subscriptions(id TEXT PRIMARY KEY,quote_id TEXT,customer_id TEXT,price_version_id TEXT,status TEXT,created_at INTEGER);
CREATE TABLE invoices(id TEXT PRIMARY KEY,subscription_id TEXT NOT NULL UNIQUE REFERENCES subscriptions(id),total_minor INTEGER,currency TEXT,finalized_at INTEGER);
CREATE TABLE invoice_lines(invoice_id TEXT REFERENCES invoices(id),price_version_id TEXT,component_code TEXT,amount_minor INTEGER,PRIMARY KEY(invoice_id,component_code));
CREATE TABLE payment_operations(id TEXT PRIMARY KEY,invoice_id TEXT NOT NULL UNIQUE REFERENCES invoices(id),provider_key TEXT UNIQUE,amount_minor INTEGER,currency TEXT,status TEXT CHECK(status IN ('created','submitted','unknown','succeeded')));
CREATE TABLE entitlements(subscription_id TEXT PRIMARY KEY REFERENCES subscriptions(id),status TEXT,source_operation_id TEXT NOT NULL REFERENCES payment_operations(id),updated_at INTEGER);
INSERT INTO quotes VALUES('old-quote','old-customer','basic-v1',2000,'USD',0,'old-fingerprint');
INSERT INTO subscriptions VALUES('old-sub','old-quote','old-customer','basic-v1','pending',1788220800000000000);
INSERT INTO invoices VALUES('old-invoice','old-sub',2000,'USD',1788220800000000000);
INSERT INTO invoice_lines VALUES('old-invoice','basic-v1','fixed',2000);
INSERT INTO payment_operations VALUES('old-op','old-invoice','capture:old-invoice',2000,'USD','submitted');`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	pdb, err := sql.Open("sqlite3", provider)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pdb.Exec(`CREATE TABLE captures(provider_key TEXT PRIMARY KEY,amount_minor INTEGER,currency TEXT,status TEXT NOT NULL CHECK(status = 'succeeded'));
INSERT INTO captures VALUES('capture:old-invoice',2000,'USD','succeeded');`)
	if err != nil {
		t.Fatal(err)
	}
	pdb.Close()
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	l, err := Open(local, provider, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	ctx := context.Background()
	periods, err := l.Periods(ctx, "old-sub")
	if err != nil || len(periods) != 1 || periods[0].InvoiceID != "old-invoice" {
		t.Fatalf("initial period backfill: %+v, %v", periods, err)
	}
	var lineAmount int64
	if err := l.db.QueryRow(`SELECT amount_minor FROM invoice_lines WHERE invoice_id='old-invoice'`).Scan(&lineAmount); err != nil || lineAmount != 2000 {
		t.Fatalf("old invoice line not preserved: amount=%d err=%v", lineAmount, err)
	}
	if found, err := l.ReconcilePayment(ctx, "old-op"); err != nil || !found {
		t.Fatalf("old provider outcome lost: found=%v err=%v", found, err)
	}
	if err := l.RebuildEntitlements(ctx); err != nil {
		t.Fatal(err)
	}
	now = periods[0].End
	if renewed, err := l.RunRenewals(ctx); err != nil || len(renewed) != 1 {
		t.Fatalf("migrated subscription cannot renew: %+v, %v", renewed, err)
	}
}
