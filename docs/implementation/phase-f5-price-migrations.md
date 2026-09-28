# F5：價格版本、cohort 與既有訂閱遷移

## 價格發布及新購

`PublishProPrice` 建立 draft、寫入固定費、席次、tasks allowance 與超額費率元件，再原子發布。已發布版本及元件由資料庫 trigger 禁止修改；同 payload 重送回原版本，不同 payload 使用同 ID 會衝突。`SelectCatalogPrice` 在指定生效時間建立 cohort selection；既有 selection 不可覆寫。`CreateQuoteForCohort` 只從該 cohort 的已發布且已生效版本報價；原訂閱仍保持原 PriceVersion。

S08 oracle 使用 Pro v1 固定 $50＋五席 $50＝$100，Pro v2 固定 $60＋五席 $50＝$110。cohort A 的新報價是 $110；default 仍是 $100。

## 遷移及故障處理

`PreviewPriceMigration` 對每戶展示原／目標版本、席次、下期原價／新價、現有權益狀態及「同方案，續約付款決定權益」規則。`PlanPriceMigration` 以 cohort、目標版本與排序後訂閱清單形成穩定批次；每戶保存原版本、revision、價格快照和下一期界。已排程變更、未決期中升級與其他未決價遷移互斥。

期界續約在同一交易內用 revision 和原版本 CAS 關閉 v1 assignment、開 v2 assignment、建立 v2 發票。重跑不重開。若某戶狀態與預覽快照不符，該戶標為 `conflicted` 並暫停批次；其他尚未處理的戶暫停續價。操作員可檢視差異、略過衝突戶，或在仍有待處理戶時恢復批次。略過的客戶保留舊價。回退使用新的反向遷移與 assignment；不修改已核定發票或舊價格版本。

CLI「價格版本與 cohort 遷移」提供發布、選用、預覽、建批次、暫停、略過、恢復；「狀態查詢」可讀價格、cohort 與逐戶遷移狀態。驗證涵蓋兩戶 $110 續約、期界重跑、第二戶衝突時第一戶已成功的部分批次、明確略過後舊價續約，以及下一期反向遷移至 v1。

限制：此切片的發布入口只支援現有 Pro 元件語彙與 tasks meter；AI tokens SKU 的新增元件與 API 相容性仍由 P01／P02 處理。此為本地單 writer lab，遷移暫停屬顯式操作狀態，不模擬跨區域 worker 協調。
