# C. 商務情境、故障點與對帳修復

狀態：紙上 oracle，尚未執行測試。政策依 [A](01-pricing-and-policies.md)，保護點依 [B](02-domain-and-invariants.md)。除特別註明，時間為 UTC、幣別為 USD；月份使用 `[start,end)`。每次 trace 都要能由持久事實重播，不靠程式記憶體中的「成功」旗標。

## S01–S12 trace

| 情境 | 事實順序、金額及失敗時答案 |
| --- | --- |
| **S01 新 Basic** | 2026-09-01 Quote 引用 Basic v1、固定 $20、有效期限及 fingerprint；接受後建立 $20 invoice 與固定 key 的 capture operation。發送前崩潰由 outbox 重送同一 operation；capture `succeeded` 且 allocation 入帳後 Subscription 才 `active`，權益投影依來源開啟。Quote 本身不是應收或付款證據。 |
| **S02 正常續約** | 帳期由原月日錨點算 `[09-30,10-30)`、`[10-30,11-30)` 等；31 日錨點於 2 月截月底，3 月回 31 日。`subscription+period+charge_group+revision` 唯一；固定預付 $20 在新期核定一次，capture operation 也只對應一次義務。重跑關帳／worker 回原結果。 |
| **S03 續約失敗與 grace** | 10-01 Basic $20 invoice 到期，確定失敗仍保留 open 應收；依 due_at 起 7 天 grace，權益先 `grace`，到 deadline 才 `suspended`。補款 $20 確認後分配到原 invoice，權益回 `active`，保留中斷歷史。是否在 suspended 時繼續新期計費由明示服務政策決定；本 lab 暫停新增預付服務期，未付歷史不抹除。 |
| **S04 provider 已成功，本地回應遺失** | $20 capture 使用既存 provider key 發出，timeout 後 operation 為 `unknown`，本地不可聲稱失敗或建立另一 key。查原 key：若確證成功，入一筆觀察／allocation／activation；若查詢仍無終局，維持 unknown 並告警；僅 provider 的終局無扣款證據能釋放保留。 |
| **S05 webhook 重複、亂序及崩潰** | 同 event ID 的 success 只消費一次；較晚收到的 pending 不讓 success 倒退。若成功觀察與 allocation 已提交而權益 worker 崩潰，重建權益投影；若崩在提交前，重新消費 inbox／查 provider。同一 capture 對同一義務仍只分配一次。 |
| **S06 下期變更、取消／恢復** | Basic → Pro 五席排到 10-01，當期仍 Basic；同一邊界只允一項排程，由 `subscription revision` CAS 拒絕競爭寫入。`cancel_at=10-01` 與下期升級衝突時本 lab 不猜優先序，要求先撤銷取消再接受升級；取消前 resume 清除排程且保留 audit。10-01 已結束後不叫 resume，重新購買。 |
| **S07 期中 Pro 升級** | 9 月 `[09-01,10-01)`，Basic 已預付 $20，Pro 五席為 `$50+$10×5=$100`。09-16 立即切換的剩餘 15/30 天：Basic 未提供段退款行 `−$20×15/30=−$10`，Pro 新段 `$100×15/30=$50`，補差額 invoice **$40**；負行已抵正行，不另生可花 credit。若 $40 capture 為 UNKNOWN，Basic 持續、Pro 不先開。09-18 才證實已收並啟用，剩 13/30：`−$20×13/30=−$8.67`，`$100×13/30=$43.33`，實際補差額 **$34.66**。保留原 $40 invoice，另作 `+$1.33` Basic 回收修正和 `−$6.67` Pro 服務修正，淨更正 `−$5.34`；已收 $40 中釋出的 **$5.34** 才成為 funded credit，可抵下期或申請退款，兩者共用額度。 |
| **S08 新價與 cohort** | 發布 Pro v2 並設定 cohort A 的 CatalogSelection；原訂閱仍指 Pro v1。遷移先預覽每戶下一期金額、權益和差額，再以 `migration_id+subscription_id+target_version` 做唯一命令；在期界 CAS 關閉 v1 assignment、開 v2 assignment。部分完成時停止新批次，已成功者保留明確歷史；回退用反向新 assignment，不改舊帳或已發布價格。 |
| **S09 用量與晚到** | Pro 當期包含 20,000 tasks，超額 $0.001/task。關帳 cutoff 前共 20,003，精確超額 $0.003，按當期同元件段 half-even 為 **$0.00**；invoice line 仍保存原量及未捨入值。晚到同原期 7 tasks 後累計 20,010，精確 $0.010、累計應收 $0.01，減已入帳 $0.00，下一張票的原期 debit **$0.01**。相同 event 重送 delta $0；同 event ID 內容變動回 conflict。負差額走 CreditNote，不能改原 invoice。 |
| **S10 Acme 合約** | Acme 的 ContractVersion 覆寫白名單固定費 `$40`、席次 `$7×5`，當期總 **$75**、Net30。PriceVersion 提供元件與捨入等基底，合約引用價源並保存覆寫；條款允許核定後先開服務，非「已付款」。合約到期及後續指派在期界；若缺明確後續價，不暗用 catalog current price，停止自動續價、報 discrepancy 與人工處理。 |
| **S11 更正與退款競爭** | 原 invoice $100 更正淨義務為 $80。未付時只將 open 應收降至 $80，credit=$0；已收 $60 時剩餘應收 $20，credit=$0；已收 $100 時釋出 capture allocation $20 成 funded credit，應收 $0。若同時申請兩筆各 $15 refund，第一筆預留 $15 後第二筆因可用只剩 $5 而拒絕或縮額；若先抵未來帳 $20，退款可用為 $0。退款 UNKNOWN 時保留額仍佔用。 |
| **S12 對帳缺口** | 對帳比較已核定 invoice／應收、allocation、provider observations、subscription assignment 和權益來源 revision。單純缺投影可重建；未送 outbox 重跑原工作；provider timeout 查原 key；未知 provider transaction 或金額／幣別矛盾先封存證據並人工審閱。RepairOperation 記 `expected/actual`、前置 revision、穩定 key、執行與再核對；重跑不創第二效果。 |

