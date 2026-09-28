package main

import (
	"context"
	"fmt"
	"strconv"
	"time"
)

func (m *menu) usageMenu() error {
	return m.loop("用量與關帳", []menuOption{
		{"1", "記錄訂閱 meter 用量事件", (*menu).recordUsage},
		{"2", "記錄既有事件的用量撤銷", (*menu).recordUsageAdjustment},
		{"3", "關閉一個帳期的用量", (*menu).closeUsagePeriod},
		{"4", "重算晚到事件", (*menu).rerateUsagePeriod},
		{"5", "過帳負差額 CreditNote", (*menu).runUsageCreditNotes},
	}, false)
}

func (m *menu) pickActiveSubscription() (string, error) {
	s, err := m.state()
	if err != nil {
		return "", err
	}
	return m.pick("有效訂閱", activeSubscriptionChoices(s))
}

func (m *menu) recordUsage() error {
	subID, err := m.pickActiveSubscription()
	if err != nil || subID == "" {
		return err
	}
	meterID, err := m.meterForSubscription(subID)
	if err != nil {
		return err
	}
	source, err := m.required("事件來源")
	if err != nil {
		return err
	}
	eventID, err := m.required("事件 ID（重送須重用）")
	if err != nil {
		return err
	}
	quantity, err := m.positiveInt(meterID + " 數量")
	if err != nil {
		return err
	}
	raw, err := m.askDefault("事件發生時間 RFC3339", m.now().UTC().Format(time.RFC3339))
	if err != nil {
		return err
	}
	at, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return err
	}
	result, err := m.lab.RecordUsage(context.Background(), source, eventID, subID, meterID, quantity, at)
	if err != nil {
		return err
	}
	return m.print(result)
}

func (m *menu) recordUsageAdjustment() error {
	subID, err := m.pickActiveSubscription()
	if err != nil || subID == "" {
		return err
	}
	source, err := m.required("更正來源")
	if err != nil {
		return err
	}
	eventID, err := m.required("更正事件 ID")
	if err != nil {
		return err
	}
	originalSource, err := m.required("原事件來源")
	if err != nil {
		return err
	}
	originalID, err := m.required("原事件 ID")
	if err != nil {
		return err
	}
	quantity, err := m.positiveInt("撤銷 tasks 數量")
	if err != nil {
		return err
	}
	result, err := m.lab.RecordUsageAdjustment(context.Background(), source, eventID, subID, originalSource, originalID, quantity)
	if err != nil {
		return err
	}
	return m.print(result)
}

func (m *menu) usagePeriodInput() (string, int, error) {
	subID, err := m.pickActiveSubscription()
	if err != nil || subID == "" {
		return "", 0, err
	}
	s, err := m.state()
	if err != nil {
		return "", 0, err
	}
	for _, p := range s.Periods {
		if p.SubscriptionID == subID {
			fmt.Fprintf(m.out, "帳期 %d: %s 至 %s\n", p.Index, p.Start.Format(time.RFC3339), p.End.Format(time.RFC3339))
		}
	}
	raw, err := m.required("帳期 index（首期為 0）")
	if err != nil {
		return "", 0, err
	}
	index, err := strconv.Atoi(raw)
	if err != nil || index < 0 {
		return "", 0, fmt.Errorf("請輸入非負整數帳期")
	}
	return subID, index, nil
}

func (m *menu) closeUsagePeriod() error {
	subID, index, err := m.usagePeriodInput()
	if err != nil || subID == "" {
		return err
	}
	rating, err := m.lab.CloseUsagePeriod(context.Background(), subID, index, m.now().UTC())
	if err != nil {
		return err
	}
	return m.print(rating)
}

func (m *menu) rerateUsagePeriod() error {
	subID, index, err := m.usagePeriodInput()
	if err != nil || subID == "" {
		return err
	}
	rating, err := m.lab.RerateUsagePeriod(context.Background(), subID, index)
	if err != nil {
		return err
	}
	return m.print(rating)
}

func (m *menu) runUsageCreditNotes() error {
	result, err := m.lab.RunUsageCreditNotes(context.Background())
	if err != nil {
		return err
	}
	return m.print(result)
}

func (m *menu) showUsage() error {
	s, err := m.state()
	if err != nil {
		return err
	}
	var rows []string
	for _, e := range s.UsageEvents {
		rows = append(rows, fmt.Sprintf("%s/%s\t%s\t%d\t%d\t%s\t%s", e.Source, e.EventID, e.SubscriptionID, e.PeriodIndex, e.Quantity, e.EventAt.Format(time.RFC3339), e.PriceVersionID))
	}
	table(m.out, "事件\t訂閱\t帳期\t數量\t事件時間\t價格版本", rows)
	rows = nil
	for _, p := range s.UsagePeriods {
		rows = append(rows, fmt.Sprintf("%s\t%d\t%s\t%d\t%d\t%d", p.SubscriptionID, p.PeriodIndex, p.PriceVersionID, p.RatedMinor, p.BilledMinor, p.CreditedMinor))
	}
	table(m.out, "訂閱\t原帳期\t價格版本\t累計評價 cents\t已開票 cents\t已更正 cents", rows)
	rows = nil
	for _, r := range s.UsageRatings {
		rows = append(rows, fmt.Sprintf("%s\t%d\t%d\t%d\t%d/%d\t%d\t%+d", r.SubscriptionID, r.PeriodIndex, r.Revision, r.Quantity, r.ExactMinorNumerator, r.ExactMinorDenominator, r.RoundedMinor, r.DeltaMinor))
	}
	table(m.out, "訂閱\t帳期\t評價版\t原量\t精確 cents\t捨入 cents\t差額 cents", rows)
	rows = nil
	for _, n := range s.UsageCreditNotes {
		rows = append(rows, fmt.Sprintf("%s\t%d\t%s\t%d\t%s", n.SubscriptionID, n.PeriodIndex, n.SourceInvoiceID, n.AmountMinor, n.CorrectionID))
	}
	table(m.out, "訂閱\t原帳期\t原發票\t更正 cents\tCorrection", rows)
	return nil
}
