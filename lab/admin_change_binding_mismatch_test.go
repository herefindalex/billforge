package lab

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"testing"
)

func TestAdminChangePreviewRejectsAnotherQuotesBindingForBothModes(t *testing.T) {
	l, _ := openChangingLab(t)
	ctx := context.Background()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	sub := paidBasicSubscription(t, l, "binding-mismatch-customer")
	var revision int64
	if err := l.db.QueryRowContext(ctx, `SELECT revision FROM subscriptions WHERE id=?`, sub.SubscriptionID).Scan(&revision); err != nil {
		t.Fatal(err)
	}

	for _, scenario := range []struct{ mode, action string }{{"next_period", "C03"}, {"immediate", "C04"}} {
		t.Run(scenario.action, func(t *testing.T) {
			original, err := l.CreateQuoteForCohort(ctx, "binding-mismatch-customer", "pro", "default", 5)
			if err != nil {
				t.Fatal(err)
			}
			other, err := l.CreateQuoteForCohort(ctx, "binding-mismatch-customer", "pro", "default", 7)
			if err != nil {
				t.Fatal(err)
			}
			if original.AmountMinor == other.AmountMinor {
				t.Fatal("different seat counts did not produce different quote amounts")
			}
			binding, err := l.BindChangeQuote(ctx, original.ID, sub.SubscriptionID, scenario.mode, revision)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := l.BindChangeQuote(ctx, other.ID, sub.SubscriptionID, scenario.mode, revision); err != nil {
				t.Fatal(err)
			}
			payload, err := json.Marshal(AdminChangePlanPayload{QuoteID: other.ID, Fingerprint: binding.Fingerprint, Revision: strconv.FormatInt(revision, 10)})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := l.AdminCreatePreview(ctx, "local-admin", scenario.action, sub.SubscriptionID, payload); !errors.Is(err, ErrChangeQuoteBindingMismatch) || !errors.Is(err, ErrConflict) {
				t.Fatalf("mismatched quote binding must be a specific conflict: %v", err)
			}
			var previews, commands int
			if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_previews WHERE action_id=? AND target_id=?`, scenario.action, sub.SubscriptionID).Scan(&previews); err != nil {
				t.Fatal(err)
			}
			if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_commands WHERE action_id=? AND target_id=?`, scenario.action, sub.SubscriptionID).Scan(&commands); err != nil {
				t.Fatal(err)
			}
			if previews != 0 || commands != 0 {
				t.Fatalf("mismatched quote wrote preview or command: previews=%d commands=%d", previews, commands)
			}
		})
	}

	var schedules, upgrades int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM subscription_schedules WHERE subscription_id=? AND kind='change'`, sub.SubscriptionID).Scan(&schedules); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM immediate_changes WHERE subscription_id=?`, sub.SubscriptionID).Scan(&upgrades); err != nil {
		t.Fatal(err)
	}
	if schedules != 0 || upgrades != 0 {
		t.Fatalf("mismatched quote changed subscription: schedules=%d upgrades=%d", schedules, upgrades)
	}
}
