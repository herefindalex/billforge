package main

import (
	"context"
	"fmt"
	"io"
	"text/tabwriter"

	"billforge/lab"
)

func (m *menu) stateMenu() error {
	return m.loop("狀態查詢", []menuOption{
		{"1", "總覽：報價、訂閱、帳期、帳單、付款、credit、退款", (*menu).showAll},
		{"2", "查訂閱與帳期", (*menu).showSubscription},
		{"3", "查發票餘額與更正", (*menu).showInvoice},
		{"4", "查 credit 來源與餘額", (*menu).showCredit},
		{"5", "查退款", (*menu).showRefund},
		{"6", "查 fake provider 狀態", (*menu).showProvider},
		{"7", "查期中方案變更與補差額", (*menu).showImmediateChanges},
		{"8", "查價格版本與 cohort", (*menu).showPriceCatalog},
		{"9", "查價格遷移批次", (*menu).showPriceMigrations},
		{"10", "查用量事件、評價與更正", (*menu).showUsage},
		{"11", "查合約與企業訂閱", (*menu).showContracts},
		{"12", "查對帳差異", (*menu).showDiscrepancies},
		{"13", "查帳戶遷移狀態", (*menu).showAccountMigrations},
	}, false)
}

func (m *menu) showImmediateChanges() error {
	s, err := m.state()
	if err != nil {
		return err
	}
	var rows []string
	for _, c := range s.ImmediateChanges {
		rows = append(rows, fmt.Sprintf("%s\t%s\t%s\t%d\t%d\t%d\t%s\t%v", c.ID, c.SubscriptionID, c.TargetPriceVersionID, c.SeatQuantity, c.QuotedAmountMinor, c.CorrectionMinor, c.Status, c.Resolved))
	}
	table(m.out, "變更 ID\t訂閱\t目標版本\t席次\t補差額 cents\t更正 cents\t狀態\t已處理", rows)
	return nil
}

func table(out io.Writer, header string, rows []string) {
	fmt.Fprintln(out)
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, header)
	for _, row := range rows {
		fmt.Fprintln(w, row)
	}
	if len(rows) == 0 {
		fmt.Fprintln(w, "（無資料）")
	}
	w.Flush()
}

func money(amount int64, currency string) string {
	return fmt.Sprintf("%s %d.%02d (%d)", currency, amount/100, amount%100, amount)
}

