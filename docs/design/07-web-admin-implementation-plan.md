# Web Admin 實作計畫

狀態：設計與實作規劃完成；**功能實作進行中**。使用者已確認 React 與 Ant Design，並已授權依計畫完成實作。下方 checkbox 需在完整驗收後才勾選，不能把局部實作視為整項任務通過。

上位文件：[功能設計](05-web-admin.md)、[工程契約](06-web-admin-contracts.md)、[驗收計畫](08-web-admin-test-plan.md)。[JSONL](web-admin-tasks.jsonl) 保留原始任務估算與依賴，當前驗收狀態以[實作紀錄](../implementation/web-admin.md)為準。

目前已可使用的切片：本機 `.env` 管理帳密登入、session／CSRF、React＋Ant Design 頁面、分頁列表、資源詳情、概覽，以及 C01–C49 的命令入口。訂閱、帳單、對帳與帳戶遷移均有可追來源的詳情頁；批次進度、命令恢復及本機 fake provider 故障也可操作。主要成功路徑與部分錯誤路徑已有 Go 和瀏覽器證據；**A01–A30 的完整逐項驗收仍未完成**，最新結果見[實作紀錄](../implementation/web-admin.md)。

## 1. 工程審查結論

前一版有完整功能方向，但不足以直接開工。此次完成 architecture、code quality、testing、performance 四個面向的設計審查；沒有執行實作、安裝前端依賴、啟動新服務或部署。

| 發現 | 目前證據／缺口 | 已選方案 |
| --- | --- | --- |
| F01 管理讀取缺口 | `api/v1.go` 路由有限；`lab/inspect.go` 的 State 一次返回全量 | 新增有界、穩定排序、來源可追查的管理 DTO，不把 State 全量轉 JSON |
| F02 管理權限繞過 | 現有 `serve` 提供本機 v1，僅內部操作帶 token | 新 `admin` server 專用 session 路由；不掛載未受管理授權的 v1 |
| F03 寫入與 command 不原子 | 領域方法自行 BeginTx，`Lab` 限制單連線；外層 wrapper 會死鎖或分離提交 | 抽 Tx helpers，command receipt 與本地業務 commit 同 transaction |
| F04 預覽非承諾 | quote 席次／revision 已有保護，但沒有通用管理 preview 或確認金額上限 | 持久化 preview、執行再核對、精確金額／上限政策，且先處理冪等重播 |
| F05 尚無管理工作恢復 | outbox 與 provider key 已存在，但沒有管理 actor、command/job receipts | 加入 command、lease generation、job membership、逐項結果及 restart recovery |
| F06 next item 可能漂移 | `DispatchRefundNext`、部分 Run* 方法自行找下一筆或掃描全部 | by-ID dispatcher／逐項 helper；preview 固定 membership，避免執行新進對象 |
| F07 時鐘與故障是 CLI 局部能力 | menu clock 與 server wall clock 不同；已有兩種故障模式 | 固定每命令 business time，session／lease 保持 wall clock；fault ticket 按 operation 隔離 |
| F08 前端呈現責任未固定 | 還沒有 React 專案、共用狀態元件、精確金額 DTO | 確定前端套件分工、decimal string、狀態與 stale/error 行為 |
| F09 UI 能力無逐項驗收 | 只有 CLI／domain/API 既有測試 | 為 C01–C49 建立 HTTP 行為矩陣與真實本機 browser 情境 |
| F10 build／升級未定義 | 現有純 Go 專案、既有 SQLite 檔案 | adminui build tag、保留無 dist 的 Go build、增量 schema＋舊 DB fixture 驗證 |

現有 `AcceptQuote`、變更命令、balance、provider key、reconciliation 與 migration guard 都重用。只有交易 helper、按 ID 工作、preview、admin persistence、查詢 DTO 與 Web 層是新增責任；不重建計價引擎。

## 2. 任務與依賴

每項任務都包含程式、相關測試及必要契約更新。P1＝阻擋該階段交付。此輪不把未完成能力降成日後 TODO。人工作業粗估以熟悉 Go／React 的工程師計、包含測試與審查，範圍可重疊；AI 執行時間沒有量測資料，不列虛構加速倍數。

