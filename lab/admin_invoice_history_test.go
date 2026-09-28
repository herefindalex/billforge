package lab

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestAdminInvoiceHistoryPagesBeyondDetailLimitWithoutRepeats(t *testing.T) {
	l, clock := openChangingLab(t)
	ctx := context.Background()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	paid := paidBasicSubscription(t, l, "invoice-history-customer")
	for i := 0; i < 101; i++ {
		if _, err := l.PostReduction(ctx, paid.InvoiceID, 1, "history correction", fmt.Sprintf("invoice-history-%03d", i)); err != nil {
			t.Fatalf("correction %d: %v", i, err)
		}
	}
	detail, err := l.AdminInvoiceDetail(ctx, paid.InvoiceID)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Corrections) != 100 || !detail.CorrectionsTruncated || len(detail.CreditGrants) != 100 || !detail.CreditGrantsTruncated {
		t.Fatalf("detail did not expose the history limit: corrections=%d/%v grants=%d/%v", len(detail.Corrections), detail.CorrectionsTruncated, len(detail.CreditGrants), detail.CreditGrantsTruncated)
	}
	first, err := l.AdminInvoiceHistory(ctx, paid.InvoiceID, "corrections", nil, 50)
	if err != nil || first.Next == nil {
		t.Fatalf("first page: %+v %v", first, err)
	}
	*clock = clock.Add(time.Second)
	if _, err := l.PostReduction(ctx, paid.InvoiceID, 1, "newer correction", "invoice-history-newer"); err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool)
	for page, cursor := first, (*AdminInvoiceHistoryCursor)(nil); ; {
		items := page.Items.([]AdminInvoiceCorrection)
		for _, item := range items {
			if seen[item.ID] {
				t.Fatalf("repeated correction %s", item.ID)
			}
			seen[item.ID] = true
			if item.Reason == "newer correction" {
				t.Fatal("new correction entered an older cursor page")
			}
		}
		cursor = page.Next
		if cursor == nil {
			break
		}
		page, err = l.AdminInvoiceHistory(ctx, paid.InvoiceID, "corrections", cursor, 50)
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(seen) != 101 {
		t.Fatalf("history rows=%d, want 101", len(seen))
	}
	grants, err := l.AdminInvoiceHistory(ctx, paid.InvoiceID, "grants", nil, 100)
	if err != nil || grants.Next == nil || len(grants.Items.([]AdminInvoiceCreditGrant)) != 100 {
		t.Fatalf("grant first page: %+v %v", grants, err)
	}
	if _, err := l.AdminInvoiceHistory(ctx, paid.InvoiceID, "grants", first.Next, 20); !errors.Is(err, ErrConflict) {
		t.Fatalf("cross-kind cursor accepted: %v", err)
	}
	if _, err := l.AdminInvoiceHistory(ctx, "missing-invoice", "corrections", nil, 20); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing invoice: %v", err)
	}
	if _, err := l.AdminInvoiceHistory(ctx, paid.InvoiceID, "unknown", nil, 20); !errors.Is(err, ErrAdminUnsupportedAction) {
		t.Fatalf("unknown kind: %v", err)
	}
}
