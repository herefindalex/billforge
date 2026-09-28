package lab

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestAdminExplicitPostContractPriceMatchesDomainFinancialFacts(t *testing.T) {
	ctx := context.Background()
	now := fixedNow
	open := func() *Lab {
		t.Helper()
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
	runAdmin := func(action, target, key string, payload json.RawMessage, needsPreview bool) AdminCommand {
		t.Helper()
		previewID := ""
		if needsPreview {
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
	refs := func(command AdminCommand) map[string]string {
		t.Helper()
		var result map[string]string
		if err := json.Unmarshal(command.ResultRefs, &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	comparePeriod := func(label string, index int, domainSub, adminSub string, expectedMinor, expectedApplied int64) {
		t.Helper()
		domainFacts := loadRenewalFactsAt(t, domain, domainSub, index)
		adminFacts := loadRenewalFactsAt(t, admin, adminSub, index)
		if !reflect.DeepEqual(domainFacts, adminFacts) || adminFacts.InvoiceMinor != expectedMinor || adminFacts.PaymentMinor != expectedMinor || adminFacts.Allocated != expectedApplied {
			t.Fatalf("%s financial facts differ:\ndomain: %+v\nadmin: %+v", label, domainFacts, adminFacts)
		}
		contractSource := func(l *Lab, subID string) string {
			t.Helper()
			var source string
			if err := l.db.QueryRowContext(ctx, `SELECT COALESCE((SELECT c.contract_version_id FROM invoice_contracts c WHERE c.invoice_id=p.invoice_id),'') FROM billing_periods p WHERE p.subscription_id=? AND p.period_index=?`, subID, index).Scan(&source); err != nil {
				t.Fatal(err)
			}
			return source
		}
		if d, a := contractSource(domain, domainSub), contractSource(admin, adminSub); d != a {
			t.Fatalf("%s contract source differs: domain=%q admin=%q", label, d, a)
		}
	}
	compareBalance := func(label, domainInvoice, adminInvoice string, expectedApplied int64) {
		t.Helper()
		d, err := domain.Balance(ctx, domainInvoice)
		if err != nil {
			t.Fatal(err)
		}
		a, err := admin.Balance(ctx, adminInvoice)
		if err != nil {
			t.Fatal(err)
		}
		d.InvoiceID, a.InvoiceID = "", ""
		if !reflect.DeepEqual(d, a) || a.NetAppliedMinor != expectedApplied || a.OutstandingMinor != 0 {
			t.Fatalf("%s balance differs: domain=%+v admin=%+v", label, d, a)
		}
	}
	compareTransition := func(domainSub, adminSub string) {
		t.Helper()
		read := func(l *Lab, subID string) string {
			t.Helper()
			var current, post, source string
			var assignments, transitions int
			var effective int64
			if err := l.db.QueryRowContext(ctx, `SELECT price_version_id FROM subscriptions WHERE id=?`, subID).Scan(&current); err != nil {
				t.Fatal(err)
			}
			if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pricing_assignments WHERE subscription_id=?`, subID).Scan(&assignments); err != nil {
				t.Fatal(err)
			}
			if err := l.db.QueryRowContext(ctx, `SELECT price_version_id,source_contract_id,effective_start FROM pricing_assignments WHERE subscription_id=? AND assignment_index=1`, subID).Scan(&post, &source, &effective); err != nil {
				t.Fatal(err)
			}
			if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM contract_transitions WHERE subscription_id=?`, subID).Scan(&transitions); err != nil {
				t.Fatal(err)
			}
			return fmt.Sprintf("%s/%s/%s/%d/%d/%d", current, post, source, effective, assignments, transitions)
		}
		want := fmt.Sprintf("pro-v1/pro-v1/acme-contract-v1/%d/2/1", time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC).UnixNano())
		if d, a := read(domain, domainSub), read(admin, adminSub); d != want || a != want {
			t.Fatalf("post-contract transition differs: domain=%s admin=%s want=%s", d, a, want)
		}
	}

	spec := acmeContract("pro-v1")
	spec.EffectiveTo = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	if _, err := domain.PublishContract(ctx, spec); err != nil {
		t.Fatal(err)
	}
	publishPayload, err := json.Marshal(adminPublishContractPayload{
		ID: spec.ID, CustomerID: spec.CustomerID, Version: "1", BasePriceVersionID: spec.BasePriceVersionID,
		FixedMinor: "4000", SeatMinor: "700", EffectiveFrom: spec.EffectiveFrom.Format(time.RFC3339Nano),
		EffectiveTo: spec.EffectiveTo.Format(time.RFC3339Nano), PostContractPriceVersionID: spec.PostContractPriceVersionID,
	})
	if err != nil {
		t.Fatal(err)
	}
	runAdmin("C31", "", "post-price-publish-contract", publishPayload, true)
	domainQuote, err := domain.CreateContractQuote(ctx, spec.CustomerID, spec.ID, 5)
	if err != nil {
		t.Fatal(err)
	}
	domainReceipt, err := domain.AcceptContractQuote(ctx, domainQuote.ID, domainQuote.Fingerprint, "post-price-domain-accept")
	if err != nil {
		t.Fatal(err)
	}
	quotePayload, err := json.Marshal(AdminCreateQuotePayload{CustomerID: spec.CustomerID, ContractVersionID: spec.ID, Seats: "5"})
	if err != nil {
		t.Fatal(err)
	}
	quoteID := refs(runAdmin("C01", "", "post-price-create-quote", quotePayload, false))["quote_id"]
	var fingerprint string
	if err := admin.db.QueryRowContext(ctx, `SELECT fingerprint FROM quotes WHERE id=?`, quoteID).Scan(&fingerprint); err != nil {
		t.Fatal(err)
	}
	acceptPayload, err := json.Marshal(AdminAcceptQuotePayload{Fingerprint: fingerprint})
	if err != nil {
		t.Fatal(err)
	}
	accepted := refs(runAdmin("C02", quoteID, "post-price-accept-quote", acceptPayload, true))
	domainSub, adminSub := domainReceipt.SubscriptionID, accepted["subscription_id"]
	for _, l := range []*Lab{domain, admin} {
		if err := l.RefreshEntitlements(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if d, a := loadPurchaseFacts(t, domain, domainSub), loadPurchaseFacts(t, admin, adminSub); !reflect.DeepEqual(d, a) || a.InvoiceMinor != 7500 {
		t.Fatalf("initial contract invoice differs: domain=%+v admin=%+v", d, a)
	}

	now = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if renewed, err := domain.RunRenewals(ctx); err != nil || len(renewed) != 1 || renewed[0].AmountMinor != 7500 {
		t.Fatalf("domain contract renewal: %+v err=%v", renewed, err)
	}
	runAdmin("C44", "", "post-price-renew-contract", json.RawMessage(`{}`), true)
	comparePeriod("contract renewal", 1, domainSub, adminSub, 7500, 0)
	if count, err := domain.CollectDueContractInvoices(ctx); err != nil || count != 1 {
		t.Fatalf("domain first collection: %d err=%v", count, err)
	}
	runAdmin("C32", "", "post-price-collect-first", json.RawMessage(`{}`), true)
	for _, l := range []*Lab{domain, admin} {
		if _, err := l.DispatchNext(ctx, ""); err != nil {
			t.Fatal(err)
		}
	}
	compareBalance("first Net30 invoice", domainReceipt.InvoiceID, accepted["invoice_id"], 7500)

	now = time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)
	if count, err := domain.CollectDueContractInvoices(ctx); err != nil || count != 1 {
		t.Fatalf("domain second collection: %d err=%v", count, err)
	}
	runAdmin("C32", "", "post-price-collect-second", json.RawMessage(`{}`), true)
	for _, l := range []*Lab{domain, admin} {
		if _, err := l.DispatchNext(ctx, ""); err != nil {
			t.Fatal(err)
		}
	}
	comparePeriod("settled contract renewal", 1, domainSub, adminSub, 7500, 7500)

	now = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	if renewed, err := domain.RunRenewals(ctx); err != nil || len(renewed) != 1 || renewed[0].AmountMinor != 10000 {
		t.Fatalf("domain post-contract renewal: %+v err=%v", renewed, err)
	}
	runAdmin("C44", "", "post-price-renew-self-serve", json.RawMessage(`{}`), true)
	comparePeriod("post-contract renewal", 2, domainSub, adminSub, 10000, 0)
	compareTransition(domainSub, adminSub)
	for _, l := range []*Lab{domain, admin} {
		if _, err := l.DispatchNext(ctx, ""); err != nil {
			t.Fatal(err)
		}
	}
	comparePeriod("settled post-contract renewal", 2, domainSub, adminSub, 10000, 10000)

	now = time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	if renewed, err := domain.RunRenewals(ctx); err != nil || len(renewed) != 1 || renewed[0].AmountMinor != 10000 {
		t.Fatalf("domain next self-serve renewal: %+v err=%v", renewed, err)
	}
	runAdmin("C44", "", "post-price-renew-next", json.RawMessage(`{}`), true)
	comparePeriod("next self-serve renewal", 3, domainSub, adminSub, 10000, 0)
	compareTransition(domainSub, adminSub)
	if got, want := captureCount(t, admin), captureCount(t, domain); got != want || got != 3 {
		t.Fatalf("capture count differs: admin=%d domain=%d", got, want)
	}
}
