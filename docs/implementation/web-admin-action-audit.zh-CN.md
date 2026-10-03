# Web Admin C01–C49 证据盘点

[English](web-admin-action-audit.md) | [繁體中文](web-admin-action-audit.zh-TW.md) | **简体中文**

A01–A30 在文档列明的本机范围通过（30／30），T01–T22 全完成。最终独立复查没有阻挡或重要问题；49 项动作记录连接 UI 路径、handler、domain helper、来源守卫、能力、预览政策、收据／恢复政策、HTTP 测试、浏览器情境与执行证据。

## 最终本机验收（2026-10-03）

A01–A30 在文档列明的本机范围通过（30／30），T01–T22 全完成。最终独立复查没有阻挡或重要问题；49 项动作记录连接 UI 路径、handler、domain helper、来源守卫、能力、预览政策、收据／恢复政策、HTTP 测试、浏览器情境与执行证据。

最新完整 Go 回归：355 个顶层测试／1115 个含子案例通过事件，零失败。浏览器快照：156／156；后加耗尽 Credit、A13 与 A17 断言各以 2／2 通过，与完整快照分开记录。

首次 30 秒 process 就绪超时根因未确认。单独 1 次、诊断重复 20 次及最终完整 Go 回归通过；test-only worker 诊断不证明已修复。结果限于本机 Go／SQLite／fake provider 与列明的 fixture。

下方较早的逐案例记录保留当时的结果与待核项目；目前判定以上方最终验收与逐动作完整证据记录为准。

## 逐动作完整证据记录（2026-10-03）

本表依 action ID 连接既有 UI 路径、能力、预览政策及交易测试表，合并填齐验收计划要求的 11 个字段。适用的 payload／来源／金额变体由链接的 domain fixture 与 A01–A29 证据核对；未使用的金融字段及 N 类预览规则标为不适用。这是限定本机验收，不推论任意变体。

- **H**：`POST /admin/api/commands` → `api/admin/commands.go::submitCommand`；R 类先走 `api/admin/previews.go::createPreview`。共用 session／CSRF 与 `authorizeActionIntent` 核能力；最新 Go 已通过 `TestEveryActionHTTPAdmissionGuards`、`TestEveryActionRejectsUnknownPayloadBeforeAdmission`、`TestEveryActionRequiresItsCapability`。每行浏览器案例提供真实合法请求与配对收据。**Hc** 另含 C01／C02 合约能力／来源分支与 `TestContractQuoteAcceptRequiresContractCapability`。
- **G**：先查原 actor／key，再验 canonical intent 与 actor／action／target；41 个 R 动作另核 payload／TTL／clock revision／claim。8 个 N 动作只有 C01／C10／C17／C23／C26／C33／C37／C38。单一 `local-admin` 的跨 actor 浏览器操作不适用；C02／C07 的 Go 隔离与共用守卫提供证据，A08 另有到期预览原键重播。
- **L**：37 个本地动作的领域事实、命令收据／状态与 audit 同交易，带 lease fencing；恢复先核原收据。**E**：C09／C10／C16／C17 保留并查证指定 provider 操作。**B**：C13／C30／C32／C44／C45 固定成员，每项事实与状态／result 收据同 commit，再恢复 summary。**Q**：C34 以稳定修复键重核证据／revision／provider。**P**：C47／C48 的 decision 与 control receipt 在 provider SQLite 同交易，再恢复 commerce 收据，不宣称两库同一交易。A06／A07／A15／A16／A18／A23／A25 已证这些机制。
- **X**：revision `049e5a9743a70bf543a9771b9db68c127e27b7cf`、schema 9、source／runtime 指纹列于 `/tmp/billforge-acceptance-manifest-20261003.json`。最新 Go 355 个顶层／1115 个含子案例通过事件、零失败。浏览器快照 156／156 通过（25.4 分钟），105 个稽核案例涵盖 49 动作的配对收据、原键重播与不同 payload 冲突；下列名称对应 `/tmp/billforge-admin-full-final-cases-20261003.jsonl`。具名原始 artifact 可逐项查证，上方命令可重建执行证据。后加耗尽 Credit、A13／A17 断言另以浏览器 2／2＋2／2＋2／2 补证，执行逻辑相同，全套与补充指纹分开。受控响应只证 UI，金融事实由真 Go／API／provider fixture 证明。首次 process 就绪超时仍保留为根因未确认的限制，不宣称已修复。

| 动作 | 领域 helper／adapter | 来源守卫（另加 G） | 收据／恢复 | HTTP | 浏览器情境 | 执行证据 |
| --- | --- | --- | --- | --- | --- | --- |
| C01 | `createQuoteForCohort / createContractQuoteTx + bindChangeQuoteTx` | 互斥来源、客户／席次、revision、原子绑定 | L | Hc | `admin password and internal token stay out of assets responses URLs and logs` | X |
| C02 | `executeAcceptQuoteTx -> acceptQuoteTx / acceptContractQuoteTx` | fingerprint、用途、到期、绑定、writer | L | Hc | `rejected financial action restores an editable form and does not retain its intent` | X |
| C03 | `executeSchedulePlanTx -> scheduleNextPlanTx` | 报价绑定、revision、选定价格 | L | H | `lost scheduled plan response recovers its original command and one schedule` | X |
| C04 | `executeImmediateUpgradeTx -> requestImmediateProUpgradeTx` | 绑定／revision、实际金额不超确认上限 | L | H | `lost immediate upgrade response recovers one obligation without switching service early` | X |
| C05 | `executeScheduleCancelTx` | 订阅 revision、取消状态、writer | L | H | `concurrent cancel and resume keep their original intents after source changes` | X |
| C06 | `executeResumeCancelTx` | 订阅 revision、取消状态、writer | L | H | `concurrent cancel and resume keep their original intents after source changes` | X |
| C07 | `createPaymentTx` | 可收余额、原 queued／failed 操作、来源快照 | L | H | `a lost payment command response recovers one operation across sessions and reload` | X |
| C08 | `retryFailedPaymentTx` | 可收余额、原 queued／failed 操作、来源快照 | L | H | `a retry payment shows a lower balance after another operator reduces the invoice` | X |
| C09 | `adminExternalPreviewCurrent -> adminExecuteExternalCommand (payment)` | 指定操作 ID、status／outbox／amount、故障拥有者 | E | H | `rejected financial action restores an editable form and does not retain its intent` | X |
| C10 | `adminExecuteExternalCommand (payment verification)` | 原操作／provider key；无证据保留义务 | E | H | `rejected financial action restores an editable form and does not retain its intent` | X |
| C11 | `executePostReductionTx -> postReductionTx` | 帐单余额、减额、来源 | L | H | `lost reduction response recovers its original correction and credit` | X |
| C12 | `executeApplyCreditTx -> applyCreditTx` | funded／已用／预留额度、帐单识别／余额 | L | H | `C12 applies funded credit to a later invoice once and keeps the command receipt` | X |
| C13 | `adminRunBatchItem -> runChangeCorrectionTx` | 固定成员 ID、逐项来源 revision 与资格 | B | H | `midperiod upgrade previews USD 40.00 and keeps service on the old price until paid` | X |
| C14 | `executeResolveUnfulfilledTx -> resolveUnfulfilledImmediateChangeTx` | 原变更、履行、期限／来源 | L | H | `unfulfilled upgrade exposes its correction and credit provenance in the invoice` | X |
| C15 | `executeReserveRefundTx -> reserveRefundTx` | grant 预算、capture allocation、来源 | L | H | `provider commit followed by a crash preserves the refund reservation across sessions` | X |
| C16 | `adminExternalPreviewCurrent -> adminExecuteExternalCommand (refund)` | 指定操作 ID、status／outbox／amount、故障拥有者 | E | H | `provider commit followed by a crash preserves the refund reservation across sessions` | X |
| C17 | `adminExecuteExternalCommand (refund verification)` | 原操作／provider key；无证据保留义务 | E | H | `funded reduction reserves and refunds exactly once after a lost response` | X |
| C18 | `executeAdminCatalogTx -> publishProPriceTx` | 完整 spec、ID／version／schema／checksum／dependency／selection | L | H | `a newly selected Pro price rejects the old bound quote before scheduling` | X |
| C19 | `executeAdminCatalogTx -> registerMeterTx` | 完整 spec、ID／version／schema／checksum／dependency／selection | L | H | `meter registration refuses an older conflicting schema preview` | X |
| C20 | `executeAdminCatalogTx -> publishMeteredPriceTx` | 完整 spec、ID／version／schema／checksum／dependency／selection | L | H | `metered price publication refuses an older conflicting version preview` | X |
| C21 | `executeAdminCatalogTx -> selectCatalogPriceTx` | 完整 spec、ID／version／schema／checksum／dependency／selection | L | H | `a newly selected Pro price rejects the old bound quote before scheduling` | X |
| C22 | `executeAdminMigrationTx -> planPriceMigrationTx` | 固定目标、revision／席次／排程、项目／批次状态 | L | H | `price migration pins members and preserves a skipped item through pause and resume` | X |
| C23 | `pausePriceMigrationTx` | 原批次状态 | L | H | `price migration pins members and preserves a skipped item through pause and resume` | X |
| C24 | `executeAdminMigrationTx -> skipPriceMigrationItemTx` | 固定目标、revision／席次／排程、项目／批次状态 | L | H | `price migration pins members and preserves a skipped item through pause and resume` | X |
| C25 | `executeAdminMigrationTx -> resumePriceMigrationTx` | 固定目标、revision／席次／排程、项目／批次状态 | L | H | `price migration pins members and preserves a skipped item through pause and resume` | X |
| C26 | `recordUsageTx` | business key／payload hash、原事件不可改 | L | H | `an uncertain usage response cannot become a second event intent` | X |
| C27 | `executeAdminUsageTx -> recordUsageAdjustmentTx` | 事件来源、帐期／成员、rating version | L | H | `usage keeps the original event, closes at zero and rerates late usage to one cent` | X |
| C28 | `executeAdminUsageTx -> closeUsagePeriodTx` | 事件来源、帐期／成员、rating version | L | H | `usage keeps the original event, closes at zero and rerates late usage to one cent` | X |
| C29 | `executeAdminUsageTx -> rerateUsagePeriodTx` | 事件来源、帐期／成员、rating version | L | H | `usage keeps the original event, closes at zero and rerates late usage to one cent` | X |
| C30 | `adminRunBatchItem -> runUsageCreditNoteTx` | 固定成员 ID、逐项来源 revision 与资格 | B | H | `usage keeps the original event, closes at zero and rerates late usage to one cent` | X |
| C31 | `executeAdminContractTx -> publishContractTx` | 完整 spec、base price／version／checksum／客户 | L | H | `concurrent contract publication keeps the first version and rejects an older preview` | X |
| C32 | `adminRunBatchItem (due contract invoice collection)` | 固定成员 ID、逐项来源 revision 与资格 | B | H | `publishes enterprise contract, quotes seats and accepts Net30 without early capture` | X |
| C33 | `persistReconciliationTx (after collecting observations)` | 逐来源观测时间；不宣称跨库快照 | L | H | `repairs a missing entitlement projection once with a recorded command receipt` | X |
| C34 | `adminExecuteRepairCommand` | expected／actual、revision／provider、稳定修复键 | Q | H | `repairs a missing entitlement projection once with a recorded command receipt` | X |
| C35 | `executeManualDecisionTx` | 目前差异证据／version、理由、不创建金融更正 | L | H | `provider amount mismatch stays manual without changing the captured amount` | X |
| C36 | `executeAccountLinkTx -> linkLegacyAccountTx` | legacy／客户／beneficiary／cohort／history 映射 | L | H | `account migration detail shows readiness thresholds and keeps owners after stop` | X |
| C37 | `executeAdminShadowTx -> shadowQuoteTx` | account／writer／price／subscription 来源与观测时间 | L | H | `account migration detail shows readiness thresholds and keeps owners after stop` | X |
| C38 | `executeAdminShadowTx -> shadowEntitlementTx` | account／writer／price／subscription 来源与观测时间 | L | H | `account migration detail shows readiness thresholds and keeps owners after stop` | X |
| C39 | `executeAdminProvenanceTx -> backfillLegacyProvenanceTx` | 原映射／provider keys、来源版本 | L | H | `historical account provenance review gates read and writer cutover` | X |
| C40 | `executeAdminProvenanceTx -> resolveLegacyProvenanceTx` | 原映射／provider keys、来源版本 | L | H | `historical account provenance review gates read and writer cutover` | X |
| C41 | `executeAdminCutoverTx -> switchAccountReadTx` | 最新 readiness／owners、UNKNOWN／stop、先读后写 | L | H | `historical account provenance review gates read and writer cutover` | X |
| C42 | `executeAdminCutoverTx -> switchAccountWriterTx` | 最新 readiness／owners、UNKNOWN／stop、先读后写 | L | H | `historical account provenance review gates read and writer cutover` | X |
| C43 | `executeAdminCutoverTx -> stopAccountMigrationTx` | 确认时 AccountLink／owners／stopped 状态、stop reason；保留金融史与 UNKNOWN 义务 | L | H | `account migration detail shows readiness thresholds and keeps owners after stop` | X |
| C44 | `adminRunBatchItem -> renewOneTx` | 固定成员 ID、逐项来源 revision 与资格 | B | H | `contract without a follow-on price holds renewal and shows the reason` | X |
| C45 | `adminRunBatchItem -> refreshOneEntitlementTx` | 固定成员 ID、逐项来源 revision 与资格 | B | H | `subscription history pages and entitlement provenance use the real database` | X |
| C46 | `adminControlPreviewCurrentTx -> executeAdminClockTx` | 持久化 clock revision | L | H | `clock changes while a payment awaits verification preserve its accepted business time` | X |
| C47 | `adminExecuteProviderControl -> adminSetDecision (payment)` | 最新 provider decision 快照、provider control receipt | P | H | `a retry payment shows a lower balance after another operator reduces the invoice` | X |
| C48 | `adminExecuteProviderControl -> adminSetDecision (refund)` | 最新 provider decision 快照、provider control receipt | P | H | `funded reduction reserves and refunds exactly once after a lost response` | X |
| C49 | `adminControlPreviewCurrentTx -> executeAdminFaultTx` | 指定操作与故障票据状态 | L | H | `quote and acceptance create one financial obligation under replay` | X |

