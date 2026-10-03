# Web Admin 工程契约

[English](06-web-admin-contracts.md) | [繁體中文](06-web-admin-contracts.zh-TW.md) | **简体中文**


状态：截至 2026-10-03，Web Admin 实作与 A01–A30 本机验收完成。原设计范围不变，目前证据与限制以 [实作记录](../implementation/web-admin.zh-CN.md) 为准。

## 1. 确定的架构

- 前端：React、TypeScript、Vite、React Router、TanStack Query、Ant Design 与 Zod。Ant Design Form 负责交互表单和即时字段提示；Zod 验证 API DTO 与金额、日期等输入格式。采 client rendering，不引入 SSR 或第二个业务后端。UI 不混用 React Hook Form、Radix UI、Refine 或另一套组件框架。
- 套件管理：pnpm；实作时选择彼此兼容的稳定版本并提交 lockfile。规划阶段不安装套件，不虚构尚未解析的版本号。
- 前端位置：`web/admin/`；Go 管理 HTTP：`api/admin/`；查找、命令交易与迁移仍在 `lab/`，方便重用既有未导出的交易 helper，避免第二套金额逻辑。
- 视觉与交互基准：以 Ant Design 的 ConfigProvider／App 统一主题 token、间距、字体、locale 与消息容器；页面使用 Layout、Menu、Table、Form、Descriptions、Alert、Modal、Drawer、Result、Empty 等既有组件。业务组件 DataTable、Money、StatusBadge、ObjectLink、RevisionNotice、Timeline、CommandProgress、PreviewDiff、ConfirmAction、FieldError、EmptyState、ErrorState 在其上封装领域语意；只用少量局部 CSS 处理布局与金额对齐。状态同时呈现文本与图标，不单靠颜色。
- 金融输入与确认：金额和高精度费率使用字符串输入与明确格式解析，不通过 JavaScript number 或 Ant Design InputNumber 决定金融值。ConfirmAction 必须显示服务器预览、版本与影响范围，依命令契约送出并可恢复进度；Ant Design Modal／Popconfirm 只提供交互外壳，不承担授权、幂等或金融正确性。
- 添加启动入口：`lab admin commerce.db provider.db [127.0.0.1:8080]`。这个 server 只挂载管理静态页、session 与管理 API；**不挂载目前未具完整授权的 v1 写入路由**。保留既有 `lab serve` 作独立的本机 API 实验入口。
- 同源管理 API 前缀 `/admin/api`；页面前缀 `/admin`；支持明确 IPv4／IPv6 loopback。CLI、v1、admin 最终重用同一领域检查，但本机文件拥有者不受 Web RBAC 隔离；不把本机 SQLite 当多租户安全边界。
- 发布 build 使用 `adminui` build tag 嵌入前端资产；普通 `go test ./...`／纯 API build 不需要 dist。没有内嵌资产的 binary 运行 `admin` 时明确指出缺少建置步骤。不得静默回传空页。
- 开发 Vite 仅绑 loopback，proxy 管理 API；只有明确设置的开发 origin 可使用。session 与 Origin 检查仍生效。

前端不直接调用 `State()`、不存内部 API token、不计算收款金额、不把过期预览当成授权依据。

## 2. 页面契约与 frontend 边界

| route（省略 `/admin`） | 主数据／主要操作 | 关联面板 |
| --- | --- | --- |
| `/` | 营运待办与数据观测时间 | 筛选后的列表链接 |
| `/customers`、`/customers/:id` | 客户汇整、订阅列表、新购 | 帐单、credit、legacy 映射 |
| `/quotes`、`/quotes/:id` | 既有报价、组件、用途、期限与 fingerprint；接受或创建新报价 | 客户、绑定订阅、必要 client 能力 |
| `/subscriptions`、`/subscriptions/:id` | 实际与下期价格、席次、revision；报价／升降级／取消／恢复 | 帐期、付款、权益来源、时间轴 |
| `/invoices`、`/invoices/:id`、`/credits` | 帐单来源、credit 配额；更正／应用／补偿 | lines、allocations、退款来源 |
| `/payments`、`/payments/:id`、`/refunds` | 义务、尝试、provider 观测；送出／查证 | 命令与原始资金来源 |
| `/catalog/prices`、`/catalog/prices/:id`、`/catalog/meters` | 版本比较、发布、meter 注册、cohort 选价 | checksum、生效范围 |
| `/price-migrations`、`/price-migrations/:id` | 迁移预览、逐户进度；暂停／略过／恢复 | 新旧 assignment 与冲突 |
| `/usage`、`/usage/:subscriptionId/:periodIndex` | 事件、rating、晚到差额；关帐／重算／credit note | 撤销链、invoice line |
| `/contracts`、`/contracts/:id` | 合约条款、Net30、后续价；发布／报价／到期收款 | 对应订阅与 invoice |
| `/reconciliation`、`/discrepancies/:id` | runs、expected/actual、证据；修复／人工决议 | 修复前后 revision／run |
| `/account-migrations`、`/account-migrations/:legacyId` | 映射、shadow、回填、readiness、owners；切换／停止 | legacy provenance、adapter view |
| `/commands`、`/commands/:id`、`/jobs/:id` | 命令／job 状态、逐项结果 | 所产生的领域对象 |
| `/lab` | 仿真时钟、fake provider 及支持的故障 | 被影响的命令／操作 |

