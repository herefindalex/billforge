# Web Admin C01–C49 證據盤點

[English](web-admin-action-audit.md) | **繁體中文** | [简体中文](web-admin-action-audit.zh-CN.md)

A01–A30 在文件列明的本機範圍通過（30／30），T01–T22 全完成。最終獨立複查沒有阻擋或重要問題；49 項動作紀錄連接 UI 路徑、handler、domain helper、來源守衛、能力、預覽政策、收據／恢復政策、HTTP 測試、瀏覽器情境與執行證據。







## 最終本機驗收（2026-10-03）

A01–A30 在文件列明的本機範圍通過（30／30），T01–T22 全完成。最終獨立複查沒有阻擋或重要問題；49 項動作紀錄連接 UI 路徑、handler、domain helper、來源守衛、能力、預覽政策、收據／恢復政策、HTTP 測試、瀏覽器情境與執行證據。

最新完整 Go 回歸：355 個頂層測試／1115 個含子案例通過事件，零失敗。瀏覽器快照：156／156；後加耗盡 Credit、A13 與 A17 斷言各以 2／2 通過，與完整快照分開記錄。

首次 30 秒 process 就緒逾時根因未確認。單獨 1 次、診斷重複 20 次及最終完整 Go 回歸通過；test-only worker 診斷不證明已修復。結果限於本機 Go／SQLite／fake provider 與列明的 fixture。

下方較早的逐案例紀錄保留當時的結果與待核項目；目前判定以上方最終驗收與逐動作完整證據紀錄為準。

## 逐動作完整證據紀錄（2026-10-03）

本表依 action ID 連接既有 UI 路徑、能力、預覽政策及交易測試表，合併填齊驗收計畫要求的 11 個欄位。適用的 payload／來源／金額變體由連結的 domain fixture 與 A01–A29 證據核對；未使用的金融欄位及 N 類預覽規則標為不適用。這是限定本機驗收，不推論任意變體。

- **H**：`POST /admin/api/commands` → `api/admin/commands.go::submitCommand`；R 類先走 `api/admin/previews.go::createPreview`。共用 session／CSRF 與 `authorizeActionIntent` 核能力；最新 Go 已通過 `TestEveryActionHTTPAdmissionGuards`、`TestEveryActionRejectsUnknownPayloadBeforeAdmission`、`TestEveryActionRequiresItsCapability`。每列瀏覽器案例提供真實合法請求與配對收據。**Hc** 另含 C01／C02 合約能力／來源分支與 `TestContractQuoteAcceptRequiresContractCapability`。
- **G**：先查原 actor／key，再驗 canonical intent 與 actor／action／target；41 個 R 動作另核 payload／TTL／clock revision／claim。8 個 N 動作只有 C01／C10／C17／C23／C26／C33／C37／C38。單一 `local-admin` 的跨 actor 瀏覽器操作不適用；C02／C07 的 Go 隔離與共用守衛提供證據，A08 另有到期預覽原鍵重播。
- **L**：37 個本地動作的領域事實、命令收據／狀態與 audit 同交易，帶 lease fencing；恢復先核原收據。**E**：C09／C10／C16／C17 保留並查證指定 provider 操作。**B**：C13／C30／C32／C44／C45 固定成員，每項事實與狀態／result 收據同 commit，再恢復 summary。**Q**：C34 以穩定修復鍵重核證據／revision／provider。**P**：C47／C48 的 decision 與 control receipt 在 provider SQLite 同交易，再恢復 commerce 收據，不宣稱兩庫同一交易。A06／A07／A15／A16／A18／A23／A25 已證這些機制。
- **X**：revision `049e5a9743a70bf543a9771b9db68c127e27b7cf`、schema 9、source／runtime 指紋列於 `/tmp/billforge-acceptance-manifest-20261003.json`。最新 Go 355 個頂層／1115 個含子案例通過事件、零失敗。瀏覽器快照 156／156 通過（25.4 分鐘），105 個稽核案例涵蓋 49 動作的配對收據、原鍵重播與不同 payload 衝突；下列名稱對應 `/tmp/billforge-admin-full-final-cases-20261003.jsonl`。具名原始 artifact 可逐項查證，上方命令可重建執行證據。後加耗盡 Credit、A13／A17 斷言另以瀏覽器 2／2＋2／2＋2／2 補證，執行邏輯相同，全套與補充指紋分開。受控回應只證 UI，金融事實由真 Go／API／provider fixture 證明。首次 process 就緒逾時仍保留為根因未確認的限制，不宣稱已修復。

| 動作 | 領域 helper／adapter | 來源守衛（另加 G） | 收據／恢復 | HTTP | 瀏覽器情境 | 執行證據 |
| --- | --- | --- | --- | --- | --- | --- |
| C01 | `createQuoteForCohort / createContractQuoteTx + bindChangeQuoteTx` | 互斥來源、客戶／席次、revision、原子綁定 | L | Hc | `admin password and internal token stay out of assets responses URLs and logs` | X |
| C02 | `executeAcceptQuoteTx -> acceptQuoteTx / acceptContractQuoteTx` | fingerprint、用途、到期、綁定、writer | L | Hc | `rejected financial action restores an editable form and does not retain its intent` | X |
| C03 | `executeSchedulePlanTx -> scheduleNextPlanTx` | 報價綁定、revision、選定價格 | L | H | `lost scheduled plan response recovers its original command and one schedule` | X |
| C04 | `executeImmediateUpgradeTx -> requestImmediateProUpgradeTx` | 綁定／revision、實際金額不超確認上限 | L | H | `lost immediate upgrade response recovers one obligation without switching service early` | X |
| C05 | `executeScheduleCancelTx` | 訂閱 revision、取消狀態、writer | L | H | `concurrent cancel and resume keep their original intents after source changes` | X |
| C06 | `executeResumeCancelTx` | 訂閱 revision、取消狀態、writer | L | H | `concurrent cancel and resume keep their original intents after source changes` | X |
| C07 | `createPaymentTx` | 可收餘額、原 queued／failed 操作、來源快照 | L | H | `a lost payment command response recovers one operation across sessions and reload` | X |
| C08 | `retryFailedPaymentTx` | 可收餘額、原 queued／failed 操作、來源快照 | L | H | `a retry payment shows a lower balance after another operator reduces the invoice` | X |
| C09 | `adminExternalPreviewCurrent -> adminExecuteExternalCommand (payment)` | 指定操作 ID、status／outbox／amount、故障擁有者 | E | H | `rejected financial action restores an editable form and does not retain its intent` | X |
| C10 | `adminExecuteExternalCommand (payment verification)` | 原操作／provider key；無證據保留義務 | E | H | `rejected financial action restores an editable form and does not retain its intent` | X |
| C11 | `executePostReductionTx -> postReductionTx` | 帳單餘額、減額、來源 | L | H | `lost reduction response recovers its original correction and credit` | X |
| C12 | `executeApplyCreditTx -> applyCreditTx` | funded／已用／預留額度、帳單識別／餘額 | L | H | `C12 applies funded credit to a later invoice once and keeps the command receipt` | X |
| C13 | `adminRunBatchItem -> runChangeCorrectionTx` | 固定成員 ID、逐項來源 revision 與資格 | B | H | `midperiod upgrade previews USD 40.00 and keeps service on the old price until paid` | X |
| C14 | `executeResolveUnfulfilledTx -> resolveUnfulfilledImmediateChangeTx` | 原變更、履行、期限／來源 | L | H | `unfulfilled upgrade exposes its correction and credit provenance in the invoice` | X |
| C15 | `executeReserveRefundTx -> reserveRefundTx` | grant 預算、capture allocation、來源 | L | H | `provider commit followed by a crash preserves the refund reservation across sessions` | X |
| C16 | `adminExternalPreviewCurrent -> adminExecuteExternalCommand (refund)` | 指定操作 ID、status／outbox／amount、故障擁有者 | E | H | `provider commit followed by a crash preserves the refund reservation across sessions` | X |
| C17 | `adminExecuteExternalCommand (refund verification)` | 原操作／provider key；無證據保留義務 | E | H | `funded reduction reserves and refunds exactly once after a lost response` | X |
| C18 | `executeAdminCatalogTx -> publishProPriceTx` | 完整 spec、ID／version／schema／checksum／dependency／selection | L | H | `a newly selected Pro price rejects the old bound quote before scheduling` | X |
| C19 | `executeAdminCatalogTx -> registerMeterTx` | 完整 spec、ID／version／schema／checksum／dependency／selection | L | H | `meter registration refuses an older conflicting schema preview` | X |
| C20 | `executeAdminCatalogTx -> publishMeteredPriceTx` | 完整 spec、ID／version／schema／checksum／dependency／selection | L | H | `metered price publication refuses an older conflicting version preview` | X |
| C21 | `executeAdminCatalogTx -> selectCatalogPriceTx` | 完整 spec、ID／version／schema／checksum／dependency／selection | L | H | `a newly selected Pro price rejects the old bound quote before scheduling` | X |
| C22 | `executeAdminMigrationTx -> planPriceMigrationTx` | 固定目標、revision／席次／排程、項目／批次狀態 | L | H | `price migration pins members and preserves a skipped item through pause and resume` | X |
| C23 | `pausePriceMigrationTx` | 原批次狀態 | L | H | `price migration pins members and preserves a skipped item through pause and resume` | X |
| C24 | `executeAdminMigrationTx -> skipPriceMigrationItemTx` | 固定目標、revision／席次／排程、項目／批次狀態 | L | H | `price migration pins members and preserves a skipped item through pause and resume` | X |
| C25 | `executeAdminMigrationTx -> resumePriceMigrationTx` | 固定目標、revision／席次／排程、項目／批次狀態 | L | H | `price migration pins members and preserves a skipped item through pause and resume` | X |
| C26 | `recordUsageTx` | business key／payload hash、原事件不可改 | L | H | `an uncertain usage response cannot become a second event intent` | X |
| C27 | `executeAdminUsageTx -> recordUsageAdjustmentTx` | 事件來源、帳期／成員、rating version | L | H | `usage keeps the original event, closes at zero and rerates late usage to one cent` | X |
| C28 | `executeAdminUsageTx -> closeUsagePeriodTx` | 事件來源、帳期／成員、rating version | L | H | `usage keeps the original event, closes at zero and rerates late usage to one cent` | X |
| C29 | `executeAdminUsageTx -> rerateUsagePeriodTx` | 事件來源、帳期／成員、rating version | L | H | `usage keeps the original event, closes at zero and rerates late usage to one cent` | X |
| C30 | `adminRunBatchItem -> runUsageCreditNoteTx` | 固定成員 ID、逐項來源 revision 與資格 | B | H | `usage keeps the original event, closes at zero and rerates late usage to one cent` | X |
| C31 | `executeAdminContractTx -> publishContractTx` | 完整 spec、base price／version／checksum／客戶 | L | H | `concurrent contract publication keeps the first version and rejects an older preview` | X |
| C32 | `adminRunBatchItem (due contract invoice collection)` | 固定成員 ID、逐項來源 revision 與資格 | B | H | `publishes enterprise contract, quotes seats and accepts Net30 without early capture` | X |
| C33 | `persistReconciliationTx (after collecting observations)` | 逐來源觀測時間；不宣稱跨庫快照 | L | H | `repairs a missing entitlement projection once with a recorded command receipt` | X |
| C34 | `adminExecuteRepairCommand` | expected／actual、revision／provider、穩定修復鍵 | Q | H | `repairs a missing entitlement projection once with a recorded command receipt` | X |
| C35 | `executeManualDecisionTx` | 目前差異證據／version、理由、不建立金融更正 | L | H | `provider amount mismatch stays manual without changing the captured amount` | X |
| C36 | `executeAccountLinkTx -> linkLegacyAccountTx` | legacy／客戶／beneficiary／cohort／history 映射 | L | H | `account migration detail shows readiness thresholds and keeps owners after stop` | X |
| C37 | `executeAdminShadowTx -> shadowQuoteTx` | account／writer／price／subscription 來源與觀測時間 | L | H | `account migration detail shows readiness thresholds and keeps owners after stop` | X |
| C38 | `executeAdminShadowTx -> shadowEntitlementTx` | account／writer／price／subscription 來源與觀測時間 | L | H | `account migration detail shows readiness thresholds and keeps owners after stop` | X |
| C39 | `executeAdminProvenanceTx -> backfillLegacyProvenanceTx` | 原映射／provider keys、來源版本 | L | H | `historical account provenance review gates read and writer cutover` | X |
| C40 | `executeAdminProvenanceTx -> resolveLegacyProvenanceTx` | 原映射／provider keys、來源版本 | L | H | `historical account provenance review gates read and writer cutover` | X |
| C41 | `executeAdminCutoverTx -> switchAccountReadTx` | 最新 readiness／owners、UNKNOWN／stop、先讀後寫 | L | H | `historical account provenance review gates read and writer cutover` | X |
| C42 | `executeAdminCutoverTx -> switchAccountWriterTx` | 最新 readiness／owners、UNKNOWN／stop、先讀後寫 | L | H | `historical account provenance review gates read and writer cutover` | X |
| C43 | `executeAdminCutoverTx -> stopAccountMigrationTx` | 確認時 AccountLink／owners／stopped 狀態、stop reason；保留金融史與 UNKNOWN 義務 | L | H | `account migration detail shows readiness thresholds and keeps owners after stop` | X |
| C44 | `adminRunBatchItem -> renewOneTx` | 固定成員 ID、逐項來源 revision 與資格 | B | H | `contract without a follow-on price holds renewal and shows the reason` | X |
| C45 | `adminRunBatchItem -> refreshOneEntitlementTx` | 固定成員 ID、逐項來源 revision 與資格 | B | H | `subscription history pages and entitlement provenance use the real database` | X |
| C46 | `adminControlPreviewCurrentTx -> executeAdminClockTx` | 持久化 clock revision | L | H | `clock changes while a payment awaits verification preserve its accepted business time` | X |
| C47 | `adminExecuteProviderControl -> adminSetDecision (payment)` | 最新 provider decision 快照、provider control receipt | P | H | `a retry payment shows a lower balance after another operator reduces the invoice` | X |
| C48 | `adminExecuteProviderControl -> adminSetDecision (refund)` | 最新 provider decision 快照、provider control receipt | P | H | `funded reduction reserves and refunds exactly once after a lost response` | X |
| C49 | `adminControlPreviewCurrentTx -> executeAdminFaultTx` | 指定操作與故障票據狀態 | L | H | `quote and acceptance create one financial obligation under replay` | X |

