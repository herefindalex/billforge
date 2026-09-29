# Web Admin：操作與驗收紀錄

## 現況

React 19＋Ant Design 管理介面已接到本機 Go server。C01–C49 各有管理命令與 UI 操作入口；概覽、資源列表、詳情、命令及批次工作也可讀取。這是本機管理實驗環境，尚未完成 [A01–A30 全項驗收](../design/08-web-admin-test-plan.md)，因此不能把「入口存在」等同於整體交付完成。

價格遷移詳情的 `GET /admin/api/price-migrations/{id}` 現在回傳有界摘要與各狀態計數；`GET /admin/api/price-migrations/{id}/items` 以每頁 1–100 筆的游標查詢逐項資料，支援狀態篩選並回傳觀測時間。游標綁定批次 ID 與篩選條件；項目狀態變更後，應從第一頁重新查詢。前端預設每頁 20 筆，恢復按鈕依全批次衝突數判斷。金額仍以精確的最小貨幣單位字串傳遞。

## 啟動

1. 需要 Go 1.27、CGO、Node.js 與 pnpm。複製 `.env.example` 為 `.env`，設定 `BILLFORGE_ADMIN_USERNAME` 和 12–72 bytes 的 `BILLFORGE_ADMIN_PASSWORD`；限制 `.env` 讀取權限。可改用程序環境變數，且程序環境優先於檔案。
2. 在專案根目錄執行：

   ```sh
   pnpm --dir web/admin install --frozen-lockfile
   pnpm --dir web/admin build
   go run ./cmd/lab admin ./billforge-data/commerce.db ./billforge-data/provider.db 127.0.0.1:8080
   ```

3. 開啟 `http://127.0.0.1:8080/admin/` 登入。以 Ctrl+C 停止。資料留在兩個指定的 SQLite 檔案；停止 server 不會刪除資料。

可選的 `BILLFORGE_ADMIN_CAPABILITIES` 是逗號分隔的能力清單，必須包含 `read`；未設定時給予完整本機管理權限。變更後重啟，既有 session 失效。重啟恢復會依新能力檢查尚未執行的命令：可確定尚未產生效果者記為 `failed/PERMISSION_REVOKED`，已送出的付款／退款仍用原 operation 查證，維持 `waiting_verification` 或完成結果。已開始的批次與無法安全證明未產生效果的操作保留恢復義務；後者在命令詳情顯示 `PERMISSION_REVOKED_REVIEW` 並停止無效輪詢。「檢查既有收據」只查原 provider key 與已記錄的收據，可能更新本機查證結果，但不派送新的 provider 操作；若證據後來可查，可直接完成原命令。若仍無證據，需恢復能力後再重啟繼續。

若想指定另一個檔案，使用 `go run ./cmd/lab admin --env-file /path/to/admin.env commerce.db provider.db 127.0.0.1:8080`。前端資產可用 `go build -tags admin_ui -o /tmp/billforge-admin ./cmd/lab` 嵌入二進位檔；必須先執行前端 build。開發版不帶 tag 時從 `cmd/lab/adminassets/dist` 讀取資產。兩個資料庫路徑必須不同，管理服務僅接受明確的 loopback IP。

## 操作模型

- 登入後從左側選單進入資源、操作與實驗控制。一般資源列表可依該資源允許的欄位篩選；其中有 `created_at` 的價格遷移、訂閱、Credit、退款、帳戶遷移及對帳執行可用 UTC 的 `created_from`（含）與 `created_before`（不含），輸入 RFC3339 時間。篩選條件、游標與返回前頁的位置保存在 URL。游標綁定資源與篩選條件，依資料列順序前進；跨頁時新增的資料不構成同一個全域快照。命令詳情可在 `/admin/commands` 查詢；`/admin/jobs` 可依建立順序分頁找回批次工作、查看逐項完成與需處理數，`/admin/jobs/:id` 顯示逐項結果。
- 客戶列表 `/admin/customers` 使用客戶 ID 游標；客戶詳情 `/admin/customers/:id` 彙整訂閱、報價、帳單、Credit 與舊帳戶映射，並可預填新報價。訂閱列表 `/admin/subscriptions` 與詳情 `/admin/subscriptions/:id` 顯示實際價格、席次、帳期、權益來源、續約阻擋原因及下期安排；報價列表可由 `/admin/quotes` 進入。每類客戶關聯資料最多顯示最近 100 筆，超過時會明示截斷；`/admin/data/customers` 與原有大寫 `/admin/data/Customers` 路徑仍可讀取。
- 金融操作先取得 preview，再提交命令。preview 綁定操作者、目標、來源 revision、business clock revision 與有效期；過期或來源變動要重新預覽。重送相同 idempotency key 與相同 payload 應回到原命令；同 key 不同 payload 拒絕。
- API 先將命令持久化，再執行 domain 動作。命令收據與業務效果在可行處同交易；外部 fake provider 的未知結果保留 `waiting_verification`，查證原操作後再推進。不要因網頁逾時建立新的付款或退款義務。服務重啟會掃描可恢復命令。
- 批次工作 C13、C30、C32、C44、C45 固定 preview 成員，逐項提交，持久化成功／衝突／等待／略過結果，重啟後接續未完成項。worker lease 使用 wall clock 與 generation fencing；舊 worker 無法覆寫接管者結果。
- `/admin/lab/controls` 可設定持久化 business clock、付款或退款 fake provider 決策，以及一次性指定故障。具備 `lab.control` 能力時，頁面另顯示獨立 provider 資料庫的收款、退款、已設定決策數與待用故障票據數；`/admin/lab/provider-captures`、`/admin/lab/provider-refunds` 提供狀態篩選與游標分頁。provider 決策與 control receipt 同交易；重新啟動後時鐘仍有效。商務與 provider 資料分別觀測，實驗控制不代表真實支付服務行為。

- 瀏覽器的管理 API 讀取等待上限為 10 秒，其他請求為 20 秒。逾時只表示瀏覽器沒有取得確定回應；命令頁保留原 request key，查證後才能開始另一筆。概覽與列表若保留上次成功讀取的資料，會顯示更新失敗警示及原觀測時間；重新讀取成功後警示消失。

## 登入與權限

登入採 server side session 與 CSRF 驗證。session 有 30 分鐘閒置及 8 小時總期限；錯誤密碼和未知帳號回相同訊息，失敗驗證有限速。管理 API 的每個 C01–C49 action 都映射 capability；C01 建立合約報價與 C02 接受合約報價需同時具備 `subscription.manage` 與 `contract.manage`，一般方案報價只需前者。目前是 `.env` 定義的單一管理帳號，沒有多使用者權限編輯畫面。不得把密碼放入 `VITE_` 變數或前端資產。

## 資料庫升級與恢復

`admin` 啟動會執行 `InitAdmin`。既有 admin schema v1 會在單一交易內升至 v8，包括 preview clock revision、帳單來源查詢索引與命令／稽核 request ID 欄位；升級失敗會回滾，重跑可保持冪等。升級前請備份兩個持久化 SQLite 檔案，且在離線副本先試跑。執行中的命令仍保留提交時的 business time；之後更動時鐘會使舊 preview 失效。

如果 UI 顯示命令等待，先到命令詳情查看原 id 和狀態，再使用同一命令的恢復入口；不要換 idempotency key 重建義務。付款或退款出現不確定回應時，以 fake provider 的原操作證據查證。批次工作按 job ID 查看每項結果，成功項不會重新套用。

## 已執行驗證（截至 2026-09-28）

可重跑的 browser 驗收：`pnpm --dir web/admin test:e2e`。測試會先建置 UI，再於暫存目錄建置與啟動嵌入式 Go server、建立隔離的 commerce/provider SQLite，完成後關閉服務並清理測試資料。需要本機 Chrome／Chromium；若未安裝 Playwright 預設瀏覽器，可設 `BILLFORGE_CHROME_BIN` 指向可執行檔。

| 證據 | 結果 | 邊界 |
| --- | --- | --- |
| `playwright test` 定向執行價格遷移、帳單、訂閱、Credit、付款與退款詳情的 6 個讀取故障案例 | 6／6 通過；逐頁驗 503 保留並標示舊資料、停用寫入入口、重試恢復，以及 403 隱藏快取財務內容 | 瀏覽器網路回應受控；各路徑的真實 SQLite 查詢錯誤分類另由 Go 測試驗證 |
| `pnpm --dir web/admin exec playwright test --reporter=dot --output=/tmp/billforge-web-admin-suite-20260928` | 2026-09-28 全套 125 個瀏覽器案例通過；最後前端改動重新建置後，價格遷移詳情與暫停頁 2 個目標案例也再次通過 | 本機 fake provider 與隔離 SQLite；不代表實際支付供應商驗收 |
| `go test ./api/admin -run TestPriceMigrationItemsAreBoundedAndScoped -count=1`、`playwright test e2e/price-migration-items.spec.ts e2e/pause-migration-refresh.spec.ts` | 真實 SQLite／HTTP 驗 123 筆批次的 50／50／23 分頁、狀態篩選與變更、游標綁定批次與篩選、精確金額、404／400／500；瀏覽器驗 20 筆分頁、篩選與全批次衝突數，且既有暫停頁仍可恢復讀取 | 狀態改變後需從第一頁重新查詢；未宣稱跨請求快照一致 |
| `go test ./...` | 全套 Go 測試通過；包含 session、CSRF、C01–C49 capability 矩陣、schema v1→v8、命令／收據、lease fencing、批次恢復、fake provider 故障等切片 | 不等於 C01–C49 每種錯誤與瀏覽器情境都已覆蓋 |
| `go test ./lab -run TestAdminS07 -count=1` | 兩條 S07 管理／領域直接比對通過：付款回應遺失後延遲開通與 funded 更正；帳期邊界 hold、晚到收款、未履行決議後 Basic 續約。逐階段核對金額、來源、provider capture 及命令收據。 | 隔離 SQLite／假服務商的領域驗證；對應瀏覽器情境仍依 A14／A26 盤點。 |
| `go test ./api/admin -run TestMoneyAndTimePayloadBoundariesRejectBeforeHTTPAdmission -count=1` | 隔離 SQLite、具備相應權限與 CSRF 的真實 HTTP handler 驗金額、費率分母和 UTC 錯誤均被拒於入場前，且無命令；有效大整數字串保留精確度。 | 只覆蓋 C07／C11／C12／C15／C18／C33 指定欄位，不代表所有操作與瀏覽器欄位皆已驗。 |
| `go test ./lab -run 'TestAdminResourcePageKeepsMissingSourcesDistinctFromZero|TestAdminSubscriptionAndInvoicePagesDoNotRepeatAfterInsert' -count=1`、`go test ./api/admin -run TestResourceListHTTPKeepsMissingProjectionAndPeriodAsNull -count=1` | 訂閱缺權益投影及升級補充帳單缺帳期來源保持 `null`，真正的第 0 帳期保持 `0`；兩類列表跨頁插入後不重複。API 仍把非空數字序列化為精確字串。 | 目前直接核對訂閱與帳單；其他資源缺值矩陣仍待補。 |
| `pnpm --dir web/admin test:e2e --grep 'UTC action field rejects invalid dates and precision before admission'` | Playwright 在真實管理介面驗 C07／C11／C12／C15 int64 越界阻止預覽，改填合法大整數後錯誤解除；原有 C18 分母與 C33 UTC 案例同時通過。 | 此次是定向案例；最新完整瀏覽器回歸見 2026-09-28 紀錄。 |
| `pnpm --dir web/admin build` | TypeScript 與 Vite build 通過 | 不是 browser E2E |
| `go build -o /tmp/billforge_lab ./cmd/lab` 及 `go build -tags admin_ui -o /tmp/billforge_lab_admin ./cmd/lab` | 兩種建置通過 | 未測公開部署 |
| 本機 browser smoke | 登入、固定時鐘、重啟後 session 失效及時鐘保留；另驗付款空列表、報價必填錯誤、390 px 版面、手機選單 Escape／焦點恢復。價格發布的原 preview 因時鐘 revision 變更而被拒絕，UI 自動重建預覽並要求再次確認；資料庫確認沒有發布原價格。切換操作後錯誤不殘留。58 個已知基本及帶 ID 路由均可開啟，無 404 畫面或瀏覽器錯誤記錄。臨時服務已關閉 | 路由可開啟不代表每條命令已完成操作驗收 |
| `go test ./api/admin -run TestSubscriptionHistoryReadErrorsAndSessionGuard` 與 `go test ./lab -run TestAdminSubscriptionHistoryPaginatesWithoutRepeatingAndShowsEntitlementSource` | 訂閱歷史三個 API 都受 session 保護；缺訂閱回 404，非法游標與上限回 400；帳期及時間軸跨頁不重複，權益可追來源操作。 | 尚未覆蓋各種排程、合約與立即變更事件同時跨頁的組合。 |
| `go test ./lab -run TestReconciliationRun` 與 `go test ./api/admin -run TestReconciliationRunDetailSessionGuardAndReadErrors` | 對帳當次 finding 使用不可變快照；後續執行改變同一差異時，舊 expected／actual／證據仍保留。舊資料僅以當前值補錄並標示品質；詳情 API 驗 401／404／400。 | 舊資料在導入快照前的原始觀測無法還原；UI 明確揭示這個限制。 |
| `go test ./lab -run TestAdminAccountMigrationDetailShowsOwnersShadowsAndStopWithoutRollback` 與 `go test ./api/admin -run TestAccountMigrationDetailAndReadinessGuardInputs` | 帳戶詳情以同一讀取交易取得 owner、shadow、來源及事件；Readiness 要求明確門檻，未登入與無效參數分別拒絕。 | 舊資料庫副本與大量歷史記錄仍待驗；詳情每類最多顯示 100 筆並標示截斷。 |
| `go test ./lab -run TestAdminAccountMigrationHistoryPagesDoNotRepeatRows` 與 `go test ./api/admin -run TestAccountMigrationDetailAndReadinessGuardInputs` | Shadow 與來源歷史使用有界 keyset 分頁，跨三頁不重複；兩個端點均驗 session、404、非法游標和上限。詳情頁的「查看完整歷史」可進入對應頁面。 | 目前是小樣本；大規模歷史負載仍待量測。 |
| `go test ./lab -run 'TestAdminInvoiceDetail|TestAdminInvoiceHistory'`、`go test ./api/admin -run TestInvoiceHistorySessionCursorAndReadContracts` 與 `go test ./lab -run TestAdminSchema` | 帳單來源 DTO 於單一讀取交易連結減額、更正釋出的額度、跨帳單抵扣及退款；舊庫升級套用四個來源查詢索引且可重跑。四類來源可依建立時間與 ID 做有界游標分頁；101 筆更正可查完，插入新紀錄後不重複，跨種類游標被拒；API 驗 session、404、非法游標及 limit。 | 跨頁不提供全域資料快照；新增資料可能不在原有分頁序列中。 |
| `pnpm --dir web/admin test:e2e` | 2026-09-27 完整回歸的 94 個 Playwright 測試通過，包含並行減額預覽、可重試命令恢復、付款／退款 provider commit 後中斷，付款待查證期間調整模擬時間，以及正反向價格遷移與部分項目衝突的案例：登入、跨分頁登出後的 401、原頁重新登入與延遲舊 401 的隔離、錯密碼與缺 CSRF 阻擋、受保護讀取、舊 CLI 資料庫啟動升級、58 條路由、Credit 詳情與退款來源狀態、報價與合約、付款及退款結果未定的原命令查證、客戶與訂閱詳情、價格與用量操作、對帳證據、帳戶遷移及讀寫切換。資源篩選案例驗 URL 中的篩選條件與游標能跨頁、返回及重新整理，切換篩選後從第一頁重新查詢。用量案例驗證已付款續約後的負差額產生一筆 Credit Note 和一筆額度；合約案例驗證缺後續價格時暫停續約；帳戶案例驗證歷史來源審核與 UNKNOWN 付款阻擋切換，查證後才放行。直接核對隔離 SQLite 的金額、來源、唯一外部操作與命令收據。帳單案例另驗四類來源歷史、游標往返，以及 C14 未履行升級更正和 Credit 來源。 批次案例另驗 C45 預覽後新增合格訂閱仍維持固定成員，以及待查證項目不計入完成進度。 追加案例驗 C01／C26 入場前 422 拒絕後表單解鎖且無命令、回應遺失後重新整理仍沿原 key 查回；C07 改金額後撤銷預覽，受控 409 拒絕後可重新預覽且無 C07 命令；C18 預覽後改輸入、舊飛行回應不復活確認入口，Escape 關閉確認框後焦點返回原按鈕。 概覽與列表的受控 503、實際 10 秒讀取逾時，以及 C01 回應延遲至 20 秒寫入逾時後沿原 key 查回，也已加入瀏覽器回歸。 58 條既有路由另以 390 px 視窗檢查整頁無橫向溢出與行動選單首個 Tab 焦點。 C34 另以已付款訂閱製造缺失的權益投影，經 UI 對帳、預覽及修復後核對唯一操作與收據；加強斷言的單項重跑確認 expected／actual 快照及後續 verification。 C34 預覽後來源 revision 變動的案例核對 blocked 操作、未重建的權益與畫面警示。 服務商金額不符案例驗 C34 blocked、C35 人工決議與 provider 收款總筆數不變。 | 使用本機 fake provider；A01–A30 的完整錯誤與 browser 矩陣尚未覆蓋。 |
| 嵌入式 binary＋隔離 SQLite 的 HTTP smoke | `/admin/` 與 `/admin/login` 200；未登入 API 401；CSRF 登入 200、概覽 200、登出 204、登出後 401；測試服務已停止 | 驗證啟動與 session 路徑，尚未驗證全部 UI 操作 |
| 1 萬訂閱／10 萬用量事件的 `BenchmarkAdminStateLoad`，`-benchtime=1x` | 100 筆用量頁 6.98 ms、訂閱頁 1.58 ms；5 讀取者＋1 寫入者共 100 次讀取，P95 58.47 ms、最大 112.35 ms | 本機 DB benchmark，並非 HTTP／瀏覽器 P95；aggregate state 約 714 ms，仍不適合頁面每次全量讀取 |
| 相同資料量的 `BenchmarkAdminHTTPReadLoad`，`-benchtime=1x` | 真實本機 HTTP handler：訂閱 100 筆 22,270 bytes／42.99 ms，用量 100 筆 29,379 bytes／19.06 ms；5 HTTP 讀取者＋1 SQLite 寫入者完成 100 次讀取、20 次寫入，讀取 P95 84.57 ms、最大 193.77 ms | 寫入者只插入 audit 記錄；尚須測長時間業務 worker 與更多 UI 逾時情境 |

