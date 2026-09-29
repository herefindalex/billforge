# D. API 契約、相容性與平台演進

[English](04-platform-apis-and-migration.md) | **繁體中文** | [简体中文](04-platform-apis-and-migration.zh-CN.md)


狀態：設計契約；[本機 v1 API](../implementation/phase-p2-v1-api.zh-TW.md)與[帳戶遷移演練](../implementation/phase-p3-account-cutover.zh-TW.md)已對應其中的 MVP 切片，兩份紀錄說明已實作行為與限制。依 [A 的價格解析](01-pricing-and-policies.zh-TW.md)、[B 的事實所有權](02-domain-and-invariants.zh-TW.md)、[C 的故障 trace](03-scenarios-and-reconciliation.zh-TW.md)。API 的目標是讓產品團隊按公開定價元件接入新 SKU，同時讓 Commerce 對應收、付款與權益保持單一寫入責任。

## 兩種 consumer 與命令／查詢

Consumer 1 是自助購買／結帳 UI，需看「現在確定要收多少、日後哪些費用依用量變動、何時會開通」。Consumer 2 是內部 Sales／Support／財務工具，需看合約來源、Net30、變更原因、修復權限與證據。兩者共用價格與訂閱契約，內部端透過授權的合約／修復命令取得額外欄位，不繞過領域服務改 DB。

| 契約 | 必要輸入或輸出 | 邊界與錯誤 |
| --- | --- | --- |
| `POST /v1/quotes` | customer、billing account、beneficiary、plan ID、seat quantity、期望 effective_at；回 `quote_id`、expiry、context fingerprint、PriceVersion ID／checksum、ContractVersion、逐元件現在／未來費用、用量費率、tax 尚不支援標示 | 不接受由 client 傳單價；無唯一可選版本、缺 meter、合約不適用時明確 `PRICE_UNAVAILABLE`／`CONTRACT_CONFLICT`。 |
| `POST /v1/subscriptions` | quote ID、fingerprint、`Idempotency-Key`；回 subscription、invoice、payment operation 與 `pending/active` | 檢查 quote expiry、customer、revision 與釘選版本仍可售；選價切換本身不替換有效 quote 的價格。同 key／同 payload 回原資源，不同 payload `409 IDEMPOTENCY_CONFLICT`。 |
| `POST /v1/subscriptions/{id}/changes` | quote ID、expected subscription revision、change mode `next_period/immediate`、`Idempotency-Key`；回 change intent、待支付額和 `requested/effective` 狀態 | `409 REVISION_CONFLICT` 要重報價；付款 UNKNOWN 回 `PAYMENT_PENDING_VERIFICATION`，不可默認切換。 |
| `GET /v1/subscriptions/{id}` | effective plan／pricing assignment、scheduled change／cancel_at、invoice／payment 狀態、權益 reason 與 grace deadline、revision | 讀模型可延遲，但須標 source revision／`as_of`；不能把 `requested_plan` 當 `effective_plan`。 |
| `POST /v1/usage-events` | tenant、source、event ID、meter、quantity、event_at，批次每項獨立結果 | 相同 event ID＋payload 回原結果；不同 payload `409 EVENT_CONFLICT`；不默默丟棄。 |
| `GET /v1/entitlements/{beneficiary}` | feature、`active/grace/suspended/expired`、effective interval、reason、來源 assignment／invoice／policy revision | 消費者依權益 API 決定服務；讀取延遲顯示 `as_of`，不從付款布林值推斷。 |
| `GET /v1/invoices/{id}` | final line 的 price／contract／period／quantity／rate／amount 與更正引用 | 核定內容不變；更正是另列資源。 |
| 內部 `POST /v1/contracts`、`/v1/migrations`、`/v1/repairs` | 版本化合約、preview＋cohort＋target PriceVersion、discrepancy＋前置 revision | 授權／audit；禁止直接改已核定發票、已發布價格或手填 credit balance。 |

