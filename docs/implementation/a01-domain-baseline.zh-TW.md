# A01 領域回歸與管理入口比對

[English](a01-domain-baseline.md) | **繁體中文** | [简体中文](a01-domain-baseline.zh-CN.md)


狀態：部分完成。2026-09-27 以隔離的 commerce／provider SQLite 執行 `go test ./... -count=1`，950／950 通過；`go vet ./...` 通過。通過代表下列既有路徑在本機 fixture 中保持原行為；只有「直接比對」欄列出的案例，才證明管理命令與直接領域入口產生相同的金融事實。

| 情境 | 領域或 API 基線 | 關聯管理操作 | 直接比對 |
| --- | --- | --- | --- |
| S01 新購買 | `lab/lab_test.go` 的 `TestS01NewBasicPurchase` | C01／C02／C09／C45 | C01／C02 Pro 五席購買：`lab/admin_financial_parity_test.go` |
| S02 月底續約 | `lab/billing_test.go` 的 `TestS02MonthEndAnchorAndIdempotentRenewal` | C44／C09 | C44 月底帳期、帳單、付款義務及實收：同上 |
| S03 續約失敗與寬限 | `lab/billing_test.go` 的 `TestS03FailedRenewalGraceSuspensionAndRecovery` 與 `TestUnknownRenewalRetainsVerificationGrace` | C44／C09／C08／C10／C45 | 確定失敗、寬限／暫停、C08 補款：`lab/admin_failed_renewal_parity_test.go`；未知結果超過寬限期限仍保留查證寬限、C10 查證後恢復：`lab/admin_unknown_renewal_parity_test.go` |
| S04 收款回應遺失 | `lab/lab_test.go` 的 `TestS04LostResponseKeepsOriginalOperation` | C09／C10／C49 | C49 注入回應遺失後，C09 保留原操作待查證，C10 查證並完成原命令：`lab/admin_lost_response_parity_test.go` |
| S05 中斷、Webhook 次序與投影恢復 | `lab/lab_test.go` 的 `TestS05CrashWebhookOrderAndProjectionRecovery` | C09／C45／C49 | C49 注入 provider 提交後中斷，C09 復原原命令，重啟後重複及過期 Webhook 與 C45 投影重建：`lab/admin_crash_webhook_parity_test.go` |
| S06 下期變更與取消 | `lab/subscription_changes_test.go` 的兩個 `TestS06*` | C03／C05／C06／C44／C45 | C01 變更報價＋C03 排程＋C44 續約比對：`lab/admin_financial_parity_test.go`；C05／C06 取消、恢復、再次取消與 C44／C45 邊界到期比對：`lab/admin_cancel_resume_parity_test.go` |
| S07 期中升級與待查證 | `lab/immediate_test.go` 的兩個 `TestS07*` | C04／C09／C10／C13／C14／C44／C49 | C04 正常付款的期中金額與權益：`lab/admin_financial_parity_test.go`；C49／C09 回應遺失、C10 沿原付款查證、C13 延遲更正，以及跨帳期 C44 hold、C14 未履行決議後 Basic 續約：`lab/admin_s07_parity_test.go`。兩個隔離 SQLite／假服務商案例逐階段比對升級狀態、帳單義務／實收／釋出／未清、funded grant、收據、provider capture 及續約帳單。 |
| S08 價格版本與遷移 | `lab/catalog_admin_test.go`、`lab/price_migration_test.go` 的 `TestS08*` | C18／C21–C25／C44 | C18 發布、C21 選價、C22 計畫、C23 暫停、C25 恢復與 C44 邊界套用及回遷；另驗 revision 衝突令批次暫停、C24 明確跳過後 C44 以舊價續約：`lab/admin_price_migration_parity_test.go` |
| S09 用量關帳與晚到事件 | `lab/usage_test.go` 的 `TestS09UsageCloseLateEventAndOriginalPeriodDebit` | C26–C30／C44 | C26 用量、C28 關帳、C29 重算、C27 調整、C30 credit note 及 C44 續約／暫停：`lab/admin_usage_parity_test.go` |
| S10 企業合約與 Net30 | `lab/contracts_test.go` 的兩個 `TestS10*` | C01／C02／C31／C32／C44 | C31／C01／C02 建立、C44 缺後續價格時暫停續約、C32 到期收款：`lab/admin_contract_parity_test.go`；指定後續價格、兩期合約收款與轉為自助價格：`lab/admin_contract_transition_parity_test.go` |
| S11 減額、抵扣與退款額度 | `lab/corrections_test.go`、`lab/corrections_edges_test.go` 的 `TestS11*` | C11／C12／C15–C17 | C11 部分付款及 C11／C12 funded credit：`lab/admin_financial_parity_test.go`；C11／C12 與 C15／C16／C17 共用 funded credit 預算、未知退款保留預留額及原 capture 來源：`lab/admin_refund_budget_parity_test.go`；未知收款時封鎖 C11、C10 查證原 C09 後才可減額：`lab/admin_uncertain_reduction_parity_test.go`；部分收款加減額結清並啟用權益：`lab/admin_reduction_activation_parity_test.go`；C12 拒絕跨客戶使用 credit：`lab/admin_foreign_credit_parity_test.go`；未付款先減額並以 C07／C09 收取剩餘金額：`lab/admin_unpaid_reduction_parity_test.go`；其餘邊界情境仍待比對 |
| S12 對帳與修復 | `lab/reconciliation_test.go` 的 `TestS12*` | C08–C10／C33–C35 | C33 產生分類、C34 安全投影修復／原收款重試／未知付款查證／來源 revision 改變時封鎖、C35 人工決策：`lab/admin_reconciliation_parity_test.go`；C08–C10 的付款入口另見 S03／S04 |
| P01 AI token SKU | `lab/meter_catalog_test.go` 的 `TestP01AITokensSKUUsesGenericMeteredPath` | C19／C20／C21／C01／C02／C26／C28／C44 | C19 註冊 meter、C20 發布、C21 選價、C01／C02 購買、C26 記錄、C28 關帳與 C44 續約的同一路徑金額比對：`lab/admin_ai_meter_parity_test.go`；來源／meter 拒絕、重算與 credit note 邊界仍待直接比對 |
| P02 舊 client 的費用可見性 | `api/v1_test.go` 的 `TestV1LegacyClientRejectsUnshownAITokenCharge` | C19／C20／C21 發布後的 `/v1` 消費者 | `TestV1LegacyClientRejectsAdminPublishedUnshownCharge` 直接驗管理發布、舊 client 拒絕及新 client 接受 |
| P03 帳戶邊界遷移 | `lab/account_migration_test.go` 的兩個 `TestP03*` | C36–C43 | C36 連結、C37／C38 shadow、C41／C42 先讀後寫切換、C43 stop；另驗 C39 錯誤歷史憑證須人工審查、C40 更正，以及未知付款由 C10 查證後才可切換：`lab/admin_account_cutover_parity_test.go` |