func (m *menu) showAll() error {
	s, err := m.state()
	if err != nil {
		return err
	}
	fmt.Fprintf(m.out, "目前記錄：報價 %d、訂閱 %d、變更排程 %d、發票 %d、付款操作 %d、更正 %d、credit %d、credit 應用 %d、退款 %d、待處理 outbox %d。\n", len(s.Quotes), len(s.Subscriptions), len(s.Schedules), len(s.Invoices), len(s.Payments), len(s.Corrections), len(s.Credits), len(s.CreditApplications), len(s.Refunds), len(s.PendingOutbox))
	var rows []string
	for _, q := range s.Quotes {
		status := "未接受"
		if q.Accepted {
			status = "已接受"
		}
		rows = append(rows, fmt.Sprintf("%s\t%s\t%s\t%s\t%s\t%s", q.ID, q.CustomerID, q.PriceVersionID, money(q.AmountMinor, q.Currency), q.ExpiresAt.Format("2006-01-02 15:04Z"), status))
	}
	table(m.out, "報價 ID\t客戶\t價格版本\t金額\t到期\t狀態", rows)
	rows = nil
	for _, x := range s.Subscriptions {
		rows = append(rows, fmt.Sprintf("%s\t%s\t%s\t%d\t%d\t%s\t%s", x.ID, x.CustomerID, x.PriceVersionID, x.SeatQuantity, x.Revision, x.Status, x.EntitlementStatus))
	}
	table(m.out, "訂閱 ID\t客戶\t價格版本\t席次\tRevision\t訂閱\t權益", rows)
	rows = nil
	for _, x := range s.Schedules {
		rows = append(rows, fmt.Sprintf("%s\t%s\t%s\t%s\t%d\t%s\t%s", x.ID, x.SubscriptionID, x.Kind, x.TargetPriceVersionID, x.SeatQuantity, x.EffectiveAt.Format("2006-01-02 15:04Z"), x.Status))
	}
	table(m.out, "排程 ID\t訂閱 ID\t種類\t目標價格版本\t席次\t生效時刻\t狀態", rows)
	rows = nil
	for _, x := range s.Assignments {
		end := "—"
		if x.EffectiveEnd != nil {
			end = x.EffectiveEnd.Format("2006-01-02 15:04Z")
		}
		rows = append(rows, fmt.Sprintf("%s\t%d\t%s\t%d\t%s\t%s", x.SubscriptionID, x.Index, x.PriceVersionID, x.SeatQuantity, x.EffectiveStart.Format("2006-01-02 15:04Z"), end))
	}
	table(m.out, "訂閱 ID\tAssignment\t價格版本\t席次\t開始\t結束", rows)
	rows = nil
	for _, x := range s.Periods {
		rows = append(rows, fmt.Sprintf("%s\t%d\t%s\t%s\t%s\t%s", x.SubscriptionID, x.Index, x.Start.Format("2006-01-02"), x.End.Format("2006-01-02"), x.DueAt.Format("2006-01-02"), x.InvoiceID))
	}
	table(m.out, "訂閱 ID\t期數\t開始\t結束\t到期\t發票 ID", rows)
	rows = nil
	for _, x := range s.Invoices {
		b := x.Balance
		rows = append(rows, fmt.Sprintf("%s\t%s\t%d\t%s\t%s\t%s", x.ID, x.SubscriptionID, x.PeriodIndex, money(b.ObligationMinor, b.Currency), money(b.NetAppliedMinor, b.Currency), money(b.OutstandingMinor, b.Currency)))
	}
	table(m.out, "發票 ID\t訂閱 ID\t期數\t應收\t淨已付\t未付", rows)
	rows = nil
	for _, x := range s.Payments {
		rows = append(rows, fmt.Sprintf("%s\t%s\t%s\t%s\t%s", x.ID, x.InvoiceID, money(x.AmountMinor, x.Currency), x.Status, x.RequestKey))
	}
	table(m.out, "付款 ID\t發票 ID\t金額\t狀態\t請求鍵", rows)
	rows = nil
	for _, x := range s.Corrections {
		rows = append(rows, fmt.Sprintf("%s\t%s\t%d\t%d\t%d\t%s", x.ID, x.InvoiceID, x.PriorObligationMinor, x.ReductionMinor, x.NewObligationMinor, x.RequestKey))
	}
	table(m.out, "更正 ID\t發票 ID\t原應收 minor\t減額 minor\t新應收 minor\t請求鍵", rows)
	rows = nil
	for _, x := range s.Credits {
		b := x.Balance
		rows = append(rows, fmt.Sprintf("%s\t%s\t%s\t%s\t%s\t%s", x.ID, x.SourceInvoiceID, money(b.GrantedMinor, b.Currency), money(b.AppliedMinor, b.Currency), money(b.ReservedMinor+b.RefundedMinor, b.Currency), money(b.AvailableMinor, b.Currency)))
	}
	table(m.out, "Credit ID\t來源發票\t核發\t已應用\t退款中／已退款\t可用", rows)
	rows = nil
	for _, x := range s.CreditApplications {
		rows = append(rows, fmt.Sprintf("%s\t%s\t%s\t%d\t%s", x.ID, x.GrantID, x.InvoiceID, x.AmountMinor, x.RequestKey))
	}
	table(m.out, "Credit 應用 ID\tCredit ID\t目標發票\t金額 minor\t請求鍵", rows)
	rows = nil
	for _, x := range s.Refunds {
		rows = append(rows, fmt.Sprintf("%s\t%s\t%s\t%s", x.ID, x.GrantID, money(x.AmountMinor, x.Currency), x.Status))
	}
	table(m.out, "退款 ID\tCredit ID\t金額\t狀態", rows)
	rows = nil
	for _, x := range s.PendingOutbox {
		rows = append(rows, fmt.Sprintf("%s\t%s\t%s", x.ID, x.Kind, x.ObjectID))
	}
	table(m.out, "待處理 outbox ID\t類型\t物件 ID", rows)
	return nil
}

