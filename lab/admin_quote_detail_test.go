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

func TestAdminQuoteDetailSeparatesDueNowFullTermAndUsageRate(t *testing.T) {
	l, _ := openChangingLab(t)
	ctx := context.Background()
	pro, err := l.CreateQuoteForCohort(ctx, "quote-detail-pro", "pro", "default", 5)
	if err != nil {
		t.Fatal(err)
	}
	detail, err := l.AdminQuoteDetail(ctx, pro.ID)
	if err != nil || detail.DueNowMinor == nil || *detail.DueNowMinor != pro.AmountMinor || detail.NextFullTermFixedMinor != pro.AmountMinor || detail.UsageMeterID != "tasks" || detail.IncludedQuantity != 20000 || detail.UsageRateNum != 1 || detail.UsageRateDen != 10 {
		t.Fatalf("new pro quote terms: %+v err=%v", detail, err)
	}

	paid := paidBasicSubscription(t, l, "quote-detail-change")
	for _, mode := range []string{"next_period", "immediate"} {
		quote, err := l.CreateQuoteForCohort(ctx, "quote-detail-change", "pro", "default", 5)
		if err != nil {
			t.Fatal(err)
		}
		binding, err := l.BindChangeQuote(ctx, quote.ID, paid.SubscriptionID, mode, 1)
		if err != nil {
			t.Fatal(err)
		}
		detail, err := l.AdminQuoteDetail(ctx, quote.ID)
		if err != nil || detail.ChangeMode != mode || detail.ChangeSubscriptionID != paid.SubscriptionID || detail.BindingFingerprint != binding.Fingerprint || detail.NextFullTermFixedMinor != quote.AmountMinor {
			t.Fatalf("%s quote terms: %+v err=%v", mode, detail, err)
		}
		if mode == "next_period" && (detail.DueNowMinor == nil || *detail.DueNowMinor != 0) {
			t.Fatalf("scheduled change has immediate payment: %+v", detail)
		}
		if mode == "immediate" && detail.DueNowMinor != nil {
			t.Fatalf("immediate change quoted a proration before preview: %+v", detail)
		}
	}

	contractSpec := acmeContract("")
	if _, err := l.PublishContract(ctx, contractSpec); err != nil {
		t.Fatal(err)
	}
	contractQuote, err := l.CreateContractQuote(ctx, contractSpec.CustomerID, contractSpec.ID, 5)
	if err != nil {
		t.Fatal(err)
	}
	detail, err = l.AdminQuoteDetail(ctx, contractQuote.ID)
	if err != nil || detail.ContractVersionID != contractSpec.ID || detail.DueNowMinor == nil || *detail.DueNowMinor != 0 || detail.NextFullTermFixedMinor != 7500 {
		t.Fatalf("net30 contract quote terms: %+v err=%v", detail, err)
	}
}