## 已直接比對的金融不變量

金額以 USD 最小單位的整數保存。上述直接比對使用兩個獨立 SQLite 實例，以相同業務時間及相同輸入分別走領域與管理路徑；隨機產生的 ID 不參與數值比較。

| 不變量 | 本機證據 | 結論範圍 |
| --- | --- | --- |
| 接受報價後帳單、付款與權益歸屬一致 | C01／C02 Pro 五席比較帳單明細、帳期、價格指派、付款及權益 | 只涵蓋該購買 fixture |
| 月底續約不改變帳期界線或金額 | C44 比較 1 月 31 日起的下一帳期、到期日、帳單明細、付款操作、outbox 與 capture 後實收 2000 | 只涵蓋單戶正常續約 |
| 續約確定失敗後的寬限與補款 | C44／C09／C45／C08 與直接領域路徑比對 2000 帳單、原付款義務確定失敗、寬限到期暫停、同鍵重播只建立一筆新付款義務、補款後權益恢復與實收 2000 | 涵蓋確定失敗的單戶分支 |
| 續約結果未知時持續查證 | C49 注入回應遺失；C44／C09／C45 與直接領域路徑均在第八天保持 `grace` 且未入帳，C10 查證原操作後各只入帳一次，權益恢復 | 涵蓋單戶未知結果分支與指定時間點 |
| 收款回應遺失不重複請款 | C49 注入一次回應遺失；直接領域路徑與 C09／C10 均保持原付款操作 `unknown` 且零入帳，查證後各只形成一筆 provider capture 與一筆 2000 allocation；原管理派送命令及查證命令各一筆收據 | 涵蓋單一付款的回應遺失與重播；其他故障次序另待比對 |
| provider 提交後中斷可由來源事實恢復 | 直接領域路徑與 C09／C49 均在本地 allocation 前中斷；兩次重啟後，重複成功及過期 pending Webhook 不覆寫已成功付款；C45 重建權益，原 C09 命令完成且僅有一筆收據與 provider capture | 涵蓋單戶單付款的指定中斷點與 Webhook 次序 |
| 取消與恢復不得在邊界開出新帳單 | C05 安排取消、C06 恢復、再次 C05 取消後，C44 在 10 月 1 日不新增帳期或帳單；C45 令權益到期，狀態與直接領域路徑一致，再次 C44 仍不續約 | 涵蓋單戶排程取消／恢復順序；邊界後人工恢復另待比對 |
| 價格遷移只在帳期邊界生效 | C18／C21／C22／C44 與直接領域路徑比較兩戶 10 月 Pro v2 帳單各 11000、付款及實收；11 月只讓指定一戶回遷 v1，兩戶帳單分別為 10000／11000，價格指派來源與筆數一致 | 涵蓋正常遷移與回遷 |
| 遷移衝突不得替另一戶套新價 | 兩戶遷移中一戶 revision 改變；C44 與直接領域路徑只讓未衝突者以 11000 續約，批次暫停且衝突者尚無新帳期；C24 明確跳過後第二次 C44 才以原價 10000 續約，最終批次完成 | 涵蓋 revision 衝突與人工跳過；其他衝突理由仍待比對 |
| 人工暫停與恢復保留遷移目標 | C23 暫停、C25 恢復與直接領域路徑維持同一遷移項；恢復後 C44 才在下期產生 11000 Pro v2 帳單且批次完成 | 涵蓋一戶未衝突的暫停／恢復 |
| 晚到用量歸原帳期並受 credit note 守衛 | C26–C30／C44 與直接領域路徑比對 20003→20010→20003 的 rating revision、原帳期晚到事件的次期 1 個最小貨幣單位 debit、反向調整後暫停續約、credit note 資金來源 1 及恢復後 10000 帳單 | 涵蓋 tasks 計量單戶與指定事件次序；其他 meter 與部分關帳仍待比對 |
| 對帳修復不得創造第二筆收款 | C33／C34 與直接領域路徑比對待派送 outbox 與 provider 已成功但本地未知兩種分類；修復及重播後均只保留原付款操作、單筆 provider capture 與 2000 入帳 | 涵蓋單戶兩種付款修復 fixture |
| 修復必須服從來源版本與人工審查 | 權益投影缺失由 C34 安全重建且同命令重播僅一筆修復及收據；來源 revision 改變後 C34 與直接領域路徑均封鎖；provider 金額不符維持 `MANUAL_REVIEW`，C34 不改付款與帳單，C35 留下人工決策 | 涵蓋投影、revision 變動與金額不符 fixture |
| 新計量 SKU 沿通用帳單與用量路徑 | C19／C20／C21 發布 AI token 費率後，C01／C02 以 3000 建立與結清首期；C26 記錄 105 tokens，C28 扣除含量 100 後計價 1，C44 次期帳單 3001 並標記原帳期用量來源；與直接領域路徑逐項一致 | 涵蓋單一 AI token SKU；來源驗證與晚到調整仍待比對 |
| 帳戶切換維持唯一付款 writer | C36／C37／C38／C41／C42／C43 與直接領域路徑比對 legacy writer 時零新訂閱、shadow 完成後先切讀再切寫、切換後首筆 2000 帳單及唯一 capture、stop 後新收費被拒而原接受命令可重播 | 涵蓋單戶無歷史資料的切換順序 |
| 歷史憑證與未知付款阻擋切換 | 有歷史帳單時，未知付款與錯誤價格憑證均阻擋 readiness；C39 錯誤 backfill 留在人工審查，C10 查證原收款後仍不能切換，C40 以原帳單 basic-v1 更正後才 ready；兩路均保留原 2000 入帳與單筆 capture | 涵蓋單一歷史帳單與指定審查決策 |
| funded credit 的套用與退款共用預算 | C11 建立 1000 funded grant，C12 套用 500 後 C15 只保留其餘 500；C16 回應遺失期間仍保留預留額且拒絕額外退款，C17 查證後轉為已退 500，provider 僅一筆退款並指向原帳單 capture；兩路金融事實一致 | 涵蓋單一 grant、單次用款與未知退款分支 |
| 下期方案變更不得提前改變已生效價格 | C01／C03 確認前後原 Basic 價格仍生效且 revision 為 2；C44 到期後兩路均用 Pro 五席產生 10000 的帳單與同樣的付款義務 | 尚未比對取消、恢復或併行排程 |
| 部分付款減額只降低應收，不憑空建立 funded credit | C11 比較原額 10000、已收 6000、減額 2000、未收 2000，確認舊全額付款操作取消；補收後兩路各只有兩筆 capture | 只涵蓋此付款次序與金額 |
| 未知收款不得先減額或建立 credit | 直接領域入口與 C11 預覽均拒絕回應遺失期間的 2000 減額；SQLite 無更正、grant 或 C11 命令。C10 查證並完成原 C09 後，兩路帳單義務 8000、實收 10000、funded grant 2000，provider 各僅一筆 capture；瀏覽器驗 HTTP 409、畫面錯誤及無 C11 寫入 | 涵蓋單戶全額收款回應遺失與一次減額；不推論其他中斷點或並行命令 |
| 減額結清首期應收時啟用權益 | 兩路先收 6000 仍保持 pending，再減額 4000 後帳單義務與實收均為 6000、未清為零，訂閱與權益均 active；沒有 funded grant，provider 各只有一筆 capture | 涵蓋單戶首期部分付款後一次減額，不推論續約或併行減額 |
| funded credit 不得跨客戶挪用 | 兩路從已付款帳單建立 1000 grant；直接套用及 C12 預覽對外客戶帳單的 500 抵扣均拒絕。grant 可用額仍為 1000、目標帳單未清 10000，沒有 credit application 或 C12 命令 | 涵蓋單一外客戶帳單與單次套用，未驗跨客戶退款或資料庫直寫 |
| 未付款減額須取消舊收款，只收剩餘應收 | 兩路先將 10000 帳單減額 2000，原付款操作與 outbox 均取消、provider 零 capture、無 funded grant；直接付款與 C07／C09 各建立新 8000 操作並只 capture 一次。最終義務與實收均 8000、未清為零，刷新後權益 active | 涵蓋單戶未付款減額與一次補收；重播鍵衝突與跨程序並行仍待比對 |
| 已付款減額的釋出與跨帳單抵扣同源且不超支 | C11／C12 比較來源帳單釋出 1000、grant 1000、套用 500、剩餘 500，以及目標帳單未收 1500 | 單次套用與後續退款已直接比對；並行爭用的領域及管理測試另列 A16 |
| Net30 在到期前無 capture 義務，到期後只收一次 | C31／C01／C02 比較合約來源、7500 帳單與 2026-10-01 到期日；到期前 outbox 為零，C32 與直接收款各建立一筆 outbox，capture 後各實收 7500 且未收為零 | 只涵蓋單一合約帳單 |
| 缺後續價格時不得憑空續約 | 到期時直接 `RunRenewals` 與 C44 均只留下 `contract_next_price_missing`，不建立下一帳期，權益暫停；既有 Net30 應收仍可由 C32 收回 | 涵蓋該合約 fixture 的缺價分支 |
| 明確後續價格只切換一次 | C31 設定後續價格；C44／C32 與直接領域路徑逐期比對合約 7500、兩張 Net30 帳單到期收款、轉價後 10000 帳單、付款義務與實收；11 月 1 日指派新增一次，12 月 1 日仍只有兩筆價格指派與一筆合約轉換 | 涵蓋該合約 fixture 的指定後續價格分支 |
| 新費率不得讓未展示費用的舊 client 建立義務 | C19／C20／C21 透過管理命令發布 AI token SKU；`/v1` 報價揭露三個元件與當下應付 3000；無 `meter:ai_tokens_admin` 能力宣告回 409 且零訂閱，有宣告才接受 | 涵蓋本機該 SKU 與舊 API client；未驗所有 consumer 版本 |

這份對照不把原有領域測試通過推論成所有管理入口等價。未標示直接比對的情境仍須核對其管理命令是否沿用相同領域交易、來源守衛與恢復規則；A01 因此保持部分完成。
