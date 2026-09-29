# Web Admin C01–C49 證據盤點

[English](web-admin-action-audit.md) | **繁體中文** | [简体中文](web-admin-action-audit.zh-CN.md)


狀態：**部分完成**。盤點日期：2026-09-29。C01–C49 均有基礎入口表；C07–C12、C15–C21 已補執行鏈與證據判定，其餘仍待同粒度核對。本表記錄可重跑的程式與測試入口，不把程式碼存在或頁面可開啟視為完整驗收。

## 瀏覽器請求與收據關聯（2026-09-29）

完整 Playwright 回歸 **155／155 通過**。其中 `admin.spec.ts` 的稽核檔記錄 104 筆瀏覽器情境；`audit-action-cases.mjs` 確認 C01–C49 **49／49** 均有瀏覽器 `POST /admin/api/commands`，且請求的冪等鍵對上同一動作的 `admin_commands` 成功狀態與 `admin_command_receipts`。稽核不再以「同一測試有瀏覽器請求，也有某筆成功命令」推斷兩者相關；以該舊條件構造的反例會失敗。可依根目錄 README 的命令重跑。其後新增的 C01 過期 revision 原子性瀏覽器案例定向執行 **1／1 通過**。

這項證據只證明逐動作的瀏覽器提交與成功收據關聯；逐動作的來源守衛、錯誤與中斷恢復、金融事實仍須各自核對，A30 維持部分完成。

2026-09-29 後續定向瀏覽器驗證：C01 過期 revision 原子性、C03 舊報價遇新選價、C03／C04 競爭者修改訂閱後重新預覽，以及 C03／C04 回應遺失後的原鍵恢復，六條相關路徑各自通過。新增情境已納入完整回歸 155／155，且 104 筆瀏覽器情境的收據關聯稽核通過。

## 共用執行路徑

- 所有命令實際使用 `POST /admin/api/commands`，以 `action_id` 指定 C01–C49；需要預覽的命令先使用 `POST /admin/api/previews`。下表的路徑是 Web Admin 頁面路徑，不是各動作獨立的 HTTP endpoint。這與工程契約中逐資源的擬定 POST 路徑不同；目前採單一受控入口，應以這個已實作契約作為後續驗收依據。
- `api/admin/permissions.go` 決定能力，`lab/admin_commands.go` 負責 canonical payload、冪等鍵、預覽綁定、命令受理及執行。命令可由 `GET /admin/api/commands/{id}` 找回；可恢復者使用 `POST /admin/api/commands/{id}/resume`。批次另由 `GET /admin/api/jobs/{id}` 查逐項結果。
- `api/admin/action_http_matrix_test.go` 對 49 個動作的命令與預覽入口逐項驗無 session、缺 CSRF、缺能力時的 HTTP 狀態與錯誤碼；另在具備能力時逐項送未知 payload 欄位，驗 422 且未受理任何命令。`api/admin/permissions_test.go` 驗每個動作有能力映射，及具備其能力時的授權判斷。這些測試尚未證明每種合法 payload 或各種金融結果。
- `lab/admin_numeric_payload_test.go` 驗 C07/C15 的大整數精確度與不合法金額格式、C18 的精確費率分母、C33 的 UTC 截止時間邊界。`api/admin/numeric_http_test.go` 進一步在真實管理 HTTP handler 驗 C07／C11／C12／C15 不合法金額、C18 不合法分母和 C33 不合法 UTC 於命令或預覽入場前被拒，且無命令；有效大整數字串在 C07／C15 抵達來源查詢，C18 預覽保留精確分母。`web/admin/e2e/admin.spec.ts` 另驗 C07／C11／C12／C15 表單拒絕超出 int64 上限、改為超過 JavaScript safe integer 但仍在 int64 範圍後解除錯誤，且不建立命令。其餘欄位仍需各自的 HTTP／browser 邊界案例。
- `lab/admin_preview_admission_test.go` 在 C07 驗預覽的 actor、動作、對象、內容、期限及單次 claim；原鍵在預覽到期後仍找回原命令，不同 payload 不能共用該鍵。受理流程現在對所有 R 類動作使用同一組核對，且在不符時不建立命令；各動作的來源版本與交易內重算仍需個別證據。
- `lab/admin_reconcile_no_evidence_test.go` 驗 C10/C17 查證時 provider 尚無終局事實，命令維持待查證且無成功收據；退款額度保持預留。原 provider key 後來出現成功證據，原命令才完成。瀏覽器另驗 C10 的等待與恢復。
- 完整 Playwright 套件目前 89 個測試（9 個檔案），包含真實 Go／SQLite 瀏覽器情境及 58 個已知路由的開啟檢查；其中三個情境驗跨分頁登出、舊畫面清除、重新登入回原頁，以及延遲抵達的舊 401 不覆蓋新登入。資源篩選情境驗跨頁、返回、重整、變更篩選與 UTC 建立時間下界的 URL 狀態。`lab/admin_resource_filters_test.go` 與 `api/admin/resource_filters_test.go` 驗白名單、篩選後游標、時間範圍及查詢錯誤；`lab/admin_resource_queries_test.go`、`api/admin/resource_filters_test.go` 另驗缺來源的 null、真正的零值及訂閱／帳單插入後不重複，瀏覽器驗對應欄位顯示；`api/admin/query_params_test.go` 驗其他分頁清單拒絕未知、重複和格式損壞的參數。路由檢查只證明頁面可進入。下表的 Go 欄是包含該動作 ID 的交易測試檔，不表示完整錯誤矩陣已覆蓋。

`R` 表示必須持有效預覽確認；`N` 表示無預覽，但仍須授權、冪等與來源檢查。表中路徑省略 `/admin` 前綴。Go 證據路徑省略 `lab/` 前綴。