## 领域、命令与精确值验收（2026-10-03）

最新完整 Go 回归与独立逐条复查支持 A01／A06／A07／A09／A10／A28 通过；A30 最终判定见上方。

| ID | 本机通过 |
| --- | --- |
| A01 | S01–S12／P01–P03 均有 domain／admin fixture、固定金额／状态 oracle 与共用交易 helper 对照。最新完整 Go 回归 355 个顶层测试／1115 个含子案例事件通过、零失败。首次 process 就绪超时在单独 1 次、诊断 20 次及最终全套均未重现；根因未确认，test-only 退出诊断不宣称修复。 |
| A06 | 同键 client 只受理一个命令；注入收据故障回滚本地金融事实，重开与重播恢复唯一效果／收据。事实、收据、命令状态及 audit 共用交易。固定成员 job 的每项效果与项目收据同 commit，最终 summary 可恢复；浏览器响应丢失保留原意图。 |
| A07 | lease 接管隔离旧 generation；撤权令未开始工作 PERMISSION_REVOKED，已提交 provider 义务保留查证。付款／退款 fencing、原 provider receipt 收敛与跨库控制义务保留均有证据。 |
| A09 | 共用预览核对 actor／action／target／payload／claim／到期／clock revision；浏览器来源变动回 409 且不写入，保留意图并显示新旧差异。明确 clock revision 变更须重预览，只有契约允许的同 revision 自然时间下降可接受，估算与实际金额分开验证。 |
| A10 | Go／HTTP／浏览器涵盖零、负值、int64 上限／溢位、超 JS safe integer、小数／指数／JSON number 拒绝、UTC 精度／范围与 rational 分母。有效字符串精确往返，非法输入在命令／事实写入前拒绝；Money 使用 BigInt，金融输入保持十进制字符串。 |
| A11 | 19 类共用资源采用有界 rowid 游标与白名单筛选；真 HTTP 覆盖跨页插入、status／filter scope、catalog、usage、客户 keyset，以及独立 job／provider／command／history 机制。UI 明示非全局快照，缺来源保持 null／未知，第 0 帐期与 epoch 仍是真值；金融敏感 key 需能力，共用 handler 区分空数据／不存在／禁止／故障。 |
| A12 | 58 条已知操作路由逐页有 390px 首个 Tab、label 关联、ARIA 名称与无溢出证据；移动菜单 Enter／Escape 焦点、拒绝确认后焦点／编辑、字段错误关联及各独立读取呈现类型均有浏览器证据。共用状态文字、精确输入、provider 结果与离页停止轮询亦已验；受控读取／拒绝只证 UI，金融事实由真 Go／API／provider fixture 证明。 |
| A28 | 真 HTTP 数据库故障回 QUERY_FAILED，不伪装空数据或不存在。付款／退款 provider 暂不可查时保留原可重试命令与 unknown 义务，没有成功收据，退款预留额仍保留；断线与响应丢失恢复原命令。COMMAND_PENDING_RETRY 带 command_id／retryable，浏览器刷新／继续保留原 ID；audit 的 actor／request／command／result 关联与秘密排除均有直接证据。 |

```sh
rtk proxy go test ./... -count=1 -timeout=300s -json > /tmp/billforge-go-acceptance-diagnostic.json
```

## A13／A17 补充验收证据（2026-10-03）

A13：下期 Pro 五席意图与必要绑定冲突的浏览器案例 2／2 通过（27.2 秒）。A17：原始明细不变、原始／目前应收／已收／尚待支付固定金额与截断历史入口 2／2 通过（15.5 秒）；其中历史导航使用受控响应，四类 101 笔完整历史则由隔离 SQLite 真实领域写入，Go 2／2 通过。四个 quote 绑定／价格／revision Go 案例及既有 HTTP 游标契约亦通过。此轮未改执行逻辑，新增断言晚于 156 项完整套件快照，属补充证据。

```sh
rtk pnpm --dir web/admin exec playwright test e2e/admin.spec.ts --grep '(cancel, resume and schedule a plan change|a different seat price quote cannot use)' --reporter=line
rtk pnpm --dir web/admin exec playwright test e2e/admin.spec.ts --grep '(lost reduction response recovers|invoice history page follows)' --reporter=line
rtk go test ./lab -run 'TestAdminInvoice(HistoryPagesBeyondDetailLimitWithoutRepeats|ApplicationAndRefundHistoryPagesBeyondDetailLimit)$' -count=1 -timeout=120s
rtk go test ./lab ./api/admin -run 'Test(AdminChange(QuoteAndScheduledPlanAtomic|PreviewRejectsAnotherQuotesBindingForBothModes|PreviewRejectsSupersededPriceForBothModes|PreviewRejectsChangedRevisionForBothModes)|InvoiceHistorySessionCursorAndReadContracts)$' -count=1 -timeout=120s
```

## 限定场景验收判定（2026-10-03）

历史 checkpoint（已由上方最终本机验收取代）：A08／A13／A15／A16／A17／A23／A26 已逐条满足原场景条件，判定经独立复查。A23 在既有比对案例补人工决议后的金融断言，A16 补耗尽额度的停用原因。其他逐动作变体仍归 A30，Web Admin 尚未整体验收。 A13 补下期 Pro 五席显示；A17 补四类完整历史、原始明细不变与固定帐务数字。

完整浏览器回归：156/156 (25.4m)；逐动作稽核 49／49、相关 Go 场景 9／9、补强浏览器断言 2／2。

| ID | 本机通过 |
| --- | --- |
| A08 | 41 个预览动作到期后沿原键找回原命令，命令与收据数不增加。共用受理路径先找 actor／key 重播，再核预览与来源。Go 另验真实时钟 revision 变更、原结果引用、改 payload 冲突与过期新键拒绝；C02／C07 已验刷新恢复。 |
| A13 | Basic 新购、Pro 下期排程、取消／恢复、过期／已购报价拒绝与 quote/binding 原子性均有直接证据。席次、选价与 revision 不符时拒绝且不写入；订阅详情现直接核对下期 pro-v1／5 席意图，成功与绑定冲突的补强浏览器案例 2／2 通过。 |
| A15 | 真实 2001 拒绝验 USD 20 可收上限；合法部分付款与竞争预览只收固定 USD 15／20，各一笔 allocation 与收据。C08 保留原失败操作与帐单；C09 指定派送时较早队列仍待处理。失败／等待／确认与原操作查证入口已验。 |
| A16 | 退款预留 500 与抵扣 500 共用 funded Credit 1000，不超支。UNKNOWN 保留额度，原键查证后只有一笔退款；指定 400 退款连回原 capture，较早退款未派送。详情区分可用／保留／已退，耗尽时明示原因并停用支出入口；补强 UI 断言另以 2／2 通过。 |
| A17 | C11 减额及原键恢复后原始 Lines 相同；画面核对原始 USD20、目前应收 USD15、已收 USD20、尚待支付 USD0。C11／C13／C14 保留来源、固定 534 oracle 与唯一重播效果。更正、Credit、抵扣、退款各有 101 笔真实数据，以有界游标查完且不重复，新插入不混入旧游标；详情截断、完整历史入口、非法／跨种类游标均有证据。补强浏览器 2／2、历史 Go 2／2 通过。 |
| A23 | 浏览器验原 expected／actual、安全修复与 verification、revision 改变时 blocked、provider 金额不符须人工审查。C35 后比对案例核对未清 2000、已用 0、零本地更正／allocation；provider 事实不变，原修复键与刷新恢复亦有证据。 |
| A26 | 稳定 local-admin actor 在新 session 可找回原命令；真实重启使旧 session 失效后仍恢复原命令。刷新与真实写入超时保留原键；外部不确定性维持等待，provider 事实、金融效果与收据皆唯一。 |

```sh
BILLFORGE_E2E_ACTION_CASE_AUDIT=/tmp/billforge-admin-full-final-cases.jsonl rtk pnpm --dir web/admin exec playwright test --reporter=line --output=/tmp/billforge-admin-full-final
rtk proxy node web/admin/e2e/audit-action-cases.mjs /tmp/billforge-admin-full-final-cases.jsonl
rtk pnpm --dir web/admin exec playwright test e2e/admin.spec.ts --grep '(C12 applies funded credit|credit detail separates available)' --reporter=line
rtk go test ./lab -run 'TestAdmin(SafeEntitlementRepairMatchesDomain|UnsafeProviderCaptureStaysManualReviewMatchesDomain|RepairRejectsChangedSourceRevisionMatchesDomain|ManualDecisionStoresReasonAndReceipt|SuccessfulCommandReplayPrecedesExpiredPreviewAndClockRevision|CreatePaymentReplacesUnsentOperationAtomically|RetryPaymentTargetsDefinitivelyFailedOperation|CreditApplicationAndUnknownRefundShareFundedBudget|RefundDispatchTargetsExactRefund)$' -count=1 -timeout=120s
```

## 输入修正与旧错误清除（2026-10-03）

C07 在修改金额后仍保留失败预览或已拒绝命令的错误；C03／C04 共用方案表单在更正报价后也保留绑定冲突错误。输入变更时现在会重置对应 mutation 状态。既有输入 revision 仍会拒绝较旧的预览响应；结果不确定的命令意图仍保留并锁定编辑。

| 范围 | 回归证据 | 定向结果 |
| --- | --- | --- |
| C07 预览 | 真实 HTTP 409 拒绝对 USD 20.00 帐单输入 2001；改成 1000 后旧错误消失，必须重新取得 USD 10.00 预览，零 C07 命令入场。 | 2／2，含成功预览变更后失效（15.4 秒）。 |
| C07 命令 | 受控 HTTP 422 未受理命令；由 300 改成 400 后旧拒绝错误消失，重新预览与确认只创建一笔 400 操作。 | 2／2，含跨 session／重整沿原键恢复（20.1 秒）。 |
| C03 绑定 | 真实 HTTP 409 拒绝七席报价配五席绑定；改回原报价后旧错误消失，必须重新取得 USD 100.00 预览，零 C03 命令入场。 | 2／2，含成功方案预览变更后失效（24.7 秒）。 |

三项回归在修正前均因旧错误仍存在而失败，修正后通过。C07 命令拒绝为受控 UI 证据，两项预览拒绝来自真实 Go API。前端构建通过；独立代码与测试审查没有发现待修问题。完整浏览器回归：156/156 (25.4m)；逐动作稽核 49／49。

```sh
rtk pnpm --dir web/admin exec playwright test e2e/admin.spec.ts --grep 'editing (an overpayment|a payment amount)' --reporter=line
rtk pnpm --dir web/admin exec playwright test e2e/admin.spec.ts --grep '(a different seat price quote|cancel, resume and schedule a plan change)' --reporter=line
rtk pnpm --dir web/admin exec playwright test e2e/admin.spec.ts --grep '(rejected payment creation restores focus|a lost payment command response)' --reporter=line
```

## 付款竞争与实际收款补验（2026-10-03）

历史 checkpoint（已由上方最终本机验收取代）：补强两个既有浏览器案例，使用真实 Go HTTP、隔离 commerce/provider SQLite 与 fake provider，定向 **2／2 通过**。C07 现在派送胜出的替代操作；C08 改用固定 USD 20.00 oracle，避免只比较同一操作衍生的两个值。两案均核对唯一帐单 allocation 与 C09 收据。请求稽核另验 C01、C02、C07、C08、C09、C47 的原键重播与同键不同 payload 拒绝。A30 仍为部分完成；本次定向记录已由上方完整回归与限定场景判定补充。

