# Billforge 商務系統正確性實驗室

[English](README.md) | 繁體中文 | [简体中文](README.zh-CN.md)

Billforge 是本機的商務系統正確性實驗專案。它用 Go 與兩個獨立的 SQLite 資料庫演練定價版本、訂閱與合約、付款與權益、用量關帳、更正、退款、對帳及帳戶遷移。設計背景見[平台計畫](docs/commerce-lab-plan.md)與 [A–D 設計推演](docs/design/README.md)；實作證據見 [MVP 完成追蹤](docs/implementation/mvp-completion-tracker.md)。

## 目前狀態

本機 CLI、API，以及以 React 與 Ant Design 建置的 Web Admin 已可使用。Web Admin 的 A01–A30 驗收仍在進行；[驗收紀錄](docs/implementation/web-admin.md)區分已驗證案例與剩餘檢查。

## 這個實驗室驗證什麼

- 已發布的價格與合約保留版本，後續變更不會默默改寫既有應收義務。
- 管理操作使用預覽、來源版本檢查、冪等鍵及命令收據。時鐘、支付服務決策與故障票據等實驗室控制動作，在受理新命令前也必須取得伺服器預覽。付款或退款派送預覽失效時，Web Admin 會呈現原預覽與目前操作狀態，供管理員核對。
- 商務資料庫與模擬支付服務資料庫彼此獨立，可跨越外部系統邊界測試重試、回應遺失與對帳。
- 對帳修復會將支付服務金額不符的檢查限定在待修復的付款；該付款會被阻擋，但其他付款仍可沿原操作恢復。若瀏覽器遺失修復成功的回應，管理員可用原 request key 查回命令，不會重複執行修復。

## 需求與測試

需要 Go 1.27、CGO 與 C 編譯器；Web Admin 另需 Node.js 與 pnpm。端到端測試還需要 Python 3，以及 Chrome／Chromium 或 Playwright 安裝的 Chromium。付款使用本機 fake provider，不連接真實支付服務。

```sh
go test ./...
pnpm --dir web/admin install --frozen-lockfile
pnpm --dir web/admin build
pnpm --dir web/admin test:e2e
go build -tags admin_ui -o /tmp/billforge-admin ./cmd/lab
```

若要確認 C01–C49 每項管理操作都由瀏覽器送出，且產生成功的命令收據，請在執行端對端測試時記錄驗收檔案：

```sh
audit_file="$(mktemp /tmp/billforge-action-cases.XXXXXX)"
BILLFORGE_E2E_ACTION_CASE_AUDIT="$audit_file" pnpm --dir web/admin test:e2e
node web/admin/e2e/audit-action-cases.mjs "$audit_file"
```

## CLI 與本機 API

```sh
go run ./cmd/lab
go run ./cmd/lab menu ./billforge-data/commerce.db ./billforge-data/provider.db
go run ./cmd/lab serve ./billforge-data/commerce.db ./billforge-data/provider.db 127.0.0.1:8080
go run ./cmd/lab demo
go run ./cmd/lab renewal-demo
go run ./cmd/lab correction-demo
go run ./cmd/lab demo /tmp/billforge-commerce.db /tmp/billforge-provider.db lost_response
go run ./cmd/lab demo /tmp/billforge-crash-commerce.db /tmp/billforge-crash-provider.db crash_after_provider
```

互動式選單可執行操作並查詢目前狀態，見 [CLI 使用說明](docs/implementation/interactive-cli.md)。`serve` 僅接受 loopback 位址；部分內部 HTTP 操作需設定 `BILLFORGE_INTERNAL_TOKEN`，見 [v1 API 說明](docs/implementation/phase-p2-v1-api.md)。

## Web Admin

複製 `.env.example` 為 `.env`，設定 `BILLFORGE_ADMIN_USERNAME` 與 `BILLFORGE_ADMIN_PASSWORD`（12–72 bytes）。密碼僅放在伺服器環境或 `.env`，不可使用 `VITE_` 前綴。限制 `.env` 僅供本機使用者讀取。建置前端後，以兩個**不同**的持久化資料庫檔案啟動：

```sh
pnpm --dir web/admin install --frozen-lockfile
pnpm --dir web/admin build
go run ./cmd/lab admin ./billforge-data/commerce.db ./billforge-data/provider.db 127.0.0.1:8080
```

開啟 `http://127.0.0.1:8080/admin/`。管理伺服器僅綁定明確的 loopback IP，且請求的 Host 必須與監聽位址一致。若要使用其他環境檔，將 `--env-file path` 放在資料庫路徑之前。程序環境變數優先於檔案值。

合約列表可開啟合約版本詳情，查看已發布條款、相關報價與訂閱。詳情頁的報價及訂閱各最多顯示 20 筆；使用「查看全部報價」或「查看全部訂閱」可開啟依該合約版本篩選的分頁列表。也可從詳情頁建立已預填客戶與合約版本的報價。

「批次工作」頁面列出近期工作、逐項計數與詳情連結；游標分頁可避免新工作插入後移動已讀頁面。

「價格遷移」詳情顯示由伺服器計算的各項狀態總數。逐項表格每頁最多讀取 20 筆，支援依狀態篩選，並以精確的最小貨幣單位顯示金額。更新失敗時會以警示標示上次成功讀取的資料，並停用依賴舊批次狀態的操作。套用或略過項目、恢復批次後，請更新詳情以查看最新計數並重新分頁。

帳單、訂閱、Credit、付款、退款、價格及合約詳情也遵循相同的讀取失敗規則：暫時性故障會標示快取資料並停用依賴該資料的操作；權限遭拒時會隱藏快取資料。

共用操作表單與訂閱方案變更的命令結果也遵循此規則。暫時無法讀取時，介面會以警示標示上次成功讀取的結果，並停用依賴命令狀態的操作。權限遭拒時，快取的命令結果會隱藏；重新讀取成功後才會再次顯示。

具備 `lab.control` 權限時，「實驗控制」可查看 fake provider 狀態，以及分頁的收款與退款紀錄。這些資料來自獨立的本機資料庫；API 金額仍以精確的最小貨幣單位字串回傳。

若要限制管理員能力，可設定 `BILLFORGE_ADMIN_CAPABILITIES=read`，或提供其他以逗號分隔的能力。未設定時，本機管理員具有完整權限。能力變更需重啟；恢復中的命令會依新權限與既有外部義務重新判定，詳見 [Web Admin 實作紀錄](docs/implementation/web-admin.md)。

若要將前端資產嵌入二進位檔，先建置前端，再執行上方的 `go build -tags admin_ui`。不帶此 tag 的開發執行會從 `cmd/lab/adminassets/dist` 讀取建置產物。完整操作、升級與恢復流程見 [Web Admin 使用與驗收紀錄](docs/implementation/web-admin.md)。

此專案未涵蓋真實支付、稅、多幣別、正式總帳或公開部署。
