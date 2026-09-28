package lab

import (
	"context"
	"errors"
	"testing"
)

func proV2Spec() ProPriceSpec {
	return ProPriceSpec{ID: "pro-v2", Version: 2, FixedMinor: 6000, SeatMinor: 1000, IncludedTasks: 20000, UsageRateNum: 1, UsageRateDen: 10, EffectiveFrom: fixedNow}
}

func TestS08PublishedPriceAndCohortSelection(t *testing.T) {
	l, _ := openChangingLab(t)
	ctx := context.Background()
	before, err := l.CreateQuoteWithSeats(ctx, "old", "pro", 5)
	if err != nil || before.PriceVersionID != "pro-v1" || before.AmountMinor != 10000 {
		t.Fatalf("default Pro v1: %+v, %v", before, err)
	}
	spec := proV2Spec()
	if terms, err := l.PublishProPrice(ctx, spec); err != nil || terms.FixedMinor != 6000 || terms.SeatMinor != 1000 {
		t.Fatalf("publish v2: %+v, %v", terms, err)
	}
	if _, err := l.PublishProPrice(ctx, spec); err != nil {
		t.Fatalf("publish replay: %v", err)
	}
	changed := spec
	changed.SeatMinor = 1100
	if _, err := l.PublishProPrice(ctx, changed); !errors.Is(err, ErrConflict) {
		t.Fatalf("published version changed: %v", err)
	}
	if _, err := l.db.ExecContext(ctx, `UPDATE price_components SET amount_minor=1100 WHERE price_version_id='pro-v2' AND component_code='seats'`); err == nil {
		t.Fatal("published component changed")
	}
	if err := l.SelectCatalogPrice(ctx, "pro", "A", fixedNow, spec.ID); err != nil {
		t.Fatal(err)
	}
	if err := l.SelectCatalogPrice(ctx, "pro", "A", fixedNow, spec.ID); err != nil {
		t.Fatalf("selection replay: %v", err)
	}
	if err := l.SelectCatalogPrice(ctx, "pro", "A", fixedNow, "pro-v1"); !errors.Is(err, ErrConflict) {
		t.Fatalf("selection overwritten: %v", err)
	}
	a, err := l.CreateQuoteForCohort(ctx, "new-a", "pro", "A", 5)
	if err != nil || a.PriceVersionID != "pro-v2" || a.AmountMinor != 11000 {
		t.Fatalf("cohort A v2: %+v, %v", a, err)
	}
	defaultQuote, err := l.CreateQuoteForCohort(ctx, "new-default", "pro", "default", 5)
	if err != nil || defaultQuote.PriceVersionID != "pro-v1" || defaultQuote.AmountMinor != 10000 {
		t.Fatalf("default still v1: %+v, %v", defaultQuote, err)
	}
}
