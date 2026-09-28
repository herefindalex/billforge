package lab

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestProFiveSeatsKeepsVersionAndLinesAcrossRenewal(t *testing.T) {
	dir := t.TempDir()
	now := fixedNow
	l, err := Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	ctx := context.Background()
	q, err := l.CreateQuoteWithSeats(ctx, "pro-customer", "pro", 5)
	if err != nil || q.AmountMinor != 10000 || q.SeatQuantity != 5 || q.PriceVersionID != "pro-v1" {
		t.Fatalf("Pro five-seat quote: %+v, %v", q, err)
	}
	if _, err := l.CreateQuoteWithSeats(ctx, "pro-customer", "pro", 0); err == nil {
		t.Fatal("Pro accepted zero paid seats")
	}
	r, err := l.AcceptQuote(ctx, q.ID, q.Fingerprint, "pro-purchase")
	if err != nil || r.AmountMinor != 10000 {
		t.Fatalf("Pro purchase: %+v, %v", r, err)
	}
	assertProLines := func(invoiceID string) {
		t.Helper()
		rows, err := l.db.Query(`SELECT component_code,amount_minor FROM invoice_lines WHERE invoice_id=? ORDER BY component_code`, invoiceID)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		got := map[string]int64{}
		for rows.Next() {
			var code string
			var amount int64
			if err := rows.Scan(&code, &amount); err != nil {
				t.Fatal(err)
			}
			got[code] = amount
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 || got["fixed"] != 5000 || got["seats"] != 5000 {
			t.Fatalf("invoice %s lost price components: %+v", invoiceID, got)
		}
	}
	assertProLines(r.InvoiceID)
	if _, err := l.DispatchNext(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := l.db.Exec(`UPDATE price_versions SET fixed_amount_minor=1 WHERE id='pro-v1'`); err == nil {
		t.Fatal("published Pro price changed in place")
	}
	if _, err := l.db.Exec(`INSERT INTO price_components(price_version_id,component_code,kind,amount_minor) VALUES('pro-v1','extra','per_seat',1)`); err == nil {
		t.Fatal("published Pro component changed in place")
	}
	now = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	renewals, err := l.RunRenewals(ctx)
	if err != nil || len(renewals) != 1 || renewals[0].AmountMinor != 10000 {
		t.Fatalf("Pro renewal: %+v, %v", renewals, err)
	}
	assertProLines(renewals[0].InvoiceID)
}
