package lab

import (
	"context"
	"encoding/json"
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
