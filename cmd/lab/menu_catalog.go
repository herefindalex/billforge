package main

import (
	"context"
	"fmt"
	"strings"

	"billforge/lab"
)

func (m *menu) catalogMenu() error {
	return m.loop("價格版本與 cohort 遷移", []menuOption{
		{"1", "發布 Pro 價格版本", (*menu).publishProPrice},
		{"2", "指定 cohort 新購價格", (*menu).selectCatalogPrice},
		{"3", "預覽既有訂閱價格遷移", (*menu).previewPriceMigration},
		{"4", "建立價格遷移批次", (*menu).planPriceMigration},
		{"5", "暫停價格遷移批次", (*menu).pausePriceMigration},
		{"6", "略過衝突客戶", (*menu).skipPriceMigrationItem},
		{"7", "恢復已檢查的批次", (*menu).resumePriceMigration},
		{"8", "註冊計量 meter", (*menu).registerMeter},
		{"9", "發布配置式計量價格", (*menu).publishMeteredPrice},
	}, false)
}

func (m *menu) publishProPrice() error {
	id, err := m.required("新價格版本 ID")
	if err != nil {
		return err
	}
	version, err := m.positiveInt("版本號")
	if err != nil {
		return err
	}
	fixed, err := m.amount("每期固定費")
	if err != nil {
		return err
	}
	seat, err := m.amount("每期每席費")
	if err != nil {
		return err
	}
	included, err := m.positiveInt("每期內含 tasks 數")
	if err != nil {
		return err
	}
	num, err := m.positiveInt("超額單價分子（cents）")
	if err != nil {
		return err
	}
	den, err := m.positiveInt("超額單價分母（tasks）")
	if err != nil {
		return err
	}
	returnValue, err := m.lab.PublishProPrice(context.Background(), lab.ProPriceSpec{ID: id, Version: version, FixedMinor: fixed, SeatMinor: seat, IncludedTasks: included, UsageRateNum: num, UsageRateDen: den, EffectiveFrom: m.now().UTC()})
	if err != nil {
		return err
	}
	return m.print(returnValue)
}

func (m *menu) selectCatalogPrice() error {
	s, err := m.state()
	if err != nil {
		return err
	}
	var choices []menuChoice
	for _, p := range s.Prices {
		if p.State == "published" {
			choices = append(choices, menuChoice{p.ID, fmt.Sprintf("方案 %s；固定 %d cents；每席 %d cents", p.PlanID, p.FixedMinor, p.SeatMinor)})
		}
	}
	id, err := m.pick("已發布價格版本", choices)
	if err != nil || id == "" {
		return err
	}
	cohort, err := m.required("cohort 名稱")
	if err != nil {
		return err
	}
	planID := ""
	for _, p := range s.Prices {
		if p.ID == id {
			planID = p.PlanID
			break
		}
	}
	if planID == "" {
		return lab.ErrConflict
	}
	if err := m.lab.SelectCatalogPrice(context.Background(), planID, cohort, m.now().UTC(), id); err != nil {
		return err
	}
	return m.print(map[string]any{"plan_id": planID, "cohort": cohort, "price_version_id": id, "effective_at": m.now().UTC()})
}

func (m *menu) migrationInput() (string, []string, error) {
	s, err := m.state()
	if err != nil {
		return "", nil, err
	}
	var choices []menuChoice
	for _, p := range s.Prices {
		if p.PlanID == "pro" && p.State == "published" {
			choices = append(choices, menuChoice{p.ID, fmt.Sprintf("固定 %d cents；每席 %d cents", p.FixedMinor, p.SeatMinor)})
		}
	}
	target, err := m.pick("目標價格版本", choices)
	if err != nil || target == "" {
		return "", nil, err
	}
	for _, sub := range s.Subscriptions {
		if sub.Status == "active" {
			fmt.Fprintf(m.out, "訂閱 %s：%s；目前 %s；revision %d\n", sub.ID, sub.CustomerID, sub.PriceVersionID, sub.Revision)
		}
	}
	raw, err := m.required("要遷移的訂閱 ID（多筆以逗號分隔）")
	if err != nil {
		return "", nil, err
	}
	var ids []string
	for _, v := range strings.Split(raw, ",") {
		if id := strings.TrimSpace(v); id != "" {
			ids = append(ids, id)
		}
	}
	return target, ids, nil
}

func (m *menu) previewPriceMigration() error {
	target, ids, err := m.migrationInput()
	if err != nil || target == "" {
		return err
	}
	preview, err := m.lab.PreviewPriceMigration(context.Background(), target, ids)
	if err != nil {
		return err
	}
	return m.print(preview)
}

