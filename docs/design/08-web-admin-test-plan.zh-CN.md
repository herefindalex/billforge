# Web Admin 验收计划

[English](08-web-admin-test-plan.md) | [繁體中文](08-web-admin-test-plan.zh-TW.md) | **简体中文**


状态：截至 2026-10-03，Web Admin 实作与 A01–A30 本机验收完成。原设计范围不变，目前证据与限制以 [实作记录](../implementation/web-admin.zh-CN.md) 为准。

依据：[功能设计](05-web-admin.zh-CN.md)、[工程契约与 C01–C49](06-web-admin-contracts.zh-CN.md)、[任务计划](07-web-admin-implementation-plan.zh-CN.md)。测试环境为真实 Go server、隔离 commerce/provider SQLite、可控制的 business clock；无外部支付连接。

## 1. 证据与共用矩阵

每笔运行纪录保存 source revision 或工作区 hash、schema version、环境／时钟、数据种子、命令、结果、失败时的 trace。测试帐号与数据全为合成。不能只以 HTTP 200 判定成功，必须核对数据库金融事实、命令 receipt、provider fake 记录和 UI 状态。

**C01–C49 每一条命令**都要有：合法 payload、字段／范围错误、缺 session、缺 capability、跨 actor preview、同 key 同 payload、同 key 不同 payload、相同对象重播、UI 成功／错误呈现。需要 preview 的命令另测 expiry、来源改变、其他 command 已 claim、交易内 revalidation；N 类仍要验证其独立业务来源 guard。适用性表不能默认略过；任何 N/A 都需对应方法与理由。

所有 GET 列表另测：缺省／最大 limit、未知 filter、cursor 与 filter 不符、稳定排序、空结果、取消、去敏、schema 类型、数据插入时跨页语意。ID 为不存在或不同资源类型时明确回 404/422，不触发写入。

## 2. 场景与判定

