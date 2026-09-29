# Web Admin：操作与验收纪录

[English](web-admin.md) | [繁體中文](web-admin.zh-TW.md) | **简体中文**


## 现况

React 19＋Ant Design 管理接口已接到本机 Go server。C01–C49 各有管理命令与 UI 操作入口；概览、资源列表、详情、命令及批量工作也可读取。这是本机管理实验环境，尚未完成 [A01–A30 全项验收](../design/08-web-admin-test-plan.zh-CN.md)，因此不能把「入口存在」等同于整体交付完成。

价格迁移详情的 `GET /admin/api/price-migrations/{id}` 现在回传有界摘要与各状态计数；`GET /admin/api/price-migrations/{id}/items` 以每页 1–100 笔的光标查找逐项数据，支持状态筛选并回传观测时间。光标绑定批量 ID 与筛选条件；项目状态变更后，应从第一页重新查找。前端缺省每页 20 笔，恢复按钮依全批量冲突数判断。金额仍以精确的最小货币单位字符串传递。

## 启动

1. 需要 Go 1.27、CGO、Node.js 与 pnpm。拷贝 `.env.example` 为 `.env`，设置 `BILLFORGE_ADMIN_USERNAME` 和 12–72 bytes 的 `BILLFORGE_ADMIN_PASSWORD`；限制 `.env` 读取权限。可改用进程环境变量，且进程环境优先于文件。
2. 在项目根目录运行：

   ```sh
   pnpm --dir web/admin install --frozen-lockfile
   pnpm --dir web/admin build
   go run ./cmd/lab admin ./billforge-data/commerce.db ./billforge-data/provider.db 127.0.0.1:8080
   ```

3. 打开 `http://127.0.0.1:8080/admin/` 登录。以 Ctrl+C 停止。数据留在两个指定的 SQLite 文件；停止 server 不会删除数据。

可选的 `BILLFORGE_ADMIN_CAPABILITIES` 是逗号分隔的能力清单，必须包含 `read`；未设置时给予完整本机管理权限。变更后重启，既有 session 失效。重启恢复会依新能力检查尚未运行的命令：可确定尚未产生效果者记为 `failed/PERMISSION_REVOKED`，已送出的付款／退款仍用原 operation 查证，维持 `waiting_verification` 或完成结果。已开始的批量与无法安全证明未产生效果的操作保留恢复义务；后者在命令详情显示 `PERMISSION_REVOKED_REVIEW` 并停止无效轮询。「检查既有收据」只查原 provider key 与已记录的收据，可能更新本机查证结果，但不派送新的 provider 操作；若证据后来可查，可直接完成原命令。若仍无证据，需恢复能力后再重启继续。

若想指定另一个文件，使用 `go run ./cmd/lab admin --env-file /path/to/admin.env commerce.db provider.db 127.0.0.1:8080`。前端资产可用 `go build -tags admin_ui -o /tmp/billforge-admin ./cmd/lab` 嵌入二进位档；必须先运行前端 build。开发版不带 tag 时从 `cmd/lab/adminassets/dist` 读取资产。两个数据库路径必须不同，管理服务仅接受明确的 loopback IP。

## 操作模型

- 登录后从左侧菜单进入资源、操作与实验控制。一般资源列表可依该资源允许的字段筛选；其中有 `created_at` 的价格迁移、订阅、Credit、退款、帐户迁移及对帐运行可用 UTC 的 `created_from`（含）与 `created_before`（不含），输入 RFC3339 时间。筛选条件、光标与返回前页的位置保存在 URL。光标绑定资源与筛选条件，依数据列顺序前进；跨页时添加的数据不构成同一个全域快照。命令详情可在 `/admin/commands` 查找；`/admin/jobs` 可依创建顺序分页找回批量工作、查看逐项完成与需处理数，`/admin/jobs/:id` 显示逐项结果。
- 客户列表 `/admin/customers` 使用客户 ID 光标；客户详情 `/admin/customers/:id` 汇整订阅、报价、帐单、Credit 与旧帐户映射，并可预填新报价。订阅列表 `/admin/subscriptions` 与详情 `/admin/subscriptions/:id` 显示实际价格、席次、帐期、权益来源、续约阻挡原因及下期安排；报价列表可由 `/admin/quotes` 进入。每类客户关联数据最多显示最近 100 笔，超过时会明示截断；`/admin/data/customers` 与原有大写 `/admin/data/Customers` 路径仍可读取。
- 金融操作先取得 preview，再提交命令。preview 绑定操作者、目标、来源 revision、business clock revision 与有效期；过期或来源变动要重新预览。重送相同 idempotency key 与相同 payload 应回到原命令；同 key 不同 payload 拒绝。
- API 先将命令持久化，再运行 domain 动作。命令收据与业务效果在可行处同交易；外部 fake provider 的未知结果保留 `waiting_verification`，查证原操作后再推进。不要因网页逾时创建新的付款或退款义务。服务重启会扫描可恢复命令。
- 批量工作 C13、C30、C32、C44、C45 固定 preview 成员，逐项提交，持久化成功／冲突／等待／略过结果，重启后接续未完成项。worker lease 使用 wall clock 与 generation fencing；旧 worker 无法覆写接管者结果。
- `/admin/lab/controls` 可设置持久化 business clock、付款或退款 fake provider 决策，以及一次性指定故障。具备 `lab.control` 能力时，页面另显示独立 provider 数据库的收款、退款、已设置决策数与待用故障票据数；`/admin/lab/provider-captures`、`/admin/lab/provider-refunds` 提供状态筛选与光标分页。provider 决策与 control receipt 同交易；重新启动后时钟仍有效。商务与 provider 数据分别观测，实验控制不代表真实支付服务行为。

- 浏览器的管理 API 读取等待上限为 10 秒，其他请求为 20 秒。逾时只表示浏览器没有取得确定回应；命令页保留原 request key，查证后才能开始另一笔。概览与列表若保留上次成功读取的数据，会显示更新失败警示及原观测时间；重新读取成功后警示消失。

## 登录与权限

登录采 server side session 与 CSRF 验证。session 有 30 分钟闲置及 8 小时总期限；错误密码和未知帐号回相同消息，失败验证有限速。管理 API 的每个 C01–C49 action 都映射 capability；C01 创建合约报价与 C02 接受合约报价需同时具备 `subscription.manage` 与 `contract.manage`，一般方案报价只需前者。目前是 `.env` 定义的单一管理帐号，没有多用户权限编辑画面。不得把密码放入 `VITE_` 变量或前端资产。

## 数据库升级与恢复

`admin` 启动会运行 `InitAdmin`。既有 admin schema v1 会在单一交易内升至 v8，包括 preview clock revision、帐单来源查找索引与命令／稽核 request ID 字段；升级失败会回滚，重跑可保持幂等。升级前请备份两个持久化 SQLite 文件，且在脱机副本先试跑。运行中的命令仍保留提交时的 business time；之后更动时钟会使旧 preview 失效。

如果 UI 显示命令等待，先到命令详情查看原 id 和状态，再使用同一命令的恢复入口；不要换 idempotency key 重建义务。付款或退款出现不确定回应时，以 fake provider 的原操作证据查证。批量工作按 job ID 查看每项结果，成功项不会重新套用。

## 已运行验证（截至 2026-09-28）

可重跑的 browser 验收：`pnpm --dir web/admin test:e2e`。测试会先建置 UI，再于暂存目录建置与启动嵌入式 Go server、创建隔离的 commerce/provider SQLite，完成后关闭服务并清理测试数据。需要本机 Chrome／Chromium；若未安装 Playwright 缺省浏览器，可设 `BILLFORGE_CHROME_BIN` 指向可运行档。

| 证据 | 结果 | 边界 |
| --- | --- | --- |
| `playwright test` 定向运行价格迁移、帐单、订阅、Credit、付款与退款详情的 6 个读取故障案例 | 6／6 通过；逐页验 503 保留并标示旧数据、停用写入入口、重试恢复，以及 403 隐藏缓存财务内容 | 浏览器网络回应受控；各路径的真实 SQLite 查找错误分类另由 Go 测试验证 |
| `pnpm --dir web/admin exec playwright test --reporter=dot --output=/tmp/billforge-web-admin-suite-20260928` | 2026-09-28 全套 125 个浏览器案例通过；最后前端改动重新建置后，价格迁移详情与暂停页 2 个目标案例也再次通过 | 本机 fake provider 与隔离 SQLite；不代表实际支付供应商验收 |
| `go test ./api/admin -run TestPriceMigrationItemsAreBoundedAndScoped -count=1`、`playwright test e2e/price-migration-items.spec.ts e2e/pause-migration-refresh.spec.ts` | 真实 SQLite／HTTP 验 123 笔批量的 50／50／23 分页、状态筛选与变更、光标绑定批量与筛选、精确金额、404／400／500；浏览器验 20 笔分页、筛选与全批量冲突数，且既有暂停页仍可恢复读取 | 状态改变后需从第一页重新查找；未宣称跨请求快照一致 |
| `go test ./...` | 全套 Go 测试通过；包含 session、CSRF、C01–C49 capability 矩阵、schema v1→v8、命令／收据、lease fencing、批量恢复、fake provider 故障等切片 | 不等于 C01–C49 每种错误与浏览器情境都已覆盖 |
| `go test ./lab -run TestAdminS07 -count=1` | 两条 S07 管理／领域直接比对通过：付款回应遗失后延迟开通与 funded 更正；帐期边界 hold、晚到收款、未履行决议后 Basic 续约。逐阶段核对金额、来源、provider capture 及命令收据。 | 隔离 SQLite／假服务商的领域验证；对应浏览器情境仍依 A14／A26 盘点。 |
| `go test ./api/admin -run TestMoneyAndTimePayloadBoundariesRejectBeforeHTTPAdmission -count=1` | 隔离 SQLite、具备相应权限与 CSRF 的真实 HTTP handler 验金额、费率分母和 UTC 错误均被拒于入场前，且无命令；有效大整数字串保留精确度。 | 只覆盖 C07／C11／C12／C15／C18／C33 指定字段，不代表所有操作与浏览器字段皆已验。 |
| `go test ./lab -run 'TestAdminResourcePageKeepsMissingSourcesDistinctFromZero|TestAdminSubscriptionAndInvoicePagesDoNotRepeatAfterInsert' -count=1`、`go test ./api/admin -run TestResourceListHTTPKeepsMissingProjectionAndPeriodAsNull -count=1` | 订阅缺权益投影及升级补充帐单缺帐期来源保持 `null`，真正的第 0 帐期保持 `0`；两类列表跨页插入后不重复。API 仍把非空数字串行化为精确字符串。 | 目前直接核对订阅与帐单；其他资源缺值矩阵仍待补。 |
| `pnpm --dir web/admin test:e2e --grep 'UTC action field rejects invalid dates and precision before admission'` | Playwright 在真实管理接口验 C07／C11／C12／C15 int64 越界阻止预览，改填合法大整数后错误解除；原有 C18 分母与 C33 UTC 案例同时通过。 | 此次是定向案例；最新完整浏览器回归见 2026-09-28 纪录。 |
| `pnpm --dir web/admin build` | TypeScript 与 Vite build 通过 | 不是 browser E2E |
| `go build -o /tmp/billforge_lab ./cmd/lab` 及 `go build -tags admin_ui -o /tmp/billforge_lab_admin ./cmd/lab` | 两种建置通过 | 未测公开部署 |
| 本机 browser smoke | 登录、固定时钟、重启后 session 失效及时钟保留；另验付款空列表、报价必填错误、390 px 版面、手机菜单 Escape／焦点恢复。价格发布的原 preview 因时钟 revision 变更而被拒绝，UI 自动重建预览并要求再次确认；数据库确认没有发布原价格。切换操作后错误不残留。58 个已知基本及带 ID 路由均可打开，无 404 画面或浏览器错误记录。临时服务已关闭 | 路由可打开不代表每条命令已完成操作验收 |
| `go test ./api/admin -run TestSubscriptionHistoryReadErrorsAndSessionGuard` 与 `go test ./lab -run TestAdminSubscriptionHistoryPaginatesWithoutRepeatingAndShowsEntitlementSource` | 订阅历史三个 API 都受 session 保护；缺订阅回 404，非法光标与上限回 400；帐期及时间轴跨页不重复，权益可追来源操作。 | 尚未覆盖各种调度、合约与立即变更事件同时跨页的组合。 |
| `go test ./lab -run TestReconciliationRun` 与 `go test ./api/admin -run TestReconciliationRunDetailSessionGuardAndReadErrors` | 对帐当次 finding 使用不可变快照；后续运行改变同一差异时，旧 expected／actual／证据仍保留。旧数据仅以当前值补录并标示品质；详情 API 验 401／404／400。 | 旧数据在导入快照前的原始观测无法还原；UI 明确揭示这个限制。 |
| `go test ./lab -run TestAdminAccountMigrationDetailShowsOwnersShadowsAndStopWithoutRollback` 与 `go test ./api/admin -run TestAccountMigrationDetailAndReadinessGuardInputs` | 帐户详情以同一读取交易取得 owner、shadow、来源及事件；Readiness 要求明确门槛，未登录与无效参数分别拒绝。 | 旧数据库副本与大量历史记录仍待验；详情每类最多显示 100 笔并标示截断。 |
| `go test ./lab -run TestAdminAccountMigrationHistoryPagesDoNotRepeatRows` 与 `go test ./api/admin -run TestAccountMigrationDetailAndReadinessGuardInputs` | Shadow 与来源历史使用有界 keyset 分页，跨三页不重复；两个端点均验 session、404、非法光标和上限。详情页的「查看完整历史」可进入对应页面。 | 目前是小样本；大规模历史负载仍待量测。 |
| `go test ./lab -run 'TestAdminInvoiceDetail|TestAdminInvoiceHistory'`、`go test ./api/admin -run TestInvoiceHistorySessionCursorAndReadContracts` 与 `go test ./lab -run TestAdminSchema` | 帐单来源 DTO 於单一读取交易链接减额、更正发布的额度、跨帐单抵扣及退款；旧库升级套用四个来源查找索引且可重跑。四类来源可依创建时间与 ID 做有界光标分页；101 笔更正可查完，插入新纪录后不重复，跨种类光标被拒；API 验 session、404、非法光标及 limit。 | 跨页不提供全域数据快照；添加数据可能不在原有分页串行中。 |
| `pnpm --dir web/admin test:e2e` | 2026-09-27 完整回归的 94 个 Playwright 测试通过，包含并行减额预览、可重试命令恢复、付款／退款 provider commit 后中断，付款待查证期间调整仿真时间，以及正反向价格迁移与部分项目冲突的案例：登录、跨分页注销后的 401、原页重新登录与延迟旧 401 的隔离、错密码与缺 CSRF 阻挡、受保护读取、旧 CLI 数据库启动升级、58 条路由、Credit 详情与退款来源状态、报价与合约、付款及退款结果未定的原命令查证、客户与订阅详情、价格与用量操作、对帐证据、帐户迁移及读写切换。资源筛选案例验 URL 中的筛选条件与光标能跨页、返回及刷新，切换筛选后从第一页重新查找。用量案例验证已付款续约后的负差额产生一笔 Credit Note 和一笔额度；合约案例验证缺后续价格时暂停续约；帐户案例验证历史来源审核与 UNKNOWN 付款阻挡切换，查证后才放行。直接核对隔离 SQLite 的金额、来源、唯一外部操作与命令收据。帐单案例另验四类来源历史、光标往返，以及 C14 未履行升级更正和 Credit 来源。 批量案例另验 C45 预览后添加合格订阅仍维持固定成员，以及待查证项目不计入完成进度。 追加案例验 C01／C26 入场前 422 拒绝后表单解锁且无命令、回应遗失后刷新仍沿原 key 查回；C07 改金额后撤销预览，受控 409 拒绝后可重新预览且无 C07 命令；C18 预览后改输入、旧飞行回应不复活确认入口，Escape 关闭确认框后焦点返回原按钮。 概览与列表的受控 503、实际 10 秒读取逾时，以及 C01 回应延迟至 20 秒写入逾时后沿原 key 查回，也已加入浏览器回归。 58 条既有路由另以 390 px 窗口检查整页无横向溢出与行动菜单首个 Tab 焦点。 C34 另以已付款订阅制造缺失的权益投影，经 UI 对帐、预览及修复后核对唯一操作与收据；加强断言的单项重跑确认 expected／actual 快照及后续 verification。 C34 预览后来源 revision 变动的案例核对 blocked 操作、未重建的权益与画面警示。 服务商金额不符案例验 C34 blocked、C35 人工决议与 provider 收款总笔数不变。 | 使用本机 fake provider；A01–A30 的完整错误与 browser 矩阵尚未覆盖。 |
| 嵌入式 binary＋隔离 SQLite 的 HTTP smoke | `/admin/` 与 `/admin/login` 200；未登录 API 401；CSRF 登录 200、概览 200、注销 204、注销后 401；测试服务已停止 | 验证启动与 session 路径，尚未验证全部 UI 操作 |
| 1 万订阅／10 万用量事件的 `BenchmarkAdminStateLoad`，`-benchtime=1x` | 100 笔用量页 6.98 ms、订阅页 1.58 ms；5 读取者＋1 写入者共 100 次读取，P95 58.47 ms、最大 112.35 ms | 本机 DB benchmark，并非 HTTP／浏览器 P95；aggregate state 约 714 ms，仍不适合页面每次全量读取 |
| 相同数据量的 `BenchmarkAdminHTTPReadLoad`，`-benchtime=1x` | 真实本机 HTTP handler：订阅 100 笔 22,270 bytes／42.99 ms，用量 100 笔 29,379 bytes／19.06 ms；5 HTTP 读取者＋1 SQLite 写入者完成 100 次读取、20 次写入，读取 P95 84.57 ms、最大 193.77 ms | 写入者只插入 audit 记录；尚须测长时间业务 worker 与更多 UI 逾时情境 |

