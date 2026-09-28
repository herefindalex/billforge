# P01：配置式計量 SKU

## 交付路徑

`meter_schemas` 記錄 meter ID、事件來源、單位與 schema 版本。`RegisterMeter` 為同一 ID 保留不可變設定；事件來源對應到記錄用量時的 `source`。既有 `tasks` meter 為相容舊資料使用 `*` 來源。

`PublishMeteredPrice` 接受方案、價格版本、固定費、可選席次費、meter、內含量、超額精確單價及生效時間。發布時確認 meter 已註冊，將所有元件與不可變 checksum 一起提交。`SelectCatalogPrice` 將已發布版本指派給 cohort。購買仍使用原有 Quote、AcceptQuote、付款與權益路徑；關帳、重算、續約則按價格版本的 meter 評價及寫入發票項目。

互動選單「價格版本與 cohort 遷移」可註冊 meter、發布計量價格、指定 cohort；「狀態查詢」顯示 meter、內含量與超額費率；「用量與關帳」依訂閱有效價格選擇 meter。

## 金額 oracle

AI 方案例子：每期固定 3000 cents、內含 100 個 `ai_tokens`、超額每 5 個 token 收 1 cent。105 個 token 的超額量是 5，關帳結果為 1 cent。續約發票包含固定費 3000 cents 與 `usage:ai_tokens:period:0` 1 cent，合計 3001 cents。既有 tasks 案例仍保留 20,003→0 cents、20,010→1 cent 的回歸。

## 邊界

一個價格版本目前支援一個用量 meter；複合 meter、階梯價、預付儲值、稅與多幣別需要額外模型。用量事件必須匹配該訂閱在事件時間的價格版本及 meter，且來源符合 meter 註冊資訊。新 SKU 的權益仍使用現有訂閱權益投影；每個 SKU 的 feature 集合與獨立權益政策版本尚未建模。`MeteredPriceSpec` 是本機管理入口，不是外部自助發布 API。

驗收：`go test ./lab -run TestP01 -count=1` 覆蓋價格發布重播與不可變性、來源與 meter 拒絕、事件去重、精確計價及發票來源項目；`go test ./...` 保持既有案例回歸。
