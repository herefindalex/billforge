package main

import (
	"context"
	"fmt"
	"time"

	"billforge/lab"
)

func (m *menu) contractMenu() error {
	return m.loop("企業合約與 Net30", []menuOption{
		{"1", "發布客戶合約版本", (*menu).publishContract},
		{"2", "建立合約報價", (*menu).createContractQuote},
		{"3", "接受合約報價並開通服務", (*menu).acceptContractQuote},
		{"4", "將到期 Net30 發票送入收款佇列", (*menu).collectDueContractInvoices},
	}, false)
}

func (m *menu) publishContract() error {
	id, err := m.required("合約版本 ID")
	if err != nil {
		return err
	}
	customer, err := m.required("客戶 ID")
	if err != nil {
		return err
	}
	version, err := m.positiveInt("合約版本號")
	if err != nil {
		return err
	}
	s, err := m.state()
	if err != nil {
		return err
	}
	var choices []menuChoice
	for _, p := range s.Prices {
		if p.PlanID == "pro" && p.State == "published" {
			choices = append(choices, menuChoice{p.ID, fmt.Sprintf("固定 %d cents；每席 %d cents", p.FixedMinor, p.SeatMinor)})
		}
	}
	base, err := m.pick("合約基底價格版本", choices)
	if err != nil || base == "" {
		return err
	}
	fixed, err := m.amount("合約每期固定費")
	if err != nil {
		return err
	}
	seat, err := m.amount("合約每期每席費")
	if err != nil {
		return err
	}
	rawEnd, err := m.required("合約到期時間 RFC3339（需對齊帳期邊界）")
	if err != nil {
		return err
	}
	end, err := time.Parse(time.RFC3339, rawEnd)
	if err != nil {
		return err
	}
	post, err := m.ask("合約到期後明確價格版本 ID（留空表示停止自動續價）")
	if err != nil {
		return err
	}
	result, err := m.lab.PublishContract(context.Background(), lab.ContractSpec{ID: id, CustomerID: customer, Version: version, BasePriceVersionID: base, FixedMinor: fixed, SeatMinor: seat, EffectiveFrom: m.now().UTC(), EffectiveTo: end.UTC(), PostContractPriceVersionID: post})
	if err != nil {
		return err
	}
	return m.print(result)
}

func (m *menu) createContractQuote() error {
	s, err := m.state()
	if err != nil {
		return err
	}
	var choices []menuChoice
	for _, c := range s.Contracts {
		choices = append(choices, menuChoice{c.ID, fmt.Sprintf("%s：固定 %d cents＋每席 %d cents；Net30", c.CustomerID, c.FixedMinor, c.SeatMinor)})
	}
	id, err := m.pick("合約版本", choices)
	if err != nil || id == "" {
		return err
	}
	var customer string
	for _, c := range s.Contracts {
		if c.ID == id {
			customer = c.CustomerID
			break
		}
	}
	seats, err := m.positiveInt("席次數")
	if err != nil {
		return err
	}
	result, err := m.lab.CreateContractQuote(context.Background(), customer, id, seats)
	if err != nil {
		return err
	}
	return m.print(result)
}

func (m *menu) acceptContractQuote() error {
	s, err := m.state()
	if err != nil {
		return err
	}
	var choices []menuChoice
	for _, q := range s.Quotes {
		if !q.Accepted && q.ContractVersionID != "" {
			choices = append(choices, menuChoice{q.ID, fmt.Sprintf("%s / %s / %s", q.CustomerID, q.ContractVersionID, money(q.AmountMinor, q.Currency))})
		}
	}
	id, err := m.pick("未接受合約報價", choices)
	if err != nil || id == "" {
		return err
	}
	key, err := m.requestKey()
	if err != nil {
		return err
	}
	for _, q := range s.Quotes {
		if q.ID == id {
			r, err := m.lab.AcceptContractQuote(context.Background(), id, q.Fingerprint, key)
			if err != nil {
				return err
			}
			return m.print(r)
		}
	}
	return fmt.Errorf("報價不存在")
}

func (m *menu) collectDueContractInvoices() error {
	count, err := m.lab.CollectDueContractInvoices(context.Background())
	if err != nil {
		return err
	}
	return m.print(map[string]int{"queued": count})
}

func (m *menu) showContracts() error {
	s, err := m.state()
	if err != nil {
		return err
	}
	var rows []string
	for _, c := range s.Contracts {
		rows = append(rows, fmt.Sprintf("%s\t%s\t%s\t%d\t%d\t%s\t%s", c.ID, c.CustomerID, c.BasePriceVersionID, c.FixedMinor, c.SeatMinor, c.EffectiveTo.Format(time.RFC3339), c.PostContractPriceVersionID))
	}
	table(m.out, "合約版本\t客戶\t基底價\t固定 cents\t每席 cents\t到期\t後續價", rows)
	rows = nil
	for _, x := range s.ContractSubscriptions {
		rows = append(rows, fmt.Sprintf("%s\t%s\t%v", x.SubscriptionID, x.ContractVersionID, x.PostTransitioned))
	}
	table(m.out, "訂閱\t合約版本\t已轉後續價", rows)
	return nil
}
