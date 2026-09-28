# Web Admin 工程契約

狀態：規劃完成、部分實作。2026-09-26；React 與 Ant Design 已由使用者確認。本文將 [功能設計](05-web-admin.md) 的開放選項收斂為可實作決策。條目包含尚未實作的 API 與命令，實際可用範圍以[實作計畫](07-web-admin-implementation-plan.md)中的進度紀錄為準。

## 1. 確定的架構

- 前端：React、TypeScript、Vite、React Router、TanStack Query、Ant Design 與 Zod。Ant Design Form 負責互動表單和即時欄位提示；Zod 驗證 API DTO 與金額、日期等輸入格式。採 client rendering，不引入 SSR 或第二個業務後端。UI 不混用 React Hook Form、Radix UI、Refine 或另一套元件框架。
- 套件管理：pnpm；實作時選擇彼此相容的穩定版本並提交 lockfile。規劃階段不安裝套件，不虛構尚未解析的版本號。
- 前端位置：`web/admin/`；Go 管理 HTTP：`api/admin/`；查詢、命令交易與遷移仍在 `lab/`，方便重用既有未匯出的交易 helper，避免第二套金額邏輯。
- 視覺與互動基準：以 Ant Design 的 ConfigProvider／App 統一主題 token、間距、字體、locale 與訊息容器；頁面使用 Layout、Menu、Table、Form、Descriptions、Alert、Modal、Drawer、Result、Empty 等既有元件。業務元件 DataTable、Money、StatusBadge、ObjectLink、RevisionNotice、Timeline、CommandProgress、PreviewDiff、ConfirmAction、FieldError、EmptyState、ErrorState 在其上封裝領域語意；只用少量局部 CSS 處理布局與金額對齊。狀態同時呈現文字與圖示，不單靠顏色。
- 金融輸入與確認：金額和高精度費率使用字串輸入與明確格式解析，不透過 JavaScript number 或 Ant Design InputNumber 決定金融值。ConfirmAction 必須顯示伺服器預覽、版本與影響範圍，依命令契約送出並可恢復進度；Ant Design Modal／Popconfirm 只提供互動外殼，不承擔授權、冪等或金融正確性。
- 新增啟動入口：`lab admin commerce.db provider.db [127.0.0.1:8080]`。這個 server 只掛載管理靜態頁、session 與管理 API；**不掛載目前未具完整授權的 v1 寫入路由**。保留既有 `lab serve` 作獨立的本機 API 實驗入口。
- 同源管理 API 前綴 `/admin/api`；頁面前綴 `/admin`；支援明確 IPv4／IPv6 loopback。CLI、v1、admin 最終重用同一領域檢查，但本機檔案擁有者不受 Web RBAC 隔離；不把本機 SQLite 當多租戶安全邊界。
- 發布 build 使用 `adminui` build tag 嵌入前端資產；普通 `go test ./...`／純 API build 不需要 dist。沒有內嵌資產的 binary 執行 `admin` 時明確指出缺少建置步驟。不得靜默回傳空頁。
- 開發 Vite 僅綁 loopback，proxy 管理 API；只有明確設定的開發 origin 可使用。session 與 Origin 檢查仍生效。

前端不直接呼叫 `State()`、不存內部 API token、不計算收款金額、不把過期預覽當成授權依據。

## 2. 頁面契約與 frontend 邊界

| route（省略 `/admin`） | 主資料／主要操作 | 關聯面板 |
| --- | --- | --- |
| `/` | 營運待辦與資料觀測時間 | 篩選後的列表連結 |
| `/customers`、`/customers/:id` | 客戶彙整、訂閱列表、新購 | 帳單、credit、legacy 映射 |
| `/quotes`、`/quotes/:id` | 既有報價、元件、用途、期限與 fingerprint；接受或建立新報價 | 客戶、綁定訂閱、必要 client 能力 |
| `/subscriptions`、`/subscriptions/:id` | 實際與下期價格、席次、revision；報價／升降級／取消／恢復 | 帳期、付款、權益來源、時間軸 |
| `/invoices`、`/invoices/:id`、`/credits` | 帳單來源、credit 配額；更正／應用／補償 | lines、allocations、退款來源 |
| `/payments`、`/payments/:id`、`/refunds` | 義務、嘗試、provider 觀測；送出／查證 | 命令與原始資金來源 |
| `/catalog/prices`、`/catalog/prices/:id`、`/catalog/meters` | 版本比較、發布、meter 註冊、cohort 選價 | checksum、生效範圍 |
| `/price-migrations`、`/price-migrations/:id` | 遷移預覽、逐戶進度；暫停／略過／恢復 | 新舊 assignment 與衝突 |
| `/usage`、`/usage/:subscriptionId/:periodIndex` | 事件、rating、晚到差額；關帳／重算／credit note | 撤銷鏈、invoice line |
| `/contracts`、`/contracts/:id` | 合約條款、Net30、後續價；發布／報價／到期收款 | 對應訂閱與 invoice |
| `/reconciliation`、`/discrepancies/:id` | runs、expected/actual、證據；修復／人工決議 | 修復前後 revision／run |
| `/account-migrations`、`/account-migrations/:legacyId` | 映射、shadow、回填、readiness、owners；切換／停止 | legacy provenance、adapter view |
| `/commands`、`/commands/:id`、`/jobs/:id` | 命令／job 狀態、逐項結果 | 所產生的領域物件 |
| `/lab` | 模擬時鐘、fake provider 及支援的故障 | 被影響的命令／操作 |

