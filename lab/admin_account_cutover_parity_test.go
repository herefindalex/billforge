package lab

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestAdminShadowReadSingleWriterCutoverMatchesDomain(t *testing.T) {
	ctx := context.Background()
	open := func() *Lab {
		t.Helper()
		dir := t.TempDir()
		l, err := Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), func() time.Time { return fixedNow })
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = l.Close() })
		return l
	}
	domain, admin := open(), open()
	if err := admin.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	runAdmin := func(action, target, key string, payload json.RawMessage, previewRequired bool) AdminCommand {
		t.Helper()
		previewID := ""
		if previewRequired {
			preview, err := admin.AdminCreatePreview(ctx, "local-admin", action, target, payload)
			if err != nil {
				t.Fatalf("%s preview: %v", action, err)
			}
			previewID = preview.ID
		}
		command, _, err := admin.AdminSubmitCommand(ctx, "local-admin", key, action, target, payload, previewID)
		if err != nil {
			t.Fatalf("%s submit: %v", action, err)
		}
		command, err = admin.AdminExecuteCommand(ctx, command.ID)
		if err != nil || command.Status != "succeeded" {
			t.Fatalf("%s execute: %+v err=%v", action, command, err)
		}
		return command
	}
	marshal := func(value any) json.RawMessage {
		t.Helper()
		payload, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return payload
	}
	refs := func(command AdminCommand) map[string]string {
		t.Helper()
		var result map[string]string
		if err := json.Unmarshal(command.ResultRefs, &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	compareLink := func(readOwner, writerOwner string, stopped bool) {
		t.Helper()
		d, err := domain.AccountLink(ctx, "legacy:cutover-parity")
		if err != nil {
			t.Fatal(err)
		}
		a, err := admin.AccountLink(ctx, "legacy:cutover-parity")
		if err != nil || !reflect.DeepEqual(d, a) || a.ReadOwner != readOwner || a.WriterOwner != writerOwner || a.Stopped != stopped {
			t.Fatalf("account link differs: domain=%+v admin=%+v err=%v", d, a, err)
		}
	}
	adminQuote := func(key string) (string, string) {
		t.Helper()
		quoteRefs := refs(runAdmin("C01", "", key, marshal(AdminCreateQuotePayload{
			CustomerID: "cutover-parity-customer", PlanID: "basic", Cohort: "default", Seats: "0",
		}), false))
		var fingerprint string
		if err := admin.db.QueryRowContext(ctx, `SELECT fingerprint FROM quotes WHERE id=?`, quoteRefs["quote_id"]).Scan(&fingerprint); err != nil {
			t.Fatal(err)
		}
		return quoteRefs["quote_id"], fingerprint
	}
	rejectAdminAccept := func(quoteID, fingerprint, key string, stopped bool) {
		t.Helper()
		payload := marshal(AdminAcceptQuotePayload{Fingerprint: fingerprint})
		preview, err := admin.AdminCreatePreview(ctx, "local-admin", "C02", quoteID, payload)
		if err != nil {
			if !errors.Is(err, ErrConflict) && !errors.Is(err, ErrAdminPreviewStale) {
				t.Fatalf("unexpected C02 preview failure: %v", err)
			}
			return
		}
		command, _, err := admin.AdminSubmitCommand(ctx, "local-admin", key, "C02", quoteID, payload, preview.ID)
		if err != nil {
			if !errors.Is(err, ErrConflict) && !errors.Is(err, ErrAdminPreviewStale) {
				t.Fatalf("unexpected C02 admission failure: %v", err)
			}
			return
		}
		command, err = admin.AdminExecuteCommand(ctx, command.ID)
		allowedCode := command.ErrorCode == "DOMAIN_REJECTED" || command.ErrorCode == "PREVIEW_STALE"
		if stopped {
			allowedCode = command.ErrorCode == "ACCOUNT_MIGRATION_STOPPED"
		}
		if err != nil || command.Status != "failed" || !allowedCode {
			t.Fatalf("legacy or stopped writer rejection: %+v err=%v", command, err)
		}
	}
	link := AccountLink{
		LegacyAccountID: "legacy:cutover-parity", CustomerID: "cutover-parity-customer",
		BeneficiaryID: "cutover-parity-customer", Cohort: "default",
	}
	if _, err := domain.LinkLegacyAccount(ctx, link); err != nil {
		t.Fatal(err)
	}
	runAdmin("C36", "", "cutover-parity-link", marshal(adminLinkAccountPayload{
		LegacyAccountID: link.LegacyAccountID, CustomerID: link.CustomerID,
		BeneficiaryID: link.BeneficiaryID, Cohort: link.Cohort, HasHistory: "false",
	}), true)
	compareLink("legacy", "legacy", false)
	domainQuote, err := domain.CreateQuote(ctx, link.CustomerID, "basic")
	if err != nil {
		t.Fatal(err)
	}
	adminQuoteID, adminFingerprint := adminQuote("cutover-parity-first-quote")
	if _, err := domain.AcceptQuote(ctx, domainQuote.ID, domainQuote.Fingerprint, "cutover-parity-first-accept"); !errors.Is(err, ErrConflict) {
		t.Fatalf("domain legacy writer accepted commerce checkout: %v", err)
	}
	rejectAdminAccept(adminQuoteID, adminFingerprint, "cutover-parity-premature-accept", false)
	for _, l := range []*Lab{domain, admin} {
		var subscriptions int
		if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM subscriptions WHERE customer_id=?`, link.CustomerID).Scan(&subscriptions); err != nil || subscriptions != 0 {
			t.Fatalf("legacy writer created subscription: count=%d err=%v", subscriptions, err)
		}
	}
	if shadow, err := domain.ShadowQuote(ctx, link.LegacyAccountID, "basic", 0, 2100, "USD"); err != nil || shadow.Matched {
		t.Fatalf("domain mismatched shadow: %+v err=%v", shadow, err)
	}
	runAdmin("C37", link.LegacyAccountID, "cutover-parity-bad-shadow", marshal(adminShadowQuotePayload{
		PlanID: "basic", Seats: "0", LegacyAmountMinor: "2100", LegacyCurrency: "USD",
	}), false)
	if shadow, err := domain.ShadowQuote(ctx, link.LegacyAccountID, "basic", 0, 2000, "USD"); err != nil || !shadow.Matched {
		t.Fatalf("domain matched shadow: %+v err=%v", shadow, err)
	}
	runAdmin("C37", link.LegacyAccountID, "cutover-parity-good-shadow", marshal(adminShadowQuotePayload{
		PlanID: "basic", Seats: "0", LegacyAmountMinor: "2000", LegacyCurrency: "USD",
	}), false)
	if shadow, err := domain.ShadowEntitlement(ctx, link.LegacyAccountID, "future-sub", "missing"); err != nil || !shadow.Matched {
		t.Fatalf("domain entitlement shadow: %+v err=%v", shadow, err)
	}
	runAdmin("C38", link.LegacyAccountID, "cutover-parity-entitlement-shadow", marshal(adminShadowEntitlementPayload{
		SubscriptionID: "future-sub", LegacyStatus: "missing",
	}), false)
	if _, err := domain.RunReconciliation(ctx, fixedNow); err != nil {
		t.Fatal(err)
	}
	runAdmin("C33", "", "cutover-parity-reconciliation", marshal(AdminReconciliationPayload{AsOf: fixedNow.Format(time.RFC3339Nano)}), false)
	cutoverPayload := marshal(adminCutoverPayload{
		MaxQuoteP95Millis: "5000", MaxUnknownPayments: "0", MaxOpenDiscrepancies: "0",
	})
	if _, err := domain.SwitchAccountWriter(ctx, link.LegacyAccountID, cutoverLimits); !errors.Is(err, ErrConflict) {
		t.Fatalf("domain writer switched before read: %v", err)
	}
	if _, err := domain.SwitchAccountRead(ctx, link.LegacyAccountID, cutoverLimits); err != nil {
		t.Fatal(err)
	}
	runAdmin("C41", link.LegacyAccountID, "cutover-parity-read", cutoverPayload, true)
	compareLink("commerce", "legacy", false)
	if _, err := domain.SwitchAccountWriter(ctx, link.LegacyAccountID, cutoverLimits); err != nil {
		t.Fatal(err)
	}
	runAdmin("C42", link.LegacyAccountID, "cutover-parity-writer", cutoverPayload, true)
	compareLink("commerce", "commerce", false)
	domainReceipt, err := domain.AcceptQuote(ctx, domainQuote.ID, domainQuote.Fingerprint, "cutover-parity-first-accept")
	if err != nil {
		t.Fatal(err)
	}
	adminAccepted := refs(runAdmin("C02", adminQuoteID, "cutover-parity-accepted", marshal(AdminAcceptQuotePayload{Fingerprint: adminFingerprint}), true))
	for _, l := range []*Lab{domain, admin} {
		if _, err := l.DispatchNext(ctx, ""); err != nil {
			t.Fatal(err)
		}
		if err := l.RebuildEntitlements(ctx); err != nil {
			t.Fatal(err)
		}
	}
	domainRead, err := domain.ReadAccountEntitlement(ctx, link.LegacyAccountID, domainReceipt.SubscriptionID)
	if err != nil {
		t.Fatal(err)
	}
	adminRead, err := admin.ReadAccountEntitlement(ctx, link.LegacyAccountID, adminAccepted["subscription_id"])
	if err != nil || domainRead.Owner != "commerce" || adminRead.Owner != "commerce" || domainRead.Status != "active" || adminRead.Status != "active" {
		t.Fatalf("commerce read differs: domain=%+v admin=%+v err=%v", domainRead, adminRead, err)
	}
	d, a := loadPurchaseFacts(t, domain, domainReceipt.SubscriptionID), loadPurchaseFacts(t, admin, adminAccepted["subscription_id"])
	if !reflect.DeepEqual(d, a) || a.InvoiceMinor != 2000 || a.PaymentStatus != "succeeded" {
		t.Fatalf("post-cutover purchase differs: domain=%+v admin=%+v", d, a)
	}
	if _, err := domain.StopAccountMigration(ctx, link.LegacyAccountID, "shadow alert"); err != nil {
		t.Fatal(err)
	}
	runAdmin("C43", link.LegacyAccountID, "cutover-parity-stop", marshal(adminStopMigrationPayload{Reason: "shadow alert"}), true)
	compareLink("commerce", "commerce", true)
	secondDomainQuote, err := domain.CreateQuote(ctx, link.CustomerID, "basic")
	if err != nil {
		t.Fatal(err)
	}
	secondAdminQuote, secondFingerprint := adminQuote("cutover-parity-second-quote")
	if _, err := domain.AcceptQuote(ctx, secondDomainQuote.ID, secondDomainQuote.Fingerprint, "cutover-parity-second-accept"); !errors.Is(err, ErrConflict) {
		t.Fatalf("stopped domain account accepted new charge: %v", err)
	}
	rejectAdminAccept(secondAdminQuote, secondFingerprint, "cutover-parity-stopped-accept", true)
	if replay, err := domain.AcceptQuote(ctx, domainQuote.ID, domainQuote.Fingerprint, "cutover-parity-first-accept"); err != nil || replay.SubscriptionID != domainReceipt.SubscriptionID {
		t.Fatalf("domain original acceptance replay: %+v err=%v", replay, err)
	}
	for _, item := range []struct {
		l      *Lab
		quoted string
	}{{domain, domainReceipt.SubscriptionID}, {admin, adminAccepted["subscription_id"]}} {
		var subscriptions int
		if err := item.l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM subscriptions WHERE customer_id=?`, link.CustomerID).Scan(&subscriptions); err != nil || subscriptions != 1 {
			t.Fatalf("stopped account subscription count=%d err=%v", subscriptions, err)
		}
		if count := captureCount(t, item.l); count != 1 {
			t.Fatalf("stopped account capture count=%d", count)
		}
	}
}