前端数据以 resource＋ID＋filters＋cursor 作 Query key，mutation 完成后失效相依查找；金融写入不做 optimistic success。列表搜索 debounce 250ms，URL 保存筛选，limit 缺省 25、最大 100。可见命令页以 2 秒开始轮询、无变化退避至 10 秒；离页停止轮询，回页重新验证。刷新失败时保留旧数据并显示「过期／上次更新时间」。

缓存旧数据仅可用于暂时性读取故障（网络、408、429、5xx），必须标示上次成功读取时间并停用依赖该状态的写入入口；401／403／404 需隐藏已缓存的敏感内容并显示对应状态。UI state 至少覆盖 loading、empty、loaded、stale、forbidden、not-found、error、submitting、waiting-verification。表单 double-click 防护只是 UX，真正幂等由服务器负责。只在同一未完成意图中保留 request key；修改 payload 或重新确认后是新意图。

`next_actions` 回传可用 action、permission 与 blocked reason code，供 UI 显示说明；运行时仍重新核对。页面不得因权益 active 就把 UNKNOWN 付款隐藏。

## 3. Session 与权限

用户已指定采用 `.env` 的单一管理帐号密码，取代一次性 bootstrap。添加 `/admin/login`，包含帐号、密码、登录按钮、送出中与验证失败状态。帐号使用 autocomplete=username，密码使用 type=password／autocomplete=current-password；支持 Enter 送出，登录成功导回同站原页面，禁止外部 redirect URL。

设置键为 `BILLFORGE_ADMIN_USERNAME`、`BILLFORGE_ADMIN_PASSWORD`，可选的 `BILLFORGE_ADMIN_CAPABILITIES` 用于收紧能力且须包含 `read`。添加 `lab admin --env-file .env commerce.db provider.db [127.0.0.1:8080]`；缺省读目前工作目录的 `.env`，已明确设置的 process environment 同名键优先。使用 `godotenv` 的 map 解析器保留 quoted value／特殊字符，不将整份文件逐项列入 log。帐号 1–64 字、不可含控制字符；密码 12–72 UTF-8 bytes（bcrypt 的明确输入限制），建议至少 16 字符。缺少、空白或格式错误时启动失败，不提供缺省帐密。

Go 启动时以 `golang.org/x/crypto/bcrypt` cost 12 产生内存中的 salted hash，登录时验证 hash；不把 plaintext 写进 SQLite 或任何 response。`.env` 本身仍保存用户指定的密码，因此实作要忽略 `.env`／`.env.*`，只允许提交不含有效凭证的 `.env.example`，并在操作文档说明本机文件权限。密码不能使用 `VITE_` 前缀、不能进入前端 build-time env、localStorage、URL 或 log。不能宣称 Go process memory 已安全抹除 plaintext。

session 闲置 30 分钟、最长 8 小时，注销撤销；重启 server 后 session 失效。更改 `.env` 的帐密须重启，会同时撤销既有 session。忘记密码由本机拥有者更新 `.env` 后重启；不添加注册、忘记密码邮件或帐号管理功能。此轮只有规划，不创建 `.env` 或任何实际帐密。

cookie：HttpOnly、SameSite=Strict、Path=/admin；正式 HTTPS 时 Secure。只接受已设置 Host，防止不预期 Host 指向本机服务。所有写入包含 session 绑定的 CSRF token 并核对 Origin，JSON content type，缺省 request body 最大 1 MiB；没有 CORS wildcard。登录也核对 Origin／Host。登录前 GET `/session/csrf` 取得与短期 pre-auth cookie 绑定的 nonce，POST `/session` 验证成功后轮换 session ID 与 CSRF token，防止沿用未登录 session。未知帐号也做 dummy bcrypt 验证，统一显示「帐号或密码错误」。每个来源／帐号组合 15 分钟内最多 5 次失败，并设每进程每分钟最多 20 次登录验证，单次只运行一个 bcrypt 验证；超限回 429 和 retry-after，不永久锁帐。这些本机限流状态在重启后重置。一般业务读取也需 session。

权限能力：`read`、`subscription.manage`、`finance.adjust`、`catalog.publish`、`migration.manage`、`reconciliation.repair`、`operations.run`、`usage.manage`、`contract.manage`、`lab.control`。目前单一 operator 拥有全部能力；测试 fixture 可配置不同 actor 与 capability，不开发帐号管理 UI。

