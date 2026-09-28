# Web Admin 驗收計畫

狀態：A01–A30 的完整逐項驗收 **尚未完成**。C01–C49 均有 API 與 UI 操作入口；Go 測試覆蓋主要成功路徑、session／CSRF、schema 升級、lease、批次恢復與 fake provider 故障。可重跑的 Playwright 測試涵蓋購買、付款與退款結果未定的查證、價格與用量、合約、對帳、帳戶遷移及其切換門檻；詳情與歷史資料可跨頁查閱。逐項證據和剩餘錯誤矩陣見[Web Admin 實作紀錄](../implementation/web-admin.md)。

依據：[功能設計](05-web-admin.md)、[工程契約與 C01–C49](06-web-admin-contracts.md)、[任務計畫](07-web-admin-implementation-plan.md)。測試環境為真實 Go server、隔離 commerce/provider SQLite、可控制的 business clock；無外部支付連線。

## 1. 證據與共用矩陣

每筆執行紀錄保存 source revision 或工作區 hash、schema version、環境／時鐘、資料種子、命令、結果、失敗時的 trace。測試帳號與資料全為合成。不能只以 HTTP 200 判定成功，必須核對資料庫金融事實、命令 receipt、provider fake 記錄和 UI 狀態。

**C01–C49 每一條命令**都要有：合法 payload、欄位／範圍錯誤、缺 session、缺 capability、跨 actor preview、同 key 同 payload、同 key 不同 payload、相同對象重播、UI 成功／錯誤呈現。需要 preview 的命令另測 expiry、來源改變、其他 command 已 claim、交易內 revalidation；N 類仍要驗證其獨立業務來源 guard。適用性表不能默認略過；任何 N/A 都需對應方法與理由。

所有 GET 列表另測：預設／最大 limit、未知 filter、cursor 與 filter 不符、穩定排序、空結果、取消、去敏、schema 型別、資料插入時跨頁語意。ID 為不存在或不同資源型別時明確回 404/422，不觸發寫入。

## 2. 場景與判定