| 動作 | UI 路徑 | 能力 | 預覽 | 交易測試檔 |
| --- | --- | --- | --- | --- |
| C01 | `/quotes/new` | `subscription.manage`；合約另需 `contract.manage` | N | `admin_commands_test.go`、`admin_contract_actions_test.go`、`admin_financial_parity_test.go`（Pro 五席金融事實比對）、`admin_contract_parity_test.go`（Net30 合約報價比對）、`web/admin/e2e/admin.spec.ts`（購買與變更綁定報價） |
| C02 | `/quotes/:id/accept` | `subscription.manage`；合約另需 `contract.manage` | R | `admin_commands_test.go`、`admin_financial_parity_test.go`（Pro 五席金融事實比對）、`admin_contract_parity_test.go`（Net30 接受後帳單與權益比對）、`admin_stopped_writer_test.go`（停止後拒絕新預覽，舊預覽執行失敗且無訂閱／收據）、`web/admin/e2e/admin.spec.ts`（購買與預覽後來源變動） |
| C03 | `/subscriptions/:id/schedule-plan` | `subscription.manage` | R | `admin_schedule_plan_test.go`、`admin_financial_parity_test.go`（下期 Pro 五席排程及 C44 續約與直接領域路徑比對）、`price_migration_test.go`、`web/admin/e2e/migration-conflict.spec.ts`（遷移待處理時預覽回 409，正式排程亦拒絕）；`admin.spec.ts` 實際排程並核對目標價、唯一訂閱與成功收據。 |
| C04 | `/subscriptions/:id/upgrade` | `subscription.manage` | R | `admin_immediate_upgrade_test.go`、`admin_financial_parity_test.go`（週期中點升級的領域／管理路徑金額與權益比對）、`web/admin/e2e/admin.spec.ts`（期中升級與舊預覽重算） |
| C05 | `/subscriptions/:id/cancel` | `subscription.manage` | R | `admin_commands_test.go`、`price_migration_test.go`、`web/admin/e2e/migration-conflict.spec.ts`（遷移 pending／conflicted 時預覽回 409，沒有預覽或排程寫入）；`admin.spec.ts` 實際排定取消並核對此訂閱唯一成功收據。 |
| C06 | `/subscriptions/:id/cancel`（依狀態顯示恢復） | `subscription.manage` | R | `admin_commands_test.go`、`admin_stopped_writer_test.go`（停止後拒絕新預覽，舊預覽執行失敗且保留取消排程）、`web/admin/e2e/admin.spec.ts` 實際恢復取消排程、核對排程狀態與此訂閱唯一成功收據。 |
| C07 | `/invoices/:id/payments/new` | `finance.adjust` | R | `admin_payment_test.go`（含收據失敗時保留舊收款、重啟後原鍵恢復）、`admin_competing_payment_preview_test.go`（兩個已受理預覽競爭，唯一替代操作與收據、舊命令失效及雙鍵重播）、`admin_cross_instance_payment_test.go`（兩個獨立 Lab 實例共用 SQLite 並行執行）、`admin_cross_process_payment_test.go`（兩個獨立 OS 進程同步執行）、`web/admin/e2e/admin.spec.ts`（部分付款、預覽後餘額變動及雙分頁替代操作競爭）、`admin_payment_dispatch_interleaving_test.go`（跨實例付款建立、舊預覽失效及精確派送） |
| C08 | `/payments/:id/retry` | `finance.adjust` | R | `admin_payment_test.go`、`admin_competing_payment_preview_test.go`（兩個已受理重試預覽只建立一筆新義務，失效命令零成功收據、原鍵重播）、`admin_cross_instance_payment_test.go`（兩個獨立 Lab 實例共用 SQLite 並行執行）、`admin_cross_process_payment_test.go`（兩個獨立 OS 進程同步執行）、`web/admin/e2e/admin.spec.ts`（確定失敗後重試與餘額變動）、`admin_payment_dispatch_interleaving_test.go`（跨實例付款建立、舊預覽失效及精確派送）、`web/admin/e2e/admin.spec.ts`（雙分頁 C08 預覽競爭、HTTP 409 與勝出操作後續 C09 精確派送） |
| C09 | `/payments/:id/dispatch` | `finance.adjust` | R | `admin_external_test.go`、`web/admin/e2e/admin.spec.ts`（付款 provider commit 後中斷；待查證時調整時鐘仍沿原 business time 完成）、`admin_payment_dispatch_interleaving_test.go`（跨實例付款建立、舊預覽失效及精確派送）、`web/admin/e2e/admin.spec.ts`（C07 取代後舊 C09 預覽 HTTP 409，僅新操作派送）、`web/admin/e2e/admin.spec.ts`（指定較晚付款操作派送，不取出佇列較早操作） |
| C10 | `/payments/:id/reconcile` | `finance.adjust` | N | `admin_external_test.go`、`admin_reconcile_no_evidence_test.go`、`provider_lookup_failure_test.go`、`api/admin/provider_lookup_failure_test.go`、`web/admin/e2e/admin.spec.ts` |
| C11 | `/invoices/:id/reductions/new` | `finance.adjust` | R | `admin_reduction_test.go`、`admin_financial_parity_test.go`（部分付款與已付款減額兩種資金來源和直接領域路徑比對）、`web/admin/e2e/admin.spec.ts` 的帳單來源案例 |
| C12 | `/credits/:id/apply` | `finance.adjust` | R | `admin_apply_credit_test.go`、`admin_financial_parity_test.go`（跨帳單套用額度與直接領域路徑比對）、`admin_credit_budget_interleaving_test.go`（跨實例與 C15 爭用同一額度）、`web/admin/e2e/admin.spec.ts` 的 C12 案例 |
| C13 | `/jobs/change-corrections` | `finance.adjust` | R | `admin_change_corrections_test.go`（101 筆來源變動後跨批輪轉）、`web/admin/e2e/admin.spec.ts` 的延遲更正來源案例：實際執行批次，模擬提交後回應遺失；重整沿同一冪等鍵找回，SQLite 核對唯一工作、逐項結果、更正與命令收據。 |
| C14 | `/changes/:id/resolve-unfulfilled` | `finance.adjust` | R | `admin_unfulfilled_test.go`、`web/admin/e2e/admin.spec.ts` 的未履行升級案例：實際執行未履行決議，模擬提交後回應遺失；重整沿同一冪等鍵找回，SQLite 核對唯一決議、更正、Credit 與命令收據。 |
| C15 | `/credits/:id/refunds/new` | `finance.adjust` | R | `admin_reserve_refund_test.go`（含收據失敗回滾、重啟與原鍵恢復）、`admin_refund_budget_parity_test.go`（與直接領域路徑比對 funded credit 預算）、`admin_credit_budget_interleaving_test.go`（跨實例與 C12 爭用同一額度）、`web/admin/e2e/admin.spec.ts`（已收款減額後預留退款、C12 額度爭用、雙分頁 C15 預覽失效後重新確認與唯一收據） |
| C16 | `/refunds/:id/dispatch` | `finance.adjust` | R | `admin_external_test.go`、`web/admin/e2e/admin.spec.ts`（退款 provider commit 後中斷；確定失敗時保留額回到可用額度）、`admin_refund_dispatch_interleaving_test.go`（C15／C16 跨實例競爭、唯一 provider 退款與額度收據） |
| C17 | `/refunds/:id/reconcile` | `finance.adjust` | N | `admin_external_test.go`、`admin_reconcile_no_evidence_test.go`、`provider_lookup_failure_test.go`、`admin_refund_reconcile_test.go`（UNKNOWN 退款查證、同鍵重播與唯一收據）、`web/admin/e2e/admin.spec.ts`（C16 回應遺失後經 C17 查證，原 C16 仍沿原鍵完成，提供者退款唯一） |
| C18 | `/prices/pro/new` | `catalog.publish` | R | `admin_catalog_test.go`、`web/admin/e2e/admin.spec.ts`（發布元件與 checksum、同版號與原 ID 變更衝突、所有數值欄位的 UI 非法邊界、版本來源變動；提交後回應遺失再以原鍵恢復，SQLite 價格、命令與收據各一筆）、`catalog_receipt_failure_test.go`（真實 HTTP 收據 INSERT 故障回滾及原鍵恢復）、`web/admin/e2e/migration-conflict.spec.ts`（遷移目標價格）；`admin_price_minimum_upfront_test.go` 與 `minimum_upfront_http_test.go` 驗固定費用加最少一席的 `int64` 邊界與預覽拒絕 |
| C19 | `/meters/new` | `catalog.publish` | R | `admin_catalog_test.go`（同 ID 不同 schema 的舊預覽失效、唯一收據、成功／失敗同鍵重播及改 payload 拒絕）、`api/v1_test.go`（管理發布 AI token meter 後檢查舊 client）、`web/admin/e2e/admin.spec.ts`（雙分頁衝突顯示 `PREVIEW_STALE`、既有 unit 不被覆寫；建立 AI token meter 並完成報價、接受及用量記錄） |
| C20 | `/prices/metered/new` | `catalog.publish` | R | `admin_catalog_test.go`（同 ID 不同金額、同方案版號不同 ID 的舊預覽失效，唯一價格與收據、原鍵重播與改 payload 拒絕）、`api/v1_test.go`（新費率揭露及舊 client 能力守衛）、`web/admin/e2e/admin.spec.ts`（雙分頁舊價格預覽顯示 `PREVIEW_STALE`、已發布 3500 與元件不被覆寫；全部數值欄位的 UI 非法邊界、AI 報價與 token 用量）；`admin_price_minimum_upfront_test.go` 與 `minimum_upfront_http_test.go` 驗固定費用加最少一席的 `int64` 邊界與預覽拒絕 |
| C21 | `/catalog-selections/new` | `catalog.publish` | R | `admin_catalog_test.go`（同方案／cohort／時點的兩個已受理命令競爭，舊命令 `PREVIEW_STALE`、唯一選價與收據、成功／失敗鍵重播）、`api/v1_test.go`（選價後同一報價的舊／新 client 行為）、`web/admin/e2e/migration-reversal.spec.ts`（選回舊價格）、`web/admin/e2e/admin.spec.ts`（雙分頁舊預覽回 409 且不覆寫先選價格；同時點改選回 409；AI 價格選價後報價接受） |
| C22 | `/price-migrations/new` | `migration.manage` | R | `admin_migrations_test.go`、`price_migration_test.go`（來源價格、席次與排程變動後的批次暫停及金融事實不變）、`web/admin/e2e/migration-reversal.spec.ts`（新批次反向遷移）、`migration-conflict.spec.ts`（revision 變動，以及來源價格、席位、排程三種獨立批次衝突）；`admin.spec.ts` 實際建立兩戶批次，SQLite 核對該批次唯一成功收據。 |
| C23 | `/price-migrations/:id/pause` | `migration.manage` | N | `admin_migrations_test.go`、`web/admin/e2e/admin.spec.ts` 實際暫停批次，SQLite 核對 paused 狀態及此批次唯一成功收據。 |
| C24 | `/price-migrations/:id/skip` | `migration.manage` | R | `admin_migrations_test.go`、`web/admin/e2e/migration-conflict.spec.ts`（衝突後明確略過，不撤銷已套用戶）；`admin.spec.ts` 略過一戶並核對 skipped 項目及此批次唯一成功收據。 |
| C25 | `/price-migrations/:id/resume` | `migration.manage` | R | `admin_migrations_test.go`、`web/admin/e2e/admin.spec.ts` 恢復後保留略過戶、未完成戶回 pending，SQLite 核對 active 狀態及此批次唯一成功收據。 |
| C26 | `/usage-events/new` | `usage.manage` | N | `admin_commands_test.go`、`web/admin/e2e/admin.spec.ts`（原事件入帳、回應遺失恢復、相同事件重送與變更 payload 拒絕） |
| C27 | `/usage-adjustments/new` | `usage.manage` | R | `admin_usage_actions_test.go`（併發調整耗用原事件剩餘量後舊預覽失效、零寫入，新預覽只反向剩餘量）、`web/admin/e2e/usage-preview-stale.spec.ts`（雙分頁併發調整回 409，原數量無法重新預覽，改為剩餘量後才成功）、`web/admin/e2e/admin.spec.ts`（用量調整與差額） |
| C28 | `/usage-periods/:id/close` | `usage.manage` | R | `admin_usage_actions_test.go`（預覽後新增符合 cutoff 的事件，舊關帳零計價修訂／收據，新預覽精確關帳）、`web/admin/e2e/usage-preview-stale.spec.ts`（雙分頁來源變動回 409，顯示 20000→20010，重新確認前零修訂，最終唯一收據）、`web/admin/e2e/admin.spec.ts`（關帳） |
| C29 | `/usage-periods/:id/rerate` | `usage.manage` | R | `admin_usage_actions_test.go`（預覽後新增晚到事件，舊重算不新增修訂／收據，新預覽含完整晚到用量）、`web/admin/e2e/usage-preview-stale.spec.ts`（雙分頁晚到事件回 409，顯示 20020→20030，重新確認前保留原修訂，最後僅一筆新修訂／收據）、`web/admin/e2e/admin.spec.ts`（晚到事件重算） |
| C30 | `/jobs/usage-credit-notes` | `finance.adjust` | R | `admin_usage_credit_notes_test.go`（101 個合成帳期衝突後跨批輪轉）、`admin_batch_jobs_test.go`（首項提交後重開只完成剩餘項）、`web/admin/e2e/admin.spec.ts`：負差額後實際執行 Credit Note 批次，模擬提交後回應遺失；重整沿同一冪等鍵找回，SQLite 核對唯一工作、逐項結果、Credit Note 與命令收據。 |
| C31 | `/contracts/new` | `contract.manage` | R | `admin_contract_actions_test.go`（同 ID 同內容併發發布只保留一版與唯一收據；不同內容令舊預覽失效、無成功收據且不可重寫）、`admin_contract_parity_test.go`（發布後合約來源與直接領域路徑比對）、`web/admin/e2e/admin.spec.ts`（雙分頁同 ID 不同內容回 409，顯示失效與重新預覽失敗；企業合約建立與 Net30）；`admin_price_minimum_upfront_test.go` 與 `minimum_upfront_http_test.go` 驗固定費用加最少一席的 `int64` 邊界與預覽拒絕 |
| C32 | `/jobs/contract-collections` | `finance.adjust` | R | `admin_contract_actions_test.go`（101 筆到期操作跨批收款；預覽後另一收款器先建立 capture outbox 時本批項目記為 `SOURCE_CHANGED`，不重複派送，同鍵找回原命令）、`admin_batch_jobs_test.go`（首項提交後重開只建立剩餘 capture outbox）、`admin_contract_parity_test.go`（到期收款與直接領域路徑比對）、`web/admin/e2e/admin.spec.ts`（合約到期收款批次） |
| C33 | `/reconciliation-runs/new` | `reconciliation.repair` | N | `admin_reconciliation_test.go`、`web/admin/e2e/admin.spec.ts`（差異建立與修復後 verification） |
| C34 | `/discrepancies/:id/repair` | `reconciliation.repair` | R | `admin_reconciliation_test.go`、`web/admin/e2e/admin.spec.ts`（安全修復、來源變動阻擋與 verification） |
| C35 | `/discrepancies/:id/manual-decisions` | `reconciliation.repair` | R | `admin_reconciliation_test.go`（另一審核者於預覽後先記錄決議，舊命令 `PREVIEW_STALE` 且零額外決議／收據；新預覽才記錄新決議）、`web/admin/e2e/manual-decision-stale.spec.ts`（雙分頁 409、顯示 `open → investigating`、再次確認前唯一決議／收據）、`web/admin/e2e/admin.spec.ts`（金額不符的人工作業記錄） |
| C36 | `/account-migrations/new` | `migration.manage` | R | `admin_account_links_test.go`、`web/admin/e2e/admin.spec.ts` 實際連結帳戶，顯示 owner 與門檻；SQLite 核對該 legacy ID 唯一成功收據。 |
| C37 | `/account-migrations/:id/shadow-quotes` | `migration.manage` | N | `admin_account_links_test.go`、`web/admin/e2e/admin.spec.ts` 實際記錄報價 shadow，驗比對一致及該帳戶唯一成功收據。 |
| C38 | `/account-migrations/:id/shadow-entitlements` | `migration.manage` | N | `admin_account_links_test.go`、`web/admin/e2e/admin.spec.ts` 實際記錄權益 shadow，驗 adapter 權益與該帳戶唯一成功收據。 |
| C39 | `/account-migrations/:id/provenance` | `migration.manage` | R | `admin_account_links_test.go`、`admin_provenance_stale_test.go`（預覽後同 legacy invoice 被不同映射先回填，原命令 `PREVIEW_STALE`，不覆寫來源或建立成功收據）、`web/admin/e2e/provenance-preview-stale.spec.ts`（雙分頁 409，保留先寫入的 manual review 映射與唯一收據）、`web/admin/e2e/admin.spec.ts`（不完整歷史來源入場） |
| C40 | `/account-migrations/:accountId/provenance/:legacyInvoiceId/resolve` | `migration.manage` | R | `admin_account_links_test.go`、`admin_provenance_stale_test.go`（另一審核者先完成來源決議，舊預覽失效且無重複決議事件／收據）、`web/admin/e2e/provenance-preview-stale.spec.ts`（雙分頁 409，保留唯一決議事件與收據）、`web/admin/e2e/admin.spec.ts`（歷史來源人工審核） |
| C41 | `/account-migrations/:id/switch-read` | `migration.manage` | R | `admin_account_links_test.go`、`admin_cutover_stale_test.go`（預覽後新增不一致報價 shadow，舊切讀 `PREVIEW_STALE`，讀寫 owner 不變且零成功收據）、`web/admin/e2e/admin.spec.ts`（未過門檻阻擋與切讀） |
| C42 | `/account-migrations/:id/switch-writer` | `migration.manage` | R | `admin_account_links_test.go`、`admin_cutover_stale_test.go`（切讀後預覽切寫，準備條件下降時舊命令失效且 writer 保持 legacy）、`web/admin/e2e/cutover-readiness-stale.spec.ts`（雙分頁 shadow 變動回 409，零切寫事件／收據）、`web/admin/e2e/admin.spec.ts`（切讀後切寫；成功回應遺失後重新整理，以原 request key 找回，命令、收據與 `writer_cutover` 各只一筆） |
| C43 | `/account-migrations/:id/stop` | `migration.manage` | R | `admin_account_links_test.go`、`admin_stopped_writer_test.go`（C02／C05／C06 預覽與停止後原命令）、`web/admin/e2e/admin.spec.ts`（停止事件及 owner；C02／C05 新預覽回 409，零後續商務命令）；SQLite 核對停止命令唯一成功收據。 |
| C44 | `/jobs/renewals` | `operations.run` | R | `admin_maintenance_jobs_test.go`（預覽固定成員；第二戶確認前到期仍須新批次；前 100 戶因遷移暫停而等待時，第 101 戶可由下批續約）、`admin_batch_jobs_test.go`（首戶提交後重開只續約剩餘戶）、`admin_financial_parity_test.go`（月底及排程變價續約比對）、`admin_contract_parity_test.go`（缺後續合約價格時暫停續約比對）、`web/admin/e2e/migration-reversal.spec.ts`（前後兩期金額及指派來源）、`migration-conflict.spec.ts`（部分套用、暫停、略過後不同價格續約；三種來源變動均無新增帳期或帳單） |
| C45 | `/jobs/entitlement-refresh` | `operations.run` | R | `admin_maintenance_jobs_test.go`（101 戶跨三批輪轉，尾端戶不飢餓；預覽後其中一戶 revision 改變時該項 `SOURCE_CHANGED` 且不重建其權益，另一戶仍成功並保留唯一批次收據）、`web/admin/e2e/entitlement-batch-source-stale.spec.ts`（雙分頁變更訂閱，工作詳情將衝突與成功戶分列，SQLite 保留唯一收據）、`web/admin/e2e/admin.spec.ts`（固定成員）、`web/admin/e2e/maintenance-backlog.spec.ts`（超過 100 戶時提示仍需下一批） |
| C46 | `/lab/clock` | `lab.control` | N | `admin_controls_test.go`、`web/admin/e2e/admin.spec.ts`（C09 待查證期間前進一天，不自動收款且不改原命令業務時間） |
| C47 | `/lab/payment-decisions/:id` | `lab.control` | N | `admin_controls_test.go`（provider 決策已提交、付款已 capture 後，重啟沿原 receipt 完成命令；相反結果競爭與終態後新命令均拒絕）、`web/admin/e2e/admin.spec.ts`（第二個相反決策顯示 `DOMAIN_REJECTED`、僅首命令有收據；確定失敗後重試） |
| C48 | `/lab/refund-decisions/:id` | `lab.control` | N | `admin_controls_test.go`（相反結果競爭與終態後新命令均拒絕）、`web/admin/e2e/admin.spec.ts`（第二個相反決策顯示 `DOMAIN_REJECTED`、僅首命令有收據且派送前無 provider 退款；確定失敗後 C16 派送釋放保留額；C16 回應遺失後經 C17 查證，最終僅一筆） |
| C49 | `/lab/faults/:id` | `lab.control` | N | `admin_controls_test.go`（另一筆付款先執行不會誤領指定票據；同操作第二張票據以 `failed/DOMAIN_REJECTED` 結束、原鍵重播不新增票據；付款／退款在領票後、provider 呼叫前重啟仍沿原命令使用票據）、`web/admin/e2e/admin.spec.ts`（重複建票錯誤可見且原票據繼續派送；一次性故障票據與原命令恢復）。`TestAdminFaultTicketsKeepPendingTicketVisibleAfterRecentUsedTickets` 與 `lab-fault-pagination.spec.ts` 驗待使用票據優先及有界游標可跨頁找回。 |