| 新客户／单笔详情的 `BenchmarkAdminHTTPReadLoad`，`-benchtime=1x` | Intel Xeon Gold 6133：1 万订阅与 10 万用量事件下，客户 100 笔 7,001 bytes／19.42 ms、客户详情 1.15 ms、订阅详情 0.75 ms、报价详情 0.35 ms；五读一写 P95 91.15 ms | 单次本机量测；写入者只插入 audit，尚未涵盖业务 worker 与更多 UI 逾时情境。 |
| 真实用量 worker 的 `BenchmarkAdminHTTPReadLoad`，`-benchtime=1x` | 1 万订阅、10 万既有用量事件；5 个 HTTP 读取者同时做 100 次用量列表读取，1 个 worker 经 `RecordUsage` 写入 20 笔新事件并验落库。最近一次运行的 100 笔订阅／用量／客户回应分别为 22,463／29,493／7,004 bytes；读取 P95 85.70 ms、最大 130.42 ms；worker 20 笔用时 617.32 ms。前次同设置运行的 P95 为 92.47 ms | 2026-09-27 两次各 `-benchtime=1x` 的本机量测，含真实领域写入与 fake provider 的已付款订阅；不代表其他机器或生产流量。旧两列是只写 audit 的历史基线。 |

重跑负载量测：`go test ./lab -run '^$' -bench BenchmarkAdminStateLoad -benchtime=1x -count=1 -v` 与 `go test ./api/admin -run '^$' -bench BenchmarkAdminHTTPReadLoad -benchtime=1x -count=1 -v`。单次 benchmark 只描述该机器与该次数据；正式比较需固定硬件、Go 版本及多次样本。

本次量测环境：2026-09-26 UTC，Linux x86_64、Intel Xeon Gold 6133 2.50 GHz、Go 1.27.1。工作区尚无 Git HEAD；量测时 151 个 Go／前端来源与模块档的 SHA-256 组合摘要为 `f7859893233ca89b163ac213c943f39e838164da04103470519c7e143dd976f9`。

真实用量 worker 量测环境：2026-09-27 UTC，Linux 7.0.0-31-generic x86_64、Intel Xeon Gold 6133 2.50 GHz、Go 1.27.1。benchmark 明确验证预载恰为 10,000 笔订阅及 100,000 笔用量事件；付款后的 Pro 订阅提供实际的帐期、价格指派与 meter，写入经领域交易，HTTP 列表逐页查找而非全量 State dump。五个读取者与 worker 由同一信号启动，提交后逐笔计数核对进度。P95 取 100 笔已完成请求中的第 95 笔；单次样本不构成跨环境的性能保证。

验收缺口：A01–A30 仍有未完成项。C01–C49 的无 session、缺 CSRF 与缺 capability 已逐项验证；尚缺其余动作的跨 actor 预览、重播与不同 payload 组合矩阵，全部 UI 的键盘与窄屏幕检查，以及真实 HTTP 断线、并行故障的端到端证据。对应的 [验收计划](../design/08-web-admin-test-plan.zh-CN.md)在这些项目完成前维持未结案。

### A01–A30 证据盘点

「部分」表示有可重跑的切片测试，仍缺该案例列出的完整条件；「未运行」表示尚无对应的正式验收结果；「本机通过」表示此案例列出的本机门槛已验。此表是缺口盘点，不代表整体签核。

