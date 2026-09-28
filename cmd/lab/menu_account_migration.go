package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"billforge/lab"
)

func (m *menu) accountMigrationMenu() error {
	return m.loop("帳戶邊界與灰度遷移", []menuOption{
		{"1", "建立舊帳戶與 Commerce ID 映射", (*menu).linkLegacyAccount},
		{"2", "唯讀 shadow 報價比較", (*menu).shadowAccountQuote},
		{"3", "唯讀 shadow 權益比較", (*menu).shadowAccountEntitlement},
		{"4", "回填歷史發票來源", (*menu).backfillAccountProvenance},
		{"5", "審查並修正歷史來源映射", (*menu).resolveAccountProvenance},
		{"6", "查看切換準備度與擁有者", (*menu).showAccountMigrations},
		{"7", "切換讀取來源", (*menu).switchAccountRead},
		{"8", "切換命令寫入者", (*menu).switchAccountWriter},
		{"9", "停止新命令及遷移", (*menu).stopAccountMigration},
		{"10", "透過帳戶 adapter 查權益", (*menu).readAccountEntitlement},
	}, false)
}

func (m *menu) migrationLimits() (lab.MigrationThresholds, error) {
	raw, err := m.askDefault("Quote p95 最大毫秒數（依基線決定）", "5000")
	if err != nil {
		return lab.MigrationThresholds{}, err
	}
	p95, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || p95 <= 0 {
		return lab.MigrationThresholds{}, errors.New("p95 門檻須為正整數")
	}
	return lab.MigrationThresholds{MaxQuoteP95Millis: p95, MaxUnknownPayments: 0, MaxOpenDiscrepancies: 0}, nil
}

func (m *menu) pickAccountLink() (string, error) {
	links, err := m.lab.AccountLinks(context.Background())
	if err != nil {
		return "", err
	}
	var choices []menuChoice
	for _, a := range links {
		choices = append(choices, menuChoice{a.LegacyAccountID, fmt.Sprintf("%s / %s / read=%s writer=%s", a.CustomerID, a.Cohort, a.ReadOwner, a.WriterOwner)})
	}
	return m.pick("舊帳戶", choices)
}

func (m *menu) linkLegacyAccount() error {
	id, err := m.required("舊帳戶 ID")
	if err != nil {
		return err
	}
	customer, err := m.required("Commerce customer ID")
	if err != nil {
		return err
	}
	beneficiary, err := m.required("服務受益者 ID")
	if err != nil {
		return err
	}
	cohort, err := m.askDefault("cohort", "default")
	if err != nil {
		return err
	}
	history, err := m.askDefault("有歷史發票需回填？yes/no", "no")
	if err != nil {
		return err
	}
	if history != "yes" && history != "no" {
		return errors.New("請輸入 yes 或 no")
	}
	result, err := m.lab.LinkLegacyAccount(context.Background(), lab.AccountLink{LegacyAccountID: id, CustomerID: customer, BeneficiaryID: beneficiary, Cohort: cohort, HasHistory: history == "yes"})
	if err != nil {
		return err
	}
	return m.print(result)
}

func (m *menu) shadowAccountQuote() error {
	id, err := m.pickAccountLink()
	if err != nil || id == "" {
		return err
	}
	plan, err := m.required("方案 ID")
	if err != nil {
		return err
	}
	seatsRaw, err := m.askDefault("席次數（無席次則 0）", "0")
	if err != nil {
		return err
	}
	seats, err := strconv.ParseInt(seatsRaw, 10, 64)
	if err != nil || seats < 0 {
		return errors.New("席次須為非負整數")
	}
	amount, err := m.amount("舊系統報價")
	if err != nil {
		return err
	}
	currency, err := m.askDefault("幣別", "USD")
	if err != nil {
		return err
	}
	result, err := m.lab.ShadowQuote(context.Background(), id, plan, seats, amount, currency)
	if err != nil {
		return err
	}
	return m.print(result)
}

func (m *menu) shadowAccountEntitlement() error {
	id, err := m.pickAccountLink()
	if err != nil || id == "" {
		return err
	}
	subID, err := m.required("Commerce 訂閱 ID（尚未回填可輸入預期 ID）")
	if err != nil {
		return err
	}
	status, err := m.required("舊系統權益狀態（如 missing、pending、active、grace）")
	if err != nil {
		return err
	}
	result, err := m.lab.ShadowEntitlement(context.Background(), id, subID, status)
	if err != nil {
		return err
	}
	return m.print(result)
}