| 新客戶／單筆詳情的 `BenchmarkAdminHTTPReadLoad`，`-benchtime=1x` | Intel Xeon Gold 6133：1 萬訂閱與 10 萬用量事件下，客戶 100 筆 7,001 bytes／19.42 ms、客戶詳情 1.15 ms、訂閱詳情 0.75 ms、報價詳情 0.35 ms；五讀一寫 P95 91.15 ms | 單次本機量測；寫入者只插入 audit，尚未涵蓋業務 worker 與更多 UI 逾時情境。 |
| 真實用量 worker 的 `BenchmarkAdminHTTPReadLoad`，`-benchtime=1x` | 1 萬訂閱、10 萬既有用量事件；5 個 HTTP 讀取者同時做 100 次用量列表讀取，1 個 worker 經 `RecordUsage` 寫入 20 筆新事件並驗落庫。最近一次執行的 100 筆訂閱／用量／客戶回應分別為 22,463／29,493／7,004 bytes；讀取 P95 85.70 ms、最大 130.42 ms；worker 20 筆用時 617.32 ms。前次同設定執行的 P95 為 92.47 ms | 2026-09-27 兩次各 `-benchtime=1x` 的本機量測，含真實領域寫入與 fake provider 的已付款訂閱；不代表其他機器或生產流量。舊兩列是只寫 audit 的歷史基線。 |

重跑負載量測：`go test ./lab -run '^$' -bench BenchmarkAdminStateLoad -benchtime=1x -count=1 -v` 與 `go test ./api/admin -run '^$' -bench BenchmarkAdminHTTPReadLoad -benchtime=1x -count=1 -v`。單次 benchmark 只描述該機器與該次資料；正式比較需固定硬體、Go 版本及多次樣本。

本次量測環境：2026-09-26 UTC，Linux x86_64、Intel Xeon Gold 6133 2.50 GHz、Go 1.27.1。工作區尚無 Git HEAD；量測時 151 個 Go／前端來源與模組檔的 SHA-256 組合摘要為 `f7859893233ca89b163ac213c943f39e838164da04103470519c7e143dd976f9`。

真實用量 worker 量測環境：2026-09-27 UTC，Linux 7.0.0-31-generic x86_64、Intel Xeon Gold 6133 2.50 GHz、Go 1.27.1。benchmark 明確驗證預載恰為 10,000 筆訂閱及 100,000 筆用量事件；付款後的 Pro 訂閱提供實際的帳期、價格指派與 meter，寫入經領域交易，HTTP 列表逐頁查詢而非全量 State dump。五個讀取者與 worker 由同一訊號啟動，提交後逐筆計數核對進度。P95 取 100 筆已完成請求中的第 95 筆；單次樣本不構成跨環境的效能保證。

驗收缺口：A01–A30 仍有未完成項。C01–C49 的無 session、缺 CSRF 與缺 capability 已逐項驗證；尚缺其餘動作的跨 actor 預覽、重播與不同 payload 組合矩陣，全部 UI 的鍵盤與窄螢幕檢查，以及真實 HTTP 斷線、並行故障的端到端證據。對應的 [驗收計畫](../design/08-web-admin-test-plan.md)在這些項目完成前維持未結案。

### A01–A30 證據盤點

「部分」表示有可重跑的切片測試，仍缺該案例列出的完整條件；「未執行」表示尚無對應的正式驗收結果；「本機通過」表示此案例列出的本機門檻已驗。此表是缺口盤點，不代表整體簽核。