| ID | 状态 | 已有证据／主要缺口 |
| --- | --- | --- |
| A01 | 部分 | 全套 Go 回归通过；隔离双数据库比较 C01／C02 Pro 五席购买的帐单、帐期、付款、权益与价格指派，以及 C04 Basic 周期中点升级 Pro 五席的抵扣 1000、新额 5000、应付实收 4000。C44 月底续约比较帐期界线、到期日、帐单明细、付款义务、capture outbox 与实收 2000。 S03 另以 C44／C09／C45／C08 对比确定失败、宽限、暂停与补款恢复；原失败操作不被重用，同键重播不添加付款义务。 未知续约付款另以 C49／C09／C45／C10 比对第八天仍保留查证宽限，确认原操作后只入帐一次并恢复权益。 S04 以 C49 注入收款回应遗失，C09 维持原操作待查证，C10 查证后原命令完成；两路均只有一笔 provider capture 与一笔 2000 入帐。 S05 以 C49 注入 provider 提交后中断，重启后比对重复／过期 Webhook、C45 权益投影重建及原 C09 命令唯一收据。 S06 以 C05／C06／C44／C45 比对取消、恢复、再次取消及边界到期；两路均没有添加帐期或帐单。 S07 添加两条隔离 SQLite／假服务商直接比对：C04 期中升级付款回应遗失时 C09 无成功收据且订阅维持 Basic，两天后 C10 查证并完成原 C09，C13 只更正 534 并产生同额 funded grant；另一条在帐期边界先由 C44 hold 续约，晚到收款转 needs_review，C14 发布 4000 后 C44 才创建 2000 的 Basic 续约。逐阶段核对 provider capture 唯一性、帐单义务／实收／发布／未清、grant、命令收据与直接领域路径。 S08 以 C18／C21／C22／C44 比对 Pro v2 于帐期边界迁移、其中一户下期回迁 v1；两户逐期帐单、实收与价格指派来源一致。 另验 revision 冲突时 C44 先只让未冲突户以新价续约，C24 明确跳过后第二次 C44 才让冲突户以旧价续约。 C23 暂停与 C25 恢复另以同一迁移计划直接比对，恢复后 C44 才产生 Pro v2 帐单。 S09 以 C26–C30／C44 比对用量关帐、晚到事件回溯原帐期、次期 debit、反向调整后暂停续约，以及 credit note 创建后恢复。 S12 以 C33–C35 比对安全权益修复、待派送与未知付款查证、来源 revision 改变封锁、provider 金额不符时不自动动帐，以及人工审查决策。 P01 以 C19／C20／C21／C01／C02／C26／C28／C44 比对 AI token SKU 的 3000 首期、105 tokens 关帐后次期 3001 帐单与原帐期用量来源。 P03 以 C36–C43 比对 shadow、先读后写切换、stop 后新收费阻挡；另一案例验未知付款与错误历史凭证阻挡 readiness，C10 查证及 C40 更正后才可切换。 S11 以 C11／C12／C15–C17 比对 funded credit 先套用 500、余额保留供退款 500；退款回应遗失期间预留额不释放，查证后只向原 capture 退款一次。C01／C03 调度下期 Pro 五席时，原 Basic 价格保持生效；C44 到期后和直接领域路径同样产生 10000 的 Pro 帐单与付款义务。C11 部分付款 6000 后减额 2000，比较原额 10000、应付 8000、未收 2000、无 funded credit，再补收 2000 后两路均结清。C11／C12 已付款减额 1000 并跨帐单套用 500，比对来源帐单发布、目标未收 1500、额度已用 500／可用 500。S10 以 C31／C01／C02 创建 Net30 合约，C44 缺后续价格时不创建下一帐期且暂停权益，C32 仍可收回原 7500 帐单；两路比对合约来源、到期前零 capture outbox 与到期后唯一实收。 另以 C31 设置后续价格，C44／C32 与直接领域路径比对两期 7500 合约帐单、各自到期后唯一收款、11 月切换后及 12 月延续的 10000 帐单；价格指派只添加一次，合约转换只有一笔。P02 以 C19／C20／C21 管理发布新计量 SKU 后，验旧 `/v1` client 因未声明能力而回 409 且零订阅，具备能力的 client 可接受同一报价。其余 S01–S12／P01–P03 情境尚未逐项比对；详见 [A01 基线与比对矩阵](a01-domain-baseline.zh-CN.md)。 添加 S11 隔离双数据库比对：未知收款时直接入口与 C11 预览均拒绝 2000 减额，C10 查证原 C09 后两路帐单义务 8000、实收 10000、funded credit 2000，provider 各一笔 capture；详见 [A01 基线](a01-domain-baseline.zh-CN.md)。 另以隔离双数据库验证首期收 6000、C11 减额 4000 后才激活权益，该情境无 funded credit 且各只有一笔 capture；C12 跨客户抵扣则不产生 application。 S11 未付款减额已由 `TestAdminUnpaidReductionAndReplacementCollectionMatchDomain` 验证：直接领域路径与 C11／C07／C09 均取消原 10000 收款，不产生 funded credit，只收取更正后的 8000 并结清相同账单余额。直接权益重建与 C45 均启用权益；C11／C07／C09／C45 使用原请求键重放后，仍只有四条命令收据和一笔 provider capture。 |
| A02 | 本机通过 | 前端、普通 Go／嵌入式 Go build 通过；纯 API binary 在无前端资产的工作目录启动；缺资产时 admin 启动指出建置命令；58 个直接路由与静态资产测试通过。 |
| A03 | 本机通过 | Go 测试涵盖 session、CSRF、闲置与总期限；真 HTTP handler 的受控时钟案例验 29 分钟活动后延长闲置期限、再闲置 31 分钟回 401，以及持续活动至八小时总期限仍回 401。受控时钟另验同来源同帐号 5 次失败后节流、全域每分钟 20 次上限、期限到后恢复、未知帐号与错密码同回应，以及成功后 login nonce 失效。Playwright 以仅由 `.env` 提供、含 `#` 与 `=` 的密码验登录、注销、未登录读取 401、错误密码不创建 session，以及缺 CSRF 的金融命令回 403 且无报价写入；另用互相冲突的文件与进程环境帐密验进程环境优先；缺少管理密码时，真管理进程拒绝启动。另验第一分页注销后，第二分页的下一次读取会清除旧管理缓存、回登录画面，重新登录回原页；若第二分页直接按注销而收到 401，也会关闭旧管理画面。延迟回应测试先固定旧请求，再完成新登录，证实旧请求的 401 不会误清除新 session。管理 listener 现以实际监听地址验 Host，Go 测试与真 HTTP／浏览器测试确认其他 Host 对 API 与静态页面均回 403，正常登录仍可用；缺省 HTTP 80 端口省略 Host 端口号仍可用。时间边界由受控时钟的 HTTP handler 验证；浏览器验证 session 失效后的 401 与重新登录流程，未实际等待八小时。 |
| A04 | 本机通过 | `TestEveryActionHTTPAdmissionGuards` 与 `TestEveryActionRequiresItsCapability` 逐项验 C01–C49 的无 session 401、缺 CSRF 403、缺能力 403，具备能力时才进入 payload 验证；C01/C02 合约分支另需 `contract.manage`。真 HTTP／浏览器案例验已登录管理 listener 的 `/v1/quotes` 回 404 且无报价写入。`admin.New` 只保存 bcrypt hash，管理 listener 不读取内部 token；测试用随机密码与 token 不出现在建置资产、session／概览回应、URL 或启动／登录日志；`.env.example` 不含有效帐密，`.env` 与 `.env.local` 被忽略。预览的跨 actor 来源守卫列于 A09。 |
| A05 | 部分 | schema v1→v8、幂等及失败回滚测试；v3 为客户与单笔详情加入读取索引，v4 为订阅历史时间轴加入来源查找索引，v5 为对帐差异详情加入反向索引，v6 为帐户迁移详情加入读取索引，v7 为帐单更正、抵扣与退款来源加入查找索引，v8 加入命令与稽核 request ID 字段及索引。升级失败测试另验既有帐单金额及 provider capture 不变，且失败后仍能由原 CLI 领域路径创建与接受新报价；v4→v7 保留 checksum、已收款帐单、成功付款状态及 provider capture。浏览器测试验 CLI seed 数据的帐单与 capture 在 admin 启动后仍可查。仍需旧实际数据库副本演练。 |
| A06 | 部分 | `TestAdminConcurrentQuoteAdmissionAndReceiptFailureRecovery` 验两个 client 同 key 只有一笔 C01 admission；收据写入失败时报价回滚，重启后沿原命令恢复，重播仍是一笔报价与收据。`TestAdminAcceptQuoteRollsBackFinancialFactsWhenReceiptWriteFails` 验 C02 收据写入失败时订阅、帐单、付款义务与 outbox 全部回滚；重启并重复恢复后各只有一笔，provider 尚无 capture。`TestAdminResolveUnfulfilledRollsBackFinancialFactsWhenReceiptFails` 验 C14 的更正、Credit、决议与收据在收据插入失败时全部回滚，重启恢复后各只有一笔且 provider capture 不变。`TestAdminReserveRefundReceiptFailureRollsBackReservationAndRecoversOnce` 验 C15 收据写入失败时退款预留、退款 outbox 与收据都没有落库，Credit 可用额保持 1000；重启后沿原命令及原键重播仅创建一笔 500 预留、一笔 outbox 与一笔收据，尚未产生 provider 退款。`TestAdminChangeCorrectionBatchResumesCommittedItemAfterReceiptFailure` 验 C13 逐项更正已提交、最终收据失败时保留单一已完成项；重启后补上唯一收据，没有第二笔更正、Credit 或 provider capture。`TestAdminCreatePaymentReceiptFailureRestoresOriginalCollectionUntilRecovery` 验 C07 收据失败时旧付款与 capture outbox 仍保持待派送，没有新付款或收据；重启后原命令只取消旧操作并创建一笔 1000 的新付款、对应 outbox 与收据，原键重播不增加效果。其他命令的全部中断点仍待验。 |
| A07 | 部分 | lease generation 接管与旧 worker fencing；付款与退款 stale-lease 派送测试、`TestAdminCapabilityRevocationPreservesExternalObligations`、`TestServerRestartRevokesUnstartedCommandUnderCurrentCapabilities`。退款另验三种跨数据库状态：未提交时撤销后无外部效果、已提交但 provider 无证据时维持等待、provider 已提交而回应遗失时沿原 key 完成且只有一笔退款与收据。未运行的本地命令及尚为 `created` 的付款／退款派送会记 `failed/PERMISSION_REVOKED`；`submitted` 仍查证。C34/C47/C48 在跨数据库效果无法证明未发生且没有 receipt 时保留 `accepted/PERMISSION_REVOKED_REVIEW`，不会在撤销后新启动；UI 显示原因并可用 read-only session 检查原收据，有 receipt 时沿原命令完成。完整恢复／人工处理矩阵仍待验，因此 A07 尚未签核。 添加两个独立 Lab 实例共用 SQLite 档并行运行 C07／C08 的回归；先前 lease 与命令交易读后写升级会回 `database is locked`、使命令滞留 `accepted`，现先取得写入权后连跑各 10 次均有终态，且原 generation fencing 测试通过。其他命令的多进程交错及完整恢复矩阵仍待验。 C07 另由两个独立 OS 测试子进程同步竞争，同一 SQLite 档连跑 10 次皆落可查回终态；C08 也以双子进程连跑 10 次通过。 |
| A08 | 部分 | C01／C02 在真实 HTTP／SQLite 下验同 key 同 payload 回原命令；C01 同 key 不同 payload 409，且 quote、subscription、invoice 与付款操作均未重复。C07 领域回归另验预览到期后原 key 仍找回原命令，不同 payload 则冲突。C02／C07 真实浏览器回应遗失后，重新加载仍沿原 request key 取得既有命令，订阅、帐单与付款操作未重复；C44／C45 同路径验工作与逐项成员不重建；C44 的续约帐单、付款义务及命令收据亦维持各一笔。其余 preview action 的端到端矩阵待验。 C12 Go 回归验来源额度改变后的 `PREVIEW_STALE` 失败命令沿原 key 重播仍返回同一失败结果，不创建新抵扣或收据。 |
| A09 | 部分 | business clock revision 使旧 preview 失效、C02/C07/C13/C14/C16 跨 actor 预览隔离、C04 期中升级预览后金额上升拒绝、下降可完成的测试；受理时已对所有 R 类动作共用 actor／动作／对象／内容／期限／claim 守卫，C07 回归验不符时无命令写入。价格发布 browser 流程验自动重建预览及再次确认；通用操作表单在预览后修改输入会移除旧预览，较旧的飞行中预览回应也不能复活确认按钮。C02／C03／C04 的业务报价期限转实际预览 TTL 已验。真实 SQLite 的双分页 C11 案例另验另一分页先减额时，旧预览首次提交回 409 且不多写金融更正；UI 自动取得新预览、显示原先与现在金额，同 key 重播仍回原 `failed/PREVIEW_STALE` 命令，重新确认后才创建第二笔更正，原 invoice lines 不变。除 C02／C03／C04／C05／C06／C07／C08／C11／C12／C15 外，其余动作的来源变动与 UI 差异情境仍待验。 通用表单与 C07 付款、C03／C04 方案变更的专用表单，均在输入改动时撤销旧预览；飞行中的旧预览回应不得重新打开确认入口。 C07 的失效预览 UI 案例以受控 HTTP 409 `PREVIEW_STALE` 验证未入场意图被释放并自动重建预览；真实 SQLite 双分页案例验另一操作者先将 $20.00 帐单减额 $5.00，旧付款预览提交回 409、没有添加付款操作，接口显示原先 $20.00 与现在 $15.00 未清余额，重新确认后才创建付款操作；C05／C06 真实 SQLite 双分页案例验另一操作者先调度取消及其后先恢复时，两次旧预览各回 409；接口重新读取订阅并显示原操作、目前可用操作、revision 与取消调度差异，且不会自动预览反向操作；取消调度各阶段分别仅有一笔及零笔。C02 真实 SQLite 双分页案例验另一操作者先接受同一报价后，旧预览回 409；接口重新读取报价并显示尚未接受到已接受的状态差异，不再提供接受预览，订阅与帐单各仅一笔。C03 真实 SQLite 双分页案例验另一操作者先调度取消使订阅 revision 改变后，旧方案预览回 409、变更调度未创建；接口保留报价 ID 与绑定 Fingerprint，显示 revision 差异并要求重新绑定。C04 真实 SQLite 案例验业务时钟先后移动后，旧预览两次各回 409，接口分别显示较低与较高的新本期差额，重新确认前无升级写入；最终仅一笔升级。C08 真实 SQLite 双分页案例验另一操作者先将 $20.00 帐单减额 $5.00 后，旧重试预览回 409、新预览显示 $15.00、再次确认前没有添加付款操作；另一分页再提交旧预览回 409，接口列出已创建的新付款操作并拒绝产生第三笔。 C12 真实 SQLite 双分页案例验另一操作者在抵扣 500 的预览后先用 C15 预留退款 500；旧预览提交回 409 `PREVIEW_STALE`，原 C12 命令没有抵扣或收据。接口显示来源 `grant_reserved_minor` 变动并创建新预览，重新确认后才抵扣 500。 C15 真实 SQLite 双分页案例验另一操作者先预留退款 500 后，旧预览提交回 409 `PREVIEW_STALE`，旧命令失败且没有第二笔退款；接口重新预览并要求再次确认，最终两笔退款预留合计恰为 grant 1000。 |
| A10 | 部分 | 大整数串行化与 domain 金额测试；C07/C15 金额 payload 验超过 JavaScript safe integer 与 int64 上限内精确往返，并拒绝零、负值、溢出、小数、指数及 JSON number。C18 费率分母采精确正整数字串；C33 拒绝非 UTC、错误日期和数值时间。真实管理 HTTP handler 的 `TestMoneyAndTimePayloadBoundariesRejectBeforeHTTPAdmission` 另验 C07／C11／C12／C15 不合法金额在预览与命令入口均回 422 且零命令，C18 不合法分母同样被拒，C33 不合法 UTC 命令被拒；超过 JavaScript safe integer 的 C07／C15 字符串能通过语法验证而抵达来源查找，C18 预览保留完整的 9007199254740993 分母。Playwright 在 C07／C11／C12／C15 表单验证超出 int64 上限时阻止预览、改为 9007199254740993 后错误消失，且没有命令入场；C18 分母的相同浏览器边界与预览精度亦已验。报价到期、接受后帐期终点及待开通付款时的新帐期终点，均在写入或新 provider capture 前检查 Unix nanosecond 范围；既有 capture 的观测若无法表示开通帐期，会回领域冲突且不留下部分本地事实。另以隔离 SQLite 与管理 HTTP handler 验证已发布 Pro 价格的 9007199254740993 固定金额及费率分母，在价格详情和一席报价详情仍为精确字符串；含席次费用的报价金额为 9007199254740994。价格组件的有效极值与跨组件数值政策，以及其余字段的 HTTP／browser 边界矩阵仍待验。 最少一席可报价检查已覆盖 C18／C20／C31：三种领域发布路径拒绝固定费用与一席费用合计溢出，管理预览 HTTP 回 422 且不创建命令；恰好等于 `int64` 上限的有效组合仍可发布与报价。Ant Design 表单在前端阻止溢出组合并于字段修正后清调试误，定向浏览器案例通过。其他量值组合、时间与冲突矩阵仍待逐项验收。 |
| A11 | 部分 | 资源查找采 rowid 光标与当页查找；客户列表另采 customer ID keyset 光标。一般资源的筛选字段由后端白名单限定，光标绑定资源与筛选条件；未知、重复或格式损坏的查找参数回 400。六个有 `created_at` 的资源支持 UTC 时间范围，下界含、上界不含；非法时区、超出 Unix 奈秒范围与反向区间被拒绝。价格迁移详情改为依 ID 查找，不再加载全份 `State`；测试验证即使无关数据表无法读取，仍能回传指定详情或 404。真 HTTP Go 测试验客户、筛选后报价与订阅，以及未筛选的帐单、付款、Credit、退款在跨页插入新数据后不重复且能走到新尾项；每个声明的筛选字段能查找，其余分页清单拒绝未知／重复参数。补充帐单没有 billing period 时，资源 API 现回 `PeriodIndex: null`，原首期仍回 `"0"`；缺少权益投影的订阅回 `EntitlementStatus: null` 与 `EntitlementReason: null`。原先的 `-1` 与空字符串会掩盖来源缺失，定向测试先重现失败再验修正。Playwright 验资源筛选条件与光标跨页、返回、重整及切换筛选的 URL 行为；另验客户无效 cursor、管理命令 21 笔以上与新命令插入后第二页不重复；帐单及对帐差异列表超过首页时可沿 UI 分页找到指定数据并打开详情。Playwright 验订阅列表的缺权益投影显示「未知」、第 0 帐期显示 `0`，以及即时升级补充帐单的缺帐期显示「未知」。其余资源的时间语意、缺来源值与跨页插入矩阵仍未全验。 添加真 HTTP 分页测试：报价与订阅在 customer_id 筛选下读取首页后插入同客户及其他客户数据，下一页只收同客户新列且不重复；客户 ID 光标在首尾两侧插入名称后，只显示光标之后的名称。测试直接确认 total 更新，符合非全域快照契约。报价列表的定向 Playwright 案例亦验首页加载后添加同客户报价、下一页与重整均显示两笔尾项，1／1 通过；其他资源仍待矩阵验收。 合约列表曾将 `EffectiveTo` 原始 Unix 奈秒显示为数字；Go 与真 HTTP 回归先重现，再将 `*_to`、`*_start`、`*_end` 时间边界与既有 `*_at`／`*_from` 一同串行化为 UTC RFC3339Nano。新测试核对合约两端时间、来源列与 API 字符串；合约列表现直接显示两端 UTC 时间，定向浏览器案例 1／1 通过。其余资源矩阵仍待验。 资源表列键改为使用用量事件、用量帐期与目录选价的实际复合识别字段；同一订阅不同帐期不再共用 React 列键，帐户迁移与旧帐单来源亦优先使用其唯一 ID。  批量任务栏表添加隔离 SQLite 插入后光标与逐项计数测试，HTTP 对无效光标、空列表和数据库故障有明确回应；真实浏览器验 22 笔工作分页、新工作插入、刷新与详情链接。 |
| A12 | 部分 | Playwright 验证报价表单必填错误、390 px 版面及手机菜单 Escape／焦点恢复；通用操作表单的价格发布确认框可由 Escape 关闭并把焦点还给原按钮，命令未写入。其余页面的详细键盘、API 错误与焦点矩阵待验。 Playwright 另验明确 400／422 拒绝后，报价与用量表单重新开放编辑，且没有添加命令。通用 C11 金融表单在有效预览后遭受控 422 拒绝时，确认框关闭、焦点回原按钮、金额栏可编辑；刷新没有残留待查意图，必填错误以 `aria-invalid` 与 `aria-describedby` 关联到输入。 Playwright 另逐一打开 58 条已知路由，以 390 px 窗口等待加载后检查整页没有横向溢出，并验行动菜单在每页都是首个 Tab 焦点；数据表内部仍可横向卷动。 同一轮路由巡检逐页检查可见表单标签确实连到同一字段的控制项，以及每个可见输入框、文本区与原生菜单皆有 label 或 ARIA 名称；修正实验控制页两个原先仅靠 placeholder 的字段。 命令详情以受控 `accepted` 回应验证持续轮询，切到概览后等待超过一次轮询间隔，没有新的命令读取；此案例只验离页停止。 价格／合约跨组件溢出的定向浏览器案例再验每席字段错误有 `aria-invalid`、`aria-describedby`，修正固定费用后错误与无效状态清除，且未送出预览。 |
| A13 | 部分 | Playwright 走通 C01／C02 购买、C05 调度取消、C06 撤销取消、C01 变更绑定报价与 C03 下期 Pro 调度；添加客户列表／详情与订阅详情，验客户预填报价、实际 Pro 五席 USD 100.00、当前帐期及调度取消后的下期状态。单笔报价／订阅详情改为目标查找，无需加载全库；订阅历史以帐期序号与事件键跨页查找。C04 另有 UI 案例。过期报价的真实浏览器案例固定业务时钟越过原报价到期日，C02 预览回 `409 QUOTE_EXPIRED`；画面保留原报价金额、指出过期原因并带原客户创建新报价；SQLite 确认零订阅、零 C02 命令。添加两张五席 USD 100.00 与七席 USD 120.00 的已绑定报价，浏览器把第二张报价 ID 与第一张的 Fingerprint 混用时，C03 预览回 `409 CHANGE_QUOTE_BINDING_MISMATCH`，画面指出错配且 SQLite 中零变更调度、零 C03 命令；Go 测试 `TestAdminChangePreviewRejectsAnotherQuotesBindingForBothModes`、`TestAdminChangePreviewRejectsSupersededPriceForBothModes`、`TestAdminChangePreviewRejectsChangedRevisionForBothModes` 分别固定其他输入，只变更席次绑定、目录选价或订阅 revision；C03／C04 均拒绝预览，没有新预览、命令、变更调度或即时升级事实。其他冲突及重播矩阵待验。 |
| A14 | 本机通过 | Playwright 以 Basic $20 → Pro 五席 $100 的 30 日帐期中点验 C04 预览 $40.00；C49 令升级付款回应遗失时，订阅仍维持 Basic。沿原 C09 命令在两天后查证，实际差额为 $34.66，C13 批量产生 $5.34 减额；SQLite 验变更状态、金额与更正来源。Go 测试 `TestAdminImmediateUpgradeAtomicAndBoundedByPreview` 与 `TestAdminImmediateUpgradePreviewAmountOnlyAllowsDecrease` 验确认上限与原子性；浏览器案例 `an immediate upgrade shows lower higher amounts business clock changes` 验预览后业务时间前进或后退皆回 409、显示新旧金额且拒绝旧确认，重新预览后才成功；`an immediate upgrade refuses preview after another operator changes subscription` 验来源 revision 冲突时保留原输入、零升级与零收据。A14 要求的差额、上限、来源冲突及付款未知期间维持原服务均有本机 Go／SQLite 与浏览器证据。 |
| A15 | 部分 | Playwright 验 C07 以 1000 最小单位部分付款取代未送出的付款操作，C47 设置确定失败，C09 派送后 C08 创建未清余额 2000 的重试；取消的旧操作不再阻挡重试。另验 C49 回应遗失后 C09 待查证、跨页及重启后原命令查证；添加 C10 对无 provider 事实的 submitted 操作保持 waiting_verification，待原 key 出现证据才完成，只有一笔 capture／收据。其余付款 UI 错误矩阵待验。 C07 专用付款表单在金额变更后移除旧预览，重新预览显示新金额；尚未确认时没有 C07 命令。 C07 预览提交被受控 HTTP 409 `PREVIEW_STALE` 拒绝时，专用页移除旧预览并解锁原金额字段；再创建预览成功，SQLite 的 C07 命令数未增加。此案例只验 UI 的拒绝路径。 添加 C07 两个真实预览的竞争：Go／SQLite 验先受理的 6000 失效、后受理的 7000 胜出，只有一笔替代操作与成功收据，原键重播与改 payload 冲突保持正确；双分页浏览器验 1000／1500 竞争的 HTTP 409、失效画面与 provider 零 capture。完整付款错误矩阵仍待验。 C08 同帐单两个已受理重试预览另以 Go／SQLite 验一笔 10000 新义务与 retry request、另一命令 `PREVIEW_STALE` 且零成功收据；两键重播原结果，新 provider key 尚未派送。已补双分页与跨进程竞争；其他错误矩阵待验。 C07／C08 各添加两个独立 Lab 实例共用 SQLite 档的同时运行测试：各 10 次皆一成功、一 `PREVIEW_STALE`，只有一笔新义务及收据；`-race` 定向通过。 C07 多进程同步竞争再连跑 10 次皆一成功、一 `PREVIEW_STALE`，唯一替代付款与收据；C07／C09 跨实例同步竞争及 C08 创建后的 C09 派送交错已验； 添加 `admin_payment_dispatch_interleaving_test.go`：C07 取消旧操作后，旧 C09 正确以 `PREVIEW_STALE` 结束；C07／C09 跨实例同步竞争依提交顺序产生零或一笔正确收款；C08 后派送新操作，原重试预览失效，核对 provider 金额、币别、帐单余额及收据。 `web/admin/e2e/admin.spec.ts` 添加 C08 双分页同时预览：胜出页创建唯一重试义务，旧页收到 409 并显示预览失效；再以 C09 派送新操作，provider 金额与操作一致。 浏览器补验旧 C09 预览与 C07 替代付款的双分页交错：旧页 HTTP 409、失效提示与不可再次确认，旧 provider key 无收款；新操作的 C09 仅收 1500。 C09 接口另验两笔待付款时先派送队列较晚的指定操作：较早操作仍为 created／pending、provider key 零 capture，指定操作成功且只有一笔收据。 两个添加 C09 浏览器案例加入后，完整 Playwright 回归 92／92 通过。 |
| A16 | 部分 | Playwright 走通已收款帐单的 C11 减额、C15 预留退款、C49 指定 C16 回应遗失与原命令查证；SQLite 核对 credit 1000、预留 500、provider 退款只有一笔及收据只有一笔。C17 添加 UNKNOWN 退款查证：Go 测试验 provider 已提交、回应遗失后的查证、同键重播、唯一收据与唯一退款；浏览器从原 C16 waiting_verification 进入 C17 查证，确认退款成功后再沿原 C16 命令完成，provider 退款仍仅一笔。添加 C12 案例：以已收款帐单减额产生的 1000 额度抵扣同客户续期帐单 500，经 UI 预览、确认及重新查找，SQLite 核对来源、目标、唯一抵扣与一笔收据；预览明示将取消的未送出付款操作数、抵扣后应付金额及重建付款提示；超过可用额度的预览被拒绝且未创建命令，帐单详情显示已抵扣金额与剩余应付。C12 浏览器案例再从 C07 预览创建剩余 1500 的新付款、由 C09 派送；SQLite 与 fake provider 核对旧 2000 操作／outbox 已取消且从未 capture，新的 provider capture 只有 1500，帐单尚待支付归零。Go 回归另验 C12 预览后额度被退款预留占用时，原命令以 `PREVIEW_STALE` 失败、不添加抵扣或收据；`TestAdminApplyCreditCancelsOldCollectionAndCapturesOnlyRemainder` 另验 C12 抵扣 500 时原续期 2000 收款操作及 outbox 被取消、provider 未收到旧金额，C07 创建 1500 剩余应付的新操作后只 capture 1500，帐单余额归零；C17 缺 provider 证据时命令待查证、500 额度不释放，原 key 后来成功才写收据。不同客户、过期帐期及争用等 UI 情境待验。 同一浏览器案例核对 C15 预留退款 500 加 C12 抵扣 500，恰好用完原 1000 Credit；旧 C12 命令失败且未重复抵扣，重新确认后的命令才有唯一收据。 C15 双分页预留额竞争另以浏览器及 SQLite 验旧预览失效、原键留下 `PREVIEW_STALE`、重新确认前只有一笔退款，以及两笔成功命令各有收据且总预留额不超过 funded grant。`TestAdminReserveRefundRejectsStaleSourceEvenWhenBudgetStillFits` 在领域交易层另验额度仍足够时也须比较来源版本，且成功／失败原键重播不增加退款或收据。 添加未知收款阻止 C11 预览的 Go／SQLite 与浏览器证据：HTTP 409，没有 C11 命令、更正或 credit grant；原收款查证后才可减额。其余并行及中断点仍待验。 C12 跨客户抵扣在预览阶段拒绝，grant 可用额与目标帐单不变；首期收款加减额刚好结清时，C11 不创建无资金 credit 并激活权益。 未付款减额后的 C07／C09 补收亦与直接领域路径比对，旧 10000 操作无 capture，新 8000 操作各只收一次；既有浏览器案例覆盖画面与 SQLite。 添加 C15 预留与 C16 指定退款派送的跨实例同步竞争：修正前 C16 可回 `database is locked`；修正后原退款 provider 仅一笔 USD 400、额度与收据依提交顺序一致，定向重跑及 `-race` 通过。 最新退款观察交易另以原 C16 回应遗失的浏览器情境定向 1／1 通过。 添加 `/admin/credits/:id` 详情，以同一 SQLite 快照区分原始、已抵扣、退款保留、已退款与可用额度；Go 测试涵盖回应遗失时保留额不释放、查证后转已退款、后续帐单抵扣，定向浏览器验列表进入与画面金额转换。 跨两个管理实例同时运行 C12 抵扣 700 与 C15 退款预留 700 的 Go／SQLite 测试通过：1000 额度只允许一个成功，另一个 `PREVIEW_STALE`；一笔收据、一笔财务事实、零 provider 退款，额度与帐单余额一致。定向 `-race -count=10` 通过。 |
| A17 | 部分 | 帐单详情在同一 SQLite 读取交易内回传应收余额、原始明细、付款操作，以及有来源 ID 的减额更正、发布 Credit、抵扣本帐单的 Credit 与来源退款；各来源清单最多 100 笔并明示截断，读取索引支持查找。四类来源另有光标历史页，Go 测试验 101 笔更正跨页及退款、抵扣分页。Go 测试核对 C11 人工命令、C13 延迟开通更正及 C14 未履行升级决议的来源关联，也验来源帐单、grant、退款与目标帐单抵扣；C11、C13、C14 同 key 重播不重复创建更正，C11／C14 的 Credit 也各仍一笔；C13 固定成员不因重播纳入后来合格的项目。Playwright 可由 C11 更正进入原命令，从 C13 更正看到升级变更 ID 与 USD 5.34，查到 500 退款由 `created` 到 `succeeded`，并走通 C14 跨帐期后查证、决议及帐单历史来源。添加 C11 未收款帐单案例：预览明示取消一笔旧付款及减额后 1500 应付；确认后旧付款与 outbox 取消、没有 Credit grant，C07 新建 1500 操作并由 C09 只向 fake provider 收 1500，订阅开通且帐单余额归零。Go 定向测试另核对相同金额、来源与 provider key。四类真实 SQLite 历史页与受控下一页／上一页操作已验；其他错误情境仍待验。 |
| A18 | 本机通过 | C13／C30／C32／C44／C45 均固定预览成员，每批最多 100 项并显示剩余候选；来源变动、时间前进与超过 100 项的 Go 案例验证不扩大原批量且后续批量可轮转取得尾端候选。五种工作均有磁盘 SQLite 逐项提交后关闭／重开的恢复证据，其中 C13 验收据写入失败、C30／C32／C44 验首项不重做、C45 验第二项来源冲突。批量收据分列成功、失败、冲突、待查证与略过；工作页另分列运行中，待查证不计入已完成进度。Playwright 对五种工作验原 request key 的回应遗失恢复，C45 另验预览后添加合格订阅仍维持固定成员、刷新后进度与错误码可见，超过 100 户提示仍需新批量。 |
| A19 | 本机通过 | C18–C21 浏览器走通价格版本、meter 与选价，价格发布预览 stale 后重建；不同 ID 占用既有版号与原 ID 改动规格都回 409，原 checksum 不变。C18／C20 的数值字段逐栏验 int64 越界、正数字段验 0，非法日期不送预览；发布后核对 SQLite 固定金额、席次、包含量、超额费率组件与 checksum。C21 同一方案、cohort、生效时间改选另一已发布价格回 409，既有选价不变。添加真实浏览器案例由 C19／C20／C21 发布 AI token meter、价格及选价，在预览显示生效时间，经 C01／C02 显示并接受 USD 30.00 报价，再经 C26 记录 105 token；另一个 Pro 订阅同时能记录 tasks。`TestP01AITokensSKUUsesGenericMeteredPath` 另验 token 用量结算与原 tasks 路径。有效极值与跨组件数值政策的剩余矩数组于 A10，完整套件仍依 A30 统一验收。 |
| A20 | 本机通过 | 浏览器走通 C22–C25 迁移规划、固定成员、略过、暂停与恢复。隔离 SQLite／fake provider 案例先用 C18／C21 发布并选用新 Pro 价格，C22 固定一笔订阅、C44 在第一个续约边界套用迁移，帐单为 11000 最小货币单位；完成该期收款后，C21 选回旧价格并以新的 C22 批量反向迁移，下一次 C44 产生 10000 的帐单，当前价格回到 `pro-v1`，指派历史三笔且来源为反向批量。添加 `/price-migrations/:id` 详情页，逐户显示原价、目标价、预期 revision、金额、状态及持久化的冲突原因；旧数据原因缺失会明示。另一个真实浏览器案例在批量计划后改动其中一户 revision：C44 只套用未冲突户，批量暂停；详情页说明原因，C24 明确略过后再运行 C44，两户首期续约帐单分别为 11000／10000，已套用价格没有被暂停抹除。Go 测试验既有 `price_migration_items` 表可加字段并保留数据，重跑升级不重复变更。添加领域测试验 C03 下期变更与 C05 调度取消在迁移项目待处理时于预览阶段即回冲突，正式入口亦拒绝且不创建调度；C03／C05 浏览器情境在 pending 状态均验 HTTP 409、没有确认入口，SQLite 中预览及调度皆为零；C05 另在批量暂停、项目 conflicted 后验 409 及零调度；若预览先于迁移规划创建，提交后运行会以 `PREVIEW_STALE` 失败，调度与迁移状态均不被改写。隔离 SQLite 领域测试另外仿真来源价格、席次与下期调度在规划后变动：批量暂停并持久化对应原因，续约不添加帐期、帐单或价格指派。另以隔离 SQLite／fake provider 的真实浏览器矩阵创建三笔各自固定成员的迁移，于续约前分别改动来源价格、席位数与下期调度；C44 后三个批量均暂停，详情页逐一显示持久化原因，且各户没有添加帐期、帐单或价格指派。订阅 schema 仅允许 `pending`／`active`，管理流程只会将已付款订阅由 `pending` 激活，迁移规划要求有效订阅；故 `subscription_inactive` 是防护分支，没有本 MVP 可操作的 `active` 退回 `pending` 情境。A20 的正常户与可发生冲突户依设计验收通过。 |
| A21 | 本机通过 | 浏览器走通 C26–C29 原始事件、关帐、晚到重算与撤销；续约帐单已付款后，负差额重算经 C30 产生 1 分 Credit Note 和 1 分 funded credit，原事件不变。C26 真实浏览器案例另验相同来源与事件 ID 在新管理命令中以相同 payload 重送仍只保留一笔用量；改变量量的重送显示 `failed/DOMAIN_REJECTED`，原事件数量保持不变。添加用量帐期详情读取、estimated/finalized/rerated 状态、估算与最新 rating 的精确计算来源、依 revision 分页的 rating 历史，以及用量事件帐期筛选；真实 SQLite 浏览器在关帐前、关帐后、晚到重算与 Credit Note 后逐段验证。Go 测试验空帐期估算、修订跨页、错误光标、401／400／404。更广的错误状态 UI 矩阵归入 A30 综合覆盖盘点。 C26 的浏览器回应遗失案例验服务器已写入原事件时，页面刷新后仍锁住原意图，沿同一 request key 查回，SQLite 只有一笔事件与命令。 |
| A22 | 本机通过 | 浏览器走通 C31 发布合约、C01 合约报价、C02 接受与 C32 到期收款；SQLite 验五席 $75、首期 Net30 到期前无 capture outbox。固定时钟的第二个案例验证缺后续价格时 C44 暂停续约、不创建下一帐期，订阅详情以中文显示暂停原因，并保留 `contract_next_price_missing` 供追查。合约双重权限与服务开通另有 Go 测试。 |
| A23 | 部分 | 对帐、修复与人工决议的 domain 测试通过；Playwright 以孤儿服务商收款创建差异，核对 expected／actual／证据／对帐运行并记录人工决议，再由 SQLite 确认唯一决议。对帐差异列表在计算是否需翻页前等待数据表加载；测试以延迟 API 固定重现原先的加载竞态，修正前失败、修正后通过。对帐运行详情已支持差异光标分页、当次不可变 expected／actual 快照与目前状态分离；旧数据补录值明确标为 `backfilled_current`。Go 测试证明后续对帐不覆写旧运行证据，并验 v4→v5 索引升级。 C34 浏览器情境由已付款且曾刷新权益的订阅删除测试投影，对帐后确认为 `SAFE_AUTO_REPAIR`；详情页将来源 revision／证据带入修复表单，确认后 SQLite 仅一笔修复操作与命令收据、差异变 resolved，刷新仍找回原命令。操作保留原 expected／actual，状态 `verified`，verification 指向新的对帐结果。另验 C34 预览后订阅来源 revision 变动：权益未重建，修复操作为 `blocked`、命令只有一笔收据；UI 在命令状态 `succeeded` 旁明示修复未运行与 verification 原因。服务商收款与本地帐单金额不符时，对帐分类为 `MANUAL_REVIEW`；C34 仅记录 `blocked`，UI 可返回差异详情，C35 留下人工决议。浏览器以 provider SQLite 核对原金额与总收款笔数不变。其他 blocked 原因及差异类别的完整 UI 矩阵仍待验。 |
| A24 | 本机通过 | C36–C43 的 Go 领域测试及浏览器路径覆盖链接、shadow、历史来源回填与人工确认、读写切换及停止；帐户详情显示 owner、比对、来源、事件与 Readiness。付款回应遗失时 UNKNOWN 阻挡读取切换；沿原 C09 命令查证、更新权益与 shadow、重新对帐后才允许 C41／C42，且各有收据。shadow／来源完整历史另有光标分页页面与 API。  C43 切写后停止的真实浏览器案例证明 read／writer owner 不回退、停止事件与命令收据各一笔；其后 C05 预览回 409 `ACCOUNT_MIGRATION_STOPPED`，没有 C05 命令或取消调度。领域测试另验预览创建后才停止时，原命令失败且没有金融写入。 C42 切写后回应遗失的浏览器故障注入改为以相同 request key 另送受控请求、确认提交后中止原浏览器请求；原先 `route.fetch()` 后再 `route.abort()` 在完整套件中偶发 route 已处理错误。改写后定向重复 3／3、完整套件 89／89 通过。 |
| A25 | 本机通过 | 时钟持久化、付款／退款决策与一次性故障测试；Playwright 验 C49 付款及退款回应遗失的票据各只一笔，且各绑定原 C09／C16 命令。真实本机浏览器案例验 `crash_after_provider`：付款与退款各经 UI 创建一次性票据、由指定操作派送，provider fact 已提交而原命令回 503；故障票据绑定原命令，刷新后继续同一命令，provider capture／refund 仍各只一笔。退款在查证前保留 500 最小单位预留额，完成后转为已退 500。添加浏览器 C48 案例：先设置假退款结果为成功，派送前 provider 仍没有退款；C16 回应遗失后以 C17 查证，原 C16 命令沿原键完成，provider 退款保持唯一。另一个 Playwright 案例在 C09 等待 provider 查证时经 C46 接口把固定时钟前进一天：调时没有自动创建 allocation 或第二笔 capture；从命令详情查证后，命令仍使用接收时的 business time 开通第一帐期，provider capture 与命令收据各只有一笔。Go `TestAdminFaultTicketOnlyAffectsItsSelectedPayment` 验另一笔付款先派送仍不会领取指定票据，该付款正常成功，指定付款才进入待查证并沿原命令完成。`TestAdminPaymentDecisionRecoversAfterProviderCommitAndCapture` 验 C47 的 provider 决策先提交、本地命令尚未完成，期间付款已 capture；重启后沿原 control receipt 完成命令，provider 决策、capture 与管理收据各只有一笔。`TestAdminBusinessClockChangeDoesNotExpireWorkerLease` 验持有者取得 lease 后把商务时钟跳至 2040 年，第二个 SQLite 实例仍不能接管未到期的 wall-clock lease。 `TestBusinessClockChangeDoesNotChangeSessionDeadline` 验同样的商务时钟变动不改变 session 闲置期限；`TestAdminFaultTicketOnlyAffectsItsSelectedRefund` 补付款以外的指定退款隔离。实验控制页现常驻标示本机假付款环境，并显示目前时钟与最近故障票据；更多故障／权限交错排列留在 A30 的逐动作矩阵，不影响 A25 已列情境的本机验收。 |
| A26 | 部分 | Playwright 验付款／退款命令在等待查证后页面刷新可找回；服务重启使旧 session 失效，重新登录后可用 /commands/:id 读取原命令并完成查证；退款可从命令行表跨页恢复，列表已支持光标寻找旧命令，provider 仍各只有一笔 capture/refund。C11 浏览器回应遗失案例在服务器已提交更正后中断回应；刷新仍保留原 request key，只查回原命令，SQLite 核对唯一更正、funded grant 与收据。更多写入情境的浏览器逾时及批量逐项中断恢复待验。 C01 与 C26 的浏览器回应遗失案例先由服务器完成写入再中断回应；刷新后保留待查意图，只能用原 request key 查回，没有第二笔命令。 添加真实浏览器逾时案例：C01 已在 SQLite 创建命令后，浏览器的原回应被保留至 20 秒写入期限；UI 保留原 request key，刷新后查回原命令，总数仍只有一笔。C02／C07 另验服务器已完成写入、浏览器回应被中断后刷新，接口沿原 request key 找回原命令；C02 订阅与帐单、C07 付款操作各只添加一笔。C45 批量刷新权益另验服务器已创建命令、工作与预览时固定的逐项成员后中断回应；重新加载沿原 request key 找回同一命令，工作与逐项成员均没有第二份，并可打开原工作详情。C44 另用独立的既有数据库验到期续约工作：服务器创建一张 $20 续约帐单及一笔付款义务后中断浏览器回应，重新加载沿原 request key 找回命令；重播后工作、成员、帐单、付款操作及命令收据各只有一份，provider capture 数量不变。C13／C30／C32 也已验回应遗失后重新加载、沿原 key 找回同一批量工作，分别核对更正、Credit Note、capture outbox、逐项成员与收据未重复。 受控浏览器案例另验 503 `COMMAND_PENDING_RETRY` 带原 command ID 时，操作页保存该 ID，刷新后直接加载 `accepted` 命令；命令详情可沿原 ID 继续，成功后原表单提交次数仍为一。此案例验 UI 回应处理；provider 故障与 HTTP 证据另列 A28。 另以真实 C49 `crash_after_provider` 的 C09／C16 浏览器案例验 503 后原 command ID 被保存；重整页面后继续原命令，付款 allocation 与退款状态各只完成一次。  C42 切写另验服务器已提交后浏览器回应遗失：页面刷新保留原 request key，查回同一命令；SQLite 中 C42 命令、成功收据与 `writer_cutover` 事件各一笔。 |
| A27 | 本机通过 | 同规模合成数据的 C45 首批／轮转后 100 户预览单次 DB 量测分别为 3.67／3.10 ms（非 HTTP P95）；10,000 笔订阅及 100,000 笔既有用量事件下，五个 HTTP client 与一个真正的 `RecordUsage` worker 同时启动。最近一次 100 次用量列表读取 P95 85.70 ms、最大 130.42 ms；100 笔订阅／用量／客户回应分别为 22,463／29,493／7,004 bytes，均低于 1 MiB。worker 提交并核对 20 笔新事件，耗时 617.32 ms；前次同设置 P95 92.47 ms。benchmark 会直接拒绝超过 500 ms 的 P95 或不足 20 笔的 worker 进度。环境为 Intel Xeon Gold 6133／Go 1.27.1；列表使用有界分页，worker 使用短交易，没有全量 State dump 或长时间 provider 交易。概览与列表的受控 503、10 秒读取逾时会保留最后成功数据并明示旧结果，成功重读后警示消失。此状态只表示 A27 所列本机门槛已验，非跨环境性能承诺。 |
| A28 | 部分 | Playwright 验 fake provider 回应遗失及 C10 查无终局 provider 证据时的 `waiting_verification`；C10/C17 Go 回归验没有成功收据、保留原操作及退款预留额度。概览与 44 条资源／详情／故障票据读取路径在 DB 关闭时均回 500 `QUERY_FAILED`，不伪装成空数据或 404；预览及工作不存在时另回 404。C01 已验受理前写入失败、受理后收据写入失败、运行中断线与成功后回应遗失的原键恢复；C15 已验收据故障时额度预留回滚与原键恢复；C17 已验 provider 暂不可查时退款预留与原命令恢复；其他动作的中途断线与 DB 故障矩阵待验。新请求另有服务器产生的 request ID，C01 真登录案例已核对错误回应、命令、稽核及详情页的 ID 关联与秘密排除。 C01／C26 以真实 HTTP 422 验证明确的入场前拒绝与网络回应遗失被区分：前者解锁表单且命令数不变，后者保留原意图供查证。 C07 另以受控 HTTP 409 验入场前预览失效后可重新预览，未知或 5xx 仍保留原 key；此切片没有宣称完整 client 断线矩阵。 Playwright 另验 503 与真实 fetch 逾时的区别：保留旧表格但标示失败，没有把旧数据当作新结果；C01 写入回应逾时维持原 key 查回。完整 DB 错误与断线矩阵仍待验。 关闭 SQLite 连接后，以同一 HTTP handler 验证概览、订阅与用量列表、客户列表、报价／订阅／帐单详情均回 500 `QUERY_FAILED`，不会把读取故障伪装成空列表或 404。 领域回归另以持久化 fake provider 分别完成付款与退款后遗失回应，暂时关闭 provider 查找：两者都维持 `unknown`，付款无本地 allocation；恢复 provider 后沿原 key 反复查证，付款只入帐一次，provider capture 与 refund 各仅一笔。 同一故障测试还走 C10／C17 管理命令：provider 不可查时原命令维持 `accepted`、没有成功收据，恢复后以原命令完成；同 request key 重播仍回原命令，收据各只一笔。真实 HTTP／SQLite 故障测试以独占锁令 provider 查找逾时，C10 提交回 503 `COMMAND_PENDING_RETRY`、`retryable=true` 与非空原 command ID；原命令仍为 `accepted`，没有收据或 allocation。释放锁后用相同 request key 完成及重播，回同一 ID 且只有一笔收据与 allocation。修正前回应的 `command_id` 为空，修正后通过。 服务器修正后的非空 command ID 现在被 React API client 保存，操作页和命令详情提供已入场 `accepted` 命令的「继续原命令」入口；浏览器以受控 503、重整及恢复回应验证没有第二次表单提交。 真实浏览器与 SQLite 再验 provider 已提交后服务中断：C09／C16 均回带非空原 ID 的 503，UI 没有声明成功，待原命令查证后才显示完成；provider 的收款及退款均只一笔。  添加 30 条主要清单、详情与历史读取路径的关库错误矩阵，均回 500 `QUERY_FAILED`；用量帐期页另以真实浏览器验 503 后保留旧估算与修订数据、显示更新失败警示，成功重读后清除警示。 添加的 Credit 详情已纳入关库读取错误矩阵：数据库故障回 500 `QUERY_FAILED`，不同于不存在 grant 的 404。 |
| A29 | 本机通过 | Playwright 以新数据库建置并启动嵌入式 binary，验登录、概览、注销与 58 个路由；付款等待期间重启同一 binary／SQLite，重新登录后找回原命令并查证。另一个案例先由 CLI binary 创建既有帐单与 provider capture，确认其没有 admin schema，再启动 admin binary 升级至 v6；登录后仍可查看原帐单，金额与 provider capture 不变。纯 API binary 已手动验证；README 提供建置与登录指令。 |
| A30 | 部分 | [49 个动作证据盘点](web-admin-action-audit.zh-CN.md)列出实际共用 endpoint、逐项 UI 路径、能力、预览型态及交易测试入口；命令与预览的 HTTP admission guard、命令未知字段拒绝与零写入已逐项验证，Playwright 已验 58 个路由。C12 另有完整浏览器抵扣及 SQLite 收据证据。49 项动作现均在盘点表列有浏览器测试文件引用；仍需逐项核对情境是否真正运行该动作，以及每动作的收据／恢复、其余输入与冲突矩阵，A30 维持未结案。 C15 添加真实双分页预览失效与重确认案例，直接核对两笔成功收据、单笔失败原命令及 grant 预算。  C42 的浏览器案例现直接运行切写并验回应遗失后原键恢复，核对唯一命令、收据与切换事件。  C43 已补真实停止与后续 C05 阻挡证据，并区分帐户已停止与预览来源变动的错误。 C07／C08／C09 已增付款创建与派送交错的跨实例 Go／SQLite 证据；这不取代其他动作缺少的完整矩阵。 |

