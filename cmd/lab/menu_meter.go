package main

import (
	"context"
	"errors"
	"strconv"

	"billforge/lab"
)

func (m *menu) registerMeter() error {
	id, err := m.required("meter ID（小寫英數與底線）")
	if err != nil {
		return err
	}
	source, err := m.required("唯一事件來源（tasks 相容來源可用 *）")
	if err != nil {
		return err
	}
	unit, err := m.required("計量單位")
	if err != nil {
		return err
	}
	version, err := m.positiveInt("meter schema 版本")
	if err != nil {
		return err
	}
	spec := lab.MeterSpec{ID: id, Source: source, Unit: unit, SchemaVersion: version}
	if err := m.lab.RegisterMeter(context.Background(), spec); err != nil {
		return err
	}
	return m.print(spec)
}

func (m *menu) publishMeteredPrice() error {
	id, err := m.required("新價格版本 ID（小寫英數與底線）")
	if err != nil {
		return err
	}
	planID, err := m.required("方案 ID（小寫英數與底線）")
	if err != nil {
		return err
	}
	version, err := m.positiveInt("價格版本號")
	if err != nil {
		return err
	}
	fixed, err := m.amount("每期固定費")
	if err != nil {
		return err
	}
	seatRaw, err := m.askDefault("每期每席費 cents（無席次則 0）", "0")
	if err != nil {
		return err
	}
	seat, err := strconv.ParseInt(seatRaw, 10, 64)
	if err != nil || seat < 0 {
		return errors.New("每席費須為非負整數")
	}
	meters, err := m.lab.MeterCatalog(context.Background())
	if err != nil {
		return err
	}
	choices := make([]menuChoice, 0, len(meters))
	for _, meter := range meters {
		choices = append(choices, menuChoice{meter.ID, meter.Unit + " / 來源 " + meter.Source})
	}
	meterID, err := m.pick("計量 meter", choices)
	if err != nil || meterID == "" {
		return err
	}
	included, err := m.positiveInt("每期內含數量")
	if err != nil {
		return err
	}
	num, err := m.positiveInt("超額單價分子（cents）")
	if err != nil {
		return err
	}
	den, err := m.positiveInt("超額單價分母（計量單位）")
	if err != nil {
		return err
	}
	terms, err := m.lab.PublishMeteredPrice(context.Background(), lab.MeteredPriceSpec{
		ID: id, PlanID: planID, Version: version, FixedMinor: fixed, SeatMinor: seat,
		MeterID: meterID, IncludedQuantity: included, UsageRateNum: num, UsageRateDen: den,
		EffectiveFrom: m.now().UTC(),
	})
	if err != nil {
		return err
	}
	return m.print(terms)
}

func (m *menu) meterForSubscription(subID string) (string, error) {
	s, err := m.state()
	if err != nil {
		return "", err
	}
	for _, sub := range s.Subscriptions {
		if sub.ID == subID {
			terms, err := m.lab.PriceTerms(context.Background(), sub.PriceVersionID)
			if err != nil {
				return "", err
			}
			if terms.MeterID == "" {
				return "", errors.New("此訂閱沒有用量 meter")
			}
			return terms.MeterID, nil
		}
	}
	return "", errors.New("找不到訂閱")
}

func (m *menu) createCatalogQuote() error {
	customer,err:=m.required("客戶 ID")
	if err!=nil{return err}
	plan,err:=m.required("方案 ID")
	if err!=nil{return err}
	cohort,err:=m.askDefault("cohort","default")
	if err!=nil{return err}
	seatsRaw,err:=m.askDefault("席次數（無席次則 0）","0")
	if err!=nil{return err}
	seats,err:=strconv.ParseInt(seatsRaw,10,64)
	if err!=nil || seats<0 {return errors.New("席次須為非負整數")}
	q,err:=m.lab.CreateQuoteForCohort(context.Background(),customer,plan,cohort,seats)
	if err!=nil{return err}
	return m.print(q)
}