前端資料以 resource＋ID＋filters＋cursor 作 Query key，mutation 完成後失效相依查詢；金融寫入不做 optimistic success。列表搜尋 debounce 250ms，URL 保存篩選，limit 預設 25、最大 100。可見命令頁以 2 秒開始輪詢、無變化退避至 10 秒；離頁停止輪詢，回頁重新驗證。刷新失敗時保留舊資料並顯示「過期／上次更新時間」。

UI state 至少覆蓋 loading、empty、loaded、stale、forbidden、not-found、error、submitting、waiting-verification。表單 double-click 防護只是 UX，真正冪等由伺服器負責。只在同一未完成意圖中保留 request key；修改 payload 或重新確認後是新意圖。

`next_actions` 回傳可用 action、permission 與 blocked reason code，供 UI 顯示說明；執行時仍重新核對。頁面不得因權益 active 就把 UNKNOWN 付款隱藏。

## 3. Session 與權限

使用者已指定採用 `.env` 的單一管理帳號密碼，取代一次性 bootstrap。新增 `/admin/login`，包含帳號、密碼、登入按鈕、送出中與驗證失敗狀態。帳號使用 autocomplete=username，密碼使用 type=password／autocomplete=current-password；支援 Enter 送出，登入成功導回同站原頁面，禁止外部 redirect URL。

設定鍵為 `BILLFORGE_ADMIN_USERNAME`、`BILLFORGE_ADMIN_PASSWORD`，可選的 `BILLFORGE_ADMIN_CAPABILITIES` 用於收緊能力且須包含 `read`。新增 `lab admin --env-file .env commerce.db provider.db [127.0.0.1:8080]`；預設讀目前工作目錄的 `.env`，已明確設定的 process environment 同名鍵優先。使用 `godotenv` 的 map 解析器保留 quoted value／特殊字元，不將整份檔案逐項列入 log。帳號 1–64 字、不可含控制字元；密碼 12–72 UTF-8 bytes（bcrypt 的明確輸入限制），建議至少 16 字元。缺少、空白或格式錯誤時啟動失敗，不提供預設帳密。

Go 啟動時以 `golang.org/x/crypto/bcrypt` cost 12 產生記憶體中的 salted hash，登入時驗證 hash；不把 plaintext 寫進 SQLite 或任何 response。`.env` 本身仍保存使用者指定的密碼，因此實作要忽略 `.env`／`.env.*`，只允許提交不含有效憑證的 `.env.example`，並在操作文件說明本機檔案權限。密碼不能使用 `VITE_` 前綴、不能進入前端 build-time env、localStorage、URL 或 log。不能宣稱 Go process memory 已安全抹除 plaintext。

session 閒置 30 分鐘、最長 8 小時，登出撤銷；重啟 server 後 session 失效。更改 `.env` 的帳密須重啟，會同時撤銷既有 session。忘記密碼由本機擁有者更新 `.env` 後重啟；不新增註冊、忘記密碼郵件或帳號管理功能。此輪只有規劃，不建立 `.env` 或任何實際帳密。

cookie：HttpOnly、SameSite=Strict、Path=/admin；正式 HTTPS 時 Secure。只接受已設定 Host，防止不預期 Host 指向本機服務。所有寫入包含 session 綁定的 CSRF token 並核對 Origin，JSON content type，預設 request body 最大 1 MiB；沒有 CORS wildcard。登入也核對 Origin／Host。登入前 GET `/session/csrf` 取得與短期 pre-auth cookie 綁定的 nonce，POST `/session` 驗證成功後輪換 session ID 與 CSRF token，防止沿用未登入 session。未知帳號也做 dummy bcrypt 驗證，統一顯示「帳號或密碼錯誤」。每個來源／帳號組合 15 分鐘內最多 5 次失敗，並設每程序每分鐘最多 20 次登入驗證，單次只執行一個 bcrypt 驗證；超限回 429 和 retry-after，不永久鎖帳。這些本機限流狀態在重啟後重置。一般業務讀取也需 session。

權限能力：`read`、`subscription.manage`、`finance.adjust`、`catalog.publish`、`migration.manage`、`reconciliation.repair`、`operations.run`、`usage.manage`、`contract.manage`、`lab.control`。目前單一 operator 擁有全部能力；測試 fixture 可配置不同 actor 與 capability，不開發帳號管理 UI。

