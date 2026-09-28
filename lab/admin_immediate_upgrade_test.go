package lab

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestAdminImmediateUpgradeAtomicAndBoundedByPreview(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	now := time.Now().UTC().Truncate(time.Second)
	l, err := Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	sub := paidBasicSubscription(t, l, "admin-immediate-customer")
	quote, err := l.CreateQuoteForCohort(ctx, "admin-immediate-customer", "pro", "default", 5)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := l.BindChangeQuote(ctx, quote.ID, sub.SubscriptionID, "immediate", 1)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(AdminChangePlanPayload{QuoteID: quote.ID, Fingerprint: binding.Fingerprint, Revision: "1"})
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C04", sub.SubscriptionID, payload)
	if err != nil {
		t.Fatal(err)
	}
	var impact map[string]string
	if err := json.Unmarshal(preview.Impact, &impact); err != nil {
		t.Fatal(err)
	}
	if impact["net_minor"] == "" || impact["target_price_version_id"] != "pro-v1" {
		t.Fatalf("wrong impact: %+v", impact)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "immediate-upgrade-001", "C04", sub.SubscriptionID, payload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	command, err = l.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "succeeded" {
		t.Fatalf("execute: %+v %v", command, err)
	}
	var refs map[string]string
	if err := json.Unmarshal(command.ResultRefs, &refs); err != nil {
		t.Fatal(err)
	}
	if refs["change_id"] == "" || refs["invoice_id"] == "" || refs["operation_id"] == "" || refs["net_minor"] != impact["net_minor"] {
		t.Fatalf("wrong refs: %+v", refs)
	}
	var changes, receipts, outbox int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM immediate_changes WHERE id=?`, refs["change_id"]).Scan(&changes); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox WHERE kind='capture' AND object_id=?`, refs["operation_id"]).Scan(&outbox); err != nil {
		t.Fatal(err)
	}
	if changes != 1 || receipts != 1 || outbox != 1 {
		t.Fatalf("inconsistent facts: changes=%d receipts=%d outbox=%d", changes, receipts, outbox)
	}
}

