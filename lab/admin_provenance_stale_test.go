package lab

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func provenanceRaceFixture(t *testing.T) (*Lab, LegacyProvenance) {
	t.Helper()
	l, _, _ := openTestLab(t)
	ctx := context.Background()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	quote, err := l.CreateQuote(ctx, "provenance-race-customer", "basic")
	if err != nil {
		t.Fatal(err)
	}
	purchased, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "provenance-race-buy")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.LinkLegacyAccount(ctx, AccountLink{
		LegacyAccountID: "provenance-race-legacy", CustomerID: "provenance-race-customer",
		BeneficiaryID: "provenance-race-beneficiary", Cohort: "default", HasHistory: true,
	}); err != nil {
		t.Fatal(err)
	}
	return l, LegacyProvenance{
		LegacyInvoiceID: "provenance-race-invoice", LegacySubscriptionID: "provenance-race-subscription",
		LegacyAccountID: "provenance-race-legacy", CommerceSubscriptionID: purchased.SubscriptionID,
		CommerceInvoiceID: purchased.InvoiceID, PriceVersionID: "pro-v1",
	}
}

func TestAdminProvenanceBackfillPreviewRejectsDifferentConcurrentMapping(t *testing.T) {
	l, record := provenanceRaceFixture(t)
	ctx := context.Background()
	payload, err := json.Marshal(adminBackfillProvenancePayload{
		LegacyInvoiceID: record.LegacyInvoiceID, LegacySubscriptionID: record.LegacySubscriptionID,
		CommerceSubscriptionID: record.CommerceSubscriptionID, CommerceInvoiceID: record.CommerceInvoiceID,
		PriceVersionID: record.PriceVersionID,
	})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C39", record.LegacyAccountID, payload)
	if err != nil {
		t.Fatal(err)
	}
	correct := record
	correct.PriceVersionID = "basic-v1"
	concurrent, err := l.BackfillLegacyProvenance(ctx, correct)
	if err != nil || concurrent.Status != "complete" {
		t.Fatalf("concurrent mapping=%+v err=%v", concurrent, err)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "stale-provenance-backfill", "C39", record.LegacyAccountID, payload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	command, err = l.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "failed" || command.ErrorCode != "PREVIEW_STALE" {
		t.Fatalf("changed mapping command=%+v err=%v", command, err)
	}
	var savedPrice, status string
	if err := l.db.QueryRowContext(ctx, `SELECT price_version_id,status FROM legacy_provenance WHERE legacy_invoice_id=?`, record.LegacyInvoiceID).Scan(&savedPrice, &status); err != nil {
		t.Fatal(err)
	}
	var receipts int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if savedPrice != "basic-v1" || status != "complete" || receipts != 0 {
		t.Fatalf("saved price=%s status=%s receipts=%d", savedPrice, status, receipts)
	}
	if _, err := l.AdminCreatePreview(ctx, "local-admin", "C39", record.LegacyAccountID, payload); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflicting backfill appeared valid: %v", err)
	}
}

func TestAdminProvenanceResolvePreviewRejectsConcurrentResolution(t *testing.T) {
	l, record := provenanceRaceFixture(t)
	ctx := context.Background()
	backfilled, err := l.BackfillLegacyProvenance(ctx, record)
	if err != nil || backfilled.Status != "manual_review" {
		t.Fatalf("review fixture=%+v err=%v", backfilled, err)
	}
	payload, err := json.Marshal(adminResolveProvenancePayload{
		CommerceSubscriptionID: record.CommerceSubscriptionID, CommerceInvoiceID: record.CommerceInvoiceID,
		PriceVersionID: "basic-v1", Decision: "verified_source",
	})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C40", record.LegacyInvoiceID, payload)
	if err != nil {
		t.Fatal(err)
	}
	correct := record
	correct.PriceVersionID = "basic-v1"
	concurrent, err := l.ResolveLegacyProvenance(ctx, correct, "other-reviewer", "verified_source")
	if err != nil || concurrent.Status != "complete" {
		t.Fatalf("concurrent resolution=%+v err=%v", concurrent, err)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "stale-provenance-resolution", "C40", record.LegacyInvoiceID, payload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	command, err = l.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "failed" || command.ErrorCode != "PREVIEW_STALE" {
		t.Fatalf("resolved source command=%+v err=%v", command, err)
	}
	var events, receipts int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM account_migration_events WHERE legacy_account_id=? AND kind='provenance_resolved'`, record.LegacyAccountID).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?`, command.ID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if events != 1 || receipts != 0 {
		t.Fatalf("resolution events=%d receipts=%d", events, receipts)
	}
	if _, err := l.AdminCreatePreview(ctx, "local-admin", "C40", record.LegacyInvoiceID, payload); !errors.Is(err, ErrConflict) {
		t.Fatalf("resolved provenance appeared reviewable: %v", err)
	}
}