| ID | 环境／入口与受控条件 | 验证与预期结果 | 若失败，用户必须看见 |
| --- | --- | --- | --- |
| A01 | 现有 Go fixtures；T05 前后 | S01–S12/P01–P03 既有金额与状态回归；检查同测试是否真正走相同 domain path | 回归阻挡交付；不以 UI workaround 遮盖 |
| A02 | 新暂存目录；普通及 adminui build；直接开详情 URL | 无 dist 的纯 API build 通过；缺 dist 的 admin 启动报明确建置错误；有资产的深链接与静态档正常 | 可采取动作的建置／路由错误，不能空白画面 |
| A03 | `/admin/login`、`.env`、session、logout；时间受控 | `.env` 缺值拒启动、process env 优先、特殊字符正确、错密码／未知帐号同消息、5 次失败／15 分钟及全域节流；nonce／session 轮换；session 30 分钟闲置／8 小时／重启失效；其他 Host/Origin、缺 CSRF、错 content type 被拒绝 | 登录或明确 403；不回金融成功提示 |
| A04 | 管理 server 路由；不同 capability actor | `/v1` 写入不可通过管理 listener 进入；所有 action 后端拒绝不足权限；内部 token／管理密码不在 bundle、response、URL 或 log；`.env.example` 无有效凭证，忽略规则生效 | 403 或未挂载路由的 404；无数据变更 |
| A05 | 新库、目前 schema fixture、故意让迁移中途失败 | admin 表／索引按 version 一次创建；失败整体 rollback；原 invoice／provider facts unchanged；旧 CLI 的允许写入仍可用 | 启动失败原因与复原步骤，未开放 HTTP 接受命令 |
| A06 | 两个 client 同 key；于 admission 后／domain commit 前后确定性中断 | 一个 command、一次 financial effect、一个 receipt；重启查回结果；不得有 financial commit 无 receipt 的本地操作 | 同一 command 的进度／结果，不产生第二个意图 |
| A07 | lease 过期后新 worker claim，旧 worker 再回报；能力撤销 | 旧 generation 不能盖新结果；未运行命令撤销；已存在 provider 义务继续查证 | `failed/PERMISSION_REVOKED` 或 waiting；不能把有外部效果的命令当一般失败 |
| A08 | 成功后令 preview 过期、revision 改变，再送原 key | 优先回原 command/receipt；改 payload 则 409；重整页面找回 command | 原结果或 key conflict，不显示假性 quote expired |
| A09 | preview 到期、clock revision 变更、另一操作者改 source；超确认金额 | 无 financial 写入，409 PREVIEW_STALE；保留用户意图并显示新旧差异；仅允许契约规定的期中金额下降 | 重新预览的理由与差异 |
| A10 | 0、负值、int64 边界、超 JS safe integer、exponent、invalid UTC | 许可值精确往返；不许可值在写入前拒绝；minor units 不经 JS Number；rational 分母检查 | 字段级错误；无默默四舍五入或截断 |
| A11 | 各列表插入跨页新数据、status filters、缺来源值 | cursor 稳定、有界；标示非全域 snapshot；unknown 不当 0；敏感 key 只对应权限可见 | 正确 empty/stale/not-found/forbidden |
| A12 | 所有页面 keyboard、窄屏幕、API 失败、失去焦点；Ant Design Table／Form／Modal 交互 | 确认框焦点恢复、labels/error 关联、状态非仅靠颜色；金额与费率输入不经浮点转换；无金融 optimistic success；轮询离页停止 | 可操作的错误／数据观测时间 |
| A13 | `/customers/:id` 与 `/subscriptions/:id`；C01–C06 | Basic 新购；Pro 调度；取消／恢复；过期或已购 quote 不重用；quote/binding 原子；席次／价格／revision 任一不同无写入 | 实际价、下期意图与冲突原因各自显示 |
| A14 | Basic $20 → Pro 5 席 $100，30 日周期中点；C04 | 初始差额 $40；延后运行依相同政策重算，超确认上限拒绝；付款 UNKNOWN 不提前切服务 | due_now_estimated 与实际 pending_amount 分开，状态正确 |
| A15 | `/payments`；C07–C10，部分付款、确定拒绝、lost response | 合法部分额不超可收额；RetryFailedPayment 对应正确 invoice／原操作状态；只有一笔原 provider capture；不因队列换人派送 | declined／waiting／confirmed 正确，原操作查证入口 |
| A16 | `/credits`、`/refunds`；C15–C17，两者争用同 credit | reserve＋apply 不超 funded budget；退款 UNKNOWN 保留；by-ID dispatch 与确认对象一致；查证后仅一次 refund | 可用、保留、已退分开；不足／未知原因 |
| A17 | `/invoices/:id`、四类来源历史；C11–C14，延迟开通、期满未服务；超过 100 笔与跨页插入 | invoice 原始 lines 不变，correction／credit 有来源；沿用既有 $5.34 等 oracle；重播不再减额；有界光标可查完历史且不重复，非法及跨种类光标拒绝 | 原始／更正后／已收／应付数字与来源可追查；截断提示可进完整历史 |
| A18 | `/jobs`；C13/C30/C32/C44/C45；preview 后添加 eligible item | 只处理固定 membership；逐项 crash/restart 不重做；关闭 server 不丢失已 commit 的项目；summary 分开 success/conflict/waiting/skipped | 逐项原因与目前进度；不可误报全完成 |
| A19 | `/catalog`；C18–C21，Pro 与新 meter | 已发布版本唯读；checksum／组件完整；版本与选价冲突拒绝；AI token rate 可展示接受；原 tasks 不变 | 新版本发布与选价生效日期明确 |
| A20 | `/price-migrations/:id`；C22–C25，正常户＋冲突户 | 预览 pin targets；暂停／略过／恢复不重做；反向以新批量运行；已生效不被 pause 抹除 | 每户新旧价／revision／原因 |
| A21 | `/usage`；C26–C30，重复／冲突／撤销／晚到 | 原事件不可改；20,003→$0.00、20,010→下一期 $0.01 的既有 policy；负差额 CreditNote；rating 历史保留 | estimated/finalized/rerated 与差额来源 |
| A22 | `/contracts`；C31/C32、C01/C02 contract 分支 | 客户／contract 版本绑定；Acme 五席 $75；Net30 接受即服务、未到期不收；缺后续价 hold | 现在应付 0、每期承诺 $75、due date／hold 各自显示 |
| A23 | `/reconciliation`、`/discrepancies/:id`；C33–C35 | 修复前再核证据／revision；金额不符走人工；修复后有 verification run；人工决议不生成金融修正 | expected/actual、blocked reason、已运行与已验证区别 |
| A24 | `/account-migrations/:id`；C36–C43 与 adapter GET | 一致映射、shadow/readiness；不完整来源先 manual review；UNKNOWN 阻止切换；切读先于切写；stop 阻止新命令而不回退金融史 | owners、来源与每个未过门槛；不显示虚假的 rollback 成功 |
| A25 | `/lab`；C46–C49，运行中改时间、指定故障 | 一个命令用固定 business time；wall clock session/lease 不跳；fault ticket 只用一次且仅指定 payment/refund；fake decision 写入后 crash 由 provider control receipt 找回，不因后来已有 capture 而重写；改时间不自动收款 | 环境、时刻与已激活故障永久可辨识 |
| A26 | `/commands/:id`；跨页、浏览器刷新、server 重启 | actor 身分稳定；新 session 找回旧命令；不因 UI timeout 换 key；外部不确定性保留为 waiting | 可恢复操作纪录，无第二笔义务 |
| A27 | 本机负载：1 万订阅、10 万用量事件，5 读取 client＋1 worker | 同机记录 hardware／版本；列表 p95 ≤500ms、100 rows response ≤1MiB；worker 有进度，无全量 State dump／无长时间 provider transaction | 慢查找有量测；逾时显示 stale，不伪造最新结果 |
| A28 | DB query 错误、fake provider 暂不可查、client 断线 | error code 正确；command 的 retryability 与资金保留不矛盾；audit 有 actor/request/command/result refs、不含 token | 可采取下一步的错误或 waiting，没有静默失败 |
| A29 | 新暂存 DB 与旧 DB 的本机建置 binary | README 指令能从 build/login 到所有入口；Go API-only 与 adminui 都可用；退出清理测试服务 | 明确启动／退出／升级操作 |
| A30 | 最终 coverage audit | 每个 C01–C49 有 endpoint、UI、permission、receipt/recovery、适用 contract matrix 与对应 A 场景的真实证据；T01–T22 全完成 | 缺任一项就维持 Web Admin 未完成 |