```sh
BILLFORGE_E2E_ACTION_CASE_AUDIT=/tmp/billforge-payment-cases-20261003.jsonl pnpm --dir web/admin exec playwright test e2e/admin.spec.ts --grep 'competing (payment|retry) previews' --reporter=line --output=/tmp/billforge-payment-verified-20261003
go test ./lab -run 'TestAdminCompeting(Payment|Retry)Previews' -count=1 -timeout=120s
```

重跑时请改用尚未使用的稽核与输出路径。本次浏览器 2／2 通过，耗时 26.2 秒；Go 2／2 通过。`pnpm --dir web/admin build` 亦通过。

## 浏览器请求、收据与重播证据（2026-09-29）

更新后的 `admin.spec.ts` 浏览器案例于 2026-09-29 **104／104** 项通过（16.4 分钟）；此前完整 Playwright 测试套件 **155／155** 项通过。`audit-action-cases.mjs` 确认 **49／49** 项操作都有与成功收据匹配的浏览器请求、原键重放，以及同键不同请求的冲突拒绝。

对每项操作，审计从新的管理员 session 使用原始请求 body 与幂等键重放一条成功命令，确认服务器返回原命令 ID、收据仍只有一条，且命令表没有新增记录。随后在同一幂等键下修改一个有效请求字段，要求返回 `409 IDEMPOTENCY_CONFLICT`，并再次确认命令与收据均未增加。每次运行请使用新的输出路径：

```sh
BILLFORGE_E2E_ACTION_CASE_AUDIT=/tmp/billforge-action-cases.jsonl pnpm --dir web/admin test:e2e
node web/admin/e2e/audit-action-cases.mjs /tmp/billforge-action-cases.jsonl
```

此项检查只覆盖每项操作的一条请求与幂等键路径，尚不足以证明全部资金事实、来源修订版或预览冲突，以及响应中断后的恢复。下文记录逐案证据与剩余缺口。

## 共用运行路径

- 所有命令实际使用 `POST /admin/api/commands`，以 `action_id` 指定 C01–C49；需要预览的命令先使用 `POST /admin/api/previews`。下表的路径是 Web Admin 页面路径，不是各动作独立的 HTTP endpoint。这与工程契约中逐资源的拟定 POST 路径不同；目前采单一受控入口，应以这个已实作契约作为后续验收依据。
- `api/admin/permissions.go` 决定能力，`lab/admin_commands.go` 负责 canonical payload、幂等键、预览绑定、命令受理及运行。命令可由 `GET /admin/api/commands/{id}` 找回；可恢复者使用 `POST /admin/api/commands/{id}/resume`。批量另由 `GET /admin/api/jobs/{id}` 查逐项结果。
- `api/admin/action_http_matrix_test.go` 对 49 个动作的命令与预览入口逐项验无 session、缺 CSRF、缺能力时的 HTTP 状态与错误码；另在具备能力时逐项送未知 payload 字段，验 422 且未受理任何命令。`api/admin/permissions_test.go` 验每个动作有能力映射，及具备其能力时的授权判断。这些测试尚未证明每种合法 payload 或各种金融结果。
历史 checkpoint（已由上方最终本机验收取代）：- `lab/admin_numeric_payload_test.go` 验 C07/C15 的大整数精确度与不合法金额格式、C18 的精确费率分母、C33 的 UTC 截止时间边界。`api/admin/numeric_http_test.go` 进一步在真实管理 HTTP handler 验 C07／C11／C12／C15 不合法金额、C18 不合法分母和 C33 不合法 UTC 于命令或预览入场前被拒，且无命令；有效大整数字串在 C07／C15 抵达来源查找，C18 预览保留精确分母。`web/admin/e2e/admin.spec.ts` 另验 C07／C11／C12／C15 表单拒绝超出 int64 上限、改为超过 JavaScript safe integer 但仍在 int64 范围后解调试误，且不创建命令。其余字段仍需各自的 HTTP／browser 边界案例。
历史 checkpoint（已由上方最终本机验收取代）：- `lab/admin_preview_admission_test.go` 在 C07 验预览的 actor、动作、对象、内容、期限及单次 claim；原键在预览到期后仍找回原命令，不同 payload 不能共用该键。受理流程现在对所有 R 类动作使用同一组核对，且在不符时不创建命令；各动作的来源版本与交易内重算仍需个别证据。
- `lab/admin_reconcile_no_evidence_test.go` 验 C10/C17 查证时 provider 尚无终局事实，命令维持待查证且无成功收据；退款额度保持预留。原 provider key 后来出现成功证据，原命令才完成。浏览器另验 C10 的等待与恢复。
- 完整 Playwright 测试套件于 2026-09-29 **155** 项通过，其中包含 **104** 条管理界面浏览器案例。路由、过期 session、筛选与查询错误另由 `lab/admin_resource_filters_test.go`、`lab/admin_resource_queries_test.go`、`api/admin/resource_filters_test.go` 和 `api/admin/query_params_test.go` 支持。路由检查只能证明页面可访问；下方操作表不代表所有错误路径均已覆盖。

`R` 表示必须持有效预览确认；`N` 表示无预览，但仍须授权、幂等与来源检查。表中路径省略 `/admin` 前缀。Go 证据路径省略 `lab/` 前缀。

