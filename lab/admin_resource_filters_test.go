package lab

import (
	"context"
	"errors"
	"testing"
)

func TestAdminFilteredResourcePageKeepsMembershipAndCursorStable(t *testing.T) {
	l, _, _ := openTestLab(t)
	ctx := context.Background()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	firstQuote, err := l.CreateQuote(ctx, "filter-customer-a", "basic")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.CreateQuote(ctx, "filter-customer-b", "basic"); err != nil {
		t.Fatal(err)
	}
	if _, err := l.CreateQuote(ctx, "filter-customer-a", "basic"); err != nil {
		t.Fatal(err)
	}
	filters := map[string]string{"customer_id": "filter-customer-a"}
	first, total, cursor, err := l.AdminFilteredResourcePage(ctx, "quotes", 0, 1, filters)
	if err != nil || total != 2 || len(first) != 1 || cursor == 0 || first[0]["ID"] != firstQuote.ID {
		t.Fatalf("first filtered page=%v total=%d cursor=%d err=%v", first, total, cursor, err)
	}
	if _, err := l.CreateQuote(ctx, "filter-customer-b", "basic"); err != nil {
		t.Fatal(err)
	}
	second, total, next, err := l.AdminFilteredResourcePage(ctx, "quotes", cursor, 1, filters)
	if err != nil || total != 2 || len(second) != 1 || next != 0 || second[0]["ID"] == first[0]["ID"] {
		t.Fatalf("second filtered page=%v total=%d cursor=%d err=%v", second, total, next, err)
	}

	byID, total, _, err := l.AdminFilteredResourcePage(ctx, "quotes", 0, 10, map[string]string{"id_prefix": firstQuote.ID})
	if err != nil || total != 1 || len(byID) != 1 || byID[0]["ID"] != firstQuote.ID {
		t.Fatalf("literal ID prefix page=%v total=%d err=%v", byID, total, err)
	}
	wildcard, total, _, err := l.AdminFilteredResourcePage(ctx, "quotes", 0, 10, map[string]string{"id_prefix": "%"})
	if err != nil || total != 0 || len(wildcard) != 0 {
		t.Fatalf("wildcard was treated as pattern: page=%v total=%d err=%v", wildcard, total, err)
	}
	if _, err := l.AcceptQuote(ctx, firstQuote.ID, firstQuote.Fingerprint, "filter-checkout"); err != nil {
		t.Fatal(err)
	}
	payments, total, _, err := l.AdminFilteredResourcePage(ctx, "payments", 0, 10, map[string]string{"status": "created"})
	if err != nil || total != 1 || len(payments) != 1 || payments[0]["Status"] != "created" {
		t.Fatalf("payment status filter=%v total=%d err=%v", payments, total, err)
	}
	for _, tc := range []struct {
		resource string
		filters  map[string]string
	}{
		{"quotes", map[string]string{"status": "active"}},
		{"quotes", map[string]string{"customer_id": ""}},
		{"quotes", map[string]string{"customer_id": "bad\nvalue"}},
		{"payments", map[string]string{"unexpected": "value"}},
	} {
		if _, _, _, err := l.AdminFilteredResourcePage(ctx, tc.resource, 0, 10, tc.filters); !errors.Is(err, ErrAdminInvalidCommand) {
			t.Fatalf("%s filters %v accepted: %v", tc.resource, tc.filters, err)
		}
	}
}

func TestEveryResourceFilterUsesAnExistingColumn(t *testing.T) {
	l, _, _ := openTestLab(t)
	ctx := context.Background()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	for resource := range adminResourceSpecs {
		allowed, ok := adminResourceFilterColumns[resource]
		if !ok {
			t.Fatalf("%s has no filter declaration", resource)
		}
		for key := range allowed {
			t.Run(resource+"/"+key, func(t *testing.T) {
				value := "missing-filter-value"
				if key == "created_from" || key == "created_before" {
					value = "2026-01-01T00:00:00Z"
				}
				if key == "period_index" {
					value = "0"
				}
				if _, _, _, err := l.AdminFilteredResourcePage(ctx, resource, 0, 1, map[string]string{key: value}); err != nil {
					t.Fatalf("declared filter did not query: %v", err)
				}
			})
		}
	}
}

func TestAdminResourceCreatedRangeUsesUTCAndExclusiveUpperBound(t *testing.T) {
	l, _, _ := openTestLab(t)
	ctx := context.Background()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	quote, err := l.CreateQuote(ctx, "time-filter-customer", "basic")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "time-filter-checkout"); err != nil {
		t.Fatal(err)
	}
	all, _, _, err := l.AdminFilteredResourcePage(ctx, "subscriptions", 0, 10, nil)
	if err != nil || len(all) != 1 {
		t.Fatalf("all subscriptions=%v err=%v", all, err)
	}
	created, ok := all[0]["CreatedAt"].(string)
	if !ok {
		t.Fatalf("missing created time: %v", all[0])
	}
	include, total, _, err := l.AdminFilteredResourcePage(ctx, "subscriptions", 0, 10, map[string]string{"created_from": created})
	if err != nil || total != 1 || len(include) != 1 {
		t.Fatalf("inclusive lower bound items=%v total=%d err=%v", include, total, err)
	}
	exclude, total, _, err := l.AdminFilteredResourcePage(ctx, "subscriptions", 0, 10, map[string]string{"created_before": created})
	if err != nil || total != 0 || len(exclude) != 0 {
		t.Fatalf("exclusive upper bound items=%v total=%d err=%v", exclude, total, err)
	}
	for _, filters := range []map[string]string{
		{"created_from": "2026-01-01T00:00:00+00:00"},
		{"created_from": "invalid"},
		{"created_before": "1500-01-01T00:00:00Z"},
		{"created_from": created, "created_before": created},
	} {
		if _, _, _, err := l.AdminFilteredResourcePage(ctx, "subscriptions", 0, 10, filters); !errors.Is(err, ErrAdminInvalidCommand) {
			t.Fatalf("invalid time filters %v returned %v", filters, err)
		}
	}
	if _, _, _, err := l.AdminFilteredResourcePage(ctx, "quotes", 0, 10, map[string]string{"created_from": created}); !errors.Is(err, ErrAdminInvalidCommand) {
		t.Fatalf("quotes accepted unsupported created_from: %v", err)
	}
}