本機 actor 使用穩定 `local-admin` ID，與隨機 session ID 分離；重新登入及重啟不改變命令的冪等 scope。worker 使用 server 端 capability registry，不能依賴已經過期的 cookie 保存授權。

每個 command admission 及 worker 執行前檢查 capability；管理命令只允許原 actor 或相應操作權限者讀取，`read` 能看到去敏的共享業務結果。撤銷權限停止未執行命令；已提交的金融事實及恢復查證由系統繼續完成，不能因 session 過期遺失義務。

## 4. 共用 API 契約

session 路由為 GET `/session/csrf`（登入前 nonce）、POST `/session`（username/password）、GET `/session`（目前身分與權限）、DELETE `/session`（登出，需 CSRF）。登入前只開放 login page、必要靜態資產、nonce 與建立 session，其他 GET、POST 全部受管理 session 保護。金額與量值使用十進位整數字串，禁止小數、exponent、NaN、負值超出欄位政策及超出 int64 範圍。費率為 `rate_num`／`rate_den` 字串，分母正值。revision 與寫入命令的 period_index 也用十進位整數字串；頁數／limit 是有界 JSON integer。時間為 RFC3339 UTC，允許 0–9 位小數秒且必須落在 int64 Unix nanoseconds 可表示的範圍；超精度或超範圍直接拒絕，不截斷。currency 沿用 MVP 的 USD，不能靠前端傳入新幣別擴充政策。

讀取 envelope：`{data, as_of, source_revision?, next_cursor?}`。固定排序 `(created_at,id)` 或資源指定的穩定鍵；cursor 綁定 filters 與排序、不提供任意 SQL sort。多頁資料不是同一全域 snapshot；批次影響範圍不能來自逐頁瀏覽，必須由 preview/job snapshot 固定。

錯誤 envelope：`{error:{code,message,field_errors?,retryable,current_revision?},request_id,command_id?}`。400 格式；401 session；403 權限／CSRF；404 物件或未知 API 路徑；405 API 方法不支援並回 `Allow`；409 狀態、revision、key 或 preview 衝突；422 業務欄位無效；429 有界節流；500/503 系統或暫時不可用。不回傳 SQL、stack 或憑證。

`request_id` 由伺服器逐 HTTP 請求產生，並同時放在 `X-Request-ID` 回應 header 與錯誤 JSON；不能接受客戶端自行指定的 ID。命令保存受理請求 ID，後續稽核事件保存執行該階段的請求 ID；背景恢復沿用原命令的受理 ID。舊資料在 v8 升級後仍為 `NULL`，不可補上虛構的歷史來源。

命令受理時若無法確認儲存結果，回 500 `COMMAND_ADMISSION_UNKNOWN`、`retryable: true`，不提供未確認的 `command_id`。客戶端必須保留原 payload 和冪等鍵，重新登入後也以同一鍵查證或重送；不得因 500 產生新鍵。命令已受理但執行暫時失敗時，回 503 `COMMAND_PENDING_RETRY` 並附原 `command_id`，後續查證與重試仍沿用原命令。

所有 command POST 都需 `Idempotency-Key`，金融與批次操作另需 `preview_id`；原因 `reason` 最長 500 字。actor、request ID、實際執行時間從伺服器取得。拒絕未知欄位。canonical payload hash 由已解析的 typed DTO 算出，正規化數字／時間／預設值，包含 kind、target 與 preview ID。

所有 command 首次受理回 `202 {command_id,status,location}`；相同 key、相同 actor、相同 canonical payload 回相同 command（完成時可回 200），即使原 preview 已過期或 revision 已改變也優先回既有結果。不同 payload 回 `409 IDEMPOTENCY_CONFLICT`。不能先做依賴當前狀態的預覽計算，再查冪等紀錄。

GET command 回 `status`、`result_refs`、`domain_outcome`、`error`、`created_at/updated_at`；capture 被 provider 明確拒絕可為命令已完成但 `domain_outcome=declined`，UI 仍顯示付款失敗。`failed` 表示管理命令本身未完成；已可能產生外部效果時先 `waiting_verification`。

### 4.1 讀取資源

