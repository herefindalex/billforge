package main

import (
	"context"
	"errors"
	"fmt"

	"billforge/lab"
)

func (m *menu) purchaseMenu() error {
	return m.loop("報價與新購", []menuOption{
		{"1", "建立 Basic 報價", (*menu).createQuote},
		{"2", "接受既有報價", (*menu).acceptQuote},
		{"3", "建立 Pro 席次報價", (*menu).createProQuote},
		{"4", "依已發布方案與 cohort 建立報價", (*menu).createCatalogQuote},
	}, false)
}

func (m *menu) paymentMenu() error {
	return m.loop("付款", []menuOption{
		{"1", "建立部分／剩餘金額付款", (*menu).createPayment},
		{"2", "重試確定失敗的付款", (*menu).retryPayment},
		{"3", "送出下一筆付款", (*menu).dispatchPayment},
		{"4", "依原操作查證付款", (*menu).reconcilePayment},
		{"5", "設定下一筆 fake provider 付款結果", (*menu).setPaymentDecision},
	}, false)
}

func (m *menu) renewalMenu() error {
	return m.loop("續約與權益", []menuOption{
		{"1", "執行到期續約", (*menu).runRenewals},
		{"2", "重建目前權益投影", (*menu).refreshEntitlements},
	}, false)
}

func (m *menu) correctionMenu() error {
	return m.loop("發票更正與 credit", []menuOption{
		{"1", "對發票過帳減額更正", (*menu).postReduction},
		{"2", "將 credit 用於後續續約發票", (*menu).applyCredit},
		{"3", "處理未提供升級服務的補差額", (*menu).resolveUnfulfilledChange},
		{"4", "執行延遲開通減額更正", (*menu).runChangeCorrections},
	}, false)
}

func (m *menu) refundMenu() error {
	return m.loop("退款", []menuOption{
		{"1", "保留 credit 退款額度", (*menu).reserveRefund},
		{"2", "送出下一筆退款", (*menu).dispatchRefund},
		{"3", "依原操作查證退款", (*menu).reconcileRefund},
		{"4", "設定下一筆 fake provider 退款結果", (*menu).setRefundDecision},
	}, false)
}

func (m *menu) subscriptionMenu() error {
	return m.loop("訂閱排程與取消", []menuOption{
		{"1", "安排下期 Basic／Pro 方案", (*menu).scheduleNextPlan},
		{"2", "安排期末取消", (*menu).scheduleCancel},
		{"3", "取消前恢復訂閱", (*menu).resumeCancel},
		{"4", "期中升級 Pro 五席或更多", (*menu).requestImmediatePro},
	}, false)
}

func (m *menu) requestImmediatePro() error {
	s, err := m.state()
	if err != nil {
		return err
	}
	id, err := m.pick("有效 Basic 訂閱", activeSubscriptionChoices(s))
	if err != nil || id == "" {
		return err
	}
	seats, err := m.positiveInt("Pro 付費席次數")
	if err != nil {
		return err
	}
	key, err := m.requestKey()
	if err != nil {
		return err
	}
	result, err := m.lab.RequestImmediateProUpgrade(context.Background(), id, seats, subscriptionRevision(s, id), key)
	if err != nil {
		return err
	}
	return m.print(result)
}

func (m *menu) runChangeCorrections() error {
	results, err := m.lab.RunChangeCorrections(context.Background())
	if err != nil {
		return err
	}
	return m.print(results)
}

func (m *menu) resolveUnfulfilledChange() error {
	s, err := m.state()
	if err != nil {
		return err
	}
	var choices []menuChoice
	for _, c := range s.ImmediateChanges {
		if c.Status == "needs_review" {
			choices = append(choices, menuChoice{c.ID, fmt.Sprintf("%s 補差額 %d cents 已處理=%v", c.SubscriptionID, c.QuotedAmountMinor, c.Resolved)})
		}
	}
	id, err := m.pick("未提供升級服務的變更", choices)
	if err != nil || id == "" {
		return err
	}
	result, err := m.lab.ResolveUnfulfilledImmediateChange(context.Background(), id)
	if err != nil {
		return err
	}
	return m.print(result)
}