| ID | 狀態 | 已有證據／主要缺口 |
| --- | --- | --- |
| A01 | 部分 | 全套 Go 回歸通過；隔離雙資料庫比較 C01／C02 Pro 五席購買的帳單、帳期、付款、權益與價格指派，以及 C04 Basic 週期中點升級 Pro 五席的抵扣 1000、新額 5000、應付實收 4000。C44 月底續約比較帳期界線、到期日、帳單明細、付款義務、capture outbox 與實收 2000。 S03 另以 C44／C09／C45／C08 對比確定失敗、寬限、暫停與補款恢復；原失敗操作不被重用，同鍵重播不新增付款義務。 未知續約付款另以 C49／C09／C45／C10 比對第八天仍保留查證寬限，確認原操作後只入帳一次並恢復權益。 S04 以 C49 注入收款回應遺失，C09 維持原操作待查證，C10 查證後原命令完成；兩路均只有一筆 provider capture 與一筆 2000 入帳。 S05 以 C49 注入 provider 提交後中斷，重啟後比對重複／過期 Webhook、C45 權益投影重建及原 C09 命令唯一收據。 S06 以 C05／C06／C44／C45 比對取消、恢復、再次取消及邊界到期；兩路均沒有新增帳期或帳單。 S07 新增兩條隔離 SQLite／假服務商直接比對：C04 期中升級付款回應遺失時 C09 無成功收據且訂閱維持 Basic，兩天後 C10 查證並完成原 C09，C13 只更正 534 並產生同額 funded grant；另一條在帳期邊界先由 C44 hold 續約，晚到收款轉 needs_review，C14 釋出 4000 後 C44 才建立 2000 的 Basic 續約。逐階段核對 provider capture 唯一性、帳單義務／實收／釋出／未清、grant、命令收據與直接領域路徑。 S08 以 C18／C21／C22／C44 比對 Pro v2 於帳期邊界遷移、其中一戶下期回遷 v1；兩戶逐期帳單、實收與價格指派來源一致。 另驗 revision 衝突時 C44 先只讓未衝突戶以新價續約，C24 明確跳過後第二次 C44 才讓衝突戶以舊價續約。 C23 暫停與 C25 恢復另以同一遷移計畫直接比對，恢復後 C44 才產生 Pro v2 帳單。 S09 以 C26–C30／C44 比對用量關帳、晚到事件回溯原帳期、次期 debit、反向調整後暫停續約，以及 credit note 建立後恢復。 S12 以 C33–C35 比對安全權益修復、待派送與未知付款查證、來源 revision 改變封鎖、provider 金額不符時不自動動帳，以及人工審查決策。 P01 以 C19／C20／C21／C01／C02／C26／C28／C44 比對 AI token SKU 的 3000 首期、105 tokens 關帳後次期 3001 帳單與原帳期用量來源。 P03 以 C36–C43 比對 shadow、先讀後寫切換、stop 後新收費阻擋；另一案例驗未知付款與錯誤歷史憑證阻擋 readiness，C10 查證及 C40 更正後才可切換。 S11 以 C11／C12／C15–C17 比對 funded credit 先套用 500、餘額保留供退款 500；退款回應遺失期間預留額不釋放，查證後只向原 capture 退款一次。C01／C03 排程下期 Pro 五席時，原 Basic 價格保持生效；C44 到期後和直接領域路徑同樣產生 10000 的 Pro 帳單與付款義務。C11 部分付款 6000 後減額 2000，比較原額 10000、應付 8000、未收 2000、無 funded credit，再補收 2000 後兩路均結清。C11／C12 已付款減額 1000 並跨帳單套用 500，比對來源帳單釋出、目標未收 1500、額度已用 500／可用 500。S10 以 C31／C01／C02 建立 Net30 合約，C44 缺後續價格時不建立下一帳期且暫停權益，C32 仍可收回原 7500 帳單；兩路比對合約來源、到期前零 capture outbox 與到期後唯一實收。 另以 C31 設定後續價格，C44／C32 與直接領域路徑比對兩期 7500 合約帳單、各自到期後唯一收款、11 月切換後及 12 月延續的 10000 帳單；價格指派只新增一次，合約轉換只有一筆。P02 以 C19／C20／C21 管理發布新計量 SKU 後，驗舊 `/v1` client 因未宣告能力而回 409 且零訂閱，具備能力的 client 可接受同一報價。其餘 S01–S12／P01–P03 情境尚未逐項比對；詳見 [A01 基線與比對矩陣](a01-domain-baseline.md)。 新增 S11 隔離雙資料庫比對：未知收款時直接入口與 C11 預覽均拒絕 2000 減額，C10 查證原 C09 後兩路帳單義務 8000、實收 10000、funded credit 2000，provider 各一筆 capture；詳見 [A01 基線](a01-domain-baseline.md)。 另以隔離雙資料庫驗證首期收 6000、C11 減額 4000 後才啟用權益，該情境無 funded credit 且各只有一筆 capture；C12 跨客戶抵扣則不產生 application。 再補 S11 未付款減額：直接入口與 C11／C07／C09 比對舊收款取消、無 funded credit、新 8000 付款唯一 capture、結清後權益啟用。 |
| A02 | 本機通過 | 前端、普通 Go／嵌入式 Go build 通過；純 API binary 在無前端資產的工作目錄啟動；缺資產時 admin 啟動指出建置命令；58 個直接路由與靜態資產測試通過。 |
| A03 | 本機通過 | Go 測試涵蓋 session、CSRF、閒置與總期限；真 HTTP handler 的受控時鐘案例驗 29 分鐘活動後延長閒置期限、再閒置 31 分鐘回 401，以及持續活動至八小時總期限仍回 401。受控時鐘另驗同來源同帳號 5 次失敗後節流、全域每分鐘 20 次上限、期限到後恢復、未知帳號與錯密碼同回應，以及成功後 login nonce 失效。Playwright 以僅由 `.env` 提供、含 `#` 與 `=` 的密碼驗登入、登出、未登入讀取 401、錯誤密碼不建立 session，以及缺 CSRF 的金融命令回 403 且無報價寫入；另用互相衝突的檔案與程序環境帳密驗程序環境優先；缺少管理密碼時，真管理程序拒絕啟動。另驗第一分頁登出後，第二分頁的下一次讀取會清除舊管理快取、回登入畫面，重新登入回原頁；若第二分頁直接按登出而收到 401，也會關閉舊管理畫面。延遲回應測試先固定舊請求，再完成新登入，證實舊請求的 401 不會誤清除新 session。管理 listener 現以實際監聽位址驗 Host，Go 測試與真 HTTP／瀏覽器測試確認其他 Host 對 API 與靜態頁面均回 403，正常登入仍可用；預設 HTTP 80 埠省略 Host 埠號仍可用。時間邊界由受控時鐘的 HTTP handler 驗證；瀏覽器驗證 session 失效後的 401 與重新登入流程，未實際等待八小時。 |
| A04 | 本機通過 | `TestEveryActionHTTPAdmissionGuards` 與 `TestEveryActionRequiresItsCapability` 逐項驗 C01–C49 的無 session 401、缺 CSRF 403、缺能力 403，具備能力時才進入 payload 驗證；C01/C02 合約分支另需 `contract.manage`。真 HTTP／瀏覽器案例驗已登入管理 listener 的 `/v1/quotes` 回 404 且無報價寫入。`admin.New` 只保存 bcrypt hash，管理 listener 不讀取內部 token；測試用隨機密碼與 token 不出現在建置資產、session／概覽回應、URL 或啟動／登入日誌；`.env.example` 不含有效帳密，`.env` 與 `.env.local` 被忽略。預覽的跨 actor 來源守衛列於 A09。 |
| A05 | 部分 | schema v1→v8、冪等及失敗回滾測試；v3 為客戶與單筆詳情加入讀取索引，v4 為訂閱歷史時間軸加入來源查詢索引，v5 為對帳差異詳情加入反向索引，v6 為帳戶遷移詳情加入讀取索引，v7 為帳單更正、抵扣與退款來源加入查詢索引，v8 加入命令與稽核 request ID 欄位及索引。升級失敗測試另驗既有帳單金額及 provider capture 不變，且失敗後仍能由原 CLI 領域路徑建立與接受新報價；v4→v7 保留 checksum、已收款帳單、成功付款狀態及 provider capture。瀏覽器測試驗 CLI seed 資料的帳單與 capture 在 admin 啟動後仍可查。仍需舊實際資料庫副本演練。 |
| A06 | 部分 | `TestAdminConcurrentQuoteAdmissionAndReceiptFailureRecovery` 驗兩個 client 同 key 只有一筆 C01 admission；收據寫入失敗時報價回滾，重啟後沿原命令恢復，重播仍是一筆報價與收據。`TestAdminAcceptQuoteRollsBackFinancialFactsWhenReceiptWriteFails` 驗 C02 收據寫入失敗時訂閱、帳單、付款義務與 outbox 全部回滾；重啟並重複恢復後各只有一筆，provider 尚無 capture。`TestAdminResolveUnfulfilledRollsBackFinancialFactsWhenReceiptFails` 驗 C14 的更正、Credit、決議與收據在收據插入失敗時全部回滾，重啟恢復後各只有一筆且 provider capture 不變。`TestAdminReserveRefundReceiptFailureRollsBackReservationAndRecoversOnce` 驗 C15 收據寫入失敗時退款預留、退款 outbox 與收據都沒有落庫，Credit 可用額保持 1000；重啟後沿原命令及原鍵重播僅建立一筆 500 預留、一筆 outbox 與一筆收據，尚未產生 provider 退款。`TestAdminChangeCorrectionBatchResumesCommittedItemAfterReceiptFailure` 驗 C13 逐項更正已提交、最終收據失敗時保留單一已完成項；重啟後補上唯一收據，沒有第二筆更正、Credit 或 provider capture。`TestAdminCreatePaymentReceiptFailureRestoresOriginalCollectionUntilRecovery` 驗 C07 收據失敗時舊付款與 capture outbox 仍保持待派送，沒有新付款或收據；重啟後原命令只取消舊操作並建立一筆 1000 的新付款、對應 outbox 與收據，原鍵重播不增加效果。其他命令的全部中斷點仍待驗。 |
| A07 | 部分 | lease generation 接管與舊 worker fencing；付款與退款 stale-lease 派送測試、`TestAdminCapabilityRevocationPreservesExternalObligations`、`TestServerRestartRevokesUnstartedCommandUnderCurrentCapabilities`。退款另驗三種跨資料庫狀態：未提交時撤銷後無外部效果、已提交但 provider 無證據時維持等待、provider 已提交而回應遺失時沿原 key 完成且只有一筆退款與收據。未執行的本地命令及尚為 `created` 的付款／退款派送會記 `failed/PERMISSION_REVOKED`；`submitted` 仍查證。C34/C47/C48 在跨資料庫效果無法證明未發生且沒有 receipt 時保留 `accepted/PERMISSION_REVOKED_REVIEW`，不會在撤銷後新啟動；UI 顯示原因並可用 read-only session 檢查原收據，有 receipt 時沿原命令完成。完整恢復／人工處理矩陣仍待驗，因此 A07 尚未簽核。 新增兩個獨立 Lab 實例共用 SQLite 檔並行執行 C07／C08 的回歸；先前 lease 與命令交易讀後寫升級會回 `database is locked`、使命令滯留 `accepted`，現先取得寫入權後連跑各 10 次均有終態，且原 generation fencing 測試通過。其他命令的多進程交錯及完整恢復矩陣仍待驗。 C07 另由兩個獨立 OS 測試子進程同步競爭，同一 SQLite 檔連跑 10 次皆落可查回終態；C08 也以雙子進程連跑 10 次通過。 |
| A08 | 部分 | C01／C02 在真實 HTTP／SQLite 下驗同 key 同 payload 回原命令；C01 同 key 不同 payload 409，且 quote、subscription、invoice 與付款操作均未重複。C07 領域回歸另驗預覽到期後原 key 仍找回原命令，不同 payload 則衝突。C02／C07 真實瀏覽器回應遺失後，重新載入仍沿原 request key 取得既有命令，訂閱、帳單與付款操作未重複；C44／C45 同路徑驗工作與逐項成員不重建；C44 的續約帳單、付款義務及命令收據亦維持各一筆。其餘 preview action 的端到端矩陣待驗。 C12 Go 回歸驗來源額度改變後的 `PREVIEW_STALE` 失敗命令沿原 key 重播仍返回同一失敗結果，不建立新抵扣或收據。 |
| A09 | 部分 | business clock revision 使舊 preview 失效、C02/C07/C13/C14/C16 跨 actor 預覽隔離、C04 期中升級預覽後金額上升拒絕、下降可完成的測試；受理時已對所有 R 類動作共用 actor／動作／對象／內容／期限／claim 守衛，C07 回歸驗不符時無命令寫入。價格發布 browser 流程驗自動重建預覽及再次確認；通用操作表單在預覽後修改輸入會移除舊預覽，較舊的飛行中預覽回應也不能復活確認按鈕。C02／C03／C04 的業務報價期限轉實際預覽 TTL 已驗。真實 SQLite 的雙分頁 C11 案例另驗另一分頁先減額時，舊預覽首次提交回 409 且不多寫金融更正；UI 自動取得新預覽、顯示原先與現在金額，同 key 重播仍回原 `failed/PREVIEW_STALE` 命令，重新確認後才建立第二筆更正，原 invoice lines 不變。除 C02／C03／C04／C05／C06／C07／C08／C11／C12／C15 外，其餘動作的來源變動與 UI 差異情境仍待驗。 通用表單與 C07 付款、C03／C04 方案變更的專用表單，均在輸入改動時撤銷舊預覽；飛行中的舊預覽回應不得重新開啟確認入口。 C07 的失效預覽 UI 案例以受控 HTTP 409 `PREVIEW_STALE` 驗證未入場意圖被釋放並自動重建預覽；真實 SQLite 雙分頁案例驗另一操作者先將 $20.00 帳單減額 $5.00，舊付款預覽提交回 409、沒有新增付款操作，介面顯示原先 $20.00 與現在 $15.00 未清餘額，重新確認後才建立付款操作；C05／C06 真實 SQLite 雙分頁案例驗另一操作者先排程取消及其後先恢復時，兩次舊預覽各回 409；介面重新讀取訂閱並顯示原操作、目前可用操作、revision 與取消排程差異，且不會自動預覽反向操作；取消排程各階段分別僅有一筆及零筆。C02 真實 SQLite 雙分頁案例驗另一操作者先接受同一報價後，舊預覽回 409；介面重新讀取報價並顯示尚未接受到已接受的狀態差異，不再提供接受預覽，訂閱與帳單各僅一筆。C03 真實 SQLite 雙分頁案例驗另一操作者先排程取消使訂閱 revision 改變後，舊方案預覽回 409、變更排程未建立；介面保留報價 ID 與綁定 Fingerprint，顯示 revision 差異並要求重新綁定。C04 真實 SQLite 案例驗業務時鐘先後移動後，舊預覽兩次各回 409，介面分別顯示較低與較高的新本期差額，重新確認前無升級寫入；最終僅一筆升級。C08 真實 SQLite 雙分頁案例驗另一操作者先將 $20.00 帳單減額 $5.00 後，舊重試預覽回 409、新預覽顯示 $15.00、再次確認前沒有新增付款操作；另一分頁再提交舊預覽回 409，介面列出已建立的新付款操作並拒絕產生第三筆。 C12 真實 SQLite 雙分頁案例驗另一操作者在抵扣 500 的預覽後先用 C15 預留退款 500；舊預覽提交回 409 `PREVIEW_STALE`，原 C12 命令沒有抵扣或收據。介面顯示來源 `grant_reserved_minor` 變動並建立新預覽，重新確認後才抵扣 500。 C15 真實 SQLite 雙分頁案例驗另一操作者先預留退款 500 後，舊預覽提交回 409 `PREVIEW_STALE`，舊命令失敗且沒有第二筆退款；介面重新預覽並要求再次確認，最終兩筆退款預留合計恰為 grant 1000。 |
| A10 | 部分 | 大整數序列化與 domain 金額測試；C07/C15 金額 payload 驗超過 JavaScript safe integer 與 int64 上限內精確往返，並拒絕零、負值、溢位、小數、指數及 JSON number。C18 費率分母採精確正整數字串；C33 拒絕非 UTC、錯誤日期和數值時間。真實管理 HTTP handler 的 `TestMoneyAndTimePayloadBoundariesRejectBeforeHTTPAdmission` 另驗 C07／C11／C12／C15 不合法金額在預覽與命令入口均回 422 且零命令，C18 不合法分母同樣被拒，C33 不合法 UTC 命令被拒；超過 JavaScript safe integer 的 C07／C15 字串能通過語法驗證而抵達來源查詢，C18 預覽保留完整的 9007199254740993 分母。Playwright 在 C07／C11／C12／C15 表單驗證超出 int64 上限時阻止預覽、改為 9007199254740993 後錯誤消失，且沒有命令入場；C18 分母的相同瀏覽器邊界與預覽精度亦已驗。報價到期、接受後帳期終點及待開通付款時的新帳期終點，均在寫入或新 provider capture 前檢查 Unix nanosecond 範圍；既有 capture 的觀測若無法表示開通帳期，會回領域衝突且不留下部分本地事實。另以隔離 SQLite 與管理 HTTP handler 驗證已發布 Pro 價格的 9007199254740993 固定金額及費率分母，在價格詳情和一席報價詳情仍為精確字串；含席次費用的報價金額為 9007199254740994。價格元件的有效極值與跨元件數值政策，以及其餘欄位的 HTTP／browser 邊界矩陣仍待驗。 最少一席可報價檢查已覆蓋 C18／C20／C31：三種領域發布路徑拒絕固定費用與一席費用合計溢位，管理預覽 HTTP 回 422 且不建立命令；恰好等於 `int64` 上限的有效組合仍可發布與報價。Ant Design 表單在前端阻止溢位組合並於欄位修正後清除錯誤，定向瀏覽器案例通過。其他量值組合、時間與衝突矩陣仍待逐項驗收。 |
| A11 | 部分 | 資源查詢採 rowid 游標與當頁查詢；客戶列表另採 customer ID keyset 游標。一般資源的篩選欄位由後端白名單限定，游標綁定資源與篩選條件；未知、重複或格式損壞的查詢參數回 400。六個有 `created_at` 的資源支援 UTC 時間範圍，下界含、上界不含；非法時區、超出 Unix 奈秒範圍與反向區間被拒絕。價格遷移詳情改為依 ID 查詢，不再載入全份 `State`；測試驗證即使無關資料表無法讀取，仍能回傳指定詳情或 404。真 HTTP Go 測試驗客戶、篩選後報價與訂閱，以及未篩選的帳單、付款、Credit、退款在跨頁插入新資料後不重複且能走到新尾項；每個宣告的篩選欄位能查詢，其餘分頁清單拒絕未知／重複參數。補充帳單沒有 billing period 時，資源 API 現回 `PeriodIndex: null`，原首期仍回 `"0"`；缺少權益投影的訂閱回 `EntitlementStatus: null` 與 `EntitlementReason: null`。原先的 `-1` 與空字串會掩蓋來源缺失，定向測試先重現失敗再驗修正。Playwright 驗資源篩選條件與游標跨頁、返回、重整及切換篩選的 URL 行為；另驗客戶無效 cursor、管理命令 21 筆以上與新命令插入後第二頁不重複；帳單及對帳差異列表超過首頁時可沿 UI 分頁找到指定資料並開啟詳情。Playwright 驗訂閱列表的缺權益投影顯示「未知」、第 0 帳期顯示 `0`，以及即時升級補充帳單的缺帳期顯示「未知」。其餘資源的時間語意、缺來源值與跨頁插入矩陣仍未全驗。 新增真 HTTP 分頁測試：報價與訂閱在 customer_id 篩選下讀取首頁後插入同客戶及其他客戶資料，下一頁只收同客戶新列且不重複；客戶 ID 游標在首尾兩側插入名稱後，只顯示游標之後的名稱。測試直接確認 total 更新，符合非全域快照契約。報價列表的定向 Playwright 案例亦驗首頁載入後新增同客戶報價、下一頁與重整均顯示兩筆尾項，1／1 通過；其他資源仍待矩陣驗收。 合約列表曾將 `EffectiveTo` 原始 Unix 奈秒顯示為數字；Go 與真 HTTP 回歸先重現，再將 `*_to`、`*_start`、`*_end` 時間邊界與既有 `*_at`／`*_from` 一同序列化為 UTC RFC3339Nano。新測試核對合約兩端時間、來源列與 API 字串；合約列表現直接顯示兩端 UTC 時間，定向瀏覽器案例 1／1 通過。其餘資源矩陣仍待驗。 資源表列鍵改為使用用量事件、用量帳期與目錄選價的實際複合識別欄位；同一訂閱不同帳期不再共用 React 列鍵，帳戶遷移與舊帳單來源亦優先使用其唯一 ID。  批次工作列表新增隔離 SQLite 插入後游標與逐項計數測試，HTTP 對無效游標、空列表和資料庫故障有明確回應；真實瀏覽器驗 22 筆工作分頁、新工作插入、重新整理與詳情連結。 |
| A12 | 部分 | Playwright 驗證報價表單必填錯誤、390 px 版面及手機選單 Escape／焦點恢復；通用操作表單的價格發布確認框可由 Escape 關閉並把焦點還給原按鈕，命令未寫入。其餘頁面的詳細鍵盤、API 錯誤與焦點矩陣待驗。 Playwright 另驗明確 400／422 拒絕後，報價與用量表單重新開放編輯，且沒有新增命令。通用 C11 金融表單在有效預覽後遭受控 422 拒絕時，確認框關閉、焦點回原按鈕、金額欄可編輯；重新整理沒有殘留待查意圖，必填錯誤以 `aria-invalid` 與 `aria-describedby` 關聯到輸入。 Playwright 另逐一開啟 58 條已知路由，以 390 px 視窗等待載入後檢查整頁沒有橫向溢出，並驗行動選單在每頁都是首個 Tab 焦點；資料表內部仍可橫向捲動。 同一輪路由巡檢逐頁檢查可見表單標籤確實連到同一欄位的控制項，以及每個可見輸入框、文字區與原生選單皆有 label 或 ARIA 名稱；修正實驗控制頁兩個原先僅靠 placeholder 的欄位。 命令詳情以受控 `accepted` 回應驗證持續輪詢，切到概覽後等待超過一次輪詢間隔，沒有新的命令讀取；此案例只驗離頁停止。 價格／合約跨元件溢位的定向瀏覽器案例再驗每席欄位錯誤有 `aria-invalid`、`aria-describedby`，修正固定費用後錯誤與無效狀態清除，且未送出預覽。 |
| A13 | 部分 | Playwright 走通 C01／C02 購買、C05 排程取消、C06 撤銷取消、C01 變更綁定報價與 C03 下期 Pro 排程；新增客戶列表／詳情與訂閱詳情，驗客戶預填報價、實際 Pro 五席 USD 100.00、當前帳期及排程取消後的下期狀態。單筆報價／訂閱詳情改為目標查詢，無需載入全庫；訂閱歷史以帳期序號與事件鍵跨頁查詢。C04 另有 UI 案例。過期報價的真實瀏覽器案例固定業務時鐘越過原報價到期日，C02 預覽回 `409 QUOTE_EXPIRED`；畫面保留原報價金額、指出過期原因並帶原客戶建立新報價；SQLite 確認零訂閱、零 C02 命令。新增兩張五席 USD 100.00 與七席 USD 120.00 的已綁定報價，瀏覽器把第二張報價 ID 與第一張的 Fingerprint 混用時，C03 預覽回 `409 CHANGE_QUOTE_BINDING_MISMATCH`，畫面指出錯配且 SQLite 中零變更排程、零 C03 命令；Go 測試另驗 C03／C04 均不建立預覽、命令或變更事實。其他衝突及重播矩陣待驗。 |
| A14 | 部分 | Playwright 以 Basic $20 → Pro 五席 $100 的 30 日帳期中點驗 C04 預覽 $40.00；C49 令升級付款回應遺失時，訂閱仍維持 Basic。沿原 C09 命令在兩天後查證，實際差額為 $34.66，C13 批次產生 $5.34 減額；SQLite 驗變更狀態、金額與更正來源。Go 測試另驗確認上限與原子性；超上限及衝突來源的 UI 矩陣仍待驗。 |
| A15 | 部分 | Playwright 驗 C07 以 1000 最小單位部分付款取代未送出的付款操作，C47 設定確定失敗，C09 派送後 C08 建立未清餘額 2000 的重試；取消的舊操作不再阻擋重試。另驗 C49 回應遺失後 C09 待查證、跨頁及重啟後原命令查證；新增 C10 對無 provider 事實的 submitted 操作保持 waiting_verification，待原 key 出現證據才完成，只有一筆 capture／收據。其餘付款 UI 錯誤矩陣待驗。 C07 專用付款表單在金額變更後移除舊預覽，重新預覽顯示新金額；尚未確認時沒有 C07 命令。 C07 預覽提交被受控 HTTP 409 `PREVIEW_STALE` 拒絕時，專用頁移除舊預覽並解鎖原金額欄位；再建立預覽成功，SQLite 的 C07 命令數未增加。此案例只驗 UI 的拒絕路徑。 新增 C07 兩個真實預覽的競爭：Go／SQLite 驗先受理的 6000 失效、後受理的 7000 勝出，只有一筆替代操作與成功收據，原鍵重播與改 payload 衝突保持正確；雙分頁瀏覽器驗 1000／1500 競爭的 HTTP 409、失效畫面與 provider 零 capture。完整付款錯誤矩陣仍待驗。 C08 同帳單兩個已受理重試預覽另以 Go／SQLite 驗一筆 10000 新義務與 retry request、另一命令 `PREVIEW_STALE` 且零成功收據；兩鍵重播原結果，新 provider key 尚未派送。已補雙分頁與跨程序競爭；其他錯誤矩陣待驗。 C07／C08 各新增兩個獨立 Lab 實例共用 SQLite 檔的同時執行測試：各 10 次皆一成功、一 `PREVIEW_STALE`，只有一筆新義務及收據；`-race` 定向通過。 C07 多進程同步競爭再連跑 10 次皆一成功、一 `PREVIEW_STALE`，唯一替代付款與收據；C07／C09 跨實例同步競爭及 C08 建立後的 C09 派送交錯已驗； 新增 `admin_payment_dispatch_interleaving_test.go`：C07 取消舊操作後，舊 C09 正確以 `PREVIEW_STALE` 結束；C07／C09 跨實例同步競爭依提交順序產生零或一筆正確收款；C08 後派送新操作，原重試預覽失效，核對 provider 金額、幣別、帳單餘額及收據。 `web/admin/e2e/admin.spec.ts` 新增 C08 雙分頁同時預覽：勝出頁建立唯一重試義務，舊頁收到 409 並顯示預覽失效；再以 C09 派送新操作，provider 金額與操作一致。 瀏覽器補驗舊 C09 預覽與 C07 替代付款的雙分頁交錯：舊頁 HTTP 409、失效提示與不可再次確認，舊 provider key 無收款；新操作的 C09 僅收 1500。 C09 介面另驗兩筆待付款時先派送佇列較晚的指定操作：較早操作仍為 created／pending、provider key 零 capture，指定操作成功且只有一筆收據。 兩個新增 C09 瀏覽器案例加入後，完整 Playwright 回歸 92／92 通過。 |
| A16 | 部分 | Playwright 走通已收款帳單的 C11 減額、C15 預留退款、C49 指定 C16 回應遺失與原命令查證；SQLite 核對 credit 1000、預留 500、provider 退款只有一筆及收據只有一筆。C17 新增 UNKNOWN 退款查證：Go 測試驗 provider 已提交、回應遺失後的查證、同鍵重播、唯一收據與唯一退款；瀏覽器從原 C16 waiting_verification 進入 C17 查證，確認退款成功後再沿原 C16 命令完成，provider 退款仍僅一筆。新增 C12 案例：以已收款帳單減額產生的 1000 額度抵扣同客戶續期帳單 500，經 UI 預覽、確認及重新查詢，SQLite 核對來源、目標、唯一抵扣與一筆收據；預覽明示將取消的未送出付款操作數、抵扣後應付金額及重建付款提示；超過可用額度的預覽被拒絕且未建立命令，帳單詳情顯示已抵扣金額與剩餘應付。C12 瀏覽器案例再從 C07 預覽建立剩餘 1500 的新付款、由 C09 派送；SQLite 與 fake provider 核對舊 2000 操作／outbox 已取消且從未 capture，新的 provider capture 只有 1500，帳單尚待支付歸零。Go 回歸另驗 C12 預覽後額度被退款預留占用時，原命令以 `PREVIEW_STALE` 失敗、不新增抵扣或收據；`TestAdminApplyCreditCancelsOldCollectionAndCapturesOnlyRemainder` 另驗 C12 抵扣 500 時原續期 2000 收款操作及 outbox 被取消、provider 未收到舊金額，C07 建立 1500 剩餘應付的新操作後只 capture 1500，帳單餘額歸零；C17 缺 provider 證據時命令待查證、500 額度不釋放，原 key 後來成功才寫收據。不同客戶、過期帳期及爭用等 UI 情境待驗。 同一瀏覽器案例核對 C15 預留退款 500 加 C12 抵扣 500，恰好用完原 1000 Credit；舊 C12 命令失敗且未重複抵扣，重新確認後的命令才有唯一收據。 C15 雙分頁預留額競爭另以瀏覽器及 SQLite 驗舊預覽失效、原鍵留下 `PREVIEW_STALE`、重新確認前只有一筆退款，以及兩筆成功命令各有收據且總預留額不超過 funded grant。`TestAdminReserveRefundRejectsStaleSourceEvenWhenBudgetStillFits` 在領域交易層另驗額度仍足夠時也須比較來源版本，且成功／失敗原鍵重播不增加退款或收據。 新增未知收款阻止 C11 預覽的 Go／SQLite 與瀏覽器證據：HTTP 409，沒有 C11 命令、更正或 credit grant；原收款查證後才可減額。其餘並行及中斷點仍待驗。 C12 跨客戶抵扣在預覽階段拒絕，grant 可用額與目標帳單不變；首期收款加減額剛好結清時，C11 不建立無資金 credit 並啟用權益。 未付款減額後的 C07／C09 補收亦與直接領域路徑比對，舊 10000 操作無 capture，新 8000 操作各只收一次；既有瀏覽器案例覆蓋畫面與 SQLite。 新增 C15 預留與 C16 指定退款派送的跨實例同步競爭：修正前 C16 可回 `database is locked`；修正後原退款 provider 僅一筆 USD 400、額度與收據依提交順序一致，定向重跑及 `-race` 通過。 最新退款觀察交易另以原 C16 回應遺失的瀏覽器情境定向 1／1 通過。 新增 `/admin/credits/:id` 詳情，以同一 SQLite 快照區分原始、已抵扣、退款保留、已退款與可用額度；Go 測試涵蓋回應遺失時保留額不釋放、查證後轉已退款、後續帳單抵扣，定向瀏覽器驗列表進入與畫面金額轉換。 跨兩個管理實例同時執行 C12 抵扣 700 與 C15 退款預留 700 的 Go／SQLite 測試通過：1000 額度只允許一個成功，另一個 `PREVIEW_STALE`；一筆收據、一筆財務事實、零 provider 退款，額度與帳單餘額一致。定向 `-race -count=10` 通過。 |
| A17 | 部分 | 帳單詳情在同一 SQLite 讀取交易內回傳應收餘額、原始明細、付款操作，以及有來源 ID 的減額更正、釋出 Credit、抵扣本帳單的 Credit 與來源退款；各來源清單最多 100 筆並明示截斷，讀取索引支援查詢。四類來源另有游標歷史頁，Go 測試驗 101 筆更正跨頁及退款、抵扣分頁。Go 測試核對 C11 人工命令、C13 延遲開通更正及 C14 未履行升級決議的來源關聯，也驗來源帳單、grant、退款與目標帳單抵扣；C11、C13、C14 同 key 重播不重複建立更正，C11／C14 的 Credit 也各仍一筆；C13 固定成員不因重播納入後來合格的項目。Playwright 可由 C11 更正進入原命令，從 C13 更正看到升級變更 ID 與 USD 5.34，查到 500 退款由 `created` 到 `succeeded`，並走通 C14 跨帳期後查證、決議及帳單歷史來源。新增 C11 未收款帳單案例：預覽明示取消一筆舊付款及減額後 1500 應付；確認後舊付款與 outbox 取消、沒有 Credit grant，C07 新建 1500 操作並由 C09 只向 fake provider 收 1500，訂閱開通且帳單餘額歸零。Go 定向測試另核對相同金額、來源與 provider key。四類真實 SQLite 歷史頁與受控下一頁／上一頁操作已驗；其他錯誤情境仍待驗。 |
| A18 | 本機通過 | C13／C30／C32／C44／C45 均固定預覽成員，每批最多 100 項並顯示剩餘候選；來源變動、時間前進與超過 100 項的 Go 案例驗證不擴大原批次且後續批次可輪轉取得尾端候選。五種工作均有磁碟 SQLite 逐項提交後關閉／重開的恢復證據，其中 C13 驗收據寫入失敗、C30／C32／C44 驗首項不重做、C45 驗第二項來源衝突。批次收據分列成功、失敗、衝突、待查證與略過；工作頁另分列執行中，待查證不計入已完成進度。Playwright 對五種工作驗原 request key 的回應遺失恢復，C45 另驗預覽後新增合格訂閱仍維持固定成員、重新整理後進度與錯誤碼可見，超過 100 戶提示仍需新批次。 |
| A19 | 本機通過 | C18–C21 瀏覽器走通價格版本、meter 與選價，價格發布預覽 stale 後重建；不同 ID 佔用既有版號與原 ID 改動規格都回 409，原 checksum 不變。C18／C20 的數值欄位逐欄驗 int64 越界、正數欄位驗 0，非法日期不送預覽；發布後核對 SQLite 固定金額、席次、包含量、超額費率元件與 checksum。C21 同一方案、cohort、生效時間改選另一已發布價格回 409，既有選價不變。新增真實瀏覽器案例由 C19／C20／C21 發布 AI token meter、價格及選價，在預覽顯示生效時間，經 C01／C02 顯示並接受 USD 30.00 報價，再經 C26 記錄 105 token；另一個 Pro 訂閱同時能記錄 tasks。`TestP01AITokensSKUUsesGenericMeteredPath` 另驗 token 用量結算與原 tasks 路徑。有效極值與跨元件數值政策的剩餘矩陣列於 A10，完整套件仍依 A30 統一驗收。 |
| A20 | 本機通過 | 瀏覽器走通 C22–C25 遷移規劃、固定成員、略過、暫停與恢復。隔離 SQLite／fake provider 案例先用 C18／C21 發布並選用新 Pro 價格，C22 固定一筆訂閱、C44 在第一個續約邊界套用遷移，帳單為 11000 最小貨幣單位；完成該期收款後，C21 選回舊價格並以新的 C22 批次反向遷移，下一次 C44 產生 10000 的帳單，當前價格回到 `pro-v1`，指派歷史三筆且來源為反向批次。新增 `/price-migrations/:id` 詳情頁，逐戶顯示原價、目標價、預期 revision、金額、狀態及持久化的衝突原因；舊資料原因缺失會明示。另一個真實瀏覽器案例在批次計畫後改動其中一戶 revision：C44 只套用未衝突戶，批次暫停；詳情頁說明原因，C24 明確略過後再執行 C44，兩戶首期續約帳單分別為 11000／10000，已套用價格沒有被暫停抹除。Go 測試驗既有 `price_migration_items` 表可加欄位並保留資料，重跑升級不重複變更。新增領域測試驗 C03 下期變更與 C05 排程取消在遷移項目待處理時於預覽階段即回衝突，正式入口亦拒絕且不建立排程；C03／C05 瀏覽器情境在 pending 狀態均驗 HTTP 409、沒有確認入口，SQLite 中預覽及排程皆為零；C05 另在批次暫停、項目 conflicted 後驗 409 及零排程；若預覽先於遷移規劃建立，提交後執行會以 `PREVIEW_STALE` 失敗，排程與遷移狀態均不被改寫。隔離 SQLite 領域測試另外模擬來源價格、席次與下期排程在規劃後變動：批次暫停並持久化對應原因，續約不新增帳期、帳單或價格指派。另以隔離 SQLite／fake provider 的真實瀏覽器矩陣建立三筆各自固定成員的遷移，於續約前分別改動來源價格、席位數與下期排程；C44 後三個批次均暫停，詳情頁逐一顯示持久化原因，且各戶沒有新增帳期、帳單或價格指派。訂閱 schema 僅允許 `pending`／`active`，管理流程只會將已付款訂閱由 `pending` 啟用，遷移規劃要求有效訂閱；故 `subscription_inactive` 是防護分支，沒有本 MVP 可操作的 `active` 退回 `pending` 情境。A20 的正常戶與可發生衝突戶依設計驗收通過。 |
| A21 | 本機通過 | 瀏覽器走通 C26–C29 原始事件、關帳、晚到重算與撤銷；續約帳單已付款後，負差額重算經 C30 產生 1 分 Credit Note 和 1 分 funded credit，原事件不變。C26 真實瀏覽器案例另驗相同來源與事件 ID 在新管理命令中以相同 payload 重送仍只保留一筆用量；改變數量的重送顯示 `failed/DOMAIN_REJECTED`，原事件數量保持不變。新增用量帳期詳情讀取、estimated/finalized/rerated 狀態、估算與最新 rating 的精確計算來源、依 revision 分頁的 rating 歷史，以及用量事件帳期篩選；真實 SQLite 瀏覽器在關帳前、關帳後、晚到重算與 Credit Note 後逐段驗證。Go 測試驗空帳期估算、修訂跨頁、錯誤游標、401／400／404。更廣的錯誤狀態 UI 矩陣歸入 A30 綜合覆蓋盤點。 C26 的瀏覽器回應遺失案例驗伺服器已寫入原事件時，頁面重新整理後仍鎖住原意圖，沿同一 request key 查回，SQLite 只有一筆事件與命令。 |
| A22 | 本機通過 | 瀏覽器走通 C31 發布合約、C01 合約報價、C02 接受與 C32 到期收款；SQLite 驗五席 $75、首期 Net30 到期前無 capture outbox。固定時鐘的第二個案例驗證缺後續價格時 C44 暫停續約、不建立下一帳期，訂閱詳情以中文顯示暫停原因，並保留 `contract_next_price_missing` 供追查。合約雙重權限與服務開通另有 Go 測試。 |
| A23 | 部分 | 對帳、修復與人工決議的 domain 測試通過；Playwright 以孤兒服務商收款建立差異，核對 expected／actual／證據／對帳執行並記錄人工決議，再由 SQLite 確認唯一決議。對帳差異列表在計算是否需翻頁前等待資料表載入；測試以延遲 API 固定重現原先的載入競態，修正前失敗、修正後通過。對帳執行詳情已支援差異游標分頁、當次不可變 expected／actual 快照與目前狀態分離；舊資料補錄值明確標為 `backfilled_current`。Go 測試證明後續對帳不覆寫舊執行證據，並驗 v4→v5 索引升級。 C34 瀏覽器情境由已付款且曾刷新權益的訂閱刪除測試投影，對帳後確認為 `SAFE_AUTO_REPAIR`；詳情頁將來源 revision／證據帶入修復表單，確認後 SQLite 僅一筆修復操作與命令收據、差異變 resolved，重新整理仍找回原命令。操作保留原 expected／actual，狀態 `verified`，verification 指向新的對帳結果。另驗 C34 預覽後訂閱來源 revision 變動：權益未重建，修復操作為 `blocked`、命令只有一筆收據；UI 在命令狀態 `succeeded` 旁明示修復未執行與 verification 原因。服務商收款與本地帳單金額不符時，對帳分類為 `MANUAL_REVIEW`；C34 僅記錄 `blocked`，UI 可返回差異詳情，C35 留下人工決議。瀏覽器以 provider SQLite 核對原金額與總收款筆數不變。其他 blocked 原因及差異類別的完整 UI 矩陣仍待驗。 |
| A24 | 本機通過 | C36–C43 的 Go 領域測試及瀏覽器路徑覆蓋連結、shadow、歷史來源回填與人工確認、讀寫切換及停止；帳戶詳情顯示 owner、比對、來源、事件與 Readiness。付款回應遺失時 UNKNOWN 阻擋讀取切換；沿原 C09 命令查證、更新權益與 shadow、重新對帳後才允許 C41／C42，且各有收據。shadow／來源完整歷史另有游標分頁頁面與 API。  C43 切寫後停止的真實瀏覽器案例證明 read／writer owner 不回退、停止事件與命令收據各一筆；其後 C05 預覽回 409 `ACCOUNT_MIGRATION_STOPPED`，沒有 C05 命令或取消排程。領域測試另驗預覽建立後才停止時，原命令失敗且沒有金融寫入。 C42 切寫後回應遺失的瀏覽器故障注入改為以相同 request key 另送受控請求、確認提交後中止原瀏覽器請求；原先 `route.fetch()` 後再 `route.abort()` 在完整套件中偶發 route 已處理錯誤。改寫後定向重複 3／3、完整套件 89／89 通過。 |
| A25 | 本機通過 | 時鐘持久化、付款／退款決策與一次性故障測試；Playwright 驗 C49 付款及退款回應遺失的票據各只一筆，且各綁定原 C09／C16 命令。真實本機瀏覽器案例驗 `crash_after_provider`：付款與退款各經 UI 建立一次性票據、由指定操作派送，provider fact 已提交而原命令回 503；故障票據綁定原命令，重新整理後繼續同一命令，provider capture／refund 仍各只一筆。退款在查證前保留 500 最小單位預留額，完成後轉為已退 500。新增瀏覽器 C48 案例：先設定假退款結果為成功，派送前 provider 仍沒有退款；C16 回應遺失後以 C17 查證，原 C16 命令沿原鍵完成，provider 退款保持唯一。另一個 Playwright 案例在 C09 等待 provider 查證時經 C46 介面把固定時鐘前進一天：調時沒有自動建立 allocation 或第二筆 capture；從命令詳情查證後，命令仍使用接收時的 business time 開通第一帳期，provider capture 與命令收據各只有一筆。Go `TestAdminFaultTicketOnlyAffectsItsSelectedPayment` 驗另一筆付款先派送仍不會領取指定票據，該付款正常成功，指定付款才進入待查證並沿原命令完成。`TestAdminPaymentDecisionRecoversAfterProviderCommitAndCapture` 驗 C47 的 provider 決策先提交、本地命令尚未完成，期間付款已 capture；重啟後沿原 control receipt 完成命令，provider 決策、capture 與管理收據各只有一筆。`TestAdminBusinessClockChangeDoesNotExpireWorkerLease` 驗持有者取得 lease 後把商務時鐘跳至 2040 年，第二個 SQLite 實例仍不能接管未到期的 wall-clock lease。 `TestBusinessClockChangeDoesNotChangeSessionDeadline` 驗同樣的商務時鐘變動不改變 session 閒置期限；`TestAdminFaultTicketOnlyAffectsItsSelectedRefund` 補付款以外的指定退款隔離。實驗控制頁現常駐標示本機假付款環境，並顯示目前時鐘與最近故障票據；更多故障／權限交錯排列留在 A30 的逐動作矩陣，不影響 A25 已列情境的本機驗收。 |
| A26 | 部分 | Playwright 驗付款／退款命令在等待查證後頁面重新整理可找回；服務重啟使舊 session 失效，重新登入後可用 /commands/:id 讀取原命令並完成查證；退款可從命令列表跨頁恢復，列表已支援游標尋找舊命令，provider 仍各只有一筆 capture/refund。C11 瀏覽器回應遺失案例在伺服器已提交更正後中斷回應；重新整理仍保留原 request key，只查回原命令，SQLite 核對唯一更正、funded grant 與收據。更多寫入情境的瀏覽器逾時及批次逐項中斷恢復待驗。 C01 與 C26 的瀏覽器回應遺失案例先由伺服器完成寫入再中斷回應；重新整理後保留待查意圖，只能用原 request key 查回，沒有第二筆命令。 新增真實瀏覽器逾時案例：C01 已在 SQLite 建立命令後，瀏覽器的原回應被保留至 20 秒寫入期限；UI 保留原 request key，重新整理後查回原命令，總數仍只有一筆。C02／C07 另驗伺服器已完成寫入、瀏覽器回應被中斷後重新整理，介面沿原 request key 找回原命令；C02 訂閱與帳單、C07 付款操作各只新增一筆。C45 批次刷新權益另驗伺服器已建立命令、工作與預覽時固定的逐項成員後中斷回應；重新載入沿原 request key 找回同一命令，工作與逐項成員均沒有第二份，並可開啟原工作詳情。C44 另用獨立的既有資料庫驗到期續約工作：伺服器建立一張 $20 續約帳單及一筆付款義務後中斷瀏覽器回應，重新載入沿原 request key 找回命令；重播後工作、成員、帳單、付款操作及命令收據各只有一份，provider capture 數量不變。C13／C30／C32 也已驗回應遺失後重新載入、沿原 key 找回同一批次工作，分別核對更正、Credit Note、capture outbox、逐項成員與收據未重複。 受控瀏覽器案例另驗 503 `COMMAND_PENDING_RETRY` 帶原 command ID 時，操作頁儲存該 ID，重新整理後直接載入 `accepted` 命令；命令詳情可沿原 ID 繼續，成功後原表單提交次數仍為一。此案例驗 UI 回應處理；provider 故障與 HTTP 證據另列 A28。 另以真實 C49 `crash_after_provider` 的 C09／C16 瀏覽器案例驗 503 後原 command ID 被保存；重整頁面後繼續原命令，付款 allocation 與退款狀態各只完成一次。  C42 切寫另驗伺服器已提交後瀏覽器回應遺失：頁面重新整理保留原 request key，查回同一命令；SQLite 中 C42 命令、成功收據與 `writer_cutover` 事件各一筆。 |
| A27 | 本機通過 | 同規模合成資料的 C45 首批／輪轉後 100 戶預覽單次 DB 量測分別為 3.67／3.10 ms（非 HTTP P95）；10,000 筆訂閱及 100,000 筆既有用量事件下，五個 HTTP client 與一個真正的 `RecordUsage` worker 同時啟動。最近一次 100 次用量列表讀取 P95 85.70 ms、最大 130.42 ms；100 筆訂閱／用量／客戶回應分別為 22,463／29,493／7,004 bytes，均低於 1 MiB。worker 提交並核對 20 筆新事件，耗時 617.32 ms；前次同設定 P95 92.47 ms。benchmark 會直接拒絕超過 500 ms 的 P95 或不足 20 筆的 worker 進度。環境為 Intel Xeon Gold 6133／Go 1.27.1；列表使用有界分頁，worker 使用短交易，沒有全量 State dump 或長時間 provider 交易。概覽與列表的受控 503、10 秒讀取逾時會保留最後成功資料並明示舊結果，成功重讀後警示消失。此狀態只表示 A27 所列本機門檻已驗，非跨環境效能承諾。 |
| A28 | 部分 | Playwright 驗 fake provider 回應遺失及 C10 查無終局 provider 證據時的 `waiting_verification`；C10/C17 Go 回歸驗沒有成功收據、保留原操作及退款預留額度。概覽與 44 條資源／詳情／故障票據讀取路徑在 DB 關閉時均回 500 `QUERY_FAILED`，不偽裝成空資料或 404；預覽及工作不存在時另回 404。C01 已驗受理前寫入失敗、受理後收據寫入失敗、執行中斷線與成功後回應遺失的原鍵恢復；C15 已驗收據故障時額度預留回滾與原鍵恢復；C17 已驗 provider 暫不可查時退款預留與原命令恢復；其他動作的中途斷線與 DB 故障矩陣待驗。新請求另有伺服器產生的 request ID，C01 真登入案例已核對錯誤回應、命令、稽核及詳情頁的 ID 關聯與秘密排除。 C01／C26 以真實 HTTP 422 驗證明確的入場前拒絕與網路回應遺失被區分：前者解鎖表單且命令數不變，後者保留原意圖供查證。 C07 另以受控 HTTP 409 驗入場前預覽失效後可重新預覽，未知或 5xx 仍保留原 key；此切片沒有宣稱完整 client 斷線矩陣。 Playwright 另驗 503 與真實 fetch 逾時的區別：保留舊表格但標示失敗，沒有把舊資料當作新結果；C01 寫入回應逾時維持原 key 查回。完整 DB 錯誤與斷線矩陣仍待驗。 關閉 SQLite 連線後，以同一 HTTP handler 驗證概覽、訂閱與用量列表、客戶列表、報價／訂閱／帳單詳情均回 500 `QUERY_FAILED`，不會把讀取故障偽裝成空列表或 404。 領域回歸另以持久化 fake provider 分別完成付款與退款後遺失回應，暫時關閉 provider 查詢：兩者都維持 `unknown`，付款無本地 allocation；恢復 provider 後沿原 key 反覆查證，付款只入帳一次，provider capture 與 refund 各僅一筆。 同一故障測試還走 C10／C17 管理命令：provider 不可查時原命令維持 `accepted`、沒有成功收據，恢復後以原命令完成；同 request key 重播仍回原命令，收據各只一筆。真實 HTTP／SQLite 故障測試以獨占鎖令 provider 查詢逾時，C10 提交回 503 `COMMAND_PENDING_RETRY`、`retryable=true` 與非空原 command ID；原命令仍為 `accepted`，沒有收據或 allocation。釋放鎖後用相同 request key 完成及重播，回同一 ID 且只有一筆收據與 allocation。修正前回應的 `command_id` 為空，修正後通過。 伺服器修正後的非空 command ID 現在被 React API client 保存，操作頁和命令詳情提供已入場 `accepted` 命令的「繼續原命令」入口；瀏覽器以受控 503、重整及恢復回應驗證沒有第二次表單提交。 真實瀏覽器與 SQLite 再驗 provider 已提交後服務中斷：C09／C16 均回帶非空原 ID 的 503，UI 沒有宣告成功，待原命令查證後才顯示完成；provider 的收款及退款均只一筆。  新增 30 條主要清單、詳情與歷史讀取路徑的關庫錯誤矩陣，均回 500 `QUERY_FAILED`；用量帳期頁另以真實瀏覽器驗 503 後保留舊估算與修訂資料、顯示更新失敗警示，成功重讀後清除警示。 新增的 Credit 詳情已納入關庫讀取錯誤矩陣：資料庫故障回 500 `QUERY_FAILED`，不同於不存在 grant 的 404。 |
| A29 | 本機通過 | Playwright 以新資料庫建置並啟動嵌入式 binary，驗登入、概覽、登出與 58 個路由；付款等待期間重啟同一 binary／SQLite，重新登入後找回原命令並查證。另一個案例先由 CLI binary 建立既有帳單與 provider capture，確認其沒有 admin schema，再啟動 admin binary 升級至 v6；登入後仍可檢視原帳單，金額與 provider capture 不變。純 API binary 已手動驗證；README 提供建置與登入指令。 |
| A30 | 部分 | [49 個動作證據盤點](web-admin-action-audit.md)列出實際共用 endpoint、逐項 UI 路徑、能力、預覽型態及交易測試入口；命令與預覽的 HTTP admission guard、命令未知欄位拒絕與零寫入已逐項驗證，Playwright 已驗 58 個路由。C12 另有完整瀏覽器抵扣及 SQLite 收據證據。49 項動作現均在盤點表列有瀏覽器測試檔案引用；仍需逐項核對情境是否真正執行該動作，以及每動作的收據／恢復、其餘輸入與衝突矩陣，A30 維持未結案。 C15 新增真實雙分頁預覽失效與重確認案例，直接核對兩筆成功收據、單筆失敗原命令及 grant 預算。  C42 的瀏覽器案例現直接執行切寫並驗回應遺失後原鍵恢復，核對唯一命令、收據與切換事件。  C43 已補真實停止與後續 C05 阻擋證據，並區分帳戶已停止與預覽來源變動的錯誤。 C07／C08／C09 已增付款建立與派送交錯的跨實例 Go／SQLite 證據；這不取代其他動作缺少的完整矩陣。 |