GET `/overview`；`/quotes`、`/quotes/{id}`；`/customers`、`/customers/{id}`；`/subscriptions`、`/subscriptions/{id}` 及其 `/periods`、`/timeline`、`/entitlement`；`/invoices`、`/invoices/{id}`；`/credits`、`/credits/{id}`；`/payments`、`/payments/{id}`；`/refunds`、`/refunds/{id}`；`/prices`、`/prices/{id}`；`/meters`；`/catalog-selections`；`/price-migrations`、`/price-migrations/{id}` 及 `/items`；`/usage-events`；`/usage-periods/{subscriptionId}/{periodIndex}`；`/contracts`、`/contracts/{id}`；`/reconciliation-runs`、`/reconciliation-runs/{id}`；`/discrepancies`、`/discrepancies/{id}`；`/account-migrations`、`/account-migrations/{legacyId}` 及 `/provenance`、`/shadows`、`/readiness`、`/entitlements/{subscriptionId}`；`/commands`、`/commands/{id}`；`/jobs`、`/jobs/{id}`；`/outbox`；`/lab/status`、`/lab/provider-captures`、`/lab/provider-refunds`（lab.control，分頁）。

列表只允許該資源適用的 customer_id、subscription_id、status、time range、ID prefix 等白名單 filters。ready/readiness 只是帶時間的讀取觀測；切換命令仍在交易內再驗證。必要欄位：

| DTO | 必要內容 |
| --- | --- |
| quote | customer、用途、price/contract 版本、元件、fingerprint、到期、required capabilities、due_now/estimated、recurring commitment、change binding |
| subscription | customer、actual price/checksum/seats、revision、period、scheduled intention、billing/payment/service 各自狀態、entitlement source revision |
| invoice | original／obligation／allocated／outstanding minor、immutable lines、price/contract/usage references、更正與 credit applications |
| credit/refund | source invoice/correction、funded amount、available/applied/reserved/refunded、currency、來源 operation；未知保留不可算可用 |
| price | publication state、所有元件、meter、有效期間、checksum、cohort 使用情況；已發布唯讀 |
| migration/job | frozen targets、counts 分類、逐項 revision／結果／blocked reason、pause scope；不把部分成功標全成功 |
| discrepancy | kind、expected、actual、evidence refs、source revision、run、repair/manual decisions、verification |
| account migration | legacy/customer/beneficiary、history、read/writer owner、stopped/reason、readiness 各門檻及證據時刻 |

金額欄位語意直接沿用領域 balance struct；不得以「原始額－退款」自行拼出應付餘額。來源缺漏顯示 unknown，不補 0。

`GET /commands` 以 1–100 筆為一頁（預設 50），回傳 `items` 與 `next_cursor`；cursor 使用命令寫入順序的 rowid，新增命令不會使已讀頁重複。可用 `GET /commands/{id}` 開啟舊命令，列表頁不是全域快照。無效 cursor 回 `INVALID_CURSOR`。

### 4.2 Preview

POST `/previews`：`{action_id,target_id?,payload,expected_revision?}`。action_id 僅可為下表列舉，透過 typed switch 驗證，不使用反射呼叫函式。回 `preview_id,expires_at,source_versions,impact,blocking_reasons`；preview 5 分鐘有效，且不超過引用 quote 的 expiry。GET `/previews/{id}` 僅原 actor 或相應能力可查看。

preview 不保留付款／退款額度、不呼叫 provider、不執行領域寫入；唯一寫入是預覽紀錄。其內容保存 canonical intent、來源版本、金額及逐項目標，確認命令從伺服器紀錄取得，不信任瀏覽器回傳的總額。

金額敏感命令執行時在同一交易重新計算：退款、credit、更正必須符合確認的確切金額；期中升級允許按原政策重算後降低、不可超過確認上限；新增項目、價格、席次、帳期或生效範圍改變一律 `409 PREVIEW_STALE`。價格發布確認的是完整 canonical spec/checksum。排程確認的是實際未來生效時的選價；與既有 quote 價格不一致就拒絕。

C02 預覽若引用已過期報價，回 `409 QUOTE_EXPIRED`，不建立接受命令或付款義務。接受頁顯示原報價與到期原因，並提供帶入原客戶的新報價入口；新報價必須重新計價，不延長原報價。

C03／C04 預覽若報價 ID、綁定 Fingerprint、目標訂閱或變更方式不相符，回 `409 CHANGE_QUOTE_BINDING_MISMATCH`，不建立預覽或命令。畫面須指出綁定錯配，讓操作員從正確的報價詳情重新進入；訂閱 revision 變動仍維持原本的來源衝突處理。

批次 preview 固定 target IDs＋source versions＋預期金額；job 不得執行後來才符合條件的對象。明確逐項衝突可留待新預覽，不能偷偷擴大 batch。C13／C30／C32／C44／C45 每批最多固定 100 個候選；預覽讀取第 101 個候選以判斷是否有剩餘項目，只將前 100 個寫入固定成員。五種工作皆依前一個已建立工作的固定清單最後一項輪轉，必要時回到開頭，避免衝突或待查證項反覆阻擋其他候選。僅重開預覽不移動游標。C32 已建立的 capture outbox 會退出候選集合。preview impact 的 `has_more_candidates` 為字串 `true`／`false`，表示預覽當下仍有本批之外的候選項目；UI 必須提醒操作員建立新批次。

### 4.3 命令清單