## 領域、命令與精確值驗收（2026-10-03）

最新完整 Go 回歸與獨立逐條複查支持 A01／A06／A07／A09／A10／A28 通過；A30 最終判定見上方。

| ID | 本機通過 |
| --- | --- |
| A01 | S01–S12／P01–P03 均有 domain／admin fixture、固定金額／狀態 oracle 與共用交易 helper 對照。最新完整 Go 回歸 355 個頂層測試／1115 個含子案例事件通過、零失敗。首次 process 就緒逾時在單獨 1 次、診斷 20 次及最終全套均未重現；根因未確認，test-only 退出診斷不宣稱修復。 |
| A06 | 同鍵 client 只受理一個命令；注入收據故障回滾本地金融事實，重開與重播恢復唯一效果／收據。事實、收據、命令狀態及 audit 共用交易。固定成員 job 的每項效果與項目收據同 commit，最終 summary 可恢復；瀏覽器回應遺失保留原意圖。 |
| A07 | lease 接管隔離舊 generation；撤權令未開始工作 PERMISSION_REVOKED，已提交 provider 義務保留查證。付款／退款 fencing、原 provider receipt 收斂與跨庫控制義務保留均有證據。 |
| A09 | 共用預覽核對 actor／action／target／payload／claim／到期／clock revision；瀏覽器來源變動回 409 且不寫入，保留意圖並顯示新舊差異。明確 clock revision 變更須重預覽，只有契約允許的同 revision 自然時間下降可接受，估算與實際金額分開驗證。 |
| A10 | Go／HTTP／瀏覽器涵蓋零、負值、int64 上限／溢位、超 JS safe integer、小數／指數／JSON number 拒絕、UTC 精度／範圍與 rational 分母。有效字串精確往返，非法輸入在命令／事實寫入前拒絕；Money 使用 BigInt，金融輸入保持十進位字串。 |
| A11 | 19 類共用資源採有界 rowid 游標與白名單篩選；真 HTTP 覆蓋跨頁插入、status／filter scope、catalog、usage、客戶 keyset，以及獨立 job／provider／command／history 機制。UI 明示非全域快照，缺來源保持 null／未知，第 0 帳期與 epoch 仍是真值；金融敏感 key 須能力，共用 handler 區分空資料／不存在／禁止／故障。 |
| A12 | 58 條已知操作路由逐頁有 390px 首個 Tab、label 關聯、ARIA 名稱與無溢位證據；行動選單 Enter／Escape 焦點、拒絕確認後焦點／編輯、欄位錯誤關聯及各獨立讀取呈現類型均有瀏覽器證據。共用狀態文字、精確輸入、provider 結果與離頁停止輪詢亦已驗；受控讀取／拒絕只證 UI，金融事實由真 Go／API／provider fixture 證明。 |
| A28 | 真 HTTP 資料庫故障回 QUERY_FAILED，不偽裝空資料或不存在。付款／退款 provider 暫不可查時保留原可重試命令與 unknown 義務，沒有成功收據，退款預留額仍保留；斷線與回應遺失恢復原命令。COMMAND_PENDING_RETRY 帶 command_id／retryable，瀏覽器重整／續跑保留原 ID；audit 的 actor／request／command／result 關聯與秘密排除均有直接證據。 |

```sh
rtk proxy go test ./... -count=1 -timeout=300s -json > /tmp/billforge-go-acceptance-diagnostic.json
```

## A13／A17 補充驗收證據（2026-10-03）

A13：下期 Pro 五席意圖與必要綁定衝突的瀏覽器案例 2／2 通過（27.2 秒）。A17：原始明細不變、原始／目前應收／已收／尚待支付固定金額與截斷歷史入口 2／2 通過（15.5 秒）；其中歷史導覽使用受控回應，四類 101 筆完整歷史則由隔離 SQLite 真實領域寫入，Go 2／2 通過。四個 quote 綁定／價格／revision Go 案例及既有 HTTP 游標契約亦通過。此輪未改執行邏輯，新增斷言晚於 156 項完整套件快照，屬補充證據。

```sh
rtk pnpm --dir web/admin exec playwright test e2e/admin.spec.ts --grep '(cancel, resume and schedule a plan change|a different seat price quote cannot use)' --reporter=line
rtk pnpm --dir web/admin exec playwright test e2e/admin.spec.ts --grep '(lost reduction response recovers|invoice history page follows)' --reporter=line
rtk go test ./lab -run 'TestAdminInvoice(HistoryPagesBeyondDetailLimitWithoutRepeats|ApplicationAndRefundHistoryPagesBeyondDetailLimit)$' -count=1 -timeout=120s
rtk go test ./lab ./api/admin -run 'Test(AdminChange(QuoteAndScheduledPlanAtomic|PreviewRejectsAnotherQuotesBindingForBothModes|PreviewRejectsSupersededPriceForBothModes|PreviewRejectsChangedRevisionForBothModes)|InvoiceHistorySessionCursorAndReadContracts)$' -count=1 -timeout=120s
```

## 限定場景驗收判定（2026-10-03）

歷史 checkpoint（已由上方最終本機驗收取代）：A08／A13／A15／A16／A17／A23／A26 已逐條滿足原場景條件，判定經獨立複查。A23 在既有比對案例補人工決議後的金融斷言，A16 補耗盡額度的停用原因。其他逐動作變體仍歸 A30，Web Admin 尚未整體簽核。 A13 補下期 Pro 五席顯示；A17 補四類完整歷史、原始明細不變與固定帳務數字。

完整瀏覽器回歸：156/156 (25.4m)；逐動作稽核 49／49、相關 Go 場景 9／9、補強瀏覽器斷言 2／2。

| ID | 本機通過 |
| --- | --- |
| A08 | 41 個預覽動作到期後沿原鍵找回原命令，命令與收據數不增加。共用受理路徑先找 actor／key 重播，再核預覽與來源。Go 另驗真實時鐘 revision 變更、原結果參照、改 payload 衝突與過期新鍵拒絕；C02／C07 已驗重整恢復。 |
| A13 | Basic 新購、Pro 下期排程、取消／恢復、過期／已購報價拒絕與 quote/binding 原子性均有直接證據。席次、選價與 revision 不符時拒絕且不寫入；訂閱詳情現直接核對下期 pro-v1／5 席意圖，成功與綁定衝突的補強瀏覽器案例 2／2 通過。 |
| A15 | 真實 2001 拒絕驗 USD 20 可收上限；合法部分付款與競爭預覽只收固定 USD 15／20，各一筆 allocation 與收據。C08 保留原失敗操作與帳單；C09 指定派送時較早佇列仍待處理。失敗／等待／確認與原操作查證入口已驗。 |
| A16 | 退款預留 500 與抵扣 500 共用 funded Credit 1000，不超支。UNKNOWN 保留額度，原鍵查證後只有一筆退款；指定 400 退款連回原 capture，較早退款未派送。詳情區分可用／保留／已退，耗盡時明示原因並停用支出入口；補強 UI 斷言另以 2／2 通過。 |
| A17 | C11 減額及原鍵恢復後原始 Lines 相同；畫面核對原始 USD20、目前應收 USD15、已收 USD20、尚待支付 USD0。C11／C13／C14 保留來源、固定 534 oracle 與唯一重播效果。更正、Credit、抵扣、退款各有 101 筆真實資料，以有界游標查完且不重複，新插入不混入舊游標；詳情截斷、完整歷史入口、非法／跨種類游標均有證據。補強瀏覽器 2／2、歷史 Go 2／2 通過。 |
| A23 | 瀏覽器驗原 expected／actual、安全修復與 verification、revision 改變時 blocked、provider 金額不符須人工審查。C35 後比對案例核對未清 2000、已用 0、零本地更正／allocation；provider 事實不變，原修復鍵與重整恢復亦有證據。 |
| A26 | 穩定 local-admin actor 在新 session 可找回原命令；真實重啟使舊 session 失效後仍恢復原命令。重整與真實寫入逾時保留原鍵；外部不確定性維持等待，provider 事實、金融效果與收據皆唯一。 |

```sh
BILLFORGE_E2E_ACTION_CASE_AUDIT=/tmp/billforge-admin-full-final-cases.jsonl rtk pnpm --dir web/admin exec playwright test --reporter=line --output=/tmp/billforge-admin-full-final
rtk proxy node web/admin/e2e/audit-action-cases.mjs /tmp/billforge-admin-full-final-cases.jsonl
rtk pnpm --dir web/admin exec playwright test e2e/admin.spec.ts --grep '(C12 applies funded credit|credit detail separates available)' --reporter=line
rtk go test ./lab -run 'TestAdmin(SafeEntitlementRepairMatchesDomain|UnsafeProviderCaptureStaysManualReviewMatchesDomain|RepairRejectsChangedSourceRevisionMatchesDomain|ManualDecisionStoresReasonAndReceipt|SuccessfulCommandReplayPrecedesExpiredPreviewAndClockRevision|CreatePaymentReplacesUnsentOperationAtomically|RetryPaymentTargetsDefinitivelyFailedOperation|CreditApplicationAndUnknownRefundShareFundedBudget|RefundDispatchTargetsExactRefund)$' -count=1 -timeout=120s
```

## 輸入修正與舊錯誤清除（2026-10-03）

C07 在修改金額後仍保留失敗預覽或已拒絕命令的錯誤；C03／C04 共用方案表單在更正報價後也保留綁定衝突錯誤。輸入變更時現在會重設對應 mutation 狀態。既有輸入 revision 仍會拒絕較舊的預覽回應；結果不確定的命令意圖仍保留並鎖定編輯。

| 範圍 | 回歸證據 | 定向結果 |
| --- | --- | --- |
| C07 預覽 | 真實 HTTP 409 拒絕對 USD 20.00 帳單輸入 2001；改成 1000 後舊錯誤消失，必須重新取得 USD 10.00 預覽，零 C07 命令入場。 | 2／2，含成功預覽變更後失效（15.4 秒）。 |
| C07 命令 | 受控 HTTP 422 未受理命令；由 300 改成 400 後舊拒絕錯誤消失，重新預覽與確認只建立一筆 400 操作。 | 2／2，含跨 session／重整沿原鍵恢復（20.1 秒）。 |
| C03 綁定 | 真實 HTTP 409 拒絕七席報價配五席綁定；改回原報價後舊錯誤消失，必須重新取得 USD 100.00 預覽，零 C03 命令入場。 | 2／2，含成功方案預覽變更後失效（24.7 秒）。 |

三項回歸在修正前均因舊錯誤仍存在而失敗，修正後通過。C07 命令拒絕為受控 UI 證據，兩項預覽拒絕來自真實 Go API。前端建置通過；獨立程式與測試審查沒有發現待修問題。完整瀏覽器回歸：156/156 (25.4m)；逐動作稽核 49／49。

```sh
rtk pnpm --dir web/admin exec playwright test e2e/admin.spec.ts --grep 'editing (an overpayment|a payment amount)' --reporter=line
rtk pnpm --dir web/admin exec playwright test e2e/admin.spec.ts --grep '(a different seat price quote|cancel, resume and schedule a plan change)' --reporter=line
rtk pnpm --dir web/admin exec playwright test e2e/admin.spec.ts --grep '(rejected payment creation restores focus|a lost payment command response)' --reporter=line
```

## 付款競爭與實際收款補驗（2026-10-03）

歷史 checkpoint（已由上方最終本機驗收取代）：補強兩個既有瀏覽器案例，使用真實 Go HTTP、隔離 commerce/provider SQLite 與 fake provider，定向 **2／2 通過**。C07 現在派送勝出的替代操作；C08 改用固定 USD 20.00 oracle，避免只比較同一操作衍生的兩個值。兩案均核對唯一帳單 allocation 與 C09 收據。請求稽核另驗 C01、C02、C07、C08、C09、C47 的原鍵重播與同鍵不同 payload 拒絕。A30 仍為部分完成；本次定向紀錄已由上方完整回歸與限定場景判定補充。

```sh
BILLFORGE_E2E_ACTION_CASE_AUDIT=/tmp/billforge-payment-cases-20261003.jsonl pnpm --dir web/admin exec playwright test e2e/admin.spec.ts --grep 'competing (payment|retry) previews' --reporter=line --output=/tmp/billforge-payment-verified-20261003
go test ./lab -run 'TestAdminCompeting(Payment|Retry)Previews' -count=1 -timeout=120s
```

