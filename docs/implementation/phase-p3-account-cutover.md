# P03：Account／Commerce 邊界與灰度遷移

## 本機遷移模型

`account_links` 保存舊帳戶 ID、Commerce customer ID、beneficiary、cohort、歷史資料標記、讀取來源與唯一命令寫入者。既有 Commerce 發票的帳戶必須標示歷史資料並回填來源。遷移初始讀取及寫入來源為 `legacy`；新購、合約接受、訂閱變更與新用量事件會在 Commerce 交易中檢查寫入者，避免兩邊同時建立新商務事實。同一請求鍵的已提交操作仍可重播。

`ShadowQuote` 依目前已發布價格與帳戶 cohort 做唯讀報價，保存舊系統提供的金額／幣別、Commerce 結果、是否相符及本機計算延遲。`ShadowEntitlement` 保存舊權益狀態與 Commerce 訂閱投影結果。Shadow 只記錄比較證據，不建立應收或呼叫 provider。

`BackfillLegacyProvenance` 將舊訂閱／發票 ID 連到 Commerce 訂閱、發票與 PriceVersion。只有客戶、發票、帳期內有效的 assignment 及不可變 invoice line 相互吻合才標 `complete`；其餘標 `manual_review`。審查者可用 `ResolveLegacyProvenance` 指定已核實的正確映射並留下決議，原發票與 assignment 不改寫。同一 Commerce 發票不可被兩個舊發票 ID 重複認領。有歷史資料的帳戶必須完成其全部現有發票映射。

`MigrationReadiness` 要求最新 shadow quote 和 entitlement 相符、回填完整、最後一次對帳不早於 shadow、付款沒有 `created/submitted/unknown` 未決操作、帳戶相關對帳差異在設定門檻內，且 quote p95 不超過依基線設定的上限。未決付款固定要求為零。先切換讀取來源，再以同一資料庫交易核對門檻與切換唯一寫入者。`StopAccountMigration` 記錄停止原因並阻止新的 Commerce 命令；已接受的義務仍保留原寫入者和讀取來源，供對帳、查證與更正。

CLI「帳戶邊界與灰度遷移」可操作映射、shadow、回填、人工修正、準備度、讀取切換、寫入切換、停止及 adapter 權益查詢。「狀態查詢」列出每個帳戶的擁有者與準備度。受內部憑證保護的 `GET /v1/internal/accounts/{account}/entitlements/{subscription}` 會按目前讀取來源返回權益。

## 驗收與範圍

`go test ./lab -run TestP03 -count=1` 驗證 shadow mismatch 停止切換、legacy writer 拒絕新購、讀取先於寫入切換、切換後原請求可重播、停止後拒絕新命令、UNKNOWN 付款及缺來源回填阻止切換、人工核實後恢復準備度。API 契約測試驗證 adapter 讀取切換及內部憑證。

這是單一 SQLite 程式中的遷移演練，舊系統觀測值由操作者提供。它沒有連線真實單體或測量生產流量。Shadow 準備度目前採每個帳戶、每種類型的最新比較；實際 rollout 需依 tenant、plan、合約和舊價格 cohort 取得具代表性的樣本，設定真實延遲與差異率基線，並配置正式身分驗證、權限及審計保存政策。