下表的 `route` 記錄原先規劃的資源語意路徑。已實作的寫入介面統一為 `POST /admin/api/commands`，由 `action_id`、`target_id` 和 typed payload 選擇表中的動作；需要預覽時先呼叫 `POST /admin/api/previews`。單一命令入口共用 session、CSRF、能力、冪等與收據處理，實際 UI 路徑與證據見[動作盤點](../implementation/web-admin-action-audit.md)。R=需 preview；N=不需確認預覽（仍須授權、冪等及領域檢查）。表中的 inputs 不重複列 reason、preview_id、expected_revision 等共用欄位；`target_id` 必須對應表中資源對象。

| ID | route | inputs／領域服務 | 權限 | 預覽 |
| --- | --- | --- | --- | --- |
| C01 | `/quotes` | customer_id；互斥的 plan_id＋cohort＋seats 或 contract_version_id＋seats；可選 change_subscription_id＋mode＋revision。`CreateQuoteForCohort/CreateContractQuote/BindChangeQuote` | subscription.manage；合約另需 contract.manage | N |
| C02 | `/quotes/{id}/accept` | fingerprint；`AcceptQuote/AcceptContractQuote`，拒絕 change quote | subscription.manage；合約另需 contract.manage | R |
| C03 | `/subscriptions/{id}/schedule-plan` | quote_id、binding fingerprint、revision；從 quote 取 plan/seats。`ScheduleNextPlanAtPrice` | subscription.manage | R |
| C04 | `/subscriptions/{id}/upgrade` | quote_id、binding fingerprint、revision；`RequestImmediateProUpgradeAtPrice` | subscription.manage | R |
| C05 | `/subscriptions/{id}/cancel` | revision；`ScheduleCancel` | subscription.manage | R |
| C06 | `/subscriptions/{id}/resume` | revision；`ResumeCancel` | subscription.manage | R |
| C07 | `/invoices/{id}/payments` | amount_minor；`CreatePayment` | finance.adjust | R |
| C08 | `/payments/{id}/retry` | 原 operation ID 在交易內解析 invoice_id 並核對確定失敗；`RetryFailedPayment` 實際接收 invoice_id | finance.adjust | R |
| C09 | `/payments/{id}/dispatch` | 固定 operation ID；`DispatchCapture` | finance.adjust | R |
| C10 | `/payments/{id}/reconcile` | 原操作；`ReconcilePayment` | finance.adjust | N |
| C11 | `/invoices/{id}/reductions` | reduction_minor、reason；`PostReduction` | finance.adjust | R |
| C12 | `/credits/{id}/applications` | invoice_id、amount_minor；`ApplyCredit` | finance.adjust | R |
| C13 | `/jobs/change-corrections` | preview 固定 change IDs；拆解 `RunChangeCorrections` | finance.adjust | R |
| C14 | `/changes/{id}/resolve-unfulfilled` | change ID；`ResolveUnfulfilledImmediateChange` | finance.adjust | R |
| C15 | `/credits/{id}/refunds` | amount_minor；`ReserveRefund` | finance.adjust | R |
| C16 | `/refunds/{id}/dispatch` | 固定 refund ID；由 `DispatchRefundNext` 抽出按 ID 執行 helper | finance.adjust | R |
| C17 | `/refunds/{id}/reconcile` | 原退款；`ReconcileRefund` | finance.adjust | N |
| C18 | `/prices/pro` | ProPriceSpec；`PublishProPrice` | catalog.publish | R |
| C19 | `/meters` | id、source、unit、schema_version；`RegisterMeter` | catalog.publish | R |
| C20 | `/prices/metered` | MeteredPriceSpec；`PublishMeteredPrice` | catalog.publish | R |
| C21 | `/catalog-selections` | plan_id、cohort、effective_at、price_version_id；`SelectCatalogPrice` | catalog.publish | R |
| C22 | `/price-migrations` | id、cohort、target_price_version_id、固定 subscription_ids；`PreviewPriceMigration/PlanPriceMigration` | migration.manage | R |
| C23 | `/price-migrations/{id}/pause` | batch ID；`PausePriceMigration` | migration.manage | N |
| C24 | `/price-migrations/{id}/items/{subId}/skip` | 明確對象與理由；`SkipPriceMigrationItem` | migration.manage | R |
| C25 | `/price-migrations/{id}/resume` | 重查後的 batch；`ResumePriceMigration` | migration.manage | R |
| C26 | `/usage-events` | source、event_id、subscription_id、meter_id、occurred_at、quantity；`RecordUsage` | usage.manage | N |
| C27 | `/usage-adjustments` | source/event_id/subscription_id、original_source/original_event_id、reverse_quantity；`RecordUsageAdjustment` | usage.manage | R |
| C28 | `/usage-periods/{subId}/{index}/close` | cutoff；`CloseUsagePeriod` | usage.manage | R |
| C29 | `/usage-periods/{subId}/{index}/rerate` | 帳期；`RerateUsagePeriod` | usage.manage | R |
| C30 | `/jobs/usage-credit-notes` | preview 固定差額項目；拆解 `RunUsageCreditNotes` | finance.adjust | R |
| C31 | `/contracts` | ContractSpec；`PublishContract` | contract.manage | R |
| C32 | `/jobs/contract-collections` | preview 固定到期 invoices；拆解 `CollectDueContractInvoices` | finance.adjust | R |
| C33 | `/reconciliation-runs` | as_of；`RunReconciliation` | reconciliation.repair | N |
| C34 | `/discrepancies/{id}/repair` | discrepancy ID、來源證據；`RepairDiscrepancy` | reconciliation.repair | R |
| C35 | `/discrepancies/{id}/manual-decisions` | decision、reason；reviewer 從 session 取得。`RecordManualDecision` | reconciliation.repair | R |
| C36 | `/account-migrations` | legacy_account_id、customer_id、beneficiary_id、cohort、has_history；`LinkLegacyAccount` | migration.manage | R |
| C37 | `/account-migrations/{id}/shadow-quotes` | plan_id、seats、legacy_amount_minor、legacy_currency；`ShadowQuote` | migration.manage | N |
| C38 | `/account-migrations/{id}/shadow-entitlements` | subscription_id、legacy_status；`ShadowEntitlement` | migration.manage | N |
| C39 | `/account-migrations/{id}/provenance` | LegacyProvenance mapping；`BackfillLegacyProvenance` | migration.manage | R |
| C40 | `/account-migrations/{id}/provenance/{legacyInvoiceId}/resolve` | corrected mapping、decision；reviewer 從 session 取得。`ResolveLegacyProvenance` | migration.manage | R |
| C41 | `/account-migrations/{id}/switch-read` | MigrationThresholds＋來源版本；`SwitchAccountRead` | migration.manage | R |
| C42 | `/account-migrations/{id}/switch-writer` | MigrationThresholds＋來源版本；`SwitchAccountWriter` | migration.manage | R |
| C43 | `/account-migrations/{id}/stop` | reason；`StopAccountMigration` | migration.manage | R |
| C44 | `/jobs/renewals` | preview 固定到期 subscriptions＋帳期；拆解 `RunRenewals` | operations.run | R |
| C45 | `/jobs/entitlement-refresh` | 固定 subscriptions；拆解 `RefreshEntitlements`，沿用來源事實 | operations.run | R |
| C46 | `/lab/clock` | UTC instant 或 mode=real；clock revision | lab.control | R |
| C47 | `/lab/payment-decisions` | operation_id、status=`succeeded`或`definitively_failed`；`SetFakePaymentDecision` | lab.control | R |
| C48 | `/lab/refund-decisions` | refund_id、status=`succeeded`或`definitively_failed`；`SetFakeRefundDecision` | lab.control | R |
| C49 | `/lab/faults` | operation kind/ID、mode=`lost_response`或`crash_after_provider`，一次性 fault ticket | lab.control | R |