| 动作 | UI 路径 | 能力 | 预览 | 交易测试档 |
| --- | --- | --- | --- | --- |
| C01 | `/quotes/new` | `subscription.manage`；合约另需 `contract.manage` | N | `admin_commands_test.go`、`admin_contract_actions_test.go`、`admin_financial_parity_test.go`（Pro 五席金融事实比对）、`admin_contract_parity_test.go`（Net30 合约报价比对）、`web/admin/e2e/admin.spec.ts`（购买与变更绑定报价） |
| C02 | `/quotes/:id/accept` | `subscription.manage`；合约另需 `contract.manage` | R | `admin_commands_test.go`、`admin_financial_parity_test.go`（Pro 五席金融事实比对）、`admin_contract_parity_test.go`（Net30 接受后帐单与权益比对）、`admin_stopped_writer_test.go`（停止后拒绝新预览，旧预览运行失败且无订阅／收据）、`web/admin/e2e/admin.spec.ts`（购买与预览后来源变动） |
| C03 | `/subscriptions/:id/schedule-plan` | `subscription.manage` | R | `admin_schedule_plan_test.go`、`admin_financial_parity_test.go`（下期 Pro 五席调度及 C44 续约与直接领域路径比对）、`price_migration_test.go`、`web/admin/e2e/migration-conflict.spec.ts`（迁移待处理时预览回 409，正式调度亦拒绝）；`admin.spec.ts` 实际调度并核对目标价、唯一订阅与成功收据。 |
| C04 | `/subscriptions/:id/upgrade` | `subscription.manage` | R | `admin_immediate_upgrade_test.go`、`admin_financial_parity_test.go`（周期中点升级的领域／管理路径金额与权益比对）、`web/admin/e2e/admin.spec.ts`（期中升级与旧预览重算） |
| C05 | `/subscriptions/:id/cancel` | `subscription.manage` | R | `admin_commands_test.go`、`price_migration_test.go`、`web/admin/e2e/migration-conflict.spec.ts`（迁移 pending／conflicted 时预览回 409，没有预览或调度写入）；`admin.spec.ts` 实际排定取消并核对此订阅唯一成功收据。 |
| C06 | `/subscriptions/:id/cancel`（依状态显示恢复） | `subscription.manage` | R | `admin_commands_test.go`、`admin_stopped_writer_test.go`（停止后拒绝新预览，旧预览运行失败且保留取消调度）、`web/admin/e2e/admin.spec.ts` 实际恢复取消调度、核对调度状态与此订阅唯一成功收据。 |
| C07 | `/invoices/:id/payments/new` | `finance.adjust` | R | `admin_payment_test.go`（含收据失败时保留旧收款、重启后原键恢复）、`admin_competing_payment_preview_test.go`（两个已受理预览竞争，唯一替代操作与收据、旧命令失效及双键重播）、`admin_cross_instance_payment_test.go`（两个独立 Lab 实例共用 SQLite 并行运行）、`admin_cross_process_payment_test.go`（两个独立 OS 进程同步运行）、`web/admin/e2e/admin.spec.ts`（部分付款、预览后余额变动及双分页替代操作竞争）、`admin_payment_dispatch_interleaving_test.go`（跨实例付款创建、旧预览失效及精确派送） |
| C08 | `/payments/:id/retry` | `finance.adjust` | R | `admin_payment_test.go`、`admin_competing_payment_preview_test.go`（两个已受理重试预览只创建一笔新义务，失效命令零成功收据、原键重播）、`admin_cross_instance_payment_test.go`（两个独立 Lab 实例共用 SQLite 并行运行）、`admin_cross_process_payment_test.go`（两个独立 OS 进程同步运行）、`web/admin/e2e/admin.spec.ts`（确定失败后重试与余额变动）、`admin_payment_dispatch_interleaving_test.go`（跨实例付款创建、旧预览失效及精确派送）、`web/admin/e2e/admin.spec.ts`（双分页 C08 预览竞争、HTTP 409 与胜出操作后续 C09 精确派送） |
| C09 | `/payments/:id/dispatch` | `finance.adjust` | R | `admin_external_test.go`、`web/admin/e2e/admin.spec.ts`（付款 provider commit 后中断；待查证时调整时钟仍沿原 business time 完成）、`admin_payment_dispatch_interleaving_test.go`（跨实例付款创建、旧预览失效及精确派送）、`web/admin/e2e/admin.spec.ts`（C07 取代后旧 C09 预览 HTTP 409，仅新操作派送）、`web/admin/e2e/admin.spec.ts`（指定较晚付款操作派送，不取出队列较早操作） |
| C10 | `/payments/:id/reconcile` | `finance.adjust` | N | `admin_external_test.go`、`admin_reconcile_no_evidence_test.go`、`provider_lookup_failure_test.go`、`api/admin/provider_lookup_failure_test.go`、`web/admin/e2e/admin.spec.ts` |
| C11 | `/invoices/:id/reductions/new` | `finance.adjust` | R | `admin_reduction_test.go`、`admin_financial_parity_test.go`（部分付款与已付款减额两种资金来源和直接领域路径比对）、`web/admin/e2e/admin.spec.ts` 的帐单来源案例 |
| C12 | `/credits/:id/apply` | `finance.adjust` | R | `admin_apply_credit_test.go`、`admin_financial_parity_test.go`（跨帐单套用额度与直接领域路径比对）、`admin_credit_budget_interleaving_test.go`（跨实例与 C15 争用同一额度）、`web/admin/e2e/admin.spec.ts` 的 C12 案例 |
| C13 | `/jobs/change-corrections` | `finance.adjust` | R | `admin_change_corrections_test.go`（101 笔来源变动后跨批轮转）、`web/admin/e2e/admin.spec.ts` 的延迟更正来源案例：实际运行批量，仿真提交后回应遗失；重整沿同一幂等键找回，SQLite 核对唯一工作、逐项结果、更正与命令收据。 |
| C14 | `/changes/:id/resolve-unfulfilled` | `finance.adjust` | R | `admin_unfulfilled_test.go`、`web/admin/e2e/admin.spec.ts` 的未履行升级案例：实际运行未履行决议，仿真提交后回应遗失；重整沿同一幂等键找回，SQLite 核对唯一决议、更正、Credit 与命令收据。 |
| C15 | `/credits/:id/refunds/new` | `finance.adjust` | R | `admin_reserve_refund_test.go`（含收据失败回滚、重启与原键恢复）、`admin_refund_budget_parity_test.go`（与直接领域路径比对 funded credit 预算）、`admin_credit_budget_interleaving_test.go`（跨实例与 C12 争用同一额度）、`web/admin/e2e/admin.spec.ts`（已收款减额后预留退款、C12 额度争用、双分页 C15 预览失效后重新确认与唯一收据） |
| C16 | `/refunds/:id/dispatch` | `finance.adjust` | R | `admin_external_test.go`、`web/admin/e2e/admin.spec.ts`（退款 provider commit 后中断；确定失败时保留额回到可用额度）、`admin_refund_dispatch_interleaving_test.go`（C15／C16 跨实例竞争、唯一 provider 退款与额度收据） |
| C17 | `/refunds/:id/reconcile` | `finance.adjust` | N | `admin_external_test.go`、`admin_reconcile_no_evidence_test.go`、`provider_lookup_failure_test.go`、`admin_refund_reconcile_test.go`（UNKNOWN 退款查证、同键重播与唯一收据）、`web/admin/e2e/admin.spec.ts`（C16 回应遗失后经 C17 查证，原 C16 仍沿原键完成，提供者退款唯一） |
| C18 | `/prices/pro/new` | `catalog.publish` | R | `admin_catalog_test.go`、`web/admin/e2e/admin.spec.ts`（发布组件与 checksum、同版号与原 ID 变更冲突、所有数值字段的 UI 非法边界、版本来源变动；提交后回应遗失再以原键恢复，SQLite 价格、命令与收据各一笔）、`catalog_receipt_failure_test.go`（真实 HTTP 收据 INSERT 故障回滚及原键恢复）、`web/admin/e2e/migration-conflict.spec.ts`（迁移目标价格）；`admin_price_minimum_upfront_test.go` 与 `minimum_upfront_http_test.go` 验固定费用加最少一席的 `int64` 边界与预览拒绝 |
| C19 | `/meters/new` | `catalog.publish` | R | `admin_catalog_test.go`（同 ID 不同 schema 的旧预览失效、唯一收据、成功／失败同键重播及改 payload 拒绝）、`api/v1_test.go`（管理发布 AI token meter 后检查旧 client）、`web/admin/e2e/admin.spec.ts`（双分页冲突显示 `PREVIEW_STALE`、既有 unit 不被覆写；创建 AI token meter 并完成报价、接受及用量记录） |
| C20 | `/prices/metered/new` | `catalog.publish` | R | `admin_catalog_test.go`（同 ID 不同金额、同方案版号不同 ID 的旧预览失效，唯一价格与收据、原键重播与改 payload 拒绝）、`api/v1_test.go`（新费率揭露及旧 client 能力守卫）、`web/admin/e2e/admin.spec.ts`（双分页旧价格预览显示 `PREVIEW_STALE`、已发布 3500 与组件不被覆写；全部数值字段的 UI 非法边界、AI 报价与 token 用量）；`admin_price_minimum_upfront_test.go` 与 `minimum_upfront_http_test.go` 验固定费用加最少一席的 `int64` 边界与预览拒绝 |
| C21 | `/catalog-selections/new` | `catalog.publish` | R | `admin_catalog_test.go`（同方案／cohort／时点的两个已受理命令竞争，旧命令 `PREVIEW_STALE`、唯一选价与收据、成功／失败键重播）、`api/v1_test.go`（选价后同一报价的旧／新 client 行为）、`web/admin/e2e/migration-reversal.spec.ts`（选回旧价格）、`web/admin/e2e/admin.spec.ts`（双分页旧预览回 409 且不覆写先选价格；同时点改选回 409；AI 价格选价后报价接受） |
| C22 | `/price-migrations/new` | `migration.manage` | R | `admin_migrations_test.go`、`price_migration_test.go`（来源价格、席次与调度变动后的批量暂停及金融事实不变）、`web/admin/e2e/migration-reversal.spec.ts`（新批量反向迁移）、`migration-conflict.spec.ts`（revision 变动，以及来源价格、席位、调度三种独立批量冲突）；`admin.spec.ts` 实际创建两户批量，SQLite 核对该批量唯一成功收据。 |
| C23 | `/price-migrations/:id/pause` | `migration.manage` | N | `admin_migrations_test.go`、`web/admin/e2e/admin.spec.ts` 实际暂停批量，SQLite 核对 paused 状态及此批量唯一成功收据。 |
| C24 | `/price-migrations/:id/skip` | `migration.manage` | R | `admin_migrations_test.go`、`web/admin/e2e/migration-conflict.spec.ts`（冲突后明确略过，不撤销已套用户）；`admin.spec.ts` 略过一户并核对 skipped 项目及此批量唯一成功收据。 |
| C25 | `/price-migrations/:id/resume` | `migration.manage` | R | `admin_migrations_test.go`、`web/admin/e2e/admin.spec.ts` 恢复后保留略过户、未完成户回 pending，SQLite 核对 active 状态及此批量唯一成功收据。 |
| C26 | `/usage-events/new` | `usage.manage` | N | `admin_commands_test.go`、`web/admin/e2e/admin.spec.ts`（原事件入帐、回应遗失恢复、相同事件重送与变更 payload 拒绝） |
| C27 | `/usage-adjustments/new` | `usage.manage` | R | `admin_usage_actions_test.go`（并发调整耗用原事件剩余量后旧预览失效、零写入，新预览只反向剩余量）、`web/admin/e2e/usage-preview-stale.spec.ts`（双分页并发调整回 409，原数量无法重新预览，改为剩余量后才成功）、`web/admin/e2e/admin.spec.ts`（用量调整与差额） |
| C28 | `/usage-periods/:id/close` | `usage.manage` | R | `admin_usage_actions_test.go`（预览后添加符合 cutoff 的事件，旧关帐零计价修订／收据，新预览精确关帐）、`web/admin/e2e/usage-preview-stale.spec.ts`（双分页来源变动回 409，显示 20000→20010，重新确认前零修订，最终唯一收据）、`web/admin/e2e/admin.spec.ts`（关帐） |
| C29 | `/usage-periods/:id/rerate` | `usage.manage` | R | `admin_usage_actions_test.go`（预览后添加晚到事件，旧重算不添加修订／收据，新预览含完整晚到用量）、`web/admin/e2e/usage-preview-stale.spec.ts`（双分页晚到事件回 409，显示 20020→20030，重新确认前保留原修订，最后仅一笔新修订／收据）、`web/admin/e2e/admin.spec.ts`（晚到事件重算） |
| C30 | `/jobs/usage-credit-notes` | `finance.adjust` | R | `admin_usage_credit_notes_test.go`（101 个合成帐期冲突后跨批轮转）、`admin_batch_jobs_test.go`（首项提交后重开只完成剩余项）、`web/admin/e2e/admin.spec.ts`：负差额后实际运行 Credit Note 批量，仿真提交后回应遗失；重整沿同一幂等键找回，SQLite 核对唯一工作、逐项结果、Credit Note 与命令收据。 |
| C31 | `/contracts/new` | `contract.manage` | R | `admin_contract_actions_test.go`（同 ID 同内容并发发布只保留一版与唯一收据；不同内容令旧预览失效、无成功收据且不可重写）、`admin_contract_parity_test.go`（发布后合约来源与直接领域路径比对）、`web/admin/e2e/admin.spec.ts`（双分页同 ID 不同内容回 409，显示失效与重新预览失败；企业合约创建与 Net30）；`admin_price_minimum_upfront_test.go` 与 `minimum_upfront_http_test.go` 验固定费用加最少一席的 `int64` 边界与预览拒绝 |
| C32 | `/jobs/contract-collections` | `finance.adjust` | R | `admin_contract_actions_test.go`（101 笔到期操作跨批收款；预览后另一收款器先创建 capture outbox 时本批项目记为 `SOURCE_CHANGED`，不重复派送，同键找回原命令）、`admin_batch_jobs_test.go`（首项提交后重开只创建剩余 capture outbox）、`admin_contract_parity_test.go`（到期收款与直接领域路径比对）、`web/admin/e2e/admin.spec.ts`（合约到期收款批量） |
| C33 | `/reconciliation-runs/new` | `reconciliation.repair` | N | `admin_reconciliation_test.go`、`web/admin/e2e/admin.spec.ts`（差异创建与修复后 verification） |
| C34 | `/discrepancies/:id/repair` | `reconciliation.repair` | R | `admin_reconciliation_test.go`、`web/admin/e2e/admin.spec.ts`（安全修复、来源变动阻挡与 verification） |
| C35 | `/discrepancies/:id/manual-decisions` | `reconciliation.repair` | R | `admin_reconciliation_test.go`（另一审核者于预览后先记录决议，旧命令 `PREVIEW_STALE` 且零额外决议／收据；新预览才记录新决议）、`web/admin/e2e/manual-decision-stale.spec.ts`（双分页 409、显示 `open → investigating`、再次确认前唯一决议／收据）、`web/admin/e2e/admin.spec.ts`（金额不符的人工作业记录） |
| C36 | `/account-migrations/new` | `migration.manage` | R | `admin_account_links_test.go`、`web/admin/e2e/admin.spec.ts` 实际链接帐户，显示 owner 与门槛；SQLite 核对该 legacy ID 唯一成功收据。 |
| C37 | `/account-migrations/:id/shadow-quotes` | `migration.manage` | N | `admin_account_links_test.go`、`web/admin/e2e/admin.spec.ts` 实际记录报价 shadow，验比对一致及该帐户唯一成功收据。 |
| C38 | `/account-migrations/:id/shadow-entitlements` | `migration.manage` | N | `admin_account_links_test.go`、`web/admin/e2e/admin.spec.ts` 实际记录权益 shadow，验 adapter 权益与该帐户唯一成功收据。 |
| C39 | `/account-migrations/:id/provenance` | `migration.manage` | R | `admin_account_links_test.go`、`admin_provenance_stale_test.go`（预览后同 legacy invoice 被不同映射先回填，原命令 `PREVIEW_STALE`，不覆写来源或创建成功收据）、`web/admin/e2e/provenance-preview-stale.spec.ts`（双分页 409，保留先写入的 manual review 映射与唯一收据）、`web/admin/e2e/admin.spec.ts`（不完整历史来源入场） |
| C40 | `/account-migrations/:accountId/provenance/:legacyInvoiceId/resolve` | `migration.manage` | R | `admin_account_links_test.go`、`admin_provenance_stale_test.go`（另一审核者先完成来源决议，旧预览失效且无重复决议事件／收据）、`web/admin/e2e/provenance-preview-stale.spec.ts`（双分页 409，保留唯一决议事件与收据）、`web/admin/e2e/admin.spec.ts`（历史来源人工审核） |
| C41 | `/account-migrations/:id/switch-read` | `migration.manage` | R | `admin_account_links_test.go`、`admin_cutover_stale_test.go`（预览后添加不一致报价 shadow，旧切读 `PREVIEW_STALE`，读写 owner 不变且零成功收据）、`web/admin/e2e/admin.spec.ts`（未过门槛阻挡与切读） |
| C42 | `/account-migrations/:id/switch-writer` | `migration.manage` | R | `admin_account_links_test.go`、`admin_cutover_stale_test.go`（切读后预览切写，准备条件下降时旧命令失效且 writer 保持 legacy）、`web/admin/e2e/cutover-readiness-stale.spec.ts`（双分页 shadow 变动回 409，零切写事件／收据）、`web/admin/e2e/admin.spec.ts`（切读后切写；成功回应遗失后刷新，以原 request key 找回，命令、收据与 `writer_cutover` 各只一笔） |
| C43 | `/account-migrations/:id/stop` | `migration.manage` | R | `admin_account_links_test.go`、`admin_stopped_writer_test.go`（C02／C05／C06 预览与停止后原命令）、`web/admin/e2e/admin.spec.ts`（停止事件及 owner；C02／C05 新预览回 409，零后续商务命令）；SQLite 核对停止命令唯一成功收据。 |
| C44 | `/jobs/renewals` | `operations.run` | R | `admin_maintenance_jobs_test.go`（预览固定成员；第二户确认前到期仍须新批量；前 100 户因迁移暂停而等待时，第 101 户可由下批续约）、`admin_batch_jobs_test.go`（首户提交后重开只续约剩余户）、`admin_financial_parity_test.go`（月底及调度变价续约比对）、`admin_contract_parity_test.go`（缺后续合约价格时暂停续约比对）、`web/admin/e2e/migration-reversal.spec.ts`（前后两期金额及指派来源）、`migration-conflict.spec.ts`（部分套用、暂停、略过后不同价格续约；三种来源变动均无添加帐期或帐单） |
| C45 | `/jobs/entitlement-refresh` | `operations.run` | R | `admin_maintenance_jobs_test.go`（101 户跨三批轮转，尾端户不饥饿；预览后其中一户 revision 改变时该项 `SOURCE_CHANGED` 且不重建其权益，另一户仍成功并保留唯一批量收据）、`web/admin/e2e/entitlement-batch-source-stale.spec.ts`（双分页变更订阅，工作详情将冲突与成功户分列，SQLite 保留唯一收据）、`web/admin/e2e/admin.spec.ts`（固定成员）、`web/admin/e2e/maintenance-backlog.spec.ts`（超过 100 户时提示仍需下一批） |
| C46 | `/lab/clock` | `lab.control` | R | `admin_controls_test.go`、`web/admin/e2e/admin.spec.ts`（C09 待查证期间前进一天，不自动收款且不改原命令业务时间） |
| C47 | `/lab/payment-decisions/:id` | `lab.control` | R | `admin_controls_test.go`（provider 决策已提交、付款已 capture 后，重启沿原 receipt 完成命令；相反结果竞争与终态后新命令均拒绝）、`web/admin/e2e/admin.spec.ts`（第二个相反决策显示 `DOMAIN_REJECTED`、仅首命令有收据；确定失败后重试） |
| C48 | `/lab/refund-decisions/:id` | `lab.control` | R | `admin_controls_test.go`（相反结果竞争与终态后新命令均拒绝）、`web/admin/e2e/admin.spec.ts`（第二个相反决策显示 `DOMAIN_REJECTED`、仅首命令有收据且派送前无 provider 退款；确定失败后 C16 派送释放保留额；C16 回应遗失后经 C17 查证，最终仅一笔） |
| C49 | `/lab/faults/:id` | `lab.control` | R | `admin_controls_test.go`（另一笔付款先运行不会误领指定票据；同操作第二张票据以 `failed/DOMAIN_REJECTED` 结束、原键重播不添加票据；付款／退款在领票后、provider 调用前重启仍沿原命令使用票据）、`web/admin/e2e/admin.spec.ts`（重复建票错误可见且原票据继续派送；一次性故障票据与原命令恢复）。`TestAdminFaultTicketsKeepPendingTicketVisibleAfterRecentUsedTickets` 与 `lab-fault-pagination.spec.ts` 验待使用票据优先及有界光标可跨页找回。 |