### A24／A28 帳戶遷移詳情讀取故障（2026-09-28）

`web/admin/e2e/account-migration-detail-read-failure.spec.ts` 由真實 C36 建立帳戶映射，定向 1／1 通過。詳情更新回 503 時保留並標示上次 owner 與來源資料，停用 C37–C39、C41–C43 的入口、Readiness 計算與權益查詢，且不顯示舊 Readiness 的「可切換」結果；重新讀取成功後恢復可用入口。回 403 時隱藏舊帳戶資料與切換操作。這只驗證遷移詳情頁的讀取狀態與入口控制，A24／A28 的完整來源與故障矩陣仍待核對。

### A23／A28 對帳詳情讀取故障（2026-09-28）

真實 SQLite／fake provider 建立 21 筆孤兒收款，從瀏覽器執行 C33 後進入對帳執行與差異詳情。`web/admin/e2e/reconciliation-detail-read-failure.spec.ts` 定向 1／1 通過：更新回 503 時保留並標示上次成功讀取的 20 筆差異與 expected／actual，停用依過期游標進入下一頁、C34 修復及 C35 人工決議入口；重試成功後恢復，403 時隱藏舊證據與操作。這補強 A23 的證據可見性與 A28 的錯誤分類；其他差異類別、阻擋原因與讀取故障矩陣仍待逐項驗收。

