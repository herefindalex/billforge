package lab

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
)

func TestAdminAccountMigrationHistoryPagesDoNotRepeatRows(t *testing.T) {
	l, _, _ := openTestLab(t)
	ctx := context.Background()
	accountID := "legacy:history-page"
	if _, err := l.LinkLegacyAccount(ctx, AccountLink{LegacyAccountID: accountID, CustomerID: "history-page-customer", BeneficiaryID: "history-page-customer", Cohort: "default", HasHistory: true}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := l.ShadowQuote(ctx, accountID, "basic", 0, int64(2000+i), "USD"); err != nil {
			t.Fatal(err)
		}
		_, err := l.BackfillLegacyProvenance(ctx, LegacyProvenance{
			LegacyAccountID: accountID, LegacyInvoiceID: fmt.Sprintf("old-invoice-%d", i), LegacySubscriptionID: fmt.Sprintf("old-sub-%d", i),
			CommerceSubscriptionID: fmt.Sprintf("missing-sub-%d", i), CommerceInvoiceID: fmt.Sprintf("missing-invoice-%d", i), PriceVersionID: "basic-v1",
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	seenShadows := map[string]bool{}
	var cursor *AdminShadowCursor
	for i := 0; i < 3; i++ {
		page, err := l.AdminAccountMigrationShadows(ctx, accountID, cursor, 1)
		if err != nil || len(page.Items) != 1 || seenShadows[page.Items[0].ID] {
			t.Fatalf("shadow page %d = %+v err=%v", i, page, err)
		}
		seenShadows[page.Items[0].ID] = true
		cursor = page.Next
	}
	if cursor != nil || len(seenShadows) != 3 {
		t.Fatalf("shadow pagination ended incorrectly: cursor=%+v seen=%d", cursor, len(seenShadows))
	}
	seenProvenance := map[string]bool{}
	var after string
	for i := 0; i < 3; i++ {
		page, err := l.AdminAccountMigrationProvenance(ctx, accountID, after, 1)
		if err != nil || len(page.Items) != 1 || seenProvenance[page.Items[0].LegacyInvoiceID] {
			t.Fatalf("provenance page %d = %+v err=%v", i, page, err)
		}
		seenProvenance[page.Items[0].LegacyInvoiceID] = true
		after = page.NextID
	}
	if after != "" || len(seenProvenance) != 3 {
		t.Fatalf("provenance pagination ended incorrectly: after=%q seen=%d", after, len(seenProvenance))
	}
	if _, err := l.AdminAccountMigrationShadows(ctx, "missing", nil, 1); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing shadows error=%v", err)
	}
	if _, err := l.AdminAccountMigrationProvenance(ctx, "missing", "", 1); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing provenance error=%v", err)
	}
}
