# F3：Pro 席次價格與下期訂閱排程

日期：2026-09-26。這個切片完成 S06 的下期升降級、期末取消及取消前恢復，並建立後續用量／價格遷移共用的價格元件基礎。

## 已落地的行為

- 內建 Pro v1 為每期固定 $50 加每付費席 $10；五席的前付費發票為 $100，`fixed` 與 `seats` 分別成為不可變 invoice line。Basic 維持 $20。Pro v1 同時記錄 20,000 tasks allowance 和每 task $0.001 的後付費元件，但本切片**尚未產生用量發票**。
- `PriceVersion` 具有 `draft/published` 狀態和有效區間；已發布版本及其元件由 SQLite trigger 阻止原地修改。報價固定版本、席次、金額與 fingerprint。舊資料庫開啟時補欄位與初始 assignment；原 Basic 訂閱金額和付款身分保持不變。
- `ScheduleNextPlan` 與 `ScheduleCancel` 使用預期 subscription revision、穩定 request key 與單一待生效排程約束。下一期價格版本在排程時選定；當期 plan 不提前改動。同一邊界的取消與方案變更互斥。
- `ResumeCancel` 僅在取消生效前成立，保留取消及恢復的 audit。期界 `RunRenewals` 在同一交易中關閉原 pricing assignment、開啟新 assignment、核定新價格發票，或過帳 `subscription_ends` 而不產生續約發票。重跑不會產第二張。
- 訂閱結束後狀態查詢顯示 `ended`，權益重建顯示 `expired/cancel_at`。已結束訂閱不再續約；重新購買是另一筆新訂閱。

## 驗收證據

- `TestProFiveSeatsKeepsVersionAndLinesAcrossRenewal`：$50＋$10×5＝$100，續約仍是 $100；已發布價和元件不可修改。
- `TestS06NextPeriodChangeUsesRevisionAndAssignmentHistory`：舊 Basic 當期不變；10-01 才改 Pro 五席、開 $100 續約，重跑不重複；重播／不同 payload／舊 revision／競爭排程的結果固定。
- `TestS06CancelResumeAndBoundaryExpiry`：取消與升級衝突、取消前恢復、再次排程取消、期界無續約、權益過期及結束後拒絕 resume。
- `TestMenuSchedulesProAtNextBoundary`：選單可完成 Basic 購買、排下期 Pro、推進時鐘與續約。舊 E schema 升級測試及全部既有場景保持通過。

此階段沒有 S07 的期中 proration、S08 的逐戶版本遷移、S09 的用量關帳或 S10 合約。價格元件及 assignment 為它們提供來源，但不能單憑資料表宣稱那些情境已完成。