Quote 回應需把 `due_now`、`recurring_committed`、`metered_estimate_or_rate` 分開；未來用量未知就只給費率與 allowance，不把預估值宣稱為已承諾總額。Server 持有計價和捨入政策；client 只呈現 line items。`Idempotency-Key` 範圍是 tenant＋endpoint／命令類型＋key，保留 payload hash 與原回應。外部 PaymentOperation 使用另一個穩定 provider key，不能直接用 HTTP attempt ID。

## 相容性規則（P02）

1. v1 response 新增可選欄位可向前擴充；既有必填欄位的語義、整數 cents 單位、狀態含義不能靜默改。新的 charge kind 先用 `components[]` 的已知 `kind`／`display_label` 表示；舊 client 若無法理解新計價種類，quote 標 `requires_client_capability`，接受時檢查宣告能力並拒絕，不讓它盲簽。不要把新 kind 假裝成 FixedCharge。
2. 未知 enum 值在只讀畫面可顯示通用狀態與 server 提供的說明；對建立訂閱或金額確認的命令，未知值需 fail closed。舊 client 不能透過自己計算價格或忽略新 line 來繞過 server quote。
3. `PriceVersion`、`EntitlementPolicyVersion`、ContractVersion、meter schema 分別版本化。Quote 暴露解析結果與來源引用；任何不可相容變更用 `/v2` 或新能力協商。舊 API client 的契約測試至少涵蓋 Basic／Pro、UNKNOWN、grace、合約 quote、新元件拒絕與晚到更正的讀取。
4. 延遲讀模型不得對客戶宣稱「已開通」而來源尚未提交；回 `pending` 和可輪詢操作 ID。Support 可查看來源 revision 與 provider observation，避免把查詢快取當最終金流真相。

## P01：三天推出 AI tokens SKU 的設計推演

若 AI tokens 同樣是「每期包含 N、超額每 token 固定精確單價」，先註冊 `meter_id=ai_tokens`、資料來源與去重規則，再建立新 Plan／PriceVersion 的 `IncludedQuantity`＋`UsageOverage` 元件、權益政策、文案和測試 fixture；Pricing／Billing／Payments 核心無需為 SKU 分支。發布前驗證元件完整性、quote 和 rating 金額 oracle、老 client 能力協商、shadow 資料差異與 Finance 簽核。三天是目標情境，未有實測推出時間。若要求階梯價、階段性促銷、稅務或即時 hard quota，本 lab 元件不足，必須另做設計與程式變更，不能宣稱配置即可解決。

## P03：成熟單體拆界與價格灰度 ADR

**決定**：Account 保有認證、客戶與組織身分；Commerce 擁有 price selection、assignment、rating、invoice、payment obligation 和權益決策。初始以既有單體中的 adapter 呼叫 Commerce 契約；遷移期間每個事實只有一個 writer，不能兩套系統同時收款或核定發票。

**順序**：先建立來源 ID 映射與唯讀 shadow quote／entitlement 比較，按 tenant、plan、合約／無合約及舊版本 cohort 分桶量差；不以 shadow 結果對客戶收款。回填歷史 assignment／invoice provenance，將缺資料列 discrepancy 人工處理。再以 feature flag 小 cohort 啟用新讀路徑，確認延遲與差異；最後逐項把 command writer 交給 Commerce，交接點有唯一 owner 記錄和可回放 outbox。每一步記錄 p95 quote latency、差異率、UNKNOWN 停滯時間、對帳缺口及客服事件，設停止門檻，但數值門檻需取得基線後設定。

**回復**：配置／selection 出錯時停止新 cohort 選用，未接受 quote 可重新報價；已接受 quote、已送 provider operation、已核定 invoice 不因回退而重算。writer 切換出錯時先停新命令並對帳，確認未決 operation 後才切回；不能在同一義務上讓舊 writer 再建一筆 capture。已遷移訂閱若需回 v1，建立新的未來 assignment 和審計決策，不更改 v2 歷史。

**代價**：shadow 比較、來源映射、雙讀與單 writer 協調增加短期工作量；換得能在歷史正確性可查的前提下逐步拆開帳戶與 Commerce。文件、能力協商及可觀測性是產品團隊接入 SKU 的必要交付，不是之後補的說明。