### A13／C01 变更报价绑定的原子性（2026-09-29）

`TestAdminCreateChangeQuoteRollsBackWhenBindingRevisionIsStale` 在 C01 命令已受理后，以过期的订阅 revision 触发绑定失败；命令以 `CHANGE_QUOTE_REVISION_CHANGED` 结束，该交易创建的报价未留下，亦无变更绑定或成功收据。Playwright 的 `a stale subscription revision rolls back a new change quote and binding` 以真实表单与 SQLite 重验同一结果，并确认画面显示 revision 原因与「报价 ID 尚未产生」。

C03／C04 的 Go 测试分别验选价被取代与订阅 revision 变动时回传可辨识的冲突，且不创建预览、命令或变更事实。两条浏览器方案变更路径在竞争者改动订阅后重新预览，均收到 `409 CHANGE_QUOTE_REVISION_CHANGED` 并显示修正方式。另一浏览器案例先以 C18／C21 发布并选用新 Pro 价格，再将业务时钟推到生效后；旧绑定报价的 C03 预览收到 `409 CHANGE_QUOTE_PRICE_SUPERSEDED`，画面要求依新金额重新报价，SQLite 确认零 C03 预览、命令与变更调度。A13 其他冲突及重播矩阵仍待验。

### A08／A26／A30：C03／C04 回应遗失与原键恢复（2026-09-29）