| ID | 環境／入口與受控條件 | 驗證與預期結果 | 若失敗，使用者必須看見 |
| --- | --- | --- | --- |
| A01 | 現有 Go fixtures；T05 前後 | S01–S12/P01–P03 既有金額與狀態回歸；檢查同測試是否真正走相同 domain path | 回歸阻擋交付；不以 UI workaround 遮蓋 |
| A02 | 新暫存目錄；普通及 adminui build；直接開詳情 URL | 無 dist 的純 API build 通過；缺 dist 的 admin 啟動報明確建置錯誤；有資產的深連結與靜態檔正常 | 可採取動作的建置／路由錯誤，不能空白畫面 |
| A03 | `/admin/login`、`.env`、session、logout；時間受控 | `.env` 缺值拒啟動、process env 優先、特殊字元正確、錯密碼／未知帳號同訊息、5 次失敗／15 分鐘及全域節流；nonce／session 輪換；session 30 分鐘閒置／8 小時／重啟失效；其他 Host/Origin、缺 CSRF、錯 content type 被拒絕 | 登入或明確 403；不回金融成功提示 |
| A04 | 管理 server 路由；不同 capability actor | `/v1` 寫入不可透過管理 listener 進入；所有 action 後端拒絕不足權限；內部 token／管理密碼不在 bundle、response、URL 或 log；`.env.example` 無有效憑證，忽略規則生效 | 403 或未掛載路由的 404；無資料變更 |
| A05 | 新庫、目前 schema fixture、故意讓遷移中途失敗 | admin 表／索引按 version 一次建立；失敗整體 rollback；原 invoice／provider facts unchanged；舊 CLI 的允許寫入仍可用 | 啟動失敗原因與復原步驟，未開放 HTTP 接受命令 |
| A06 | 兩個 client 同 key；於 admission 後／domain commit 前後確定性中斷 | 一個 command、一次 financial effect、一個 receipt；重啟查回結果；不得有 financial commit 無 receipt 的本地操作 | 同一 command 的進度／結果，不產生第二個意圖 |
| A07 | lease 過期後新 worker claim，舊 worker 再回報；能力撤銷 | 舊 generation 不能蓋新結果；未執行命令撤銷；已存在 provider 義務繼續查證 | `failed/PERMISSION_REVOKED` 或 waiting；不能把有外部效果的命令當一般失敗 |
| A08 | 成功後令 preview 過期、revision 改變，再送原 key | 優先回原 command/receipt；改 payload 則 409；重整頁面找回 command | 原結果或 key conflict，不顯示假性 quote expired |
| A09 | preview 到期、clock revision 變更、另一操作者改 source；超確認金額 | 無 financial 寫入，409 PREVIEW_STALE；保留使用者意圖並顯示新舊差異；僅允許契約規定的期中金額下降 | 重新預覽的理由與差異 |
| A10 | 0、負值、int64 邊界、超 JS safe integer、exponent、invalid UTC | 許可值精確往返；不許可值在寫入前拒絕；minor units 不經 JS Number；rational 分母檢查 | 欄位級錯誤；無默默四捨五入或截斷 |
| A11 | 各列表插入跨頁新資料、status filters、缺來源值 | cursor 穩定、有界；標示非全域 snapshot；unknown 不當 0；敏感 key 只對應權限可見 | 正確 empty/stale/not-found/forbidden |
| A12 | 所有頁面 keyboard、窄螢幕、API 失敗、失去焦點；Ant Design Table／Form／Modal 互動 | 確認框焦點恢復、labels/error 關聯、狀態非僅靠顏色；金額與費率輸入不經浮點轉換；無金融 optimistic success；輪詢離頁停止 | 可操作的錯誤／資料觀測時間 |
| A13 | `/customers/:id` 與 `/subscriptions/:id`；C01–C06 | Basic 新購；Pro 排程；取消／恢復；過期或已購 quote 不重用；quote/binding 原子；席次／價格／revision 任一不同無寫入 | 實際價、下期意圖與衝突原因各自顯示 |
| A14 | Basic $20 → Pro 5 席 $100，30 日週期中點；C04 | 初始差額 $40；延後執行依相同政策重算，超確認上限拒絕；付款 UNKNOWN 不提前切服務 | due_now_estimated 與實際 pending_amount 分開，狀態正確 |
| A15 | `/payments`；C07–C10，部分付款、確定拒絕、lost response | 合法部分額不超可收額；RetryFailedPayment 對應正確 invoice／原操作狀態；只有一筆原 provider capture；不因隊列換人派送 | declined／waiting／confirmed 正確，原操作查證入口 |
| A16 | `/credits`、`/refunds`；C15–C17，兩者爭用同 credit | reserve＋apply 不超 funded budget；退款 UNKNOWN 保留；by-ID dispatch 與確認對象一致；查證後僅一次 refund | 可用、保留、已退分開；不足／未知原因 |
| A17 | `/invoices/:id`、四類來源歷史；C11–C14，延遲開通、期滿未服務；超過 100 筆與跨頁插入 | invoice 原始 lines 不變，correction／credit 有來源；沿用既有 $5.34 等 oracle；重播不再減額；有界游標可查完歷史且不重複，非法及跨種類游標拒絕 | 原始／更正後／已收／應付數字與來源可追查；截斷提示可進完整歷史 |
| A18 | `/jobs`；C13/C30/C32/C44/C45；preview 後新增 eligible item | 只處理固定 membership；逐項 crash/restart 不重做；關閉 server 不丟失已 commit 的項目；summary 分開 success/conflict/waiting/skipped | 逐項原因與目前進度；不可誤報全完成 |
| A19 | `/catalog`；C18–C21，Pro 與新 meter | 已發布版本唯讀；checksum／元件完整；版本與選價衝突拒絕；AI token rate 可展示接受；原 tasks 不變 | 新版本發布與選價生效日期明確 |
| A20 | `/price-migrations/:id`；C22–C25，正常戶＋衝突戶 | 預覽 pin targets；暫停／略過／恢復不重做；反向以新批次執行；已生效不被 pause 抹除 | 每戶新舊價／revision／原因 |
| A21 | `/usage`；C26–C30，重複／衝突／撤銷／晚到 | 原事件不可改；20,003→$0.00、20,010→下一期 $0.01 的既有 policy；負差額 CreditNote；rating 歷史保留 | estimated/finalized/rerated 與差額來源 |
| A22 | `/contracts`；C31/C32、C01/C02 contract 分支 | 客戶／contract 版本綁定；Acme 五席 $75；Net30 接受即服務、未到期不收；缺後續價 hold | 現在應付 0、每期承諾 $75、due date／hold 各自顯示 |
| A23 | `/reconciliation`、`/discrepancies/:id`；C33–C35 | 修復前再核證據／revision；金額不符走人工；修復後有 verification run；人工決議不生成金融修正 | expected/actual、blocked reason、已執行與已驗證區別 |
| A24 | `/account-migrations/:id`；C36–C43 與 adapter GET | 一致映射、shadow/readiness；不完整來源先 manual review；UNKNOWN 阻止切換；切讀先於切寫；stop 阻止新命令而不回退金融史 | owners、來源與每個未過門檻；不顯示虛假的 rollback 成功 |
| A25 | `/lab`；C46–C49，執行中改時間、指定故障 | 一個命令用固定 business time；wall clock session/lease 不跳；fault ticket 只用一次且僅指定 payment/refund；fake decision 寫入後 crash 由 provider control receipt 找回，不因後來已有 capture 而重寫；改時間不自動收款 | 環境、時刻與已啟用故障永久可辨識 |
| A26 | `/commands/:id`；跨頁、瀏覽器重新整理、server 重啟 | actor 身分穩定；新 session 找回舊命令；不因 UI timeout 換 key；外部不確定性保留為 waiting | 可恢復操作紀錄，無第二筆義務 |
| A27 | 本機負載：1 萬訂閱、10 萬用量事件，5 讀取 client＋1 worker | 同機記錄 hardware／版本；列表 p95 ≤500ms、100 rows response ≤1MiB；worker 有進度，無全量 State dump／無長時間 provider transaction | 慢查詢有量測；逾時顯示 stale，不偽造最新結果 |
| A28 | DB query 錯誤、fake provider 暫不可查、client 斷線 | error code 正確；command 的 retryability 與資金保留不矛盾；audit 有 actor/request/command/result refs、不含 token | 可採取下一步的錯誤或 waiting，沒有靜默失敗 |
| A29 | 新暫存 DB 與舊 DB 的本機建置 binary | README 指令能從 build/login 到所有入口；Go API-only 與 adminui 都可用；退出清理測試服務 | 明確啟動／退出／升級操作 |
| A30 | 最終 coverage audit | 每個 C01–C49 有 endpoint、UI、permission、receipt/recovery、適用 contract matrix 與對應 A 場景的真實證據；T01–T22 全完成 | 缺任一項就維持 Web Admin 未完成 |

