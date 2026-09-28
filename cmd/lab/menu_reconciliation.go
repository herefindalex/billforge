package main

import (
	"context"
	"fmt"
)

func (m *menu) reconciliationMenu() error {
	return m.loop("對帳、修復與人工決議", []menuOption{
		{"1", "執行本地與 fake provider 對帳", (*menu).runReconciliation},
		{"2", "查對帳差異", (*menu).showDiscrepancies},
		{"3", "執行指定差異的安全修復或原 key 查詢", (*menu).repairDiscrepancy},
		{"4", "記錄高風險差異的人工決議", (*menu).recordManualDecision},
	}, false)
}

func (m *menu) runReconciliation() error {
	r, err := m.lab.RunReconciliation(context.Background(), m.now().UTC())
	if err != nil {
		return err
	}
	fmt.Fprintf(m.out, "對帳 %s：本地觀測 %s；provider 觀測 %s；差異 %d 項\n", r.ID, r.LocalObservedAt.Format("2006-01-02 15:04:05Z07:00"), r.ProviderObservedAt.Format("2006-01-02 15:04:05Z07:00"), len(r.Findings))
	return m.showDiscrepancies()
}

func (m *menu) showDiscrepancies() error {
	findings, err := m.lab.Discrepancies(context.Background())
	if err != nil {
		return err
	}
	rows := make([]string, 0, len(findings))
	for _, d := range findings {
		rows = append(rows, fmt.Sprintf("%s\t%s\t%s\t%s\t%s\t%s\t%s\t%d", d.ID, d.Kind, d.ObjectID, d.Classification, d.Status, d.Expected, d.Actual, d.SourceRevision))
	}
	table(m.out, "差異 ID\t種類\t物件\t分類\t狀態\t預期\t實際\t來源 revision", rows)
	return nil
}

func (m *menu) pickDiscrepancy() (string, error) {
	findings, err := m.lab.Discrepancies(context.Background())
	if err != nil {
		return "", err
	}
	choices := make([]menuChoice, 0, len(findings))
	for _, d := range findings {
		if d.Status != "resolved" {
			choices = append(choices, menuChoice{d.ID, fmt.Sprintf("%s / %s / %s", d.Kind, d.ObjectID, d.Classification)})
		}
	}
	return m.pick("未解差異", choices)
}

func (m *menu) repairDiscrepancy() error {
	id, err := m.pickDiscrepancy()
	if err != nil || id == "" {
		return err
	}
	key, err := m.requestKey()
	if err != nil {
		return err
	}
	op, err := m.lab.RepairDiscrepancy(context.Background(), id, key)
	if err != nil {
		return err
	}
	m.print(op)
	return nil
}

func (m *menu) recordManualDecision() error {
	id, err := m.pickDiscrepancy()
	if err != nil || id == "" {
		return err
	}
	reviewer, err := m.required("審查者 ID")
	if err != nil {
		return err
	}
	decision, err := m.required("決議與證據說明")
	if err != nil {
		return err
	}
	decisionID, err := m.lab.RecordManualDecision(context.Background(), id, reviewer, decision)
	if err != nil {
		return err
	}
	fmt.Fprintf(m.out, "已記錄人工決議 %s\n", decisionID)
	return nil
}
