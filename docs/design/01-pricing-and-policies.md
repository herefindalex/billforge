# A. 計價版本與業務政策

狀態：Billforge lab 的設計決策。輸入是 [整體方案](../commerce-lab-plan.md)及 [34 份研究](../research/README.md)；這些規則是本 lab 的選擇，不宣稱外部產品都採同一政策。所有時間用 UTC，[start, end) 表示包含起點、不包含終點。

## PriceVersion 的責任

Product 回答「賣什麼」；Plan 回答「Basic 或 Pro 等長期識別碼」；**PriceVersion 回答一個已發布版本如何收費**。它是可執行的規則資料，由定價引擎解讀。已發布版本的經濟內容不可更動。

最小結構如下；欄位名是概念契約，尚不是 SQL schema：

| 欄位 | 型別／例子 | 規則 |
| --- | --- | --- |
| id、plan_id、version | pro-v2、pro、2 | id 全域唯一；(plan_id, version) 唯一且遞增。Plan 不改名來代替價格變更。 |
| publication_state | draft／published | draft 可修；published 之後經濟 payload 不可修改。停止出售用另行審計的 availability event。 |
| effective_from、effective_to | 2026-10-01T00:00Z、可空 | **只界定新訂閱／明確重新指派的可選區間**；不代表舊訂閱在該時刻自動遷移。 |
| currency、billing_cadence | USD、calendar_month | 本 lab 僅 USD／月。帳期錨點在 Subscription，非所有客戶共用的 PriceVersion 欄位。 |
| charge_definitions | 下表的版本內值物件 | 使用穩定 component_code，保存計價種類、單位、數值、時點及必要 meter。 |
| rounding_policy_ref | component-period-segment／HALF_EVEN | 先以精確值聚合，再於明確範圍捨入至 cents；不能逐事件先捨入。 |
| proration_policy_ref | elapsed_utc_seconds／HALF_EVEN | 變更計價需由版本能找回當時政策；沒有期中變更的元件也明確標示。 |
| supersedes_id、published_at、checksum | pro-v1、時間、payload hash | 供追溯與 quote 驗證；checksum 不能取代資料庫不可變約束。 |

ChargeDefinition 在一個 PriceVersion 內至少包含 component_code、kind、unit、rate／amount、charge_timing、aggregation_policy 及是否可按比例計算。按種類補欄位：

| Kind | 必要欄位與例子 | 計算 |
| --- | --- | --- |
| FixedCharge | amount_minor=5000、UPFRONT | 期間固定 $50。 |
| PerSeatCharge | unit_amount_minor=1000、quantity_min=1、unit=seat、UPFRONT | $10 × 該服務區段已承諾付費席次。 |
| IncludedQuantity | meter_id=tasks、quantity=20000、unit=task | 當期有 20,000 tasks 的免費計價 allowance；它不是服務停止上限。 |
| UsageOverage | meter_id=tasks、unit_rate_decimal=`0.001`、ARREARS | max(期間 tasks − allowance, 0) × 精確單價，聚合後才捨入。 |

每個 `charge_definitions` 項目還需要版本內唯一的 `component_code`、`kind`、`unit`、`charge_timing`；有用量的項目需指定 `meter_id`。金額欄位以整數 cents 保存固定／席次費，超額單價以十進位字串保存，禁止 binary float。`IncludedQuantity` 是計價 allowance，與權益配額分離。欄位的合法組合應在發布時檢查，例如 `UsageOverage` 必須指向同版本、同 meter 的 allowance 定義；否則發布失敗。

Pro v2 可由 FixedCharge $50、PerSeatCharge $10、IncludedQuantity 20,000 tasks、UsageOverage $0.001／task 組成。Basic v1 只有 FixedCharge $20。元件代號例如 base、seat、tasks_included、tasks_overage 必須在版本內唯一，發票行項能引用原代號及版本 ID。

**不放進 PriceVersion 的資料**：客戶席次與事件用量、特定客戶折扣或合約、目前付款狀態、權益是否已開啟、帳期實際錨點、對某客戶的遷移決定。這些屬於變動的商業事實與其他政策版本。EntitlementPolicyVersion 獨立，SubscriptionPricingAssignment 同時指向定價版本及權益政策版本；Enterprise ContractVersion 可在允許的元件上覆寫價與付款條款。

Quote 保存 customer、subscription revision、選到的 PriceVersion ID／checksum、ContractVersion、seat quantity、effective_at、逐元件金額、未來用量費率、rounding policy、過期時間及 fingerprint。InvoiceLine 再保存核定時的元件、期間、數量、精確率、未捨入值、捨入金額與來源引用。Quote 是有期限的預覽；核定發票才形成不可改的財務事實。

### 價格解析與版本發布

1. 客戶和 API 指出 plan，不自行算價。對新指派，以購買時間找可用的預設 PriceVersion；既有訂閱先讀自己釘選的 assignment。任何 cohort 實驗使用明確的 eligibility／assignment 記錄。
   `effective_from`／`effective_to` 是版本的候選區間，不是唯一預設的充分條件。另以追加式 `CatalogSelection` 切換事件（plan、cohort、`effective_at`、price_version_id、發布人及時間）指定預設：每個 scope 在某時刻採最後一個已生效事件，直到下一事件；同 scope＋`effective_at` 不許兩個目標。發布 v2 只追加切換事件，不修改 v1。已產生的 Quote 在短期限內仍可接受其釘選版本，前提是版本仍屬候選區間、沒有明示停止出售事件，且訂閱 revision／fingerprint 未變；不能在接受時暗換成新預設。舊訂閱仍依原 assignment 計價。