### A30 瀏覽器命令與收據關聯（2026-09-28）

以完整 Playwright 回歸（132／132）產生的 93 筆管理情境稽核檔，逐項核對 C01–C49 的瀏覽器請求冪等鍵、成功命令及收據，結果 **49／49**；反例資料證明同一測試中不相關的 API 成功命令不再被算入。詳見[逐動作證據盤點](web-admin-action-audit.md)。這補足瀏覽器入口與收據的關聯，尚未完成其餘逐動作來源守衛、錯誤與恢復矩陣，A30 保持部分完成。

### A23／C34 對帳修復的付款範圍（2026-09-28）

`RepairDiscrepancy` 對 `provider_amount_mismatch` 的阻擋依付款操作 ID 判斷；`pending_outbox` 先由 outbox 取得原付款操作 ID。同一付款金額不符時，原操作查證與 outbox 重試都維持 `blocked`，不增加支付服務 capture。另一付款金額不符時，原付款可沿原 key 查證或重試，差異仍留待人工處理。`lab/reconciliation_test.go` 的三個定向案例與 `go test ./lab -run '^TestS12' -count=1`（7／7）驗證此範圍。這只補強直接領域路徑的證據；A23 與 A30 的完整 HTTP／UI 逐項驗收仍未簽核。

### A28 讀取故障分類補充（更新至 2026-09-28）

價格遷移、帳單、訂閱、Credit、付款與退款詳情的瀏覽器回歸另驗暫時性更新失敗：保留並標示上次成功讀取的資料，停用依舊狀態發起的寫入入口；價格遷移的逐項查詢失敗時也停用下一頁游標。重試成功後操作恢復。若重新讀取回 403，舊財務內容立即隱藏，改顯示權限錯誤頁。新價格遷移項目端點的真實 SQLite／HTTP 測試另驗不存在回 404、錯誤查詢回 400、資料庫故障回 500，而非空列表。

快取讀取的權限邊界也延伸到價格與合約版本、命令列表與抽屜、命令詳情、批次工作及 fake provider 紀錄；403／404 不再沿用舊內容。合約報價入口與命令、工作、provider 列表的下一頁游標在暫時性讀取錯誤時停用。定向瀏覽器案例驗價格與合約詳情的 503／403、命令抽屜權限收回後不顯示結果參照、fake provider 退款權限收回後不顯示舊紀錄，以及暫停遷移頁的 403 隱藏舊狀態。

`TestFinancialReadsReportDatabaseFailureInsteadOfEmptyOrMissingData` 現涵蓋全部 19 種通用資源列表，以及客戶、訂閱、帳單、用量帳期、Credit、對帳、遷移、命令、預覽與批次工作等詳情，共 44 條關庫讀取路徑。每條均須回 500 `QUERY_FAILED`，不能把資料庫故障當成空列表或資料不存在。`TestPreviewAndJobReadsDistinguishMissingRecordsFromDatabaseFailures` 另驗資料庫正常時不存在的預覽與工作回 404 `NOT_FOUND`。實驗時鐘讀取使用已載入的記憶體狀態，因此不納入關庫矩陣。 `TestAdminCreateQuoteAtomicReceiptAndReplay` 另核對成功命令的稽核事件可連結 actor、冪等請求鍵、命令 ID 與結果 quote ID。 真實瀏覽器登入後執行 C01，從 SQLite 稽核事件確認 actor／結果引用存在，管理密碼、內部 token、CSRF 與 session cookie 均未寫入稽核 JSON。這只結案讀取故障分類切片；A28 的寫入中斷、稽核欄位及各類外部暫不可查情境仍維持部分完成。

A18 批次上限補充：C32 合約收款以 101 筆到期操作的 Go 案例證實，首批固定 100 筆並顯示 `has_more_candidates=true`；游標查詢先選第 101 筆，執行成功後下一批只取剩餘 1 筆且旗標轉為 `false`。C13 更正以 101 筆真實付款變更驗證：首批 100 筆因來源變動而維持 pending，新預覽從第 101 筆開始，再輪回衝突項。C30 以 101 個用量帳期的合成來源驗證相同輪轉；既有單筆案例確認旗標為 `false`。五種批次操作的介面共用剩餘候選提示；C45 的 Playwright 案例確認提示可見。

A18 逐項恢復補充：C32、C44、C30 新增磁碟 SQLite 關閉／重開案例，先讓第一個工作項目完成，再由 `AdminResumeAccepted` 執行剩餘項目。C32 的 capture outbox 兩項各一筆且命令收據一筆；C44 的兩戶下期帳單及付款義務各一筆；C30 的兩個用量帳期各一筆 Credit Note、已抵額各為 1 最小單位。C13 已有收據插入失敗後重開恢復案例；C45 已有首項提交後重開、第二項來源變動標成衝突的案例。伺服器關閉重開由 Go／SQLite 驗證，瀏覽器則驗五種工作回應遺失後沿原 request key 恢復。

C32 固定成員另有受控時鐘案例：第二筆合約操作在預覽時尚未到期，預覽後才到期；執行原命令只產生第一筆 capture outbox，第二筆必須由新預覽選入。這驗證批次使用預覽時的到期界線，不因確認前時間前進而擴大成員。

批次命令收據現在分別記錄 `succeeded_count`、`failed_count`、`conflicted_count`、`waiting_verification_count`、`skipped_count`。Go 回歸把五種項目狀態各放入同一工作，驗證各計數為 1 且工作總狀態為 `partial`；失敗與待查證不再被誤算成衝突。工作詳情頁持續分列六種狀態，待查證不計入已完成進度。

### A21 用量帳期讀取與差額追查

- `GET /admin/api/usage-periods/{subscription_id}/{period_index}` 在同一個 SQLite 讀取交易中提供帳期狀態、目前估算或最新已保存計價、帳務套用額與 Credit Note 彙總。未關帳估算只計入讀取時已收到的事件；介面明示它並非已列帳或已收款金額。
- `GET /admin/api/usage-periods/{subscription_id}/{period_index}/ratings?limit=20&cursor=…` 依 revision 由新到舊分頁，游標綁定訂閱與帳期；跨帳期、無效游標及超出限制的請求回 400。每筆記錄保留用量、包含量、超額量、精確分數、捨入總額與相對前次差額。
- 訂閱頁的目前及歷史帳期、用量帳期列表都能進入詳情。詳情可跳到以訂閱及帳期篩選的原始用量事件；篩選值只會當作綁定參數，非整數帳期被拒絕。
- 帳期或修訂歷史重新整理失敗時保留上次成功讀取的資料，顯示個別失敗警示與原資料查詢時間；再次讀取成功後清除警示。真實瀏覽器以 503 回應驗證兩項讀取，沒有把舊資料顯示為新結果。
- 定向驗證：`go test ./lab -run TestAdminUsagePeriodDetailEstimateAndRatingHistory`、`go test ./api/admin -run TestUsagePeriodDetailRoutesValidateAndProtectReads`、Playwright `usage keeps the original event…` 真實 SQLite 案例均通過。完整 `go test ./...` 回歸 883 個測試通過，完整 Playwright 回歸 79 個案例通過；更廣的錯誤狀態 UI 矩陣列入 A30。

## 恢復查證補充

### C13、C30、C32 批次回應遺失

三個瀏覽器案例現在都會在伺服器完成命令後中斷原 HTTP 回應，重新載入頁面，再以原 request key 查回同一命令。C13 核對延遲升級更正的工作、變更逐項成員、USD 5.34 更正及收據各一筆；C30 核對用量 Credit Note 工作、帳期逐項成員、Credit Note 及收據各一筆；C32 核對 Net30 到期收款工作、付款操作逐項成員、capture outbox 及收據各一筆。三者在重播後沒有第二份財務資料。加上既有 C44、C45 案例，五種批次工作的「已提交但瀏覽器遺失回應」路徑均已有真實瀏覽器與 SQLite 證據；這不涵蓋每種逐項中斷點或長時間 worker 恢復。

### C14 未履行升級決議回應遺失

另一個瀏覽器案例在 C14 已建立更正與 Credit 後中斷回應。重新載入後，介面沿原 request key 找回命令；SQLite 確認原命令、收據、決議及來源 Credit 各只有一筆，原有帳單來源與歷史頁仍可查。此修改後的 C14 定向測試 1／1 通過；上述 63 項完整回歸是在加入此 C14 情境之前執行。

`TestAdminRepairLookupWaitsUntilOriginalProviderEvidenceArrives` 先重現 C34 對 provider 查無終局證據時，repair 為 `waiting`、command 卻誤寫 `succeeded` 的情況；修正後 command 維持 `waiting_verification` 且沒有收據。再次查不到證據仍保持等待；原 provider key 的證據出現後，同一 repair 與 command 才完成，收據只有一筆。

受限 session 可用原命令的「重新查證」或「檢查既有收據」呼叫 `AdminVerifyExistingObligation`。它只允許查證已提交的付款／退款、原操作的 C10/C17 對帳、已在等待或完成的 C34 修復，以及有 provider control receipt 的 C47/C48；`created` 的付款、僅有 `planned` 的修復和沒有 provider control receipt 的操作不會由此路徑派送。HTTP 仍要求 session、Origin 與 CSRF。此路徑的受限權限測試見 `TestReadOnlySessionCanVerifyHeldReceiptWithoutStartingOperation`。

新增的 Playwright 暫停命令案例使用受控 API 回應檢查警示、查證按鈕與狀態更新；它只驗 UI 呈現。provider 無新派送與命令狀態的判定由上述 Go／SQLite 測試驗證。

`TestRevokedRefundDispatchVerifiesExistingProviderResult` 驗退款 provider 已提交、回應遺失而使 C16 等待查證時，撤銷能力後的恢復程序仍沿原 provider key 取得已存在的結果，將原命令完成；provider 退款與本地命令收據各只有一筆。`TestRevokedRefundWithoutProviderEvidenceRemainsWaiting` 模擬本地標為 `submitted` 後、provider 收到請求前中斷；撤銷後恢復及重複查證皆保持等待，不產生退款或收據。`TestExpiredAdminLeaseCannotStartRefundDispatch` 另驗失去 lease 的退款派送遭拒，撤銷原命令後受限查證不能啟動未提交的退款，兩個資料庫均無新外部效果。

回歸結果（2026-09-27）：最新 `go test ./... -count=1 -timeout=180s` 全部套件通過，`go vet ./...` 通過；跨實例 C12／C15 額度競爭另以 `go test -race ./lab -run TestAdminCreditApplicationAndRefundReservationCompeteAcrossInstances -count=10` 通過。價格極值修正前最近一次完整 `pnpm --dir web/admin test:e2e` 為 94／94 通過，包含 Credit 詳情、指定退款派送、命令恢復與金融來源狀態。根 Go 模組驗證涵蓋 `api`、`cmd`、`lab`；`docs/internal` 的文章實驗檔由獨立模組隔離。A01–A30 尚缺的逐項矩陣仍以各列狀態為準。
價格元件極值修正後：`go test ./... -count=1 -timeout=180s` 全套及 `go vet ./...` 通過；`TestPublishedPricesRejectUnrepresentableMinimumUpfront` 先重現可發布卻無法以一席報價的價格，再驗三種發布路徑拒絕合計溢位與有效上界。HTTP `TestPriceMinimumUpfrontBoundaryAtHTTPPreview` 驗 C18／C20／C31 的 422、有效上界與零命令。更新後的價格／合約邊界瀏覽器案例 1／1、既有 Pro／計量價格發布與企業合約瀏覽器案例 3／3 通過；其後完整 94 案例已重新通過。
合約列表 UTC 邊界與複合列鍵修正後：`go test ./... -count=1 -timeout=180s`、`go vet ./...` 通過。完整 `pnpm --dir web/admin test:e2e` 94／94 通過；最初一次完整重跑的 C21 案例因立即檢查尚未載入的「執行另一個操作」而失敗，改為等待按鈕後定向 2／2、完整 94／94 通過。最後前端列鍵及合約列表欄位調整後，再以最新資產定向驗企業合約與用量帳期案例 2／2 通過。