## C01／C03／C04 变更报价的冲突与原子性

历史 checkpoint（已由上方最终本机验收取代）：`lab/admin_change_binding_mismatch_test.go` 验 C01 在订阅 revision 失效时回滚报价与绑定，保留失败命令而不产生成功收据；C03／C04 均在报价绑定不符、选价被取代或订阅 revision 改变时拒绝预览，没有新预览、命令或方案变更。浏览器以真实表单验 C01 零残留，C03／C04 竞争者修改订阅后重新预览的原因显示，以及 C18／C21 选用新 Pro 价格后 C03 旧报价被拒绝。这些定向证据尚未覆盖 C01–C49 每项动作的完整重播与恢复矩阵。

## C03／C04 回应遗失与原键恢复

`web/admin/e2e/admin.spec.ts` 的两个真实浏览器情境在服务器已接受并完成 C03／C04 后中断原回应。刷新后接口以原幂等键取回同一命令；同键不同报价 ID 回 `409 IDEMPOTENCY_CONFLICT`。C03 只有一笔变更调度与成功收据。C04 只有一笔立即变更、帐单、付款操作与成功收据，订阅服务在付款前仍为 Basic。此证据涵盖指定两个动作的提交后回应遗失，不代表其他 47 个动作的中断路径已验完。

`TestAdminScheduledPlanReceiptFailureRollsBackAndRecoversOriginalCommand` 另在 C03 收据写入时注入 SQLite 失败：原命令保持 `accepted`，调度与订阅 revision 不变，没有添加付款或 provider capture。重启后两次恢复只产生一笔调度与收据；原键重播返回同一命令，改 payload 则冲突。

## C18 Pro 价格发布的端到端追踪

| 面向 | 目前证据与判定 |
| --- | --- |
| 入口与运行 | `/prices/pro/new` 使用共用 `ActionForm` 创建 C18 预览，再以原预览及幂等键提交共用命令入口。`executeAdminCatalogTx` 在同一交易比对来源快照后调用 `publishProPriceTx`，回传价格版本 ID、方案与 checksum；成功命令与 `admin_command_receipts` 同交易保存。 |
| 权限与输入 | C18 需要 `catalog.publish`；共用 HTTP 权限矩阵验未登录、CSRF 与缺能力拒绝。`canonicalAdminCatalogPayload`、领域发布路径与管理 HTTP 验正整数、UTC、费率分母，以及固定费用加最少一席不超出 `int64`。 |
| 价格不变性 | `admin_catalog_test.go` 与浏览器案例核对已发布组件、checksum、同版号或同 ID 不同内容的冲突，以及来源变动时旧预览失效。 |
| 回应遗失与原键恢复 | `lost C18 publish response recovers one price and receipt with the original key` 让管理 HTTP 完成 C18 后切断首个回应。接口保留未确认意图，重整后用原 request key 查回成功命令；SQLite 验同一价格版本、命令与收据各一笔，checksum 存在且没有添加付款操作。定向 Playwright 案例通过。 |
| 收据写入故障 | `TestCatalogPublishReceiptFailureRecoversOriginalHTTPCommand` 令隔离 SQLite 的收据 INSERT 失败；真实管理 HTTP 回 503 `COMMAND_PENDING_RETRY` 与原命令 ID，价格版本、组件及收据均未提交。移除故障后同键重送两次只创建一笔价格、三个组件及一笔与 checksum 对应的收据；改 payload 重播回 409 `IDEMPOTENCY_CONFLICT`。定向 Go 案例以 `-count=3` 通过。 |
| 当时待验收 | C18 的其他来源冲突交错尚未逐一用真实 HTTP／SQLite 案例验证；本项不据此宣称 49 项操作整体结案。 |

## C19 计量表注册的端到端追踪

| 面向 | 目前证据 |
| --- | --- |
| 入口与契约 | `/meters/new` 的 React／Ant Design `ActionForm` 先送 `POST /admin/api/previews`，再以 `action_id=C19` 送 `POST /admin/api/commands`；命令可由原 ID 或幂等键查回。后端 `lab/admin_catalog.go` 比对预览来源，再由 `registerMeterTx` 注册不可变的 `meter_schemas` 列。 |
| 权限与输入 | `catalog.publish` 为必需能力；共用 HTTP 矩阵涵盖 401、CSRF 403、缺能力 403 与未知字段拒绝。`canonicalAdminCatalogPayload` 要求非空 ID、source、unit 及正整数 schema 版本；`registerMeterTx` 再验 ID／source 格式。 |
| 来源冲突 | `TestAdminMeterRegistrationRejectsStaleConflictingSchema` 先让同 ID 不同单位各取得预览，先运行 `token` 版本，再运行 `request` 旧命令：后者为 `failed/PREVIEW_STALE`，既有 unit 保持 `token`，两命令只有一笔成功收据。新的冲突预览直接被拒；成功与失败命令各以原键重播，改 payload 使用旧键回幂等冲突。 |
| 实际接口 | `web/admin/e2e/admin.spec.ts` 的双分页案例先发布 `token`，旧 `request` 预览确认回 HTTP 409／`PREVIEW_STALE`，接口明示失效且无法创建相冲突的新预览；SQLite 只有一笔 schema、一笔成功收据及一笔失败命令。另一案例以注册的 meter 发布价格、创建报价并记录用量。 |

历史 checkpoint（已由上方最终本机验收取代）：此处直接证明 C19 的同 ID 竞争与恢复；其余非法字符、长度、正整数边界及跨 actor 预览仍以共用矩阵与单项 payload 测试为准，A30 尚未签核。

## C20 计量价格发布的端到端追踪

| 面向 | 目前证据 |
| --- | --- |
| 入口与契约 | `/prices/metered/new` 的 React／Ant Design 表单先取得 C20 预览，再送共用命令入口；`lab/admin_catalog.go` 把 meter schema、既有 checksum、发布状态及同方案版号占用者放进来源快照，运行时重算。`publishMeteredPriceTx` 在交易内置立版本与价格组件。 |
| 权限与输入 | `catalog.publish` 为必需能力；共用 HTTP 矩阵涵盖 session、CSRF、能力及未知字段。既有浏览器案例逐栏验固定金额、席次金额、包含用量与费率分子／分母的非法数值边界；价格必须参照已注册 meter。 |
| 版本不可变 | `TestAdminMeteredPriceRejectsStaleConflictingVersion` 先接受两个同 ID 不同金额的预览命令，3500 先发布后，3000 的旧命令为 `failed/PREVIEW_STALE`；版本维持 3500、两个价格组件及一笔成功收据。成功与失败命令各沿原键找回，改 payload 使用成功键回幂等冲突，新的相冲突预览被拒。另验不同 ID 竞争同一方案版号时只保留一个版本和一笔成功收据。 |
| 实际接口 | `web/admin/e2e/admin.spec.ts` 的双分页案例先发布 3500，再确认旧 3000 预览，回 HTTP 409／`PREVIEW_STALE`；接口显示失效并无法创建相冲突的新预览，SQLite 保持 3500、两个组件、一笔成功收据及一笔失败命令。既有 AI 计量案例从 C19／C20／C21 走到报价、接受及 token 用量。 |

历史 checkpoint（已由上方最终本机验收取代）：这些案例验同版本竞争，不推论所有日期、货币数值及跨 actor 预览边界已完成；A30 仍待逐项签核。

## C21 目录选价的端到端追踪

| 面向 | 目前证据 |
| --- | --- |
| 入口与契约 | `/catalog-selections/new` 的 React／Ant Design 表单先预览 C21，再送共用命令入口；来源快照包含价格版本的方案、发布状态、checksum、有效期间及该方案／cohort／时点的既有占用者。运行前重算来源，最终写入 `catalog_selection`。 |
| 权限与兼容性 | `catalog.publish` 为必需能力；共用 HTTP 入场矩阵覆盖 session、CSRF、能力及未知字段。`api/v1_test.go` 验新计量 SKU 选价后，旧 client 因缺能力声明而拒绝接受报价，具备能力的 client 才能接受。 |
| 同时点竞争与重播 | `TestAdminCatalogSelectionRejectsStaleCompetingPrice` 在同方案、cohort、生效时点先接受两个不同价格的预览命令，先运行者保留唯一选价，后者为 `failed/PREVIEW_STALE` 且无成功收据。两个原键各找回原命令，改 payload 使用成功键回幂等冲突；已有不同价格占用该时点时，新预览被拒。 |
| 实际接口 | `web/admin/e2e/admin.spec.ts` 的双分页案例先选价格 B，再确认较早的价格 A 预览：HTTP 409／`PREVIEW_STALE` 可见，接口要求重新预览且相冲突的新预览失败；SQLite 只有 B 一笔选价、一笔成功收据与一笔失败命令。其他案例验同时点改选回旧价被拒，以及以新选价接受 AI 计量报价。 |

历史 checkpoint（已由上方最终本机验收取代）：这些案例直接验 C21 的同时点争用；不同时点的价格阶梯与跨 actor 预览仍须按适用契约核对，A30 尚未签核。

## S07 跨动作金融正确性比对

`lab/admin_s07_parity_test.go` 在两组隔离 commerce／provider SQLite 中，以同一业务时钟分别运行直接领域入口与管理命令。它逐阶段比对 C04 的期中升级金额与原 Basic 价格、C49 指定原付款的假服务商回应遗失、C09 待查证且没有成功收据、C10 查证与原 C09 收据恢复。两天后开通只须 C13 更正 534，帐单义务与净实收同为 3466，并且只有一笔 534 funded grant。

另一情境在帐期边界先用 C44 确认未知付款阻止续约；C10 查回晚到收款后，升级转为 `needs_review`，C14 将 4000 义务与净实收发布并只留一笔 4000 funded grant，之后的 C44 才创建 2000 Basic 续约帐单。两条路径都核对 provider capture 总数维持两笔（原 Basic 与升级付款）、每个成功管理命令一笔收据、待查证命令零成功收据，以及帐单原额／义务／总实收／发布／净实收／未清额。这是上述命令在 S07 组合下的直接证据，不代替每个命令的完整输入与中断矩阵。

## C07 创建付款操作的竞争与恢复追踪

- **入口与授权：**`/invoices/:id/payments/new` 通过共用预览及命令 API 提交 `C07`；能力为 `finance.adjust`。共用 HTTP 矩阵验 session、CSRF 与能力守卫，`admin_payment_test.go` 验正常创建及收据失败后沿原键恢复。
- **来源与运行：**`adminCreatePaymentPreview` 将帐单应收、帐期／到期界线、既有付款操作状态与指定金额绑入预览；`executeCreatePaymentTx` 在交易内重算来源。来源变动使旧命令 `failed/PREVIEW_STALE`，不能沿原预览创建第二笔替代付款。
- **竞争证据：**`TestAdminCompetingPaymentPreviewsKeepSingleReplacementOperation` 先受理两个不同金额的 C07，再运行 7000 胜出与 6000 失效。SQLite 只有原 10000 操作（已取消）及新 7000 操作，旧 outbox 完成、胜出命令有一笔收据、失效命令没有成功收据；两个原键各自重播原结果，改 payload 均冲突，provider 零 capture。
- **画面证据：**`competing payment previews collect only the winning replacement amount` 在双分页创建 1000／1500 预览，1500 命令胜出，旧页收到 HTTP 409 与预览失效提示。C09 随后沿替代操作的 provider key 收取固定 USD 15.00，只有一笔 capture、一笔 allocation 与一笔派送收据。已取消原操作没有 provider capture，C07 也只有一笔成功收据。2026-10-03 定向验证通过。
- **跨实例证据：**`TestAdminCompetingPaymentCommandsAcrossLabInstances` 以两个独立 Lab 实例、各自的 SQLite 连接及同一组数据库档同时运行已受理的 C07；修正 lease 与命令交易的先读后写锁冲突后，连跑 10 次均为一成功、一 `PREVIEW_STALE`，仅一笔替代操作与收据。修正前测试重现 `database is locked` 且命令留在 `accepted`；定向 `-race` 通过。
- **多进程证据：**`TestAdminCompetingPaymentCommandsAcrossProcesses` 启动两个独立测试子进程，各自打开相同 SQLite 档并等待共同起跑信号；连跑 10 次均为一成功、一 `PREVIEW_STALE`，仅一笔替代操作与成功收据，原操作取消且 provider 零 capture。
历史 checkpoint（已由上方最终本机验收取代）：- **限制：**C07 字段／范围与 UI 错误证据仍待逐项核对；竞争预览后的收款路径已补验，C07 尚未完整签核。

## C08 确定失败付款的竞争预览

