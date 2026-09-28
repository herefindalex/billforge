# Billforge Commerce Correctness Lab

Billforge 是本機的商務系統正確性實驗專案。它用 Go 與兩個 SQLite 資料庫演練定價版本、訂閱與合約、付款與權益、用量關帳、更正、退款、對帳及帳戶遷移。設計背景見 [平台計畫](docs/commerce-lab-plan.md)與 [A–D 設計推演](docs/design/README.md)；實作證據見 [完成追蹤](docs/implementation/mvp-completion-tracker.md)。

## 需求與測試

需要 Go 1.27、CGO、C 編譯器；Web Admin 另需 Node.js 與 pnpm。`test:e2e` 還需要 Python 3 及 Chrome／Chromium，或安裝 Playwright 預設瀏覽器。支付服務使用本機 fake provider，不連接真實支付系統。

```sh
go test ./...
pnpm --dir web/admin install --frozen-lockfile
pnpm --dir web/admin build
pnpm --dir web/admin test:e2e
go build -tags admin_ui -o /tmp/billforge-admin ./cmd/lab
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

互動式選單可操作與查詢當前狀態，見 [CLI 使用說明](docs/implementation/interactive-cli.md)。`serve` 僅接受 loopback 位址；部分內部 HTTP 操作需設定 `BILLFORGE_INTERNAL_TOKEN`，見 [v1 API](docs/implementation/phase-p2-v1-api.md)。

## Web Admin

先複製 `.env.example` 為 `.env`，設定 `BILLFORGE_ADMIN_USERNAME` 與 12–72 bytes 的 `BILLFORGE_ADMIN_PASSWORD`。密碼僅放在伺服器環境或 `.env`，不要使用 `VITE_` 前綴。建置前端後，以兩個**不同**的持久化資料庫檔案啟動：

```sh
pnpm --dir web/admin install --frozen-lockfile
pnpm --dir web/admin build
go run ./cmd/lab admin ./billforge-data/commerce.db ./billforge-data/provider.db 127.0.0.1:8080
```

開啟 `http://127.0.0.1:8080/admin/`。首次登入前應確認 `.env` 僅允許本機使用者讀取。`admin` 僅綁定明確的 loopback IP，且請求的 Host 必須與監聽位址一致。可用 `--env-file path` 指定另一個環境檔，放在資料庫路徑之前。已設定的程序環境變數優先於檔案值。

若要限制管理員能力，可選設 `BILLFORGE_ADMIN_CAPABILITIES=read`，或加入逗號分隔的其他能力；未設定時維持完整本機管理權限。能力變更需重啟，恢復中的命令會依新能力及既有外部義務重新判定，詳見 [Web Admin 實作紀錄](docs/implementation/web-admin.md)。

要將前端資產嵌入二進位檔，先執行前端 build，再執行上方的 `go build -tags admin_ui`；不帶此 tag 的開發執行會從 `cmd/lab/adminassets/dist` 讀取建置產物。完整操作、升級和恢復流程見 [Web Admin 使用與驗收紀錄](docs/implementation/web-admin.md)。

此專案未涵蓋真實支付、稅、多幣別、正式總帳或公開部署。