本机 actor 使用稳定 `local-admin` ID，与随机 session ID 分离；重新登录及重启不改变命令的幂等 scope。worker 使用 server 端 capability registry，不能依赖已经过期的 cookie 保存授权。

每个 command admission 及 worker 运行前检查 capability；管理命令只允许原 actor 或相应操作权限者读取，`read` 能看到去敏的共享业务结果。撤销权限停止未运行命令；已提交的金融事实及恢复查证由系统继续完成，不能因 session 过期遗失义务。

## 4. 共用 API 契约

session 路由为 GET `/session/csrf`（登录前 nonce）、POST `/session`（username/password）、GET `/session`（目前身分与权限）、DELETE `/session`（注销，需 CSRF）。登录前只开放 login page、必要静态资产、nonce 与创建 session，其他 GET、POST 全部受管理 session 保护。金额与量值使用十进位整数字串，禁止小数、exponent、NaN、负值超出字段政策及超出 int64 范围。费率为 `rate_num`／`rate_den` 字符串，分母正值。revision 与写入命令的 period_index 也用十进位整数字串；页数／limit 是有界 JSON integer。时间为 RFC3339 UTC，允许 0–9 位小数秒且必须落在 int64 Unix nanoseconds 可表示的范围；超精度或超范围直接拒绝，不截断。currency 沿用 MVP 的 USD，不能靠前端传入新币别扩充政策。

读取 envelope：`{data, as_of, source_revision?, next_cursor?}`。固定排序 `(created_at,id)` 或资源指定的稳定键；cursor 绑定 filters 与排序、不提供任意 SQL sort。多页数据不是同一全域 snapshot；批量影响范围不能来自逐页浏览，必须由 preview/job snapshot 固定。

错误 envelope：`{error:{code,message,field_errors?,retryable,current_revision?},request_id,command_id?}`。400 格式；401 session；403 权限／CSRF；404 对象或未知 API 路径；405 API 方法不支持并回 `Allow`；409 状态、revision、key 或 preview 冲突；422 业务字段无效；429 有界节流；500/503 系统或暂时不可用。不回传 SQL、stack 或凭证。

`request_id` 由服务器逐 HTTP 请求产生，并同时放在 `X-Request-ID` 回应 header 与错误 JSON；不能接受客户端自行指定的 ID。命令保存受理请求 ID，后续稽核事件保存运行该阶段的请求 ID；背景恢复沿用原命令的受理 ID。旧数据在 v8 升级后仍为 `NULL`，不可补上虚构的历史来源。

命令受理时若无法确认保存结果，回 500 `COMMAND_ADMISSION_UNKNOWN`、`retryable: true`，不提供未确认的 `command_id`。客户端必须保留原 payload 和幂等键，重新登录后也以同一键查证或重送；不得因 500 产生新键。命令已受理但运行暂时失败时，回 503 `COMMAND_PENDING_RETRY` 并附原 `command_id`，后续查证与重试仍沿用原命令。

所有 command POST 都需 `Idempotency-Key`，金融与批量操作另需 `preview_id`；原因 `reason` 最长 500 字。actor、request ID、实际运行时间从服务器取得。拒绝未知字段。canonical payload hash 由已解析的 typed DTO 算出，范式数字／时间／默认值，包含 kind、target 与 preview ID。

所有 command 首次受理回 `202 {command_id,status,location}`；相同 key、相同 actor、相同 canonical payload 回相同 command（完成时可回 200），即使原 preview 已过期或 revision 已改变也优先回既有结果。不同 payload 回 `409 IDEMPOTENCY_CONFLICT`。不能先做依赖当前状态的预览计算，再查幂等纪录。

GET command 回 `status`、`result_refs`、`domain_outcome`、`error`、`created_at/updated_at`；capture 被 provider 明确拒绝可为命令已完成但 `domain_outcome=declined`，UI 仍显示付款失败。`failed` 表示管理命令本身未完成；已可能产生外部效果时先 `waiting_verification`。

### 4.1 读取资源

GET `/overview`；`/quotes`、`/quotes/{id}`；`/customers`、`/customers/{id}`；`/subscriptions`、`/subscriptions/{id}` 及其 `/periods`、`/timeline`、`/entitlement`；`/invoices`、`/invoices/{id}`；`/credits`、`/credits/{id}`；`/payments`、`/payments/{id}`；`/refunds`、`/refunds/{id}`；`/prices`、`/prices/{id}`；`/meters`；`/catalog-selections`；`/price-migrations`、`/price-migrations/{id}` 及 `/items`；`/usage-events`；`/usage-periods/{subscriptionId}/{periodIndex}`；`/contracts`、`/contracts/{id}`；`/reconciliation-runs`、`/reconciliation-runs/{id}`；`/discrepancies`、`/discrepancies/{id}`；`/account-migrations`、`/account-migrations/{legacyId}` 及 `/provenance`、`/shadows`、`/readiness`、`/entitlements/{subscriptionId}`；`/commands`、`/commands/{id}`；`/jobs`、`/jobs/{id}`；`/outbox`；`/lab/status`、`/lab/provider-captures`、`/lab/provider-refunds`（lab.control，分页）。