```text
T01 → T04 → T05 → T06 → T07
T02 → T09        T03 ↗        ↘ T10 → T11
T11＋T13 → T12
T04 → T08 ────────────────────↗        ↘ T13 → T18
T07＋T08＋T09 → T14 → T15
                 ├→ T16
                 └→ T17（另依賴 T10、T13）
T13＋T18 → T19；T06＋T07＋T09 → T20
全部功能 T10–T20 → T21 → T22
```

以下路徑均為預計 ownership；同一路徑的重構串行完成，避免多個工作者同時修改交易骨架。未啟動任何 subagent、worktree 或分支。

- [ ] **T01（P1，0.5–1 日）— 凍結契約與回歸基線。** 來源 F09/F10。檔案：`docs/design/06–08`、`lab/*_test.go`。盤點既有 S01–S12/P01–P03 fixture，建立 old-schema fixture 與 C01–C49 mapping。依賴：無。驗收：A01；既有測試結果另行保存，任何新發現的 domain 缺陷先列出，不以新 UI 掩蓋。
- [x] **T02（P1，0.5–1 日）— 建立 React 與嵌入式 build 骨架。** 來源 F08/F10。檔案：`web/admin/`、`cmd/lab/adminassets/`、`cmd/lab/admin.go`。加 route shell、pnpm lock、TypeScript、Vite、Ant Design ConfigProvider／App 與主題 token、build tags。依賴：T01。驗收：A02；有／無 dist build、深連結刷新、資產路徑與錯誤頁都可判定。
- [x] **T03（P1，1–2 日）— 實作 session、CSRF、權限與專用路由。** 來源 F02。檔案：`api/admin/session.go`、`permissions.go`、`router.go`、`cmd/lab/admin.go`、`.env.example`、`.gitignore`。依賴：T01。驗收：A03/A04；`.env` 缺值／格式／優先序、登入失敗限流、session 期限、Host/Origin、未授權、重啟與舊 v1 隔離。
- [ ] **T04（P1，1–2 日）— 新增 commerce/provider 的 versioned admin schema 與升級。** 來源 F05/F10。檔案：`lab/admin_schema.go`、`lab/admin_migration_test.go`。依賴：T01。驗收：A05；新庫、舊庫、重跑、失敗 rollback、financial history 不變及舊 CLI 相容。
- [ ] **T05（P1，2–4 日）— 抽出交易 helper，保留既有 public wrapper。** 來源 F03/F06。檔案：`lab/lab.go`、`billing.go`、`corrections.go`、`refunds.go`、`subscription_changes.go`、`immediate.go`、`usage.go` 及其他寫入模組。依賴：T04。驗收：A01/A06；無 nested transaction、public 回歸、業務與 receipt 同 commit。此任務不一次改變全部金額政策。
- [ ] **T06（P1，2–3 日）— 命令 admission、worker、receipt 與中斷恢復。** 來源 F03/F05。檔案：`lab/admin_commands.go`、`admin_worker.go`、`api/admin/commands.go`。依賴：T03/T05。驗收：A06/A07/A08；canonical hash、同 key 重播、lease generation、權限撤銷及不同 crash points。
- [ ] **T07（P1，1–2 日）— 管理預覽與精確 API 值型別。** 來源 F04/F08。檔案：`lab/admin_previews.go`、`api/admin/types.go`、`errors.go`。依賴：T06。驗收：A09/A10；preview 單命令綁定、expiry、不同 actor、source change、金額上限、int64 邊界與結構化錯誤。
- [ ] **T08（P1，1–2 日）— 管理查詢骨架與核心 DTO。** 來源 F01。檔案：`lab/admin_queries.go`、`api/admin/queries.go`。依賴：T04。驗收：A11；分頁與 allowlist filters、取消查詢、來源版本、去敏及新資料跨頁語意。各功能模組的查詢在對應任務補齊。
- [ ] **T09（P1，1–2 日）— 操作台共用 UI。** 來源 F08。檔案：`web/admin/src/app/`、`components/`、`api/`。依賴：T02/T03/T08。驗收：A12；以 Ant Design Table／Form／Modal 等建立表格、時間軸、預覽、命令進度、權限與 stale/error/empty 狀態；檢查鍵盤與焦點、query invalidation、金額字串輸入及不可把一般 Popconfirm 當作金融確認流程。
- [ ] **T10（P1，1–2 日）— 報價與訂閱全流程。** 來源 F04/F09；C01–C06（self-service/change）。檔案：`api/admin/subscriptions.go`、`lab/admin_subscription_commands.go`、`web/admin/src/features/subscriptions/`。依賴：T07/T09。驗收：A13/A14；quote 與 binding 原子、新購、下期變更、取消／恢復、即時升級、過期／revision／席次衝突。
- [ ] **T11（P1，1–2 日）— 付款與退款操作。** 來源 F05/F06；C07–C10、C15–C17。檔案：`api/admin/payments.go`、`refunds.go`、`lab/refunds.go`、對應 frontend features。依賴：T10。驗收：A15/A16；按 ID 派送、原 key 查證、部分付款、確定失敗重試、UNKNOWN 保留、UI 不誤報成功。
- [ ] **T12（P1，1–2 日）— 帳單更正、credit 與升級補償。** 來源 F04/F09；C11–C14。檔案：`api/admin/corrections.go`、`lab/admin_correction_commands.go`、invoice/credit features。依賴：T11/T13。驗收：A17；來源可追查、額度並行、不可重複更正、延遲／未提供服務補償與既有金額 oracle 一致。
- [ ] **T13（P1，1–2 日）— 固定 membership 的 jobs 與營運操作。** 來源 F05/F06；C44/C45 及其他 batch 基礎。檔案：`lab/admin_jobs.go`、`api/admin/jobs.go`、jobs feature。依賴：T06/T07/T08/T09。驗收：A18；到期續約、權益刷新、逐項 receipt、部分失敗、重啟、關閉時停止 claim、membership 不漂移。
- [ ] **T14（P1，1–2 日）— 產品、meter、價格與選價。** 來源 F04/F09；C18–C21。檔案：`api/admin/catalog.go`、catalog feature、相關 domain Tx helpers。依賴：T07/T09。驗收：A19；所有元件呈現、不可修改已發布價格、重播不重複發布、cohort 生效時點。
- [ ] **T15（P1，1–2 日）— 價格遷移工作台。** 來源 F04/F05；C22–C25。檔案：`api/admin/price_migrations.go`、price-migrations feature。依賴：T13/T14。驗收：A20；完整預覽、固定逐戶 ID、衝突、略過、暫停／恢復與反向新批次。
- [ ] **T16（P1，1–2 日）— 用量事件、關帳、重算與 CreditNote。** 來源 F06/F09；C26–C30。檔案：`api/admin/usage.go`、usage feature、domain usage helpers。依賴：T13/T14。驗收：A21；事件重複／內容衝突、撤銷來源、晚到差額、rating 歷史、正負差額分流。
- [ ] **T17（P1，1–2 日）— 企業合約與 Net30。** 來源 F04/F09；C31/C32 及 C01/C02 的 contract 分支。檔案：`api/admin/contracts.go`、contracts feature。依賴：T10/T13/T14。驗收：A22；條款版本、合約客戶身分、先開通、到期收款、缺後續價 hold 與明確轉換。
- [ ] **T18（P1，1–2 日）— 對帳、修復及人工決議。** 來源 F04/F05；C33–C35。檔案：`api/admin/reconciliation.go`、reconciliation feature。依賴：T11/T13。驗收：A23；expected/actual、證據、revision、穩定修復鍵、事後再核對，人工決議不等同資金修正。
- [ ] **T19（P1，1–2 日）— 帳戶灰度遷移。** 來源 F04/F09；C36–C43。檔案：`api/admin/account_migrations.go`、account-migrations feature。依賴：T13/T18。驗收：A24；映射、shadow、來源、readiness、切讀／切寫／停止與 adapter 所有入口。
- [ ] **T20（P1，1–2 日）— 實驗室時鐘與故障隔離。** 來源 F07；C46–C49。檔案：`lab/admin_clock.go`、`api/admin/lab.go`、lab feature。依賴：T06/T07/T09/T11。驗收：A25；per-command 時刻、wall clock lease、一次性 fault ticket、不同操作不受影響、UI 環境標示。
- [ ] **T21（P1，2–3 日）— 完整 browser／並行／恢復／效能驗收。** 來源 F01/F05/F09。檔案：`web/admin/e2e/`、`api/admin/*_test.go`、`lab/admin_*_test.go`。依賴：T10–T20。驗收：A01–A28；真 Go server＋隔離 SQLite，覆蓋 49 命令，不用全部 mock 成功代替。
- [ ] **T22（P1，0.5–1 日）— 交付 build 與操作文件。** 來源 F10。檔案：README、`docs/implementation/web-admin.md`、build scripts、schema upgrade 指引。依賴：T21。驗收：A29/A30；全新環境、既有 DB、建置 binary、登入到完整操作；核對 coverage，只有通過才更新完成追蹤。