效能數字是本機驗收目標，並非已測得結果或生產 SLA。達不到時以 query plan／profiling 找具體原因；不得靠提高上限讓測試變綠。首次量測記錄 fixture 與硬體後，任何調整需留下原因與新門檻。

## 3. UI 路徑與自動化策略

Playwright 測試路徑涵蓋工程契約的全部頁面。共用元件以 Vitest／React Testing Library 檢查精確金額、狀態映射、form validation、鍵盤／焦點；financial truth 仍由 Go transaction tests 與有實際資料庫的 E2E 判定。web request mock 只用於 loading/error 元件測試，不能代替付款／退款／恢復 scenario。

並行使用 channel／barrier 或可控 test hook 安排 interleaving，不以 sleep 猜時序。provider 故障由 fake provider 的既有模型與特定 test hook 控制，不連外測試。登入 fixture 使用臨時 `.env` 與正式 session path；不得在 production bundle 留 bypass。

對每一 command 在 A30 audit 前填：action ID、UI route、handler、domain helper、來源 guards、permission、preview policy、receipt policy、HTTP tests、browser scenario、執行證據。這些欄位沒有證據時保持空白並列為未完成，不能以文件存在作通過判定。

## 4. 已知限制與不宣稱事項

本規劃沒有測量真實 provider、跨區域一致性或公開部署安全。一次本機 browser smoke 不能證明 49 項功能完成；同樣，命令表有紀錄不代表與業務事實同交易。每個 invariant 都需要相應金融來源與中斷測試才能結案。