重跑時請改用尚未使用的稽核與輸出路徑。本次瀏覽器 2／2 通過，耗時 26.2 秒；Go 2／2 通過。`pnpm --dir web/admin build` 亦通過。

## 瀏覽器請求、收據與重播證據（2026-09-29）

更新後的 `admin.spec.ts` 瀏覽器案例於 2026-09-29 **104／104** 項通過（16.4 分鐘）；此前完整 Playwright 套件 **155／155** 項通過。`audit-action-cases.mjs` 確認 **49／49** 項動作都有與成功收據配對的瀏覽器請求、原鍵重播，以及同鍵不同請求的衝突拒絕。

對每項動作，稽核從新的管理員 session 使用原始請求 body 與冪等鍵重播一筆成功命令，確認伺服器回傳原命令 ID、收據仍只有一筆，且命令表未新增資料列。接著在同一冪等鍵下改動一個有效請求欄位，要求回傳 `409 IDEMPOTENCY_CONFLICT`，並再次確認命令與收據都未增加。每次執行請使用新的輸出路徑：

```sh
BILLFORGE_E2E_ACTION_CASE_AUDIT=/tmp/billforge-action-cases.jsonl pnpm --dir web/admin test:e2e
node web/admin/e2e/audit-action-cases.mjs /tmp/billforge-action-cases.jsonl
```

這項檢查只覆蓋每項動作的一條請求與冪等鍵路徑，尚不足以證明所有金流事實、來源修訂版或預覽衝突，以及回應中斷後的恢復。下文記錄逐案證據與剩餘缺口。

## 共用執行路徑

- 所有命令實際使用 `POST /admin/api/commands`，以 `action_id` 指定 C01–C49；需要預覽的命令先使用 `POST /admin/api/previews`。下表的路徑是 Web Admin 頁面路徑，不是各動作獨立的 HTTP endpoint。這與工程契約中逐資源的擬定 POST 路徑不同；目前採單一受控入口，應以這個已實作契約作為後續驗收依據。
- `api/admin/permissions.go` 決定能力，`lab/admin_commands.go` 負責 canonical payload、冪等鍵、預覽綁定、命令受理及執行。命令可由 `GET /admin/api/commands/{id}` 找回；可恢復者使用 `POST /admin/api/commands/{id}/resume`。批次另由 `GET /admin/api/jobs/{id}` 查逐項結果。
- `api/admin/action_http_matrix_test.go` 對 49 個動作的命令與預覽入口逐項驗無 session、缺 CSRF、缺能力時的 HTTP 狀態與錯誤碼；另在具備能力時逐項送未知 payload 欄位，驗 422 且未受理任何命令。`api/admin/permissions_test.go` 驗每個動作有能力映射，及具備其能力時的授權判斷。這些測試尚未證明每種合法 payload 或各種金融結果。
歷史 checkpoint（已由上方最終本機驗收取代）：- `lab/admin_numeric_payload_test.go` 驗 C07/C15 的大整數精確度與不合法金額格式、C18 的精確費率分母、C33 的 UTC 截止時間邊界。`api/admin/numeric_http_test.go` 進一步在真實管理 HTTP handler 驗 C07／C11／C12／C15 不合法金額、C18 不合法分母和 C33 不合法 UTC 於命令或預覽入場前被拒，且無命令；有效大整數字串在 C07／C15 抵達來源查詢，C18 預覽保留精確分母。`web/admin/e2e/admin.spec.ts` 另驗 C07／C11／C12／C15 表單拒絕超出 int64 上限、改為超過 JavaScript safe integer 但仍在 int64 範圍後解除錯誤，且不建立命令。其餘欄位仍需各自的 HTTP／browser 邊界案例。
歷史 checkpoint（已由上方最終本機驗收取代）：- `lab/admin_preview_admission_test.go` 在 C07 驗預覽的 actor、動作、對象、內容、期限及單次 claim；原鍵在預覽到期後仍找回原命令，不同 payload 不能共用該鍵。受理流程現在對所有 R 類動作使用同一組核對，且在不符時不建立命令；各動作的來源版本與交易內重算仍需個別證據。
- `lab/admin_reconcile_no_evidence_test.go` 驗 C10/C17 查證時 provider 尚無終局事實，命令維持待查證且無成功收據；退款額度保持預留。原 provider key 後來出現成功證據，原命令才完成。瀏覽器另驗 C10 的等待與恢復。
- 完整 Playwright 套件於 2026-09-29 **155** 項通過，其中包含 **104** 筆管理介面瀏覽器案例。路由、過期 session、篩選與查詢錯誤另由 `lab/admin_resource_filters_test.go`、`lab/admin_resource_queries_test.go`、`api/admin/resource_filters_test.go` 和 `api/admin/query_params_test.go` 支撐。路由檢查只能證明頁面可抵達；下方動作表不代表所有錯誤路徑均已覆蓋。

`R` 表示必須持有效預覽確認；`N` 表示無預覽，但仍須授權、冪等與來源檢查。表中路徑省略 `/admin` 前綴。Go 證據路徑省略 `lab/` 前綴。