本輪另驗 C02／C06 停止後的預覽與舊意圖：`TestStoppedCommerceWriterRejectsAcceptQuotePreviewAndAcceptedIntent`、`TestStoppedCommerceWriterRejectsResumePreviewAndAcceptedIntent` 均驗證專屬停止原因、原鍵找回、零成功收據與零新商務事實；C02 的 Playwright 帳戶遷移案例同時驗畫面拒絕、HTTP 錯誤碼及 SQLite 零訂閱／命令。`go test ./... -count=1`（909 個測試）、`go vet ./...`、`pnpm --dir web/admin build` 與定向 Playwright 案例均通過；完整瀏覽器套件仍以最近一次 76／76 的紀錄為準。

C27–C29 另以真實 SQLite 測試預覽後來源變動：反向調整的剩餘量遭併發耗用、關帳前新增符合 cutoff 的事件、重算前新增晚到事件，舊命令均為 `failed/PREVIEW_STALE` 且沒有成功收據或多餘修訂；新預覽才套用最新用量。`web/admin/e2e/usage-preview-stale.spec.ts` 以雙分頁驗 C27–C29 的 HTTP 409；C27 原事件剩餘量不足時，新預覽明確失敗，改填剩餘量後才成功，舊調整不落庫。C28／C29 畫面分別顯示用量 20000→20010 與 20020→20030；重新確認前不新增計價修訂，確認後各有唯一收據，關帳及重算結果分別為 1 與 3 最小單位超額費。最新 Go 回歸 912 個測試及這個定向瀏覽器案例通過；全套瀏覽器測試尚未在此輪重跑。

C31／C32 補充來源競爭驗收：合約發布預覽後，另一操作者以相同 ID 發布相同內容時，原命令只增加自己的唯一收據，不新增合約版本；若發布不同內容，原命令為 `failed/PREVIEW_STALE`，不能覆寫已發布金額，重新預覽會拒絕舊內容。雙分頁 Playwright 驗 C31 的 409、畫面失效提示、既有 5000 最小單位合約與唯一成功收據。C32 預覽後若另一收款器先建立 capture outbox，原批次將該項記為 `conflicted/SOURCE_CHANGED`，outbox 仍只有一筆；同鍵重播只找回原命令。

C35 新增預覽來源變動驗收：另一審核者在預覽後先記錄決議，原命令回 `failed/PREVIEW_STALE`，沒有第二筆決議或成功收據；雙分頁瀏覽器顯示 `open → investigating` 並要求再次確認，確認後才新增決議。既有完整瀏覽器套件 81／81 通過；C35 案例是在該次套件啟動後新增，另以定向 Playwright 執行通過，尚未計入 81 項。

C39／C40 歷史來源審核新增來源競爭測試：同一 legacy invoice 在 C39 預覽後先由不同映射回填時，舊命令為 `failed/PREVIEW_STALE`，既有來源與狀態保持不變，沒有成功收據。C40 預覽後另一審核者先完成決議時，舊命令亦失效，只有一筆 `provenance_resolved` 事件；已完成的來源不能重新建立決議預覽。`web/admin/e2e/provenance-preview-stale.spec.ts` 另以雙分頁執行 C39／C40：兩次舊預覽均回 409，畫面拒絕沿舊來源重建預覽；SQLite 保留先寫入的映射與唯一決議事件／成功收據。

C41／C42 補充準備度變動驗收：預覽後新增不一致的報價 shadow，原切讀或切寫命令均為 `failed/PREVIEW_STALE`，沒有成功收據；切讀前的 owner 全留 legacy，已切讀但尚未切寫的帳戶則保留 `read_owner=commerce`、`writer_owner=legacy`。`web/admin/e2e/cutover-readiness-stale.spec.ts` 以雙分頁驗 C42 的 HTTP 409、畫面拒絕重新建立不合門檻的預覽，且沒有 `writer_cutover` 事件。

C35、C39／C40、C42 三個新瀏覽器案例另以同一命令組合執行，3／3 通過；最近一次完整瀏覽器套件仍為先前的 81／81，尚未包含這三個後加案例。

C45 權益刷新新增逐項來源變動驗收：批次預覽固定兩戶後，其中一戶由另一操作改變訂閱 revision；原批次只將該戶標為 `conflicted/SOURCE_CHANGED`，不為它重建權益，另一戶仍成功，批次只有一筆收據。`web/admin/e2e/entitlement-batch-source-stale.spec.ts` 以雙分頁真實執行 C05 與 C45，於工作詳情分別看到衝突與成功項，並以 SQLite 核對權益及收據。

目前 Playwright 列出 85 個案例；上次完整套件驗收為 81／81，之後加入的 C35、C39／C40、C42 三案合跑 3／3，C45 定向案例另行通過。85 案尚未整套重跑。

## A10 數值與 UTC 邊界補充

`TestAdminUTCRejectsPrecisionLossAndUnixNanoOverflow` 先重現 Go 時間解析接受第 10 位小數秒並截斷，以及接受超出 `int64` Unix nanoseconds 範圍的日期；修正後 C18/C20/C21 價格時間、C26 用量事件、C28 截止時間、C31 合約期間、C33 對帳時點與 C46 固定時鐘共用精確 UTC 驗證。允許 0–9 位小數秒及可表示的最小／最大 nanosecond，拒絕非法日期、偏移時區、過度精度及溢位日期。

React 表單在送出前驗證 Gregorian 日期與 `int64` 上限，數值保留字串，不經 JS `Number` 轉換。Playwright A10 案例驗非法 UTC、超過 9 位小數秒與超過 `int64` 的席次會顯示欄位錯誤，SQLite 命令筆數不變；超過 JS safe integer、仍在 `int64` 範圍的字串可通過表單驗證。C18 分母溢位在建立預覽前顯示欄位錯誤；合法的 `9007199254740993` 在預覽畫面精確顯示，尚未確認時命令筆數不變。更新後的 A10 單項 Playwright 測試通過；先前的 29 個 Playwright 案例完整回歸通過。其餘欄位與各動作的完整 HTTP／browser 邊界矩陣仍待驗。

`TestAdminRateDenominatorUsesExactPositiveInt64` 驗 C18 費率分母拒絕零、負值、指數及溢位，合法且超過 JavaScript safe integer 的值在正規化命令 payload 中維持原字串精度。`TestAdminRateDenominatorPersistsWithoutPrecisionLoss` 驗 preview 顯示原值、發布命令成功，且從 SQLite 載入的價格條款保留相同 `int64` 數值；瀏覽器詳情的顯示仍待驗。

`TestQuoteExpiryStaysWithinUnixNanoRange` 先重現固定時鐘接近上限時，15 分鐘報價期限回繞成負數仍被寫入；修正後上限內最後一個到期時點可建立，超出上限的報價不會留下資料。該測試也先重現接受報價時下一帳期終點超出儲存範圍所引發的 SQLite 錯誤；現在於財務寫入前回傳領域拒絕，不留下訂閱。`TestAdminQuoteWithOverflowingExpiryFailsWithoutReceipt` 驗 C46 固定時鐘後的 C01 在此邊界失敗且無報價或命令收據。`TestQuoteExpiryRejectsClockOutsideUnixNanoRange` 驗直接呼叫的時鐘也受保護。其他會推算未來時間的動作仍需逐一驗證。

`TestCaptureRejectsUnrepresentableActivationBeforeProviderCall` 先重現接近儲存上限時，收款已送到 provider，卻因開通後的新帳期終點溢位而使本地交易失敗；修正後待開通付款在 provider 呼叫前拒絕，付款維持 `created` 且 provider capture 為零。相同情境的 C09 預覽／命令會結束為 `failed/DOMAIN_REJECTED`，沒有 provider capture 或命令收據。`TestExistingProviderCaptureCannotOverflowActivationPeriod` 驗已存在的 provider capture 在此邊界被觀測時回傳明確領域衝突，本地 inbox、allocation 與開通不會部分寫入；既有 provider 事實仍需人工處理。其他推算未來時間的路徑仍需驗證。

`TestAdminDuplicateFaultTicketFailsWithoutStrandingCommand` 驗 C49 同一付款操作重複建票：第一張票據建立後，第二個不同鍵命令會結束為 `failed/DOMAIN_REJECTED`，同鍵重播仍找回原失敗命令。SQLite 只保留第一張票據與一筆成功控制收據；後續 C09 派送可領取第一張票據並進入待查證。這項測試只涵蓋付款票據的重複建票與派送，尚未取代 C46–C49 的完整故障、權限與恢復矩陣。

`TestAdminClaimedFaultSurvivesCrashBeforeProviderDispatch` 固定 C49 票據已綁定原 C09／C16 命令、但 provider 尚未收到請求的持久化狀態。重啟後，原命令重新領取同一票據，付款與退款都進入 `waiting_verification`；沿原命令查證後，各只有一筆 provider capture／refund 與一筆命令收據。`TestAdminRevokedDispatchReleasesClaimedFaultBeforeProviderCall` 與 `TestAdminRevokedRefundDispatchRetainsReservationAndFault` 另驗此時撤銷原命令會釋放票據，外部 capture／refund 為零、原命令沒有成功收據，退款 500 預留額不被釋放；新授權命令領取同一票據後才各產生唯一外部效果與收據。併行接管仍待驗。

`TestAdminClaimedFaultBlocksCompetingDispatchUntilOwnerResumes` 固定同一付款有兩個已受理命令，票據已綁定後建立的原擁有者。較早排入恢復清單的競爭命令不能跳過票據去呼叫 provider；啟動恢復會略過該暫時受阻命令，讓擁有者沿票據進入待查證，完成後競爭命令可沿既有 provider 事實結束，capture 仍只有一筆。`TestAdminStalePreviewReleasesClaimedFault` 以受控來源快照失效驗證原命令 `failed/PREVIEW_STALE`、零 provider capture／成功收據，票據釋放後由新預覽命令領取。這些是單程序可控交錯；真正跨程序同時接管尚待驗。

`TestAdminConflictingProviderDecisionsPreserveFirstOutcome` 對 C47 付款與 C48 退款各驗相反的管理決策：第一個成功決策在 provider 留下一筆控制收據，第二個確定失敗決策以 `failed/DOMAIN_REJECTED` 結束且沒有收據；實際派送後只有一筆成功 capture／refund。操作已達終態時再提交新的決策也會失敗，provider 控制收據仍只有第一筆。瀏覽器在既有付款重試與退款流程中實際提交相反決策，畫面顯示 `DOMAIN_REJECTED`，SQLite 驗僅第一個決策有命令收據，退款 provider 事實在 C16 派送前仍為零；兩個定向案例 2／2 通過。權限撤銷交錯及其餘 UI 錯誤呈現矩陣尚待補齊。

`TestAdminMeterRegistrationRejectsStaleConflictingSchema` 與新的雙分頁瀏覽器案例補 C19 不可變 schema 的競爭驗收：同 ID 的 `request`／`token` 計量表先各取得預覽，`token` 先發布後，舊 `request` 命令以 `failed/PREVIEW_STALE` 結束，畫面顯示 HTTP 409 並拒絕相衝突的新預覽。SQLite 的 unit 保持 `token`，兩命令僅一筆成功收據；Go 另驗成功與失敗命令沿原鍵找回、舊鍵改 payload 拒絕。這項案例定向 Go 1／1、Playwright 1／1 通過；[A30 盤點](web-admin-action-audit.md)記錄其餘邊界。

`TestAdminMeteredPriceRejectsStaleConflictingVersion` 補 C20 價格版本競爭：同 ID 不同固定金額及不同 ID 搶同一方案版號的舊預覽均以 `PREVIEW_STALE` 結束，只保留一個版本與一筆成功收據；成功／失敗命令沿原鍵找回，改 payload 使用舊鍵被拒。新增雙分頁瀏覽器案例驗 3500 先發布後，舊 3000 預覽回 HTTP 409、畫面明示失效，SQLite 仍為 3500、兩個價格元件與唯一成功收據。兩個定向案例各 1／1 通過；其餘逐項邊界仍見 [A30 盤點](web-admin-action-audit.md)。

`TestAdminCatalogSelectionRejectsStaleCompetingPrice` 與新增雙分頁瀏覽器案例補 C21 同方案、cohort、生效時點的選價競爭。兩個不同價格先各取得預覽，後確認者的舊命令回 `PREVIEW_STALE`／HTTP 409，畫面要求重新預覽；SQLite 只保留先確認的價格與唯一成功收據。Go 另驗成功／失敗原鍵重播、改 payload 使用舊鍵拒絕，以及相衝突的新預覽被拒。定向 Go 與 Playwright 各 1／1 通過；完整回歸計數仍以本文件最新紀錄為準。

## 範圍邊界

此環境只有單一管理員和本機 fake provider，未涵蓋真實支付、稅、多幣別、正式總帳、公開部署或跨區域服務。相關金融不變量以 domain 與 SQLite 測試為準，UI mock 或成功訊息不能代替資料庫證據。

## 付款建立與派送交錯回歸（2026-09-27）

- 當次驗證：`go test ./... -count=1 -timeout=180s` 959／959；`go vet ./...` 通過；四組付款交錯定向 `-race -count=2` 共 18 個案例通過；Playwright 完整套件 90／90 通過。

- `TestAdminReplacementInvalidatesOldDispatchAndCapturesOnlyNewOperation`：C07 取代未送出的操作後，已受理的舊 C09 必須回 `failed/PREVIEW_STALE`，不能留在 `waiting_verification`；新 C09 只建立一筆 USD 6000 的成功 provider capture，舊 key 沒有 capture，帳單尚欠 4000，兩筆成功命令各有一筆收據。修正前測試重現舊 C09 卡在等待查證。
- 瀏覽器 `dispatching a selected payment does not send an earlier queued operation`：先建立兩筆未派送付款，再於 C09 介面指定較晚的操作；SQLite 核對較早操作仍為 `created` 且 outbox `pending`，其 provider key 零 capture，指定操作成功且僅一筆成功收據。定向 Playwright 1／1 通過。
- 瀏覽器 `replacing a payment invalidates an older dispatch preview in another tab`：先預覽舊 C09，再由另一分頁以 C07 取代付款。舊分頁提交得到 HTTP 409、失效提示且不能再確認；SQLite 的舊 C09 無成功收據，舊 provider key 無 capture。新 C09 只對替代操作收取 1500。定向 Playwright 1／1 通過；新增兩個 C09 案例後完整套件 92／92 通過。
- `TestAdminDispatchInvalidatesEarlierReplacementPreview`：原 C09 先完成唯一收款後，較早受理的 C07 預覽回 `PREVIEW_STALE`；帳單沒有第二筆操作，C07 沒有成功收據，未清餘額為 0。
- `TestAdminRetryDispatchMakesCompetingRetryStaleWithoutDuplicateCapture`：首筆確定失敗後，C08 建立唯一 USD 10000 重試操作；新 C09 派送成功，另一個先前受理的 C08 變為 `PREVIEW_STALE` 且沒有成功收據。provider 只有原失敗和新成功兩筆 capture，帳單尚欠 0。
- `TestAdminReplacementAndDispatchCompeteAcrossLabInstances`：兩個 Lab 共用 SQLite，從同一起跑點執行 C07 與舊 C09；若替代先提交，舊 C09 失效且無 provider capture；若派送先提交，C07 失效且僅原操作收款。修正前能重現 `database is locked`；寫入前預留 SQLite writer 後，連跑 5 輪共 30 個子案例通過。
- 瀏覽器 `competing retry previews leave one obligation and dispatch its exact amount` 用同一個登入工作階段的兩個分頁持有 C08 預覽：第二頁勝出，第一頁提交收到 HTTP 409；SQLite 只有一筆重試義務、一筆 C08 成功收據，後續 C09 只對該操作建立一筆金額相同的 capture。
- 這些測試檢查本機 SQLite 及 fake provider 的交易事實。更多付款錯誤矩陣和 A30 全動作逐項簽核仍待完成。

## 退款預留與派送交錯回歸（2026-09-27）

- `TestAdminRefundReservationAndDispatchCompeteAcrossLabInstances`：兩個 Lab 共用 SQLite 同時執行 C15 預留 500 與既有 C16 退款 400。修正前重現 C16 `database is locked`；退款服務商觀察交易改為先預留 SQLite writer 後，連跑 5 輪共 30 個子案例、定向 `-race -count=2` 共 12 個案例通過。每輪 provider 只有一筆 USD 400 成功退款；第二筆預留依提交順序成功或 `PREVIEW_STALE`，額度、操作及成功收據一致。
- 最新 Go 回歸 `go test ./... -count=1 -timeout=180s` 965／965，`go vet ./...` 通過。新增兩個 C09 瀏覽器案例後的完整 Playwright 套件 92／92 通過；該完整套件啟動時尚未包含本段退款修正，因此另以最新程式定向重跑退款回應遺失瀏覽器案例 1／1 通過。
- A16 其餘退款 UI 錯誤矩陣及 A30 全動作證據仍維持部分完成。

## Credit 額度詳情（2026-09-27）