UI 的「送出下一筆」先列出並確認具體 operation，提交 C09／C16；不得因隊列改變改送另一筆。C49 ticket 只可由有 `lab.control` 的操作者在該操作派送時使用；普通金融頁不接受任意 fault 字串。

所有 preview 需在首次開始執行前仍有效；重播已提交結果優先查 receipt，不重驗 expiry。job 在有效期內確認並持久化固定 membership 後，不因執行時間超過 preview 期限中途擴大或撤回範圍，各項目仍必須做來源與金額檢查。未開始的過期命令回 PREVIEW_STALE，不能默默延長。

C43 停止後，受該帳戶控制的新增商務寫入必須拒絕，不能把停止誤報成來源 revision 變動。C02、C05、C06 新預覽回 `409 ACCOUNT_MIGRATION_STOPPED`；C03、C04 也在建立預覽時檢查寫入權。若預覽後才停止，原命令可被查回，但執行結果為 `failed/ACCOUNT_MIGRATION_STOPPED`，不得建立訂閱、取消排程或成功收據，也不得恢復已排程的取消。同一 request key 重播仍回原命令。

C01 的建立與 change binding 必須在一個交易完成；contract／purchase／change 三種用途互斥。前端 capabilities 是呈現能力，不能取代 authorization。管理端實際支援的 meter／Net30 元件才能接受對應 quote。

### 4.4 Payload schema

ProPriceSpec：`id,version,fixed_minor,seat_minor,included_tasks,usage_rate_num,usage_rate_den,effective_from`。MeteredPriceSpec：`id,plan_id,version,fixed_minor,seat_minor,meter_id,included_quantity,usage_rate_num,usage_rate_den,effective_from`。ContractSpec：`id,customer_id,version,base_price_version_id,fixed_minor,seat_minor,effective_from,effective_to,post_contract_price_version_id?`。欄位政策由領域 validator 共用；不能由 HTTP 層放寬。