`GET /price-migrations/{id}` 回传批量字段及 `ItemCount`、`PendingCount`、`AppliedCount`、`ConflictedCount`、`SkippedCount`，不内嵌无界项目数组。`GET /price-migrations/{id}/items` 接受 `limit`（缺省 20，范围 1–100）、`status`（`pending`、`applied`、`conflicted`、`skipped`）与 `cursor`；回传 `items`、`next_cursor`、`observed_at`。光标绑定批量 ID 与状态筛选；状态变更后需从第一页重新查找。金额和计数皆以精确十进位字符串输出。

列表只允许该资源适用的 customer_id、subscription_id、status、time range、ID prefix 等白名单 filters。ready/readiness 只是带时间的读取观测；切换命令仍在交易内再验证。必要字段：

| DTO | 必要内容 |
| --- | --- |
| quote | customer、用途、price/contract 版本、组件、fingerprint、到期、required capabilities、due_now/estimated、recurring commitment、change binding |
| subscription | customer、actual price/checksum/seats、revision、period、scheduled intention、billing/payment/service 各自状态、entitlement source revision |
| invoice | original／obligation／allocated／outstanding minor、immutable lines、price/contract/usage references、更正与 credit applications |
| credit/refund | source invoice/correction、funded amount、available/applied/reserved/refunded、currency、来源 operation；未知保留不可算可用 |
| price | publication state、所有组件、meter、有效期间、checksum、cohort 使用情况；已发布唯读 |
| migration/job | frozen targets、counts 分类、逐项 revision／结果／blocked reason、pause scope；不把部分成功标全成功 |
| discrepancy | kind、expected、actual、evidence refs、source revision、run、repair/manual decisions、verification |
| account migration | legacy/customer/beneficiary、history、read/writer owner、stopped/reason、readiness 各门槛及证据时刻 |

金额字段语意直接沿用领域 balance struct；不得以「原始额－退款」自行拼出应付余额。来源缺漏显示 unknown，不补 0。

`GET /commands` 以 1–100 笔为一页（缺省 50），回传 `items` 与 `next_cursor`；cursor 使用命令写入顺序的 rowid，添加命令不会使已读页重复。可用 `GET /commands/{id}` 打开旧命令，列表页不是全域快照。无效 cursor 回 `INVALID_CURSOR`。

### 4.2 Preview

POST `/previews`：`{action_id,target_id?,payload,expected_revision?}`。action_id 仅可为下表枚举，通过 typed switch 验证，不使用反射调用函数。回 `preview_id,expires_at,source_versions,impact,blocking_reasons`；preview 5 分钟有效，且不超过引用 quote 的 expiry。GET `/previews/{id}` 仅原 actor 或相应能力可查看。

preview 不保留付款／退款额度、不调用 provider、不运行领域写入；唯一写入是预览纪录。其内容保存 canonical intent、来源版本、金额及逐项目标，确认命令从服务器纪录取得，不信任浏览器回传的总额。

金额敏感命令运行时在同一交易重新计算：退款、credit、更正必须符合确认的确切金额；期中升级允许按原政策重算后降低、不可超过确认上限；添加项目、价格、席次、帐期或生效范围改变一律 `409 PREVIEW_STALE`。价格发布确认的是完整 canonical spec/checksum。调度确认的是实际未来生效时的选价；与既有 quote 价格不一致就拒绝。

C02 预览若引用已过期报价，回 `409 QUOTE_EXPIRED`，不创建接受命令或付款义务。接受页显示原报价与到期原因，并提供带入原客户的新报价入口；新报价必须重新计价，不延长原报价。

C03／C04 预览若报价 ID、绑定 Fingerprint、目标订阅或变更方式不相符，回 `409 CHANGE_QUOTE_BINDING_MISMATCH`，不创建预览或命令。画面须指出绑定错配，让操作员从正确的报价详情重新进入。

C01 创建变更报价时，若绑定的订阅 revision 已改变，命令以 `CHANGE_QUOTE_REVISION_CHANGED` 失败；报价与绑定在同一交易回滚，不产生成功收据。C03／C04 预览若绑定或订阅 revision 已改变，回 `409 CHANGE_QUOTE_REVISION_CHANGED`；若报价价格版本已非目前选价，回 `409 CHANGE_QUOTE_PRICE_SUPERSEDED`。两者均不创建预览或命令，接口分别指出需依最新订阅状态或最新价格重新报价。

