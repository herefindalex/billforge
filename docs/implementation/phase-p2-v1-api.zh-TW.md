# P02：兩種 consumer 與 v1 API 相容

[English](phase-p2-v1-api.md) | **繁體中文** | [简体中文](phase-p2-v1-api.zh-CN.md)


本機 API 以 `go run ./cmd/lab serve commerce.db provider.db [127.0.0.1:8080]` 啟動，只接受明確的 loopback 位址。`BILLFORGE_INTERNAL_TOKEN` 設定內部操作憑證。這是本機驗證入口，沒有完整使用者身分驗證或外部網路部署設定。

| 契約 | 行為 |
| --- | --- |
| `POST /v1/quotes` | 公開價格或受內部憑證保護的合約報價；回固定、席次、內含量與超額費率、版本 checksum、現在應付、未來固定承諾、有效期限及必要 client 能力。若指定訂閱、mode 與 expected revision，建立綁定該訂閱的變更報價。 |
| `POST /v1/subscriptions` | 使用 quote ID、fingerprint、`Idempotency-Key`；檢查必要 client 能力，再建立訂閱及付款操作。合約接受另需內部憑證。 |
| `POST /v1/subscriptions/{id}/changes` | 只接受綁定原訂閱及 revision 的報價；在建立排程／期中變更的同一資料庫交易中確認釘選價格、席次及 quote expiry。 |
| `GET /v1/subscriptions/{id}` | 顯示實際價格、revision、排程意圖、發票、付款及權益來源 revision；權益投影未跟上時顯示 pending。內部憑證額外顯示 provider key 與請求鍵。 |
| `GET /v1/entitlements/{beneficiary}`、`GET /v1/invoices/{id}` | 查權益狀態及來源，或核定帳單 line、更正及餘額。此 MVP 將 beneficiary 對應到 customer ID，feature 為通用 `service`。 |
| `POST /v1/usage-events` | 每個事件獨立返回 accepted／rejected；核對 tenant、source、meter、時間與事件身分。 |
| 內部 `POST /v1/contracts`、`/v1/migrations`、`/v1/repairs` | 需內部憑證；沿用同一領域服務發布合約、預覽或建立遷移批次、執行指定差異修復。 |

變更報價的 `recurring_committed_minor` 是目標方案的整期費用。下期排程的 `due_now_minor` 為 0；期中升級的 `due_now_minor` 是依報價時刻計算的按比例估值，`due_now_estimated=true`。執行變更時會在同一交易中重新計算實際應付金額，回應中的 `pending_amount_minor` 才是新建付款義務的金額。報價席次與執行席次必須一致。

v1 既有 client 已知 `fixed`、`per_seat` 與 tasks 用量。新 meter 或 Net30 合約會在報價附上 `required_client_capabilities`。接受報價時必須透過 `X-Client-Capabilities` 宣告對應能力；未宣告回 `409 CLIENT_CAPABILITY_REQUIRED`，且不建立訂閱。新 meter 的費率仍以原始分子／分母及 meter ID 呈現，不偽裝成固定費。內部合約操作另外要求 `X-Lab-Internal-Token`。

API 合約測試覆蓋 Basic 購買與重播、付款 UNKNOWN 後的 pending 權益、確認後開通、發票與權益查詢、AI tokens 舊 client 拒絕與新 client 接受、Net30 的應付時點與能力拒絕、綁定 revision 的訂閱變更、報價與執行席次不一致、已購或過期報價重用、排程與期中變更的應付時點、報價後未來目標價改變的拒絕、grace 與更正讀取。

限制：變更報價使用當前可售價格，若期界有另一個有效價格，命令會明確衝突並要求重新報價。API 不提供稅、完整 beneficiary／billing account 分離、正式授權、速率限制或跨服務部署。`as_of` 是本機時鐘觀測值；需搭配來源 revision 解讀權益投影。
