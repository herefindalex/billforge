package lab

import (
	"context"
	"fmt"
	"testing"
)

func TestAdminContractVersionDetailBoundsQuoteHistory(t *testing.T) {
	ctx := context.Background()
	l, _, _ := openTestLab(t)
	contract, err := l.PublishContract(ctx, acmeContract(""))
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 21; index++ {
		if _, err := l.CreateContractQuote(ctx, contract.CustomerID, contract.ID, 5); err != nil {
			t.Fatal(err)
		}
	}
	detail, err := l.AdminContractVersionDetail(ctx, contract.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.QuoteCount != 21 || len(detail.Quotes) != 20 || !detail.QuotesTruncated || detail.SubscriptionCount != 0 {
		t.Fatalf("unbounded contract quote history: count=%d page=%d truncated=%t subscriptions=%d", detail.QuoteCount, len(detail.Quotes), detail.QuotesTruncated, detail.SubscriptionCount)
	}
}

func TestAdminContractResourceFiltersKeepMembershipAcrossPages(t *testing.T) {
	ctx := context.Background()
	l, _, _ := openTestLab(t)
	first, err := l.PublishContract(ctx, acmeContract(""))
	if err != nil {
		t.Fatal(err)
	}
	otherSpec := acmeContract("")
	otherSpec.ID = "other-contract-v1"
	otherSpec.CustomerID = "other-customer"
	other, err := l.PublishContract(ctx, otherSpec)
	if err != nil {
		t.Fatal(err)
	}

	var firstQuoteIDs, firstSubscriptionIDs []string
	for i, contract := range []ContractState{first, other, first, first} {
		quote, err := l.CreateContractQuote(ctx, contract.CustomerID, contract.ID, 5)
		if err != nil {
			t.Fatal(err)
		}
		if contract.ID == first.ID {
			firstQuoteIDs = append(firstQuoteIDs, quote.ID)
		}
		if i == 3 {
			continue
		}
		receipt, err := l.AcceptContractQuote(ctx, quote.ID, quote.Fingerprint, fmt.Sprintf("filter-contract-accept-%d", i))
		if err != nil {
			t.Fatal(err)
		}
		if contract.ID == first.ID {
			firstSubscriptionIDs = append(firstSubscriptionIDs, receipt.SubscriptionID)
		}
	}

	for _, tc := range []struct {
		resource string
		wantIDs  []string
	}{
		{"quotes", firstQuoteIDs},
		{"subscriptions", firstSubscriptionIDs},
	} {
		t.Run(tc.resource, func(t *testing.T) {
			filters := map[string]string{"contract_version_id": first.ID, "customer_id": first.CustomerID}
			seen := make(map[string]bool)
			var cursor int64
			for {
				items, total, next, err := l.AdminFilteredResourcePage(ctx, tc.resource, cursor, 1, filters)
				if err != nil || total != len(tc.wantIDs) || len(items) != 1 {
					t.Fatalf("page=%v total=%d next=%d err=%v", items, total, next, err)
				}
				id, ok := items[0]["ID"].(string)
				if !ok || seen[id] {
					t.Fatalf("duplicate or missing ID: %v", items[0])
				}
				seen[id] = true
				if next == 0 {
					break
				}
				cursor = next
			}
			if len(seen) != len(tc.wantIDs) {
				t.Fatalf("seen=%v want=%v", seen, tc.wantIDs)
			}
			for _, id := range tc.wantIDs {
				if !seen[id] {
					t.Fatalf("missing %s in %v", id, seen)
				}
			}
			otherItems, total, _, err := l.AdminFilteredResourcePage(ctx, tc.resource, 0, 10, map[string]string{"contract_version_id": other.ID})
			if err != nil || total != 1 || len(otherItems) != 1 {
				t.Fatalf("other contract page=%v total=%d err=%v", otherItems, total, err)
			}
		})
	}
}

func TestMigrateContractsAddsResourceFilterIndexesToExistingTables(t *testing.T) {
	l, _, _ := openTestLab(t)
	for _, name := range []string{"contract_quotes_version_quote_idx", "contract_subscriptions_version_subscription_idx"} {
		if _, err := l.db.Exec("DROP INDEX " + name); err != nil {
			t.Fatal(err)
		}
	}
	if err := migrateContracts(l.db); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"contract_quotes_version_quote_idx", "contract_subscriptions_version_subscription_idx"} {
		var found int
		if err := l.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?`, name).Scan(&found); err != nil || found != 1 {
			t.Fatalf("index %s found=%d err=%v", name, found, err)
		}
	}
}
