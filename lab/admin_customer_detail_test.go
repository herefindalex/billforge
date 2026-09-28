package lab

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

func TestAdminCustomerReadsUseStableCursorAndBoundedRelatedRows(t *testing.T) {
	l, _ := openChangingLab(t)
	ctx := context.Background()
	receipt := paidBasicSubscription(t, l, "customer-b")
	if _, err := l.CreateQuote(ctx, "customer-d", "basic"); err != nil {
		t.Fatal(err)
	}
	first, total, next, err := l.AdminCustomerPage(ctx, "", 1)
	if err != nil || total != 2 || len(first) != 1 || first[0].ID != "customer-b" || next != "customer-b" {
		t.Fatalf("first page: items=%+v total=%d next=%q err=%v", first, total, next, err)
	}
	if _, err := l.CreateQuote(ctx, "customer-a", "basic"); err != nil {
		t.Fatal(err)
	}
	second, total, next, err := l.AdminCustomerPage(ctx, next, 1)
	if err != nil || total != 3 || len(second) != 1 || second[0].ID != "customer-d" || next != "" {
		t.Fatalf("second page: items=%+v total=%d next=%q err=%v", second, total, next, err)
	}
	detail, err := l.AdminCustomerDetail(ctx, "customer-b")
	if err != nil {
		t.Fatal(err)
	}
	if detail.SubscriptionCount != 1 || detail.QuoteCount != 1 || detail.InvoiceCount != 1 || len(detail.Subscriptions) != 1 || detail.Subscriptions[0].ID != receipt.SubscriptionID || len(detail.Invoices) != 1 || detail.Invoices[0].ID != receipt.InvoiceID {
		t.Fatalf("unexpected customer detail: %+v", detail)
	}
	if _, err := l.AdminCustomerDetail(ctx, "missing-customer"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing customer error = %v", err)
	}
}
