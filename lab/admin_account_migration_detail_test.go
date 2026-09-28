package lab

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

func TestAdminAccountMigrationDetailShowsOwnersShadowsAndStopWithoutRollback(t *testing.T) {
	l, _, _ := openTestLab(t)
	ctx := context.Background()
	link, err := l.LinkLegacyAccount(ctx, AccountLink{LegacyAccountID: "legacy:detail", CustomerID: "detail-customer", BeneficiaryID: "detail-customer", Cohort: "default"})
	if err != nil {
		t.Fatal(err)
	}
	shadow, err := l.ShadowQuote(ctx, link.LegacyAccountID, "basic", 0, 2100, "USD")
	if err != nil {
		t.Fatal(err)
	}
	detail, err := l.AdminAccountMigrationDetail(ctx, link.LegacyAccountID)
	if err != nil || detail.Link.CustomerID != link.CustomerID || detail.Link.ReadOwner != "legacy" || detail.Link.WriterOwner != "legacy" || len(detail.Shadows) != 1 || detail.Shadows[0].ID != shadow.ID || detail.Shadows[0].Matched || len(detail.Provenance) != 0 {
		t.Fatalf("account detail=%+v err=%v", detail, err)
	}
	readiness, err := l.AdminMigrationReadiness(ctx, link.LegacyAccountID, cutoverLimits)
	if err != nil || readiness.Ready || readiness.QuoteMatches {
		t.Fatalf("readiness=%+v err=%v", readiness, err)
	}
	stopped, err := l.StopAccountMigration(ctx, link.LegacyAccountID, "manual risk review")
	if err != nil || !stopped.Stopped {
		t.Fatalf("stop=%+v err=%v", stopped, err)
	}
	detail, err = l.AdminAccountMigrationDetail(ctx, link.LegacyAccountID)
	if err != nil || !detail.Link.Stopped || detail.Link.StopReason != "manual risk review" || detail.Link.ReadOwner != "legacy" || detail.Link.WriterOwner != "legacy" || len(detail.Events) != 1 || detail.Events[0].Kind != "stopped" {
		t.Fatalf("stopped detail=%+v err=%v", detail, err)
	}
	if _, err := l.AdminAccountMigrationDetail(ctx, "missing-account"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing migration error=%v", err)
	}
}