批量 preview 固定 target IDs＋source versions＋预期金额；job 不得运行后来才符合条件的对象。明确逐项冲突可留待新预览，不能偷偷扩大 batch。C13／C30／C32／C44／C45 每批最多固定 100 个候选；预览读取第 101 个候选以判断是否有剩余项目，只将前 100 个写入固定成员。五种工作皆依前一个已创建工作的固定清单最后一项轮转，必要时回到开头，避免冲突或待查证项反复阻挡其他候选。仅重开预览不移动光标。C32 已创建的 capture outbox 会退出候选集合。preview impact 的 `has_more_candidates` 为字符串 `true`／`false`，表示预览当下仍有本批之外的候选项目；UI 必须提醒操作员创建新批量。

### 4.3 命令清单

下表的 `route` 记录原先规划的资源语意路径。已实作的写入接口统一为 `POST /admin/api/commands`，由 `action_id`、`target_id` 和 typed payload 选择表中的动作；需要预览时先调用 `POST /admin/api/previews`。单一命令入口共用 session、CSRF、能力、幂等与收据处理，实际 UI 路径与证据见[动作盘点](../implementation/web-admin-action-audit.zh-CN.md)。R=需 preview；N=不需确认预览（仍须授权、幂等及领域检查）。表中的 inputs 不重复列 reason、preview_id、expected_revision 等共用字段；`target_id` 必须对应表中资源对象。

| ID | route | inputs／领域服务 | 权限 | 预览 |
| --- | --- | --- | --- | --- |
| C01 | `/quotes` | customer_id；互斥的 plan_id＋cohort＋seats 或 contract_version_id＋seats；可选 change_subscription_id＋mode＋revision。`CreateQuoteForCohort/CreateContractQuote/BindChangeQuote` | subscription.manage；合约另需 contract.manage | N |
| C02 | `/quotes/{id}/accept` | fingerprint；`AcceptQuote/AcceptContractQuote`，拒绝 change quote | subscription.manage；合约另需 contract.manage | R |
| C03 | `/subscriptions/{id}/schedule-plan` | quote_id、binding fingerprint、revision；从 quote 取 plan/seats。`ScheduleNextPlanAtPrice` | subscription.manage | R |
| C04 | `/subscriptions/{id}/upgrade` | quote_id、binding fingerprint、revision；`RequestImmediateProUpgradeAtPrice` | subscription.manage | R |
| C05 | `/subscriptions/{id}/cancel` | revision；`ScheduleCancel` | subscription.manage | R |
| C06 | `/subscriptions/{id}/resume` | revision；`ResumeCancel` | subscription.manage | R |
| C07 | `/invoices/{id}/payments` | amount_minor；`CreatePayment` | finance.adjust | R |
| C08 | `/payments/{id}/retry` | 原 operation ID 在交易内解析 invoice_id 并核对确定失败；`RetryFailedPayment` 实际接收 invoice_id | finance.adjust | R |
| C09 | `/payments/{id}/dispatch` | 固定 operation ID；`DispatchCapture` | finance.adjust | R |
| C10 | `/payments/{id}/reconcile` | 原操作；`ReconcilePayment` | finance.adjust | N |
| C11 | `/invoices/{id}/reductions` | reduction_minor、reason；`PostReduction` | finance.adjust | R |
| C12 | `/credits/{id}/applications` | invoice_id、amount_minor；`ApplyCredit` | finance.adjust | R |
| C13 | `/jobs/change-corrections` | preview 固定 change IDs；拆解 `RunChangeCorrections` | finance.adjust | R |
| C14 | `/changes/{id}/resolve-unfulfilled` | change ID；`ResolveUnfulfilledImmediateChange` | finance.adjust | R |
| C15 | `/credits/{id}/refunds` | amount_minor；`ReserveRefund` | finance.adjust | R |
| C16 | `/refunds/{id}/dispatch` | 固定 refund ID；由 `DispatchRefundNext` 抽出按 ID 运行 helper | finance.adjust | R |
| C17 | `/refunds/{id}/reconcile` | 原退款；`ReconcileRefund` | finance.adjust | N |
| C18 | `/prices/pro` | ProPriceSpec；`PublishProPrice` | catalog.publish | R |
| C19 | `/meters` | id、source、unit、schema_version；`RegisterMeter` | catalog.publish | R |
| C20 | `/prices/metered` | MeteredPriceSpec；`PublishMeteredPrice` | catalog.publish | R |
| C21 | `/catalog-selections` | plan_id、cohort、effective_at、price_version_id；`SelectCatalogPrice` | catalog.publish | R |
| C22 | `/price-migrations` | id、cohort、target_price_version_id、固定 subscription_ids；`PreviewPriceMigration/PlanPriceMigration` | migration.manage | R |
| C23 | `/price-migrations/{id}/pause` | batch ID；`PausePriceMigration` | migration.manage | N |
| C24 | `/price-migrations/{id}/items/{subId}/skip` | 明确对象与理由；`SkipPriceMigrationItem` | migration.manage | R |
| C25 | `/price-migrations/{id}/resume` | 重查后的 batch；`ResumePriceMigration` | migration.manage | R |
| C26 | `/usage-events` | source、event_id、subscription_id、meter_id、occurred_at、quantity；`RecordUsage` | usage.manage | N |
| C27 | `/usage-adjustments` | source/event_id/subscription_id、original_source/original_event_id、reverse_quantity；`RecordUsageAdjustment` | usage.manage | R |
| C28 | `/usage-periods/{subId}/{index}/close` | cutoff；`CloseUsagePeriod` | usage.manage | R |
| C29 | `/usage-periods/{subId}/{index}/rerate` | 帐期；`RerateUsagePeriod` | usage.manage | R |
| C30 | `/jobs/usage-credit-notes` | preview 固定差额项目；拆解 `RunUsageCreditNotes` | finance.adjust | R |
| C31 | `/contracts` | ContractSpec；`PublishContract` | contract.manage | R |
| C32 | `/jobs/contract-collections` | preview 固定到期 invoices；拆解 `CollectDueContractInvoices` | finance.adjust | R |
| C33 | `/reconciliation-runs` | as_of；`RunReconciliation` | reconciliation.repair | N |
| C34 | `/discrepancies/{id}/repair` | discrepancy ID、来源证据；`RepairDiscrepancy` | reconciliation.repair | R |
| C35 | `/discrepancies/{id}/manual-decisions` | decision、reason；reviewer 从 session 取得。`RecordManualDecision` | reconciliation.repair | R |
| C36 | `/account-migrations` | legacy_account_id、customer_id、beneficiary_id、cohort、has_history；`LinkLegacyAccount` | migration.manage | R |
| C37 | `/account-migrations/{id}/shadow-quotes` | plan_id、seats、legacy_amount_minor、legacy_currency；`ShadowQuote` | migration.manage | N |
| C38 | `/account-migrations/{id}/shadow-entitlements` | subscription_id、legacy_status；`ShadowEntitlement` | migration.manage | N |
| C39 | `/account-migrations/{id}/provenance` | LegacyProvenance mapping；`BackfillLegacyProvenance` | migration.manage | R |
| C40 | `/account-migrations/{id}/provenance/{legacyInvoiceId}/resolve` | corrected mapping、decision；reviewer 从 session 取得。`ResolveLegacyProvenance` | migration.manage | R |
| C41 | `/account-migrations/{id}/switch-read` | MigrationThresholds＋来源版本；`SwitchAccountRead` | migration.manage | R |
| C42 | `/account-migrations/{id}/switch-writer` | MigrationThresholds＋来源版本；`SwitchAccountWriter` | migration.manage | R |
| C43 | `/account-migrations/{id}/stop` | reason；`StopAccountMigration` | migration.manage | R |
| C44 | `/jobs/renewals` | preview 固定到期 subscriptions＋帐期；拆解 `RunRenewals` | operations.run | R |
| C45 | `/jobs/entitlement-refresh` | 固定 subscriptions；拆解 `RefreshEntitlements`，沿用来源事实 | operations.run | R |
| C46 | `/lab/clock` | UTC instant 或 mode=real；clock revision | lab.control | R |
| C47 | `/lab/payment-decisions` | operation_id、status=`succeeded`或`definitively_failed`；`SetFakePaymentDecision` | lab.control | R |
| C48 | `/lab/refund-decisions` | refund_id、status=`succeeded`或`definitively_failed`；`SetFakeRefundDecision` | lab.control | R |
| C49 | `/lab/faults` | operation kind/ID、mode=`lost_response`或`crash_after_provider`，一次性 fault ticket | lab.control | R |