func activeSubscriptionChoices(s lab.State) []menuChoice {
	var choices []menuChoice
	for _, x := range s.Subscriptions {
		if x.Status == "active" {
			choices = append(choices, menuChoice{x.ID, fmt.Sprintf("%s / %s / revision %d", x.CustomerID, x.PriceVersionID, x.Revision)})
		}
	}
	return choices
}

func subscriptionRevision(s lab.State, id string) int64 {
	for _, x := range s.Subscriptions {
		if x.ID == id {
			return x.Revision
		}
	}
	return 0
}

func (m *menu) scheduleNextPlan() error {
	s, err := m.state()
	if err != nil {
		return err
	}
	id, err := m.pick("有效訂閱", activeSubscriptionChoices(s))
	if err != nil || id == "" {
		return err
	}
	plan, err := m.required("目標方案 [basic/pro]")
	if err != nil {
		return err
	}
	var seats int64
	if plan == "pro" {
		seats, err = m.positiveInt("下期付費席次數")
		if err != nil {
			return err
		}
	} else if plan != "basic" {
		return errors.New("目前只支援 basic 或 pro")
	}
	key, err := m.requestKey()
	if err != nil {
		return err
	}
	result, err := m.lab.ScheduleNextPlan(context.Background(), id, plan, seats, subscriptionRevision(s, id), key)
	if err != nil {
		return err
	}
	return m.print(result)
}

func (m *menu) scheduleCancel() error {
	s, err := m.state()
	if err != nil {
		return err
	}
	id, err := m.pick("有效訂閱", activeSubscriptionChoices(s))
	if err != nil || id == "" {
		return err
	}
	key, err := m.requestKey()
	if err != nil {
		return err
	}
	result, err := m.lab.ScheduleCancel(context.Background(), id, subscriptionRevision(s, id), key)
	if err != nil {
		return err
	}
	return m.print(result)
}

func (m *menu) resumeCancel() error {
	s, err := m.state()
	if err != nil {
		return err
	}
	var choices []menuChoice
	for _, x := range s.Schedules {
		if x.Kind == "cancel" && x.Status == "scheduled" {
			choices = append(choices, menuChoice{x.SubscriptionID, "取消生效 " + x.EffectiveAt.Format("2006-01-02 15:04Z")})
		}
	}
	id, err := m.pick("待取消訂閱", choices)
	if err != nil || id == "" {
		return err
	}
	key, err := m.requestKey()
	if err != nil {
		return err
	}
	result, err := m.lab.ResumeCancel(context.Background(), id, subscriptionRevision(s, id), key)
	if err != nil {
		return err
	}
	return m.print(result)
}

func (m *menu) createQuote() error {
	customer, err := m.required("客戶 ID")
	if err != nil {
		return err
	}
	q, err := m.lab.CreateQuote(context.Background(), customer, "basic")
	if err != nil {
		return err
	}
	return m.print(q)
}

func (m *menu) createProQuote() error {
	customer, err := m.required("客戶 ID")
	if err != nil {
		return err
	}
	seats, err := m.positiveInt("付費席次數")
	if err != nil {
		return err
	}
	q, err := m.lab.CreateQuoteWithSeats(context.Background(), customer, "pro", seats)
	if err != nil {
		return err
	}
	return m.print(q)
}

func (m *menu) acceptQuote() error {
	s, err := m.state()
	if err != nil {
		return err
	}
	var choices []menuChoice
	for _, x := range s.Quotes {
		if !x.Accepted && x.ContractVersionID == "" {
			choices = append(choices, menuChoice{x.ID, x.CustomerID + " / " + money(x.AmountMinor, x.Currency)})
		}
	}
	id, err := m.pick("未接受報價", choices)
	if err != nil || id == "" {
		return err
	}
	key, err := m.requestKey()
	if err != nil {
		return err
	}
	for _, x := range s.Quotes {
		if x.ID == id {
			r, err := m.lab.AcceptQuote(context.Background(), id, x.Fingerprint, key)
			if err != nil {
				return err
			}
			return m.print(r)
		}
	}
	return errors.New("報價不存在")
}