## C01／C03／C04 變更報價的衝突與原子性

`lab/admin_change_binding_mismatch_test.go` 驗 C01 在訂閱 revision 失效時回滾報價與綁定，保留失敗命令而不產生成功收據；C03／C04 均在報價綁定不符、選價被取代或訂閱 revision 改變時拒絕預覽，沒有新預覽、命令或方案變更。瀏覽器以真實表單驗 C01 零殘留，C03／C04 競爭者修改訂閱後重新預覽的原因顯示，以及 C18／C21 選用新 Pro 價格後 C03 舊報價被拒絕。這些定向證據尚未覆蓋 C01–C49 每項動作的完整重播與恢復矩陣。

## C03／C04 回應遺失與原鍵恢復

`web/admin/e2e/admin.spec.ts` 的兩個真實瀏覽器情境在伺服器已接受並完成 C03／C04 後中斷原回應。重新整理後介面以原冪等鍵取回同一命令；同鍵不同報價 ID 回 `409 IDEMPOTENCY_CONFLICT`。C03 只有一筆變更排程與成功收據。C04 只有一筆立即變更、帳單、付款操作與成功收據，訂閱服務在付款前仍為 Basic。此證據涵蓋指定兩個動作的提交後回應遺失，不代表其他 47 個動作的中斷路徑已驗完。

`TestAdminScheduledPlanReceiptFailureRollsBackAndRecoversOriginalCommand` 另在 C03 收據寫入時注入 SQLite 失敗：原命令保持 `accepted`，排程與訂閱 revision 不變，沒有新增付款或 provider capture。重啟後兩次恢復只產生一筆排程與收據；原鍵重播返回同一命令，改 payload 則衝突。

## C18 Pro 價格發布的端到端追蹤

