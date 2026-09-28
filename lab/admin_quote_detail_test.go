package lab

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

func TestAdminQuoteDetailReadsOneQuoteAndAcceptance(t *testing.T) {
	l, _ := openChangingLab(t)
	ctx := context.Background()
	quote, err := l.CreateQuote(ctx, "quote-detail-customer", "basic")
	if err != nil {
		t.Fatal(err)
	}
	detail, err := l.AdminQuoteDetail(ctx, quote.ID)
	if err != nil || detail.Accepted || detail.AmountMinor != quote.AmountMinor || detail.Fingerprint != quote.Fingerprint {
		t.Fatalf("unaccepted detail=%+v err=%v", detail, err)
	}
	if _, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "quote-detail-accept"); err != nil {
		t.Fatal(err)
	}
	detail, err = l.AdminQuoteDetail(ctx, quote.ID)
	if err != nil || !detail.Accepted {
		t.Fatalf("accepted detail=%+v err=%v", detail, err)
	}
	if _, err := l.AdminQuoteDetail(ctx, "missing-quote"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing quote error = %v", err)
	}
}
