package lab

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"testing"
	"time"
)

func TestAdminCreateChangeQuoteRollsBackWhenBindingRevisionIsStale(t *testing.T) {
	l, _ := openChangingLab(t)
	ctx := context.Background()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	customerID := "atomic-change-binding-customer"
	sub := paidBasicSubscription(t, l, customerID)

	var revision int64
	if err := l.db.QueryRowContext(ctx, `SELECT revision FROM subscriptions WHERE id=?`, sub.SubscriptionID).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	var quotesBefore int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM quotes WHERE customer_id=?`, customerID).Scan(&quotesBefore); err != nil {
		t.Fatal(err)
	}

	payload, err := json.Marshal(AdminCreateQuotePayload{
		CustomerID:           customerID,
		PlanID:               "pro",
		Cohort:               "default",
		Seats:                "5",
		ChangeSubscriptionID: sub.SubscriptionID,
		Mode:                 "next_period",
		Revision:             strconv.FormatInt(revision+1, 10),
	})
	if err != nil {
		t.Fatal(err)
	}
	command, replay, err := l.AdminSubmitCommand(ctx, "local-admin", "atomic-change-binding-stale", "C01", "", payload, "")
	if err != nil || replay {
		t.Fatalf("submit change quote: command=%+v replay=%t err=%v", command, replay, err)
	}
	command, err = l.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "failed" {
		t.Fatalf("stale binding must fail its command: command=%+v err=%v", command, err)
	}
	if command.ErrorCode != "CHANGE_QUOTE_REVISION_CHANGED" {
		t.Fatalf("stale binding must explain the revision conflict: %+v", command)
	}

	var quotesAfter, bindings, receipts int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM quotes WHERE customer_id=?`, customerID).Scan(&quotesAfter); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM change_quote_bindings WHERE subscription_id=?`, sub.SubscriptionID).Scan(&bindings); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if quotesAfter != quotesBefore || bindings != 0 || receipts != 0 {
		t.Fatalf("failed binding left a quote or receipt: quotes=%d→%d bindings=%d receipts=%d", quotesBefore, quotesAfter, bindings, receipts)
	}
}

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

func TestAdminChangePreviewRejectsSupersededPriceForBothModes(t *testing.T) {
	for _, scenario := range []struct{ mode, action string }{{"next_period", "C03"}, {"immediate", "C04"}} {
		t.Run(scenario.action, func(t *testing.T) {
			l, clock := openChangingLab(t)
			ctx := context.Background()
			if err := l.InitAdmin(ctx); err != nil {
				t.Fatal(err)
			}
			customerID := "superseded-price-" + scenario.action
			sub := paidBasicSubscription(t, l, customerID)
			var revision int64
			if err := l.db.QueryRowContext(ctx, `SELECT revision FROM subscriptions WHERE id=?`, sub.SubscriptionID).Scan(&revision); err != nil {
				t.Fatal(err)
			}
			quote, err := l.CreateQuoteForCohort(ctx, customerID, "pro", "default", 5)
			if err != nil {
				t.Fatal(err)
			}
			if quote.PriceVersionID != "pro-v1" || quote.AmountMinor != 10000 {
				t.Fatalf("unexpected original quote: %+v", quote)
			}
			binding, err := l.BindChangeQuote(ctx, quote.ID, sub.SubscriptionID, scenario.mode, revision)
			if err != nil {
				t.Fatal(err)
			}
			spec := proV2Spec()
			spec.EffectiveFrom = fixedNow.Add(time.Second)
			if _, err := l.PublishProPrice(ctx, spec); err != nil {
				t.Fatal(err)
			}
			if err := l.SelectCatalogPrice(ctx, "pro", "default", spec.EffectiveFrom, spec.ID); err != nil {
				t.Fatal(err)
			}
			*clock = fixedNow.Add(2 * time.Second)
			payload, err := json.Marshal(AdminChangePlanPayload{
				QuoteID: quote.ID, Fingerprint: binding.Fingerprint, Revision: strconv.FormatInt(revision, 10),
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := l.AdminCreatePreview(ctx, "local-admin", scenario.action, sub.SubscriptionID, payload); !errors.Is(err, ErrChangeQuotePriceSuperseded) || !errors.Is(err, ErrConflict) {
				t.Fatalf("superseded quote price should reject preview: %v", err)
			}
			for _, query := range []string{
				`SELECT COUNT(*) FROM admin_previews WHERE action_id=? AND target_id=?`,
				`SELECT COUNT(*) FROM admin_commands WHERE action_id=? AND target_id=?`,
			} {
				var count int
				if err := l.db.QueryRowContext(ctx, query, scenario.action, sub.SubscriptionID).Scan(&count); err != nil || count != 0 {
					t.Fatalf("rejected preview wrote admin state: count=%d err=%v", count, err)
				}
			}
			for _, query := range []string{
				`SELECT COUNT(*) FROM subscription_schedules WHERE subscription_id=?`,
				`SELECT COUNT(*) FROM immediate_changes WHERE subscription_id=?`,
			} {
				var count int
				if err := l.db.QueryRowContext(ctx, query, sub.SubscriptionID).Scan(&count); err != nil || count != 0 {
					t.Fatalf("rejected preview wrote financial state: count=%d err=%v", count, err)
				}
			}
			var price string
			var currentRevision int64
			if err := l.db.QueryRowContext(ctx, `SELECT price_version_id,revision FROM subscriptions WHERE id=?`, sub.SubscriptionID).Scan(&price, &currentRevision); err != nil || price != "basic-v1" || currentRevision != revision {
				t.Fatalf("subscription changed after rejected preview: price=%s revision=%d err=%v", price, currentRevision, err)
			}
		})
	}
}

func TestAdminChangePreviewRejectsChangedRevisionForBothModes(t *testing.T) {
	for _, scenario := range []struct{ mode, action string }{{"next_period", "C03"}, {"immediate", "C04"}} {
		t.Run(scenario.action, func(t *testing.T) {
			l, _ := openChangingLab(t)
			ctx := context.Background()
			if err := l.InitAdmin(ctx); err != nil {
				t.Fatal(err)
			}
			customerID := "changed-revision-" + scenario.action
			sub := paidBasicSubscription(t, l, customerID)
			var revision int64
			if err := l.db.QueryRowContext(ctx, `SELECT revision FROM subscriptions WHERE id=?`, sub.SubscriptionID).Scan(&revision); err != nil {
				t.Fatal(err)
			}
			quote, err := l.CreateQuoteForCohort(ctx, customerID, "pro", "default", 5)
			if err != nil {
				t.Fatal(err)
			}
			binding, err := l.BindChangeQuote(ctx, quote.ID, sub.SubscriptionID, scenario.mode, revision)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := l.db.ExecContext(ctx, `UPDATE subscriptions SET revision=revision+1 WHERE id=?`, sub.SubscriptionID); err != nil {
				t.Fatal(err)
			}
			payload, err := json.Marshal(AdminChangePlanPayload{
				QuoteID: quote.ID, Fingerprint: binding.Fingerprint, Revision: strconv.FormatInt(revision, 10),
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := l.AdminCreatePreview(ctx, "local-admin", scenario.action, sub.SubscriptionID, payload); !errors.Is(err, ErrChangeQuoteRevisionChanged) || !errors.Is(err, ErrConflict) {
				t.Fatalf("changed subscription revision should reject preview: %v", err)
			}
			for _, query := range []string{
				`SELECT COUNT(*) FROM admin_previews WHERE action_id=? AND target_id=?`,
				`SELECT COUNT(*) FROM admin_commands WHERE action_id=? AND target_id=?`,
			} {
				var count int
				if err := l.db.QueryRowContext(ctx, query, scenario.action, sub.SubscriptionID).Scan(&count); err != nil || count != 0 {
					t.Fatalf("changed revision wrote admin state: count=%d err=%v", count, err)
				}
			}
			for _, query := range []string{
				`SELECT COUNT(*) FROM subscription_schedules WHERE subscription_id=?`,
				`SELECT COUNT(*) FROM immediate_changes WHERE subscription_id=?`,
			} {
				var count int
				if err := l.db.QueryRowContext(ctx, query, sub.SubscriptionID).Scan(&count); err != nil || count != 0 {
					t.Fatalf("changed revision wrote financial state: count=%d err=%v", count, err)
				}
			}
		})
	}
}
