package lab

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestBoundChangeQuoteCannotBecomeSecondPurchase(t *testing.T) {
	l, _ := openChangingLab(t)
	ctx := context.Background()
	paid := paidBasicSubscription(t, l, "bound-change-customer")
	for _, mode := range []string{"next_period", "immediate"} {
		quote, err := l.CreateQuoteForCohort(ctx, "bound-change-customer", "pro", "default", 5)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := l.BindChangeQuote(ctx, quote.ID, paid.SubscriptionID, mode, 1); err != nil {
			t.Fatal(err)
		}
		if _, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "wrong-purchase:"+mode); !errors.Is(err, ErrConflict) {
			t.Fatalf("%s bound quote accepted as new purchase: %v", mode, err)
		}
		var subscriptions, invoices int
		if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM subscriptions WHERE quote_id=?`, quote.ID).Scan(&subscriptions); err != nil {
			t.Fatal(err)
		}
		if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM invoices WHERE subscription_id IN (SELECT id FROM subscriptions WHERE quote_id=?)`, quote.ID).Scan(&invoices); err != nil {
			t.Fatal(err)
		}
		if subscriptions != 0 || invoices != 0 {
			t.Fatalf("%s bound quote created purchase facts: subscriptions=%d invoices=%d", mode, subscriptions, invoices)
		}
	}
}

func TestAcceptPreviewCannotPurchaseQuoteBoundAfterPreview(t *testing.T) {
	l, _ := openChangingLab(t)
	ctx := context.Background()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	paid := paidBasicSubscription(t, l, "bound-after-preview")
	quote, err := l.CreateQuoteForCohort(ctx, "bound-after-preview", "pro", "default", 5)
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
	if _, err := l.BindChangeQuote(ctx, quote.ID, paid.SubscriptionID, "next_period", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := l.AdminCreatePreview(ctx, "local-admin", "C02", quote.ID, payload); !errors.Is(err, ErrConflict) {
		t.Fatalf("bound quote allowed new purchase preview: %v", err)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "bound-after-preview-purchase", "C02", quote.ID, payload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	command, err = l.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "failed" || command.ErrorCode != "PREVIEW_STALE" {
		t.Fatalf("old purchase preview executed bound quote: %+v err=%v", command, err)
	}
	var subscriptions, receipts int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM subscriptions WHERE quote_id=?`, quote.ID).Scan(&subscriptions); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if subscriptions != 0 || receipts != 0 {
		t.Fatalf("bound quote created purchase facts: subscriptions=%d receipts=%d", subscriptions, receipts)
	}
}