UI 的「送出下一笔」先列出并确认具体 operation，提交 C09／C16；不得因队列改变改送另一笔。C49 ticket 只可由有 `lab.control` 的操作者在该操作派送时使用；普通金融页不接受任意 fault 字符串。

所有 preview 需在首次开始运行前仍有效；重播已提交结果优先查 receipt，不重验 expiry。job 在有效期内确认并持久化固定 membership 后，不因运行时间超过 preview 期限中途扩大或撤回范围，各项目仍必须做来源与金额检查。未开始的过期命令回 PREVIEW_STALE，不能默默延长。

C43 停止后，受该帐户控制的添加商务写入必须拒绝，不能把停止误报成来源 revision 变动。C02、C05、C06 新预览回 `409 ACCOUNT_MIGRATION_STOPPED`；C03、C04 也在创建预览时检查写入权。若预览后才停止，原命令可被查回，但运行结果为 `failed/ACCOUNT_MIGRATION_STOPPED`，不得创建订阅、取消调度或成功收据，也不得恢复已调度的取消。同一 request key 重播仍回原命令。

C01 的创建与 change binding 必须在一个交易完成；contract／purchase／change 三种用途互斥。前端 capabilities 是呈现能力，不能取代 authorization。管理端实际支持的 meter／Net30 组件才能接受对应 quote。

### 4.4 Payload schema