| 動作 | UI 路徑 | 能力 | 預覽 | 交易測試檔 |
| --- | --- | --- | --- | --- |
| C01 | `/quotes/new` | `subscription.manage`；合約另需 `contract.manage` | N | `admin_commands_test.go`、`admin_contract_actions_test.go`、`admin_financial_parity_test.go`（Pro 五席金融事實比對）、`admin_contract_parity_test.go`（Net30 合約報價比對）、`web/admin/e2e/admin.spec.ts`（購買與變更綁定報價） |
| C02 | `/quotes/:id/accept` | `subscription.manage`；合約另需 `contract.manage` | R | `admin_commands_test.go`、`admin_financial_parity_test.go`（Pro 五席金融事實比對）、`admin_contract_parity_test.go`（Net30 接受後帳單與權益比對）、`admin_stopped_writer_test.go`（停止後拒絕新預覽，舊預覽執行失敗且無訂閱／收據）、`web/admin/e2e/admin.spec.ts`（購買與預覽後來源變動） |
| C03 | `/subscriptions/:id/schedule-plan` | `subscription.manage` | R | `admin_schedule_plan_test.go`、`admin_financial_parity_test.go`（下期 Pro 五席排程及 C44 續約與直接領域路徑比對）、`price_migration_test.go`、`web/admin/e2e/migration-conflict.spec.ts`（遷移待處理時預覽回 409，正式排程亦拒絕）；`admin.spec.ts` 實際排程並核對目標價、唯一訂閱與成功收據。 |
| C04 | `/subscriptions/:id/upgrade` | `subscription.manage` | R | `admin_immediate_upgrade_test.go`、`admin_financial_parity_test.go`（週期中點升級的領域／管理路徑金額與權益比對）、`web/admin/e2e/admin.spec.ts`（期中升級與舊預覽重算） |
| C05 | `/subscriptions/:id/cancel` | `subscription.manage` | R | `admin_commands_test.go`、`price_migration_test.go`、`web/admin/e2e/migration-conflict.spec.ts`（遷移 pending／conflicted 時預覽回 409，沒有預覽或排程寫入）；`admin.spec.ts` 實際排定取消並核對此訂閱唯一成功收據。 |
| C06 | `/subscriptions/:id/cancel`（依狀態顯示恢復） | `subscription.manage` | R | `admin_commands_test.go`、`admin_stopped_writer_test.go`（停止後拒絕新預覽，舊預覽執行失敗且保留取消排程）、`web/admin/e2e/admin.spec.ts` 實際恢復取消排程、核對排程狀態與此訂閱唯一成功收據。 |
| C07 | `/invoices/:id/payments/new` | `finance.adjust` | R | `admin_payment_test.go`（含收據失敗時保留舊收款、重啟後原鍵恢復）、`admin_competing_payment_preview_test.go`（兩個已受理預覽競爭，唯一替代操作與收據、舊命令失效及雙鍵重播）、`admin_cross_instance_payment_test.go`（兩個獨立 Lab 實例共用 SQLite 並行執行）、`admin_cross_process_payment_test.go`（兩個獨立 OS 進程同步執行）、`web/admin/e2e/admin.spec.ts`（部分付款、預覽後餘額變動及雙分頁替代操作競爭）、`admin_payment_dispatch_interleaving_test.go`（跨實例付款建立、舊預覽失效及精確派送） |
| C08 | `/payments/:id/retry` | `finance.adjust` | R | `admin_payment_test.go`、`admin_competing_payment_preview_test.go`（兩個已受理重試預覽只建立一筆新義務，失效命令零成功收據、原鍵重播）、`admin_cross_instance_payment_test.go`（兩個獨立 Lab 實例共用 SQLite 並行執行）、`admin_cross_process_payment_test.go`（兩個獨立 OS 進程同步執行）、`web/admin/e2e/admin.spec.ts`（確定失敗後重試與餘額變動）、`admin_payment_dispatch_interleaving_test.go`（跨實例付款建立、舊預覽失效及精確派送）、`web/admin/e2e/admin.spec.ts`（雙分頁 C08 預覽競爭、HTTP 409 與勝出操作後續 C09 精確派送） |
| C09 | `/payments/:id/dispatch` | `finance.adjust` | R | `admin_external_test.go`、`web/admin/e2e/admin.spec.ts`（付款 provider commit 後中斷；待查證時調整時鐘仍沿原 business time 完成）、`admin_payment_dispatch_interleaving_test.go`（跨實例付款建立、舊預覽失效及精確派送）、`web/admin/e2e/admin.spec.ts`（C07 取代後舊 C09 預覽 HTTP 409，僅新操作派送）、`web/admin/e2e/admin.spec.ts`（指定較晚付款操作派送，不取出佇列較早操作） |
| C10 | `/payments/:id/reconcile` | `finance.adjust` | N | `admin_external_test.go`、`admin_reconcile_no_evidence_test.go`、`provider_lookup_failure_test.go`、`api/admin/provider_lookup_failure_test.go`、`web/admin/e2e/admin.spec.ts` |
| C11 | `/invoices/:id/reductions/new` | `finance.adjust` | R | `admin_reduction_test.go`、`admin_financial_parity_test.go`（部分付款與已付款減額兩種資金來源和直接領域路徑比對）、`web/admin/e2e/admin.spec.ts` 的帳單來源案例 |
| C12 | `/credits/:id/apply` | `finance.adjust` | R | `admin_apply_credit_test.go`、`admin_financial_parity_test.go`（跨帳單套用額度與直接領域路徑比對）、`admin_credit_budget_interleaving_test.go`（跨實例與 C15 爭用同一額度）、`web/admin/e2e/admin.spec.ts` 的 C12 案例 |
| C13 | `/jobs/change-corrections` | `finance.adjust` | R | `admin_change_corrections_test.go`（101 筆來源變動後跨批輪轉）、`web/admin/e2e/admin.spec.ts` 的延遲更正來源案例：實際執行批次，模擬提交後回應遺失；重整沿同一冪等鍵找回，SQLite 核對唯一工作、逐項結果、更正與命令收據。 |
| C14 | `/changes/:id/resolve-unfulfilled` | `finance.adjust` | R | `admin_unfulfilled_test.go`、`web/admin/e2e/admin.spec.ts` 的未履行升級案例：實際執行未履行決議，模擬提交後回應遺失；重整沿同一冪等鍵找回，SQLite 核對唯一決議、更正、Credit 與命令收據。 |
| C15 | `/credits/:id/refunds/new` | `finance.adjust` | R | `admin_reserve_refund_test.go`（含收據失敗回滾、重啟與原鍵恢復）、`admin_refund_budget_parity_test.go`（與直接領域路徑比對 funded credit 預算）、`admin_credit_budget_interleaving_test.go`（跨實例與 C12 爭用同一額度）、`web/admin/e2e/admin.spec.ts`（已收款減額後預留退款、C12 額度爭用、雙分頁 C15 預覽失效後重新確認與唯一收據） |
| C16 | `/refunds/:id/dispatch` | `finance.adjust` | R | `admin_external_test.go`、`web/admin/e2e/admin.spec.ts`（退款 provider commit 後中斷；確定失敗時保留額回到可用額度）、`admin_refund_dispatch_interleaving_test.go`（C15／C16 跨實例競爭、唯一 provider 退款與額度收據） |
| C17 | `/refunds/:id/reconcile` | `finance.adjust` | N | `admin_external_test.go`、`admin_reconcile_no_evidence_test.go`、`provider_lookup_failure_test.go`、`admin_refund_reconcile_test.go`（UNKNOWN 退款查證、同鍵重播與唯一收據）、`web/admin/e2e/admin.spec.ts`（C16 回應遺失後經 C17 查證，原 C16 仍沿原鍵完成，提供者退款唯一） |
| C18 | `/prices/pro/new` | `catalog.publish` | R | `admin_catalog_test.go`、`web/admin/e2e/admin.spec.ts`（發布元件與 checksum、同版號與原 ID 變更衝突、所有數值欄位的 UI 非法邊界、版本來源變動；提交後回應遺失再以原鍵恢復，SQLite 價格、命令與收據各一筆）、`catalog_receipt_failure_test.go`（真實 HTTP 收據 INSERT 故障回滾及原鍵恢復）、`web/admin/e2e/migration-conflict.spec.ts`（遷移目標價格）；`admin_price_minimum_upfront_test.go` 與 `minimum_upfront_http_test.go` 驗固定費用加最少一席的 `int64` 邊界與預覽拒絕 |
| C19 | `/meters/new` | `catalog.publish` | R | `admin_catalog_test.go`（同 ID 不同 schema 的舊預覽失效、唯一收據、成功／失敗同鍵重播及改 payload 拒絕）、`api/v1_test.go`（管理發布 AI token meter 後檢查舊 client）、`web/admin/e2e/admin.spec.ts`（雙分頁衝突顯示 `PREVIEW_STALE`、既有 unit 不被覆寫；建立 AI token meter 並完成報價、接受及用量記錄） |
| C20 | `/prices/metered/new` | `catalog.publish` | R | `admin_catalog_test.go`（同 ID 不同金額、同方案版號不同 ID 的舊預覽失效，唯一價格與收據、原鍵重播與改 payload 拒絕）、`api/v1_test.go`（新費率揭露及舊 client 能力守衛）、`web/admin/e2e/admin.spec.ts`（雙分頁舊價格預覽顯示 `PREVIEW_STALE`、已發布 3500 與元件不被覆寫；全部數值欄位的 UI 非法邊界、AI 報價與 token 用量）；`admin_price_minimum_upfront_test.go` 與 `minimum_upfront_http_test.go` 驗固定費用加最少一席的 `int64` 邊界與預覽拒絕 |
| C21 | `/catalog-selections/new` | `catalog.publish` | R | `admin_catalog_test.go`（同方案／cohort／時點的兩個已受理命令競爭，舊命令 `PREVIEW_STALE`、唯一選價與收據、成功／失敗鍵重播）、`api/v1_test.go`（選價後同一報價的舊／新 client 行為）、`web/admin/e2e/migration-reversal.spec.ts`（選回舊價格）、`web/admin/e2e/admin.spec.ts`（雙分頁舊預覽回 409 且不覆寫先選價格；同時點改選回 409；AI 價格選價後報價接受） |
| C22 | `/price-migrations/new` | `migration.manage` | R | `admin_migrations_test.go`、`price_migration_test.go`（來源價格、席次與排程變動後的批次暫停及金融事實不變）、`web/admin/e2e/migration-reversal.spec.ts`（新批次反向遷移）、`migration-conflict.spec.ts`（revision 變動，以及來源價格、席位、排程三種獨立批次衝突）；`admin.spec.ts` 實際建立兩戶批次，SQLite 核對該批次唯一成功收據。 |
| C23 | `/price-migrations/:id/pause` | `migration.manage` | N | `admin_migrations_test.go`、`web/admin/e2e/admin.spec.ts` 實際暫停批次，SQLite 核對 paused 狀態及此批次唯一成功收據。 |
| C24 | `/price-migrations/:id/skip` | `migration.manage` | R | `admin_migrations_test.go`、`web/admin/e2e/migration-conflict.spec.ts`（衝突後明確略過，不撤銷已套用戶）；`admin.spec.ts` 略過一戶並核對 skipped 項目及此批次唯一成功收據。 |
| C25 | `/price-migrations/:id/resume` | `migration.manage` | R | `admin_migrations_test.go`、`web/admin/e2e/admin.spec.ts` 恢復後保留略過戶、未完成戶回 pending，SQLite 核對 active 狀態及此批次唯一成功收據。 |
| C26 | `/usage-events/new` | `usage.manage` | N | `admin_commands_test.go`、`web/admin/e2e/admin.spec.ts`（原事件入帳、回應遺失恢復、相同事件重送與變更 payload 拒絕） |
| C27 | `/usage-adjustments/new` | `usage.manage` | R | `admin_usage_actions_test.go`（併發調整耗用原事件剩餘量後舊預覽失效、零寫入，新預覽只反向剩餘量）、`web/admin/e2e/usage-preview-stale.spec.ts`（雙分頁併發調整回 409，原數量無法重新預覽，改為剩餘量後才成功）、`web/admin/e2e/admin.spec.ts`（用量調整與差額） |
| C28 | `/usage-periods/:id/close` | `usage.manage` | R | `admin_usage_actions_test.go`（預覽後新增符合 cutoff 的事件，舊關帳零計價修訂／收據，新預覽精確關帳）、`web/admin/e2e/usage-preview-stale.spec.ts`（雙分頁來源變動回 409，顯示 20000→20010，重新確認前零修訂，最終唯一收據）、`web/admin/e2e/admin.spec.ts`（關帳） |
| C29 | `/usage-periods/:id/rerate` | `usage.manage` | R | `admin_usage_actions_test.go`（預覽後新增晚到事件，舊重算不新增修訂／收據，新預覽含完整晚到用量）、`web/admin/e2e/usage-preview-stale.spec.ts`（雙分頁晚到事件回 409，顯示 20020→20030，重新確認前保留原修訂，最後僅一筆新修訂／收據）、`web/admin/e2e/admin.spec.ts`（晚到事件重算） |
| C30 | `/jobs/usage-credit-notes` | `finance.adjust` | R | `admin_usage_credit_notes_test.go`（101 個合成帳期衝突後跨批輪轉）、`admin_batch_jobs_test.go`（首項提交後重開只完成剩餘項）、`web/admin/e2e/admin.spec.ts`：負差額後實際執行 Credit Note 批次，模擬提交後回應遺失；重整沿同一冪等鍵找回，SQLite 核對唯一工作、逐項結果、Credit Note 與命令收據。 |
| C31 | `/contracts/new` | `contract.manage` | R | `admin_contract_actions_test.go`（同 ID 同內容併發發布只保留一版與唯一收據；不同內容令舊預覽失效、無成功收據且不可重寫）、`admin_contract_parity_test.go`（發布後合約來源與直接領域路徑比對）、`web/admin/e2e/admin.spec.ts`（雙分頁同 ID 不同內容回 409，顯示失效與重新預覽失敗；企業合約建立與 Net30）；`admin_price_minimum_upfront_test.go` 與 `minimum_upfront_http_test.go` 驗固定費用加最少一席的 `int64` 邊界與預覽拒絕 |
| C32 | `/jobs/contract-collections` | `finance.adjust` | R | `admin_contract_actions_test.go`（101 筆到期操作跨批收款；預覽後另一收款器先建立 capture outbox 時本批項目記為 `SOURCE_CHANGED`，不重複派送，同鍵找回原命令）、`admin_batch_jobs_test.go`（首項提交後重開只建立剩餘 capture outbox）、`admin_contract_parity_test.go`（到期收款與直接領域路徑比對）、`web/admin/e2e/admin.spec.ts`（合約到期收款批次） |
| C33 | `/reconciliation-runs/new` | `reconciliation.repair` | N | `admin_reconciliation_test.go`、`web/admin/e2e/admin.spec.ts`（差異建立與修復後 verification） |
| C34 | `/discrepancies/:id/repair` | `reconciliation.repair` | R | `admin_reconciliation_test.go`、`web/admin/e2e/admin.spec.ts`（安全修復、來源變動阻擋與 verification） |
| C35 | `/discrepancies/:id/manual-decisions` | `reconciliation.repair` | R | `admin_reconciliation_test.go`（另一審核者於預覽後先記錄決議，舊命令 `PREVIEW_STALE` 且零額外決議／收據；新預覽才記錄新決議）、`web/admin/e2e/manual-decision-stale.spec.ts`（雙分頁 409、顯示 `open → investigating`、再次確認前唯一決議／收據）、`web/admin/e2e/admin.spec.ts`（金額不符的人工作業記錄） |
| C36 | `/account-migrations/new` | `migration.manage` | R | `admin_account_links_test.go`、`web/admin/e2e/admin.spec.ts` 實際連結帳戶，顯示 owner 與門檻；SQLite 核對該 legacy ID 唯一成功收據。 |
| C37 | `/account-migrations/:id/shadow-quotes` | `migration.manage` | N | `admin_account_links_test.go`、`web/admin/e2e/admin.spec.ts` 實際記錄報價 shadow，驗比對一致及該帳戶唯一成功收據。 |
| C38 | `/account-migrations/:id/shadow-entitlements` | `migration.manage` | N | `admin_account_links_test.go`、`web/admin/e2e/admin.spec.ts` 實際記錄權益 shadow，驗 adapter 權益與該帳戶唯一成功收據。 |
| C39 | `/account-migrations/:id/provenance` | `migration.manage` | R | `admin_account_links_test.go`、`admin_provenance_stale_test.go`（預覽後同 legacy invoice 被不同映射先回填，原命令 `PREVIEW_STALE`，不覆寫來源或建立成功收據）、`web/admin/e2e/provenance-preview-stale.spec.ts`（雙分頁 409，保留先寫入的 manual review 映射與唯一收據）、`web/admin/e2e/admin.spec.ts`（不完整歷史來源入場） |
| C40 | `/account-migrations/:accountId/provenance/:legacyInvoiceId/resolve` | `migration.manage` | R | `admin_account_links_test.go`、`admin_provenance_stale_test.go`（另一審核者先完成來源決議，舊預覽失效且無重複決議事件／收據）、`web/admin/e2e/provenance-preview-stale.spec.ts`（雙分頁 409，保留唯一決議事件與收據）、`web/admin/e2e/admin.spec.ts`（歷史來源人工審核） |
| C41 | `/account-migrations/:id/switch-read` | `migration.manage` | R | `admin_account_links_test.go`、`admin_cutover_stale_test.go`（預覽後新增不一致報價 shadow，舊切讀 `PREVIEW_STALE`，讀寫 owner 不變且零成功收據）、`web/admin/e2e/admin.spec.ts`（未過門檻阻擋與切讀） |
| C42 | `/account-migrations/:id/switch-writer` | `migration.manage` | R | `admin_account_links_test.go`、`admin_cutover_stale_test.go`（切讀後預覽切寫，準備條件下降時舊命令失效且 writer 保持 legacy）、`web/admin/e2e/cutover-readiness-stale.spec.ts`（雙分頁 shadow 變動回 409，零切寫事件／收據）、`web/admin/e2e/admin.spec.ts`（切讀後切寫；成功回應遺失後重新整理，以原 request key 找回，命令、收據與 `writer_cutover` 各只一筆） |
| C43 | `/account-migrations/:id/stop` | `migration.manage` | R | `admin_account_links_test.go`、`admin_stopped_writer_test.go`（C02／C05／C06 預覽與停止後原命令）、`web/admin/e2e/admin.spec.ts`（停止事件及 owner；C02／C05 新預覽回 409，零後續商務命令）；SQLite 核對停止命令唯一成功收據。 |
| C44 | `/jobs/renewals` | `operations.run` | R | `admin_maintenance_jobs_test.go`（預覽固定成員；第二戶確認前到期仍須新批次；前 100 戶因遷移暫停而等待時，第 101 戶可由下批續約）、`admin_batch_jobs_test.go`（首戶提交後重開只續約剩餘戶）、`admin_financial_parity_test.go`（月底及排程變價續約比對）、`admin_contract_parity_test.go`（缺後續合約價格時暫停續約比對）、`web/admin/e2e/migration-reversal.spec.ts`（前後兩期金額及指派來源）、`migration-conflict.spec.ts`（部分套用、暫停、略過後不同價格續約；三種來源變動均無新增帳期或帳單） |
| C45 | `/jobs/entitlement-refresh` | `operations.run` | R | `admin_maintenance_jobs_test.go`（101 戶跨三批輪轉，尾端戶不飢餓；預覽後其中一戶 revision 改變時該項 `SOURCE_CHANGED` 且不重建其權益，另一戶仍成功並保留唯一批次收據）、`web/admin/e2e/entitlement-batch-source-stale.spec.ts`（雙分頁變更訂閱，工作詳情將衝突與成功戶分列，SQLite 保留唯一收據）、`web/admin/e2e/admin.spec.ts`（固定成員）、`web/admin/e2e/maintenance-backlog.spec.ts`（超過 100 戶時提示仍需下一批） |
| C46 | `/lab/clock` | `lab.control` | R | `admin_controls_test.go`、`web/admin/e2e/admin.spec.ts`（C09 待查證期間前進一天，不自動收款且不改原命令業務時間） |
| C47 | `/lab/payment-decisions/:id` | `lab.control` | R | `admin_controls_test.go`（provider 決策已提交、付款已 capture 後，重啟沿原 receipt 完成命令；相反結果競爭與終態後新命令均拒絕）、`web/admin/e2e/admin.spec.ts`（第二個相反決策顯示 `DOMAIN_REJECTED`、僅首命令有收據；確定失敗後重試） |
| C48 | `/lab/refund-decisions/:id` | `lab.control` | R | `admin_controls_test.go`（相反結果競爭與終態後新命令均拒絕）、`web/admin/e2e/admin.spec.ts`（第二個相反決策顯示 `DOMAIN_REJECTED`、僅首命令有收據且派送前無 provider 退款；確定失敗後 C16 派送釋放保留額；C16 回應遺失後經 C17 查證，最終僅一筆） |
| C49 | `/lab/faults/:id` | `lab.control` | R | `admin_controls_test.go`（另一筆付款先執行不會誤領指定票據；同操作第二張票據以 `failed/DOMAIN_REJECTED` 結束、原鍵重播不新增票據；付款／退款在領票後、provider 呼叫前重啟仍沿原命令使用票據）、`web/admin/e2e/admin.spec.ts`（重複建票錯誤可見且原票據繼續派送；一次性故障票據與原命令恢復）。`TestAdminFaultTicketsKeepPendingTicketVisibleAfterRecentUsedTickets` 與 `lab-fault-pagination.spec.ts` 驗待使用票據優先及有界游標可跨頁找回。 |

## C01／C03／C04 變更報價的衝突與原子性