Playwright 以真实浏览器提交 C03、C04，让服务器以独立连接完成命令后中断原浏览器回应。刷新后两个页面仍保留原 request key，按「用原 request key 查找」取得同一成功命令；同键改变报价 ID 的重播回 `409 IDEMPOTENCY_CONFLICT`。SQLite 确认 C03 只有一笔变更调度与收据；C04 只有一笔立即变更、帐单、付款操作与收据，付款前订阅仍维持 Basic。两个定向浏览器情境均通过，其余动作的回应遗失与重播矩阵仍待验。

### A06／A08：C03 收据失败回滚与重启恢复（2026-09-29）

`TestAdminScheduledPlanReceiptFailureRollsBackAndRecoversOriginalCommand` 在 C03 运行交易的收据插入点注入 SQLite 失败；原命令保持 `accepted`，变更调度、订阅 revision、付款操作与 provider capture 均不增加。关闭并重开两个数据库后两次恢复，只有一笔调度和成功收据；同键同 payload 回原命令，同键改 payload 被拒绝。定向 Go 测试 1／1 通过；其余动作的收据故障矩阵仍待验。

### A11／A24／A28 客户与历史页读取故障（2026-09-28）

`web/admin/e2e/remaining-detail-read-failure.spec.ts` 三个定向情境通过。客户详情以 CLI demo 的真实 SQLite 订阅来源验更新故障：503 保留并标示旧客户数据，停用创建／接受报价入口；重试成功后恢复，403 隐藏旧数据。帐单更正历史与迁移 Shadow／来源历史用固定 HTTP 回应验 UI 状态：503 保留精确金额或来源记录并停用旧光标的下一页，来源人工处理入口亦停用；403 隐藏既有记录。这是页面故障行为证据，不能代替历史数据的领域正确性与逐动作恢复验收。

### A24／A28 帐户迁移详情读取故障（2026-09-28）

`web/admin/e2e/account-migration-detail-read-failure.spec.ts` 由真实 C36 创建帐户映射，定向 1／1 通过。详情更新回 503 时保留并标示上次 owner 与来源数据，停用 C37–C39、C41–C43 的入口、Readiness 计算与权益查找，且不显示旧 Readiness 的「可切换」结果；重新读取成功后恢复可用入口。回 403 时隐藏旧帐户数据与切换操作。这只验证迁移详情页的读取状态与入口控制，A24／A28 的完整来源与故障矩阵仍待核对。

### A23／A28 对帐详情读取故障（2026-09-28）

真实 SQLite／fake provider 创建 21 笔孤儿收款，从浏览器运行 C33 后进入对帐运行与差异详情。`web/admin/e2e/reconciliation-detail-read-failure.spec.ts` 定向 1／1 通过：更新回 503 时保留并标示上次成功读取的 20 笔差异与 expected／actual，停用依过期光标进入下一页、C34 修复及 C35 人工决议入口；重试成功后恢复，403 时隐藏旧证据与操作。这补强 A23 的证据可见性与 A28 的错误分类；其他差异类别、阻挡原因与读取故障矩阵仍待逐项验收。

### A30 浏览器命令与收据关联（更新至 2026-09-29）

2026-09-29 完整 Playwright 回归通过（155／155，38 个测试档）。本次产生的 104 笔浏览器情境通过 `audit-action-cases.mjs` 稽核：C01–C49 全数有浏览器请求、以幂等键关联的成功命令及最终收据（49／49）。A30 其他逐动作恢复证据仍待补齐。

以完整 Playwright 回归（132／132）产生的 93 笔管理情境稽核档，逐项核对 C01–C49 的浏览器请求幂等键、成功命令及收据，结果 **49／49**；反例数据证明同一测试中不相关的 API 成功命令不再被算入。详见[逐动作证据盘点](web-admin-action-audit.zh-CN.md)。这补足浏览器入口与收据的关联，尚未完成其余逐动作来源守卫、错误与恢复矩阵，A30 保持部分完成。

### A23／C34 对帐修复的付款范围（2026-09-28）

`RepairDiscrepancy` 对 `provider_amount_mismatch` 的阻挡依付款操作 ID 判断；`pending_outbox` 先由 outbox 取得原付款操作 ID。同一付款金额不符时，原操作查证与 outbox 重试都维持 `blocked`，不增加支付服务 capture。另一付款金额不符时，原付款可沿原 key 查证或重试，差异仍留待人工处理。`lab/reconciliation_test.go` 的三个定向案例与 `go test ./lab -run '^TestS12' -count=1`（7／7）验证此范围。这只补强直接领域路径的证据；A23 与 A30 的完整 HTTP／UI 逐项验收仍未签核。

### A28 读取故障分类补充（更新至 2026-09-28）

价格迁移、帐单、订阅、Credit、付款与退款详情的浏览器回归另验暂时性更新失败：保留并标示上次成功读取的数据，停用依旧状态发起的写入入口；价格迁移的逐项查找失败时也停用下一页光标。重试成功后操作恢复。若重新读取回 403，旧财务内容立即隐藏，改显示权限错误页。新价格迁移项目端点的真实 SQLite／HTTP 测试另验不存在回 404、错误查找回 400、数据库故障回 500，而非空列表。

缓存读取的权限边界也延伸到价格与合约版本、命令行表与抽屉、命令详情、批量工作及 fake provider 纪录；403／404 不再沿用旧内容。合约报价入口与命令、工作、provider 列表的下一页光标在暂时性读取错误时停用。定向浏览器案例验价格与合约详情的 503／403、命令抽屉权限收回后不显示结果参照、fake provider 退款权限收回后不显示旧纪录，以及暂停迁移页的 403 隐藏旧状态。

`TestFinancialReadsReportDatabaseFailureInsteadOfEmptyOrMissingData` 现涵盖全部 19 种通用资源列表，以及客户、订阅、帐单、用量帐期、Credit、对帐、迁移、命令、预览与批量工作等详情，共 44 条关库读取路径。每条均须回 500 `QUERY_FAILED`，不能把数据库故障当成空列表或数据不存在。`TestPreviewAndJobReadsDistinguishMissingRecordsFromDatabaseFailures` 另验数据库正常时不存在的预览与工作回 404 `NOT_FOUND`。实验时钟读取使用已加载的内存状态，因此不纳入关库矩阵。 `TestAdminCreateQuoteAtomicReceiptAndReplay` 另核对成功命令的稽核事件可链接 actor、幂等请求键、命令 ID 与结果 quote ID。 真实浏览器登录后运行 C01，从 SQLite 稽核事件确认 actor／结果引用存在，管理密码、内部 token、CSRF 与 session cookie 均未写入稽核 JSON。这只结案读取故障分类切片；A28 的写入中断、稽核字段及各类外部暂不可查情境仍维持部分完成。

A18 批量上限补充：C32 合约收款以 101 笔到期操作的 Go 案例证实，首批固定 100 笔并显示 `has_more_candidates=true`；光标查找先选第 101 笔，运行成功后下一批只取剩余 1 笔且旗标转为 `false`。C13 更正以 101 笔真实付款变更验证：首批 100 笔因来源变动而维持 pending，新预览从第 101 笔开始，再轮回冲突项。C30 以 101 个用量帐期的合成来源验证相同轮转；既有单笔案例确认旗标为 `false`。五种批量操作的接口共用剩余候选提示；C45 的 Playwright 案例确认提示可见。

A18 逐项恢复补充：C32、C44、C30 添加磁盘 SQLite 关闭／重开案例，先让第一个工作项目完成，再由 `AdminResumeAccepted` 运行剩余项目。C32 的 capture outbox 两项各一笔且命令收据一笔；C44 的两户下期帐单及付款义务各一笔；C30 的两个用量帐期各一笔 Credit Note、已抵额各为 1 最小单位。C13 已有收据插入失败后重开恢复案例；C45 已有首项提交后重开、第二项来源变动标成冲突的案例。服务器关闭重开由 Go／SQLite 验证，浏览器则验五种工作回应遗失后沿原 request key 恢复。

C32 固定成员另有受控时钟案例：第二笔合约操作在预览时尚未到期，预览后才到期；运行原命令只产生第一笔 capture outbox，第二笔必须由新预览选入。这验证批量使用预览时的到期界线，不因确认前时间前进而扩大成员。

批量命令收据现在分别记录 `succeeded_count`、`failed_count`、`conflicted_count`、`waiting_verification_count`、`skipped_count`。Go 回归把五种项目状态各放入同一工作，验证各计数为 1 且工作总状态为 `partial`；失败与待查证不再被误算成冲突。工作详情页持续分列六种状态，待查证不计入已完成进度。

### A21 用量帐期读取与差额追查

- `GET /admin/api/usage-periods/{subscription_id}/{period_index}` 在同一个 SQLite 读取交易中提供帐期状态、目前估算或最新已保存计价、帐务套用额与 Credit Note 汇总。未关帐估算只计入读取时已收到的事件；接口明示它并非已列帐或已收款金额。
- `GET /admin/api/usage-periods/{subscription_id}/{period_index}/ratings?limit=20&cursor=…` 依 revision 由新到旧分页，光标绑定订阅与帐期；跨帐期、无效光标及超出限制的请求回 400。每笔记录保留用量、包含量、超额量、精确分数、舍入总额与相对前次差额。
- 订阅页的目前及历史帐期、用量帐期列表都能进入详情。详情可跳到以订阅及帐期筛选的原始用量事件；筛选值只会当作绑定参数，非整数帐期被拒绝。
- 帐期或修订历史刷新失败时保留上次成功读取的数据，显示个别失败警示与原数据查找时间；再次读取成功后清除警示。真实浏览器以 503 回应验证两项读取，没有把旧数据显示为新结果。
- 定向验证：`go test ./lab -run TestAdminUsagePeriodDetailEstimateAndRatingHistory`、`go test ./api/admin -run TestUsagePeriodDetailRoutesValidateAndProtectReads`、Playwright `usage keeps the original event…` 真实 SQLite 案例均通过。完整 `go test ./...` 回归 883 个测试通过，完整 Playwright 回归 79 个案例通过；更广的错误状态 UI 矩数组入 A30。

## 恢复查证补充

### C13、C30、C32 批量回应遗失

三个浏览器案例现在都会在服务器完成命令后中断原 HTTP 回应，重新加载页面，再以原 request key 查回同一命令。C13 核对延迟升级更正的工作、变更逐项成员、USD 5.34 更正及收据各一笔；C30 核对用量 Credit Note 工作、帐期逐项成员、Credit Note 及收据各一笔；C32 核对 Net30 到期收款工作、付款操作逐项成员、capture outbox 及收据各一笔。三者在重播后没有第二份财务数据。加上既有 C44、C45 案例，五种批量工作的「已提交但浏览器遗失回应」路径均已有真实浏览器与 SQLite 证据；这不涵盖每种逐项中断点或长时间 worker 恢复。

### C14 未履行升级决议回应遗失

另一个浏览器案例在 C14 已创建更正与 Credit 后中断回应。重新加载后，接口沿原 request key 找回命令；SQLite 确认原命令、收据、决议及来源 Credit 各只有一笔，原有帐单来源与历史页仍可查。此修改后的 C14 定向测试 1／1 通过；上述 63 项完整回归是在加入此 C14 情境之前运行。

`TestAdminRepairLookupWaitsUntilOriginalProviderEvidenceArrives` 先重现 C34 对 provider 查无终局证据时，repair 为 `waiting`、command 却误写 `succeeded` 的情况；修正后 command 维持 `waiting_verification` 且没有收据。再次查不到证据仍保持等待；原 provider key 的证据出现后，同一 repair 与 command 才完成，收据只有一笔。

受限 session 可用原命令的「重新查证」或「检查既有收据」调用 `AdminVerifyExistingObligation`。它只允许查证已提交的付款／退款、原操作的 C10/C17 对帐、已在等待或完成的 C34 修复，以及有 provider control receipt 的 C47/C48；`created` 的付款、仅有 `planned` 的修复和没有 provider control receipt 的操作不会由此路径派送。HTTP 仍要求 session、Origin 与 CSRF。此路径的受限权限测试见 `TestReadOnlySessionCanVerifyHeldReceiptWithoutStartingOperation`。

添加的 Playwright 暂停命令案例使用受控 API 回应检查警示、查证按钮与状态更新；它只验 UI 呈现。provider 无新派送与命令状态的判定由上述 Go／SQLite 测试验证。

