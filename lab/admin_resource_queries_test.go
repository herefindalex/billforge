package lab

import (
	"context"
	"testing"
	"time"
)

func TestAdminResourceTimestampZeroKeepsEpochButDraftPublishTimeIsUnknown(t *testing.T) {
	ctx := context.Background()
	l, _, _ := openTestLab(t)
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	selections, total, _, err := l.AdminFilteredResourcePage(ctx, "catalog-selections", 0, 10, map[string]string{"plan_id": "pro", "cohort": "default"})
	if err != nil || total == 0 || len(selections) == 0 {
		t.Fatalf("baseline selection: rows=%v total=%d err=%v", selections, total, err)
	}
	if selections[0]["EffectiveAt"] != "1970-01-01T00:00:00Z" {
		t.Fatalf("baseline effective time was hidden: %+v", selections[0])
	}
	for _, id := range []string{"basic-v1", "pro-v1"} {
		baseline, total, _, err := l.AdminFilteredResourcePage(ctx, "prices", 0, 10, map[string]string{"id_prefix": id})
		if err != nil || total != 1 || len(baseline) != 1 {
			t.Fatalf("baseline price %s: rows=%v total=%d err=%v", id, baseline, total, err)
		}
		if baseline[0]["PublishedAt"] != nil {
			t.Fatalf("seeded price %s claims a real publication time: %+v", id, baseline[0])
		}
	}
	if _, err := l.db.ExecContext(ctx, `INSERT INTO price_versions(id,plan_id,version,currency,fixed_amount_minor,published_at,checksum,publication_state,effective_from)
		VALUES('published-epoch','epoch-plan',2,'USD',100,0,'epoch-checksum','published',0)`); err != nil {
		t.Fatal(err)
	}
	published, total, _, err := l.AdminFilteredResourcePage(ctx, "prices", 0, 10, map[string]string{"id_prefix": "published-epoch"})
	if err != nil || total != 1 || len(published) != 1 || published[0]["PublishedAt"] != "1970-01-01T00:00:00Z" {
		t.Fatalf("real epoch publication was hidden: rows=%v total=%d err=%v", published, total, err)
	}
	if _, err := l.db.ExecContext(ctx, `INSERT INTO price_versions(id,plan_id,version,currency,fixed_amount_minor,published_at,checksum,publication_state,effective_from)
		VALUES('draft-epoch','epoch-plan',1,'USD',100,0,'','draft',0)`); err != nil {
		t.Fatal(err)
	}
	prices, total, _, err := l.AdminFilteredResourcePage(ctx, "prices", 0, 10, map[string]string{"id_prefix": "draft-epoch"})
	if err != nil || total != 1 || len(prices) != 1 {
		t.Fatalf("draft price: rows=%v total=%d err=%v", prices, total, err)
	}
	if prices[0]["PublishedAt"] != nil || prices[0]["EffectiveFrom"] != "1970-01-01T00:00:00Z" {
		t.Fatalf("draft publish and effective times conflated: %+v", prices[0])
	}
}

func TestAdminContractResourceFormatsBothEffectiveBoundsAsUTC(t *testing.T) {
	ctx := context.Background()
	l, _, _ := openTestLab(t)
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	from := fixedNow
	to := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	const id = "admin-resource-contract-time"
	_, err := l.PublishContract(ctx, ContractSpec{
		ID: id, CustomerID: "admin-resource-time-customer", Version: 1,
		BasePriceVersionID: "pro-v1", FixedMinor: 4000, SeatMinor: 700,
		EffectiveFrom: from, EffectiveTo: to,
	})
	if err != nil {
		t.Fatal(err)
	}
	rows, total, _, err := l.AdminFilteredResourcePage(ctx, "contracts", 0, 10, map[string]string{"id_prefix": id})
	if err != nil || total != 1 || len(rows) != 1 {
		t.Fatalf("contract resource: rows=%v total=%d err=%v", rows, total, err)
	}
	if rows[0]["EffectiveFrom"] != from.Format(time.RFC3339Nano) || rows[0]["EffectiveTo"] != to.Format(time.RFC3339Nano) {
		t.Fatalf("contract effective bounds need UTC strings: %+v", rows[0])
	}
}