歷史 checkpoint（已由上方最終本機驗收取代）：`lab/admin_change_binding_mismatch_test.go` 驗 C01 在訂閱 revision 失效時回滾報價與綁定，保留失敗命令而不產生成功收據；C03／C04 均在報價綁定不符、選價被取代或訂閱 revision 改變時拒絕預覽，沒有新預覽、命令或方案變更。瀏覽器以真實表單驗 C01 零殘留，C03／C04 競爭者修改訂閱後重新預覽的原因顯示，以及 C18／C21 選用新 Pro 價格後 C03 舊報價被拒絕。這些定向證據尚未覆蓋 C01–C49 每項動作的完整重播與恢復矩陣。

## C03／C04 回應遺失與原鍵恢復

`web/admin/e2e/admin.spec.ts` 的兩個真實瀏覽器情境在伺服器已接受並完成 C03／C04 後中斷原回應。重新整理後介面以原冪等鍵取回同一命令；同鍵不同報價 ID 回 `409 IDEMPOTENCY_CONFLICT`。C03 只有一筆變更排程與成功收據。C04 只有一筆立即變更、帳單、付款操作與成功收據，訂閱服務在付款前仍為 Basic。此證據涵蓋指定兩個動作的提交後回應遺失，不代表其他 47 個動作的中斷路徑已驗完。

`TestAdminScheduledPlanReceiptFailureRollsBackAndRecoversOriginalCommand` 另在 C03 收據寫入時注入 SQLite 失敗：原命令保持 `accepted`，排程與訂閱 revision 不變，沒有新增付款或 provider capture。重啟後兩次恢復只產生一筆排程與收據；原鍵重播返回同一命令，改 payload 則衝突。

## C18 Pro 價格發布的端到端追蹤

| 面向 | 目前證據與判定 |
| --- | --- |
| 入口與執行 | `/prices/pro/new` 使用共用 `ActionForm` 建立 C18 預覽，再以原預覽及冪等鍵提交共用命令入口。`executeAdminCatalogTx` 在同一交易比對來源快照後呼叫 `publishProPriceTx`，回傳價格版本 ID、方案與 checksum；成功命令與 `admin_command_receipts` 同交易保存。 |
| 權限與輸入 | C18 需要 `catalog.publish`；共用 HTTP 權限矩陣驗未登入、CSRF 與缺能力拒絕。`canonicalAdminCatalogPayload`、領域發布路徑與管理 HTTP 驗正整數、UTC、費率分母，以及固定費用加最少一席不超出 `int64`。 |
| 價格不變性 | `admin_catalog_test.go` 與瀏覽器案例核對已發布元件、checksum、同版號或同 ID 不同內容的衝突，以及來源變動時舊預覽失效。 |
| 回應遺失與原鍵恢復 | `lost C18 publish response recovers one price and receipt with the original key` 讓管理 HTTP 完成 C18 後切斷首個回應。介面保留未確認意圖，重整後用原 request key 查回成功命令；SQLite 驗同一價格版本、命令與收據各一筆，checksum 存在且沒有新增付款操作。定向 Playwright 案例通過。 |
| 收據寫入故障 | `TestCatalogPublishReceiptFailureRecoversOriginalHTTPCommand` 令隔離 SQLite 的收據 INSERT 失敗；真實管理 HTTP 回 503 `COMMAND_PENDING_RETRY` 與原命令 ID，價格版本、元件及收據均未提交。移除故障後同鍵重送兩次只建立一筆價格、三個元件及一筆與 checksum 對應的收據；改 payload 重播回 409 `IDEMPOTENCY_CONFLICT`。定向 Go 案例以 `-count=3` 通過。 |
| 當時待驗收 | C18 的其他來源衝突交錯尚未逐一用真實 HTTP／SQLite 案例驗證；本項不據此宣稱 49 項操作整體結案。 |

## C19 計量表註冊的端到端追蹤

| 面向 | 目前證據 |
| --- | --- |
| 入口與契約 | `/meters/new` 的 React／Ant Design `ActionForm` 先送 `POST /admin/api/previews`，再以 `action_id=C19` 送 `POST /admin/api/commands`；命令可由原 ID 或冪等鍵查回。後端 `lab/admin_catalog.go` 比對預覽來源，再由 `registerMeterTx` 註冊不可變的 `meter_schemas` 列。 |
| 權限與輸入 | `catalog.publish` 為必需能力；共用 HTTP 矩陣涵蓋 401、CSRF 403、缺能力 403 與未知欄位拒絕。`canonicalAdminCatalogPayload` 要求非空 ID、source、unit 及正整數 schema 版本；`registerMeterTx` 再驗 ID／source 格式。 |
| 來源衝突 | `TestAdminMeterRegistrationRejectsStaleConflictingSchema` 先讓同 ID 不同單位各取得預覽，先執行 `token` 版本，再執行 `request` 舊命令：後者為 `failed/PREVIEW_STALE`，既有 unit 保持 `token`，兩命令只有一筆成功收據。新的衝突預覽直接被拒；成功與失敗命令各以原鍵重播，改 payload 使用舊鍵回冪等衝突。 |
| 實際介面 | `web/admin/e2e/admin.spec.ts` 的雙分頁案例先發布 `token`，舊 `request` 預覽確認回 HTTP 409／`PREVIEW_STALE`，介面明示失效且無法建立相衝突的新預覽；SQLite 只有一筆 schema、一筆成功收據及一筆失敗命令。另一案例以註冊的 meter 發布價格、建立報價並記錄用量。 |

歷史 checkpoint（已由上方最終本機驗收取代）：此處直接證明 C19 的同 ID 競爭與恢復；其餘非法字元、長度、正整數邊界及跨 actor 預覽仍以共用矩陣與單項 payload 測試為準，A30 尚未簽核。

## C20 計量價格發布的端到端追蹤

| 面向 | 目前證據 |
| --- | --- |
| 入口與契約 | `/prices/metered/new` 的 React／Ant Design 表單先取得 C20 預覽，再送共用命令入口；`lab/admin_catalog.go` 把 meter schema、既有 checksum、發布狀態及同方案版號占用者放進來源快照，執行時重算。`publishMeteredPriceTx` 在交易內建立版本與價格元件。 |
| 權限與輸入 | `catalog.publish` 為必需能力；共用 HTTP 矩陣涵蓋 session、CSRF、能力及未知欄位。既有瀏覽器案例逐欄驗固定金額、席次金額、包含用量與費率分子／分母的非法數值邊界；價格必須參照已註冊 meter。 |
| 版本不可變 | `TestAdminMeteredPriceRejectsStaleConflictingVersion` 先接受兩個同 ID 不同金額的預覽命令，3500 先發布後，3000 的舊命令為 `failed/PREVIEW_STALE`；版本維持 3500、兩個價格元件及一筆成功收據。成功與失敗命令各沿原鍵找回，改 payload 使用成功鍵回冪等衝突，新的相衝突預覽被拒。另驗不同 ID 競爭同一方案版號時只保留一個版本和一筆成功收據。 |
| 實際介面 | `web/admin/e2e/admin.spec.ts` 的雙分頁案例先發布 3500，再確認舊 3000 預覽，回 HTTP 409／`PREVIEW_STALE`；介面顯示失效並無法建立相衝突的新預覽，SQLite 保持 3500、兩個元件、一筆成功收據及一筆失敗命令。既有 AI 計量案例從 C19／C20／C21 走到報價、接受及 token 用量。 |

歷史 checkpoint（已由上方最終本機驗收取代）：這些案例驗同版本競爭，不推論所有日期、貨幣數值及跨 actor 預覽邊界已完成；A30 仍待逐項簽核。

## C21 目錄選價的端到端追蹤

| 面向 | 目前證據 |
| --- | --- |
| 入口與契約 | `/catalog-selections/new` 的 React／Ant Design 表單先預覽 C21，再送共用命令入口；來源快照包含價格版本的方案、發布狀態、checksum、有效期間及該方案／cohort／時點的既有占用者。執行前重算來源，最終寫入 `catalog_selection`。 |
| 權限與相容性 | `catalog.publish` 為必需能力；共用 HTTP 入場矩陣覆蓋 session、CSRF、能力及未知欄位。`api/v1_test.go` 驗新計量 SKU 選價後，舊 client 因缺能力宣告而拒絕接受報價，具備能力的 client 才能接受。 |
| 同時點競爭與重播 | `TestAdminCatalogSelectionRejectsStaleCompetingPrice` 在同方案、cohort、生效時點先接受兩個不同價格的預覽命令，先執行者保留唯一選價，後者為 `failed/PREVIEW_STALE` 且無成功收據。兩個原鍵各找回原命令，改 payload 使用成功鍵回冪等衝突；已有不同價格占用該時點時，新預覽被拒。 |
| 實際介面 | `web/admin/e2e/admin.spec.ts` 的雙分頁案例先選價格 B，再確認較早的價格 A 預覽：HTTP 409／`PREVIEW_STALE` 可見，介面要求重新預覽且相衝突的新預覽失敗；SQLite 只有 B 一筆選價、一筆成功收據與一筆失敗命令。其他案例驗同時點改選回舊價被拒，以及以新選價接受 AI 計量報價。 |

歷史 checkpoint（已由上方最終本機驗收取代）：這些案例直接驗 C21 的同時點爭用；不同時點的價格階梯與跨 actor 預覽仍須按適用契約核對，A30 尚未簽核。

## S07 跨動作金融正確性比對

`lab/admin_s07_parity_test.go` 在兩組隔離 commerce／provider SQLite 中，以同一業務時鐘分別執行直接領域入口與管理命令。它逐階段比對 C04 的期中升級金額與原 Basic 價格、C49 指定原付款的假服務商回應遺失、C09 待查證且沒有成功收據、C10 查證與原 C09 收據恢復。兩天後開通只須 C13 更正 534，帳單義務與淨實收同為 3466，並且只有一筆 534 funded grant。

另一情境在帳期邊界先用 C44 確認未知付款阻止續約；C10 查回晚到收款後，升級轉為 `needs_review`，C14 將 4000 義務與淨實收釋出並只留一筆 4000 funded grant，之後的 C44 才建立 2000 Basic 續約帳單。兩條路徑都核對 provider capture 總數維持兩筆（原 Basic 與升級付款）、每個成功管理命令一筆收據、待查證命令零成功收據，以及帳單原額／義務／總實收／釋出／淨實收／未清額。這是上述命令在 S07 組合下的直接證據，不代替每個命令的完整輸入與中斷矩陣。

## C07 建立付款操作的競爭與恢復追蹤

- **入口與授權：**`/invoices/:id/payments/new` 透過共用預覽及命令 API 提交 `C07`；能力為 `finance.adjust`。共用 HTTP 矩陣驗 session、CSRF 與能力守衛，`admin_payment_test.go` 驗正常建立及收據失敗後沿原鍵恢復。
- **來源與執行：**`adminCreatePaymentPreview` 將帳單應收、帳期／到期界線、既有付款操作狀態與指定金額綁入預覽；`executeCreatePaymentTx` 在交易內重算來源。來源變動使舊命令 `failed/PREVIEW_STALE`，不能沿原預覽建立第二筆替代付款。
- **競爭證據：**`TestAdminCompetingPaymentPreviewsKeepSingleReplacementOperation` 先受理兩個不同金額的 C07，再執行 7000 勝出與 6000 失效。SQLite 只有原 10000 操作（已取消）及新 7000 操作，舊 outbox 完成、勝出命令有一筆收據、失效命令沒有成功收據；兩個原鍵各自重播原結果，改 payload 均衝突，provider 零 capture。
- **畫面證據：**`competing payment previews collect only the winning replacement amount` 在雙分頁建立 1000／1500 預覽，1500 命令勝出，舊頁收到 HTTP 409 與預覽失效提示。C09 隨後沿替代操作的 provider key 收取固定 USD 15.00，只有一筆 capture、一筆 allocation 與一筆派送收據。已取消原操作沒有 provider capture，C07 也只有一筆成功收據。2026-10-03 定向驗證通過。
- **跨實例證據：**`TestAdminCompetingPaymentCommandsAcrossLabInstances` 以兩個獨立 Lab 實例、各自的 SQLite 連線及同一組資料庫檔同時執行已受理的 C07；修正 lease 與命令交易的先讀後寫鎖衝突後，連跑 10 次均為一成功、一 `PREVIEW_STALE`，僅一筆替代操作與收據。修正前測試重現 `database is locked` 且命令留在 `accepted`；定向 `-race` 通過。
- **多進程證據：**`TestAdminCompetingPaymentCommandsAcrossProcesses` 啟動兩個獨立測試子進程，各自開啟相同 SQLite 檔並等待共同起跑訊號；連跑 10 次均為一成功、一 `PREVIEW_STALE`，僅一筆替代操作與成功收據，原操作取消且 provider 零 capture。
歷史 checkpoint（已由上方最終本機驗收取代）：- **限制：**C07 欄位／範圍與 UI 錯誤證據仍待逐項核對；競爭預覽後的收款路徑已補驗，C07 尚未完整簽核。

## C08 確定失敗付款的競爭預覽