| 面向 | 目前證據與判定 |
| --- | --- |
| 入口與執行 | `/prices/pro/new` 使用共用 `ActionForm` 建立 C18 預覽，再以原預覽及冪等鍵提交共用命令入口。`executeAdminCatalogTx` 在同一交易比對來源快照後呼叫 `publishProPriceTx`，回傳價格版本 ID、方案與 checksum；成功命令與 `admin_command_receipts` 同交易保存。 |
| 權限與輸入 | C18 需要 `catalog.publish`；共用 HTTP 權限矩陣驗未登入、CSRF 與缺能力拒絕。`canonicalAdminCatalogPayload`、領域發布路徑與管理 HTTP 驗正整數、UTC、費率分母，以及固定費用加最少一席不超出 `int64`。 |
| 價格不變性 | `admin_catalog_test.go` 與瀏覽器案例核對已發布元件、checksum、同版號或同 ID 不同內容的衝突，以及來源變動時舊預覽失效。 |
| 回應遺失與原鍵恢復 | `lost C18 publish response recovers one price and receipt with the original key` 讓管理 HTTP 完成 C18 後切斷首個回應。介面保留未確認意圖，重整後用原 request key 查回成功命令；SQLite 驗同一價格版本、命令與收據各一筆，checksum 存在且沒有新增付款操作。定向 Playwright 案例通過。 |
| 收據寫入故障 | `TestCatalogPublishReceiptFailureRecoversOriginalHTTPCommand` 令隔離 SQLite 的收據 INSERT 失敗；真實管理 HTTP 回 503 `COMMAND_PENDING_RETRY` 與原命令 ID，價格版本、元件及收據均未提交。移除故障後同鍵重送兩次只建立一筆價格、三個元件及一筆與 checksum 對應的收據；改 payload 重播回 409 `IDEMPOTENCY_CONFLICT`。定向 Go 案例以 `-count=3` 通過。 |
| 尚待驗收 | C18 的其他來源衝突交錯尚未逐一用真實 HTTP／SQLite 案例驗證；本項不據此宣稱 49 項操作整體結案。 |

## C19 計量表註冊的端到端追蹤

| 面向 | 目前證據 |
| --- | --- |
| 入口與契約 | `/meters/new` 的 React／Ant Design `ActionForm` 先送 `POST /admin/api/previews`，再以 `action_id=C19` 送 `POST /admin/api/commands`；命令可由原 ID 或冪等鍵查回。後端 `lab/admin_catalog.go` 比對預覽來源，再由 `registerMeterTx` 註冊不可變的 `meter_schemas` 列。 |
| 權限與輸入 | `catalog.publish` 為必需能力；共用 HTTP 矩陣涵蓋 401、CSRF 403、缺能力 403 與未知欄位拒絕。`canonicalAdminCatalogPayload` 要求非空 ID、source、unit 及正整數 schema 版本；`registerMeterTx` 再驗 ID／source 格式。 |
| 來源衝突 | `TestAdminMeterRegistrationRejectsStaleConflictingSchema` 先讓同 ID 不同單位各取得預覽，先執行 `token` 版本，再執行 `request` 舊命令：後者為 `failed/PREVIEW_STALE`，既有 unit 保持 `token`，兩命令只有一筆成功收據。新的衝突預覽直接被拒；成功與失敗命令各以原鍵重播，改 payload 使用舊鍵回冪等衝突。 |
| 實際介面 | `web/admin/e2e/admin.spec.ts` 的雙分頁案例先發布 `token`，舊 `request` 預覽確認回 HTTP 409／`PREVIEW_STALE`，介面明示失效且無法建立相衝突的新預覽；SQLite 只有一筆 schema、一筆成功收據及一筆失敗命令。另一案例以註冊的 meter 發布價格、建立報價並記錄用量。 |

此處直接證明 C19 的同 ID 競爭與恢復；其餘非法字元、長度、正整數邊界及跨 actor 預覽仍以共用矩陣與單項 payload 測試為準，A30 尚未簽核。

## C20 計量價格發布的端到端追蹤

| 面向 | 目前證據 |
| --- | --- |
| 入口與契約 | `/prices/metered/new` 的 React／Ant Design 表單先取得 C20 預覽，再送共用命令入口；`lab/admin_catalog.go` 把 meter schema、既有 checksum、發布狀態及同方案版號占用者放進來源快照，執行時重算。`publishMeteredPriceTx` 在交易內建立版本與價格元件。 |
| 權限與輸入 | `catalog.publish` 為必需能力；共用 HTTP 矩陣涵蓋 session、CSRF、能力及未知欄位。既有瀏覽器案例逐欄驗固定金額、席次金額、包含用量與費率分子／分母的非法數值邊界；價格必須參照已註冊 meter。 |
| 版本不可變 | `TestAdminMeteredPriceRejectsStaleConflictingVersion` 先接受兩個同 ID 不同金額的預覽命令，3500 先發布後，3000 的舊命令為 `failed/PREVIEW_STALE`；版本維持 3500、兩個價格元件及一筆成功收據。成功與失敗命令各沿原鍵找回，改 payload 使用成功鍵回冪等衝突，新的相衝突預覽被拒。另驗不同 ID 競爭同一方案版號時只保留一個版本和一筆成功收據。 |
| 實際介面 | `web/admin/e2e/admin.spec.ts` 的雙分頁案例先發布 3500，再確認舊 3000 預覽，回 HTTP 409／`PREVIEW_STALE`；介面顯示失效並無法建立相衝突的新預覽，SQLite 保持 3500、兩個元件、一筆成功收據及一筆失敗命令。既有 AI 計量案例從 C19／C20／C21 走到報價、接受及 token 用量。 |

這些案例驗同版本競爭，不推論所有日期、貨幣數值及跨 actor 預覽邊界已完成；A30 仍待逐項簽核。

## C21 目錄選價的端到端追蹤

| 面向 | 目前證據 |
| --- | --- |
| 入口與契約 | `/catalog-selections/new` 的 React／Ant Design 表單先預覽 C21，再送共用命令入口；來源快照包含價格版本的方案、發布狀態、checksum、有效期間及該方案／cohort／時點的既有占用者。執行前重算來源，最終寫入 `catalog_selection`。 |
| 權限與相容性 | `catalog.publish` 為必需能力；共用 HTTP 入場矩陣覆蓋 session、CSRF、能力及未知欄位。`api/v1_test.go` 驗新計量 SKU 選價後，舊 client 因缺能力宣告而拒絕接受報價，具備能力的 client 才能接受。 |
| 同時點競爭與重播 | `TestAdminCatalogSelectionRejectsStaleCompetingPrice` 在同方案、cohort、生效時點先接受兩個不同價格的預覽命令，先執行者保留唯一選價，後者為 `failed/PREVIEW_STALE` 且無成功收據。兩個原鍵各找回原命令，改 payload 使用成功鍵回冪等衝突；已有不同價格占用該時點時，新預覽被拒。 |
| 實際介面 | `web/admin/e2e/admin.spec.ts` 的雙分頁案例先選價格 B，再確認較早的價格 A 預覽：HTTP 409／`PREVIEW_STALE` 可見，介面要求重新預覽且相衝突的新預覽失敗；SQLite 只有 B 一筆選價、一筆成功收據與一筆失敗命令。其他案例驗同時點改選回舊價被拒，以及以新選價接受 AI 計量報價。 |

這些案例直接驗 C21 的同時點爭用；不同時點的價格階梯與跨 actor 預覽仍須按適用契約核對，A30 尚未簽核。

## S07 跨動作金融正確性比對

`lab/admin_s07_parity_test.go` 在兩組隔離 commerce／provider SQLite 中，以同一業務時鐘分別執行直接領域入口與管理命令。它逐階段比對 C04 的期中升級金額與原 Basic 價格、C49 指定原付款的假服務商回應遺失、C09 待查證且沒有成功收據、C10 查證與原 C09 收據恢復。兩天後開通只須 C13 更正 534，帳單義務與淨實收同為 3466，並且只有一筆 534 funded grant。

另一情境在帳期邊界先用 C44 確認未知付款阻止續約；C10 查回晚到收款後，升級轉為 `needs_review`，C14 將 4000 義務與淨實收釋出並只留一筆 4000 funded grant，之後的 C44 才建立 2000 Basic 續約帳單。兩條路徑都核對 provider capture 總數維持兩筆（原 Basic 與升級付款）、每個成功管理命令一筆收據、待查證命令零成功收據，以及帳單原額／義務／總實收／釋出／淨實收／未清額。這是上述命令在 S07 組合下的直接證據，不代替每個命令的完整輸入與中斷矩陣。

## C07 建立付款操作的競爭與恢復追蹤

- **入口與授權：**`/invoices/:id/payments/new` 透過共用預覽及命令 API 提交 `C07`；能力為 `finance.adjust`。共用 HTTP 矩陣驗 session、CSRF 與能力守衛，`admin_payment_test.go` 驗正常建立及收據失敗後沿原鍵恢復。
- **來源與執行：**`adminCreatePaymentPreview` 將帳單應收、帳期／到期界線、既有付款操作狀態與指定金額綁入預覽；`executeCreatePaymentTx` 在交易內重算來源。來源變動使舊命令 `failed/PREVIEW_STALE`，不能沿原預覽建立第二筆替代付款。
- **競爭證據：**`TestAdminCompetingPaymentPreviewsKeepSingleReplacementOperation` 先受理兩個不同金額的 C07，再執行 7000 勝出與 6000 失效。SQLite 只有原 10000 操作（已取消）及新 7000 操作，舊 outbox 完成、勝出命令有一筆收據、失效命令沒有成功收據；兩個原鍵各自重播原結果，改 payload 均衝突，provider 零 capture。
- **畫面證據：**`web/admin/e2e/admin.spec.ts` 的雙分頁案例先預覽 1000／1500，再提交 1500；舊分頁確認回 HTTP 409，顯示預覽失效及目前無法建立新預覽。SQLite 只有原操作與 1500 替代操作、C07 一成功一 `PREVIEW_STALE`；原與新 provider key 都尚無 capture。定向 Playwright 1／1 通過。
- **跨實例證據：**`TestAdminCompetingPaymentCommandsAcrossLabInstances` 以兩個獨立 Lab 實例、各自的 SQLite 連線及同一組資料庫檔同時執行已受理的 C07；修正 lease 與命令交易的先讀後寫鎖衝突後，連跑 10 次均為一成功、一 `PREVIEW_STALE`，僅一筆替代操作與收據。修正前測試重現 `database is locked` 且命令留在 `accepted`；定向 `-race` 通過。
- **多進程證據：**`TestAdminCompetingPaymentCommandsAcrossProcesses` 啟動兩個獨立測試子進程，各自開啟相同 SQLite 檔並等待共同起跑訊號；連跑 10 次均為一成功、一 `PREVIEW_STALE`，僅一筆替代操作與成功收據，原操作取消且 provider 零 capture。
- **限制：**並行建立後的 C09 派送、C07 全部非法 payload／UI 錯誤矩陣仍待驗；C07 尚未單項簽核。

