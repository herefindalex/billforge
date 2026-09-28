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

type purchaseFacts struct {
	PriceVersionID string
	Seats          int64
	Revision       int64
	Status         string
	InvoiceMinor   int64
	Currency       string
	PaymentMinor   int64
	PaymentStatus  string
	Entitlement    string
	PeriodStart    int64
	PeriodEnd      int64
	AssignmentID   string
	AssignedSeats  int64
	InvoiceLines   []string
}

type renewalFacts struct {
	PeriodStart   int64
	PeriodEnd     int64
	DueAt         int64
	InvoiceMinor  int64
	Currency      string
	PaymentMinor  int64
	PaymentStatus string
	Allocated     int64
	CaptureOutbox int
	PriceVersion  string
	Seats         int64
	InvoiceLines  []string
}

func loadRenewalFacts(t *testing.T, l *Lab, subID string) renewalFacts {
	return loadRenewalFactsAt(t, l, subID, 1)
}

func loadRenewalFactsAt(t *testing.T, l *Lab, subID string, periodIndex int) renewalFacts {
	t.Helper()
	ctx := context.Background()
	var facts renewalFacts
	var invoiceID string
	if err := l.db.QueryRowContext(ctx, `SELECT p.period_start,p.period_end,p.due_at,i.id,i.total_minor,i.currency,
		o.amount_minor,o.status,s.price_version_id,s.seat_quantity,
		COALESCE((SELECT SUM(a.amount_minor) FROM allocations a WHERE a.invoice_id=i.id),0),
		(SELECT COUNT(*) FROM outbox b WHERE b.id='capture:'||o.id)
		FROM billing_periods p JOIN invoices i ON i.id=p.invoice_id
		JOIN payment_operations o ON o.invoice_id=i.id
		JOIN subscriptions s ON s.id=p.subscription_id
		WHERE p.subscription_id=? AND p.period_index=?`, subID, periodIndex).Scan(
		&facts.PeriodStart, &facts.PeriodEnd, &facts.DueAt, &invoiceID, &facts.InvoiceMinor,
		&facts.Currency, &facts.PaymentMinor, &facts.PaymentStatus, &facts.PriceVersion,
		&facts.Seats, &facts.Allocated, &facts.CaptureOutbox,
	); err != nil {
		t.Fatal(err)
	}
	rows, err := l.db.QueryContext(ctx, `SELECT component_code,amount_minor FROM invoice_lines WHERE invoice_id=? ORDER BY component_code`, invoiceID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var code string
		var amount int64
		if err := rows.Scan(&code, &amount); err != nil {
			t.Fatal(err)
		}
		facts.InvoiceLines = append(facts.InvoiceLines, fmt.Sprintf("%s:%d", code, amount))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return facts
}

func TestAdminMonthEndRenewalMatchesDomainFinancialFacts(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2027, 1, 31, 9, 30, 0, 0, time.UTC)
	open := func() *Lab {
		dir := t.TempDir()
		l, err := Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), func() time.Time { return now })
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = l.Close() })
		return l
	}
	domain := open()
	admin := open()
	if err := admin.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	domainPurchase := purchase(t, domain)
	adminPurchase := purchase(t, admin)
	for _, l := range []*Lab{domain, admin} {
		if _, err := l.DispatchNext(ctx, ""); err != nil {
			t.Fatal(err)
		}
		if err := l.RebuildEntitlements(ctx); err != nil {
			t.Fatal(err)
		}
	}
	periods, err := domain.Periods(ctx, domainPurchase.SubscriptionID)
	if err != nil || len(periods) != 1 {
		t.Fatalf("initial periods=%d err=%v", len(periods), err)
	}
	now = periods[0].End
	if now.Month() != time.February || now.Day() != 28 {
		t.Fatalf("initial month-end anchor moved to %s", now)
	}
	if receipts, err := domain.RunRenewals(ctx); err != nil || len(receipts) != 1 {
		t.Fatalf("domain renewals=%d err=%v", len(receipts), err)
	}
	payload := json.RawMessage(`{}`)
	preview, err := admin.AdminCreatePreview(ctx, "local-admin", "C44", "", payload)
	if err != nil {
		t.Fatal(err)
	}
	command, _, err := admin.AdminSubmitCommand(ctx, "local-admin", "parity-month-end-renewal", "C44", "", payload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	command, err = admin.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "succeeded" {
		t.Fatalf("admin renewal=%+v err=%v", command, err)
	}
	beforeDomain := loadRenewalFacts(t, domain, domainPurchase.SubscriptionID)
	beforeAdmin := loadRenewalFacts(t, admin, adminPurchase.SubscriptionID)
	if !reflect.DeepEqual(beforeDomain, beforeAdmin) {
		t.Fatalf("renewal obligation differs before capture:\ndomain: %+v\nadmin: %+v", beforeDomain, beforeAdmin)
	}
	for _, l := range []*Lab{domain, admin} {
		if _, err := l.DispatchNext(ctx, ""); err != nil {
			t.Fatal(err)
		}
	}
	afterDomain := loadRenewalFacts(t, domain, domainPurchase.SubscriptionID)
	afterAdmin := loadRenewalFacts(t, admin, adminPurchase.SubscriptionID)
	if !reflect.DeepEqual(afterDomain, afterAdmin) {
		t.Fatalf("renewal settlement differs after capture:\ndomain: %+v\nadmin: %+v", afterDomain, afterAdmin)
	}
	if afterAdmin.PaymentStatus != "succeeded" || afterAdmin.Allocated != 2000 || afterAdmin.InvoiceMinor != 2000 || afterAdmin.CaptureOutbox != 1 {
		t.Fatalf("unexpected month-end renewal financial facts: %+v", afterAdmin)
	}
}