func TestAdminResourceQueriesAndStableCursor(t *testing.T) {
	l, _, _ := openTestLab(t)
	ctx := context.Background()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	for name := range adminResourceSpecs {
		if _, _, _, err := l.AdminResourcePage(ctx, name, 0, 20); err != nil {
			t.Fatalf("resource %s: %v", name, err)
		}
	}
	for i := 0; i < 3; i++ {
		if _, err := l.CreateQuote(ctx, "cursor-customer-"+string(rune('a'+i)), "basic"); err != nil {
			t.Fatal(err)
		}
	}
	first, total, cursor, err := l.AdminResourcePage(ctx, "quotes", 0, 2)
	if err != nil || total != 3 || len(first) != 2 || cursor == 0 {
		t.Fatalf("first page len=%d total=%d cursor=%d err=%v", len(first), total, cursor, err)
	}
	if _, err := l.CreateQuote(ctx, "cursor-customer-new", "basic"); err != nil {
		t.Fatal(err)
	}
	second, total, next, err := l.AdminResourcePage(ctx, "quotes", cursor, 2)
	if err != nil || total != 4 || len(second) != 2 || next != 0 {
		t.Fatalf("second page len=%d total=%d cursor=%d err=%v", len(second), total, next, err)
	}
	seen := map[any]bool{}
	for _, item := range append(first, second...) {
		if seen[item["ID"]] {
			t.Fatalf("duplicate quote across pages: %v", item["ID"])
		}
		seen[item["ID"]] = true
	}
}

func TestAdminResourcePageKeepsMissingSourcesDistinctFromZero(t *testing.T) {
	ctx := context.Background()
	l, clock := openChangingLab(t)
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	paid := paidBasicSubscription(t, l, "missing-resource-sources")
	subscriptions, _, _, err := l.AdminResourcePage(ctx, "subscriptions", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	foundSubscription := false
	for _, row := range subscriptions {
		if row["ID"] != paid.SubscriptionID {
			continue
		}
		foundSubscription = true
		if row["EntitlementStatus"] != nil || row["EntitlementReason"] != nil {
			t.Fatalf("missing entitlement projection should remain unknown: %+v", row)
		}
	}
	if !foundSubscription {
		t.Fatal("subscription missing from admin page")
	}
	*clock = time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)
	change, err := l.RequestImmediateProUpgrade(ctx, paid.SubscriptionID, 5, 1, "missing-period-source")
	if err != nil {
		t.Fatal(err)
	}
	invoices, _, _, err := l.AdminResourcePage(ctx, "invoices", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	foundOriginal, foundSupplement := false, false
	for _, row := range invoices {
		switch row["ID"] {
		case paid.InvoiceID:
			foundOriginal = true
			if row["PeriodIndex"] != int64(0) {
				t.Fatalf("real first billing period lost its zero index: %+v", row)
			}
		case change.InvoiceID:
			foundSupplement = true
			if row["PeriodIndex"] != nil {
				t.Fatalf("supplement invoice without period should remain unknown: %+v", row)
			}
		}
	}
	if !foundOriginal || !foundSupplement {
		t.Fatalf("missing invoice rows: original=%v supplement=%v", foundOriginal, foundSupplement)
	}
}

func TestAdminSubscriptionInvoiceAndPaymentPagesDoNotRepeatAfterInsert(t *testing.T) {
	ctx := context.Background()
	l, _, _ := openTestLab(t)
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	create := func(customer string) {
		t.Helper()
		quote, err := l.CreateQuote(ctx, customer, "basic")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, customer+":purchase"); err != nil {
			t.Fatal(err)
		}
	}
	create("cursor-source-a")
	create("cursor-source-b")
	type pageStart struct {
		id     any
		cursor int64
	}
	starts := map[string]pageStart{}
	for _, resource := range []string{"subscriptions", "invoices", "payments"} {
		first, total, cursor, err := l.AdminResourcePage(ctx, resource, 0, 1)
		if err != nil || total != 2 || len(first) != 1 || cursor == 0 {
			t.Fatalf("%s first page: len=%d total=%d cursor=%d err=%v", resource, len(first), total, cursor, err)
		}
		starts[resource] = pageStart{id: first[0]["ID"], cursor: cursor}
	}
	create("cursor-source-c")
	for _, resource := range []string{"subscriptions", "invoices", "payments"} {
		start := starts[resource]
		second, total, next, err := l.AdminResourcePage(ctx, resource, start.cursor, 2)
		if err != nil || total != 3 || len(second) != 2 || next != 0 {
			t.Fatalf("%s second page: len=%d total=%d cursor=%d err=%v", resource, len(second), total, next, err)
		}
		if start.id == second[0]["ID"] || start.id == second[1]["ID"] || second[0]["ID"] == second[1]["ID"] {
			t.Fatalf("%s repeated a row across insert: first=%+v second=%+v", resource, start.id, second)
		}
	}
}
