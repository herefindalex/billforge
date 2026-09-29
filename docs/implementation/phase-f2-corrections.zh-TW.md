# F2：發票更正、已收款 credit 與退款保留

[English](phase-f2-corrections.md) | **繁體中文** | [简体中文](phase-f2-corrections.zh-CN.md)


日期：2026-09-26。這是 S11 的本機 Go＋SQLite 切片，使用獨立 SQLite 檔案模擬支付服務。金額單位皆為貨幣最小單位（USD cents）。它驗證下列帳務不變量，不代表已具備正式會計或支付服務能力。

## 金額模型與事實來源

已定稿的 `invoices.total_minor` 不修改。每次減額更正寫入不可變的 `corrections`，記錄更正前後應收、原因、請求鍵與時間。已收付款保留在 `allocations`；若已收金額超過新應收，`allocation_releases` 逐筆指向原付款操作，並各自產生同額的 `credit_grants`。這條來源鏈讓可用 credit 只來自實際已收的款項。

```text
應收 = 原發票金額 − 累計減額
淨已付 = 原收款分配 − 已釋放分配 + credit 應用
未付 = 應收 − 淨已付

credit 可用 = grant 金額 − 已應用 − 已保留退款 − 已成功退款
```

`Balance` 與 `CreditBalance` 會計算這些數字並拒絕負值或超額狀態。`Snapshot.AllocatedMinor` 仍是原始收款分配的**總額**；查看更正後的應收和淨已付應使用 `Balance`。多次減額更正逐次保留各自的金額與來源。完全免除應收、增加發票金額、撤銷更正與 chargeback 尚未實作。

## 操作與狀態

1. `PostReduction` 用穩定請求鍵去重。若付款操作已送出或結果為 UNKNOWN，拒絕更正；未送出的操作會在同一個本地交易中取消，避免以舊金額扣款。更正後若初始發票已結清，訂閱在同一交易中啟用，權益投影可由 `RefreshEntitlements` 重建。
2. `CreatePayment` 可為剩餘應收建立部分付款。它可替換結帳時建立、尚未送出的初始全額操作；若另一個由呼叫者建立的操作仍待送出，會拒絕新操作，保留原請求鍵與操作身分。
3. `ApplyCredit` 僅允許同客戶、同貨幣、非來源發票的當期續約帳單，且不得超過 grant 可用額或該帳單未付額。它同樣先處理尚未送出的舊金額付款操作；已送出或 UNKNOWN 時拒絕。成功後加入權益重建 outbox。
4. `ReserveRefund` 先在本地保留 grant 預算，再以穩定 provider key 向獨立 fake provider 退款，且帶上原始 capture key。失去回應時保持 UNKNOWN 與預算保留；查詢或可信 webhook 確認成功後轉為已退款，確定失敗則釋放保留。重送請求鍵只回傳原操作，變更金額會衝突。

SQLite 約束與 trigger 防止減額超過原發票、分配或釋放超額、沒有來源的 grant、跨客戶 credit 應用、超過目標發票未付額，以及 credit 應用與退款保留合計超過同一 grant。Fake provider 另限制同一 capture 的累計退款不超過實收金額。外部結果與本地帳務不共用交易；崩潰與 UNKNOWN 必須以原操作鍵查詢和收斂。

## S11 金額 oracle

原發票 $100，更正後應收 $80：

| 已收 | 更正後未付 | 新產生的已收款 credit |
| ---: | ---: | ---: |
| $0 | $80 | $0 |
| $60 | $20 | $0 |
| $100 | $0 | $20 |

如果 $20 grant 已在下一期應用 $10，最多只能退款 $10。退款處於 UNKNOWN 時，這 $10 繼續占用 grant；確定失敗才可重新使用。另一個情況是已收 $60、發票減額至 $60：無 credit，應收結清，初始訂閱和權益啟用。

## 驗證證據

- `go test -count=1 ./...`：涵蓋上述三種付款位置、部分付款、退款成功／失敗／UNKNOWN、請求鍵重播、跨客戶拒絕、直接 SQL 超額插入拒絕、兩個資料庫連線同時保留的預算上限，以及 F1／E 回歸。
- `go test -count=10 ./lab -run 'TestRefundFailureReleasesReservationAndConcurrentRequestsStayBounded|TestS11NewPaymentDoesNotSilentlyCancelCallerOperation'`：重複檢查退款競爭與待付款操作身分。
- `go test -race -count=1 ./lab -run 'TestRefundFailureReleasesReservationAndConcurrentRequestsStayBounded|TestS11NewPaymentDoesNotSilentlyCancelCallerOperation'` 與 `go vet ./...`：檢查併發測試及靜態問題。
- `go run ./cmd/lab correction-demo`：Basic $20 收款，減額 $4；下一期應用 $2、退款 $2，最後 grant 可用 $0。輸出的 JSON 同時列出兩張發票的 balance、退款狀態與 grant balance。

主要程式在 `lab/corrections.go`、`lab/money.go`、`lab/refunds.go`，資料庫約束在 `lab/lab.go`，fake provider 在 `lab/provider.go`；回歸測試在 `lab/corrections_test.go` 與 `lab/corrections_edges_test.go`。

## 刻意保留的邊界

這個切片只處理已定稿固定價格發票的**減額**，沒有 S07 計量計價、S09 價格版本切換、稅、匯率、按比例計費、收入認列、總帳過帳、對帳批次或營運審批流程。信用額度只能用於本機模型中的後續續約；退款目的地與實際資金到帳仍由 fake provider 模擬。權益採可重建投影，需執行 `RefreshEntitlements`，目前沒有背景排程保證其延遲。效能、可用性和跨部門流程仍未以真實工作負載驗證。