func (m *menu) showSubscription() error {
	s, err := m.state()
	if err != nil {
		return err
	}
	choices := make([]menuChoice, 0, len(s.Subscriptions))
	for _, x := range s.Subscriptions {
		choices = append(choices, menuChoice{x.ID, x.CustomerID + " / " + x.Status})
	}
	id, err := m.pick("訂閱", choices)
	if err != nil || id == "" {
		return err
	}
	for _, x := range s.Subscriptions {
		if x.ID == id {
			if err := m.print(x); err != nil {
				return err
			}
		}
	}
	for _, x := range s.Periods {
		if x.SubscriptionID == id {
			if err := m.print(x); err != nil {
				return err
			}
		}
	}
	for _, x := range s.Schedules {
		if x.SubscriptionID == id {
			if err := m.print(x); err != nil {
				return err
			}
		}
	}
	for _, x := range s.Assignments {
		if x.SubscriptionID == id {
			if err := m.print(x); err != nil {
				return err
			}
		}
	}
	return nil
}

func invoiceChoices(s lab.State, requireOutstanding bool) []menuChoice {
	var choices []menuChoice
	for _, x := range s.Invoices {
		if requireOutstanding && x.Balance.OutstandingMinor <= 0 {
			continue
		}
		choices = append(choices, menuChoice{x.ID, fmt.Sprintf("期 %d / 未付 %s", x.PeriodIndex, money(x.Balance.OutstandingMinor, x.Balance.Currency))})
	}
	return choices
}

func creditChoices(s lab.State, requireAvailable bool) []menuChoice {
	var choices []menuChoice
	for _, x := range s.Credits {
		if requireAvailable && x.Balance.AvailableMinor <= 0 {
			continue
		}
		choices = append(choices, menuChoice{x.ID, "可用 " + money(x.Balance.AvailableMinor, x.Balance.Currency)})
	}
	return choices
}

func paymentChoices(s lab.State, statuses ...string) []menuChoice {
	var choices []menuChoice
	for _, x := range s.Payments {
		if len(statuses) != 0 {
			matched := false
			for _, status := range statuses {
				if x.Status == status {
					matched = true
				}
			}
			if !matched {
				continue
			}
		}
		choices = append(choices, menuChoice{x.ID, x.Status + " / " + money(x.AmountMinor, x.Currency)})
	}
	return choices
}

func refundChoices(s lab.State, statuses ...string) []menuChoice {
	var choices []menuChoice
	for _, x := range s.Refunds {
		if len(statuses) != 0 {
			matched := false
			for _, status := range statuses {
				if x.Status == status {
					matched = true
				}
			}
			if !matched {
				continue
			}
		}
		choices = append(choices, menuChoice{x.ID, x.Status + " / " + money(x.AmountMinor, x.Currency)})
	}
	return choices
}

func (m *menu) showInvoice() error {
	s, err := m.state()
	if err != nil {
		return err
	}
	id, err := m.pick("發票", invoiceChoices(s, false))
	if err != nil || id == "" {
		return err
	}
	for _, x := range s.Invoices {
		if x.ID == id {
			if err := m.print(x); err != nil {
				return err
			}
		}
	}
	for _, x := range s.Corrections {
		if x.InvoiceID == id {
			if err := m.print(x); err != nil {
				return err
			}
		}
	}
	for _, x := range s.Payments {
		if x.InvoiceID == id {
			if err := m.print(x); err != nil {
				return err
			}
		}
	}
	for _, x := range s.CreditApplications {
		if x.InvoiceID == id {
			if err := m.print(x); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *menu) showCredit() error {
	s, err := m.state()
	if err != nil {
		return err
	}
	id, err := m.pick("Credit", creditChoices(s, false))
	if err != nil || id == "" {
		return err
	}
	for _, x := range s.Credits {
		if x.ID == id {
			if err := m.print(x); err != nil {
				return err
			}
		}
	}
	for _, x := range s.Refunds {
		if x.GrantID == id {
			if err := m.print(x); err != nil {
				return err
			}
		}
	}
	for _, x := range s.CreditApplications {
		if x.GrantID == id {
			if err := m.print(x); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *menu) showRefund() error {
	s, err := m.state()
	if err != nil {
		return err
	}
	id, err := m.pick("退款", refundChoices(s))
	if err != nil || id == "" {
		return err
	}
	for _, x := range s.Refunds {
		if x.ID == id {
			return m.print(x)
		}
	}
	return nil
}

func (m *menu) showProvider() error {
	s, err := m.lab.ProviderState(context.Background())
	if err != nil {
		return err
	}
	fmt.Fprintln(m.out, "Fake provider 使用獨立 SQLite；以下狀態與 Commerce 總覽不是同一筆交易的快照。")
	return m.print(s)
}