- **入口与来源：**`/payments/:id/retry` 通过共用预览及命令 API 提交 `C08`，能力为 `finance.adjust`。预览只接受确定失败的原操作，依当前未清余额创建新义务；`admin_payment_test.go` 已验正常重试和取代未送出操作后的重试。
- **竞争证据：**`TestAdminCompetingRetryPreviewsKeepSingleNewObligation` 对同一确定失败操作受理两个 C08 预览。先运行命令创建一笔 10000 重试义务及成功收据，后运行命令变为 `failed/PREVIEW_STALE` 且无成功收据；两个键都重播原结果。SQLite 只有原失败操作与一笔新操作、单笔 retry request；新 provider key 未被派送，也没有成功 capture。
历史 checkpoint（已由上方最终本机验收取代）：- **跨实例证据：**`TestAdminCompetingRetryCommandsAcrossLabInstances` 以两个独立 Lab 实例共用 SQLite 档同时运行 C08；连跑 10 次均只创建一笔 retry request、新义务与成功收据，另一命令 `PREVIEW_STALE`。新 provider key 尚未派送。
历史 checkpoint（已由上方最终本机验收取代）：- **多进程证据：**`TestAdminCompetingRetryCommandsAcrossProcesses` 以两个独立测试子进程及共同起跑信号同时运行已受理 C08；连跑 10 次均只保留一笔 retry request、一笔新义务与成功收据，败方 `PREVIEW_STALE`，新 provider key 尚未派送。
- **画面证据：**`competing retry previews leave one obligation and dispatch its exact amount` 在双分页对同一确定失败付款创建预览，一笔 C08 成功，另一笔回 HTTP 409 `PREVIEW_STALE`。只有两笔付款操作、一笔 retry request 与一笔 C08 收据。C09 收取固定 USD 20.00，只有一笔成功 capture、一笔 allocation 与一笔派送收据，原付款仍为确定失败。2026-10-03 定向验证通过。
历史 checkpoint（已由上方最终本机验收取代）：- **限制：**双分页竞争与 C09 收款路径已补验；其余 C08 输入／错误与权限证据仍待逐项核对，C08 尚未完整签核。

## C09 指定付款操作派送的端到端追踪

| 验收字段 | 目前可重跑的证据 |
| --- | --- |
| Action／UI route | C09；`/payments/:id/dispatch`，只对画面所列的 `operation_id` 创建预览与提交命令。 |
| HTTP handler／授权 | `api/admin/session.go` 的 `protectedWrite` 连到 `api/admin/previews.go` 的 `createPreview` 与 `api/admin/commands.go` 的 `submitCommand`；`api/admin/permissions.go` 要求 `finance.adjust`。`api/admin/action_http_matrix_test.go` 逐动作验 session、CSRF、缺能力及未知 payload 拒绝。 |
| 预览／来源守卫 | `lab/admin_external.go` 的 `loadExternalDispatchSnapshot` 只接受 `created`、pending capture outbox、正金额及合法帐期；快照绑定 operation、invoice、provider key、金额、币别、状态和帐期。`adminExternalPreviewCurrent` 在首次派送前重验；被 C07 取消的旧操作会使 C09 `failed/PREVIEW_STALE`，不留下等待查证命令。 |
| Domain helper／外部效果 | `adminExecuteExternalCommand` 固定传入目标操作给 `dispatchCaptureAt`；`adminMarkSubmitted` 在服务商调用前持久化 submitted。结果未知时保留 `waiting_verification`，沿原 provider key 查证。服务商观察由 `applyObservationAt` 交易入库，与 C07 竞争时先取得 SQLite 写入权。 |
| 收据／恢复 | 只有终态由 `adminFinishExternalCommand` 写入唯一 `admin_command_receipts` 并标记 succeeded；失效命令无成功收据。既有 `admin_external_test.go`、`admin_lost_response_parity_test.go` 与 `admin_crash_webhook_parity_test.go` 验查证与中断恢复；`admin_payment_dispatch_interleaving_test.go` 验双实例创建／派送竞争、金额、币别、provider capture 和收据。 |
| Browser／SQLite | `web/admin/e2e/admin.spec.ts` 添加双分页旧 C09 被 C07 取代后收到 HTTP 409、失效提示、旧 key 零 capture，仅新操作收 1500；另一案例在两笔 pending outbox 中先派送较晚的指定操作，较早者仍 `created` 且零 capture。两个新案例各自定向 Playwright 1／1，加入后完整套件 92／92 通过。 |
| 当时未签核 | 已纳入完整浏览器回归 92／92；A30 其他动作仍需同粒度证据矩阵。 |

## C10 原付款操作查证的端到端追踪

| 验收字段 | 目前可重跑的证据 |
| --- | --- |
| Action／UI route | C10；`/payments/:id/reconcile`。目标是原 `operation_id`；payload 为 `{}`，依 `lab/admin_commands.go` 不需要预览。 |
| HTTP handler／授权 | 共用 `api/admin/commands.go` 的 `submitCommand`、`GET /admin/api/commands/{id}` 与 `POST /admin/api/commands/{id}/resume`；`api/admin/permissions.go` 要求 `finance.adjust`。`api/admin/action_http_matrix_test.go` 对 C10 验 session、CSRF、能力及未知字段。 |
| 来源与结果守卫 | `lab/admin_external.go` 对 C10 调用 `ReconcilePayment(ctx, targetID)`；`lab/lab.go` 用原 provider key 做 `Lookup`，找到服务商事实时才交由 `applyObservation` 入库。没有证据或非终态时命令维持 `waiting_verification`，不得推断收款成功；不存在的操作回 `DOMAIN_REJECTED`。 |
| 收据／幂等恢复 | 终态由 `adminFinishExternalCommand` 产生单一成功收据。`lab/admin_reconcile_no_evidence_test.go` 验无 provider 事实时没有成功收据；`lab/admin_external_test.go` 验取得证据后同一命令完成。C10 不添加 capture，重试依原 operation/provider key 查证。 |
| Browser／SQLite | `web/admin/e2e/admin.spec.ts` 的 `reconcile keeps submitted payment waiting until provider evidence appears` 先验 `waiting_verification` 且零成功收据，补入 provider 事实后按「重新查证」，原命令变 succeeded、只有一笔收据及原 key 一笔 capture。另有回应遗失后从命令页找回原操作的浏览器案例。 |
| 当时未签核 | A30 仍需其余动作同粒度的 HTTP、来源守卫及浏览器证据。 |

## C11 帐单减额的端到端追踪

| 验收字段 | 已核对的路径与证据 |
| --- | --- |
| Endpoint、UI、handler | `POST /admin/api/previews` 与 `POST /admin/api/commands` 使用 `action_id=C11`；`GET /admin/api/commands/{id}` 找回原命令。`web/admin/src/App.tsx` 的 `/invoices/:id/reductions/new` 使用 `ActionForm` 输入减额与理由；`api/admin/session.go`、`api/admin/commands.go` 接入管理命令。 |
| 分派、领域 helper | `lab/admin_previews.go` 分派 C11 到 `adminCreateReductionPreview`；`lab/admin_commands.go` 分派 C11 到 `executePostReductionTx`，再于同一 SQLite 交易内调用 `postReductionTx`。结果包含更正 ID、减额前后义务及实际 grant IDs。 |
| 来源守卫、预览 policy | R 类预览绑定 actor、动作、帐单、payload、期限及单次 claim。`loadReductionSnapshot` 拒绝已送出／未知付款、无效金额、减至零及已抵扣 Credit 的帐单；来源 JSON 绑定原义务、已收款、已抵扣、付款操作与 allocation 状态。运行交易内重算与比对，还核对实际减额前后义务与预览一致，变动回 `PREVIEW_STALE`。 |
| Permission | `api/admin/permissions.go` 映射 `finance.adjust`；共用 HTTP 与权限矩阵验无 session、缺 CSRF、缺能力及未知字段的入场守卫。 |
| 收据、恢复 | 更正、可能产生的 funded grant、旧未送出收款取消与成功收据在同一命令交易；同键重播返回原更正与收据。预览失效须重新显示来源及金额差异、再次确认，不能自动套用旧金额。 |
| 实测 | `lab/admin_reduction_test.go` 验已收款减额的精确 grant／收据／重播，以及未收款减额取消旧收款、只收剩余额；`lab/admin_financial_parity_test.go` 与直接领域路径比对部分付款及已收款减额；`web/admin/e2e/admin.spec.ts` 验双分页更正使旧预览 409、显示差异、重新确认后没有重复更正，并追查来源历史。添加 C11 受控 422 案例验表单与焦点恢复、错误关联且零命令；另一案例在服务器提交后切断回应，重整后沿原键找回唯一更正、funded grant 与收据。 |

## C12 抵扣 Credit 的端到端追踪

| 验收字段 | 已核对的路径与证据 |
| --- | --- |
| Endpoint、UI、handler | `POST /admin/api/previews` 与 `POST /admin/api/commands` 使用 `action_id=C12`；`GET /admin/api/commands/{id}` 找回原命令。`web/admin/src/App.tsx` 的 `/credits/:id/apply` 使用 `ActionForm` 输入目标帐单 ID 与正整数金额；`api/admin/session.go`、`api/admin/commands.go` 接入管理命令。 |
| 分派、领域 helper | `lab/admin_previews.go` 分派 C12 到 `adminCreateApplyCreditPreview`；`lab/admin_commands.go` 分派 C12 到 `executeApplyCreditTx`，再于同一 SQLite 交易内调用 `applyCreditTx`。成功写入 `credit_applications`、取消目标帐单尚未送出的旧收款操作及 outbox，剩余应付需另创建正确金额的付款。 |
| 来源守卫、预览 policy | R 类预览绑定 actor、动作、grant、payload、期限及单次 claim。`loadApplyCreditSnapshot` 拒绝不同客户、原来源帐单、非续期帐期、超过宽限、已送出／未知付款、货币不符或超额抵扣；来源 JSON 绑定 grant 各余额、目标帐单未清额、帐期／到期、既有付款操作状态及货币。运行交易内重算并比对来源，变动回 `PREVIEW_STALE`。 |
| Permission | `api/admin/permissions.go` 映射 `finance.adjust`；共用 HTTP 与权限矩阵验无 session、缺 CSRF、缺能力及未知字段的入场守卫。 |
| 收据、恢复 | 抵扣、旧付款取消与成功收据在同一命令交易；失效命令无抵扣及成功收据，原键重播保持 `failed/PREVIEW_STALE`。UI 重新显示来源／影响差异并要求新预览与再次确认；不存在自动创建剩余付款的隐含动作。 |
| 实测 | `lab/admin_apply_credit_test.go` 验成功收据、来源变动、原键重播及只收剩余额；`lab/admin_refund_budget_parity_test.go` 验 C12 与 C15 共用 funded grant 预算；`web/admin/e2e/admin.spec.ts` 验双分页退款先占额使旧抵扣预览 409、重新确认后仅抵扣一次，及旧 2000 收款被取消、仅新 1500 收款送往 provider。 |

## C15 预留退款的端到端追踪

| 验收字段 | 已核对的路径与证据 |
| --- | --- |
| Endpoint、UI、handler | `POST /admin/api/previews` 与 `POST /admin/api/commands` 使用 `action_id=C15`；`GET /admin/api/commands/{id}` 找回原命令。`web/admin/src/App.tsx` 的 `/credits/:id/refunds/new` 使用 `ActionForm`，要求正整数最小货币单位、预览与二次确认；`api/admin/session.go`、`api/admin/commands.go` 进入 `lab.AdminSubmitCommand`。 |
| 分派、领域 helper | `lab/admin_previews.go` 分派 C15 预览到 `lab/admin_reserve_refund.go`；`lab/admin_commands.go` 分派 C15 运行到 `executeReserveRefundTx`，再于同一 SQLite 交易内调用 `reserveRefundTx`。领域函数拒绝超出 funded grant 可用余额的预留，并留下退款操作与 audit event。 |
| 来源守卫、预览 policy | R 类预览绑定 actor、动作、grant、payload、期限及单次 claim。`loadReserveRefundSnapshot` 记录来源付款操作／帐单、付款成功状态、授予／已用／预留／已退／可用额度及货币；运行交易内重算并比对完整来源 JSON。即使另一笔 500 预留后还剩 500 可用，旧 500 预览仍回 `PREVIEW_STALE`。 |
| Permission | `api/admin/permissions.go` 映射 `finance.adjust`；`api/admin/action_http_matrix_test.go` 与 `permissions_test.go` 验未登录、缺 CSRF、缺能力、未知 payload 字段及权限映射。这些 HTTP 测试只证明入场守卫。 |
| 收据、恢复 | 成功退款预留与 `admin_command_receipts` 在同一交易提交；预留本身不调用 provider，后续送出与查证属 C16／C17。失效命令保留 `failed/PREVIEW_STALE` 且无成功收据，原幂等键重播仍返回原失败命令；新预览、新键、再次确认才可创建第二笔。`ActionForm` 显示来源与金额变化并要求再确认。 |
| 实测 | `lab/admin_reserve_refund_test.go` 验成功收据、来源变动后失效、成功与失败键重播、零额外退款及重新确认后总额；`lab/admin_refund_budget_parity_test.go` 与直接领域路径比对；`web/admin/e2e/admin.spec.ts` 验真实 SQLite 双分页 409、重建预览、两笔退款合计 1000 与两笔收据。 |