func TestAdminPartPaidReductionMatchesDomainFinancialFacts(t *testing.T) {
	ctx := context.Background()
	open := func() *Lab {
		dir := t.TempDir()
		l, err := Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), func() time.Time { return fixedNow })
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = l.Close() })
		return l
	}
	domain := open()
	admin := open()
	if err := admin.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	domainPurchase := purchaseHundred(t, domain, "parity-part-paid", "parity-checkout")
	adminPurchase := purchaseHundred(t, admin, "parity-part-paid", "parity-checkout")
	for _, item := range []struct {
		lab     *Lab
		invoice string
	}{{domain, domainPurchase.InvoiceID}, {admin, adminPurchase.InvoiceID}} {
		if _, err := item.lab.CreatePayment(ctx, item.invoice, 6000, "parity-pay-sixty"); err != nil {
			t.Fatal(err)
		}
		if _, err := item.lab.DispatchNext(ctx, ""); err != nil {
			t.Fatal(err)
		}
	}
	domainCorrection, err := domain.PostReduction(ctx, domainPurchase.InvoiceID, 2000, "service correction", "parity-domain-reduction")
	if err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{"reduction_minor":"2000","reason":"service correction"}`)
	preview, err := admin.AdminCreatePreview(ctx, "local-admin", "C11", adminPurchase.InvoiceID, payload)
	if err != nil {
		t.Fatal(err)
	}
	command, _, err := admin.AdminSubmitCommand(ctx, "local-admin", "parity-admin-reduction", "C11", adminPurchase.InvoiceID, payload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	command, err = admin.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "succeeded" {
		t.Fatalf("admin reduction=%+v err=%v", command, err)
	}
	if len(domainCorrection.GrantIDs) != 0 {
		t.Fatalf("part-paid domain reduction created funded credit: %+v", domainCorrection)
	}
	loadBalance := func(l *Lab, invoiceID string) InvoiceBalance {
		b, err := l.Balance(ctx, invoiceID)
		if err != nil {
			t.Fatal(err)
		}
		b.InvoiceID = ""
		return b
	}
	domainBalance := loadBalance(domain, domainPurchase.InvoiceID)
	adminBalance := loadBalance(admin, adminPurchase.InvoiceID)
	if !reflect.DeepEqual(domainBalance, adminBalance) {
		t.Fatalf("part-paid reduction differs:\ndomain: %+v\nadmin: %+v", domainBalance, adminBalance)
	}
	if adminBalance.OriginalMinor != 10000 || adminBalance.ReductionsMinor != 2000 || adminBalance.NetAppliedMinor != 6000 || adminBalance.OutstandingMinor != 2000 {
		t.Fatalf("unexpected part-paid balance: %+v", adminBalance)
	}
	for _, item := range []struct {
		lab       *Lab
		operation string
	}{{domain, domainPurchase.OperationID}, {admin, adminPurchase.OperationID}} {
		var status string
		if err := item.lab.db.QueryRowContext(ctx, `SELECT status FROM payment_operations WHERE id=?`, item.operation).Scan(&status); err != nil || status != "cancelled" {
			t.Fatalf("obsolete full-price payment status=%q err=%v", status, err)
		}
	}
	for _, item := range []struct {
		lab     *Lab
		invoice string
	}{{domain, domainPurchase.InvoiceID}, {admin, adminPurchase.InvoiceID}} {
		var grants int
		if err := item.lab.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM credit_grants WHERE source_invoice_id=?`, item.invoice).Scan(&grants); err != nil || grants != 0 {
			t.Fatalf("part-paid reduction funded grants=%d err=%v", grants, err)
		}
		if _, err := item.lab.CreatePayment(ctx, item.invoice, 2000, "parity-pay-remainder"); err != nil {
			t.Fatal(err)
		}
		if _, err := item.lab.DispatchNext(ctx, ""); err != nil {
			t.Fatal(err)
		}
	}
	domainBalance = loadBalance(domain, domainPurchase.InvoiceID)
	adminBalance = loadBalance(admin, adminPurchase.InvoiceID)
	if !reflect.DeepEqual(domainBalance, adminBalance) || adminBalance.OutstandingMinor != 0 || adminBalance.NetAppliedMinor != 8000 {
		t.Fatalf("settled part-paid reduction differs:\ndomain: %+v\nadmin: %+v", domainBalance, adminBalance)
	}
	if got, want := captureCount(t, admin), captureCount(t, domain); got != want || got != 2 {
		t.Fatalf("provider captures after corrected payments: admin=%d domain=%d", got, want)
	}
}

