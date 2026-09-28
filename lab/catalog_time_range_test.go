package lab

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCatalogWritesRejectTimesOutsideUnixNanosecondRange(t *testing.T) {
	farFuture := time.Date(2700, time.January, 1, 0, 0, 0, 0, time.UTC)

	t.Run("pro price", func(t *testing.T) {
		l, _ := openChangingLab(t)
		spec := proV2Spec()
		spec.EffectiveFrom = farFuture
		if _, err := l.PublishProPrice(context.Background(), spec); !errors.Is(err, ErrConflict) {
			t.Fatalf("publish out-of-range pro price: %v", err)
		}
		var count int
		if err := l.db.QueryRow(`SELECT COUNT(*) FROM price_versions WHERE id=?`, spec.ID).Scan(&count); err != nil || count != 0 {
			t.Fatalf("out-of-range pro price persisted: count=%d err=%v", count, err)
		}
	})

	t.Run("metered price", func(t *testing.T) {
		l, _ := openChangingLab(t)
		ctx := context.Background()
		if err := l.RegisterMeter(ctx, MeterSpec{ID: "ai_tokens", Source: "ai_gateway", Unit: "token", SchemaVersion: 1}); err != nil {
			t.Fatal(err)
		}
		spec := MeteredPriceSpec{ID: "ai_v1", PlanID: "ai", Version: 1, FixedMinor: 3000, MeterID: "ai_tokens", IncludedQuantity: 100, UsageRateNum: 1, UsageRateDen: 5, EffectiveFrom: farFuture}
		if _, err := l.PublishMeteredPrice(ctx, spec); !errors.Is(err, ErrConflict) {
			t.Fatalf("publish out-of-range metered price: %v", err)
		}
		var count int
		if err := l.db.QueryRow(`SELECT COUNT(*) FROM price_versions WHERE id=?`, spec.ID).Scan(&count); err != nil || count != 0 {
			t.Fatalf("out-of-range metered price persisted: count=%d err=%v", count, err)
		}
	})

	t.Run("catalog selection", func(t *testing.T) {
		l, _ := openChangingLab(t)
		if err := l.SelectCatalogPrice(context.Background(), "pro", "future", farFuture, "pro-v1"); !errors.Is(err, ErrConflict) {
			t.Fatalf("select out-of-range effective time: %v", err)
		}
		var count int
		if err := l.db.QueryRow(`SELECT COUNT(*) FROM catalog_selection WHERE plan_id='pro' AND cohort='future'`).Scan(&count); err != nil || count != 0 {
			t.Fatalf("out-of-range selection persisted: count=%d err=%v", count, err)
		}
	})

	t.Run("publication clock", func(t *testing.T) {
		l, clock := openChangingLab(t)
		*clock = farFuture
		if _, err := l.PublishProPrice(context.Background(), proV2Spec()); !errors.Is(err, ErrConflict) {
			t.Fatalf("publish with out-of-range business clock: %v", err)
		}
		var count int
		if err := l.db.QueryRow(`SELECT COUNT(*) FROM price_versions WHERE id='pro-v2'`).Scan(&count); err != nil || count != 0 {
			t.Fatalf("price persisted with out-of-range publication clock: count=%d err=%v", count, err)
		}
	})
}