历史 checkpoint（已由上方最终本机验收取代）：C15 的预览竞争与本地交易收据已有直接证据。A30 仍须按相同粒度核对其余 C01–C49，并完成 A01–A29 剩余条件；不能由 C15 的结果推论其他动作已签核。

## C16 指定退款操作派送的端到端追踪

| 验收字段 | 目前可重跑的证据 |
| --- | --- |
| Action／UI route | C16；`/refunds/:id/dispatch`。预览与命令由共用 API 提交，payload 为 `{}`，目标 refund ID 必填。 |
| HTTP handler／授权 | `api/admin/previews.go` 的 `createPreview` 与 `api/admin/commands.go` 的 `submitCommand` 由 `protectedWrite` 保护；`api/admin/permissions.go` 要求 `finance.adjust`。共用 HTTP 矩阵逐动作验 session、CSRF、缺能力和未知字段。 |
| 预览／来源守卫 | `lab/admin_external.go` 的 `loadExternalDispatchSnapshot` 仅接受 `created`、pending refund outbox 及正金额，绑定 grant、退款 provider key、原收款 provider key、金额、币别和状态；`adminExternalPreviewCurrent` 在首次派送前重验，来源变动时不得沿旧预览运行。 |
| Domain helper／外部效果 | `adminExecuteExternalCommand` 将固定 refund ID 传给 `dispatchRefundAt`，在服务商调用前以 `adminMarkSubmitted` 持久化 submitted；结果未知时保留 `waiting_verification`。`applyRefundObservationAt` 交易先预留 SQLite writer，再读取退款／inbox 状态并提交服务商证据。 |
| 收据／恢复 | 终态由 `adminFinishExternalCommand` 写入唯一成功收据；待查证不算已完成。`admin_external_test.go` 的 `TestAdminRefundDispatchTargetsExactRefund` 验指定 ID 而非下一笔队列项目；退款 UNKNOWN 的 C17 查证及原 C16 恢复另有 `admin_refund_reconcile_test.go` 与浏览器案例。 |
| 跨实例金融证据 | `TestAdminRefundReservationAndDispatchCompeteAcrossLabInstances` 以两个 Lab 共用 SQLite 同时运行 C15 与 C16。修正前重现 C16 `database is locked`；修正后连跑 5 轮共 30 个子案例及定向 `-race -count=2` 共 12 个案例通过。最新退款程序另以浏览器 `funded reduction reserves and refunds exactly once after a lost response` 定向 1／1 验证。每轮原退款仅一笔 USD 400 provider 事实；第二笔预留依来源先后成功或 `PREVIEW_STALE`，funded 额度与收据数一致。 |
| 当时未签核 | A16 其他退款 UI 错误矩阵及 A30 其余动作仍需逐项证据。 |

## C15 收据写入故障的 HTTP 恢复

历史 checkpoint（已由上方最终本机验收取代）：`api/admin/write_failure_test.go` 的 `TestRefundReservationReceiptFailureRetainsBudgetAndOriginalHTTPCommand` 在 C15 命令受理后对收据 INSERT 注入 SQLite 故障。首次 HTTP 回 503 `COMMAND_PENDING_RETRY` 与原命令 ID；Credit 仍有 1000 可用，退款预留、退款操作、成功收据与 provider 退款皆为零。解除故障后同键重送两次，两次均返回同一成功命令；Credit 恰预留 500，退款操作维持 `created`、收据一笔、provider 退款仍为零。这与 `lab/admin_reserve_refund_test.go` 的交易回滚测试合并覆盖 domain 与 HTTP 边界。

## C17 原退款操作查证的端到端追踪

| 验收字段 | 目前可重跑的证据 |
| --- | --- |
| Action／UI route | C17；`/refunds/:id/reconcile`，目标是原 refund ID。payload 为 `{}`，依 `lab/admin_commands.go` 不需要预览。 |
| HTTP handler／授权 | 共用 `submitCommand`、命令详情与原命令 resume 路由；`api/admin/permissions.go` 要求 `finance.adjust`。`api/admin/action_http_matrix_test.go` 验 C17 的 session、CSRF、能力与未知字段。 |
| 来源与查证 | `lab/admin_external.go` 调用 `ReconcileRefund(ctx, targetID)`；`lab/refunds.go` 以原退款 provider key 查找服务商，找到匹配的金额、币别及原收款 key 后才由 `applyRefundObservationAt` 入库。无终态证据时维持 `waiting_verification`；C17 只查证，不添加第二笔 provider refund。 |
| 收据／恢复 | `adminFinishExternalCommand` 仅在终态产生单一成功收据。`TestAdminReconcileRefundWaitsForTerminalProviderEvidence` 验无证据时没有成功收据；`TestAdminRefundReconcileResolvesUnknownWithoutSecondRefund` 验 UNKNOWN 查证、原键重播及唯一 provider 退款。 |
| Browser／SQLite | `web/admin/e2e/admin.spec.ts` 的 `funded reduction reserves and refunds exactly once after a lost response` 从原 C16 `waiting_verification` 进入 C17，确认退款成功后再恢复原 C16；SQLite 核对 provider 只有一笔退款、查证与派送命令各一笔收据。 |
| 当时未签核 | A30 其他动作仍需同粒度的来源守卫、HTTP 与浏览器证据。 |

历史 checkpoint（已由上方最终本机验收取代）：`api/admin/provider_lookup_failure_test.go` 的 `TestUnavailableRefundLookupKeepsReservationAndOriginalHTTPCommand` 补真 HTTP C17：服务商数据库被锁住时回 503 `COMMAND_PENDING_RETRY` 与原命令 ID；退款仍为 `unknown`，Credit 的 500 预留保持、已退金额为零，没有成功收据。解除故障后原键重送两次，均回同一成功命令；预留转为已退 500，Credit 可用额仍为 500，provider 只有一笔退款、命令只有一笔收据。定向测试连跑三次通过。这补足 provider 暂不可查时 HTTP 回应与资金保留的一致性；其他 C17 故障组合仍待逐项验收。

## A16 Credit 额度查找与退款 UI 证据

- `GET /admin/api/credits/{id}` 由 session 守卫；`lab.AdminCreditDetail` 在同一个 SQLite 唯读快照内取得 grant 来源与 `CreditBalance`。`api/admin/credit_detail_test.go` 验未登录 401 与不存在 404；`lab/admin_credit_detail_test.go` 验原始、已抵扣、保留、已退款与可用额度的转换，包含退款回应遗失时仍保留原额。
历史 checkpoint（已由上方最终本机验收取代）：- `/admin/credits/:id` 用 Ant Design 将五种额度分开显示并提供来源帐单、抵扣、预留与按 grant 筛选的退款入口；定向浏览器案例验从列表进入、预留后及成功退款后的金额。另一个案例先创建两笔 pending 退款，再从 C16 接口指定较晚的操作；较早者仍为 `created`、outbox `pending`、provider 零退款，指定者只退一笔且原收款 key 与金额正确。两个新案例各自定向 Playwright 1／1 通过。
历史 checkpoint（已由上方最终本机验收取代）：- 完整浏览器回归及 A30 其他动作的同粒度盘点仍待完成。

## C11、C13、C14 更正来源验收补充

帐单详情从既有 request key 与数据列关联出更正来源。`admin_reduction_test.go` 验 C11 更正指向原管理命令；`admin_change_corrections_test.go` 验 C13 更正指向立即升级变更；`admin_unfulfilled_test.go` 验 C14 更正指向未履行升级变更。C11、C13、C14 的同 key 重播测试核对更正不重复，C11／C14 的 Credit 各仍一笔，C13 不纳入后来合格的项目。Playwright 另验 C11 原命令入口、C13 延迟更正的变更 ID 与金额，以及 C14 跨帐期后查证、决议、帐单更正与来源 Credit 的详情及历史页。无法验证关联的更正标为其他领域操作并保留原 request key，不推定为管理命令。

`TestAdminReductionCancelsOldCollectionAndCapturesOnlyRemainder` 与新 Playwright 案例另验未收款的第一期帐单：C11 预览列出将取消的旧付款操作与减额后应付 1500；减额 500 后，原 2000 的付款与 outbox 取消，未产生可退 Credit。操作员由 C07 创建 1500 新付款、C09 派送后，fake provider 只有新 key 的 1500 capture，原 key 没有收款，订阅开通且帐单余额归零。C11／C12 的收款安排提示共用同一接口组件；两项浏览器案例已分别定向通过。

## C13、C14、C30、C32、C44、C45 回应恢复

批量回应遗失的可重跑证据位于 `web/admin/e2e/admin.spec.ts`：C13、C30、C32、C44、C45 都先让原命令在 SQLite 完成，再中断浏览器回应，重新加载后沿原 request key 重播。测试逐项核对工作、成员及各自的财务结果；C13 检查帐单更正，C30 检查 Credit Note，C32 检查 capture outbox，C44 检查续约帐单与付款义务，C45 检查固定的权益刷新成员。这只签核该种回应遗失与重播情境。

C14 另有同类型的非批量浏览器案例：未履行升级决议创建更正与 Credit 后中断回应，页面重新加载并沿原 key 查回，命令、收据、决议与 Credit 各只有一笔。此案例定向通过，未纳入先前 63 项完整回归。

Go 故障注入另覆盖两种不同的收据失败边界：`TestAdminResolveUnfulfilledRollsBackFinancialFactsWhenReceiptFails` 验 C14 的财务数据与收据整体回滚，重启后才一次完成；`TestAdminChangeCorrectionBatchResumesCommittedItemAfterReceiptFailure` 验 C13 已提交的逐项更正在最终收据失败时保留，重启后只完成原工作与收据。两者都核对 Credit／更正不重复，provider capture 数不增加。

## C12 浏览器验收补充

历史 checkpoint（已由上方最终本机验收取代）：`web/admin/e2e/admin.spec.ts` 的 C12 案例以已收款帐单减额产生 1000 的 Credit，设置实验时钟并由续期批量创建同客户的下一帐期帐单。操作员从抵扣页输入目标帐单与 500 金额，检查预览后确认，重新加载命令详情仍可查到成功结果。隔离 SQLite 核对抵扣来源、目标帐单、`admin:` request key、唯一抵扣纪录及一笔命令收据；帐单详情显示已抵扣 500 与剩余应付。超过可用 1000 额度的预览被拒绝，且未创建 C12 命令。`lab/admin_apply_credit_test.go` 另验预览后退款预留改变额度来源时，C12 命令以 `PREVIEW_STALE` 失败，不添加抵扣或收据。不同客户、过期帐期与并发争用的浏览器案例仍待验。

同档的 `TestAdminApplyCreditCancelsOldCollectionAndCapturesOnlyRemainder` 延伸验证抵扣后的收款：原续期帐单为 2000，抵扣 500 时旧 `created` 付款操作与 capture outbox 一同取消，旧 provider key 没有 capture；再由 C07 创建 1500 的新付款操作，只向 fake provider capture 1500，帐单余额归零。这证明该路径不会把抵扣前的 2000 送去收款，但不代表其他并行情境已全部验毕。

历史 checkpoint（已由上方最终本机验收取代）：同一条路径现也由 Playwright 验证：C12 预览明示取消一笔未送出的付款操作、抵扣后应付金额及创建新付款的提示；操作后沿 UI 进入 C07 创建剩余付款，再用 C09 派送。SQLite 与 fake provider 分别核对旧操作已取消、旧 provider key 没有 capture、新 key 只 capture 剩余 1500，帐单详情显示 USD 0.00 尚待支付。此路径已纳入最近一次 70 项完整浏览器回归。

C12 的浏览器案例进一步在预览后，于同一登录 session 的另一分页运行 C15 预留退款 500。旧 C12 预览确认回 409 `PREVIEW_STALE`，没有抵扣或命令收据；接口显示 `grant_reserved_minor` 来源变动，保留原 500 意图并要求再次确认。新预览确认后，C15 预留 500 加 C12 抵扣 500 正好等于原 Credit 1000，两笔用途没有超额或重复。此情境定向 1／1 通过，并已纳入最近一次 76／76 完整浏览器回归。

`TestAdminApplyCreditUsesGrantAndInvoiceBalanceAtomically` 另核对原 C12 失败命令沿同一 request key 重播仍是同一笔 `PREVIEW_STALE`，且不会写入新的抵扣或收据。

## C45 批量成员浏览器验收补充

历史 checkpoint（已由上方最终本机验收取代）：`web/admin/e2e/admin.spec.ts` 的 C45 案例先创建已付款订阅，通过 UI 创建权益刷新预览并读取其固定成员快照，再于另一个分页创建新的合格订阅。确认原预览后，SQLite 工作项目数与快照相同，包含原订阅而不包含新订阅；工作详情页在刷新后仍显示完整进度。另一个页面案例以受控 API 回应显示一笔成功及一笔待查证，确认进度为 1/2、两种状态分列，且逐项错误码可见。此案例只验 C45 的成员固定性及批量页面的状态呈现，尚未覆盖五种批量的逐项中断与重启 UI 矩阵。