- **入口與來源：**`/payments/:id/retry` 透過共用預覽及命令 API 提交 `C08`，能力為 `finance.adjust`。預覽只接受確定失敗的原操作，依當前未清餘額建立新義務；`admin_payment_test.go` 已驗正常重試和取代未送出操作後的重試。
- **競爭證據：**`TestAdminCompetingRetryPreviewsKeepSingleNewObligation` 對同一確定失敗操作受理兩個 C08 預覽。先執行命令建立一筆 10000 重試義務及成功收據，後執行命令變為 `failed/PREVIEW_STALE` 且無成功收據；兩個鍵都重播原結果。SQLite 只有原失敗操作與一筆新操作、單筆 retry request；新 provider key 未被派送，也沒有成功 capture。
歷史 checkpoint（已由上方最終本機驗收取代）：- **跨實例證據：**`TestAdminCompetingRetryCommandsAcrossLabInstances` 以兩個獨立 Lab 實例共用 SQLite 檔同時執行 C08；連跑 10 次均只建立一筆 retry request、新義務與成功收據，另一命令 `PREVIEW_STALE`。新 provider key 尚未派送。
歷史 checkpoint（已由上方最終本機驗收取代）：- **多進程證據：**`TestAdminCompetingRetryCommandsAcrossProcesses` 以兩個獨立測試子進程及共同起跑訊號同時執行已受理 C08；連跑 10 次均只保留一筆 retry request、一筆新義務與成功收據，敗方 `PREVIEW_STALE`，新 provider key 尚未派送。
- **畫面證據：**`competing retry previews leave one obligation and dispatch its exact amount` 在雙分頁對同一確定失敗付款建立預覽，一筆 C08 成功，另一筆回 HTTP 409 `PREVIEW_STALE`。只有兩筆付款操作、一筆 retry request 與一筆 C08 收據。C09 收取固定 USD 20.00，只有一筆成功 capture、一筆 allocation 與一筆派送收據，原付款仍為確定失敗。2026-10-03 定向驗證通過。
歷史 checkpoint（已由上方最終本機驗收取代）：- **限制：**雙分頁競爭與 C09 收款路徑已補驗；其餘 C08 輸入／錯誤與權限證據仍待逐項核對，C08 尚未完整簽核。

## C09 指定付款操作派送的端到端追蹤

| 驗收欄位 | 目前可重跑的證據 |
| --- | --- |
| Action／UI route | C09；`/payments/:id/dispatch`，只對畫面所列的 `operation_id` 建立預覽與提交命令。 |
| HTTP handler／授權 | `api/admin/session.go` 的 `protectedWrite` 連到 `api/admin/previews.go` 的 `createPreview` 與 `api/admin/commands.go` 的 `submitCommand`；`api/admin/permissions.go` 要求 `finance.adjust`。`api/admin/action_http_matrix_test.go` 逐動作驗 session、CSRF、缺能力及未知 payload 拒絕。 |
| 預覽／來源守衛 | `lab/admin_external.go` 的 `loadExternalDispatchSnapshot` 只接受 `created`、pending capture outbox、正金額及合法帳期；快照綁定 operation、invoice、provider key、金額、幣別、狀態和帳期。`adminExternalPreviewCurrent` 在首次派送前重驗；被 C07 取消的舊操作會使 C09 `failed/PREVIEW_STALE`，不留下等待查證命令。 |
| Domain helper／外部效果 | `adminExecuteExternalCommand` 固定傳入目標操作給 `dispatchCaptureAt`；`adminMarkSubmitted` 在服務商呼叫前持久化 submitted。結果未知時保留 `waiting_verification`，沿原 provider key 查證。服務商觀察由 `applyObservationAt` 交易入庫，與 C07 競爭時先取得 SQLite 寫入權。 |
| 收據／恢復 | 只有終態由 `adminFinishExternalCommand` 寫入唯一 `admin_command_receipts` 並標記 succeeded；失效命令無成功收據。既有 `admin_external_test.go`、`admin_lost_response_parity_test.go` 與 `admin_crash_webhook_parity_test.go` 驗查證與中斷恢復；`admin_payment_dispatch_interleaving_test.go` 驗雙實例建立／派送競爭、金額、幣別、provider capture 和收據。 |
| Browser／SQLite | `web/admin/e2e/admin.spec.ts` 新增雙分頁舊 C09 被 C07 取代後收到 HTTP 409、失效提示、舊 key 零 capture，僅新操作收 1500；另一案例在兩筆 pending outbox 中先派送較晚的指定操作，較早者仍 `created` 且零 capture。兩個新案例各自定向 Playwright 1／1，加入後完整套件 92／92 通過。 |
| 當時未簽核 | 已納入完整瀏覽器回歸 92／92；A30 其他動作仍需同粒度證據矩陣。 |

## C10 原付款操作查證的端到端追蹤

| 驗收欄位 | 目前可重跑的證據 |
| --- | --- |
| Action／UI route | C10；`/payments/:id/reconcile`。目標是原 `operation_id`；payload 為 `{}`，依 `lab/admin_commands.go` 不需要預覽。 |
| HTTP handler／授權 | 共用 `api/admin/commands.go` 的 `submitCommand`、`GET /admin/api/commands/{id}` 與 `POST /admin/api/commands/{id}/resume`；`api/admin/permissions.go` 要求 `finance.adjust`。`api/admin/action_http_matrix_test.go` 對 C10 驗 session、CSRF、能力及未知欄位。 |
| 來源與結果守衛 | `lab/admin_external.go` 對 C10 呼叫 `ReconcilePayment(ctx, targetID)`；`lab/lab.go` 用原 provider key 做 `Lookup`，找到服務商事實時才交由 `applyObservation` 入庫。沒有證據或非終態時命令維持 `waiting_verification`，不得推斷收款成功；不存在的操作回 `DOMAIN_REJECTED`。 |
| 收據／冪等恢復 | 終態由 `adminFinishExternalCommand` 產生單一成功收據。`lab/admin_reconcile_no_evidence_test.go` 驗無 provider 事實時沒有成功收據；`lab/admin_external_test.go` 驗取得證據後同一命令完成。C10 不新增 capture，重試依原 operation/provider key 查證。 |
| Browser／SQLite | `web/admin/e2e/admin.spec.ts` 的 `reconcile keeps submitted payment waiting until provider evidence appears` 先驗 `waiting_verification` 且零成功收據，補入 provider 事實後按「重新查證」，原命令變 succeeded、只有一筆收據及原 key 一筆 capture。另有回應遺失後從命令頁找回原操作的瀏覽器案例。 |
| 當時未簽核 | A30 仍需其餘動作同粒度的 HTTP、來源守衛及瀏覽器證據。 |

## C11 帳單減額的端到端追蹤

| 驗收欄位 | 已核對的路徑與證據 |
| --- | --- |
| Endpoint、UI、handler | `POST /admin/api/previews` 與 `POST /admin/api/commands` 使用 `action_id=C11`；`GET /admin/api/commands/{id}` 找回原命令。`web/admin/src/App.tsx` 的 `/invoices/:id/reductions/new` 使用 `ActionForm` 輸入減額與理由；`api/admin/session.go`、`api/admin/commands.go` 接入管理命令。 |
| 分派、領域 helper | `lab/admin_previews.go` 分派 C11 到 `adminCreateReductionPreview`；`lab/admin_commands.go` 分派 C11 到 `executePostReductionTx`，再於同一 SQLite 交易內呼叫 `postReductionTx`。結果包含更正 ID、減額前後義務及實際 grant IDs。 |
| 來源守衛、預覽 policy | R 類預覽綁定 actor、動作、帳單、payload、期限及單次 claim。`loadReductionSnapshot` 拒絕已送出／未知付款、無效金額、減至零及已抵扣 Credit 的帳單；來源 JSON 綁定原義務、已收款、已抵扣、付款操作與 allocation 狀態。執行交易內重算與比對，還核對實際減額前後義務與預覽一致，變動回 `PREVIEW_STALE`。 |
| Permission | `api/admin/permissions.go` 映射 `finance.adjust`；共用 HTTP 與權限矩陣驗無 session、缺 CSRF、缺能力及未知欄位的入場守衛。 |
| 收據、恢復 | 更正、可能產生的 funded grant、舊未送出收款取消與成功收據在同一命令交易；同鍵重播返回原更正與收據。預覽失效須重新顯示來源及金額差異、再次確認，不能自動套用舊金額。 |
| 實測 | `lab/admin_reduction_test.go` 驗已收款減額的精確 grant／收據／重播，以及未收款減額取消舊收款、只收剩餘額；`lab/admin_financial_parity_test.go` 與直接領域路徑比對部分付款及已收款減額；`web/admin/e2e/admin.spec.ts` 驗雙分頁更正使舊預覽 409、顯示差異、重新確認後沒有重複更正，並追查來源歷史。新增 C11 受控 422 案例驗表單與焦點恢復、錯誤關聯且零命令；另一案例在伺服器提交後切斷回應，重整後沿原鍵找回唯一更正、funded grant 與收據。 |

## C12 抵扣 Credit 的端到端追蹤

| 驗收欄位 | 已核對的路徑與證據 |
| --- | --- |
| Endpoint、UI、handler | `POST /admin/api/previews` 與 `POST /admin/api/commands` 使用 `action_id=C12`；`GET /admin/api/commands/{id}` 找回原命令。`web/admin/src/App.tsx` 的 `/credits/:id/apply` 使用 `ActionForm` 輸入目標帳單 ID 與正整數金額；`api/admin/session.go`、`api/admin/commands.go` 接入管理命令。 |
| 分派、領域 helper | `lab/admin_previews.go` 分派 C12 到 `adminCreateApplyCreditPreview`；`lab/admin_commands.go` 分派 C12 到 `executeApplyCreditTx`，再於同一 SQLite 交易內呼叫 `applyCreditTx`。成功寫入 `credit_applications`、取消目標帳單尚未送出的舊收款操作及 outbox，剩餘應付需另建立正確金額的付款。 |
| 來源守衛、預覽 policy | R 類預覽綁定 actor、動作、grant、payload、期限及單次 claim。`loadApplyCreditSnapshot` 拒絕不同客戶、原來源帳單、非續期帳期、超過寬限、已送出／未知付款、貨幣不符或超額抵扣；來源 JSON 綁定 grant 各餘額、目標帳單未清額、帳期／到期、既有付款操作狀態及貨幣。執行交易內重算並比對來源，變動回 `PREVIEW_STALE`。 |
| Permission | `api/admin/permissions.go` 映射 `finance.adjust`；共用 HTTP 與權限矩陣驗無 session、缺 CSRF、缺能力及未知欄位的入場守衛。 |
| 收據、恢復 | 抵扣、舊付款取消與成功收據在同一命令交易；失效命令無抵扣及成功收據，原鍵重播保持 `failed/PREVIEW_STALE`。UI 重新顯示來源／影響差異並要求新預覽與再次確認；不存在自動建立剩餘付款的隱含動作。 |
| 實測 | `lab/admin_apply_credit_test.go` 驗成功收據、來源變動、原鍵重播及只收剩餘額；`lab/admin_refund_budget_parity_test.go` 驗 C12 與 C15 共用 funded grant 預算；`web/admin/e2e/admin.spec.ts` 驗雙分頁退款先占額使舊抵扣預覽 409、重新確認後僅抵扣一次，及舊 2000 收款被取消、僅新 1500 收款送往 provider。 |

## C15 預留退款的端到端追蹤

| 驗收欄位 | 已核對的路徑與證據 |
| --- | --- |
| Endpoint、UI、handler | `POST /admin/api/previews` 與 `POST /admin/api/commands` 使用 `action_id=C15`；`GET /admin/api/commands/{id}` 找回原命令。`web/admin/src/App.tsx` 的 `/credits/:id/refunds/new` 使用 `ActionForm`，要求正整數最小貨幣單位、預覽與二次確認；`api/admin/session.go`、`api/admin/commands.go` 進入 `lab.AdminSubmitCommand`。 |
| 分派、領域 helper | `lab/admin_previews.go` 分派 C15 預覽到 `lab/admin_reserve_refund.go`；`lab/admin_commands.go` 分派 C15 執行到 `executeReserveRefundTx`，再於同一 SQLite 交易內呼叫 `reserveRefundTx`。領域函式拒絕超出 funded grant 可用餘額的預留，並留下退款操作與 audit event。 |
| 來源守衛、預覽 policy | R 類預覽綁定 actor、動作、grant、payload、期限及單次 claim。`loadReserveRefundSnapshot` 記錄來源付款操作／帳單、付款成功狀態、授予／已用／預留／已退／可用額度及貨幣；執行交易內重算並比對完整來源 JSON。即使另一筆 500 預留後還剩 500 可用，舊 500 預覽仍回 `PREVIEW_STALE`。 |
| Permission | `api/admin/permissions.go` 映射 `finance.adjust`；`api/admin/action_http_matrix_test.go` 與 `permissions_test.go` 驗未登入、缺 CSRF、缺能力、未知 payload 欄位及權限映射。這些 HTTP 測試只證明入場守衛。 |
| 收據、恢復 | 成功退款預留與 `admin_command_receipts` 在同一交易提交；預留本身不呼叫 provider，後續送出與查證屬 C16／C17。失效命令保留 `failed/PREVIEW_STALE` 且無成功收據，原冪等鍵重播仍返回原失敗命令；新預覽、新鍵、再次確認才可建立第二筆。`ActionForm` 顯示來源與金額變化並要求再確認。 |
| 實測 | `lab/admin_reserve_refund_test.go` 驗成功收據、來源變動後失效、成功與失敗鍵重播、零額外退款及重新確認後總額；`lab/admin_refund_budget_parity_test.go` 與直接領域路徑比對；`web/admin/e2e/admin.spec.ts` 驗真實 SQLite 雙分頁 409、重建預覽、兩筆退款合計 1000 與兩筆收據。 |