ProPriceSpec：`id,version,fixed_minor,seat_minor,included_tasks,usage_rate_num,usage_rate_den,effective_from`。MeteredPriceSpec：`id,plan_id,version,fixed_minor,seat_minor,meter_id,included_quantity,usage_rate_num,usage_rate_den,effective_from`。ContractSpec：`id,customer_id,version,base_price_version_id,fixed_minor,seat_minor,effective_from,effective_to,post_contract_price_version_id?`。字段政策由领域 validator 共用；不能由 HTTP 层放宽。

含每席费用的已发布价格及合约，`fixed_minor + seat_minor` 必须可由 `int64` 精确表示，确保最少一席可报价。无每席费用的计量价格只需固定费用本身有效；更多席次仍于报价时逐次检查溢出。管理表单与预览在创建命令前拒绝不合格组合，直接领域发布路径也运行同一限制。

LegacyProvenance：`legacy_invoice_id,legacy_subscription_id,legacy_account_id,commerce_subscription_id,commerce_invoice_id,price_version_id`；status/evidence 由领域决定，不能从 browser 采信。MigrationThresholds：`max_quote_p95_millis,max_unknown_payments,max_open_discrepancies`；沿用目前 unknown 必须零的切换政策，即使 UI 允许查看门槛也不能放宽。

publish／map 等没有单一 subscription revision 的操作，preview 绑定完整来源 fingerprint 与相关版本；运行 transaction 重查，不虚构一个适用所有对象的 revision。

## 5. 交易、命令恢复与工作模型

```text
typed request → auth/CSRF → 查原 request key → 解析/验证 intent
    → admission transaction：command + preview claim + audit(accepted)
    → 单一 local worker：claim lease + 运行前权限检查
        → domain transaction：来源再验证 + 业务事实 + receipt + audit
        → provider/outbox：同一原 operation key，在 transaction 外
        → 记录观测 → 更新 command 结果 → UI 轮询
```

command：`accepted → running → succeeded | failed | waiting_verification`；waiting 只能经原 key 查证得到结果。能力政策在重启时收紧，尚未产生效果的命令记 `failed/PERMISSION_REVOKED`；已有 provider 义务仍查证。无法安全判定跨数据库操作是否已产生效果且尚无 receipt 时，保留 `accepted/PERMISSION_REVOKED_REVIEW`，不新启动操作，待 receipt 或权限恢复后以原命令继续。每次 claim 有 lease generation，旧 worker 不得以过期 generation 覆盖新结果。第一版同进程单 worker，lease 仍用于重启恢复；本机时间调整不能改 lease 时钟，lease／session 用 wall clock，业务帐期用 business clock。

能力撤销后，`read` session 可以对既有 `waiting_verification` 与 `PERMISSION_REVOKED_REVIEW` 命令要求查证；运行端必须先以持久化的 operation／repair／provider control receipt 证明此路径不会派送新操作。查证可能追加本机观测与收据，仍需 session、Origin 与 CSRF。

对纯本地金额写入，将既有 public 方法抽成 `...Tx` helper，public wrapper 仍保留目前接口。新的管理 executor 拥有最外层 transaction，业务 facts、command receipt、审计一起 commit；不能在单连接 DB 的外层 transaction 中再次调用会 BeginTx 的方法。选定整个 admin 命令交易保证，禁止逐 endpoint 任意选弱化的「事后补纪录」。

管理调用使用稳定 `admin:<command_id>` 作 domain request key；已存在 public requestKey 的方法沿用 key 对应，provider key 仍由原领域义务产生；没有 key 的发布、切换、shadow、人工决议等操作，通过 command receipt 加 domain business key 避免再次生效。恢复先找 receipt；没有 receipt 也未 commit 的本地命令可以重入。provider side effect 不能以「没有 receipt」推断未发生，永远用原 operation 查证。

payment／refund dispatch 命令选定操作 ID 后，transaction 标示合法派送状态，提交后调用 fake provider，观测落盘。response lost 或 process crash → command waiting，保持原保留与 key。查证无 terminal evidence 时继续等待，不能释放退款额度或添加 capture。

C47/C48 会写入另一个 provider SQLite，不能宣称与 commerce command 同 transaction。为 fake provider 控制添加 `provider_control_receipts(command_id UNIQUE,payload_hash,target_key,result)`，与该次 decision 更新同一 provider transaction 提交；恢复先查此 receipt，不能因 capture 已经发生而把已完成设置误报失败。这只用于 fake provider 控制，不变更既有 capture/refund 金融事实。

对帐与 shadow 是观测型命令：收集来源观测与时间／版本后，把 evidence、结果引用与 command receipt 在 commerce transaction 提交。无法跨两个 SQLite 取得单一原子 snapshot，必须记录各来源观测时间；修复仍重新核对来源，观测 run 成功不代表资金一致。