func TestAdminHistoricalProvenanceAndUnknownPaymentGateMatchesDomain(t *testing.T) {
	ctx := context.Background()
	domain, _, _ := openTestLab(t)
	admin, _, _ := openTestLab(t)
	if err := admin.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	runAdmin := func(action, target, key string, payload json.RawMessage, previewRequired bool) AdminCommand {
		t.Helper()
		previewID := ""
		if previewRequired {
			preview, err := admin.AdminCreatePreview(ctx, "local-admin", action, target, payload)
			if err != nil {
				t.Fatalf("%s preview: %v", action, err)
			}
			previewID = preview.ID
		}
		command, _, err := admin.AdminSubmitCommand(ctx, "local-admin", key, action, target, payload, previewID)
		if err != nil {
			t.Fatalf("%s submit: %v", action, err)
		}
		command, err = admin.AdminExecuteCommand(ctx, command.ID)
		if err != nil || command.Status != "succeeded" {
			t.Fatalf("%s execute: %+v err=%v", action, command, err)
		}
		return command
	}
	marshal := func(value any) json.RawMessage {
		t.Helper()
		payload, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return payload
	}
	legacyID := "legacy:history-parity"
	domainReceipt, adminReceipt := purchase(t, domain), purchase(t, admin)
	for _, l := range []*Lab{domain, admin} {
		if _, err := l.DispatchNext(ctx, "lost_response"); err != ErrPaymentUnknown {
			t.Fatalf("lost payment response: %v", err)
		}
	}
	if _, err := domain.LinkLegacyAccount(ctx, AccountLink{
		LegacyAccountID: legacyID, CustomerID: "customer-1", BeneficiaryID: "customer-1", Cohort: "default", HasHistory: true,
	}); err != nil {
		t.Fatal(err)
	}
	runAdmin("C36", "", "history-parity-link", marshal(adminLinkAccountPayload{
		LegacyAccountID: legacyID, CustomerID: "customer-1", BeneficiaryID: "customer-1", Cohort: "default", HasHistory: "true",
	}), true)
	if _, err := domain.ShadowQuote(ctx, legacyID, "basic", 0, 2000, "USD"); err != nil {
		t.Fatal(err)
	}
	runAdmin("C37", legacyID, "history-parity-quote-shadow", marshal(adminShadowQuotePayload{
		PlanID: "basic", Seats: "0", LegacyAmountMinor: "2000", LegacyCurrency: "USD",
	}), false)
	if _, err := domain.ShadowEntitlement(ctx, legacyID, domainReceipt.SubscriptionID, "pending"); err != nil {
		t.Fatal(err)
	}
	runAdmin("C38", legacyID, "history-parity-pending-shadow", marshal(adminShadowEntitlementPayload{
		SubscriptionID: adminReceipt.SubscriptionID, LegacyStatus: "pending",
	}), false)
	if _, err := domain.RunReconciliation(ctx, fixedNow); err != nil {
		t.Fatal(err)
	}
	runAdmin("C33", "", "history-parity-first-reconciliation", marshal(AdminReconciliationPayload{AsOf: fixedNow.Format(time.RFC3339Nano)}), false)
	compareReadiness := func(label string, limits MigrationThresholds, ready, provenance bool, unknown int64) {
		t.Helper()
		for _, l := range []*Lab{domain, admin} {
			state, err := l.MigrationReadiness(ctx, legacyID, limits)
			if err != nil || state.Ready != ready || state.ProvenanceComplete != provenance || state.UnknownPayments != unknown {
				t.Fatalf("%s readiness: %+v err=%v", label, state, err)
			}
		}
	}
	lenient := MigrationThresholds{MaxQuoteP95Millis: 5000, MaxUnknownPayments: 10, MaxOpenDiscrepancies: 10}
	compareReadiness("unknown payment", lenient, false, false, 1)
	wrong := func(sub, invoice string) LegacyProvenance {
		return LegacyProvenance{
			LegacyInvoiceID: "legacy:wrong-parity", LegacySubscriptionID: "legacy:sub-parity", LegacyAccountID: legacyID,
			CommerceSubscriptionID: sub, CommerceInvoiceID: invoice, PriceVersionID: "pro-v1",
		}
	}
	if bad, err := domain.BackfillLegacyProvenance(ctx, wrong(domainReceipt.SubscriptionID, domainReceipt.InvoiceID)); err != nil || bad.Status != "manual_review" {
		t.Fatalf("domain wrong provenance: %+v err=%v", bad, err)
	}
	runAdmin("C39", legacyID, "history-parity-wrong-provenance", marshal(adminBackfillProvenancePayload{
		LegacyInvoiceID: "legacy:wrong-parity", LegacySubscriptionID: "legacy:sub-parity",
		CommerceSubscriptionID: adminReceipt.SubscriptionID, CommerceInvoiceID: adminReceipt.InvoiceID, PriceVersionID: "pro-v1",
	}), true)
	for _, item := range []struct {
		l       *Lab
		receipt Receipt
	}{{domain, domainReceipt}, {admin, adminReceipt}} {
		var status string
		if err := item.l.db.QueryRowContext(ctx, `SELECT status FROM legacy_provenance WHERE legacy_invoice_id='legacy:wrong-parity'`).Scan(&status); err != nil || status != "manual_review" {
			t.Fatalf("wrong provenance status=%s err=%v", status, err)
		}
		state, err := item.l.Snapshot(ctx, item.receipt.SubscriptionID)
		if err != nil || state.OperationStatus != "unknown" || state.AllocatedMinor != 0 {
			t.Fatalf("unknown payment prematurely settled: %+v err=%v", state, err)
		}
	}
	if found, err := domain.ReconcilePayment(ctx, domainReceipt.OperationID); err != nil || !found {
		t.Fatalf("domain provider lookup: found=%v err=%v", found, err)
	}
	runAdmin("C10", adminReceipt.OperationID, "history-parity-provider-lookup", json.RawMessage(`{}`), false)
	for _, l := range []*Lab{domain, admin} {
		if err := l.RebuildEntitlements(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := domain.ShadowEntitlement(ctx, legacyID, domainReceipt.SubscriptionID, "active"); err != nil {
		t.Fatal(err)
	}
	runAdmin("C38", legacyID, "history-parity-active-shadow", marshal(adminShadowEntitlementPayload{
		SubscriptionID: adminReceipt.SubscriptionID, LegacyStatus: "active",
	}), false)
	if _, err := domain.RunReconciliation(ctx, fixedNow); err != nil {
		t.Fatal(err)
	}
	runAdmin("C33", "", "history-parity-second-reconciliation", marshal(AdminReconciliationPayload{AsOf: fixedNow.Format(time.RFC3339Nano)}), false)
	compareReadiness("manual provenance review", cutoverLimits, false, false, 0)
	if corrected, err := domain.ResolveLegacyProvenance(ctx, LegacyProvenance{
		LegacyInvoiceID: "legacy:wrong-parity", LegacySubscriptionID: "legacy:sub-parity", LegacyAccountID: legacyID,
		CommerceSubscriptionID: domainReceipt.SubscriptionID, CommerceInvoiceID: domainReceipt.InvoiceID, PriceVersionID: "basic-v1",
	}, "local-admin", "verified original invoice"); err != nil || corrected.Status != "complete" {
		t.Fatalf("domain provenance review: %+v err=%v", corrected, err)
	}
	runAdmin("C40", "legacy:wrong-parity", "history-parity-resolve-provenance", marshal(adminResolveProvenancePayload{
		CommerceSubscriptionID: adminReceipt.SubscriptionID, CommerceInvoiceID: adminReceipt.InvoiceID,
		PriceVersionID: "basic-v1", Decision: "verified original invoice",
	}), true)
	compareReadiness("reviewed provenance", cutoverLimits, true, true, 0)
	for _, item := range []struct {
		l       *Lab
		receipt Receipt
	}{{domain, domainReceipt}, {admin, adminReceipt}} {
		var status, version string
		if err := item.l.db.QueryRowContext(ctx, `SELECT status,price_version_id FROM legacy_provenance WHERE legacy_invoice_id='legacy:wrong-parity'`).Scan(&status, &version); err != nil || status != "complete" || version != "basic-v1" {
			t.Fatalf("reviewed provenance status=%s version=%s err=%v", status, version, err)
		}
		balance, err := item.l.Balance(ctx, item.receipt.InvoiceID)
		if err != nil || balance.NetAppliedMinor != 2000 || balance.OutstandingMinor != 0 {
			t.Fatalf("review changed payment facts: %+v err=%v", balance, err)
		}
		if count := captureCount(t, item.l); count != 1 {
			t.Fatalf("review changed provider captures: %d", count)
		}
	}
}