- `GET /admin/api/credits/{id}` 以已驗證 session 讀取 credit grant 來源和 `loadCreditBalance`；`AdminCreditDetail` 在同一個 SQLite 唯讀交易內讀取來源更正、釋出紀錄與目前額度，避免把不同時間的餘額拼接。不存在的 ID 回 404。
- React＋Ant Design `/admin/credits/:id` 從 Credit 列表進入，分開呈現原始、已抵扣、退款保留、已退款及可用額度；有保留額時顯示待派送／待查證說明，並可複製來源更正 ID、進入來源帳單、抵扣、預留退款及按 grant 篩選的退款列表。金額由 `Money` 以字串／BigInt 格式化。
- `TestAdminCreditDetailSeparatesAvailableReservedAndRefunded` 核對 USD 1000 grant 的初始可用 1000、預留退款 400 後可用 600、回應遺失期間保留 400、查證後已退款 400、後續帳單抵扣 200 後可用 400；`TestCreditDetailSessionGuardAndNotFound` 驗未登入與不存在的 ID。瀏覽器 `credit detail separates available reserved and refunded balances` 定向 1／1 通過；該案例另經 C48 設定第二筆退款確定失敗，C16 派送後核對保留 300 歸零、已退款仍為 400、可用回到 600，並驗同一路徑可用「執行另一個操作」重新預留，另有 `dispatching a selected refund does not send an earlier queued refund` 定向 1／1 通過。
- 來源更正 ID 由 grant 的 allocation release 關聯取得；Go 測試對照實際更正，更新後的 Credit 詳情 Playwright 定向案例 1／1 通過。A16 其他 UI 錯誤矩陣與 A30 全動作逐項證據仍需完成。

### 讀取分類與實驗時鐘回歸（2026-09-27）

完整 `go test ./... -count=1 -timeout=180s`：993／993 通過（加入商務時鐘 lease 專項測試之前）；其後 `TestAdminBusinessClockChangeDoesNotExpireWorkerLease` 與稽核引用定向測試各自通過，`go vet ./...` 與管理前端 `typecheck` 通過。關庫矩陣新增預覽、工作與故障票據讀取；時鐘從記憶體狀態讀取，故不套用 DB 故障預期。當時未重跑完整 Playwright；當時最近一次完整瀏覽器結果為 94／94。兩個定向瀏覽器案例各自通過：價格遷移 C22–C25 與帳戶遷移 C36–C38／C43；案例現逐動作核對唯一成功收據，細節記於 [49 個動作證據盤點](web-admin-action-audit.md)。

### 故障票據可見性與分頁（2026-09-27）

`AdminFaultTicketsPage` 以「待使用優先、SQLite 插入序號遞減」排序並提供有界游標；建立時間仍顯示在列表中，避免不同小數精度的 UTC 字串導致同秒排序錯誤。超過一頁可從實驗控制頁向前／向後查閱；每頁顯示觀測時間，錯誤游標回 400 `INVALID_CURSOR`。Go 測試先重現舊版在 100 張較新已使用票據後隱藏舊待使用票據，再驗修正後置頂，以及跨六頁不重複、分頁期間插入新票據不改變既有游標後方的成員；另以同秒不同小數精度的三張票據驗證插入順序。獨立 Playwright 案例以真實 SQLite 建立 21 張待使用票據，驗第一頁、第二頁與返回第一頁。這補 A11/A25 的故障票據列表切片；其他列表與情境仍依上表狀態追蹤。

### A11 有效零時點與未發布狀態（2026-09-27）

資源時間欄原先一律把整數 0 轉為 `null`，使內建 `catalog_selection.effective_at=0` 被顯示為「未知」。`TestAdminResourceTimestampZeroKeepsEpochButDraftPublishTimeIsUnknown` 先重現，再驗目錄選價與草稿價格的有效 epoch 均回 `1970-01-01T00:00:00Z`；草稿 `published_at=0` 依 `State=draft` 明確回 `null`；`basic-v1` 與 `pro-v1` 的 0 是種子資料缺少實際發布時間，也回 `null`；其他已發布版本若實際發布時點為 0，仍保留 epoch。`resource-epoch.spec.ts` 以真實 SQLite 與瀏覽器核對列表、基準價格與草稿詳情。共用資源轉換初次改動後，完整 `go test ./... -count=1 -timeout=180s` 為 1005／1005 通過；基準價格來源補正後，相關 Go 與瀏覽器定向案例及 `go vet ./...` 通過。資源列表與故障票據列表現於翻頁區明示各頁是獨立查詢，並非跨頁快照。A11 其他資源的跨頁插入與缺來源矩陣仍未全部結案。

### A11 金融資源跨頁插入補驗（2026-09-28）

`TestResourcePagesIncludeLaterInsertsWithoutRepeatingEarlierRows` 用五筆真實報價接受、收款、減額與退款預留建立六種資源。首頁載入後插入同客戶及另一客戶的新資料，再沿原 HTTP 游標逐頁讀取：報價與訂閱保留 `customer_id` 篩選，帳單、付款、Credit 與退款讀取各自完整列表。六種清單均無重複、可讀到新尾項，且每頁 `total` 反映該次查詢的來源狀態；案例連跑五次通過。這是跨頁插入切片，其他資源與更新造成的非快照變化仍待驗，因此 A11 維持部分。

### A28 寫入與回應遺失補驗（2026-09-28）

`TestCommandAdmissionWriteFailureCanRetryOriginalKey` 對命令 INSERT 注入 SQLite 故障，首次 HTTP 請求回 500 `COMMAND_ADMISSION_UNKNOWN`（`retryable: true`，須沿用原鍵），命令、報價、收據均未寫入。解除故障後原冪等鍵首次成功提交回 202，再重播回 200，兩次均指向相同的成功命令，三種紀錄各一筆。

`TestReceiptWriteFailureReturnsRecoverableOriginalCommand` 透過真實 HTTP handler 和 SQLite trigger 注入 C01 收據 INSERT 失敗。首次請求回 503 `COMMAND_PENDING_RETRY`，保留原命令 ID 與可重試資訊；此時報價與收據均為零筆。解除故障後以原冪等鍵重送兩次，兩次均取得原命令，最後僅有一筆報價、一筆收據及一筆命令。

`TestLostCommandHTTPResponseReplaysOriginalReceipt` 在 C01 成功寫入後、回應送達客戶端前關閉真實 HTTP 連線。客戶端先收到網路錯誤；重送前資料庫已有成功命令、報價與收據各一筆。原冪等鍵重送兩次均返回相同命令，三種紀錄仍各一筆。此案例覆蓋提交成功但回應遺失；下列案例另驗受理後執行中的斷線。其餘動作及 DB 故障組合仍列為 A28 未完成條件。

`TestClientDisconnectDuringAcceptedQuoteExecutionRecoversOriginalCommand` 以真 HTTP 請求提交 C01，等待原命令已受理且執行 lease 已取得，再於報價寫入受 SQL trigger 延遲的測試環境中取消客戶端請求。伺服器結束後，原命令維持 `accepted`、lease 已釋放，報價和收據均為零；移除故障後同鍵重送兩次，只得到原命令、一筆報價和一筆收據。此測試連續執行 20 次通過，直接覆蓋受理後、執行中的斷線；其餘動作及其他 DB 故障組合仍待驗。

`TestUnavailableRefundLookupKeepsReservationAndOriginalHTTPCommand` 補 C17 真 HTTP 故障：provider 暫不可查時回 503 `COMMAND_PENDING_RETRY` 與原命令 ID，退款維持 `unknown`，Credit 的 500 預留不釋放，且不產生成功收據。解除故障後原鍵重送兩次，只完成原退款與一筆收據；Credit 預留轉為已退款 500，可用額仍為 500，provider 退款僅一筆。定向測試連跑三次通過。

`TestRefundReservationReceiptFailureRetainsBudgetAndOriginalHTTPCommand` 補 C15 寫入故障：收據 INSERT 失敗時 HTTP 回 503 與原命令 ID，退款操作及額度預留整體回滾；Credit 1000 仍可用、provider 無退款。移除故障後原鍵重送兩次，只建立一筆 `created` 退款操作、預留 500 和一筆成功收據，沒有提前派送 provider。

Playwright 案例 `uncertain admission error keeps the original quote key until a successful retry` 注入 500 `COMMAND_ADMISSION_UNKNOWN`，首次沒有建立命令。頁面重新載入後仍保留原意圖；點選「查詢原操作」使用完全相同的 payload 與冪等鍵，最終只建立一筆成功命令、一筆報價和一筆收據。定向瀏覽器測試通過。

### request ID 與稽核關聯（2026-09-28）

Admin schema v8 在同一升級交易內為 `admin_commands`、`admin_audit` 加入 `request_id` 與查詢索引；既有歷史列保留 `NULL`，不偽造當時不存在的請求識別。新請求由伺服器產生 `X-Request-ID`，錯誤 JSON 的 `request_id` 與該 header 相同。命令受理時保存原請求 ID；受理、成功、失敗、等待查證及批次收據的稽核列保存當次執行請求 ID，背景恢復則沿用原受理 ID。命令詳情顯示受理請求 ID，歷史資料明示未記錄。

`TestAdminSchemaUpgradesExistingV1` 驗 v1→v8 欄位與 ledger；完整 Go 回歸 **1011／1011 通過**。Playwright 的真登入 C01 案例驗 401 錯誤 body/header 的 ID 一致、忽略客戶端偽造的 `X-Request-ID`，以及成功命令、兩筆稽核列和詳情頁的 request ID 一致，並檢查稽核內容不含管理密碼、內部 token、CSRF 和 session cookie。CLI 資料庫升級案例另驗 v8 欄位仍保留原帳單與 provider capture。

### 未知 API 路徑與方法的錯誤契約（2026-09-28）

`TestUnknownAdminAPIRoutesKeepSessionGuardAndRequestIDEnvelope` 以真 HTTP 驗未登入的未知路徑仍回 401 `SESSION_REQUIRED`；登入後同一路徑回 JSON 404 `NOT_FOUND`，不暴露 Go 路由器的純文字預設回應。已知路徑使用不支援的方法時回 JSON 405 `METHOD_NOT_ALLOWED`，保留 `Allow: GET, HEAD`。三種錯誤均含與 `X-Request-ID` 相同的伺服器請求 ID，並拒絕客戶端指定的 ID。完整 `go test ./api/admin -count=1` **650／650 通過**。既有正常路由仍由原 handler 執行。

### 全量回歸（2026-09-28）

加入 schema v8、request ID 與新金融故障案例後，`go test ./... -count=1 -timeout=180s` **1011／1011 通過**、`go vet ./...` 通過；完整 `pnpm --dir web/admin test:e2e` **97／97 通過**。A11、A28 與 A30 的其他驗收缺口仍依上表維持部分完成。


### 命令讀取失敗時的既有資料與操作（2026-09-28）

命令列表、抽屜與詳情在已有成功讀取資料後，若重新讀取失敗，保留該資料並顯示警告、上次成功讀取時間與重試入口。只有首次讀取失敗且沒有資料時，才顯示整頁錯誤。抽屜另提供明確的「更新」操作。詳情的「繼續原命令」、「重新查證」、「檢查既有收據」，以及抽屜的「重新查證」，在詳情查詢失敗時停用；抽屜尚未成功讀取單筆詳情時也不能從列表快照執行查證。

`web/admin/e2e/command-refresh.spec.ts` 使用真實瀏覽器、本機 HTTP、SQLite 與 fake provider 建立 C01 命令，再對列表及單筆 GET 注入 503，逐處核對舊資料、警告、時間與恢復；抽屜也驗證首次單筆讀取失敗時，能清楚標示來自列表的資料與觀測時間。`waiting_verification`、`accepted`、`PERMISSION_REVOKED_REVIEW` 是受控 GET 回應，用來驗證操作按鈕在資料新鮮與過期時的啟用狀態；這些狀態切換不宣稱為 SQLite 實際命令生命週期的證據。完整 Playwright 回歸 98／98 通過（於最後一處抽屜時間來源調整之前執行）；其後以最新程式碼 `pnpm build` 通過，定向案例 1／1 再次通過，前端 `typecheck` 通過。A12 仍為部分：完整鍵盤、窄螢幕、失去焦點與 API 錯誤矩陣尚未逐頁驗收。


### 批次工作進度讀取故障（2026-09-28）

`JobDetails` 在首次讀取失敗時顯示可重試的錯誤；已有成功讀取資料後更新失敗，保留工作與逐項結果，並標示上次成功讀取時間及重試入口。`web/admin/e2e/job-refresh.spec.ts` 使用本機瀏覽器和受控工作 GET 回應，逐步驗證首次 503、成功取得一筆完成／一筆待查證、再遇 503、恢復為兩筆完成的畫面轉換。這是 UI 失敗狀態證據，工作實際執行與 SQLite 收據仍由既有整合案例驗證。最新 `pnpm build` 通過；命令與工作兩個定向瀏覽器案例 2／2 通過。A12 逐頁完整矩陣仍待驗。


### 價格遷移暫停頁的過期狀態防護（2026-09-28）

暫停頁增加明確的「更新」入口。若已顯示 `active` 批次後讀取失敗，頁面保留批次資訊、顯示上次成功讀取時間，並停用「暫停未完成項目」；確認函式本身也拒絕在查詢錯誤狀態執行。已有命令結果的再次讀取失敗亦標為舊資料，避免將舊收據當成最新狀態。保留原 request key 的查詢入口，以便在命令提交結果不明時繼續查原操作。

`web/admin/e2e/pause-migration-refresh.spec.ts` 以受控單筆 GET 驗證首次 503、成功讀取 `active`、再次 503 時操作停用，以及恢復後重新啟用；此案例只證明 UI 防護，實際 C23 命令與 SQLite 收據由既有整合案例證明。最新 `pnpm build` 與命令、工作、遷移三個定向瀏覽器案例 3／3 通過；既有真實價格遷移及工作進度整合案例 2／2 通過。A12 的全頁矩陣尚未完成。


### 來源讀取故障與原命令恢復（2026-09-28）

接受報價、下期變更、立即升級、取消／恢復取消排程，以及價格遷移暫停頁，若已儲存原命令 request key，即使來源單筆 GET 失敗，仍顯示「查詢原命令」入口；查詢暫時失敗會顯示錯誤並保留原鍵再次查詢，查回命令後提供命令頁連結。來源讀取失敗頁不提供建立新預覽或提交新意圖的入口。

真實 HTTP／SQLite 瀏覽器案例 `a lost quote acceptance response recovers one subscription and invoice` 在 C02 寫入後丟失 POST 回應，重整時注入報價 GET 503，再用原 idempotency key 查回同一命令；驗唯一 C02 命令、訂閱與帳單。`source-read-recovery.spec.ts` 另以受控 503／POST 回應驗 C03、C04、C05、C06、C23 的儲存鍵、POST `action_id`、原 idempotency key 與命令連結，並驗 C03 首次查詢回 503 後再次使用同一鍵；它只證明介面接線，不代表這五個動作在該案例中建立了 SQLite 收據。最新 `pnpm build` 通過；上述真實案例、受控矩陣、既有升級與取消排程案例共 4／4 通過。其餘動作的中斷恢復矩陣仍在 A28 與 A30 待驗範圍內。


### 外部操作命令與金流結果分別呈現（2026-09-28）

C09／C16 的命令 `succeeded` 表示送出流程已完成；`result_refs.operation_status=definitively_failed` 表示實際付款／退款沒有成功。操作結果、命令詳情、列表及抽屜現在同時顯示兩層狀態，避免只看到命令成功而誤判金流成功。警告引導操作員檢查帳單餘額或 Credit 可用額度，再決定是否建立新操作。

金額邊界：退款以最小貨幣單位記錄；C15 預留占用 Credit 可用額度，UNKNOWN 不釋放保留，服務商確定失敗才釋放；成功退款轉入已退款額。退款關聯原收款 provider key 與穩定退款 key，結果查證沿原 key，不建立第二筆外部退款。這些不變式的既有證據包括 `lab/admin_refund_reconcile_test.go`、`lab/admin_refund_dispatch_interleaving_test.go` 與真實瀏覽器的退款故障流程；本次 UI 改動沒有改動金額或 provider 邏輯。

真實瀏覽器 `credit detail separates available reserved refunded balances` 驗 C16 命令為 `succeeded`、退款狀態為 `definitively_failed`、保留額歸零與可用額恢復，且操作結果、命令詳情、列表與抽屜均標示「退款失敗」。`partial payment replaces an unsent operation and retries only definitive failure` 驗 C09 對應的付款失敗警告。兩個定向案例 2／2，命令讀取故障回歸 1／1，退款 UNKNOWN／並行派送／決策恢復的定向 Go 測試 8／8，最新 `pnpm build` 通過。這些是本機 SQLite／fake provider 的證據，不代表外部結算已完成；A16 與 A30 仍有未驗完的動作矩陣。


### 用量事件的篩選游標與跨頁插入（2026-09-28）

`TestFilteredUsageEventPagesIncludeLaterInsertWithoutDuplicate` 使用真實 HTTP 管理查詢、本機 SQLite 與已付款的 Pro 訂閱，對 `usage-events` 同時指定 `subscription_id`、`source=worker`、`period_index=0` 及一筆一頁。首頁讀取後插入同訂閱的新事件，也插入另一訂閱的事件；沿原游標讀完只得到原訂閱三筆依插入順序排列的事件，沒有重複或混入另一訂閱，回傳 total 由 2 變為 3。此案例補足用量事件在跨頁插入時的 HTTP 證據；尚未代表其他資源或所有時間欄位語意已逐項驗收。完整 `go test ./api/admin -count=1 -timeout=180s` 651／651 通過，A11 維持部分完成。