## C08 確定失敗付款的競爭預覽

- **入口與來源：**`/payments/:id/retry` 透過共用預覽及命令 API 提交 `C08`，能力為 `finance.adjust`。預覽只接受確定失敗的原操作，依當前未清餘額建立新義務；`admin_payment_test.go` 已驗正常重試和取代未送出操作後的重試。
- **競爭證據：**`TestAdminCompetingRetryPreviewsKeepSingleNewObligation` 對同一確定失敗操作受理兩個 C08 預覽。先執行命令建立一筆 10000 重試義務及成功收據，後執行命令變為 `failed/PREVIEW_STALE` 且無成功收據；兩個鍵都重播原結果。SQLite 只有原失敗操作與一筆新操作、單筆 retry request；新 provider key 未被派送，也沒有成功 capture。
- **跨實例證據：**`TestAdminCompetingRetryCommandsAcrossLabInstances` 以兩個獨立 Lab 實例共用 SQLite 檔同時執行 C08；連跑 10 次均只建立一筆 retry request、新義務與成功收據，另一命令 `PREVIEW_STALE`。新 provider key 尚未派送。
- **多進程證據：**`TestAdminCompetingRetryCommandsAcrossProcesses` 以兩個獨立測試子進程及共同起跑訊號同時執行已受理 C08；連跑 10 次均只保留一筆 retry request、一筆新義務與成功收據，敗方 `PREVIEW_STALE`，新 provider key 尚未派送。
- **限制：**雙分頁競爭、C09 派送及其餘錯誤／權限矩陣仍待逐項核對，C08 尚未單項簽核。

## C09 指定付款操作派送的端到端追蹤

| 驗收欄位 | 目前可重跑的證據 |
| --- | --- |
| Action／UI route | C09；`/payments/:id/dispatch`，只對畫面所列的 `operation_id` 建立預覽與提交命令。 |
| HTTP handler／授權 | `api/admin/session.go` 的 `protectedWrite` 連到 `api/admin/previews.go` 的 `createPreview` 與 `api/admin/commands.go` 的 `submitCommand`；`api/admin/permissions.go` 要求 `finance.adjust`。`api/admin/action_http_matrix_test.go` 逐動作驗 session、CSRF、缺能力及未知 payload 拒絕。 |
| 預覽／來源守衛 | `lab/admin_external.go` 的 `loadExternalDispatchSnapshot` 只接受 `created`、pending capture outbox、正金額及合法帳期；快照綁定 operation、invoice、provider key、金額、幣別、狀態和帳期。`adminExternalPreviewCurrent` 在首次派送前重驗；被 C07 取消的舊操作會使 C09 `failed/PREVIEW_STALE`，不留下等待查證命令。 |
| Domain helper／外部效果 | `adminExecuteExternalCommand` 固定傳入目標操作給 `dispatchCaptureAt`；`adminMarkSubmitted` 在服務商呼叫前持久化 submitted。結果未知時保留 `waiting_verification`，沿原 provider key 查證。服務商觀察由 `applyObservationAt` 交易入庫，與 C07 競爭時先取得 SQLite 寫入權。 |
| 收據／恢復 | 只有終態由 `adminFinishExternalCommand` 寫入唯一 `admin_command_receipts` 並標記 succeeded；失效命令無成功收據。既有 `admin_external_test.go`、`admin_lost_response_parity_test.go` 與 `admin_crash_webhook_parity_test.go` 驗查證與中斷恢復；`admin_payment_dispatch_interleaving_test.go` 驗雙實例建立／派送競爭、金額、幣別、provider capture 和收據。 |
| Browser／SQLite | `web/admin/e2e/admin.spec.ts` 新增雙分頁舊 C09 被 C07 取代後收到 HTTP 409、失效提示、舊 key 零 capture，僅新操作收 1500；另一案例在兩筆 pending outbox 中先派送較晚的指定操作，較早者仍 `created` 且零 capture。兩個新案例各自定向 Playwright 1／1，加入後完整套件 92／92 通過。 |
| 尚未簽核 | 已納入完整瀏覽器回歸 92／92；A30 其他動作仍需同粒度證據矩陣。 |

## C10 原付款操作查證的端到端追蹤

| 驗收欄位 | 目前可重跑的證據 |
| --- | --- |
| Action／UI route | C10；`/payments/:id/reconcile`。目標是原 `operation_id`；payload 為 `{}`，依 `lab/admin_commands.go` 不需要預覽。 |
| HTTP handler／授權 | 共用 `api/admin/commands.go` 的 `submitCommand`、`GET /admin/api/commands/{id}` 與 `POST /admin/api/commands/{id}/resume`；`api/admin/permissions.go` 要求 `finance.adjust`。`api/admin/action_http_matrix_test.go` 對 C10 驗 session、CSRF、能力及未知欄位。 |
| 來源與結果守衛 | `lab/admin_external.go` 對 C10 呼叫 `ReconcilePayment(ctx, targetID)`；`lab/lab.go` 用原 provider key 做 `Lookup`，找到服務商事實時才交由 `applyObservation` 入庫。沒有證據或非終態時命令維持 `waiting_verification`，不得推斷收款成功；不存在的操作回 `DOMAIN_REJECTED`。 |
| 收據／冪等恢復 | 終態由 `adminFinishExternalCommand` 產生單一成功收據。`lab/admin_reconcile_no_evidence_test.go` 驗無 provider 事實時沒有成功收據；`lab/admin_external_test.go` 驗取得證據後同一命令完成。C10 不新增 capture，重試依原 operation/provider key 查證。 |
| Browser／SQLite | `web/admin/e2e/admin.spec.ts` 的 `reconcile keeps submitted payment waiting until provider evidence appears` 先驗 `waiting_verification` 且零成功收據，補入 provider 事實後按「重新查證」，原命令變 succeeded、只有一筆收據及原 key 一筆 capture。另有回應遺失後從命令頁找回原操作的瀏覽器案例。 |
| 尚未簽核 | A30 仍需其餘動作同粒度的 HTTP、來源守衛及瀏覽器證據。 |

## C11 帳單減額的端到端追蹤

| 驗收欄位 | 已核對的路徑與證據 |
| --- | --- |
| Endpoint、UI、handler | `POST /admin/api/previews` 與 `POST /admin/api/commands` 使用 `action_id=C11`；`GET /admin/api/commands/{id}` 找回原命令。`web/admin/src/App.tsx` 的 `/invoices/:id/reductions/new` 使用 `ActionForm` 輸入減額與理由；`api/admin/session.go`、`api/admin/commands.go` 接入管理命令。 |
| 分派、領域 helper | `lab/admin_previews.go` 分派 C11 到 `adminCreateReductionPreview`；`lab/admin_commands.go` 分派 C11 到 `executePostReductionTx`，再於同一 SQLite 交易內呼叫 `postReductionTx`。結果包含更正 ID、減額前後義務及實際 grant IDs。 |
| 來源守衛、預覽 policy | R 類預覽綁定 actor、動作、帳單、payload、期限及單次 claim。`loadReductionSnapshot` 拒絕已送出／未知付款、無效金額、減至零及已抵扣 Credit 的帳單；來源 JSON 綁定原義務、已收款、已抵扣、付款操作與 allocation 狀態。執行交易內重算與比對，還核對實際減額前後義務與預覽一致，變動回 `PREVIEW_STALE`。 |
| Permission | `api/admin/permissions.go` 映射 `finance.adjust`；共用 HTTP 與權限矩陣驗無 session、缺 CSRF、缺能力及未知欄位的入場守衛。 |
| 收據、恢復 | 更正、可能產生的 funded grant、舊未送出收款取消與成功收據在同一命令交易；同鍵重播返回原更正與收據。預覽失效須重新顯示來源及金額差異、再次確認，不能自動套用舊金額。 |
| 實測 | `lab/admin_reduction_test.go` 驗已收款減額的精確 grant／收據／重播，以及未收款減額取消舊收款、只收剩餘額；`lab/admin_financial_parity_test.go` 與直接領域路徑比對部分付款及已收款減額；`web/admin/e2e/admin.spec.ts` 驗雙分頁更正使舊預覽 409、顯示差異、重新確認後沒有重複更正，並追查來源歷史。新增 C11 受控 422 案例驗表單與焦點恢復、錯誤關聯且零命令；另一案例在伺服器提交後切斷回應，重整後沿原鍵找回唯一更正、funded grant 與收據。 |

## C12 抵扣 Credit 的端到端追蹤

| 驗收欄位 | 已核對的路徑與證據 |
| --- | --- |
| Endpoint、UI、handler | `POST /admin/api/previews` 與 `POST /admin/api/commands` 使用 `action_id=C12`；`GET /admin/api/commands/{id}` 找回原命令。`web/admin/src/App.tsx` 的 `/credits/:id/apply` 使用 `ActionForm` 輸入目標帳單 ID 與正整數金額；`api/admin/session.go`、`api/admin/commands.go` 接入管理命令。 |
| 分派、領域 helper | `lab/admin_previews.go` 分派 C12 到 `adminCreateApplyCreditPreview`；`lab/admin_commands.go` 分派 C12 到 `executeApplyCreditTx`，再於同一 SQLite 交易內呼叫 `applyCreditTx`。成功寫入 `credit_applications`、取消目標帳單尚未送出的舊收款操作及 outbox，剩餘應付需另建立正確金額的付款。 |
| 來源守衛、預覽 policy | R 類預覽綁定 actor、動作、grant、payload、期限及單次 claim。`loadApplyCreditSnapshot` 拒絕不同客戶、原來源帳單、非續期帳期、超過寬限、已送出／未知付款、貨幣不符或超額抵扣；來源 JSON 綁定 grant 各餘額、目標帳單未清額、帳期／到期、既有付款操作狀態及貨幣。執行交易內重算並比對來源，變動回 `PREVIEW_STALE`。 |
| Permission | `api/admin/permissions.go` 映射 `finance.adjust`；共用 HTTP 與權限矩陣驗無 session、缺 CSRF、缺能力及未知欄位的入場守衛。 |
| 收據、恢復 | 抵扣、舊付款取消與成功收據在同一命令交易；失效命令無抵扣及成功收據，原鍵重播保持 `failed/PREVIEW_STALE`。UI 重新顯示來源／影響差異並要求新預覽與再次確認；不存在自動建立剩餘付款的隱含動作。 |
| 實測 | `lab/admin_apply_credit_test.go` 驗成功收據、來源變動、原鍵重播及只收剩餘額；`lab/admin_refund_budget_parity_test.go` 驗 C12 與 C15 共用 funded grant 預算；`web/admin/e2e/admin.spec.ts` 驗雙分頁退款先占額使舊抵扣預覽 409、重新確認後僅抵扣一次，及舊 2000 收款被取消、僅新 1500 收款送往 provider。 |