含每席費用的已發布價格及合約，`fixed_minor + seat_minor` 必須可由 `int64` 精確表示，確保最少一席可報價。無每席費用的計量價格只需固定費用本身有效；更多席次仍於報價時逐次檢查溢位。管理表單與預覽在建立命令前拒絕不合格組合，直接領域發布路徑也執行同一限制。

LegacyProvenance：`legacy_invoice_id,legacy_subscription_id,legacy_account_id,commerce_subscription_id,commerce_invoice_id,price_version_id`；status/evidence 由領域決定，不能從 browser 採信。MigrationThresholds：`max_quote_p95_millis,max_unknown_payments,max_open_discrepancies`；沿用目前 unknown 必須零的切換政策，即使 UI 允許查看門檻也不能放寬。

publish／map 等沒有單一 subscription revision 的操作，preview 綁定完整來源 fingerprint 與相關版本；執行 transaction 重查，不虛構一個適用所有物件的 revision。

## 5. 交易、命令恢復與工作模型

```text
typed request → auth/CSRF → 查原 request key → 解析/驗證 intent
    → admission transaction：command + preview claim + audit(accepted)
    → 單一 local worker：claim lease + 執行前權限檢查
        → domain transaction：來源再驗證 + 業務事實 + receipt + audit
        → provider/outbox：同一原 operation key，在 transaction 外
        → 記錄觀測 → 更新 command 結果 → UI 輪詢
```

command：`accepted → running → succeeded | failed | waiting_verification`；waiting 只能經原 key 查證得到結果。能力政策在重啟時收緊，尚未產生效果的命令記 `failed/PERMISSION_REVOKED`；已有 provider 義務仍查證。無法安全判定跨資料庫操作是否已產生效果且尚無 receipt 時，保留 `accepted/PERMISSION_REVOKED_REVIEW`，不新啟動操作，待 receipt 或權限恢復後以原命令繼續。每次 claim 有 lease generation，舊 worker 不得以過期 generation 覆蓋新結果。第一版同程序單 worker，lease 仍用於重啟恢復；本機時間調整不能改 lease 時鐘，lease／session 用 wall clock，業務帳期用 business clock。

能力撤銷後，`read` session 可以對既有 `waiting_verification` 與 `PERMISSION_REVOKED_REVIEW` 命令要求查證；執行端必須先以持久化的 operation／repair／provider control receipt 證明此路徑不會派送新操作。查證可能追加本機觀測與收據，仍需 session、Origin 與 CSRF。

對純本地金額寫入，將既有 public 方法抽成 `...Tx` helper，public wrapper 仍保留目前接口。新的管理 executor 擁有最外層 transaction，業務 facts、command receipt、審計一起 commit；不能在單連線 DB 的外層 transaction 中再次呼叫會 BeginTx 的方法。選定整個 admin 命令交易保證，禁止逐 endpoint 任意選弱化的「事後補紀錄」。

管理呼叫使用穩定 `admin:<command_id>` 作 domain request key；已存在 public requestKey 的方法沿用 key 對應，provider key 仍由原領域義務產生；沒有 key 的發布、切換、shadow、人工決議等操作，透過 command receipt 加 domain business key 避免再次生效。恢復先找 receipt；沒有 receipt 也未 commit 的本地命令可以重入。provider side effect 不能以「沒有 receipt」推斷未發生，永遠用原 operation 查證。

payment／refund dispatch 命令選定操作 ID 後，transaction 標示合法派送狀態，提交後呼叫 fake provider，觀測落盤。response lost 或 process crash → command waiting，保持原保留與 key。查證無 terminal evidence 時繼續等待，不能釋放退款額度或新增 capture。

C47/C48 會寫入另一個 provider SQLite，不能宣稱與 commerce command 同 transaction。為 fake provider 控制新增 `provider_control_receipts(command_id UNIQUE,payload_hash,target_key,result)`，與該次 decision 更新同一 provider transaction 提交；恢復先查此 receipt，不能因 capture 已經發生而把已完成設定誤報失敗。這只用於 fake provider 控制，不變更既有 capture/refund 金融事實。

對帳與 shadow 是觀測型命令：收集來源觀測與時間／版本後，把 evidence、結果引用與 command receipt 在 commerce transaction 提交。無法跨兩個 SQLite 取得單一原子 snapshot，必須記錄各來源觀測時間；修復仍重新核對來源，觀測 run 成功不代表資金一致。

job 固定 membership 並逐項 receipt；續約／收款／更正類不以一次全資料庫重新掃描作重試。job summary 分為 succeeded/failed/conflicted/waiting/skipped；server 正常關閉時停止 claim 新項目，進行中的效果由原操作恢復；可由 UI 暫停的是 C23 的價格遷移，初版不新增泛用 job pause 命令。管理 UI 不提供泛用重跑任意 job；金融衝突需新預覽，UNKNOWN 查原義務。

