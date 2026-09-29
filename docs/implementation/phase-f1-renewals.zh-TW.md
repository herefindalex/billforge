# F1. 月續約、失敗收款與權益寬限

[English](phase-f1-renewals.md) | **繁體中文** | [简体中文](phase-f1-renewals.zh-CN.md)


狀態：Go＋SQLite 本機切片，對應 [S02／S03 設計](../design/03-scenarios-and-reconciliation.zh-TW.md)。已執行 `go test ./...`、`go vet ./...` 與 `renewal-demo`；仍不是完整 Commerce MVP 或生產保證。

## 持久事實與時間政策

- `billing_periods` 保存每個訂閱的 `period_index`、`[period_start, period_end)`、`due_at`、`invoice_id`，同訂閱／index 與同訂閱／start 各自唯一。初購是 index 0。新一期只有前一期 invoice 已全額分配、訂閱有效，且 worker 在該期開始後的 7 天內執行時才產生。
- 週期錨點是初次**確認付款並啟用訂閱**時的 UTC 月日與時分秒；接受 Quote 到確認收款之間的等待不消耗服務月份。31 日遇短月截至月末，之後回 31 日；例如 2027-01-31 → 02-28 → 03-31 → 04-30。續約讀訂閱釘選的 PriceVersion，不從現在的 CatalogSelection 重新選價。已生效帳期不可回寫。
- 續約 invoice 於期初核定，固定費 $20 預付，`due_at=period_start`。付款操作有自己的固定 provider key／payload；確定失敗不改原操作，而由帶 request key 的 `RetryFailedPayment` 建立新 operation。相同 request key 回原 operation；不同 key 在尚有未決／成功操作時拒絕。
- Subscription 的商務狀態仍為 `active`。權益投影用當前帳期、invoice allocation、7 天 grace 與時鐘計算：本期已付 `active`；未付且在 grace 內 `grace`；grace 屆滿且無待查付款 `suspended`。`submitted/unknown` 超過 deadline 時暫留 `grace`，理由是 `payment_verification_pending`，直到查原 provider 操作或人工處理。超出帳期且沒有新的可服務期間則 `suspended`。
- 續約 worker 到達或晚於 grace 截止才執行，不補開可能未提供服務期間的全額票據，而寫唯一的 `renewal_holds`。已結束帳期的失敗付款不能直接重試收全額。續約首次付款或失敗後新操作若晚於 grace 截止才送出，也停止為 `late payment requires service correction review`；本切片只自動處理截止時刻以前（含截止瞬間）的全額補款。未付前一期也不自動產下一期預付帳單。

## E → F1 資料升級

舊 E schema 曾限制每訂閱一張 invoice、每 invoice 一個 PaymentOperation。`Open` 先在交易內複製這兩張表，移除限制並保留既有 ID／行資料；再補建 index 0 的 billing period、權益來源欄位及新 trigger。fake provider 舊的成功 capture 也升級為可記錄確定失敗的結果表。升級後執行 `foreign_key_check`。測試用舊形狀的 SQLite 檔案驗證原 invoice line、operation 與 provider 結果仍可恢復，且舊訂閱能續約。舊 E 資料沒有獨立保存實際初次開通時刻，回填只能以 `created_at` 作錨點；若舊訂閱曾延遲開通，必須另行查證與更正，不能宣稱回填已恢復真實服務期間。

## 可重現的證據

```sh
go test ./...
go vet ./...
go run ./cmd/lab renewal-demo
```

`renewal-demo` 的四個狀態是：初期收款成功／權益 active；10 月續約確定失敗／grace；第 7 天到期／suspended；用新 operation 補款／active。輸出包含兩期發票和兩個 provider key 的來源身分。

| 測試 | 驗證點 |
| --- | --- |
| `TestS02MonthEndAnchorAndIdempotentRenewal` | 01-31 → 02-28 → 03-31 → 04-30、同一期 worker 重跑不重複產票、付費後權益恢復。 |
| `TestDelayedInitialConfirmationMovesServiceAnchor` | 9/1 發起且 provider 已收、9/3 才確認並啟用，首期為 9/3–10/3；不在 10/1 提早續約。 |
| `TestS03FailedRenewalGraceSuspensionAndRecovery` | 確定失敗的原操作留存，重開 DB 後 grace 到期暫停；帶穩定 request key 的新操作補款且只分配一次。 |
| `TestUnknownRenewalRetainsVerificationGrace` | provider 已收但本地 UNKNOWN，超過 7 天不當作失敗；查原操作後成為 active。 |
| `TestMissedRenewalGraceCreatesHoldInsteadOfBackBilling` | 遲到 worker 不補收未提供期間，重跑只留下同一 hold。 |
| `TestFailedPeriodCannotBeChargedAfterItEnds` | 帳期結束後禁止用原月全額重試。 |
| `TestLateFullAmountDispatchRequiresReview` | grace 後尚未送出的全額續約操作不得首次扣款，避免收取未提供期間。 |
| `TestUpgradeStageEDatabasesPreservesFacts` | 舊 schema 的發票行、付款身分與 provider 結果保留，補建帳期後仍可續約。 |

## 下一個實作邊界

這個切片只有 Basic 固定費，沒有更正、credit／refund、晚到用量、合約或 cohort 遷移。`renewal_holds` 是可稽核的停止訊號，尚無人工解除與差額計算流程；`payment_verification_pending` 也需要排程查詢與停滯告警。若付款已確認但權益 projector 停滯，實際服務比訂閱啟用晚，還需要服務延遲更正；目前只記錄 pending 投影。下一步應先做不可變更正與 funded credit 的來源／保留額，再加入用量關帳和 PriceVersion 的完整元件計價。