func (m *menu) provenanceInput() (lab.LegacyProvenance, error) {
	id, err := m.pickAccountLink()
	if err != nil || id == "" {
		return lab.LegacyProvenance{}, err
	}
	legacySub, err := m.required("舊訂閱 ID")
	if err != nil {
		return lab.LegacyProvenance{}, err
	}
	legacyInvoice, err := m.required("舊發票 ID")
	if err != nil {
		return lab.LegacyProvenance{}, err
	}
	commerceSub, err := m.required("Commerce 訂閱 ID")
	if err != nil {
		return lab.LegacyProvenance{}, err
	}
	commerceInvoice, err := m.required("Commerce 發票 ID")
	if err != nil {
		return lab.LegacyProvenance{}, err
	}
	price, err := m.required("來源 PriceVersion ID")
	if err != nil {
		return lab.LegacyProvenance{}, err
	}
	return lab.LegacyProvenance{LegacyAccountID: id, LegacySubscriptionID: legacySub, LegacyInvoiceID: legacyInvoice, CommerceSubscriptionID: commerceSub, CommerceInvoiceID: commerceInvoice, PriceVersionID: price}, nil
}

func (m *menu) backfillAccountProvenance() error {
	input, err := m.provenanceInput()
	if err != nil || input.LegacyInvoiceID == "" {
		return err
	}
	result, err := m.lab.BackfillLegacyProvenance(context.Background(), input)
	if err != nil {
		return err
	}
	return m.print(result)
}

func (m *menu) resolveAccountProvenance() error {
	input, err := m.provenanceInput()
	if err != nil || input.LegacyInvoiceID == "" {
		return err
	}
	reviewer, err := m.required("審查者 ID")
	if err != nil {
		return err
	}
	decision, err := m.required("修正證據與決議")
	if err != nil {
		return err
	}
	result, err := m.lab.ResolveLegacyProvenance(context.Background(), input, reviewer, decision)
	if err != nil {
		return err
	}
	return m.print(result)
}

func (m *menu) showAccountMigrations() error {
	links, err := m.lab.AccountLinks(context.Background())
	if err != nil {
		return err
	}
	var rows []string
	for _, a := range links {
		rows = append(rows, fmt.Sprintf("%s\t%s\t%s\t%s\t%t\t%s", a.LegacyAccountID, a.CustomerID, a.ReadOwner, a.WriterOwner, a.Stopped, a.StopReason))
	}
	table(m.out, "舊帳戶\tCustomer\t讀取者\t寫入者\t已停止\t停止原因", rows)
	for _, a := range links {
		readiness, err := m.lab.MigrationReadiness(context.Background(), a.LegacyAccountID, lab.MigrationThresholds{MaxQuoteP95Millis: 5000, MaxUnknownPayments: 0, MaxOpenDiscrepancies: 0})
		if err != nil {
			return err
		}
		fmt.Fprintf(m.out, "%s：對帳=%t quote=%t 權益=%t 來源=%t quote-p95=%dms 未決付款=%d 差異=%d 可切換=%t\n", a.LegacyAccountID, readiness.Reconciled, readiness.QuoteMatches, readiness.EntitlementMatches, readiness.ProvenanceComplete, readiness.QuoteP95Millis, readiness.UnknownPayments, readiness.OpenDiscrepancies, readiness.Ready)
	}
	return nil
}

func (m *menu) switchAccountRead() error {
	id, err := m.pickAccountLink()
	if err != nil || id == "" {
		return err
	}
	limits, err := m.migrationLimits()
	if err != nil {
		return err
	}
	result, err := m.lab.SwitchAccountRead(context.Background(), id, limits)
	if err != nil {
		return err
	}
	return m.print(result)
}

func (m *menu) switchAccountWriter() error {
	id, err := m.pickAccountLink()
	if err != nil || id == "" {
		return err
	}
	limits, err := m.migrationLimits()
	if err != nil {
		return err
	}
	result, err := m.lab.SwitchAccountWriter(context.Background(), id, limits)
	if err != nil {
		return err
	}
	return m.print(result)
}

func (m *menu) stopAccountMigration() error {
	id, err := m.pickAccountLink()
	if err != nil || id == "" {
		return err
	}
	reason, err := m.required("停止原因與後續處置")
	if err != nil {
		return err
	}
	result, err := m.lab.StopAccountMigration(context.Background(), id, reason)
	if err != nil {
		return err
	}
	return m.print(result)
}

func (m *menu) readAccountEntitlement() error {
	id, err := m.pickAccountLink()
	if err != nil || id == "" {
		return err
	}
	subID, err := m.required("訂閱 ID")
	if err != nil {
		return err
	}
	result, err := m.lab.ReadAccountEntitlement(context.Background(), id, subID)
	if err != nil {
		return err
	}
	return m.print(result)
}