job 固定 membership 并逐项 receipt；续约／收款／更正类不以一次全数据库重新扫描作重试。job summary 分为 succeeded/failed/conflicted/waiting/skipped；server 正常关闭时停止 claim 新项目，进行中的效果由原操作恢复；可由 UI 暂停的是 C23 的价格迁移，初版不添加泛用 job pause 命令。管理 UI 不提供泛用重跑任意 job；金融冲突需新预览，UNKNOWN 查原义务。

`business_time` 在一个命令开始运行时固定，preview 提供估值时刻；clock revision 改变会使尚未确认的 preview 失效。job items 保留该 job 的业务时刻。C46 与本进程的运行 gate 协调，不在一个命令途中换 clock。领域金额／revision 保护仍在 DB transaction，不能依赖前端或单 worker 来取代并行检查。

## 6. 拟添加数据表与迁移

| table | 内核字段与约束 | 保存／恢复 |
| --- | --- | --- |
| admin_schema_migrations | version PK、checksum、applied_at | 顺序运行、checksum 不符拒绝 |
| admin_commands | id PK、actor_id、idempotency_key、kind、target、payload_json/hash、preview_id、status、business_time、clock_revision、lease_owner/generation/until、result_refs、error_code、created/updated | UNIQUE(actor_id,idempotency_key)；status CHECK；金融结果不自动清除 |
| admin_previews | id PK、actor_id、kind、intent_hash/json、source_versions、impact_json、expires_at、claimed_command_id | immutable；一个 preview 只能绑定同一 command；过期可查不可新运行 |
| admin_command_receipts | command_id PK/FK、domain_request_key、result_refs、committed_at | 与本地业务 transaction 同 commit；不可覆写 |
| admin_jobs / admin_job_items | job ID、command ID、frozen as_of；item target、source fingerprint、status、receipt、failure | UNIQUE(job_id,target_type,target_id,period_key)；冻结 membership |
| admin_audit_events | id、command_id、actor、action、target、reason、before/after refs、request_id、wall time | append-only；不保存 secrets；保存来源引用而非拷贝全部金流 payload |
| admin_lab_settings / admin_fault_tickets | clock mode/value/revision；fault target/kind/mode/claimed command | 只在 lab profile 可用；ticket 一次性与目标核对 |
| provider_control_receipts（provider DB） | command_id PK、payload_hash、target_key、result、committed_at | 与 fake decision 更新同 transaction；供 C47/C48 查回原结果 |

session／登录限流在本机进程内存，重启即失效；command/job/audit 不因注销消失。来源 financial 表的金额与 immutable history 不做 destructive migration。schema upgrade 在启动、接受 HTTP 前完成，单 transaction 套用添加表／索引，任何失败保持旧 DB 可由旧 binary 打开；事先备份 commerce/provider 配对文件。首次运行使用新暂存 DB；升级测试使用现有 schema fixture。

若添加字段到既有表，必须兼容旧 CLI／v1 写入（有 default／nullable 或共同 wrapper）；禁止把旧 facts 伪造为有 admin actor 的历史。最初只读接入可停用 admin server 回到既有 CLI；涉及新金融事实后，不以还原旧 DB 当 rollback。

## 7. 实作前已解决的设计取舍

| 问题 | 决策与代价 |
| --- | --- |
| 现有 v1 没有完整 session／RBAC | admin server 不挂载它，避免绕过管理权限；需维护独立管理 DTO，但共用 domain |
| 加 wrapper 后业务 commit 与 command 可能分离 | 抽 transaction helpers，receipt 同 commit；改动较多，换取可验证的崩溃恢复 |
| batch「全部运行」会纳入预览后的新对象 | 固定 membership＋逐项 source guard；多一层 job 表与恢复逻辑 |
| fake clock 污染 session／lease | wall clock 与 business clock 分开，命令内固定时间；需要可注入时钟测试 |
| dispatch-next 可能换了目标 | preview 选定 ID，execute-by-ID；添加 refund dispatcher helper |
| SQLite single connection 与 UI 频繁读取 | 有界分页、短 transaction、单 worker、取消不可见页面轮询；先量测再优化，禁止拷贝全部 State 到 Web |

未在范围：真实 PSP、税、多币别、总帐、公开网络部署、完整用户生命周期、MRR/ARR、新的订阅政策。原因均是超出现有本机 MVP 与 Web 操作层的目标。上述延期不会删除 C01–C49 任一现有 CLI 能力。

规模限制：一次 preview/job 最多 1,000 个目标，超出回明确错误并要求分批，不静默截断。这是本机操作界线；所有对象仍可分批操作。登录与命令限流不可消耗金融额度，也不可触发新的付款。

两个 SQLite 文件的 schema upgrade 各自在本库 transaction 完成，不宣称跨库原子性。迁移保持 additive／可重跑，admin 启动必须等两库所需版本都完成；若第二库失败则不接受 HTTP，下一次启动从已完成版本继续。旧 CLI 兼容性与配对文件备份由 A05 验证。