任務粗估不是交付承諾：主要風險集中在 T05–T07 的交易重構與恢復。T01 後依真實測試與 module 規模調整估時，保留功能範圍，不以縮減 C01–C49 趕期限。

## 3. 五階段交付範圍

| 階段 | tasks | 可以宣稱完成的內容 |
| --- | --- | --- |
| W1 | T01–T09 | 管理基礎、讀取與命令框架；不宣稱完整業務操作已完成 |
| W2 | T10 | self-service 訂閱工作台；合約報價分支仍待 W4 |
| W3 | T11/T13/T12/T18 | 付款、退款、credit、更正、營運 jobs、對帳 |
| W4 | T14–T17 | 價格／meter／cohort／遷移、用量、企業合約 |
| W5 | T19–T22 | 帳戶遷移、實驗室、完整驗收、可執行本機交付 |

可以獨立準備的部分：T02 與 T04、T03；T14 後的 T15/T16/T17 UI 可按 feature 分開。前提是共享 DTO、transaction helper、command dispatcher 已穩定；`lab/admin_commands.go` 等骨架須單一 owner。這是未來分工策略，此次沒有開始平行實作。

## 4. 執行與驗證命令（擬新增）

以下前端 scripts／admin build target 尚不存在，實作任務需建立後才能執行；目前不得把它們列為 passed。

