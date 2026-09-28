# F4：期中 Basic → Pro 升級

## 可執行範圍

`RequestImmediateProUpgrade` 以訂閱 revision CAS 和請求鍵建立補差額發票。只接受有效且當期已付清的 Basic 訂閱；同一訂閱的下期排程、取消及其他未決期中升級互斥。價格來源是請求當時已發布、已生效的 Pro catalog selection。原 Basic assignment 在款項確認前保持有效。

金額採各價款分別按剩餘奈秒／服務期奈秒比例計算，並以 half-even 捨入到 cents，再以 Pro 剩餘價款減 Basic 未使用價款。以 2026-09-01 至 2026-10-01、Basic $20、Pro 五席 $100 為例，09-16 請求建立 $40 的獨立發票與穩定 provider operation。原當期 Basic 發票不改。

付款 `UNKNOWN` 保留原操作並維持 Basic。09-18 查證原付款成功後，以實際開通時間重算：Pro $43.33、Basic 未用 $8.67，實際應收 $34.66。系統原子關閉 Basic assignment、開 Pro assignment、增加訂閱 revision，再用持久 outbox 過帳 $5.34 減額更正。更正只從已收 allocation 釋放 funded credit；重跑不會重複發給客戶。

期界時未決升級阻止下期續價，並留下 `renewal_holds`。若付款在期滿後才確認，升級標記 `needs_review`，不補開已過期的 Pro 服務。操作員可從 CLI 執行「處理未提供升級服務的補差額」；此專用命令將補差額發票義務全額沖回，按實收產生 credit，之後可走既有 credit 退款流程。處理請求鍵固定為 change ID，崩潰重跑會回傳同一更正。處理完成後可按原 Basic 價繼續續期。

## 操作與驗證

CLI 的「訂閱排程與取消」提供期中升級；「付款」提供送出與原操作查證；「發票更正與 credit」提供延遲開通更正、未服務補償；「狀態查詢」提供變更、金額、狀態與處理標記。

`go test -count=1 ./...` 通過。S07 oracle 覆蓋 $40、付款 UNKNOWN 維持 Basic、09-18 更正 $5.34 並只產生 $5.34 funded credit、重跑不重複、期界暫停與期滿補償重跑。保留的限制：本切片只處理 Basic → Pro 且補差額為正；期中降級、額外用量、企業合約及多幣別由後續情境處理。