## C15 預留退款的端到端追蹤

| 驗收欄位 | 已核對的路徑與證據 |
| --- | --- |
| Endpoint、UI、handler | `POST /admin/api/previews` 與 `POST /admin/api/commands` 使用 `action_id=C15`；`GET /admin/api/commands/{id}` 找回原命令。`web/admin/src/App.tsx` 的 `/credits/:id/refunds/new` 使用 `ActionForm`，要求正整數最小貨幣單位、預覽與二次確認；`api/admin/session.go`、`api/admin/commands.go` 進入 `lab.AdminSubmitCommand`。 |
| 分派、領域 helper | `lab/admin_previews.go` 分派 C15 預覽到 `lab/admin_reserve_refund.go`；`lab/admin_commands.go` 分派 C15 執行到 `executeReserveRefundTx`，再於同一 SQLite 交易內呼叫 `reserveRefundTx`。領域函式拒絕超出 funded grant 可用餘額的預留，並留下退款操作與 audit event。 |
| 來源守衛、預覽 policy | R 類預覽綁定 actor、動作、grant、payload、期限及單次 claim。`loadReserveRefundSnapshot` 記錄來源付款操作／帳單、付款成功狀態、授予／已用／預留／已退／可用額度及貨幣；執行交易內重算並比對完整來源 JSON。即使另一筆 500 預留後還剩 500 可用，舊 500 預覽仍回 `PREVIEW_STALE`。 |
| Permission | `api/admin/permissions.go` 映射 `finance.adjust`；`api/admin/action_http_matrix_test.go` 與 `permissions_test.go` 驗未登入、缺 CSRF、缺能力、未知 payload 欄位及權限映射。這些 HTTP 測試只證明入場守衛。 |
| 收據、恢復 | 成功退款預留與 `admin_command_receipts` 在同一交易提交；預留本身不呼叫 provider，後續送出與查證屬 C16／C17。失效命令保留 `failed/PREVIEW_STALE` 且無成功收據，原冪等鍵重播仍返回原失敗命令；新預覽、新鍵、再次確認才可建立第二筆。`ActionForm` 顯示來源與金額變化並要求再確認。 |
| 實測 | `lab/admin_reserve_refund_test.go` 驗成功收據、來源變動後失效、成功與失敗鍵重播、零額外退款及重新確認後總額；`lab/admin_refund_budget_parity_test.go` 與直接領域路徑比對；`web/admin/e2e/admin.spec.ts` 驗真實 SQLite 雙分頁 409、重建預覽、兩筆退款合計 1000 與兩筆收據。 |

C15 的預覽競爭與本地交易收據已有直接證據。A30 仍須按相同粒度核對其餘 C01–C49，並完成 A01–A29 剩餘條件；不能由 C15 的結果推論其他動作已簽核。

## C16 指定退款操作派送的端到端追蹤

| 驗收欄位 | 目前可重跑的證據 |
| --- | --- |
| Action／UI route | C16；`/refunds/:id/dispatch`。預覽與命令由共用 API 提交，payload 為 `{}`，目標 refund ID 必填。 |
| HTTP handler／授權 | `api/admin/previews.go` 的 `createPreview` 與 `api/admin/commands.go` 的 `submitCommand` 由 `protectedWrite` 保護；`api/admin/permissions.go` 要求 `finance.adjust`。共用 HTTP 矩陣逐動作驗 session、CSRF、缺能力和未知欄位。 |
| 預覽／來源守衛 | `lab/admin_external.go` 的 `loadExternalDispatchSnapshot` 僅接受 `created`、pending refund outbox 及正金額，綁定 grant、退款 provider key、原收款 provider key、金額、幣別和狀態；`adminExternalPreviewCurrent` 在首次派送前重驗，來源變動時不得沿舊預覽執行。 |
| Domain helper／外部效果 | `adminExecuteExternalCommand` 將固定 refund ID 傳給 `dispatchRefundAt`，在服務商呼叫前以 `adminMarkSubmitted` 持久化 submitted；結果未知時保留 `waiting_verification`。`applyRefundObservationAt` 交易先預留 SQLite writer，再讀取退款／inbox 狀態並提交服務商證據。 |
| 收據／恢復 | 終態由 `adminFinishExternalCommand` 寫入唯一成功收據；待查證不算已完成。`admin_external_test.go` 的 `TestAdminRefundDispatchTargetsExactRefund` 驗指定 ID 而非下一筆佇列項目；退款 UNKNOWN 的 C17 查證及原 C16 恢復另有 `admin_refund_reconcile_test.go` 與瀏覽器案例。 |
| 跨實例金融證據 | `TestAdminRefundReservationAndDispatchCompeteAcrossLabInstances` 以兩個 Lab 共用 SQLite 同時執行 C15 與 C16。修正前重現 C16 `database is locked`；修正後連跑 5 輪共 30 個子案例及定向 `-race -count=2` 共 12 個案例通過。最新退款程式另以瀏覽器 `funded reduction reserves and refunds exactly once after a lost response` 定向 1／1 驗證。每輪原退款僅一筆 USD 400 provider 事實；第二筆預留依來源先後成功或 `PREVIEW_STALE`，funded 額度與收據數一致。 |
| 尚未簽核 | A16 其他退款 UI 錯誤矩陣及 A30 其餘動作仍需逐項證據。 |

## C15 收據寫入故障的 HTTP 恢復

`api/admin/write_failure_test.go` 的 `TestRefundReservationReceiptFailureRetainsBudgetAndOriginalHTTPCommand` 在 C15 命令受理後對收據 INSERT 注入 SQLite 故障。首次 HTTP 回 503 `COMMAND_PENDING_RETRY` 與原命令 ID；Credit 仍有 1000 可用，退款預留、退款操作、成功收據與 provider 退款皆為零。解除故障後同鍵重送兩次，兩次均返回同一成功命令；Credit 恰預留 500，退款操作維持 `created`、收據一筆、provider 退款仍為零。這與 `lab/admin_reserve_refund_test.go` 的交易回滾測試合併覆蓋 domain 與 HTTP 邊界。

## C17 原退款操作查證的端到端追蹤

| 驗收欄位 | 目前可重跑的證據 |
| --- | --- |
| Action／UI route | C17；`/refunds/:id/reconcile`，目標是原 refund ID。payload 為 `{}`，依 `lab/admin_commands.go` 不需要預覽。 |
| HTTP handler／授權 | 共用 `submitCommand`、命令詳情與原命令 resume 路由；`api/admin/permissions.go` 要求 `finance.adjust`。`api/admin/action_http_matrix_test.go` 驗 C17 的 session、CSRF、能力與未知欄位。 |
| 來源與查證 | `lab/admin_external.go` 呼叫 `ReconcileRefund(ctx, targetID)`；`lab/refunds.go` 以原退款 provider key 查詢服務商，找到匹配的金額、幣別及原收款 key 後才由 `applyRefundObservationAt` 入庫。無終態證據時維持 `waiting_verification`；C17 只查證，不新增第二筆 provider refund。 |
| 收據／恢復 | `adminFinishExternalCommand` 僅在終態產生單一成功收據。`TestAdminReconcileRefundWaitsForTerminalProviderEvidence` 驗無證據時沒有成功收據；`TestAdminRefundReconcileResolvesUnknownWithoutSecondRefund` 驗 UNKNOWN 查證、原鍵重播及唯一 provider 退款。 |
| Browser／SQLite | `web/admin/e2e/admin.spec.ts` 的 `funded reduction reserves and refunds exactly once after a lost response` 從原 C16 `waiting_verification` 進入 C17，確認退款成功後再恢復原 C16；SQLite 核對 provider 只有一筆退款、查證與派送命令各一筆收據。 |
| 尚未簽核 | A30 其他動作仍需同粒度的來源守衛、HTTP 與瀏覽器證據。 |

`api/admin/provider_lookup_failure_test.go` 的 `TestUnavailableRefundLookupKeepsReservationAndOriginalHTTPCommand` 補真 HTTP C17：服務商資料庫被鎖住時回 503 `COMMAND_PENDING_RETRY` 與原命令 ID；退款仍為 `unknown`，Credit 的 500 預留保持、已退金額為零，沒有成功收據。解除故障後原鍵重送兩次，均回同一成功命令；預留轉為已退 500，Credit 可用額仍為 500，provider 只有一筆退款、命令只有一筆收據。定向測試連跑三次通過。這補足 provider 暫不可查時 HTTP 回應與資金保留的一致性；其他 C17 故障組合仍待逐項驗收。

## A16 Credit 額度查詢與退款 UI 證據

- `GET /admin/api/credits/{id}` 由 session 守衛；`lab.AdminCreditDetail` 在同一個 SQLite 唯讀快照內取得 grant 來源與 `CreditBalance`。`api/admin/credit_detail_test.go` 驗未登入 401 與不存在 404；`lab/admin_credit_detail_test.go` 驗原始、已抵扣、保留、已退款與可用額度的轉換，包含退款回應遺失時仍保留原額。
- `/admin/credits/:id` 用 Ant Design 將五種額度分開顯示並提供來源帳單、抵扣、預留與按 grant 篩選的退款入口；定向瀏覽器案例驗從列表進入、預留後及成功退款後的金額。另一個案例先建立兩筆 pending 退款，再從 C16 介面指定較晚的操作；較早者仍為 `created`、outbox `pending`、provider 零退款，指定者只退一筆且原收款 key 與金額正確。兩個新案例各自定向 Playwright 1／1 通過。
- 完整瀏覽器回歸及 A30 其他動作的同粒度盤點仍待完成。