歷史 checkpoint（已由上方最終本機驗收取代）：C15 的預覽競爭與本地交易收據已有直接證據。A30 仍須按相同粒度核對其餘 C01–C49，並完成 A01–A29 剩餘條件；不能由 C15 的結果推論其他動作已簽核。

## C16 指定退款操作派送的端到端追蹤

| 驗收欄位 | 目前可重跑的證據 |
| --- | --- |
| Action／UI route | C16；`/refunds/:id/dispatch`。預覽與命令由共用 API 提交，payload 為 `{}`，目標 refund ID 必填。 |
| HTTP handler／授權 | `api/admin/previews.go` 的 `createPreview` 與 `api/admin/commands.go` 的 `submitCommand` 由 `protectedWrite` 保護；`api/admin/permissions.go` 要求 `finance.adjust`。共用 HTTP 矩陣逐動作驗 session、CSRF、缺能力和未知欄位。 |
| 預覽／來源守衛 | `lab/admin_external.go` 的 `loadExternalDispatchSnapshot` 僅接受 `created`、pending refund outbox 及正金額，綁定 grant、退款 provider key、原收款 provider key、金額、幣別和狀態；`adminExternalPreviewCurrent` 在首次派送前重驗，來源變動時不得沿舊預覽執行。 |
| Domain helper／外部效果 | `adminExecuteExternalCommand` 將固定 refund ID 傳給 `dispatchRefundAt`，在服務商呼叫前以 `adminMarkSubmitted` 持久化 submitted；結果未知時保留 `waiting_verification`。`applyRefundObservationAt` 交易先預留 SQLite writer，再讀取退款／inbox 狀態並提交服務商證據。 |
| 收據／恢復 | 終態由 `adminFinishExternalCommand` 寫入唯一成功收據；待查證不算已完成。`admin_external_test.go` 的 `TestAdminRefundDispatchTargetsExactRefund` 驗指定 ID 而非下一筆佇列項目；退款 UNKNOWN 的 C17 查證及原 C16 恢復另有 `admin_refund_reconcile_test.go` 與瀏覽器案例。 |
| 跨實例金融證據 | `TestAdminRefundReservationAndDispatchCompeteAcrossLabInstances` 以兩個 Lab 共用 SQLite 同時執行 C15 與 C16。修正前重現 C16 `database is locked`；修正後連跑 5 輪共 30 個子案例及定向 `-race -count=2` 共 12 個案例通過。最新退款程式另以瀏覽器 `funded reduction reserves and refunds exactly once after a lost response` 定向 1／1 驗證。每輪原退款僅一筆 USD 400 provider 事實；第二筆預留依來源先後成功或 `PREVIEW_STALE`，funded 額度與收據數一致。 |
| 當時未簽核 | A16 其他退款 UI 錯誤矩陣及 A30 其餘動作仍需逐項證據。 |

## C15 收據寫入故障的 HTTP 恢復

歷史 checkpoint（已由上方最終本機驗收取代）：`api/admin/write_failure_test.go` 的 `TestRefundReservationReceiptFailureRetainsBudgetAndOriginalHTTPCommand` 在 C15 命令受理後對收據 INSERT 注入 SQLite 故障。首次 HTTP 回 503 `COMMAND_PENDING_RETRY` 與原命令 ID；Credit 仍有 1000 可用，退款預留、退款操作、成功收據與 provider 退款皆為零。解除故障後同鍵重送兩次，兩次均返回同一成功命令；Credit 恰預留 500，退款操作維持 `created`、收據一筆、provider 退款仍為零。這與 `lab/admin_reserve_refund_test.go` 的交易回滾測試合併覆蓋 domain 與 HTTP 邊界。

## C17 原退款操作查證的端到端追蹤

| 驗收欄位 | 目前可重跑的證據 |
| --- | --- |
| Action／UI route | C17；`/refunds/:id/reconcile`，目標是原 refund ID。payload 為 `{}`，依 `lab/admin_commands.go` 不需要預覽。 |
| HTTP handler／授權 | 共用 `submitCommand`、命令詳情與原命令 resume 路由；`api/admin/permissions.go` 要求 `finance.adjust`。`api/admin/action_http_matrix_test.go` 驗 C17 的 session、CSRF、能力與未知欄位。 |
| 來源與查證 | `lab/admin_external.go` 呼叫 `ReconcileRefund(ctx, targetID)`；`lab/refunds.go` 以原退款 provider key 查詢服務商，找到匹配的金額、幣別及原收款 key 後才由 `applyRefundObservationAt` 入庫。無終態證據時維持 `waiting_verification`；C17 只查證，不新增第二筆 provider refund。 |
| 收據／恢復 | `adminFinishExternalCommand` 僅在終態產生單一成功收據。`TestAdminReconcileRefundWaitsForTerminalProviderEvidence` 驗無證據時沒有成功收據；`TestAdminRefundReconcileResolvesUnknownWithoutSecondRefund` 驗 UNKNOWN 查證、原鍵重播及唯一 provider 退款。 |
| Browser／SQLite | `web/admin/e2e/admin.spec.ts` 的 `funded reduction reserves and refunds exactly once after a lost response` 從原 C16 `waiting_verification` 進入 C17，確認退款成功後再恢復原 C16；SQLite 核對 provider 只有一筆退款、查證與派送命令各一筆收據。 |
| 當時未簽核 | A30 其他動作仍需同粒度的來源守衛、HTTP 與瀏覽器證據。 |

歷史 checkpoint（已由上方最終本機驗收取代）：`api/admin/provider_lookup_failure_test.go` 的 `TestUnavailableRefundLookupKeepsReservationAndOriginalHTTPCommand` 補真 HTTP C17：服務商資料庫被鎖住時回 503 `COMMAND_PENDING_RETRY` 與原命令 ID；退款仍為 `unknown`，Credit 的 500 預留保持、已退金額為零，沒有成功收據。解除故障後原鍵重送兩次，均回同一成功命令；預留轉為已退 500，Credit 可用額仍為 500，provider 只有一筆退款、命令只有一筆收據。定向測試連跑三次通過。這補足 provider 暫不可查時 HTTP 回應與資金保留的一致性；其他 C17 故障組合仍待逐項驗收。

## A16 Credit 額度查詢與退款 UI 證據

- `GET /admin/api/credits/{id}` 由 session 守衛；`lab.AdminCreditDetail` 在同一個 SQLite 唯讀快照內取得 grant 來源與 `CreditBalance`。`api/admin/credit_detail_test.go` 驗未登入 401 與不存在 404；`lab/admin_credit_detail_test.go` 驗原始、已抵扣、保留、已退款與可用額度的轉換，包含退款回應遺失時仍保留原額。
歷史 checkpoint（已由上方最終本機驗收取代）：- `/admin/credits/:id` 用 Ant Design 將五種額度分開顯示並提供來源帳單、抵扣、預留與按 grant 篩選的退款入口；定向瀏覽器案例驗從列表進入、預留後及成功退款後的金額。另一個案例先建立兩筆 pending 退款，再從 C16 介面指定較晚的操作；較早者仍為 `created`、outbox `pending`、provider 零退款，指定者只退一筆且原收款 key 與金額正確。兩個新案例各自定向 Playwright 1／1 通過。
歷史 checkpoint（已由上方最終本機驗收取代）：- 完整瀏覽器回歸及 A30 其他動作的同粒度盤點仍待完成。

## C11、C13、C14 更正來源驗收補充

帳單詳情從既有 request key 與資料列關聯出更正來源。`admin_reduction_test.go` 驗 C11 更正指向原管理命令；`admin_change_corrections_test.go` 驗 C13 更正指向立即升級變更；`admin_unfulfilled_test.go` 驗 C14 更正指向未履行升級變更。C11、C13、C14 的同 key 重播測試核對更正不重複，C11／C14 的 Credit 各仍一筆，C13 不納入後來合格的項目。Playwright 另驗 C11 原命令入口、C13 延遲更正的變更 ID 與金額，以及 C14 跨帳期後查證、決議、帳單更正與來源 Credit 的詳情及歷史頁。無法驗證關聯的更正標為其他領域操作並保留原 request key，不推定為管理命令。

`TestAdminReductionCancelsOldCollectionAndCapturesOnlyRemainder` 與新 Playwright 案例另驗未收款的第一期帳單：C11 預覽列出將取消的舊付款操作與減額後應付 1500；減額 500 後，原 2000 的付款與 outbox 取消，未產生可退 Credit。操作員由 C07 建立 1500 新付款、C09 派送後，fake provider 只有新 key 的 1500 capture，原 key 沒有收款，訂閱開通且帳單餘額歸零。C11／C12 的收款安排提示共用同一介面元件；兩項瀏覽器案例已分別定向通過。

## C13、C14、C30、C32、C44、C45 回應恢復

批次回應遺失的可重跑證據位於 `web/admin/e2e/admin.spec.ts`：C13、C30、C32、C44、C45 都先讓原命令在 SQLite 完成，再中斷瀏覽器回應，重新載入後沿原 request key 重播。測試逐項核對工作、成員及各自的財務結果；C13 檢查帳單更正，C30 檢查 Credit Note，C32 檢查 capture outbox，C44 檢查續約帳單與付款義務，C45 檢查固定的權益刷新成員。這只簽核該種回應遺失與重播情境。

C14 另有同類型的非批次瀏覽器案例：未履行升級決議建立更正與 Credit 後中斷回應，頁面重新載入並沿原 key 查回，命令、收據、決議與 Credit 各只有一筆。此案例定向通過，未納入先前 63 項完整回歸。

Go 故障注入另覆蓋兩種不同的收據失敗邊界：`TestAdminResolveUnfulfilledRollsBackFinancialFactsWhenReceiptFails` 驗 C14 的財務資料與收據整體回滾，重啟後才一次完成；`TestAdminChangeCorrectionBatchResumesCommittedItemAfterReceiptFailure` 驗 C13 已提交的逐項更正在最終收據失敗時保留，重啟後只完成原工作與收據。兩者都核對 Credit／更正不重複，provider capture 數不增加。

## C12 瀏覽器驗收補充

歷史 checkpoint（已由上方最終本機驗收取代）：`web/admin/e2e/admin.spec.ts` 的 C12 案例以已收款帳單減額產生 1000 的 Credit，設定實驗時鐘並由續期批次建立同客戶的下一帳期帳單。操作員從抵扣頁輸入目標帳單與 500 金額，檢查預覽後確認，重新載入命令詳情仍可查到成功結果。隔離 SQLite 核對抵扣來源、目標帳單、`admin:` request key、唯一抵扣紀錄及一筆命令收據；帳單詳情顯示已抵扣 500 與剩餘應付。超過可用 1000 額度的預覽被拒絕，且未建立 C12 命令。`lab/admin_apply_credit_test.go` 另驗預覽後退款預留改變額度來源時，C12 命令以 `PREVIEW_STALE` 失敗，不新增抵扣或收據。不同客戶、過期帳期與並發爭用的瀏覽器案例仍待驗。

同檔的 `TestAdminApplyCreditCancelsOldCollectionAndCapturesOnlyRemainder` 延伸驗證抵扣後的收款：原續期帳單為 2000，抵扣 500 時舊 `created` 付款操作與 capture outbox 一同取消，舊 provider key 沒有 capture；再由 C07 建立 1500 的新付款操作，只向 fake provider capture 1500，帳單餘額歸零。這證明該路徑不會把抵扣前的 2000 送去收款，但不代表其他並行情境已全部驗畢。

歷史 checkpoint（已由上方最終本機驗收取代）：同一條路徑現也由 Playwright 驗證：C12 預覽明示取消一筆未送出的付款操作、抵扣後應付金額及建立新付款的提示；操作後沿 UI 進入 C07 建立剩餘付款，再用 C09 派送。SQLite 與 fake provider 分別核對舊操作已取消、舊 provider key 沒有 capture、新 key 只 capture 剩餘 1500，帳單詳情顯示 USD 0.00 尚待支付。此路徑已納入最近一次 70 項完整瀏覽器回歸。

C12 的瀏覽器案例進一步在預覽後，於同一登入 session 的另一分頁執行 C15 預留退款 500。舊 C12 預覽確認回 409 `PREVIEW_STALE`，沒有抵扣或命令收據；介面顯示 `grant_reserved_minor` 來源變動，保留原 500 意圖並要求再次確認。新預覽確認後，C15 預留 500 加 C12 抵扣 500 正好等於原 Credit 1000，兩筆用途沒有超額或重複。此情境定向 1／1 通過，並已納入最近一次 76／76 完整瀏覽器回歸。

`TestAdminApplyCreditUsesGrantAndInvoiceBalanceAtomically` 另核對原 C12 失敗命令沿同一 request key 重播仍是同一筆 `PREVIEW_STALE`，且不會寫入新的抵扣或收據。

## C45 批次成員瀏覽器驗收補充

歷史 checkpoint（已由上方最終本機驗收取代）：`web/admin/e2e/admin.spec.ts` 的 C45 案例先建立已付款訂閱，透過 UI 建立權益刷新預覽並讀取其固定成員快照，再於另一個分頁建立新的合格訂閱。確認原預覽後，SQLite 工作項目數與快照相同，包含原訂閱而不包含新訂閱；工作詳情頁在重新整理後仍顯示完整進度。另一個頁面案例以受控 API 回應顯示一筆成功及一筆待查證，確認進度為 1/2、兩種狀態分列，且逐項錯誤碼可見。此案例只驗 C45 的成員固定性及批次頁面的狀態呈現，尚未覆蓋五種批次的逐項中斷與重啟 UI 矩陣。