func TestAdminImmediateUpgradeReceiptFailureRollsBackAndRecoversOriginalCommand(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	commerce, provider := filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db")
	now := time.Now().UTC().Truncate(time.Second)
	open := func() *Lab {
		t.Helper()
		l, err := Open(commerce, provider, func() time.Time { return now })
		if err != nil {
			t.Fatal(err)
		}
		if err := l.InitAdmin(ctx); err != nil {
			t.Fatal(err)
		}
		return l
	}
	l := open()
	paid := paidBasicSubscription(t, l, "immediate-receipt-failure")
	quote, err := l.CreateQuoteForCohort(ctx, "immediate-receipt-failure", "pro", "default", 5)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := l.BindChangeQuote(ctx, quote.ID, paid.SubscriptionID, "immediate", 1)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(AdminChangePlanPayload{QuoteID: quote.ID, Fingerprint: binding.Fingerprint, Revision: "1"})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C04", paid.SubscriptionID, payload)
	if err != nil {
		t.Fatal(err)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "immediate-receipt-failure-key", "C04", paid.SubscriptionID, payload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	tables := map[string]int{
		"immediate_changes":      1,
		"invoices":               1,
		"invoice_lines":          2,
		"payment_operations":     1,
		"supplemental_invoices":  1,
		"outbox":                 1,
		"audit_events":           1,
		"admin_command_receipts": 1,
	}
	readCounts := func() map[string]int {
		t.Helper()
		counts := make(map[string]int, len(tables))
		for table := range tables {
			var count int
			if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&count); err != nil {
				t.Fatalf("count %s: %v", table, err)
			}
			counts[table] = count
		}
		return counts
	}
	before := readCounts()
	var revisionBefore int64
	if err := l.db.QueryRowContext(ctx, `SELECT revision FROM subscriptions WHERE id=?`, paid.SubscriptionID).Scan(&revisionBefore); err != nil {
		t.Fatal(err)
	}
	capturesBefore := captureCount(t, l)
	if _, err := l.db.ExecContext(ctx, `CREATE TEMP TRIGGER fail_immediate_receipt BEFORE INSERT ON admin_command_receipts BEGIN SELECT RAISE(ABORT, 'injected receipt failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := l.AdminExecuteCommand(ctx, command.ID); err == nil {
		t.Fatal("receipt failure unexpectedly completed immediate upgrade")
	}
	if held, err := l.AdminCommand(ctx, command.ID); err != nil || held.Status != "accepted" {
		t.Fatalf("failed receipt did not retain original command %+v %v", held, err)
	}
	afterFailure := readCounts()
	for table, want := range before {
		if got := afterFailure[table]; got != want {
			t.Fatalf("%s changed after failed receipt: got %d, want %d", table, got, want)
		}
	}
	var revision int64
	if err := l.db.QueryRowContext(ctx, `SELECT revision FROM subscriptions WHERE id=?`, paid.SubscriptionID).Scan(&revision); err != nil || revision != revisionBefore {
		t.Fatalf("subscription revision after failed receipt = %d, want %d: %v", revision, revisionBefore, err)
	}
	if got := captureCount(t, l); got != capturesBefore {
		t.Fatalf("provider captures after failed receipt = %d, want %d", got, capturesBefore)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	l = open()
	defer l.Close()
	for i := 0; i < 2; i++ {
		if err := l.AdminResumeAccepted(ctx); err != nil {
			t.Fatal(err)
		}
	}
	command, err = l.AdminCommand(ctx, command.ID)
	if err != nil || command.Status != "succeeded" {
		t.Fatalf("recovered immediate upgrade %+v %v", command, err)
	}
	var refs map[string]string
	if err := json.Unmarshal(command.ResultRefs, &refs); err != nil || refs["change_id"] == "" || refs["invoice_id"] == "" || refs["operation_id"] == "" {
		t.Fatalf("recovered result refs %+v %v", refs, err)
	}
	after := readCounts()
	for table, beforeCount := range before {
		if want := beforeCount + tables[table]; after[table] != want {
			t.Fatalf("%s after recovery = %d, want %d", table, after[table], want)
		}
	}
	if err := l.db.QueryRowContext(ctx, `SELECT revision FROM subscriptions WHERE id=?`, paid.SubscriptionID).Scan(&revision); err != nil || revision != revisionBefore+1 {
		t.Fatalf("subscription revision after recovery = %d, want %d: %v", revision, revisionBefore+1, err)
	}
	if got := captureCount(t, l); got != capturesBefore {
		t.Fatalf("recovery unexpectedly dispatched provider capture: got %d, want %d", got, capturesBefore)
	}
	replayed, replay, err := l.AdminSubmitCommand(ctx, "local-admin", "immediate-receipt-failure-key", "C04", paid.SubscriptionID, payload, preview.ID)
	if err != nil || !replay || replayed.ID != command.ID || replayed.Status != "succeeded" {
		t.Fatalf("replay changed immediate upgrade %+v replay=%t err=%v", replayed, replay, err)
	}
	changed, err := json.Marshal(AdminChangePlanPayload{QuoteID: quote.ID, Fingerprint: binding.Fingerprint, Revision: "2"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := l.AdminSubmitCommand(ctx, "local-admin", "immediate-receipt-failure-key", "C04", paid.SubscriptionID, changed, preview.ID); !errors.Is(err, ErrAdminIdempotencyConflict) {
		t.Fatalf("changed payload reused original key: %v", err)
	}
}

func TestAdminImmediateUpgradePreviewAmountOnlyAllowsDecrease(t *testing.T) {
	for _, tc := range []struct {
		name      string
		shift     time.Duration
		wantStale bool
	}{
		{"increased_amount", -time.Hour, true},
		{"decreased_amount", 14 * time.Minute, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			l, clock := openChangingLab(t)
			if err := l.InitAdmin(ctx); err != nil {
				t.Fatal(err)
			}
			paid := paidBasicSubscription(t, l, "preview-bound-"+tc.name)
			*clock = time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)
			quote, err := l.CreateQuoteForCohort(ctx, "preview-bound-"+tc.name, "pro", "default", 50)
			if err != nil {
				t.Fatal(err)
			}
			binding, err := l.BindChangeQuote(ctx, quote.ID, paid.SubscriptionID, "immediate", 1)
			if err != nil {
				t.Fatal(err)
			}
			payload, err := json.Marshal(AdminChangePlanPayload{QuoteID: quote.ID, Fingerprint: binding.Fingerprint, Revision: "1"})
			if err != nil {
				t.Fatal(err)
			}
			amount := func(preview AdminPreview) int64 {
				t.Helper()
				var impact map[string]string
				if err := json.Unmarshal(preview.Impact, &impact); err != nil {
					t.Fatal(err)
				}
				net, err := strconv.ParseInt(impact["net_minor"], 10, 64)
				if err != nil {
					t.Fatal(err)
				}
				return net
			}
			original, err := l.AdminCreatePreview(ctx, "local-admin", "C04", paid.SubscriptionID, payload)
			if err != nil {
				t.Fatal(err)
			}
			originalNet := amount(original)
			*clock = clock.Add(tc.shift)
			fresh, err := l.AdminCreatePreview(ctx, "local-admin", "C04", paid.SubscriptionID, payload)
			if err != nil {
				t.Fatal(err)
			}
			freshNet := amount(fresh)
			if tc.wantStale && freshNet <= originalNet || !tc.wantStale && freshNet >= originalNet {
				t.Fatalf("amount did not move as expected: original=%d fresh=%d", originalNet, freshNet)
			}
			command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "preview-bound-"+tc.name, "C04", paid.SubscriptionID, payload, original.ID)
			if err != nil {
				t.Fatal(err)
			}
			command, err = l.AdminExecuteCommand(ctx, command.ID)
			if err != nil {
				t.Fatal(err)
			}
			var changes int
			if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM immediate_changes WHERE subscription_id=?`, paid.SubscriptionID).Scan(&changes); err != nil {
				t.Fatal(err)
			}
			if tc.wantStale {
				if command.Status != "failed" || command.ErrorCode != "PREVIEW_STALE" || changes != 0 {
					t.Fatalf("increased amount escaped preview bound: %+v changes=%d", command, changes)
				}
			} else if command.Status != "succeeded" || changes != 1 {
				t.Fatalf("decreased amount was rejected: %+v changes=%d", command, changes)
			}
		})
	}
}