## C11、C13、C14 更正來源驗收補充

帳單詳情從既有 request key 與資料列關聯出更正來源。`admin_reduction_test.go` 驗 C11 更正指向原管理命令；`admin_change_corrections_test.go` 驗 C13 更正指向立即升級變更；`admin_unfulfilled_test.go` 驗 C14 更正指向未履行升級變更。C11、C13、C14 的同 key 重播測試核對更正不重複，C11／C14 的 Credit 各仍一筆，C13 不納入後來合格的項目。Playwright 另驗 C11 原命令入口、C13 延遲更正的變更 ID 與金額，以及 C14 跨帳期後查證、決議、帳單更正與來源 Credit 的詳情及歷史頁。無法驗證關聯的更正標為其他領域操作並保留原 request key，不推定為管理命令。

`TestAdminReductionCancelsOldCollectionAndCapturesOnlyRemainder` 與新 Playwright 案例另驗未收款的第一期帳單：C11 預覽列出將取消的舊付款操作與減額後應付 1500；減額 500 後，原 2000 的付款與 outbox 取消，未產生可退 Credit。操作員由 C07 建立 1500 新付款、C09 派送後，fake provider 只有新 key 的 1500 capture，原 key 沒有收款，訂閱開通且帳單餘額歸零。C11／C12 的收款安排提示共用同一介面元件；兩項瀏覽器案例已分別定向通過。

## C13、C14、C30、C32、C44、C45 回應恢復

批次回應遺失的可重跑證據位於 `web/admin/e2e/admin.spec.ts`：C13、C30、C32、C44、C45 都先讓原命令在 SQLite 完成，再中斷瀏覽器回應，重新載入後沿原 request key 重播。測試逐項核對工作、成員及各自的財務結果；C13 檢查帳單更正，C30 檢查 Credit Note，C32 檢查 capture outbox，C44 檢查續約帳單與付款義務，C45 檢查固定的權益刷新成員。這只簽核該種回應遺失與重播情境。

C14 另有同類型的非批次瀏覽器案例：未履行升級決議建立更正與 Credit 後中斷回應，頁面重新載入並沿原 key 查回，命令、收據、決議與 Credit 各只有一筆。此案例定向通過，未納入先前 63 項完整回歸。

Go 故障注入另覆蓋兩種不同的收據失敗邊界：`TestAdminResolveUnfulfilledRollsBackFinancialFactsWhenReceiptFails` 驗 C14 的財務資料與收據整體回滾，重啟後才一次完成；`TestAdminChangeCorrectionBatchResumesCommittedItemAfterReceiptFailure` 驗 C13 已提交的逐項更正在最終收據失敗時保留，重啟後只完成原工作與收據。兩者都核對 Credit／更正不重複，provider capture 數不增加。

## C12 瀏覽器驗收補充

`web/admin/e2e/admin.spec.ts` 的 C12 案例以已收款帳單減額產生 1000 的 Credit，設定實驗時鐘並由續期批次建立同客戶的下一帳期帳單。操作員從抵扣頁輸入目標帳單與 500 金額，檢查預覽後確認，重新載入命令詳情仍可查到成功結果。隔離 SQLite 核對抵扣來源、目標帳單、`admin:` request key、唯一抵扣紀錄及一筆命令收據；帳單詳情顯示已抵扣 500 與剩餘應付。超過可用 1000 額度的預覽被拒絕，且未建立 C12 命令。`lab/admin_apply_credit_test.go` 另驗預覽後退款預留改變額度來源時，C12 命令以 `PREVIEW_STALE` 失敗，不新增抵扣或收據。不同客戶、過期帳期與並發爭用的瀏覽器案例仍待驗。

同檔的 `TestAdminApplyCreditCancelsOldCollectionAndCapturesOnlyRemainder` 延伸驗證抵扣後的收款：原續期帳單為 2000，抵扣 500 時舊 `created` 付款操作與 capture outbox 一同取消，舊 provider key 沒有 capture；再由 C07 建立 1500 的新付款操作，只向 fake provider capture 1500，帳單餘額歸零。這證明該路徑不會把抵扣前的 2000 送去收款，但不代表其他並行情境已全部驗畢。

同一條路徑現也由 Playwright 驗證：C12 預覽明示取消一筆未送出的付款操作、抵扣後應付金額及建立新付款的提示；操作後沿 UI 進入 C07 建立剩餘付款，再用 C09 派送。SQLite 與 fake provider 分別核對舊操作已取消、舊 provider key 沒有 capture、新 key 只 capture 剩餘 1500，帳單詳情顯示 USD 0.00 尚待支付。此路徑已納入最近一次 70 項完整瀏覽器回歸。

C12 的瀏覽器案例進一步在預覽後，於同一登入 session 的另一分頁執行 C15 預留退款 500。舊 C12 預覽確認回 409 `PREVIEW_STALE`，沒有抵扣或命令收據；介面顯示 `grant_reserved_minor` 來源變動，保留原 500 意圖並要求再次確認。新預覽確認後，C15 預留 500 加 C12 抵扣 500 正好等於原 Credit 1000，兩筆用途沒有超額或重複。此情境定向 1／1 通過，並已納入最近一次 76／76 完整瀏覽器回歸。

`TestAdminApplyCreditUsesGrantAndInvoiceBalanceAtomically` 另核對原 C12 失敗命令沿同一 request key 重播仍是同一筆 `PREVIEW_STALE`，且不會寫入新的抵扣或收據。

## C45 批次成員瀏覽器驗收補充

`web/admin/e2e/admin.spec.ts` 的 C45 案例先建立已付款訂閱，透過 UI 建立權益刷新預覽並讀取其固定成員快照，再於另一個分頁建立新的合格訂閱。確認原預覽後，SQLite 工作項目數與快照相同，包含原訂閱而不包含新訂閱；工作詳情頁在重新整理後仍顯示完整進度。另一個頁面案例以受控 API 回應顯示一筆成功及一筆待查證，確認進度為 1/2、兩種狀態分列，且逐項錯誤碼可見。此案例只驗 C45 的成員固定性及批次頁面的狀態呈現，尚未覆蓋五種批次的逐項中斷與重啟 UI 矩陣。

## C34 瀏覽器驗收補充

`web/admin/e2e/admin.spec.ts` 的缺失權益投影案例，使用本機假服務商與隔離 SQLite 建立已付款訂閱，故意移除權益投影，再走 C33 對帳及 C34 詳情頁、預覽、確認、重新整理。測試核對 `SAFE_AUTO_REPAIR`、來源 revision 與證據預填、唯一 `repair_operations` 與命令收據、原 expected／actual 快照，以及修復後 `verified` 與後續對帳的 verification。另有來源 revision 在預覽後改變的 browser 案例：修復結果為 `blocked`、權益未重建、命令只有一筆收據，UI 明示 blocked verification。服務商收款金額不符的 browser 案例另驗 `MANUAL_REVIEW`：C34 被 blocked、C35 留下人工決議，provider 原金額與收款總筆數不變。其他分類及 blocked 原因仍待逐項 browser 驗收。

## C46–C49 實驗控制的現有證據

| 動作 | 已驗證的行為 | 尚需補齊 |
| --- | --- | --- |
| C46 | `TestAdminClockPersistsAndControlsRunningDomainClock` 驗時鐘設定、重啟載入、revision 使舊預覽失效；瀏覽器案例驗 C09 等待查證期間前進一天不會自動重收款，原命令仍使用受理時的業務時間。 | 與其他進行中命令交錯、各種時間邊界及錯誤呈現的逐項矩陣。 |
| C47 | `TestAdminPaymentDecisionRecoversAfterProviderCommitAndCapture` 驗 provider 決策先提交、付款後續完成、重啟沿原控制收據完成管理命令，capture 與收據各唯一；`TestAdminConflictingProviderDecisionsPreserveFirstOutcome` 驗相反結果命令失敗、第一個決策仍造成唯一成功收款、終態後新命令亦失敗；瀏覽器顯示第二個決策 `DOMAIN_REJECTED`、僅首命令有收據，並走過確定失敗後重試。 | 權限撤銷交錯與其餘 UI 錯誤呈現矩陣。 |
| C48 | `TestAdminRefundDecisionRecoversAfterProviderCommitAndDispatch` 驗 provider 決策先提交、退款後續派送、重啟沿原控制收據完成管理命令；`TestAdminConflictingProviderDecisionsPreserveFirstOutcome` 驗相反結果命令失敗、第一個決策仍造成唯一成功退款、終態後新命令亦失敗；瀏覽器顯示第二個決策 `DOMAIN_REJECTED`、僅首命令有收據且派送前無 provider 退款，另驗回應遺失後 C17 查證與唯一退款。 | 權限撤銷交錯與其餘 UI 錯誤呈現矩陣。 |
| C49 | `TestAdminFaultTicketOnlyAffectsItsSelectedPayment` 驗指定付款票據不被另一筆付款誤領；`TestAdminDuplicateFaultTicketFailsWithoutStrandingCommand` 驗第二張票據明確失敗、原鍵重播與第一張票據仍可派送；`TestAdminClaimedFaultSurvivesCrashBeforeProviderDispatch` 在付款及退款各固定領票後、provider 呼叫前的持久化狀態，重啟後原命令仍進入待查證，查證後 provider 事實與命令收據各唯一。`TestAdminRevokedDispatchReleasesClaimedFaultBeforeProviderCall` 與 `TestAdminRevokedRefundDispatchRetainsReservationAndFault` 驗領票後、provider 呼叫前撤銷原命令，不產生外部效果或成功收據；票據釋放後由新命令領取，退款預留額保持 500。`TestAdminClaimedFaultBlocksCompetingDispatchUntilOwnerResumes` 驗同一付款的第二個命令不得繞過已綁定票據直接收款，啟動恢復先略過競爭命令、讓原命令查證，兩命令最後共用唯一 capture；`TestAdminStalePreviewReleasesClaimedFault` 驗舊來源快照失效後釋票、零收款與新命令可再領票。瀏覽器從建票頁顯示第二張失敗與 `DOMAIN_REJECTED`，確認第一張仍可派送，另驗付款及退款的 `lost_response`／`crash_after_provider` 恢復。 | 真正跨程序同時接管及不同故障模式的完整組合。 |