func (m *menu) planPriceMigration() error {
	target, ids, err := m.migrationInput()
	if err != nil || target == "" {
		return err
	}
	preview, err := m.lab.PreviewPriceMigration(context.Background(), target, ids)
	if err != nil {
		return err
	}
	if err := m.print(preview); err != nil {
		return err
	}
	cohort, err := m.required("已選用目標價的 cohort 名稱")
	if err != nil {
		return err
	}
	id, err := m.required("遷移批次 ID（重試須重用）")
	if err != nil {
		return err
	}
	result, err := m.lab.PlanPriceMigration(context.Background(), id, cohort, target, ids)
	if err != nil {
		return err
	}
	return m.print(result)
}

func (m *menu) pickMigration(status string) (string, error) {
	s, err := m.state()
	if err != nil {
		return "", err
	}
	var choices []menuChoice
	for _, x := range s.PriceMigrations {
		if status == "" || x.Status == status {
			choices = append(choices, menuChoice{x.ID, fmt.Sprintf("cohort %s → %s；%s；%d 戶", x.Cohort, x.TargetPriceVersionID, x.Status, len(x.Items))})
		}
	}
	return m.pick("價格遷移批次", choices)
}

func (m *menu) pausePriceMigration() error {
	id, err := m.pickMigration("active")
	if err != nil || id == "" {
		return err
	}
	return m.lab.PausePriceMigration(context.Background(), id)
}

func (m *menu) skipPriceMigrationItem() error {
	id, err := m.pickMigration("paused")
	if err != nil || id == "" {
		return err
	}
	x, err := m.lab.PriceMigration(context.Background(), id)
	if err != nil {
		return err
	}
	var choices []menuChoice
	for _, item := range x.Items {
		if item.Status == "pending" || item.Status == "conflicted" {
			choices = append(choices, menuChoice{item.SubscriptionID, item.Status})
		}
	}
	subID, err := m.pick("略過客戶", choices)
	if err != nil || subID == "" {
		return err
	}
	return m.lab.SkipPriceMigrationItem(context.Background(), id, subID)
}

func (m *menu) resumePriceMigration() error {
	id, err := m.pickMigration("paused")
	if err != nil || id == "" {
		return err
	}
	return m.lab.ResumePriceMigration(context.Background(), id)
}

func (m *menu) showPriceCatalog() error {
	s, err := m.state()
	if err != nil {
		return err
	}
	meters, err := m.lab.MeterCatalog(context.Background())
	if err != nil {
		return err
	}
	var meterRows []string
	for _, meter := range meters {
		meterRows = append(meterRows, fmt.Sprintf("%s\t%s\t%s\t%d", meter.ID, meter.Source, meter.Unit, meter.SchemaVersion))
	}
	table(m.out, "meter\t事件來源\t單位\tschema 版本", meterRows)
	var rows []string
	for _, p := range s.Prices {
		terms, err := m.lab.PriceTerms(context.Background(), p.ID)
		if err != nil {
			return err
		}
		rows = append(rows, fmt.Sprintf("%s\t%s\t%d\t%s\t%d\t%d\t%s\t%d\t%d/%d\t%s", p.ID, p.PlanID, p.Version, p.Currency, p.FixedMinor, p.SeatMinor, terms.MeterID, terms.IncludedQuantity, terms.UsageRateNum, terms.UsageRateDen, p.State))
	}
	table(m.out, "版本 ID\t方案\t版本\t幣別\t固定 cents\t每席 cents\tmeter\t內含量\t超額 cents/單位\t狀態", rows)
	rows = nil
	for _, x := range s.CatalogSelections {
		rows = append(rows, fmt.Sprintf("%s\t%s\t%s\t%s", x.PlanID, x.Cohort, x.PriceVersionID, x.EffectiveAt.Format("2006-01-02 15:04Z")))
	}
	table(m.out, "方案\tcohort\t價格版本\t生效時間", rows)
	return nil
}

func (m *menu) showPriceMigrations() error {
	s, err := m.state()
	if err != nil {
		return err
	}
	var rows []string
	for _, x := range s.PriceMigrations {
		for _, item := range x.Items {
			rows = append(rows, fmt.Sprintf("%s\t%s\t%s\t%s\t%d\t%d\t%s", x.ID, x.Status, item.SubscriptionID, item.FromPriceVersionID, item.PriorAmountMinor, item.TargetAmountMinor, item.Status))
		}
	}
	table(m.out, "批次\t批次狀態\t訂閱\t原價格\t原價 cents\t目標 cents\t戶狀態", rows)
	return nil
}