`TestRevokedRefundDispatchVerifiesExistingProviderResult` 验退款 provider 已提交、回应遗失而使 C16 等待查证时，撤销能力后的恢复进程仍沿原 provider key 取得已存在的结果，将原命令完成；provider 退款与本地命令收据各只有一笔。`TestRevokedRefundWithoutProviderEvidenceRemainsWaiting` 仿真本地标为 `submitted` 后、provider 收到请求前中断；撤销后恢复及重复查证皆保持等待，不产生退款或收据。`TestExpiredAdminLeaseCannotStartRefundDispatch` 另验失去 lease 的退款派送遭拒，撤销原命令后受限查证不能启动未提交的退款，两个数据库均无新外部效果。

回归结果（2026-09-27）：最新 `go test ./... -count=1 -timeout=180s` 全部套件通过，`go vet ./...` 通过；跨实例 C12／C15 额度竞争另以 `go test -race ./lab -run TestAdminCreditApplicationAndRefundReservationCompeteAcrossInstances -count=10` 通过。价格极值修正前最近一次完整 `pnpm --dir web/admin test:e2e` 为 94／94 通过，包含 Credit 详情、指定退款派送、命令恢复与金融来源状态。根 Go 模块验证涵盖 `api`、`cmd`、`lab`；`docs/internal` 的文章实验档由独立模块隔离。A01–A30 尚缺的逐项矩阵仍以各列状态为准。
价格组件极值修正后：`go test ./... -count=1 -timeout=180s` 全套及 `go vet ./...` 通过；`TestPublishedPricesRejectUnrepresentableMinimumUpfront` 先重现可发布却无法以一席报价的价格，再验三种发布路径拒绝合计溢出与有效上界。HTTP `TestPriceMinimumUpfrontBoundaryAtHTTPPreview` 验 C18／C20／C31 的 422、有效上界与零命令。更新后的价格／合约边界浏览器案例 1／1、既有 Pro／计量价格发布与企业合约浏览器案例 3／3 通过；其后完整 94 案例已重新通过。
合约列表 UTC 边界与复合列键修正后：`go test ./... -count=1 -timeout=180s`、`go vet ./...` 通过。完整 `pnpm --dir web/admin test:e2e` 94／94 通过；最初一次完整重跑的 C21 案例因立即检查尚未加载的「运行另一个操作」而失败，改为等待按钮后定向 2／2、完整 94／94 通过。最后前端列键及合约列表字段调整后，再以最新资产定向验企业合约与用量帐期案例 2／2 通过。

本轮另验 C02／C06 停止后的预览与旧意图：`TestStoppedCommerceWriterRejectsAcceptQuotePreviewAndAcceptedIntent`、`TestStoppedCommerceWriterRejectsResumePreviewAndAcceptedIntent` 均验证专属停止原因、原键找回、零成功收据与零新商务事实；C02 的 Playwright 帐户迁移案例同时验画面拒绝、HTTP 错误码及 SQLite 零订阅／命令。`go test ./... -count=1`（909 个测试）、`go vet ./...`、`pnpm --dir web/admin build` 与定向 Playwright 案例均通过；完整浏览器套件仍以最近一次 76／76 的纪录为准。

C27–C29 另以真实 SQLite 测试预览后来源变动：反向调整的剩余量遭并发耗用、关帐前添加符合 cutoff 的事件、重算前添加晚到事件，旧命令均为 `failed/PREVIEW_STALE` 且没有成功收据或多余修订；新预览才套用最新用量。`web/admin/e2e/usage-preview-stale.spec.ts` 以双分页验 C27–C29 的 HTTP 409；C27 原事件剩余量不足时，新预览明确失败，改填剩余量后才成功，旧调整不落库。C28／C29 画面分别显示用量 20000→20010 与 20020→20030；重新确认前不添加计价修订，确认后各有唯一收据，关帐及重算结果分别为 1 与 3 最小单位超额费。最新 Go 回归 912 个测试及这个定向浏览器案例通过；全套浏览器测试尚未在此轮重跑。

C31／C32 补充来源竞争验收：合约发布预览后，另一操作者以相同 ID 发布相同内容时，原命令只增加自己的唯一收据，不添加合约版本；若发布不同内容，原命令为 `failed/PREVIEW_STALE`，不能覆写已发布金额，重新预览会拒绝旧内容。双分页 Playwright 验 C31 的 409、画面失效提示、既有 5000 最小单位合约与唯一成功收据。C32 预览后若另一收款器先创建 capture outbox，原批量将该项记为 `conflicted/SOURCE_CHANGED`，outbox 仍只有一笔；同键重播只找回原命令。

C35 添加预览来源变动验收：另一审核者在预览后先记录决议，原命令回 `failed/PREVIEW_STALE`，没有第二笔决议或成功收据；双分页浏览器显示 `open → investigating` 并要求再次确认，确认后才添加决议。既有完整浏览器套件 81／81 通过；C35 案例是在该次套件启动后添加，另以定向 Playwright 运行通过，尚未计入 81 项。

C39／C40 历史来源审核添加来源竞争测试：同一 legacy invoice 在 C39 预览后先由不同映射回填时，旧命令为 `failed/PREVIEW_STALE`，既有来源与状态保持不变，没有成功收据。C40 预览后另一审核者先完成决议时，旧命令亦失效，只有一笔 `provenance_resolved` 事件；已完成的来源不能重新创建决议预览。`web/admin/e2e/provenance-preview-stale.spec.ts` 另以双分页运行 C39／C40：两次旧预览均回 409，画面拒绝沿旧来源重建预览；SQLite 保留先写入的映射与唯一决议事件／成功收据。

C41／C42 补充准备度变动验收：预览后添加不一致的报价 shadow，原切读或切写命令均为 `failed/PREVIEW_STALE`，没有成功收据；切读前的 owner 全留 legacy，已切读但尚未切写的帐户则保留 `read_owner=commerce`、`writer_owner=legacy`。`web/admin/e2e/cutover-readiness-stale.spec.ts` 以双分页验 C42 的 HTTP 409、画面拒绝重新创建不合门槛的预览，且没有 `writer_cutover` 事件。

C35、C39／C40、C42 三个新浏览器案例另以同一命令组合运行，3／3 通过；最近一次完整浏览器套件仍为先前的 81／81，尚未包含这三个后加案例。

C45 权益刷新添加逐项来源变动验收：批量预览固定两户后，其中一户由另一操作改变订阅 revision；原批量只将该户标为 `conflicted/SOURCE_CHANGED`，不为它重建权益，另一户仍成功，批量只有一笔收据。`web/admin/e2e/entitlement-batch-source-stale.spec.ts` 以双分页真实运行 C05 与 C45，于工作详情分别看到冲突与成功项，并以 SQLite 核对权益及收据。

目前 Playwright 列出 85 个案例；上次完整套件验收为 81／81，之后加入的 C35、C39／C40、C42 三案合跑 3／3，C45 定向案例另行通过。85 案尚未整套重跑。

## A10 数值与 UTC 边界补充

`TestAdminUTCRejectsPrecisionLossAndUnixNanoOverflow` 先重现 Go 时间解析接受第 10 位小数秒并截断，以及接受超出 `int64` Unix nanoseconds 范围的日期；修正后 C18/C20/C21 价格时间、C26 用量事件、C28 截止时间、C31 合约期间、C33 对帐时点与 C46 固定时钟共用精确 UTC 验证。允许 0–9 位小数秒及可表示的最小／最大 nanosecond，拒绝非法日期、偏移时区、过度精度及溢出日期。

React 表单在送出前验证 Gregorian 日期与 `int64` 上限，数值保留字符串，不经 JS `Number` 转换。Playwright A10 案例验非法 UTC、超过 9 位小数秒与超过 `int64` 的席次会显示字段错误，SQLite 命令笔数不变；超过 JS safe integer、仍在 `int64` 范围的字符串可通过表单验证。C18 分母溢出在创建预览前显示字段错误；合法的 `9007199254740993` 在预览画面精确显示，尚未确认时命令笔数不变。更新后的 A10 单项 Playwright 测试通过；先前的 29 个 Playwright 案例完整回归通过。其余字段与各动作的完整 HTTP／browser 边界矩阵仍待验。

`TestAdminRateDenominatorUsesExactPositiveInt64` 验 C18 费率分母拒绝零、负值、指数及溢出，合法且超过 JavaScript safe integer 的值在范式命令 payload 中维持原字符串精度。`TestAdminRateDenominatorPersistsWithoutPrecisionLoss` 验 preview 显示原值、发布命令成功，且从 SQLite 加载的价格条款保留相同 `int64` 数值；浏览器详情的显示仍待验。

`TestQuoteExpiryStaysWithinUnixNanoRange` 先重现固定时钟接近上限时，15 分钟报价期限回绕成负数仍被写入；修正后上限内最后一个到期时点可创建，超出上限的报价不会留下数据。该测试也先重现接受报价时下一帐期终点超出保存范围所引发的 SQLite 错误；现在于财务写入前回传领域拒绝，不留下订阅。`TestAdminQuoteWithOverflowingExpiryFailsWithoutReceipt` 验 C46 固定时钟后的 C01 在此边界失败且无报价或命令收据。`TestQuoteExpiryRejectsClockOutsideUnixNanoRange` 验直接调用的时钟也受保护。其他会推算未来时间的动作仍需逐一验证。

`TestCaptureRejectsUnrepresentableActivationBeforeProviderCall` 先重现接近保存上限时，收款已送到 provider，却因开通后的新帐期终点溢出而使本地交易失败；修正后待开通付款在 provider 调用前拒绝，付款维持 `created` 且 provider capture 为零。相同情境的 C09 预览／命令会结束为 `failed/DOMAIN_REJECTED`，没有 provider capture 或命令收据。`TestExistingProviderCaptureCannotOverflowActivationPeriod` 验已存在的 provider capture 在此边界被观测时回传明确领域冲突，本地 inbox、allocation 与开通不会部分写入；既有 provider 事实仍需人工处理。其他推算未来时间的路径仍需验证。

`TestAdminDuplicateFaultTicketFailsWithoutStrandingCommand` 验 C49 同一付款操作重复建票：第一张票据创建后，第二个不同键命令会结束为 `failed/DOMAIN_REJECTED`，同键重播仍找回原失败命令。SQLite 只保留第一张票据与一笔成功控制收据；后续 C09 派送可领取第一张票据并进入待查证。这项测试只涵盖付款票据的重复建票与派送，尚未取代 C46–C49 的完整故障、权限与恢复矩阵。

`TestAdminClaimedFaultSurvivesCrashBeforeProviderDispatch` 固定 C49 票据已绑定原 C09／C16 命令、但 provider 尚未收到请求的持久化状态。重启后，原命令重新领取同一票据，付款与退款都进入 `waiting_verification`；沿原命令查证后，各只有一笔 provider capture／refund 与一笔命令收据。`TestAdminRevokedDispatchReleasesClaimedFaultBeforeProviderCall` 与 `TestAdminRevokedRefundDispatchRetainsReservationAndFault` 另验此时撤销原命令会释放票据，外部 capture／refund 为零、原命令没有成功收据，退款 500 预留额不被释放；新授权命令领取同一票据后才各产生唯一外部效果与收据。并行接管仍待验。

`TestAdminClaimedFaultBlocksCompetingDispatchUntilOwnerResumes` 固定同一付款有两个已受理命令，票据已绑定后创建的原拥有者。较早排入恢复清单的竞争命令不能跳过票据去调用 provider；启动恢复会略过该暂时受阻命令，让拥有者沿票据进入待查证，完成后竞争命令可沿既有 provider 事实结束，capture 仍只有一笔。`TestAdminStalePreviewReleasesClaimedFault` 以受控来源快照失效验证原命令 `failed/PREVIEW_STALE`、零 provider capture／成功收据，票据释放后由新预览命令领取。这些是单进程可控交错；真正跨进程同时接管尚待验。

`TestAdminConflictingProviderDecisionsPreserveFirstOutcome` 对 C47 付款与 C48 退款各验相反的管理决策：第一个成功决策在 provider 留下一笔控制收据，第二个确定失败决策以 `failed/DOMAIN_REJECTED` 结束且没有收据；实际派送后只有一笔成功 capture／refund。操作已达终态时再提交新的决策也会失败，provider 控制收据仍只有第一笔。浏览器在既有付款重试与退款流程中实际提交相反决策，画面显示 `DOMAIN_REJECTED`，SQLite 验仅第一个决策有命令收据，退款 provider 事实在 C16 派送前仍为零；两个定向案例 2／2 通过。权限撤销交错及其余 UI 错误呈现矩阵尚待补齐。

`TestAdminMeterRegistrationRejectsStaleConflictingSchema` 与新的双分页浏览器案例补 C19 不可变 schema 的竞争验收：同 ID 的 `request`／`token` 计量表先各取得预览，`token` 先发布后，旧 `request` 命令以 `failed/PREVIEW_STALE` 结束，画面显示 HTTP 409 并拒绝相冲突的新预览。SQLite 的 unit 保持 `token`，两命令仅一笔成功收据；Go 另验成功与失败命令沿原键找回、旧键改 payload 拒绝。这项案例定向 Go 1／1、Playwright 1／1 通过；[A30 盘点](web-admin-action-audit.zh-CN.md)记录其余边界。

`TestAdminMeteredPriceRejectsStaleConflictingVersion` 补 C20 价格版本竞争：同 ID 不同固定金额及不同 ID 抢同一方案版号的旧预览均以 `PREVIEW_STALE` 结束，只保留一个版本与一笔成功收据；成功／失败命令沿原键找回，改 payload 使用旧键被拒。添加双分页浏览器案例验 3500 先发布后，旧 3000 预览回 HTTP 409、画面明示失效，SQLite 仍为 3500、两个价格组件与唯一成功收据。两个定向案例各 1／1 通过；其余逐项边界仍见 [A30 盘点](web-admin-action-audit.zh-CN.md)。

`TestAdminCatalogSelectionRejectsStaleCompetingPrice` 与添加双分页浏览器案例补 C21 同方案、cohort、生效时点的选价竞争。两个不同价格先各取得预览，后确认者的旧命令回 `PREVIEW_STALE`／HTTP 409，画面要求重新预览；SQLite 只保留先确认的价格与唯一成功收据。Go 另验成功／失败原键重播、改 payload 使用旧键拒绝，以及相冲突的新预览被拒。定向 Go 与 Playwright 各 1／1 通过；完整回归计数仍以本文档最新纪录为准。

## 范围边界

此环境只有单一管理员和本机 fake provider，未涵盖真实支付、税、多币别、正式总帐、公开部署或跨区域服务。相关金融不变量以 domain 与 SQLite 测试为准，UI mock 或成功消息不能代替数据库证据。

## 付款创建与派送交错回归（2026-09-27）

- 当次验证：`go test ./... -count=1 -timeout=180s` 959／959；`go vet ./...` 通过；四组付款交错定向 `-race -count=2` 共 18 个案例通过；Playwright 完整套件 90／90 通过。

- `TestAdminReplacementInvalidatesOldDispatchAndCapturesOnlyNewOperation`：C07 取代未送出的操作后，已受理的旧 C09 必须回 `failed/PREVIEW_STALE`，不能留在 `waiting_verification`；新 C09 只创建一笔 USD 6000 的成功 provider capture，旧 key 没有 capture，帐单尚欠 4000，两笔成功命令各有一笔收据。修正前测试重现旧 C09 卡在等待查证。
- 浏览器 `dispatching a selected payment does not send an earlier queued operation`：先创建两笔未派送付款，再于 C09 接口指定较晚的操作；SQLite 核对较早操作仍为 `created` 且 outbox `pending`，其 provider key 零 capture，指定操作成功且仅一笔成功收据。定向 Playwright 1／1 通过。
- 浏览器 `replacing a payment invalidates an older dispatch preview in another tab`：先预览旧 C09，再由另一分页以 C07 取代付款。旧分页提交得到 HTTP 409、失效提示且不能再确认；SQLite 的旧 C09 无成功收据，旧 provider key 无 capture。新 C09 只对替代操作收取 1500。定向 Playwright 1／1 通过；添加两个 C09 案例后完整套件 92／92 通过。
- `TestAdminDispatchInvalidatesEarlierReplacementPreview`：原 C09 先完成唯一收款后，较早受理的 C07 预览回 `PREVIEW_STALE`；帐单没有第二笔操作，C07 没有成功收据，未清余额为 0。
- `TestAdminRetryDispatchMakesCompetingRetryStaleWithoutDuplicateCapture`：首笔确定失败后，C08 创建唯一 USD 10000 重试操作；新 C09 派送成功，另一个先前受理的 C08 变为 `PREVIEW_STALE` 且没有成功收据。provider 只有原失败和新成功两笔 capture，帐单尚欠 0。
- `TestAdminReplacementAndDispatchCompeteAcrossLabInstances`：两个 Lab 共用 SQLite，从同一起跑点运行 C07 与旧 C09；若替代先提交，旧 C09 失效且无 provider capture；若派送先提交，C07 失效且仅原操作收款。修正前能重现 `database is locked`；写入前预留 SQLite writer 后，连跑 5 轮共 30 个子案例通过。
- 浏览器 `competing retry previews leave one obligation and dispatch its exact amount` 用同一个登录工作阶段的两个分页持有 C08 预览：第二页胜出，第一页提交收到 HTTP 409；SQLite 只有一笔重试义务、一笔 C08 成功收据，后续 C09 只对该操作创建一笔金额相同的 capture。
- 这些测试检查本机 SQLite 及 fake provider 的交易事实。更多付款错误矩阵和 A30 全动作逐项签核仍待完成。