性能数字是本机验收目标，并非已测得结果或生产 SLA。达不到时以 query plan／profiling 找具体原因；不得靠提高上限让测试变绿。首次量测记录 fixture 与硬件后，任何调整需留下原因与新门槛。

## 3. UI 路径与自动化策略

Playwright 测试路径涵盖工程契约的全部页面。共用组件以 Vitest／React Testing Library 检查精确金额、状态映射、form validation、键盘／焦点；financial truth 仍由 Go transaction tests 与有实际数据库的 E2E 判定。web request mock 只用于 loading/error 组件测试，不能代替付款／退款／恢复 scenario。

并行使用 channel／barrier 或可控 test hook 安排 interleaving，不以 sleep 猜时序。provider 故障由 fake provider 的既有模型与特定 test hook 控制，不连外测试。登录 fixture 使用临时 `.env` 与正式 session path；不得在 production bundle 留 bypass。

对每一 command 在 A30 audit 前填：action ID、UI route、handler、domain helper、来源 guards、permission、preview policy、receipt policy、HTTP tests、browser scenario、运行证据。这些字段没有证据时保持空白并列为未完成，不能以文档存在作通过判定。

## 4. 已知限制与不宣称事项

本规划没有测量真实 provider、跨区域一致性或公开部署安全。一次本机 browser smoke 不能证明 49 项功能完成；同样，命令表有纪录不代表与业务事实同交易。每个 invariant 都需要相应金融来源与中断测试才能结案。