## C34 浏览器验收补充

历史 checkpoint（已由上方最终本机验收取代）：`web/admin/e2e/admin.spec.ts` 的缺失权益投影案例，使用本机假服务商与隔离 SQLite 创建已付款订阅，故意移除权益投影，再走 C33 对帐及 C34 详情页、预览、确认、刷新。测试核对 `SAFE_AUTO_REPAIR`、来源 revision 与证据预填、唯一 `repair_operations` 与命令收据、原 expected／actual 快照，以及修复后 `verified` 与后续对帐的 verification。另有来源 revision 在预览后改变的 browser 案例：修复结果为 `blocked`、权益未重建、命令只有一笔收据，UI 明示 blocked verification。服务商收款金额不符的 browser 案例另验 `MANUAL_REVIEW`：C34 被 blocked、C35 留下人工决议，provider 原金额与收款总笔数不变。其他分类及 blocked 原因仍待逐项 browser 验收。

## C46–C49 实验控制的现有证据

| 动作 | 已验证的行为 | 尚需补齐 |
| --- | --- | --- |
| C46 | `TestAdminClockPersistsAndControlsRunningDomainClock` 验时钟设置、重启加载、revision 使旧预览失效；浏览器案例验 C09 等待查证期间前进一天不会自动重收款，原命令仍使用受理时的业务时间。 | 与其他进行中命令交错、各种时间边界及错误呈现的逐项矩阵。 |
| C47 | `TestAdminPaymentDecisionRecoversAfterProviderCommitAndCapture` 验 provider 决策先提交、付款后续完成、重启沿原控制收据完成管理命令，capture 与收据各唯一；`TestAdminConflictingProviderDecisionsPreserveFirstOutcome` 验相反结果命令失败、第一个决策仍造成唯一成功收款、终态后新命令亦失败；浏览器显示第二个决策 `DOMAIN_REJECTED`、仅首命令有收据，并走过确定失败后重试。 | 权限撤销交错与其余 UI 错误呈现矩阵。 |
| C48 | `TestAdminRefundDecisionRecoversAfterProviderCommitAndDispatch` 验 provider 决策先提交、退款后续派送、重启沿原控制收据完成管理命令；`TestAdminConflictingProviderDecisionsPreserveFirstOutcome` 验相反结果命令失败、第一个决策仍造成唯一成功退款、终态后新命令亦失败；浏览器显示第二个决策 `DOMAIN_REJECTED`、仅首命令有收据且派送前无 provider 退款，另验回应遗失后 C17 查证与唯一退款。 | 权限撤销交错与其余 UI 错误呈现矩阵。 |
| C49 | `TestAdminFaultTicketOnlyAffectsItsSelectedPayment` 验指定付款票据不被另一笔付款误领；`TestAdminDuplicateFaultTicketFailsWithoutStrandingCommand` 验第二张票据明确失败、原键重播与第一张票据仍可派送；`TestAdminClaimedFaultSurvivesCrashBeforeProviderDispatch` 在付款及退款各固定领票后、provider 调用前的持久化状态，重启后原命令仍进入待查证，查证后 provider 事实与命令收据各唯一。`TestAdminRevokedDispatchReleasesClaimedFaultBeforeProviderCall` 与 `TestAdminRevokedRefundDispatchRetainsReservationAndFault` 验领票后、provider 调用前撤销原命令，不产生外部效果或成功收据；票据释放后由新命令领取，退款预留额保持 500。`TestAdminClaimedFaultBlocksCompetingDispatchUntilOwnerResumes` 验同一付款的第二个命令不得绕过已绑定票据直接收款，启动恢复先略过竞争命令、让原命令查证，两命令最后共用唯一 capture；`TestAdminStalePreviewReleasesClaimedFault` 验旧来源快照失效后释票、零收款与新命令可再领票。浏览器从建票页显示第二张失败与 `DOMAIN_REJECTED`，确认第一张仍可派送，另验付款及退款的 `lost_response`／`crash_after_provider` 恢复。 | 真正跨进程同时接管及不同故障模式的完整组合。 |

历史 checkpoint（已由上方最终本机验收取代）：这些案例是 C46–C49 的定向证据；A25 与 A30 仍需完成上列缺口和共通矩阵，不能视为四个动作已全面签核。

## 证据范围与限制

上方逐动作完整记录取代先前泛称的额外矩阵缺口。每个动作均有真实请求与成功收据配对、原键重播、不同 payload 冲突、共用 HTTP 入场证据，以及适用 helper／来源／收据契约。未使用的金融字段与 N 类预览不适用；单一 local-admin 的跨 actor 浏览器操作不适用，领域隔离另有实证。

证据限于列出的 domain fixture、共用机制与本机版本。UI 受控响应证明呈现／恢复，provider 与金融效果由链接的真实 fixture 证明；完整套件与后加补充断言的指纹分开。首次 process 就绪超时根因未确认，诊断重复与最终完整 Go 回归通过。不推论任意故障顺序或正式 provider 行为。

## request ID 稽核关联补验（2026-09-28）

`web/admin/e2e/admin.spec.ts` 真登录案例先核对未登录 401 JSON 的 `request_id` 等于 `X-Request-ID`，且不采用客户端伪造的 header；再创建 C01 报价，核对回应 header、`admin_commands.request_id`、受理与成功两笔 `admin_audit.request_id` 及命令详情页显示的值一致。原有秘密排除检查仍覆盖稽核文本中的密码、内部 token、CSRF 与 session cookie。v8 升级保留旧列的 `NULL` 请求 ID，避免为历史请求虚构来源。

`api/admin/route_errors_test.go` 补真 HTTP 路由边界：未知 API 路径在登录前先回 401，登录后回 JSON 404；已知路径的不支持方法回 JSON 405 与 `Allow`。每笔错误 body 的 `request_id` 等于 header，且新请求有不同 ID。这避免纯文本路由器缺省错误绕过管理 API 的 session 与错误契约。

## C01 写入故障与回应遗失补验（2026-09-28）

`api/admin/write_failure_test.go` 先对命令 INSERT 注入故障，验证入场失败回 500 `COMMAND_ADMISSION_UNKNOWN`（`retryable: true`，须沿用原键）、没有命令／报价／收据。解除故障后原键首次提交回 202，再重播回 200，且只有一笔命令、报价和收据。

历史 checkpoint（已由上方最终本机验收取代）：`api/admin/write_failure_test.go` 对收据 INSERT 注入持久 SQLite 故障，验证命令已受理但财务事实回滚时回 503 `COMMAND_PENDING_RETRY`，并提供原命令 ID。解除故障后，同键重播两次只创建一笔报价与一笔成功收据。

历史 checkpoint（已由上方最终本机验收取代）：`api/admin/client_disconnect_test.go` 使用真 HTTP 连接在命令成功后丢弃首个回应。重送前已存在成功命令、报价与收据；同键重送两次后仍各一笔，且返回同一命令。另一案例等待 C01 命令受理与运行 lease 取得，在报价写入受 SQL trigger 延迟的测试环境中取消客户端请求；服务器结束后原命令保留为 `accepted`、lease 释放、报价及收据为零。同键重送两次后仅创建一笔报价及收据。后一案例连续运行 20 次通过；其他动作的运行中断线与 DB 故障组合仍待验。

`web/admin/e2e/admin.spec.ts` 另以受控 500 `COMMAND_ADMISSION_UNKNOWN` 验证 C01 首次入场不确定时，重新加载后仍保留原 payload 与幂等键；再次送出才创建一笔成功命令、一笔报价与一笔收据。这补足了受理前 DB 失败到 UI 恢复的交界，但没有把受控 HTTP 错误当成真实 SQLite 故障的浏览器注入。

## C18–C21 发布链的成功收据补验（2026-09-28）

历史 checkpoint（已由上方最终本机验收取代）：`web/admin/e2e/admin.spec.ts` 的「catalog publishes immutable price and meter versions before cohort selection」案例已在真实浏览器逐一运行 C18（Pro 价格）、C19（计量表）、C20（两个独立计量价格版本）与 C21（cohort 选价）。每一个来源 ID／方案各以 SQLite 关联 `admin_commands` 与 `admin_command_receipts` 核对唯一成功收据；同版号与已选价冲突的额外预览仍在入场前被拒绝，没有增加命令。定向 Playwright 1／1 通过；这补成功链的收据证据，不取代其他输入、重播及恢复矩阵。


## C02 来源读取失败时的原键恢复（2026-09-28）

`web/admin/e2e/admin.spec.ts` 的真实 C02 案例先接受报价并让成功 POST 回应遗失，再令报价详情 GET 回 503；页面重整后仍可按原 idempotency key 查回同一命令，命令页显示成功。SQLite 直接核对 C02 命令、订阅与帐单各一笔。其余 C03／C04／C05／C06／C23 的来源故障入口与键／动作接线由 `web/admin/e2e/source-read-recovery.spec.ts` 受控接口案例验证；C03 首次查找回 503 后再次使用同一键。该案例不作财务收据宣称。


## C09／C16 命令完成与外部操作失败的 UI 区分（2026-09-28）

`adminFinishExternalCommand` 在服务商提供确定失败的终态证据后，可将派送命令记为 `succeeded`，同时将 `operation_status=definitively_failed` 写入结果参照。两者分属命令流程与外部金流结果。React 操作结果及命令各入口现额外标示付款或退款确定失败。真实 C16 浏览器案例核对 Credit 保留额释放、可用额恢复，以及操作结果、命令详情、列表、抽屉的警告；真实 C09 案例核对付款操作失败警告。

## C02 收据故障后沿原 HTTP 命令恢复（2026-09-28）

历史 checkpoint（已由上方最终本机验收取代）：`api/admin/quote_acceptance_receipt_failure_test.go` 在隔离 SQLite 以真实管理 HTTP handler 提交 C02。收据 INSERT 故障时回 503 `COMMAND_PENDING_RETRY` 与原命令 ID，订阅、帐单、帐期、付款义务、outbox 和成功收据均未提交。解除故障后同键重送两次，两次都取得同一成功命令；上述财务事实与收据各一笔，金额和来源 ID 一致，provider capture 仍为零。同键提交另一张有效报价回 409 `IDEMPOTENCY_CONFLICT`，第二张报价没有创建订阅。这补足原有领域层收据回滚测试的 HTTP 回应、幂等恢复与键冲突证据，不取代其他 C02 故障点矩阵。

## C12 收据故障与提交后回应遗失（2026-09-28）

`api/admin/credit_apply_receipt_failure_test.go` 以来源已付款帐单的 funded Credit 抵扣同户下期帐单。收据 INSERT 失败时，HTTP 回 503 与原命令 ID，抵扣和旧付款取消均回滚；同键恢复后只抵扣 500 一次，旧付款取消且 outbox 设为 `done`，余额须另创建新付款操作。另一笔 250 抵扣在成功提交后遗失首个 HTTP 回应；重送两次都回同一成功命令，两笔抵扣合计 750，Credit 可用 250，目标未清 1250，收据与 provider capture 没有重复。这补足 C12 的 HTTP 回应／收据边界证据，不代替其余动作的故障矩阵。

## C11 已收款减额的 HTTP 收据故障（2026-09-28）

`api/admin/reduction_receipt_failure_test.go` 在隔离 SQLite 对已实收帐单提交 C11，注入收据 INSERT 故障时 HTTP 回 503 与原命令 ID，减额、funded Credit、原收款发布和成功收据全部回滚；原帐单与 provider capture 保持不变。同键恢复后只有一笔 1000 correction、grant、release 与收据，净实收等于修正后义务。另一笔有效减额若沿用相同键则回 409，没有第二次财务效果。既有浏览器案例另验提交后回应遗失的 C11 原键恢复。

## 专用操作页命令指针恢复（2026-09-28）

历史 checkpoint（已由上方最终本机验收取代）：C01、C02、C03／C04、C05／C06、C07、C08、C23、C26 的专用页改以 actor、动作及目标为键保存最近命令 ID；共用 `ActionForm` 亦使用同一机制。这让重新加载页面后可沿原命令 ID 查找状态，不以本地状态宣称财务操作成功。C01 与 C05 的 Playwright 情境直接验重整后仍可读原命令；C05 的下一步需明确清除本页指针，重新读取订阅后才进入 C06。其余专用页目前是程序接线与建置证据，尚不能由 C01／C05 案例推论所有动作的回应遗失及 404／403 读取矩阵已签核。

历史 checkpoint（已由上方最终本机验收取代）：2026-09-28 完整回归：Playwright 109／109 通过，React 建置及 Go 全套测试通过。此结果验证现有测试涵盖的路径；逐动作尚列「部分」的权限、故障与可观测性矩阵仍待逐项签核。