`business_time` 在一個命令開始執行時固定，preview 提供估值時刻；clock revision 改變會使尚未確認的 preview 失效。job items 保留該 job 的業務時刻。C46 與本程序的執行 gate 協調，不在一個命令途中換 clock。領域金額／revision 保護仍在 DB transaction，不能依賴前端或單 worker 來取代並行檢查。

## 6. 擬新增資料表與遷移

| table | 核心欄位與約束 | 保存／恢復 |
| --- | --- | --- |
| admin_schema_migrations | version PK、checksum、applied_at | 順序執行、checksum 不符拒絕 |
| admin_commands | id PK、actor_id、idempotency_key、kind、target、payload_json/hash、preview_id、status、business_time、clock_revision、lease_owner/generation/until、result_refs、error_code、created/updated | UNIQUE(actor_id,idempotency_key)；status CHECK；金融結果不自動清除 |
| admin_previews | id PK、actor_id、kind、intent_hash/json、source_versions、impact_json、expires_at、claimed_command_id | immutable；一個 preview 只能綁定同一 command；過期可查不可新執行 |
| admin_command_receipts | command_id PK/FK、domain_request_key、result_refs、committed_at | 與本地業務 transaction 同 commit；不可覆寫 |
| admin_jobs / admin_job_items | job ID、command ID、frozen as_of；item target、source fingerprint、status、receipt、failure | UNIQUE(job_id,target_type,target_id,period_key)；凍結 membership |
| admin_audit_events | id、command_id、actor、action、target、reason、before/after refs、request_id、wall time | append-only；不保存 secrets；保存來源引用而非複製全部金流 payload |
| admin_lab_settings / admin_fault_tickets | clock mode/value/revision；fault target/kind/mode/claimed command | 只在 lab profile 可用；ticket 一次性與目標核對 |
| provider_control_receipts（provider DB） | command_id PK、payload_hash、target_key、result、committed_at | 與 fake decision 更新同 transaction；供 C47/C48 查回原結果 |

session／登入限流在本機進程記憶體，重啟即失效；command/job/audit 不因登出消失。來源 financial 表的金額與 immutable history 不做 destructive migration。schema upgrade 在啟動、接受 HTTP 前完成，單 transaction 套用新增表／索引，任何失敗保持舊 DB 可由舊 binary 開啟；事先備份 commerce/provider 配對檔案。首次執行使用新暫存 DB；升級測試使用現有 schema fixture。

若新增欄位到既有表，必須兼容舊 CLI／v1 寫入（有 default／nullable 或共同 wrapper）；禁止把舊 facts 偽造為有 admin actor 的歷史。最初只讀接入可停用 admin server 回到既有 CLI；涉及新金融事實後，不以還原舊 DB 當 rollback。

## 7. 實作前已解決的設計取捨

| 問題 | 決策與代價 |
| --- | --- |
| 現有 v1 沒有完整 session／RBAC | admin server 不掛載它，避免繞過管理權限；需維護獨立管理 DTO，但共用 domain |
| 加 wrapper 後業務 commit 與 command 可能分離 | 抽 transaction helpers，receipt 同 commit；改動較多，換取可驗證的崩潰恢復 |
| batch「全部執行」會納入預覽後的新對象 | 固定 membership＋逐項 source guard；多一層 job 表與恢復邏輯 |
| fake clock 污染 session／lease | wall clock 與 business clock 分開，命令內固定時間；需要可注入時鐘測試 |
| dispatch-next 可能換了目標 | preview 選定 ID，execute-by-ID；新增 refund dispatcher helper |
| SQLite single connection 與 UI 頻繁讀取 | 有界分頁、短 transaction、單 worker、取消不可見頁面輪詢；先量測再優化，禁止複製全部 State 到 Web |

未在範圍：真實 PSP、稅、多幣別、總帳、公開網路部署、完整使用者生命週期、MRR/ARR、新的訂閱政策。原因均是超出現有本機 MVP 與 Web 操作層的目標。上述延期不會刪除 C01–C49 任一現有 CLI 能力。

規模限制：一次 preview/job 最多 1,000 個目標，超出回明確錯誤並要求分批，不靜默截斷。這是本機操作界線；所有對象仍可分批操作。登入與命令限流不可消耗金融額度，也不可觸發新的付款。

兩個 SQLite 檔案的 schema upgrade 各自在本庫 transaction 完成，不宣稱跨庫原子性。遷移保持 additive／可重跑，admin 啟動必須等兩庫所需版本都完成；若第二庫失敗則不接受 HTTP，下一次啟動從已完成版本繼續。舊 CLI 相容性與配對檔案備份由 A05 驗證。