## 退款预留与派送交错回归（2026-09-27）

- `TestAdminRefundReservationAndDispatchCompeteAcrossLabInstances`：两个 Lab 共用 SQLite 同时运行 C15 预留 500 与既有 C16 退款 400。修正前重现 C16 `database is locked`；退款服务商观察交易改为先预留 SQLite writer 后，连跑 5 轮共 30 个子案例、定向 `-race -count=2` 共 12 个案例通过。每轮 provider 只有一笔 USD 400 成功退款；第二笔预留依提交顺序成功或 `PREVIEW_STALE`，额度、操作及成功收据一致。
- 最新 Go 回归 `go test ./... -count=1 -timeout=180s` 965／965，`go vet ./...` 通过。添加两个 C09 浏览器案例后的完整 Playwright 套件 92／92 通过；该完整套件启动时尚未包含本段退款修正，因此另以最新程序定向重跑退款回应遗失浏览器案例 1／1 通过。
- A16 其余退款 UI 错误矩阵及 A30 全动作证据仍维持部分完成。

## Credit 额度详情（2026-09-27）

- `GET /admin/api/credits/{id}` 以已验证 session 读取 credit grant 来源和 `loadCreditBalance`；`AdminCreditDetail` 在同一个 SQLite 唯读交易内读取来源更正、发布纪录与目前额度，避免把不同时间的余额拼接。不存在的 ID 回 404。
- React＋Ant Design `/admin/credits/:id` 从 Credit 列表进入，分开呈现原始、已抵扣、退款保留、已退款及可用额度；有保留额时显示待派送／待查证说明，并可拷贝来源更正 ID、进入来源帐单、抵扣、预留退款及按 grant 筛选的退款列表。金额由 `Money` 以字符串／BigInt 格式化。
- `TestAdminCreditDetailSeparatesAvailableReservedAndRefunded` 核对 USD 1000 grant 的初始可用 1000、预留退款 400 后可用 600、回应遗失期间保留 400、查证后已退款 400、后续帐单抵扣 200 后可用 400；`TestCreditDetailSessionGuardAndNotFound` 验未登录与不存在的 ID。浏览器 `credit detail separates available reserved and refunded balances` 定向 1／1 通过；该案例另经 C48 设置第二笔退款确定失败，C16 派送后核对保留 300 归零、已退款仍为 400、可用回到 600，并验同一路径可用「运行另一个操作」重新预留，另有 `dispatching a selected refund does not send an earlier queued refund` 定向 1／1 通过。
- 来源更正 ID 由 grant 的 allocation release 关联取得；Go 测试对照实际更正，更新后的 Credit 详情 Playwright 定向案例 1／1 通过。A16 其他 UI 错误矩阵与 A30 全动作逐项证据仍需完成。

### 读取分类与实验时钟回归（2026-09-27）

完整 `go test ./... -count=1 -timeout=180s`：993／993 通过（加入商务时钟 lease 专项测试之前）；其后 `TestAdminBusinessClockChangeDoesNotExpireWorkerLease` 与稽核引用定向测试各自通过，`go vet ./...` 与管理前端 `typecheck` 通过。关库矩阵添加预览、工作与故障票据读取；时钟从内存状态读取，故不套用 DB 故障预期。当时未重跑完整 Playwright；当时最近一次完整浏览器结果为 94／94。两个定向浏览器案例各自通过：价格迁移 C22–C25 与帐户迁移 C36–C38／C43；案例现逐动作核对唯一成功收据，细节记于 [49 个动作证据盘点](web-admin-action-audit.zh-CN.md)。

### 故障票据可见性与分页（2026-09-27）

`AdminFaultTicketsPage` 以「待使用优先、SQLite 插入序号递减」排序并提供有界光标；创建时间仍显示在列表中，避免不同小数精度的 UTC 字符串导致同秒排序错误。超过一页可从实验控制页向前／向后查阅；每页显示观测时间，错误光标回 400 `INVALID_CURSOR`。Go 测试先重现旧版在 100 张较新已使用票据后隐藏旧待使用票据，再验修正后置顶，以及跨六页不重复、分页期间插入新票据不改变既有光标后方的成员；另以同秒不同小数精度的三张票据验证插入顺序。独立 Playwright 案例以真实 SQLite 创建 21 张待使用票据，验第一页、第二页与返回第一页。这补 A11/A25 的故障票据列表切片；其他列表与情境仍依上表状态追踪。

### A11 有效零时点与未发布状态（2026-09-27）

资源时间栏原先一律把整数 0 转为 `null`，使内置 `catalog_selection.effective_at=0` 被显示为「未知」。`TestAdminResourceTimestampZeroKeepsEpochButDraftPublishTimeIsUnknown` 先重现，再验目录选价与草稿价格的有效 epoch 均回 `1970-01-01T00:00:00Z`；草稿 `published_at=0` 依 `State=draft` 明确回 `null`；`basic-v1` 与 `pro-v1` 的 0 是种子数据缺少实际发布时间，也回 `null`；其他已发布版本若实际发布时点为 0，仍保留 epoch。`resource-epoch.spec.ts` 以真实 SQLite 与浏览器核对列表、基准价格与草稿详情。共用资源转换初次改动后，完整 `go test ./... -count=1 -timeout=180s` 为 1005／1005 通过；基准价格来源补正后，相关 Go 与浏览器定向案例及 `go vet ./...` 通过。资源列表与故障票据列表现于翻页区明示各页是独立查找，并非跨页快照。A11 其他资源的跨页插入与缺来源矩阵仍未全部结案。

### A11 金融资源跨页插入补验（2026-09-28）

`TestResourcePagesIncludeLaterInsertsWithoutRepeatingEarlierRows` 用五笔真实报价接受、收款、减额与退款预留创建六种资源。首页加载后插入同客户及另一客户的新数据，再沿原 HTTP 光标逐页读取：报价与订阅保留 `customer_id` 筛选，帐单、付款、Credit 与退款读取各自完整列表。六种清单均无重复、可读到新尾项，且每页 `total` 反映该次查找的来源状态；案例连跑五次通过。这是跨页插入切片，其他资源与更新造成的非快照变化仍待验，因此 A11 维持部分。

### A28 写入与回应遗失补验（2026-09-28）

`TestCommandAdmissionWriteFailureCanRetryOriginalKey` 对命令 INSERT 注入 SQLite 故障，首次 HTTP 请求回 500 `COMMAND_ADMISSION_UNKNOWN`（`retryable: true`，须沿用原键），命令、报价、收据均未写入。解除故障后原幂等键首次成功提交回 202，再重播回 200，两次均指向相同的成功命令，三种纪录各一笔。

`TestReceiptWriteFailureReturnsRecoverableOriginalCommand` 通过真实 HTTP handler 和 SQLite trigger 注入 C01 收据 INSERT 失败。首次请求回 503 `COMMAND_PENDING_RETRY`，保留原命令 ID 与可重试信息；此时报价与收据均为零笔。解除故障后以原幂等键重送两次，两次均取得原命令，最后仅有一笔报价、一笔收据及一笔命令。

`TestLostCommandHTTPResponseReplaysOriginalReceipt` 在 C01 成功写入后、回应送达客户端前关闭真实 HTTP 连接。客户端先收到网络错误；重送前数据库已有成功命令、报价与收据各一笔。原幂等键重送两次均返回相同命令，三种纪录仍各一笔。此案例覆盖提交成功但回应遗失；下列案例另验受理后运行中的断线。其余动作及 DB 故障组合仍列为 A28 未完成条件。

`TestClientDisconnectDuringAcceptedQuoteExecutionRecoversOriginalCommand` 以真 HTTP 请求提交 C01，等待原命令已受理且运行 lease 已取得，再于报价写入受 SQL trigger 延迟的测试环境中取消客户端请求。服务器结束后，原命令维持 `accepted`、lease 已释放，报价和收据均为零；移除故障后同键重送两次，只得到原命令、一笔报价和一笔收据。此测试连续运行 20 次通过，直接覆盖受理后、运行中的断线；其余动作及其他 DB 故障组合仍待验。

`TestUnavailableRefundLookupKeepsReservationAndOriginalHTTPCommand` 补 C17 真 HTTP 故障：provider 暂不可查时回 503 `COMMAND_PENDING_RETRY` 与原命令 ID，退款维持 `unknown`，Credit 的 500 预留不释放，且不产生成功收据。解除故障后原键重送两次，只完成原退款与一笔收据；Credit 预留转为已退款 500，可用额仍为 500，provider 退款仅一笔。定向测试连跑三次通过。

`TestRefundReservationReceiptFailureRetainsBudgetAndOriginalHTTPCommand` 补 C15 写入故障：收据 INSERT 失败时 HTTP 回 503 与原命令 ID，退款操作及额度预留整体回滚；Credit 1000 仍可用、provider 无退款。移除故障后原键重送两次，只创建一笔 `created` 退款操作、预留 500 和一笔成功收据，没有提前派送 provider。

Playwright 案例 `uncertain admission error keeps the original quote key until a successful retry` 注入 500 `COMMAND_ADMISSION_UNKNOWN`，首次没有创建命令。页面重新加载后仍保留原意图；点击「查找原操作」使用完全相同的 payload 与幂等键，最终只创建一笔成功命令、一笔报价和一笔收据。定向浏览器测试通过。

### request ID 与稽核关联（2026-09-28）

Admin schema v8 在同一升级交易内为 `admin_commands`、`admin_audit` 加入 `request_id` 与查找索引；既有历史列保留 `NULL`，不伪造当时不存在的请求识别。新请求由服务器产生 `X-Request-ID`，错误 JSON 的 `request_id` 与该 header 相同。命令受理时保存原请求 ID；受理、成功、失败、等待查证及批量收据的稽核列保存当次运行请求 ID，背景恢复则沿用原受理 ID。命令详情显示受理请求 ID，历史数据明示未记录。

`TestAdminSchemaUpgradesExistingV1` 验 v1→v8 字段与 ledger；完整 Go 回归 **1011／1011 通过**。Playwright 的真登录 C01 案例验 401 错误 body/header 的 ID 一致、忽略客户端伪造的 `X-Request-ID`，以及成功命令、两笔稽核列和详情页的 request ID 一致，并检查稽核内容不含管理密码、内部 token、CSRF 和 session cookie。CLI 数据库升级案例另验 v8 字段仍保留原帐单与 provider capture。

### 未知 API 路径与方法的错误契约（2026-09-28）

`TestUnknownAdminAPIRoutesKeepSessionGuardAndRequestIDEnvelope` 以真 HTTP 验未登录的未知路径仍回 401 `SESSION_REQUIRED`；登录后同一路径回 JSON 404 `NOT_FOUND`，不暴露 Go 路由器的纯文本缺省回应。已知路径使用不支持的方法时回 JSON 405 `METHOD_NOT_ALLOWED`，保留 `Allow: GET, HEAD`。三种错误均含与 `X-Request-ID` 相同的服务器请求 ID，并拒绝客户端指定的 ID。完整 `go test ./api/admin -count=1` **650／650 通过**。既有正常路由仍由原 handler 运行。

### 全量回归（2026-09-28）

加入 schema v8、request ID 与新金融故障案例后，`go test ./... -count=1 -timeout=180s` **1011／1011 通过**、`go vet ./...` 通过；完整 `pnpm --dir web/admin test:e2e` **97／97 通过**。A11、A28 与 A30 的其他验收缺口仍依上表维持部分完成。


### 命令读取失败时的既有数据与操作（2026-09-28）

命令行表、抽屉与详情在已有成功读取数据后，若重新读取失败，保留该数据并显示警告、上次成功读取时间与重试入口。只有首次读取失败且没有数据时，才显示整页错误。抽屉另提供明确的「更新」操作。详情的「继续原命令」、「重新查证」、「检查既有收据」，以及抽屉的「重新查证」，在详情查找失败时停用；抽屉尚未成功读取单笔详情时也不能从列表快照运行查证。

`web/admin/e2e/command-refresh.spec.ts` 使用真实浏览器、本机 HTTP、SQLite 与 fake provider 创建 C01 命令，再对列表及单笔 GET 注入 503，逐处核对旧数据、警告、时间与恢复；抽屉也验证首次单笔读取失败时，能清楚标示来自列表的数据与观测时间。`waiting_verification`、`accepted`、`PERMISSION_REVOKED_REVIEW` 是受控 GET 回应，用来验证操作按钮在数据新鲜与过期时的激活状态；这些状态切换不宣称为 SQLite 实际命令生命周期的证据。完整 Playwright 回归 98／98 通过（于最后一处抽屉时间来源调整之前运行）；其后以最新代码 `pnpm build` 通过，定向案例 1／1 再次通过，前端 `typecheck` 通过。A12 仍为部分：完整键盘、窄屏幕、失去焦点与 API 错误矩阵尚未逐页验收。


### 批量工作进度读取故障（2026-09-28）

`JobDetails` 在首次读取失败时显示可重试的错误；已有成功读取数据后更新失败，保留工作与逐项结果，并标示上次成功读取时间及重试入口。`web/admin/e2e/job-refresh.spec.ts` 使用本机浏览器和受控工作 GET 回应，逐步验证首次 503、成功取得一笔完成／一笔待查证、再遇 503、恢复为两笔完成的画面转换。这是 UI 失败状态证据，工作实际运行与 SQLite 收据仍由既有集成案例验证。最新 `pnpm build` 通过；命令与工作两个定向浏览器案例 2／2 通过。A12 逐页完整矩阵仍待验。


### 价格迁移暂停页的过期状态防护（2026-09-28）

暂停页增加明确的「更新」入口。若已显示 `active` 批量后读取失败，页面保留批量信息、显示上次成功读取时间，并停用「暂停未完成项目」；确认函数本身也拒绝在查找错误状态运行。已有命令结果的再次读取失败亦标为旧数据，避免将旧收据当成最新状态。保留原 request key 的查找入口，以便在命令提交结果不明时继续查原操作。

`web/admin/e2e/pause-migration-refresh.spec.ts` 以受控单笔 GET 验证首次 503、成功读取 `active`、再次 503 时操作停用，以及恢复后重新激活；此案例只证明 UI 防护，实际 C23 命令与 SQLite 收据由既有集成案例证明。最新 `pnpm build` 与命令、工作、迁移三个定向浏览器案例 3／3 通过；既有真实价格迁移及工作进度集成案例 2／2 通过。A12 的全页矩阵尚未完成。


### 来源读取故障与原命令恢复（2026-09-28）

接受报价、下期变更、立即升级、取消／恢复取消调度，以及价格迁移暂停页，若已保存原命令 request key，即使来源单笔 GET 失败，仍显示「查找原命令」入口；查找暂时失败会显示错误并保留原键再次查找，查回命令后提供命令页链接。来源读取失败页不提供创建新预览或提交新意图的入口。

真实 HTTP／SQLite 浏览器案例 `a lost quote acceptance response recovers one subscription and invoice` 在 C02 写入后丢失 POST 回应，重整时注入报价 GET 503，再用原 idempotency key 查回同一命令；验唯一 C02 命令、订阅与帐单。`source-read-recovery.spec.ts` 另以受控 503／POST 回应验 C03、C04、C05、C06、C23 的保存键、POST `action_id`、原 idempotency key 与命令链接，并验 C03 首次查找回 503 后再次使用同一键；它只证明接口接线，不代表这五个动作在该案例中创建了 SQLite 收据。最新 `pnpm build` 通过；上述真实案例、受控矩阵、既有升级与取消调度案例共 4／4 通过。其余动作的中断恢复矩阵仍在 A28 与 A30 待验范围内。


### 外部操作命令与金流结果分别呈现（2026-09-28）

C09／C16 的命令 `succeeded` 表示送出流程已完成；`result_refs.operation_status=definitively_failed` 表示实际付款／退款没有成功。操作结果、命令详情、列表及抽屉现在同时显示两层状态，避免只看到命令成功而误判金流成功。警告引导操作员检查帐单余额或 Credit 可用额度，再决定是否创建新操作。

金额边界：退款以最小货币单位记录；C15 预留占用 Credit 可用额度，UNKNOWN 不释放保留，服务商确定失败才释放；成功退款转入已退款额。退款关联原收款 provider key 与稳定退款 key，结果查证沿原 key，不创建第二笔外部退款。这些不变式的既有证据包括 `lab/admin_refund_reconcile_test.go`、`lab/admin_refund_dispatch_interleaving_test.go` 与真实浏览器的退款故障流程；本次 UI 改动没有改动金额或 provider 逻辑。

真实浏览器 `credit detail separates available reserved refunded balances` 验 C16 命令为 `succeeded`、退款状态为 `definitively_failed`、保留额归零与可用额恢复，且操作结果、命令详情、列表与抽屉均标示「退款失败」。`partial payment replaces an unsent operation and retries only definitive failure` 验 C09 对应的付款失败警告。两个定向案例 2／2，命令读取故障回归 1／1，退款 UNKNOWN／并行派送／决策恢复的定向 Go 测试 8／8，最新 `pnpm build` 通过。这些是本机 SQLite／fake provider 的证据，不代表外部结算已完成；A16 与 A30 仍有未验完的动作矩阵。