```sh
go test ./...
go vet ./...
pnpm --dir web/admin install --frozen-lockfile
pnpm --dir web/admin typecheck
pnpm --dir web/admin test:run
pnpm --dir web/admin build
go build -tags adminui -o /tmp/billforge-admin ./cmd/lab
pnpm --dir web/admin test:e2e
```

前端 build script 將 dist 同步至 `cmd/lab/adminassets/dist`（生成物忽略，不提交），embed 檔案由 build tag 選用；測試 build 缺失資產會明確失敗，預設 API build 有 fallback。E2E script 負責啟動已 build 的 Go server、產生隔離 DB、透過 `.env` 測試帳密登入、退出及清理；禁止默認使用真實工作資料庫。

schema 與 command 測試先用 targeted `-run`，跨 transaction refactor 後跑完整回歸。race 測試只針對 worker／session／clock 共用狀態的具體並行風險，Go SQLite 的交易一致性仍用確定性的 DB interleavings 驗證。

## 5. 範圍、風險與結案規則

不實作真實金流、稅、多幣別、正式總帳、公開部署、CRM、完整帳號管理、ARR dashboard。這些不是本次 49 項命令中的缺口，不需要另建空泛 TODO。

規劃沒有待使用者決定的阻塞項；React 已確認，其餘技術與預覽政策採本文決策。若之後要公開部署、多名真實操作者或接 PSP，需另做需求與威脅模型，不能直接宣稱本機方案已滿足。

**設計結案**：文件連結、任務 DAG、49 命令 coverage、頁面／資料／失敗路徑／驗收 mapping 一致，且所有新增能力明示尚未實作。**開發結案**：T01–T22 完成、A01–A30 有真實證據、每一 C 命令都通過 contract matrix 和對應 browser 情境，才可把 Web Admin 標完成。目前只完成前者。

## 6. 本輪規劃檢查紀錄

2026-09-26 完成文件檢查：C01–C49 共 49 項命令均有任務對應；T01–T22 依賴無循環；A01–A30 共 30 組驗收均被任務引用；文件相對連結可解析。React 與 `.env` 單一管理帳密登入已納入一致契約，舊 bootstrap 提案已被取代。

以開始規劃前的內容 hash 核對，60 個既有 Go／module 檔案全部未變更。此次沒有建立前端專案、修改功能程式、安裝依賴、建立實際帳密或執行新增驗收。上述檢查證明規劃產物一致，不代表 Web Admin 功能已完成。