## C34 瀏覽器驗收補充

歷史 checkpoint（已由上方最終本機驗收取代）：`web/admin/e2e/admin.spec.ts` 的缺失權益投影案例，使用本機假服務商與隔離 SQLite 建立已付款訂閱，故意移除權益投影，再走 C33 對帳及 C34 詳情頁、預覽、確認、重新整理。測試核對 `SAFE_AUTO_REPAIR`、來源 revision 與證據預填、唯一 `repair_operations` 與命令收據、原 expected／actual 快照，以及修復後 `verified` 與後續對帳的 verification。另有來源 revision 在預覽後改變的 browser 案例：修復結果為 `blocked`、權益未重建、命令只有一筆收據，UI 明示 blocked verification。服務商收款金額不符的 browser 案例另驗 `MANUAL_REVIEW`：C34 被 blocked、C35 留下人工決議，provider 原金額與收款總筆數不變。其他分類及 blocked 原因仍待逐項 browser 驗收。

## C46–C49 實驗控制的現有證據

| 動作 | 已驗證的行為 | 尚需補齊 |
| --- | --- | --- |
| C46 | `TestAdminClockPersistsAndControlsRunningDomainClock` 驗時鐘設定、重啟載入、revision 使舊預覽失效；瀏覽器案例驗 C09 等待查證期間前進一天不會自動重收款，原命令仍使用受理時的業務時間。 | 與其他進行中命令交錯、各種時間邊界及錯誤呈現的逐項矩陣。 |
| C47 | `TestAdminPaymentDecisionRecoversAfterProviderCommitAndCapture` 驗 provider 決策先提交、付款後續完成、重啟沿原控制收據完成管理命令，capture 與收據各唯一；`TestAdminConflictingProviderDecisionsPreserveFirstOutcome` 驗相反結果命令失敗、第一個決策仍造成唯一成功收款、終態後新命令亦失敗；瀏覽器顯示第二個決策 `DOMAIN_REJECTED`、僅首命令有收據，並走過確定失敗後重試。 | 權限撤銷交錯與其餘 UI 錯誤呈現矩陣。 |
| C48 | `TestAdminRefundDecisionRecoversAfterProviderCommitAndDispatch` 驗 provider 決策先提交、退款後續派送、重啟沿原控制收據完成管理命令；`TestAdminConflictingProviderDecisionsPreserveFirstOutcome` 驗相反結果命令失敗、第一個決策仍造成唯一成功退款、終態後新命令亦失敗；瀏覽器顯示第二個決策 `DOMAIN_REJECTED`、僅首命令有收據且派送前無 provider 退款，另驗回應遺失後 C17 查證與唯一退款。 | 權限撤銷交錯與其餘 UI 錯誤呈現矩陣。 |
| C49 | `TestAdminFaultTicketOnlyAffectsItsSelectedPayment` 驗指定付款票據不被另一筆付款誤領；`TestAdminDuplicateFaultTicketFailsWithoutStrandingCommand` 驗第二張票據明確失敗、原鍵重播與第一張票據仍可派送；`TestAdminClaimedFaultSurvivesCrashBeforeProviderDispatch` 在付款及退款各固定領票後、provider 呼叫前的持久化狀態，重啟後原命令仍進入待查證，查證後 provider 事實與命令收據各唯一。`TestAdminRevokedDispatchReleasesClaimedFaultBeforeProviderCall` 與 `TestAdminRevokedRefundDispatchRetainsReservationAndFault` 驗領票後、provider 呼叫前撤銷原命令，不產生外部效果或成功收據；票據釋放後由新命令領取，退款預留額保持 500。`TestAdminClaimedFaultBlocksCompetingDispatchUntilOwnerResumes` 驗同一付款的第二個命令不得繞過已綁定票據直接收款，啟動恢復先略過競爭命令、讓原命令查證，兩命令最後共用唯一 capture；`TestAdminStalePreviewReleasesClaimedFault` 驗舊來源快照失效後釋票、零收款與新命令可再領票。瀏覽器從建票頁顯示第二張失敗與 `DOMAIN_REJECTED`，確認第一張仍可派送，另驗付款及退款的 `lost_response`／`crash_after_provider` 恢復。 | 真正跨程序同時接管及不同故障模式的完整組合。 |

歷史 checkpoint（已由上方最終本機驗收取代）：這些案例是 C46–C49 的定向證據；A25 與 A30 仍需完成上列缺口和共通矩陣，不能視為四個動作已全面簽核。

## 證據範圍與限制

上方逐動作完整紀錄取代先前泛稱的額外矩陣缺口。每個動作均有真實請求與成功收據配對、原鍵重播、不同 payload 衝突、共用 HTTP 入場證據，以及適用 helper／來源／收據契約。未使用的金融欄位與 N 類預覽不適用；單一 local-admin 的跨 actor 瀏覽器操作不適用，領域隔離另有實證。

證據限於列出的 domain fixture、共用機制與本機版本。UI 受控回應證明呈現／恢復，provider 與金融效果由連結的真實 fixture 證明；完整套件與後加補充斷言的指紋分開。首次 process 就緒逾時根因未確認，診斷重複與最終完整 Go 回歸通過。不推論任意故障次序或正式 provider 行為。

## request ID 稽核關聯補驗（2026-09-28）

`web/admin/e2e/admin.spec.ts` 真登入案例先核對未登入 401 JSON 的 `request_id` 等於 `X-Request-ID`，且不採用客戶端偽造的 header；再建立 C01 報價，核對回應 header、`admin_commands.request_id`、受理與成功兩筆 `admin_audit.request_id` 及命令詳情頁顯示的值一致。原有秘密排除檢查仍覆蓋稽核文字中的密碼、內部 token、CSRF 與 session cookie。v8 升級保留舊列的 `NULL` 請求 ID，避免為歷史請求虛構來源。

`api/admin/route_errors_test.go` 補真 HTTP 路由邊界：未知 API 路徑在登入前先回 401，登入後回 JSON 404；已知路徑的不支援方法回 JSON 405 與 `Allow`。每筆錯誤 body 的 `request_id` 等於 header，且新請求有不同 ID。這避免純文字路由器預設錯誤繞過管理 API 的 session 與錯誤契約。

## C01 寫入故障與回應遺失補驗（2026-09-28）

`api/admin/write_failure_test.go` 先對命令 INSERT 注入故障，驗證入場失敗回 500 `COMMAND_ADMISSION_UNKNOWN`（`retryable: true`，須沿用原鍵）、沒有命令／報價／收據。解除故障後原鍵首次提交回 202，再重播回 200，且只有一筆命令、報價和收據。

歷史 checkpoint（已由上方最終本機驗收取代）：`api/admin/write_failure_test.go` 對收據 INSERT 注入持久 SQLite 故障，驗證命令已受理但財務事實回滾時回 503 `COMMAND_PENDING_RETRY`，並提供原命令 ID。解除故障後，同鍵重播兩次只建立一筆報價與一筆成功收據。

歷史 checkpoint（已由上方最終本機驗收取代）：`api/admin/client_disconnect_test.go` 使用真 HTTP 連線在命令成功後丟棄首個回應。重送前已存在成功命令、報價與收據；同鍵重送兩次後仍各一筆，且返回同一命令。另一案例等待 C01 命令受理與執行 lease 取得，在報價寫入受 SQL trigger 延遲的測試環境中取消客戶端請求；伺服器結束後原命令保留為 `accepted`、lease 釋放、報價及收據為零。同鍵重送兩次後僅建立一筆報價及收據。後一案例連續執行 20 次通過；其他動作的執行中斷線與 DB 故障組合仍待驗。

`web/admin/e2e/admin.spec.ts` 另以受控 500 `COMMAND_ADMISSION_UNKNOWN` 驗證 C01 首次入場不確定時，重新載入後仍保留原 payload 與冪等鍵；再次送出才建立一筆成功命令、一筆報價與一筆收據。這補足了受理前 DB 失敗到 UI 恢復的交界，但沒有把受控 HTTP 錯誤當成真實 SQLite 故障的瀏覽器注入。

## C18–C21 發布鏈的成功收據補驗（2026-09-28）

歷史 checkpoint（已由上方最終本機驗收取代）：`web/admin/e2e/admin.spec.ts` 的「catalog publishes immutable price and meter versions before cohort selection」案例已在真實瀏覽器逐一執行 C18（Pro 價格）、C19（計量表）、C20（兩個獨立計量價格版本）與 C21（cohort 選價）。每一個來源 ID／方案各以 SQLite 關聯 `admin_commands` 與 `admin_command_receipts` 核對唯一成功收據；同版號與已選價衝突的額外預覽仍在入場前被拒絕，沒有增加命令。定向 Playwright 1／1 通過；這補成功鏈的收據證據，不取代其他輸入、重播及恢復矩陣。


## C02 來源讀取失敗時的原鍵恢復（2026-09-28）

`web/admin/e2e/admin.spec.ts` 的真實 C02 案例先接受報價並讓成功 POST 回應遺失，再令報價詳情 GET 回 503；頁面重整後仍可按原 idempotency key 查回同一命令，命令頁顯示成功。SQLite 直接核對 C02 命令、訂閱與帳單各一筆。其餘 C03／C04／C05／C06／C23 的來源故障入口與鍵／動作接線由 `web/admin/e2e/source-read-recovery.spec.ts` 受控介面案例驗證；C03 首次查詢回 503 後再次使用同一鍵。該案例不作財務收據宣稱。


## C09／C16 命令完成與外部操作失敗的 UI 區分（2026-09-28）

`adminFinishExternalCommand` 在服務商提供確定失敗的終態證據後，可將派送命令記為 `succeeded`，同時將 `operation_status=definitively_failed` 寫入結果參照。兩者分屬命令流程與外部金流結果。React 操作結果及命令各入口現額外標示付款或退款確定失敗。真實 C16 瀏覽器案例核對 Credit 保留額釋放、可用額恢復，以及操作結果、命令詳情、列表、抽屜的警告；真實 C09 案例核對付款操作失敗警告。

## C02 收據故障後沿原 HTTP 命令恢復（2026-09-28）

歷史 checkpoint（已由上方最終本機驗收取代）：`api/admin/quote_acceptance_receipt_failure_test.go` 在隔離 SQLite 以真實管理 HTTP handler 提交 C02。收據 INSERT 故障時回 503 `COMMAND_PENDING_RETRY` 與原命令 ID，訂閱、帳單、帳期、付款義務、outbox 和成功收據均未提交。解除故障後同鍵重送兩次，兩次都取得同一成功命令；上述財務事實與收據各一筆，金額和來源 ID 一致，provider capture 仍為零。同鍵提交另一張有效報價回 409 `IDEMPOTENCY_CONFLICT`，第二張報價沒有建立訂閱。這補足原有領域層收據回滾測試的 HTTP 回應、冪等恢復與鍵衝突證據，不取代其他 C02 故障點矩陣。

## C12 收據故障與提交後回應遺失（2026-09-28）

`api/admin/credit_apply_receipt_failure_test.go` 以來源已付款帳單的 funded Credit 抵扣同戶下期帳單。收據 INSERT 失敗時，HTTP 回 503 與原命令 ID，抵扣和舊付款取消均回滾；同鍵恢復後只抵扣 500 一次，舊付款取消且 outbox 設為 `done`，餘額須另建立新付款操作。另一筆 250 抵扣在成功提交後遺失首個 HTTP 回應；重送兩次都回同一成功命令，兩筆抵扣合計 750，Credit 可用 250，目標未清 1250，收據與 provider capture 沒有重複。這補足 C12 的 HTTP 回應／收據邊界證據，不代替其餘動作的故障矩陣。

## C11 已收款減額的 HTTP 收據故障（2026-09-28）

`api/admin/reduction_receipt_failure_test.go` 在隔離 SQLite 對已實收帳單提交 C11，注入收據 INSERT 故障時 HTTP 回 503 與原命令 ID，減額、funded Credit、原收款釋出和成功收據全部回滾；原帳單與 provider capture 保持不變。同鍵恢復後只有一筆 1000 correction、grant、release 與收據，淨實收等於修正後義務。另一筆有效減額若沿用相同鍵則回 409，沒有第二次財務效果。既有瀏覽器案例另驗提交後回應遺失的 C11 原鍵恢復。

## 專用操作頁命令指標恢復（2026-09-28）

歷史 checkpoint（已由上方最終本機驗收取代）：C01、C02、C03／C04、C05／C06、C07、C08、C23、C26 的專用頁改以 actor、動作及目標為鍵保存最近命令 ID；共用 `ActionForm` 亦使用同一機制。這讓重新載入頁面後可沿原命令 ID 查詢狀態，不以本地狀態宣稱財務操作成功。C01 與 C05 的 Playwright 情境直接驗重整後仍可讀原命令；C05 的下一步需明確清除本頁指標，重新讀取訂閱後才進入 C06。其餘專用頁目前是程式接線與建置證據，尚不能由 C01／C05 案例推論所有動作的回應遺失及 404／403 讀取矩陣已簽核。

歷史 checkpoint（已由上方最終本機驗收取代）：2026-09-28 完整回歸：Playwright 109／109 通過，React 建置及 Go 全套測試通過。此結果驗證現有測試涵蓋的路徑；逐動作尚列「部分」的權限、故障與可觀測性矩陣仍待逐項簽核。