### 用量事件的筛选光标与跨页插入（2026-09-28）

`TestFilteredUsageEventPagesIncludeLaterInsertWithoutDuplicate` 使用真实 HTTP 管理查找、本机 SQLite 与已付款的 Pro 订阅，对 `usage-events` 同时指定 `subscription_id`、`source=worker`、`period_index=0` 及一笔一页。首页读取后插入同订阅的新事件，也插入另一订阅的事件；沿原光标读完只得到原订阅三笔依插入顺序排列的事件，没有重复或混入另一订阅，回传 total 由 2 变为 3。此案例补足用量事件在跨页插入时的 HTTP 证据；尚未代表其他资源或所有时间字段语意已逐项验收。完整 `go test ./api/admin -count=1 -timeout=180s` 651／651 通过，A11 维持部分完成。


### 资源抽屉与最新查找数据同步（2026-09-28）

通用资源抽屉原先保存点开时的整笔列数据；列表重新读取后即使付款等状态已变，抽屉仍显示旧快照。现在抽屉保存稳定列键，从当前页数据取得列内容，提供抽屉内的更新入口与观测时间；更新失败明示使用上次成功读取数据，列不再属于当前页时关闭抽屉。

`web/admin/e2e/resource-drawer-refresh.spec.ts` 以受控付款列表 GET 验 `created → definitively_failed`、503 保留旧数据、恢复为 `succeeded` 及列消失后关闭。此案例验证接口状态同步；付款状态转移本身由既有 SQLite／provider 案例验证。最新 `pnpm build` 通过，添加案例 1／1、既有资源失败与逾时案例 2／2 通过。A11 其他资源的时间／缺失值语意及 A12 的逐页错误矩阵仍未全验。


### 目录价格与选价的筛选光标（2026-09-28）

`TestFilteredCatalogPagesIncludeLaterInsertAndKeepUTCTime` 使用已发布的 Pro 价格版本与真实目录选价，通过管理 HTTP API 分别对 `prices` 套用 `plan_id`／`id_prefix`、对 `catalog-selections` 套用 `plan_id`／`cohort`，以一笔一页读取。首页读取后发布并选取第三个版本，另插入其他 cohort 选价；沿原光标均只取得原筛选的三笔，依插入顺序且不重复。选价 `EffectiveAt` 核对为 UTC RFC3339Nano。这补充 A11 目录两种资源的跨页插入与时间串行化证据，其他资源及缺来源值矩阵仍未全验。完整 `go test ./api/admin -count=1 -timeout=180s` 652／652 通过。

### 共用操作页的命令读取故障（2026-09-28）

共用操作页在命令卡片添加「更新」入口。首次读取命令失败时显示可重试错误；已有成功读取数据后更新失败，保留旧结果，明示上次成功读取时间与重试入口。读取失败期间，旧的 `accepted`／`waiting_verification` 状态不能用来继续原命令或重新查证，旧的终态也不能用来清除命令并开始另一操作。

`web/admin/e2e/action-command-refresh.spec.ts` 以受控 C16 命令 GET 检查三种状态、503 及恢复后的按钮行为；这是 UI 故障状态证据，不代表 SQLite 命令实际转移。添加案例 1／1、既有真实 SQLite／fake provider 的付款提交后崩溃与退款预留恢复案例 2／2 通过。A12 的逐页键盘与错误矩阵、A28 的其他写入及数据库故障组合仍为部分完成。

共用操作页在原命令回应为 `COMMAND_PENDING_RETRY`、后续读到终态时，选择「运行另一个操作」会清除前一命令的 submit／resume／preview 错误与旧预览。修改被 422 拒绝的输入时，也会清除该次提交或预览的旧错误。两个添加的受控 C38 HTTP 回应案例分别验证开始另一操作及修改输入后，不再显示前一请求的错误。修正前的完整浏览器基线 103／103 通过；修正后 `pnpm build`、`pnpm typecheck` 与此档定向案例 3／3 通过。完整套件尚未在此最后修正后重跑，故不将基线写成修正后的全量结果。

### 预览阻挡原因与停留页面时到期（2026-09-28）

共用操作页现在逐项显示预览回应的 `blocking_reasons`，存在阻挡原因时停用确认；原因解除后重新创建预览才可确认。预览停留在页面期间到期时，计时更新会显示过期提示并停用确认，重新创建预览后解除提示。打开确认框后才到期，也会在框内最后按下确认时重新核对期限，不送出旧预览。确认函数再次核对阻挡原因与期限；服务器仍为命令受理及来源重新验证的权威。

`web/admin/e2e/preview-blocking.spec.ts` 使用真实浏览器与本机管理服务、受控预览 HTTP 回应验证两项交互，且确认阻挡及到期时没有送出命令。这是前端契约证据；目前领域预览的 `blocking_reasons` 均为空数组，尚无真实 SQLite 情境产生非空原因。最新 `pnpm build` 与此文件例 2／2 通过；既有真实 SQLite 预览编辑与价格发布失效案例合计 3／3 通过。A12 的其余逐页键盘、窄屏幕及错误矩阵仍为部分完成。

### 专用付款与订阅预览的期限防护（2026-09-28）

创建付款、重试付款、接受报价、方案变更及取消／恢复取消五个专用页，现在与共用操作页使用同一个预览期限判断。每页显示 `blocking_reasons` 与到期提示，阻挡时停用确认；打开确认框后到期，也会在最后按下确认时再次检查，不保存待送意图或提交命令。服务器仍在命令交易内重验预览与来源事实。

`web/admin/e2e/payment-preview-expiry.spec.ts` 以真实管理服务和受控 C07 预览回应验证阻挡原因、确认框到期、无命令提交及重新预览，1／1 通过；共用页预览案例 2／2 通过。另以真实 SQLite／假服务商的接受报价、重试付款、取消／恢复与方案调度相关浏览器案例 6／6 回归正常操作。这些证据不代表五页的所有阻挡与到期组合都已逐项验完；A12 仍维持部分完成。

### C02 接受报价的 HTTP 收据写入故障恢复（2026-09-28）

`TestQuoteAcceptanceReceiptFailureRecoversOriginalHTTPCommand` 在隔离 commerce/provider SQLite 创建 Basic 报价及管理预览，通过具 session、CSRF 和原幂等键的管理 HTTP handler 提交 C02。注入 `admin_command_receipts` INSERT 故障后，HTTP 回 503 `COMMAND_PENDING_RETRY`、`retryable=true` 和原命令 ID；此时订阅、帐单、帐期、付款义务、capture outbox、成功收据与 provider capture 均为零，只有原命令已受理。移除故障后同键重送两次，两次都回同一个成功命令；订阅、帐单、帐期、付款义务、待派送 outbox 及收据各只有一笔。帐单明细合计、帐单总额、付款义务及收据来源 ID／金额／币别均与原报价一致，provider 尚未收款。同键再提交另一张有效报价回 409 `IDEMPOTENCY_CONFLICT`，第二张报价没有订阅。定向 Go 测试通过。这补足 C02 的 HTTP 层中断及键冲突证据；A08／A28 的其他动作与故障点矩阵仍为部分完成。

### C12 Credit 抵扣的 HTTP 故障与回应遗失恢复（2026-09-28）

`TestCreditApplyReceiptFailureRecoversOriginalHTTPCommand` 先在隔离 SQLite 创建已付款 Basic 帐单、1000 的 funded Credit，以及同户下期 2000 的待付款帐单。通过具 session、CSRF 与原幂等键的管理 HTTP handler 抵扣 500。收据 INSERT 故障时回 503 `COMMAND_PENDING_RETRY` 和原命令 ID；Credit 仍可用 1000、抵扣纪录与成功收据均为零，原 2000 的付款操作及 outbox 保持待派送。移除故障后同键重送两次只创建一笔 500 抵扣与收据，Credit 可用 500、目标帐单未清 1500；旧付款操作取消、outbox 标为 `done`，不会自动创建新的付款操作。收据的 grant、invoice、金额与币别参照直接比对已提交事实；provider capture 仍只有来源帐单原先的一笔。

同一隔离情境另以新预览抵扣 250，在命令成功提交后、HTTP 首个回应送达前切断连接。客户端先收到网络错误，但第二个命令、抵扣及收据已各提交一次；同键重送两次都返回原成功命令。最终 Credit 已用 750／可用 250、目标帐单未清 1250、抵扣两笔、目标付款操作仍只有已取消的原操作，provider capture 仍为一笔。C01／C02／C12／C15 的定向 HTTP 收据故障矩阵连跑三次通过。这将「收据失败整体回滚」与「提交成功但回应遗失」分别验证；A08／A28 其他动作的完整故障矩阵仍待补。

### C11 已收款帐单减额的 HTTP 收据故障（2026-09-28）

`TestPaidReductionReceiptFailureRecoversOriginalHTTPCommand` 使用已实收的 Basic 帐单与真实管理 HTTP handler。减额 1000 的收据 INSERT 故障时回 503 `COMMAND_PENDING_RETRY` 与原命令 ID；原始帐单总额与明细仍为 2000，没有 correction、funded Credit、收款发布或成功收据，provider 原 capture 保留唯一一笔。解除故障后同键重送两次只产生一笔 correction、1000 的 funded Credit、一笔 1000 的原收款发布及一笔收据；帐单原额 2000 不变，修正后义务与净实收均为 1000，未清为零。收据的 correction ID、grant ID 和金额直接比对提交列。同键再送另一笔有效的 500 减额回 409 `IDEMPOTENCY_CONFLICT`，未添加财务事实。C01／C02／C11／C12／C15 的定向 HTTP 收据故障矩阵连跑三次通过。既有浏览器案例另覆盖 C11 成功提交后回应遗失；A08／A28 的其他故障组合仍待验。
### A11 退款筛选光标的跨页插入（2026-09-28）

`TestFilteredRefundPagesIncludeLaterInsertWithoutCrossingGrants` 使用隔离 SQLite 创建两笔 Credit grant 和多笔退款，通过具读取 session 的管理 HTTP 查找 `refunds?grant_id=...&limit=1`。读完第一页后，分别向目标与其他 grant 插入新退款；后续页的总数更新为三，依序只返回目标 grant 的原第二笔与新第三笔，不重复且不混入其他 grant。把第一页光标改套到另一个 `grant_id` 时，API 回 400 `INVALID_CURSOR`。定向测试以 `-count=3` 通过。A11 仍为部分完成：其他资源的筛选、时间语意与缺来源值矩阵尚未全验。

`TestPaymentStatusFilterKeepsCursorAfterLaterInsert` 另以真实管理 HTTP 与隔离 SQLite 验付款 `status=succeeded` 分页：首两笔成功付款之间插入未派送付款；读取首页后再插入成功与未派送付款。后续页只返回原第二笔与新成功付款，总数由二更新为三，没有重复或混入 `created`；把光标改套到 `status=created` 回 400 `INVALID_CURSOR`。定向测试以 `-count=3` 通过。

`TestFinancialResourceKeysRequireFinanceCapability` 先证明只有 `read` 能力的 session 可从付款列表取得 `ProviderKey`，再于 API 回应编码前依 session 能力移除财务内部键。现在只读 session 的付款项目不含 `ProviderKey`，退款项目不含 `ProviderKey`、`SourceProviderKey`、`RequestKey`；具有 `finance.adjust` 的 session 仍可读取这些字段。两种 session 都保留相同资源 ID，三项 A11 定向案例合计连跑三次通过。这项限制只涵盖付款与退款列表的上述字段，A11 其他资源矩阵仍未全验。

`TestCommandReadsNeverExposeIdempotencyKey` 另在真实管理 HTTP 的命令清单与详情重现 C01 幂等键外泄。读取端现在一律省略该键；只有 `read`、含 `finance.adjust`、含 `subscription.manage` 的 session 都可读取相同命令 ID，但不取得原键。原键仍保存在命令数据库供幂等重送判定，测试确认查找屏蔽不改动保存事实。此案例只验 C01 一般报价；其他动作的命令读取矩阵尚未逐一验收。

### 价格操作重入与浏览器回归（2026-09-28）

完整浏览器回归第一次在重新打开价格发布页时卡于停用的版本 ID 字段，当次为 88 通过、1 失败、19 未运行。根因是表单先从 `sessionStorage` 还原旧命令 ID 并停用输入，命令详情异步读取完成后才显示「运行另一个操作」；测试当下用不等待的 `isVisible()`，可能跳过重设。测试以延迟命令详情 GET 固定重现失败，改为在字段停用时等待并点击重设按钮后，定向案例通过。第二次完整浏览器回归为 **108／108 通过**。其后命令读取一律省略幂等键的改动，以目前代码重建前端并定向验命令读取失败、详情轮询离页停止及命令行表光标，**3／3 通过**；这三项是该后续改动的浏览器证据，不把较早启动的全套运行当作其单独证明。

### Credit 抵扣与退款额度的限定范围复查（2026-09-28）

本次只检查 funded allocation release 所产生的 Credit grant、跨帐单抵扣、退款预留与退款结果观测。`credit_grants` 的来源须对应已实收付款与发布纪录；`CreditBalance` 将已抵扣、待定退款（`created`／`submitted`／`unknown`）及成功退款分别扣除，确定失败才释放原保留额。抵扣另检查同一客户、币别、帐单未清额及未送出的旧付款操作；退款预留在同一 SQLite 交易重新检查可用额，数据库 trigger 也约束抵扣与退款的共享总额。退款查证沿原 provider key 对应原操作，未知结果仍占用额度。`TestAdminCreditApplicationAndRefundReservationCompeteAcrossInstances`、`TestAdminRefundReservationAndDispatchCompeteAcrossLabInstances`、`TestAdminCreditApplicationAndUnknownRefundShareFundedBudget` 在隔离双数据库与假服务商下以 `-count=3` 运行，合计 24 项通过。这些路径未发现已验证的金额缺陷；测试只证明模型化服务商与所选交错，没有验证外部支付服务商的真实回应或结算证据。

### C18 价格发布的回应遗失恢复（2026-09-28）

`lost C18 publish response recovers one price and receipt with the original key` 经真实管理 HTTP 完成价格发布后切断首次 202 回应。接口保留原操作意图；重整后以原 request key 找回成功命令。隔离 SQLite 直接核对 C18 命令、收据与价格版本各一笔，三个价格组件存在，收据中的版本 ID 与 checksum 对应已发布价格，且付款操作数量没有增加。定向 Playwright 案例通过。

`TestCatalogPublishReceiptFailureRecoversOriginalHTTPCommand` 另以隔离 SQLite trigger 令收据 INSERT 失败。真实管理 HTTP 回 503 `COMMAND_PENDING_RETRY` 和原命令 ID，价格、组件与收据均未提交；移除故障后同键重送两次只创建一笔价格、三个组件、一笔收据，收据 checksum 对应已发布版本。改 payload 重播回 409 `IDEMPOTENCY_CONFLICT`，没有新的财务事实。定向测试以 `-count=3` 通过；[逐动作证据盘点](web-admin-action-audit.zh-CN.md)已加入 C18 运行链及剩余限制。

## 专用操作页命令结果的重整恢复（2026-09-28）

`useStoredCommandID` 依管理 actor、动作与目标，把最近一笔命令 ID 保存在同一分页的 `sessionStorage`。共用操作页与专用的报价、订阅调度、付款、用量、价格迁移页均以此 ID 重新查找服务器命令；浏览器保存的只是查找指针，命令状态、收据与金融结果仍以服务器数据为准。完成或失败后，接口提供明确的「另一笔／继续操作」入口，清除本页指针并要求重新预览；接受报价、重试付款与暂停迁移的失败结果也可重新进入操作。

`web/admin/e2e/admin.spec.ts` 直接验证 C01 创建报价与 C05 调度取消在页面重整后仍显示原命令，C05 查看原结果后可依最新订阅状态运行 C06。其他既有并行、变更报价与用量情境改由接口明确启动下一笔操作；定向重跑通过。这些证据只涵盖指定的重整与后续操作路径，A01–A30 未完成项目与逐动作故障矩阵仍依验收计划追踪。

完整 Playwright 回归：109／109 通过（`pnpm --dir web/admin exec playwright test`，15.7 分钟）；`pnpm --dir web/admin build` 与 `go test ./...` 亦通过。这是目前工作树的回归结果，并非 A01–A30 全项签核。

### Fake provider 独立状态与分页（2026-09-28）

`GET /admin/api/lab/status` 分别读取业务时钟、待用故障票据数与独立 provider 数据库计数，并回传 provider 自己的观测时间；它不宣称跨两个数据库的原子快照。`GET /admin/api/lab/provider-captures` 与 `/provider-refunds` 只供 `lab.control` 能力读取，支持 1–100 笔上限、状态筛选与绑定资源及筛选条件的光标。金额以字符串传输，provider key 仅出现在有权限的诊断页。

`TestAdminProviderPagesKeepIndependentCursorAndExactAmounts` 验证新 provider 事件不移动已读页、refund 来源与大额金额；`TestProviderReadsRequireLabCapabilityAndPreserveMoney` 验证 401／403、光标跨资源与跨筛选拒绝、精确 JSON 金额及数据库故障。`provider-state.spec.ts` 以真实浏览器、双 SQLite 验状态数、分页、筛选、退款来源与大额显示。