這些案例是 C46–C49 的定向證據；A25 與 A30 仍需完成上列缺口和共通矩陣，不能視為四個動作已全面簽核。

## 尚不能簽核的項目

1. [驗收計畫](../design/08-web-admin-test-plan.zh-TW.md)要求的每動作合法／非法 payload、同鍵重播與衝突、相同對象重播、預覽失效與來源變動、UI 成功／錯誤呈現，尚未形成 49 列逐項結果。既有交易測試只能證明其明示斷言，不能代替整張矩陣。
2. 現有單一管理員永遠使用 `local-admin` actor。跨 actor 預覽無法透過目前登入介面操作，故 HTTP 瀏覽器矩陣不適用；`lab/admin_commands_test.go` 對 C02、`lab/admin_preview_admission_test.go` 對 C07 使用另一 actor ID 驗預覽隔離。共用受理守衛涵蓋全部 R 類動作，但來源事實與執行時重驗仍須逐項驗。缺能力可用合成 session 測試。
3. 逐資源 GET 的 filter、游標、未知來源值和插入時跨頁矩陣仍未全部完成。`api/admin/resource_pagination_insertion_test.go` 已由真 HTTP handler 驗證 customer_id 篩選下的報價、訂閱 rowid 游標，以及客戶 ID 游標在翻頁中插入資料的行為；報價列表另有載入首頁後新增資料的定向瀏覽器證據。這些證據不涵蓋其餘資源。A30 也依賴 A01–A29 的剩餘條件，尤其金融中斷、批次恢復、輸入邊界和可存取性；因此整體狀態保持「部分」。

## request ID 稽核關聯補驗（2026-09-28）

`web/admin/e2e/admin.spec.ts` 真登入案例先核對未登入 401 JSON 的 `request_id` 等於 `X-Request-ID`，且不採用客戶端偽造的 header；再建立 C01 報價，核對回應 header、`admin_commands.request_id`、受理與成功兩筆 `admin_audit.request_id` 及命令詳情頁顯示的值一致。原有秘密排除檢查仍覆蓋稽核文字中的密碼、內部 token、CSRF 與 session cookie。v8 升級保留舊列的 `NULL` 請求 ID，避免為歷史請求虛構來源。

`api/admin/route_errors_test.go` 補真 HTTP 路由邊界：未知 API 路徑在登入前先回 401，登入後回 JSON 404；已知路徑的不支援方法回 JSON 405 與 `Allow`。每筆錯誤 body 的 `request_id` 等於 header，且新請求有不同 ID。這避免純文字路由器預設錯誤繞過管理 API 的 session 與錯誤契約。

## C01 寫入故障與回應遺失補驗（2026-09-28）

`api/admin/write_failure_test.go` 先對命令 INSERT 注入故障，驗證入場失敗回 500 `COMMAND_ADMISSION_UNKNOWN`（`retryable: true`，須沿用原鍵）、沒有命令／報價／收據。解除故障後原鍵首次提交回 202，再重播回 200，且只有一筆命令、報價和收據。

`api/admin/write_failure_test.go` 對收據 INSERT 注入持久 SQLite 故障，驗證命令已受理但財務事實回滾時回 503 `COMMAND_PENDING_RETRY`，並提供原命令 ID。解除故障後，同鍵重播兩次只建立一筆報價與一筆成功收據。

`api/admin/client_disconnect_test.go` 使用真 HTTP 連線在命令成功後丟棄首個回應。重送前已存在成功命令、報價與收據；同鍵重送兩次後仍各一筆，且返回同一命令。另一案例等待 C01 命令受理與執行 lease 取得，在報價寫入受 SQL trigger 延遲的測試環境中取消客戶端請求；伺服器結束後原命令保留為 `accepted`、lease 釋放、報價及收據為零。同鍵重送兩次後僅建立一筆報價及收據。後一案例連續執行 20 次通過；其他動作的執行中斷線與 DB 故障組合仍待驗。

`web/admin/e2e/admin.spec.ts` 另以受控 500 `COMMAND_ADMISSION_UNKNOWN` 驗證 C01 首次入場不確定時，重新載入後仍保留原 payload 與冪等鍵；再次送出才建立一筆成功命令、一筆報價與一筆收據。這補足了受理前 DB 失敗到 UI 恢復的交界，但沒有把受控 HTTP 錯誤當成真實 SQLite 故障的瀏覽器注入。

## C18–C21 發布鏈的成功收據補驗（2026-09-28）

`web/admin/e2e/admin.spec.ts` 的「catalog publishes immutable price and meter versions before cohort selection」案例已在真實瀏覽器逐一執行 C18（Pro 價格）、C19（計量表）、C20（兩個獨立計量價格版本）與 C21（cohort 選價）。每一個來源 ID／方案各以 SQLite 關聯 `admin_commands` 與 `admin_command_receipts` 核對唯一成功收據；同版號與已選價衝突的額外預覽仍在入場前被拒絕，沒有增加命令。定向 Playwright 1／1 通過；這補成功鏈的收據證據，不取代其他輸入、重播及恢復矩陣。


## C02 來源讀取失敗時的原鍵恢復（2026-09-28）

`web/admin/e2e/admin.spec.ts` 的真實 C02 案例先接受報價並讓成功 POST 回應遺失，再令報價詳情 GET 回 503；頁面重整後仍可按原 idempotency key 查回同一命令，命令頁顯示成功。SQLite 直接核對 C02 命令、訂閱與帳單各一筆。其餘 C03／C04／C05／C06／C23 的來源故障入口與鍵／動作接線由 `web/admin/e2e/source-read-recovery.spec.ts` 受控介面案例驗證；C03 首次查詢回 503 後再次使用同一鍵。該案例不作財務收據宣稱。


## C09／C16 命令完成與外部操作失敗的 UI 區分（2026-09-28）

`adminFinishExternalCommand` 在服務商提供確定失敗的終態證據後，可將派送命令記為 `succeeded`，同時將 `operation_status=definitively_failed` 寫入結果參照。兩者分屬命令流程與外部金流結果。React 操作結果及命令各入口現額外標示付款或退款確定失敗。真實 C16 瀏覽器案例核對 Credit 保留額釋放、可用額恢復，以及操作結果、命令詳情、列表、抽屜的警告；真實 C09 案例核對付款操作失敗警告。

## C02 收據故障後沿原 HTTP 命令恢復（2026-09-28）

`api/admin/quote_acceptance_receipt_failure_test.go` 在隔離 SQLite 以真實管理 HTTP handler 提交 C02。收據 INSERT 故障時回 503 `COMMAND_PENDING_RETRY` 與原命令 ID，訂閱、帳單、帳期、付款義務、outbox 和成功收據均未提交。解除故障後同鍵重送兩次，兩次都取得同一成功命令；上述財務事實與收據各一筆，金額和來源 ID 一致，provider capture 仍為零。同鍵提交另一張有效報價回 409 `IDEMPOTENCY_CONFLICT`，第二張報價沒有建立訂閱。這補足原有領域層收據回滾測試的 HTTP 回應、冪等恢復與鍵衝突證據，不取代其他 C02 故障點矩陣。

## C12 收據故障與提交後回應遺失（2026-09-28）

`api/admin/credit_apply_receipt_failure_test.go` 以來源已付款帳單的 funded Credit 抵扣同戶下期帳單。收據 INSERT 失敗時，HTTP 回 503 與原命令 ID，抵扣和舊付款取消均回滾；同鍵恢復後只抵扣 500 一次，舊付款取消且 outbox 設為 `done`，餘額須另建立新付款操作。另一筆 250 抵扣在成功提交後遺失首個 HTTP 回應；重送兩次都回同一成功命令，兩筆抵扣合計 750，Credit 可用 250，目標未清 1250，收據與 provider capture 沒有重複。這補足 C12 的 HTTP 回應／收據邊界證據，不代替其餘動作的故障矩陣。

## C11 已收款減額的 HTTP 收據故障（2026-09-28）

`api/admin/reduction_receipt_failure_test.go` 在隔離 SQLite 對已實收帳單提交 C11，注入收據 INSERT 故障時 HTTP 回 503 與原命令 ID，減額、funded Credit、原收款釋出和成功收據全部回滾；原帳單與 provider capture 保持不變。同鍵恢復後只有一筆 1000 correction、grant、release 與收據，淨實收等於修正後義務。另一筆有效減額若沿用相同鍵則回 409，沒有第二次財務效果。既有瀏覽器案例另驗提交後回應遺失的 C11 原鍵恢復。

## 專用操作頁命令指標恢復（2026-09-28）

C01、C02、C03／C04、C05／C06、C07、C08、C23、C26 的專用頁改以 actor、動作及目標為鍵保存最近命令 ID；共用 `ActionForm` 亦使用同一機制。這讓重新載入頁面後可沿原命令 ID 查詢狀態，不以本地狀態宣稱財務操作成功。C01 與 C05 的 Playwright 情境直接驗重整後仍可讀原命令；C05 的下一步需明確清除本頁指標，重新讀取訂閱後才進入 C06。其餘專用頁目前是程式接線與建置證據，尚不能由 C01／C05 案例推論所有動作的回應遺失及 404／403 讀取矩陣已簽核。

2026-09-28 完整回歸：Playwright 109／109 通過，React 建置及 Go 全套測試通過。此結果驗證現有測試涵蓋的路徑；逐動作尚列「部分」的權限、故障與可觀測性矩陣仍待逐項簽核。