func (m *menu) createPayment() error {
	s, err := m.state()
	if err != nil {
		return err
	}
	id, err := m.pick("有未付金額的發票", invoiceChoices(s, true))
	if err != nil || id == "" {
		return err
	}
	amount, err := m.amount("收款金額")
	if err != nil {
		return err
	}
	key, err := m.requestKey()
	if err != nil {
		return err
	}
	opID, err := m.lab.CreatePayment(context.Background(), id, amount, key)
	if err != nil {
		return err
	}
	fmt.Fprintf(m.out, "已建立付款操作 %s；請在付款選單送出。\n", opID)
	return nil
}

func (m *menu) retryPayment() error {
	s, err := m.state()
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	var choices []menuChoice
	for _, p := range s.Payments {
		if p.Status == "definitively_failed" && !seen[p.InvoiceID] {
			seen[p.InvoiceID] = true
			choices = append(choices, menuChoice{p.InvoiceID, "曾有確定失敗付款"})
		}
	}
	id, err := m.pick("重試付款的發票", choices)
	if err != nil || id == "" {
		return err
	}
	key, err := m.requestKey()
	if err != nil {
		return err
	}
	opID, err := m.lab.RetryFailedPayment(context.Background(), id, key)
	if err != nil {
		return err
	}
	fmt.Fprintf(m.out, "已建立重試付款 %s；請在付款選單送出。\n", opID)
	return nil
}

func (m *menu) fault() (string, error) {
	value, err := m.ask("故障模擬 [normal/lost_response/crash_after_provider]（預設 normal）")
	if err != nil {
		return "", err
	}
	if value == "" || value == "normal" {
		return "", nil
	}
	if value != "lost_response" && value != "crash_after_provider" {
		return "", errors.New("不支援的故障模式")
	}
	return value, nil
}

func (m *menu) dispatchPayment() error {
	fault, err := m.fault()
	if err != nil {
		return err
	}
	id, err := m.lab.DispatchNext(context.Background(), fault)
	if id != "" {
		fmt.Fprintf(m.out, "付款操作：%s\n", id)
	}
	if errors.Is(err, lab.ErrPaymentUnknown) || errors.Is(err, lab.ErrInjectedCrash) {
		fmt.Fprintf(m.out, "結果尚未在 Commerce 確認：%v。請使用原操作查證，不要建立新請求鍵。\n", err)
		return nil
	}
	if err != nil {
		return err
	}
	fmt.Fprintln(m.out, "付款已送出並寫入本地觀察；可查狀態或重建權益。")
	return nil
}

func (m *menu) reconcilePayment() error {
	s, err := m.state()
	if err != nil {
		return err
	}
	id, err := m.pick("付款操作", paymentChoices(s))
	if err != nil || id == "" {
		return err
	}
	found, err := m.lab.ReconcilePayment(context.Background(), id)
	if err != nil {
		return err
	}
	fmt.Fprintf(m.out, "Provider 找到原操作結果：%v。\n", found)
	return nil
}

func (m *menu) setPaymentDecision() error {
	s, err := m.state()
	if err != nil {
		return err
	}
	id, err := m.pick("尚未送出的付款", paymentChoices(s, "created"))
	if err != nil || id == "" {
		return err
	}
	status, err := m.required("Provider 結果 [succeeded/definitively_failed]")
	if err != nil {
		return err
	}
	if err := m.lab.SetFakePaymentDecision(context.Background(), id, status); err != nil {
		return err
	}
	fmt.Fprintln(m.out, "已設定 fake provider 決策；現在可送出付款。")
	return nil
}

func (m *menu) runRenewals() error {
	results, err := m.lab.RunRenewals(context.Background())
	if err != nil {
		return err
	}
	fmt.Fprintf(m.out, "新增續約發票：%d 筆。\n", len(results))
	return m.print(results)
}

func (m *menu) refreshEntitlements() error {
	if err := m.lab.RefreshEntitlements(context.Background()); err != nil {
		return err
	}
	fmt.Fprintln(m.out, "權益投影已根據目前帳期與帳務事實重建。")
	return nil
}

