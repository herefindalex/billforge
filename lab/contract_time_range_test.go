package lab

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestContractPublicationRejectsOutOfRangeTermsAndClock(t *testing.T) {
	farFuture := time.Date(2700, time.January, 1, 0, 0, 0, 0, time.UTC)

	t.Run("contract term", func(t *testing.T) {
		l, _ := openChangingLab(t)
		spec := acmeContract("")
		spec.EffectiveFrom = farFuture
		spec.EffectiveTo = farFuture.Add(30 * 24 * time.Hour)
		if _, err := l.PublishContract(context.Background(), spec); !errors.Is(err, ErrConflict) {
			t.Fatalf("publish out-of-range contract term: %v", err)
		}
		var count int
		if err := l.db.QueryRow(`SELECT COUNT(*) FROM contract_versions WHERE id=?`, spec.ID).Scan(&count); err != nil || count != 0 {
			t.Fatalf("out-of-range contract persisted: count=%d err=%v", count, err)
		}
	})

	t.Run("publication clock", func(t *testing.T) {
		l, clock := openChangingLab(t)
		*clock = farFuture
		spec := acmeContract("")
		if _, err := l.PublishContract(context.Background(), spec); !errors.Is(err, ErrConflict) {
			t.Fatalf("publish with out-of-range business clock: %v", err)
		}
		var count int
		if err := l.db.QueryRow(`SELECT COUNT(*) FROM contract_versions WHERE id=?`, spec.ID).Scan(&count); err != nil || count != 0 {
			t.Fatalf("contract persisted with out-of-range clock: count=%d err=%v", count, err)
		}
	})

	t.Run("existing publication replay", func(t *testing.T) {
		l, clock := openChangingLab(t)
		ctx := context.Background()
		spec := acmeContract("")
		if _, err := l.PublishContract(ctx, spec); err != nil {
			t.Fatal(err)
		}
		*clock = farFuture
		if _, err := l.PublishContract(ctx, spec); err != nil {
			t.Fatalf("existing contract replay should not depend on the current clock: %v", err)
		}
		var count int
		if err := l.db.QueryRow(`SELECT COUNT(*) FROM contract_versions WHERE id=?`, spec.ID).Scan(&count); err != nil || count != 1 {
			t.Fatalf("contract replay count=%d err=%v", count, err)
		}
	})
}
