package lab

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestAdminContractNet30MatchesDomainFinancialFacts(t *testing.T) {
	ctx := context.Background()
	now := fixedNow
	open := func() *Lab {
		dir := t.TempDir()
		l, err := Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), func() time.Time { return now })
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
	spec := acmeContract("")
	if _, err := domain.PublishContract(ctx, spec); err != nil {
		t.Fatal(err)
	}
	publishPayload, err := json.Marshal(adminPublishContractPayload{
		ID: spec.ID, CustomerID: spec.CustomerID, Version: "1", BasePriceVersionID: spec.BasePriceVersionID,
		FixedMinor: "4000", SeatMinor: "700", EffectiveFrom: spec.EffectiveFrom.Format(time.RFC3339Nano),
		EffectiveTo: spec.EffectiveTo.Format(time.RFC3339Nano),
	})
	if err != nil {
		t.Fatal(err)
	}
	publishPreview, err := admin.AdminCreatePreview(ctx, "local-admin", "C31", "", publishPayload)
	if err != nil {
		t.Fatal(err)
	}
	publishCommand, _, err := admin.AdminSubmitCommand(ctx, "local-admin", "parity-publish-contract", "C31", "", publishPayload, publishPreview.ID)
	if err != nil {
		t.Fatal(err)
	}
	publishCommand, err = admin.AdminExecuteCommand(ctx, publishCommand.ID)
	if err != nil || publishCommand.Status != "succeeded" {
		t.Fatalf("admin contract publication: %+v err=%v", publishCommand, err)
	}
	domainQuote, err := domain.CreateContractQuote(ctx, spec.CustomerID, spec.ID, 5)
	if err != nil {
		t.Fatal(err)
	}
	domainReceipt, err := domain.AcceptContractQuote(ctx, domainQuote.ID, domainQuote.Fingerprint, "parity-domain-contract-accept")
	if err != nil {
		t.Fatal(err)
	}
	quotePayload, err := json.Marshal(AdminCreateQuotePayload{CustomerID: spec.CustomerID, ContractVersionID: spec.ID, Seats: "5"})
	if err != nil {
		t.Fatal(err)
	}
	quoteCommand, _, err := admin.AdminSubmitCommand(ctx, "local-admin", "parity-contract-quote", "C01", "", quotePayload, "")
	if err != nil {
		t.Fatal(err)
	}
	quoteCommand, err = admin.AdminExecuteCommand(ctx, quoteCommand.ID)
	if err != nil || quoteCommand.Status != "succeeded" {
		t.Fatalf("admin contract quote: %+v err=%v", quoteCommand, err)
	}
	var quoteRefs map[string]string
	if err := json.Unmarshal(quoteCommand.ResultRefs, &quoteRefs); err != nil {
		t.Fatal(err)
	}
	var fingerprint string
	if err := admin.db.QueryRowContext(ctx, `SELECT fingerprint FROM quotes WHERE id=?`, quoteRefs["quote_id"]).Scan(&fingerprint); err != nil {
		t.Fatal(err)
	}
	acceptPayload, err := json.Marshal(AdminAcceptQuotePayload{Fingerprint: fingerprint})
	if err != nil {
		t.Fatal(err)
	}
	acceptPreview, err := admin.AdminCreatePreview(ctx, "local-admin", "C02", quoteRefs["quote_id"], acceptPayload)
	if err != nil {
		t.Fatal(err)
	}
	acceptCommand, _, err := admin.AdminSubmitCommand(ctx, "local-admin", "parity-contract-accept", "C02", quoteRefs["quote_id"], acceptPayload, acceptPreview.ID)
	if err != nil {
		t.Fatal(err)
	}
	acceptCommand, err = admin.AdminExecuteCommand(ctx, acceptCommand.ID)
	if err != nil || acceptCommand.Status != "succeeded" {
		t.Fatalf("admin contract acceptance: %+v err=%v", acceptCommand, err)
	}
	var acceptRefs map[string]string
	if err := json.Unmarshal(acceptCommand.ResultRefs, &acceptRefs); err != nil {
		t.Fatal(err)
	}
	for _, l := range []*Lab{domain, admin} {
		if err := l.RefreshEntitlements(ctx); err != nil {
			t.Fatal(err)
		}
	}
	domainFacts := loadPurchaseFacts(t, domain, domainReceipt.SubscriptionID)
	adminFacts := loadPurchaseFacts(t, admin, acceptRefs["subscription_id"])
	if !reflect.DeepEqual(domainFacts, adminFacts) || adminFacts.InvoiceMinor != 7500 || adminFacts.PaymentStatus != "created" {
		t.Fatalf("Net30 acceptance differs:\ndomain: %+v\nadmin: %+v", domainFacts, adminFacts)
	}
	for _, item := range []struct {
		lab       *Lab
		invoice   string
		operation string
	}{{domain, domainReceipt.InvoiceID, domainReceipt.OperationID}, {admin, acceptRefs["invoice_id"], acceptRefs["operation_id"]}} {
		var due int64
		var contractID string
		var outbox int
		if err := item.lab.db.QueryRowContext(ctx, `SELECT p.due_at,c.contract_version_id FROM billing_periods p JOIN invoice_contracts c ON c.invoice_id=p.invoice_id WHERE p.invoice_id=?`, item.invoice).Scan(&due, &contractID); err != nil {
			t.Fatal(err)
		}
		if err := item.lab.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox WHERE id=?`, "capture:"+item.operation).Scan(&outbox); err != nil {
			t.Fatal(err)
		}
		if due != time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC).UnixNano() || contractID != spec.ID || outbox != 0 {
			t.Fatalf("Net30 charged before due: due=%d contract=%s outbox=%d", due, contractID, outbox)
		}
	}
	now = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if renewals, err := domain.RunRenewals(ctx); err != nil || len(renewals) != 0 {
		t.Fatalf("domain contract renewal should hold: count=%d err=%v", len(renewals), err)
	}
	renewalPayload := json.RawMessage(`{}`)
	renewalPreview, err := admin.AdminCreatePreview(ctx, "local-admin", "C44", "", renewalPayload)
	if err != nil {
		t.Fatal(err)
	}
	renewalCommand, _, err := admin.AdminSubmitCommand(ctx, "local-admin", "parity-contract-renewal-hold", "C44", "", renewalPayload, renewalPreview.ID)
	if err != nil {
		t.Fatal(err)
	}
	renewalCommand, err = admin.AdminExecuteCommand(ctx, renewalCommand.ID)
	if err != nil || renewalCommand.Status != "succeeded" {
		t.Fatalf("admin contract renewal hold: %+v err=%v", renewalCommand, err)
	}
	for _, item := range []struct {
		lab *Lab
		sub string
	}{{domain, domainReceipt.SubscriptionID}, {admin, acceptRefs["subscription_id"]}} {
		var reason string
		var nextPeriods int
		if err := item.lab.db.QueryRowContext(ctx, `SELECT reason FROM renewal_holds WHERE subscription_id=? AND period_index=1`, item.sub).Scan(&reason); err != nil || reason != "contract_next_price_missing" {
			t.Fatalf("contract renewal hold reason=%q err=%v", reason, err)
		}
		if err := item.lab.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM billing_periods WHERE subscription_id=? AND period_index=1`, item.sub).Scan(&nextPeriods); err != nil || nextPeriods != 0 {
			t.Fatalf("contract renewal invented next period=%d err=%v", nextPeriods, err)
		}
		if err := item.lab.RefreshEntitlements(ctx); err != nil {
			t.Fatal(err)
		}
		state, err := item.lab.Snapshot(ctx, item.sub)
		if err != nil || state.EntitlementStatus != "suspended" {
			t.Fatalf("missing next price did not suspend entitlement: %+v err=%v", state, err)
		}
	}
	if count, err := domain.CollectDueContractInvoices(ctx); err != nil || count != 1 {
		t.Fatalf("domain due collections=%d err=%v", count, err)
	}
	collectPayload := json.RawMessage(`{}`)
	collectPreview, err := admin.AdminCreatePreview(ctx, "local-admin", "C32", "", collectPayload)
	if err != nil {
		t.Fatal(err)
	}
	collectCommand, _, err := admin.AdminSubmitCommand(ctx, "local-admin", "parity-contract-collection", "C32", "", collectPayload, collectPreview.ID)
	if err != nil {
		t.Fatal(err)
	}
	collectCommand, err = admin.AdminExecuteCommand(ctx, collectCommand.ID)
	if err != nil || collectCommand.Status != "succeeded" {
		t.Fatalf("admin due collection: %+v err=%v", collectCommand, err)
	}
	for _, item := range []struct {
		lab       *Lab
		operation string
	}{{domain, domainReceipt.OperationID}, {admin, acceptRefs["operation_id"]}} {
		var outbox int
		if err := item.lab.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox WHERE id=?`, "capture:"+item.operation).Scan(&outbox); err != nil || outbox != 1 {
			t.Fatalf("due collection outbox=%d err=%v", outbox, err)
		}
		if _, err := item.lab.DispatchNext(ctx, ""); err != nil {
			t.Fatal(err)
		}
	}
	domainBalance, err := domain.Balance(ctx, domainReceipt.InvoiceID)
	if err != nil {
		t.Fatal(err)
	}
	adminBalance, err := admin.Balance(ctx, acceptRefs["invoice_id"])
	if err != nil {
		t.Fatal(err)
	}
	domainBalance.InvoiceID, adminBalance.InvoiceID = "", ""
	if !reflect.DeepEqual(domainBalance, adminBalance) || adminBalance.NetAppliedMinor != 7500 || adminBalance.OutstandingMinor != 0 {
		t.Fatalf("Net30 settled balance differs:\ndomain: %+v\nadmin: %+v", domainBalance, adminBalance)
	}
	if got, want := captureCount(t, admin), captureCount(t, domain); got != want || got != 1 {
		t.Fatalf("Net30 capture count: admin=%d domain=%d", got, want)
	}
}