### 資源抽屜與最新查詢資料同步（2026-09-28）

通用資源抽屜原先保存點開時的整筆列資料；列表重新讀取後即使付款等狀態已變，抽屜仍顯示舊快照。現在抽屜保存穩定列鍵，從當前頁資料取得列內容，提供抽屜內的更新入口與觀測時間；更新失敗明示使用上次成功讀取資料，列不再屬於當前頁時關閉抽屜。

`web/admin/e2e/resource-drawer-refresh.spec.ts` 以受控付款列表 GET 驗 `created → definitively_failed`、503 保留舊資料、恢復為 `succeeded` 及列消失後關閉。此案例驗證介面狀態同步；付款狀態轉移本身由既有 SQLite／provider 案例驗證。最新 `pnpm build` 通過，新增案例 1／1、既有資源失敗與逾時案例 2／2 通過。A11 其他資源的時間／缺失值語意及 A12 的逐頁錯誤矩陣仍未全驗。


### 目錄價格與選價的篩選游標（2026-09-28）

`TestFilteredCatalogPagesIncludeLaterInsertAndKeepUTCTime` 使用已發布的 Pro 價格版本與真實目錄選價，透過管理 HTTP API 分別對 `prices` 套用 `plan_id`／`id_prefix`、對 `catalog-selections` 套用 `plan_id`／`cohort`，以一筆一頁讀取。首頁讀取後發布並選取第三個版本，另插入其他 cohort 選價；沿原游標均只取得原篩選的三筆，依插入順序且不重複。選價 `EffectiveAt` 核對為 UTC RFC3339Nano。這補充 A11 目錄兩種資源的跨頁插入與時間序列化證據，其他資源及缺來源值矩陣仍未全驗。完整 `go test ./api/admin -count=1 -timeout=180s` 652／652 通過。

### 共用操作頁的命令讀取故障（2026-09-28）

共用操作頁在命令卡片新增「更新」入口。首次讀取命令失敗時顯示可重試錯誤；已有成功讀取資料後更新失敗，保留舊結果，明示上次成功讀取時間與重試入口。讀取失敗期間，舊的 `accepted`／`waiting_verification` 狀態不能用來繼續原命令或重新查證，舊的終態也不能用來清除命令並開始另一操作。

`web/admin/e2e/action-command-refresh.spec.ts` 以受控 C16 命令 GET 檢查三種狀態、503 及恢復後的按鈕行為；這是 UI 故障狀態證據，不代表 SQLite 命令實際轉移。新增案例 1／1、既有真實 SQLite／fake provider 的付款提交後崩潰與退款預留恢復案例 2／2 通過。A12 的逐頁鍵盤與錯誤矩陣、A28 的其他寫入及資料庫故障組合仍為部分完成。

共用操作頁在原命令回應為 `COMMAND_PENDING_RETRY`、後續讀到終態時，選擇「執行另一個操作」會清除前一命令的 submit／resume／preview 錯誤與舊預覽。修改被 422 拒絕的輸入時，也會清除該次提交或預覽的舊錯誤。兩個新增的受控 C38 HTTP 回應案例分別驗證開始另一操作及修改輸入後，不再顯示前一請求的錯誤。修正前的完整瀏覽器基線 103／103 通過；修正後 `pnpm build`、`pnpm typecheck` 與此檔定向案例 3／3 通過。完整套件尚未在此最後修正後重跑，故不將基線寫成修正後的全量結果。

### 預覽阻擋原因與停留頁面時到期（2026-09-28）

共用操作頁現在逐項顯示預覽回應的 `blocking_reasons`，存在阻擋原因時停用確認；原因解除後重新建立預覽才可確認。預覽停留在頁面期間到期時，計時更新會顯示過期提示並停用確認，重新建立預覽後解除提示。打開確認框後才到期，也會在框內最後按下確認時重新核對期限，不送出舊預覽。確認函式再次核對阻擋原因與期限；伺服器仍為命令受理及來源重新驗證的權威。

`web/admin/e2e/preview-blocking.spec.ts` 使用真實瀏覽器與本機管理服務、受控預覽 HTTP 回應驗證兩項互動，且確認阻擋及到期時沒有送出命令。這是前端契約證據；目前領域預覽的 `blocking_reasons` 均為空陣列，尚無真實 SQLite 情境產生非空原因。最新 `pnpm build` 與此檔案例 2／2 通過；既有真實 SQLite 預覽編輯與價格發布失效案例合計 3／3 通過。A12 的其餘逐頁鍵盤、窄螢幕及錯誤矩陣仍為部分完成。

### 專用付款與訂閱預覽的期限防護（2026-09-28）

建立付款、重試付款、接受報價、方案變更及取消／恢復取消五個專用頁，現在與共用操作頁使用同一個預覽期限判斷。每頁顯示 `blocking_reasons` 與到期提示，阻擋時停用確認；打開確認框後到期，也會在最後按下確認時再次檢查，不保存待送意圖或提交命令。伺服器仍在命令交易內重驗預覽與來源事實。

`web/admin/e2e/payment-preview-expiry.spec.ts` 以真實管理服務和受控 C07 預覽回應驗證阻擋原因、確認框到期、無命令提交及重新預覽，1／1 通過；共用頁預覽案例 2／2 通過。另以真實 SQLite／假服務商的接受報價、重試付款、取消／恢復與方案排程相關瀏覽器案例 6／6 回歸正常操作。這些證據不代表五頁的所有阻擋與到期組合都已逐項驗完；A12 仍維持部分完成。

### C02 接受報價的 HTTP 收據寫入故障恢復（2026-09-28）

`TestQuoteAcceptanceReceiptFailureRecoversOriginalHTTPCommand` 在隔離 commerce/provider SQLite 建立 Basic 報價及管理預覽，透過具 session、CSRF 和原冪等鍵的管理 HTTP handler 提交 C02。注入 `admin_command_receipts` INSERT 故障後，HTTP 回 503 `COMMAND_PENDING_RETRY`、`retryable=true` 和原命令 ID；此時訂閱、帳單、帳期、付款義務、capture outbox、成功收據與 provider capture 均為零，只有原命令已受理。移除故障後同鍵重送兩次，兩次都回同一個成功命令；訂閱、帳單、帳期、付款義務、待派送 outbox 及收據各只有一筆。帳單明細合計、帳單總額、付款義務及收據來源 ID／金額／幣別均與原報價一致，provider 尚未收款。同鍵再提交另一張有效報價回 409 `IDEMPOTENCY_CONFLICT`，第二張報價沒有訂閱。定向 Go 測試通過。這補足 C02 的 HTTP 層中斷及鍵衝突證據；A08／A28 的其他動作與故障點矩陣仍為部分完成。

### C12 Credit 抵扣的 HTTP 故障與回應遺失恢復（2026-09-28）

`TestCreditApplyReceiptFailureRecoversOriginalHTTPCommand` 先在隔離 SQLite 建立已付款 Basic 帳單、1000 的 funded Credit，以及同戶下期 2000 的待付款帳單。透過具 session、CSRF 與原冪等鍵的管理 HTTP handler 抵扣 500。收據 INSERT 故障時回 503 `COMMAND_PENDING_RETRY` 和原命令 ID；Credit 仍可用 1000、抵扣紀錄與成功收據均為零，原 2000 的付款操作及 outbox 保持待派送。移除故障後同鍵重送兩次只建立一筆 500 抵扣與收據，Credit 可用 500、目標帳單未清 1500；舊付款操作取消、outbox 標為 `done`，不會自動建立新的付款操作。收據的 grant、invoice、金額與幣別參照直接比對已提交事實；provider capture 仍只有來源帳單原先的一筆。

同一隔離情境另以新預覽抵扣 250，在命令成功提交後、HTTP 首個回應送達前切斷連線。客戶端先收到網路錯誤，但第二個命令、抵扣及收據已各提交一次；同鍵重送兩次都返回原成功命令。最終 Credit 已用 750／可用 250、目標帳單未清 1250、抵扣兩筆、目標付款操作仍只有已取消的原操作，provider capture 仍為一筆。C01／C02／C12／C15 的定向 HTTP 收據故障矩陣連跑三次通過。這將「收據失敗整體回滾」與「提交成功但回應遺失」分別驗證；A08／A28 其他動作的完整故障矩陣仍待補。

### C11 已收款帳單減額的 HTTP 收據故障（2026-09-28）

`TestPaidReductionReceiptFailureRecoversOriginalHTTPCommand` 使用已實收的 Basic 帳單與真實管理 HTTP handler。減額 1000 的收據 INSERT 故障時回 503 `COMMAND_PENDING_RETRY` 與原命令 ID；原始帳單總額與明細仍為 2000，沒有 correction、funded Credit、收款釋出或成功收據，provider 原 capture 保留唯一一筆。解除故障後同鍵重送兩次只產生一筆 correction、1000 的 funded Credit、一筆 1000 的原收款釋出及一筆收據；帳單原額 2000 不變，修正後義務與淨實收均為 1000，未清為零。收據的 correction ID、grant ID 和金額直接比對提交列。同鍵再送另一筆有效的 500 減額回 409 `IDEMPOTENCY_CONFLICT`，未新增財務事實。C01／C02／C11／C12／C15 的定向 HTTP 收據故障矩陣連跑三次通過。既有瀏覽器案例另覆蓋 C11 成功提交後回應遺失；A08／A28 的其他故障組合仍待驗。
### A11 退款篩選游標的跨頁插入（2026-09-28）

`TestFilteredRefundPagesIncludeLaterInsertWithoutCrossingGrants` 使用隔離 SQLite 建立兩筆 Credit grant 和多筆退款，透過具讀取 session 的管理 HTTP 查詢 `refunds?grant_id=...&limit=1`。讀完第一頁後，分別向目標與其他 grant 插入新退款；後續頁的總數更新為三，依序只返回目標 grant 的原第二筆與新第三筆，不重複且不混入其他 grant。把第一頁游標改套到另一個 `grant_id` 時，API 回 400 `INVALID_CURSOR`。定向測試以 `-count=3` 通過。A11 仍為部分完成：其他資源的篩選、時間語意與缺來源值矩陣尚未全驗。

`TestPaymentStatusFilterKeepsCursorAfterLaterInsert` 另以真實管理 HTTP 與隔離 SQLite 驗付款 `status=succeeded` 分頁：首兩筆成功付款之間插入未派送付款；讀取首頁後再插入成功與未派送付款。後續頁只返回原第二筆與新成功付款，總數由二更新為三，沒有重複或混入 `created`；把游標改套到 `status=created` 回 400 `INVALID_CURSOR`。定向測試以 `-count=3` 通過。

`TestFinancialResourceKeysRequireFinanceCapability` 先證明只有 `read` 能力的 session 可從付款列表取得 `ProviderKey`，再於 API 回應編碼前依 session 能力移除財務內部鍵。現在只讀 session 的付款項目不含 `ProviderKey`，退款項目不含 `ProviderKey`、`SourceProviderKey`、`RequestKey`；具有 `finance.adjust` 的 session 仍可讀取這些欄位。兩種 session 都保留相同資源 ID，三項 A11 定向案例合計連跑三次通過。這項限制只涵蓋付款與退款列表的上述欄位，A11 其他資源矩陣仍未全驗。

`TestCommandReadsNeverExposeIdempotencyKey` 另在真實管理 HTTP 的命令清單與詳情重現 C01 冪等鍵外洩。讀取端現在一律省略該鍵；只有 `read`、含 `finance.adjust`、含 `subscription.manage` 的 session 都可讀取相同命令 ID，但不取得原鍵。原鍵仍保存在命令資料庫供冪等重送判定，測試確認查詢遮蔽不改動儲存事實。此案例只驗 C01 一般報價；其他動作的命令讀取矩陣尚未逐一驗收。

### 價格操作重入與瀏覽器回歸（2026-09-28）

完整瀏覽器回歸第一次在重新開啟價格發布頁時卡於停用的版本 ID 欄位，當次為 88 通過、1 失敗、19 未執行。根因是表單先從 `sessionStorage` 還原舊命令 ID 並停用輸入，命令詳情非同步讀取完成後才顯示「執行另一個操作」；測試當下用不等待的 `isVisible()`，可能跳過重設。測試以延遲命令詳情 GET 固定重現失敗，改為在欄位停用時等待並點擊重設按鈕後，定向案例通過。第二次完整瀏覽器回歸為 **108／108 通過**。其後命令讀取一律省略冪等鍵的改動，以目前程式碼重建前端並定向驗命令讀取失敗、詳情輪詢離頁停止及命令列表游標，**3／3 通過**；這三項是該後續改動的瀏覽器證據，不把較早啟動的全套執行當作其單獨證明。

### Credit 抵扣與退款額度的限定範圍複查（2026-09-28）

本次只檢查 funded allocation release 所產生的 Credit grant、跨帳單抵扣、退款預留與退款結果觀測。`credit_grants` 的來源須對應已實收付款與釋出紀錄；`CreditBalance` 將已抵扣、待定退款（`created`／`submitted`／`unknown`）及成功退款分別扣除，確定失敗才釋放原保留額。抵扣另檢查同一客戶、幣別、帳單未清額及未送出的舊付款操作；退款預留在同一 SQLite 交易重新檢查可用額，資料庫 trigger 也約束抵扣與退款的共享總額。退款查證沿原 provider key 對應原操作，未知結果仍佔用額度。`TestAdminCreditApplicationAndRefundReservationCompeteAcrossInstances`、`TestAdminRefundReservationAndDispatchCompeteAcrossLabInstances`、`TestAdminCreditApplicationAndUnknownRefundShareFundedBudget` 在隔離雙資料庫與假服務商下以 `-count=3` 執行，合計 24 項通過。這些路徑未發現已驗證的金額缺陷；測試只證明模型化服務商與所選交錯，沒有驗證外部支付服務商的真實回應或結算證據。

### C18 價格發布的回應遺失恢復（2026-09-28）

`lost C18 publish response recovers one price and receipt with the original key` 經真實管理 HTTP 完成價格發布後切斷首次 202 回應。介面保留原操作意圖；重整後以原 request key 找回成功命令。隔離 SQLite 直接核對 C18 命令、收據與價格版本各一筆，三個價格元件存在，收據中的版本 ID 與 checksum 對應已發布價格，且付款操作數量沒有增加。定向 Playwright 案例通過。

`TestCatalogPublishReceiptFailureRecoversOriginalHTTPCommand` 另以隔離 SQLite trigger 令收據 INSERT 失敗。真實管理 HTTP 回 503 `COMMAND_PENDING_RETRY` 和原命令 ID，價格、元件與收據均未提交；移除故障後同鍵重送兩次只建立一筆價格、三個元件、一筆收據，收據 checksum 對應已發布版本。改 payload 重播回 409 `IDEMPOTENCY_CONFLICT`，沒有新的財務事實。定向測試以 `-count=3` 通過；[逐動作證據盤點](web-admin-action-audit.md)已加入 C18 執行鏈及剩餘限制。

## 專用操作頁命令結果的重整恢復（2026-09-28）

`useStoredCommandID` 依管理 actor、動作與目標，把最近一筆命令 ID 保存在同一分頁的 `sessionStorage`。共用操作頁與專用的報價、訂閱排程、付款、用量、價格遷移頁均以此 ID 重新查詢伺服器命令；瀏覽器儲存的只是查詢指標，命令狀態、收據與金融結果仍以伺服器資料為準。完成或失敗後，介面提供明確的「另一筆／繼續操作」入口，清除本頁指標並要求重新預覽；接受報價、重試付款與暫停遷移的失敗結果也可重新進入操作。

`web/admin/e2e/admin.spec.ts` 直接驗證 C01 建立報價與 C05 排程取消在頁面重整後仍顯示原命令，C05 檢視原結果後可依最新訂閱狀態執行 C06。其他既有並行、變更報價與用量情境改由介面明確啟動下一筆操作；定向重跑通過。這些證據只涵蓋指定的重整與後續操作路徑，A01–A30 未完成項目與逐動作故障矩陣仍依驗收計畫追蹤。

完整 Playwright 回歸：109／109 通過（`pnpm --dir web/admin exec playwright test`，15.7 分鐘）；`pnpm --dir web/admin build` 與 `go test ./...` 亦通過。這是目前工作樹的回歸結果，並非 A01–A30 全項簽核。

### Fake provider 獨立狀態與分頁（2026-09-28）

`GET /admin/api/lab/status` 分別讀取業務時鐘、待用故障票據數與獨立 provider 資料庫計數，並回傳 provider 自己的觀測時間；它不宣稱跨兩個資料庫的原子快照。`GET /admin/api/lab/provider-captures` 與 `/provider-refunds` 只供 `lab.control` 能力讀取，支援 1–100 筆上限、狀態篩選與綁定資源及篩選條件的游標。金額以字串傳輸，provider key 僅出現在有權限的診斷頁。

`TestAdminProviderPagesKeepIndependentCursorAndExactAmounts` 驗證新 provider 事件不移動已讀頁、refund 來源與大額金額；`TestProviderReadsRequireLabCapabilityAndPreserveMoney` 驗證 401／403、游標跨資源與跨篩選拒絕、精確 JSON 金額及資料庫故障。`provider-state.spec.ts` 以真實瀏覽器、雙 SQLite 驗狀態數、分頁、篩選、退款來源與大額顯示。