2. 若有 ContractVersion，核對有效區間、允許覆寫的 component_code、幣別、付款條款與客戶身分。合約缺少必要元件時拒絕，不悄悄回落到 current catalog。
3. 使用 component definitions、客戶承諾的 quantity 和計量事實計價。缺 meter、缺 component 或多個衝突的預設版本均拒絕。
4. 接受 Quote 時檢查時效、context fingerprint 與訂閱 revision；支付操作使用凍結金額與穩定 operation ID。發票核定保存同一價源和中間值。
5. 發布 v3 只改新購預設。舊客保留 v2；遷移要有 preview、cohort、effective_at、逐訂閱操作身分及停損能力。

發布驗證：無負價、同版本無重複 component_code、meter 與 unit 對應、included quantity 非負、同一 meter 的 allowance 不含糊、精確 decimal 可在系統上限內計算。對同一 plan 的預設新購版本，時間窗不可產生兩個無優先序的候選；實驗 cohort 必須顯式選擇。已發布版本若要提前停賣，記新的 availability event；它不改原定價 payload 和既有 assignment。

此設計借鑑 [OpenMeter 的版本釘選](../research/openmeter.md)、[Kill Bill 的生效日期](../research/kill-bill.md)、[Lotus 的方案遷移](../research/lotus.md)、[Shopware 的規則上下文](../research/shopware.md)；以上做法在細節上並不相同，故 Billforge 的解析順序和遷移政策必須自己明訂。

## 政策決策表

| ID | Lab 採用的政策 | 主要理由與刻意接受的成本 |
| --- | --- | --- |
| D01 | 固定費及席次預付；用量後付。 | Quote 能明說現在應收與未來變動費。需保留兩種 charge group 的帳期身分。 |
| D02 | UTC 月週期按原月日錨點，短月份截月底；區間 [start, end)。 | 續約可重播，月長不同時仍需測界線。 |
| D03 | 定價中間值為精確 decimal／有理數，按「元件 × 服務區段」聚合後 half-even 捨入至 cents。 | 可重建 $0.001 task 的最終 cents；跨區段加總可能與先總計再捨入不同，必須標版本。 |
| D04 | 新購使用 PriceVersion 可選區間；既有客戶釘選，遷移明示。 | 發布速度與歷史準確性兼顧；需維護 assignment 及 rollout。 |
| D05 | 同 usage event 身分、不同 payload 報衝突；同 payload 重送回同一結果。 | 防止改 timestamp 再收一次；需要持久化原 fingerprint。 |
| D06 | 關帳後晚到用量歸原服務期，但透過下一期更正差額；原 invoice 不改。 | 同時保留服務歸屬與歷史帳單；需能以原 PriceVersion 重算累計差額。 |
| D07 | 新購自助付款確認才啟用；續約逾期有 7 天 grace；Net30 合約依條款先啟用。 | Entitlement 有獨立政策，顯示原因及截止時間。 |
| D08 | 期中自助升級付款確認才切有效方案。收款成功但開通延遲，針對未提供期間另發更正。 | UNKNOWN 時 Basic 可持續；需能處理先收後服務的價差。 |
| D09 | 正淨額 proration 使用獨立補差額 invoice，舊 invoice 不改；負行已在該文件抵正行，不另造可花 credit。 | 防止同一抵扣雙重使用。 |
| D10 | 更正先減未付應收；只有已收且可釋出的部分能成為有資金來源的 credit。Refund 保留額與 credit 使用共用預算。 | 有限應收紀錄仍能防重退／重抵；未宣稱完整總帳。 |
| D11 | 相同 provider operation 持續使用同 key 與 payload；逾時記 UNKNOWN 並查原操作。 | 避免遠端已扣款卻換 key 再扣；不能假設查無即失敗。 |
| D12 | 合約生效／到期對齊帳期邊界，缺到期後指派就停自動續價並產生可處理的 discrepancy。 | 避免過期後不知用哪個價；初期不支援合約期中任意價段。 |

### S07 的期中升級決策

Basic $20 已為 [09-01, 10-01) 收款。09-16 00:00Z 接受 Pro 五席的 Quote，剩餘 15／30：未用 Basic −$10，Pro 五席 +$50，補差額 invoice $40。原 Basic invoice 不改，兩條 signed lines 必須保留來源。支付 operation 對 $40 發出後如結果 UNKNOWN，有效方案與權益先維持 Basic，不能再用新 key 扣 $40。

若 09-18 00:00Z 才查實 provider 已扣 $40，此時才啟用 Pro。Basic 實際服務 17 天，Pro 實際服務 13 天；實際補差額是 −$8.67＋$43.33＝$34.66。保持原補差額 invoice $40，另建 −$5.34 的更正，將多收部分轉為有原付款來源的 customer credit，必要時再走 refund。其間不以 current price 重算原價。若確認 provider 根本沒有扣款，則用更正取消補差額義務，Basic 繼續服務；若一直無法取得真相，保持 UNKNOWN 並升級人工審查。

實務產品可能選擇先開通 Pro、再承擔壞帳風險；本 lab 選擇上述政策，是為了能清楚推演 UNKNOWN、補償及資金來源。選擇的代價是可見的開通延遲。