func (m *menu) postReduction() error {
	s, err := m.state()
	if err != nil {
		return err
	}
	var choices []menuChoice
	for _, x := range s.Invoices {
		if x.Balance.ObligationMinor > 1 {
			choices = append(choices, menuChoice{x.ID, "目前應收 " + money(x.Balance.ObligationMinor, x.Balance.Currency)})
		}
	}
	id, err := m.pick("待減額發票", choices)
	if err != nil || id == "" {
		return err
	}
	amount, err := m.amount("減額")
	if err != nil {
		return err
	}
	reason, err := m.required("更正原因")
	if err != nil {
		return err
	}
	key, err := m.requestKey()
	if err != nil {
		return err
	}
	result, err := m.lab.PostReduction(context.Background(), id, amount, reason, key)
	if err != nil {
		return err
	}
	return m.print(result)
}

func (m *menu) applyCredit() error {
	s, err := m.state()
	if err != nil {
		return err
	}
	grantID, err := m.pick("有可用額度的 credit", creditChoices(s, true))
	if err != nil || grantID == "" {
		return err
	}
	var choices []menuChoice
	for _, x := range s.Invoices {
		if x.PeriodIndex > 0 && x.Balance.OutstandingMinor > 0 {
			choices = append(choices, menuChoice{x.ID, "未付 " + money(x.Balance.OutstandingMinor, x.Balance.Currency)})
		}
	}
	invoiceID, err := m.pick("後續續約發票", choices)
	if err != nil || invoiceID == "" {
		return err
	}
	amount, err := m.amount("應用額度")
	if err != nil {
		return err
	}
	key, err := m.requestKey()
	if err != nil {
		return err
	}
	id, err := m.lab.ApplyCredit(context.Background(), grantID, invoiceID, amount, key)
	if err != nil {
		return err
	}
	fmt.Fprintf(m.out, "Credit 應用 ID：%s。若發票仍未付，請建立新金額的付款操作。\n", id)
	return nil
}

func (m *menu) reserveRefund() error {
	s, err := m.state()
	if err != nil {
		return err
	}
	grantID, err := m.pick("有可用額度的 credit", creditChoices(s, true))
	if err != nil || grantID == "" {
		return err
	}
	amount, err := m.amount("退款金額")
	if err != nil {
		return err
	}
	key, err := m.requestKey()
	if err != nil {
		return err
	}
	id, err := m.lab.ReserveRefund(context.Background(), grantID, amount, key)
	if err != nil {
		return err
	}
	fmt.Fprintf(m.out, "已保留退款 %s；請在退款選單送出。\n", id)
	return nil
}

func (m *menu) dispatchRefund() error {
	fault, err := m.fault()
	if err != nil {
		return err
	}
	id, err := m.lab.DispatchRefundNext(context.Background(), fault)
	if id != "" {
		fmt.Fprintf(m.out, "退款操作：%s\n", id)
	}
	if errors.Is(err, lab.ErrPaymentUnknown) || errors.Is(err, lab.ErrInjectedCrash) {
		fmt.Fprintf(m.out, "退款結果尚未在 Commerce 確認：%v。額度仍保留，請用原操作查證。\n", err)
		return nil
	}
	if err != nil {
		return err
	}
	fmt.Fprintln(m.out, "退款已送出並寫入本地觀察。")
	return nil
}

func (m *menu) reconcileRefund() error {
	s, err := m.state()
	if err != nil {
		return err
	}
	id, err := m.pick("退款操作", refundChoices(s))
	if err != nil || id == "" {
		return err
	}
	found, err := m.lab.ReconcileRefund(context.Background(), id)
	if err != nil {
		return err
	}
	fmt.Fprintf(m.out, "Provider 找到原退款結果：%v。\n", found)
	return nil
}

func (m *menu) setRefundDecision() error {
	s, err := m.state()
	if err != nil {
		return err
	}
	id, err := m.pick("尚未送出的退款", refundChoices(s, "created"))
	if err != nil || id == "" {
		return err
	}
	status, err := m.required("Provider 結果 [succeeded/definitively_failed]")
	if err != nil {
		return err
	}
	if err := m.lab.SetFakeRefundDecision(context.Background(), id, status); err != nil {
		return err
	}
	fmt.Fprintln(m.out, "已設定 fake provider 決策；現在可送出退款。")
	return nil
}