S07 的 09-18 日期以實際開通時刻為服務邊界；如果 provider 事後證實成功時間早於 09-18，仍按實際提供 Pro 權益的時刻核算，不用 PSP 時間冒充服務時間。這是此 lab 的客戶服務政策，後續需讓產品／財務審閱。

## 故障點到恢復路徑

| 故障點 | 持久事實 | 恢復及禁止事項 |
| --- | --- | --- |
| Quote 接受交易前崩潰 | 無新 intent | 客戶可用同 idempotency key 重試；重新驗證 quote 的到期與 revision。 |
| 接受交易後、provider 發送前 | intent、operation、outbox 已提交 | 送同 key／同 payload；不重算 current price。 |
| provider 收到後 timeout | operation unknown、原 key 在案 | 查原 key，未查明不換 key；保留應收／退款預留。 |
| success webhook 後、allocation 提交前 | inbox 未完成或 pending | 重播觀察並驗證義務；交易唯一鍵保證一筆 allocation。 |
| allocation 後、權益投影前 | 成功收款及 allocation 已在案 | 依政策重建權益，不再 capture。 |
| invoice 核定後、通知前 | 完整 invoice、audit、outbox | 重送通知；不重產 invoice。 |
| refund 發送後 timeout | refund operation unknown，來源預留仍在 | 查原 refund key；不釋放也不另退。 |
| migration 批次中斷 | 各戶 assignment revision 和 migration result | 從未完成戶繼續；已完成戶不以舊指派再套一次。 |

## 對帳分類與修復矩陣

| 分類 | 典型證據 | 允許的動作 | 修復後驗證 |
| --- | --- | --- | --- |
| SAFE_AUTO_REPAIR | 成功 allocation 和有效 assignment 存在，僅 entitlement projection 缺失 | 按釘選 policy 重建投影；不改財務事實 | 權益 reason／source revision 與期望一致 |
| RETRY_REQUIRED | 已提交 outbox 未送達 | 原 job／operation key 重跑 | provider reference 或成功送達紀錄唯一 |
| EXTERNAL_LOOKUP_REQUIRED | provider timeout／互斥的非終局回報 | 查原 operation 和遠端狀態，等待可信終局 | 本地 observation 能指向外部原操作 |
| MANUAL_REVIEW | 遠端金額／幣別不符、未知外部交易、合約到期後無指派 | 暫停相關自動金流或續價；收集 provider、quote、invoice 證據 | 人工決議與核准者、後續命令有引用 |
| UNSAFE_TO_REPAIR | 缺歷史 capture 或互相矛盾的票據來源 | 不覆寫原資料；調查後另開可稽核更正 | 新更正完整指回原物件與決議 |

每次 ReconciliationRun 保存 cutoff、檢查集合、`expected`、`actual`、證據來源及觀察時間。Discrepancy 有穩定身分及狀態 `open/investigating/resolved`；「沒有查到 provider 交易」只代表當次查詢結果，不足以單獨宣告扣款未發生。修復的安全前提是來源事實完整；證據不足時分類升級，不自動湊平。