func TestAdminFundedCreditApplicationMatchesDomainFinancialFacts(t *testing.T) {
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
	domain := open()
	admin := open()
	if err := admin.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	domainPaid := paidBasicSubscription(t, domain, "parity-funded-credit")
	adminPaid := paidBasicSubscription(t, admin, "parity-funded-credit")
	domainCorrection, err := domain.PostReduction(ctx, domainPaid.InvoiceID, 1000, "service credit", "parity-domain-credit")
	if err != nil || len(domainCorrection.GrantIDs) != 1 {
		t.Fatalf("domain funded reduction: %+v err=%v", domainCorrection, err)
	}
	reductionPayload := json.RawMessage(`{"reduction_minor":"1000","reason":"service credit"}`)
	preview, err := admin.AdminCreatePreview(ctx, "local-admin", "C11", adminPaid.InvoiceID, reductionPayload)
	if err != nil {
		t.Fatal(err)
	}
	command, _, err := admin.AdminSubmitCommand(ctx, "local-admin", "parity-admin-credit", "C11", adminPaid.InvoiceID, reductionPayload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	command, err = admin.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "succeeded" {
		t.Fatalf("admin funded reduction: %+v err=%v", command, err)
	}
	var reductionRefs struct {
		GrantIDs []string `json:"grant_ids"`
	}
	if err := json.Unmarshal(command.ResultRefs, &reductionRefs); err != nil || len(reductionRefs.GrantIDs) != 1 {
		t.Fatalf("admin reduction grants: %+v err=%v", reductionRefs, err)
	}
	now = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	domainRenewals, err := domain.RunRenewals(ctx)
	if err != nil || len(domainRenewals) != 1 {
		t.Fatalf("domain renewals=%d err=%v", len(domainRenewals), err)
	}
	adminRenewals, err := admin.RunRenewals(ctx)
	if err != nil || len(adminRenewals) != 1 {
		t.Fatalf("admin renewals=%d err=%v", len(adminRenewals), err)
	}
	if _, err := domain.ApplyCredit(ctx, domainCorrection.GrantIDs[0], domainRenewals[0].InvoiceID, 500, "parity-domain-apply"); err != nil {
		t.Fatal(err)
	}
	creditPayload, err := json.Marshal(AdminApplyCreditPayload{InvoiceID: adminRenewals[0].InvoiceID, AmountMinor: "500"})
	if err != nil {
		t.Fatal(err)
	}
	creditPreview, err := admin.AdminCreatePreview(ctx, "local-admin", "C12", reductionRefs.GrantIDs[0], creditPayload)
	if err != nil {
		t.Fatal(err)
	}
	creditCommand, _, err := admin.AdminSubmitCommand(ctx, "local-admin", "parity-admin-apply", "C12", reductionRefs.GrantIDs[0], creditPayload, creditPreview.ID)
	if err != nil {
		t.Fatal(err)
	}
	creditCommand, err = admin.AdminExecuteCommand(ctx, creditCommand.ID)
	if err != nil || creditCommand.Status != "succeeded" {
		t.Fatalf("admin apply credit: %+v err=%v", creditCommand, err)
	}
	domainCredit, err := domain.CreditBalance(ctx, domainCorrection.GrantIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	adminCredit, err := admin.CreditBalance(ctx, reductionRefs.GrantIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	domainCredit.GrantID, adminCredit.GrantID = "", ""
	if !reflect.DeepEqual(domainCredit, adminCredit) || adminCredit.GrantedMinor != 1000 || adminCredit.AppliedMinor != 500 || adminCredit.AvailableMinor != 500 {
		t.Fatalf("credit funding differs:\ndomain: %+v\nadmin: %+v", domainCredit, adminCredit)
	}
	loadBalance := func(l *Lab, invoiceID string) InvoiceBalance {
		b, err := l.Balance(ctx, invoiceID)
		if err != nil {
			t.Fatal(err)
		}
		b.InvoiceID = ""
		return b
	}
	domainSource := loadBalance(domain, domainPaid.InvoiceID)
	adminSource := loadBalance(admin, adminPaid.InvoiceID)
	if !reflect.DeepEqual(domainSource, adminSource) || adminSource.ReleasedMinor != 1000 || adminSource.OutstandingMinor != 0 {
		t.Fatalf("source invoice differs:\ndomain: %+v\nadmin: %+v", domainSource, adminSource)
	}
	domainTarget := loadBalance(domain, domainRenewals[0].InvoiceID)
	adminTarget := loadBalance(admin, adminRenewals[0].InvoiceID)
	if !reflect.DeepEqual(domainTarget, adminTarget) || adminTarget.CreditAppliedMinor != 500 || adminTarget.OutstandingMinor != 1500 {
		t.Fatalf("target invoice differs:\ndomain: %+v\nadmin: %+v", domainTarget, adminTarget)
	}
}

func TestAdminScheduledPlanRenewalMatchesDomainFinancialFacts(t *testing.T) {
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
	domain := open()
	admin := open()
	if err := admin.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	domainPaid := paidBasicSubscription(t, domain, "parity-scheduled-plan")
	adminPaid := paidBasicSubscription(t, admin, "parity-scheduled-plan")
	if _, err := domain.ScheduleNextPlan(ctx, domainPaid.SubscriptionID, "pro", 5, 1, "parity-domain-next-pro"); err != nil {
		t.Fatal(err)
	}
	quotePayload, err := json.Marshal(AdminCreateQuotePayload{
		CustomerID:           "parity-scheduled-plan",
		PlanID:               "pro",
		Cohort:               "default",
		Seats:                "5",
		ChangeSubscriptionID: adminPaid.SubscriptionID,
		Mode:                 "next_period",
		Revision:             "1",
	})
	if err != nil {
		t.Fatal(err)
	}
	quoteCommand, _, err := admin.AdminSubmitCommand(ctx, "local-admin", "parity-change-quote", "C01", "", quotePayload, "")
	if err != nil {
		t.Fatal(err)
	}
	quoteCommand, err = admin.AdminExecuteCommand(ctx, quoteCommand.ID)
	if err != nil || quoteCommand.Status != "succeeded" {
		t.Fatalf("admin change quote: %+v err=%v", quoteCommand, err)
	}
	var quoteRefs map[string]string
	if err := json.Unmarshal(quoteCommand.ResultRefs, &quoteRefs); err != nil {
		t.Fatal(err)
	}
	changePayload, err := json.Marshal(AdminChangePlanPayload{
		QuoteID: quoteRefs["quote_id"], Fingerprint: quoteRefs["binding_fingerprint"], Revision: "1",
	})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := admin.AdminCreatePreview(ctx, "local-admin", "C03", adminPaid.SubscriptionID, changePayload)
	if err != nil {
		t.Fatal(err)
	}
	changeCommand, _, err := admin.AdminSubmitCommand(ctx, "local-admin", "parity-schedule-pro", "C03", adminPaid.SubscriptionID, changePayload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	changeCommand, err = admin.AdminExecuteCommand(ctx, changeCommand.ID)
	if err != nil || changeCommand.Status != "succeeded" {
		t.Fatalf("admin scheduled change: %+v err=%v", changeCommand, err)
	}
	for _, item := range []struct {
		lab *Lab
		sub string
	}{{domain, domainPaid.SubscriptionID}, {admin, adminPaid.SubscriptionID}} {
		var current, target string
		var seats, revision int64
		if err := item.lab.db.QueryRowContext(ctx, `SELECT s.price_version_id,s.seat_quantity,s.revision,sc.target_price_version_id FROM subscriptions s JOIN subscription_schedules sc ON sc.subscription_id=s.id AND sc.status='scheduled' WHERE s.id=?`, item.sub).Scan(&current, &seats, &revision, &target); err != nil {
			t.Fatal(err)
		}
		if current != "basic-v1" || seats != 0 || revision != 2 || target != "pro-v1" {
			t.Fatalf("scheduled change altered current plan early: price=%s seats=%d revision=%d target=%s", current, seats, revision, target)
		}
	}
	now = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if receipts, err := domain.RunRenewals(ctx); err != nil || len(receipts) != 1 {
		t.Fatalf("domain renewal=%d err=%v", len(receipts), err)
	}
	renewalPayload := json.RawMessage(`{}`)
	renewalPreview, err := admin.AdminCreatePreview(ctx, "local-admin", "C44", "", renewalPayload)
	if err != nil {
		t.Fatal(err)
	}
	renewalCommand, _, err := admin.AdminSubmitCommand(ctx, "local-admin", "parity-scheduled-renewal", "C44", "", renewalPayload, renewalPreview.ID)
	if err != nil {
		t.Fatal(err)
	}
	renewalCommand, err = admin.AdminExecuteCommand(ctx, renewalCommand.ID)
	if err != nil || renewalCommand.Status != "succeeded" {
		t.Fatalf("admin scheduled renewal: %+v err=%v", renewalCommand, err)
	}
	domainFacts := loadRenewalFacts(t, domain, domainPaid.SubscriptionID)
	adminFacts := loadRenewalFacts(t, admin, adminPaid.SubscriptionID)
	if !reflect.DeepEqual(domainFacts, adminFacts) || adminFacts.PriceVersion != "pro-v1" || adminFacts.Seats != 5 || adminFacts.InvoiceMinor != 10000 {
		t.Fatalf("scheduled renewal differs:\ndomain: %+v\nadmin: %+v", domainFacts, adminFacts)
	}
}

func loadPurchaseFacts(t *testing.T, l *Lab, subID string) purchaseFacts {
	t.Helper()
	ctx := context.Background()
	var facts purchaseFacts
	var invoiceID string
	err := l.db.QueryRowContext(ctx, `SELECT s.price_version_id,s.seat_quantity,s.revision,s.status,i.id,i.total_minor,i.currency,o.amount_minor,o.status,e.status,p.period_start,p.period_end,a.price_version_id,a.seat_quantity
		FROM subscriptions s
		JOIN billing_periods p ON p.subscription_id=s.id AND p.period_index=0
		JOIN invoices i ON i.id=p.invoice_id
		JOIN payment_operations o ON o.invoice_id=i.id
		JOIN entitlements e ON e.subscription_id=s.id
		JOIN pricing_assignments a ON a.subscription_id=s.id AND a.assignment_index=0
		WHERE s.id=?`, subID).Scan(&facts.PriceVersionID, &facts.Seats, &facts.Revision, &facts.Status, &invoiceID, &facts.InvoiceMinor, &facts.Currency, &facts.PaymentMinor, &facts.PaymentStatus, &facts.Entitlement, &facts.PeriodStart, &facts.PeriodEnd, &facts.AssignmentID, &facts.AssignedSeats)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := l.db.QueryContext(ctx, `SELECT component_code,amount_minor FROM invoice_lines WHERE invoice_id=? ORDER BY component_code`, invoiceID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var component string
		var amount int64
		if err := rows.Scan(&component, &amount); err != nil {
			t.Fatal(err)
		}
		facts.InvoiceLines = append(facts.InvoiceLines, fmt.Sprintf("%s:%d", component, amount))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return facts
}

func TestAdminProPurchaseMatchesDomainFinancialFacts(t *testing.T) {
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
	domain := open()
	quote, err := domain.CreateQuoteWithSeats(ctx, "parity-pro", "pro", 5)
	if err != nil {
		t.Fatal(err)
	}
	purchased, err := domain.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "domain-parity-pro")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := domain.DispatchNext(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if err := domain.RefreshEntitlements(ctx); err != nil {
		t.Fatal(err)
	}

	admin := open()
	if err := admin.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	created, _, err := admin.AdminSubmitCommand(ctx, "local-admin", "admin-parity-quote", "C01", "", json.RawMessage(`{"customer_id":"parity-pro","plan_id":"pro","cohort":"default","seats":"5"}`), "")
	if err != nil {
		t.Fatal(err)
	}
	created, err = admin.AdminExecuteCommand(ctx, created.ID)
	if err != nil || created.Status != "succeeded" {
		t.Fatalf("admin quote: %+v, %v", created, err)
	}
	var quoteRefs map[string]string
	if err := json.Unmarshal(created.ResultRefs, &quoteRefs); err != nil {
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
	preview, err := admin.AdminCreatePreview(ctx, "local-admin", "C02", quoteRefs["quote_id"], acceptPayload)
	if err != nil {
		t.Fatal(err)
	}
	accepted, _, err := admin.AdminSubmitCommand(ctx, "local-admin", "admin-parity-accept", "C02", quoteRefs["quote_id"], acceptPayload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err = admin.AdminExecuteCommand(ctx, accepted.ID)
	if err != nil || accepted.Status != "succeeded" {
		t.Fatalf("admin accept: %+v, %v", accepted, err)
	}
	var acceptRefs map[string]string
	if err := json.Unmarshal(accepted.ResultRefs, &acceptRefs); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.DispatchNext(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if err := admin.RefreshEntitlements(ctx); err != nil {
		t.Fatal(err)
	}

	want := loadPurchaseFacts(t, domain, purchased.SubscriptionID)
	got := loadPurchaseFacts(t, admin, acceptRefs["subscription_id"])
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("admin purchase facts differ from domain path:\nadmin: %+v\ndomain: %+v", got, want)
	}
	if got.InvoiceMinor != 10000 || got.PaymentMinor != 10000 || got.PaymentStatus != "succeeded" || got.Entitlement != "active" || len(got.InvoiceLines) != 2 {
		t.Fatalf("unexpected Pro five-seat facts: %+v", got)
	}
}

type immediateUpgradeFacts struct {
	TargetPriceID   string
	Seats           int64
	OldCreditMinor  int64
	NewChargeMinor  int64
	QuotedMinor     int64
	ActualMinor     int64
	CorrectionMinor int64
	ChangeStatus    string
	InvoiceMinor    int64
	PaymentMinor    int64
	PaymentStatus   string
	CurrentPriceID  string
	Entitlement     string
}

func TestAdminImmediateUpgradeMatchesDomainFinancialFacts(t *testing.T) {
	ctx := context.Background()
	run := func(t *testing.T, throughAdmin bool) immediateUpgradeFacts {
		t.Helper()
		l, clock := openChangingLab(t)
		if throughAdmin {
			if err := l.InitAdmin(ctx); err != nil {
				t.Fatal(err)
			}
		}
		paid := paidBasicSubscription(t, l, "parity-upgrade")
		*clock = time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)
		quote, err := l.CreateQuoteForCohort(ctx, "parity-upgrade", "pro", "default", 5)
		if err != nil {
			t.Fatal(err)
		}
		binding, err := l.BindChangeQuote(ctx, quote.ID, paid.SubscriptionID, "immediate", 1)
		if err != nil {
			t.Fatal(err)
		}
		var changeID string
		if throughAdmin {
			payload, err := json.Marshal(AdminChangePlanPayload{QuoteID: quote.ID, Fingerprint: binding.Fingerprint, Revision: "1"})
			if err != nil {
				t.Fatal(err)
			}
			preview, err := l.AdminCreatePreview(ctx, "local-admin", "C04", paid.SubscriptionID, payload)
			if err != nil {
				t.Fatal(err)
			}
			command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "admin-parity-upgrade", "C04", paid.SubscriptionID, payload, preview.ID)
			if err != nil {
				t.Fatal(err)
			}
			command, err = l.AdminExecuteCommand(ctx, command.ID)
			if err != nil || command.Status != "succeeded" {
				t.Fatalf("admin upgrade: %+v, %v", command, err)
			}
			var refs map[string]string
			if err := json.Unmarshal(command.ResultRefs, &refs); err != nil {
				t.Fatal(err)
			}
			changeID = refs["change_id"]
		} else {
			change, err := l.RequestImmediateProUpgradeAtPrice(ctx, paid.SubscriptionID, 5, 1, "domain-parity-upgrade", quote.ID, binding.Fingerprint)
			if err != nil {
				t.Fatal(err)
			}
			changeID = change.ID
		}
		if _, err := l.DispatchNext(ctx, ""); err != nil {
			t.Fatal(err)
		}
		if err := l.RefreshEntitlements(ctx); err != nil {
			t.Fatal(err)
		}
		change, err := loadImmediateChange(ctx, l.db, changeID)
		if err != nil {
			t.Fatal(err)
		}
		facts := immediateUpgradeFacts{
			TargetPriceID: change.TargetPriceVersionID, Seats: change.SeatQuantity,
			OldCreditMinor: change.OldCreditMinor, NewChargeMinor: change.NewChargeMinor,
			QuotedMinor: change.QuotedAmountMinor, ActualMinor: change.ActualAmountMinor,
			CorrectionMinor: change.CorrectionMinor, ChangeStatus: change.Status,
		}
		if err := l.db.QueryRowContext(ctx, `SELECT i.total_minor,o.amount_minor,o.status FROM invoices i JOIN payment_operations o ON o.invoice_id=i.id WHERE i.id=?`, change.InvoiceID).Scan(&facts.InvoiceMinor, &facts.PaymentMinor, &facts.PaymentStatus); err != nil {
			t.Fatal(err)
		}
		if err := l.db.QueryRowContext(ctx, `SELECT s.price_version_id,e.status FROM subscriptions s JOIN entitlements e ON e.subscription_id=s.id WHERE s.id=?`, paid.SubscriptionID).Scan(&facts.CurrentPriceID, &facts.Entitlement); err != nil {
			t.Fatal(err)
		}
		return facts
	}
	want := run(t, false)
	got := run(t, true)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("admin upgrade facts differ from domain path:\nadmin: %+v\ndomain: %+v", got, want)
	}
	if got.OldCreditMinor != 1000 || got.NewChargeMinor != 5000 || got.QuotedMinor != 4000 || got.ActualMinor != 4000 || got.InvoiceMinor != 4000 || got.PaymentMinor != 4000 || got.PaymentStatus != "succeeded" || got.ChangeStatus != "active" || got.CurrentPriceID != "pro-v1" {
		t.Fatalf("unexpected midpoint upgrade facts: %+v", got)
	}
}
