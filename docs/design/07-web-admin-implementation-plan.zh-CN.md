# Web Admin 实作计划

[English](07-web-admin-implementation-plan.md) | [繁體中文](07-web-admin-implementation-plan.zh-TW.md) | **简体中文**


状态：截至 2026-10-03，T01–T22 与 A01–A30 本机验收全完成。Checkbox 依实作与验收证据勾选，证据与限制见 [实作记录](../implementation/web-admin.zh-CN.md)。

上位文档：[功能设计](05-web-admin.zh-CN.md)、[工程契约](06-web-admin-contracts.zh-CN.md)、[验收计划](08-web-admin-test-plan.zh-CN.md)。[JSONL](web-admin-tasks.jsonl) 保留原始任务估算与依赖，当前验收状态以[实作纪录](../implementation/web-admin.zh-CN.md)为准。

A01–A30 在文档列明的本机范围通过（30／30），T01–T22 全完成。最终独立复查没有阻挡或重要问题；49 项动作记录连接 UI 路径、handler、domain helper、来源守卫、能力、预览政策、收据／恢复政策、HTTP 测试、浏览器情境与执行证据。

## 1. 工程审查结论

前一版有完整功能方向，但不足以直接开工。此次完成 architecture、code quality、testing、performance 四个面向的设计审查；没有运行实作、安装前端依赖、启动新服务或部署。

| 发现 | 目前证据／缺口 | 已选方案 |
| --- | --- | --- |
| F01 管理读取缺口 | `api/v1.go` 路由有限；`lab/inspect.go` 的 State 一次返回全量 | 添加有界、稳定排序、来源可追查的管理 DTO，不把 State 全量转 JSON |
| F02 管理权限绕过 | 现有 `serve` 提供本机 v1，仅内部操作带 token | 新 `admin` server 专用 session 路由；不挂载未受管理授权的 v1 |
| F03 写入与 command 不原子 | 领域方法自行 BeginTx，`Lab` 限制单连接；外层 wrapper 会死锁或分离提交 | 抽 Tx helpers，command receipt 与本地业务 commit 同 transaction |
| F04 预览非承诺 | quote 席次／revision 已有保护，但没有通用管理 preview 或确认金额上限 | 持久化 preview、运行再核对、精确金额／上限政策，且先处理幂等重播 |
| F05 尚无管理工作恢复 | outbox 与 provider key 已存在，但没有管理 actor、command/job receipts | 加入 command、lease generation、job membership、逐项结果及 restart recovery |
| F06 next item 可能漂移 | `DispatchRefundNext`、部分 Run* 方法自行找下一笔或扫描全部 | by-ID dispatcher／逐项 helper；preview 固定 membership，避免运行新进对象 |
| F07 时钟与故障是 CLI 局部能力 | menu clock 与 server wall clock 不同；已有两种故障模式 | 固定每命令 business time，session／lease 保持 wall clock；fault ticket 按 operation 隔离 |
| F08 前端呈现责任未固定 | 还没有 React 项目、共用状态组件、精确金额 DTO | 确定前端套件分工、decimal string、状态与 stale/error 行为 |
| F09 UI 能力无逐项验收 | 只有 CLI／domain/API 既有测试 | 为 C01–C49 创建 HTTP 行为矩阵与真实本机 browser 情境 |
| F10 build／升级未定义 | 现有纯 Go 项目、既有 SQLite 文件 | adminui build tag、保留无 dist 的 Go build、增量 schema＋旧 DB fixture 验证 |

现有 `AcceptQuote`、变更命令、balance、provider key、reconciliation 与 migration guard 都重用。只有交易 helper、按 ID 工作、preview、admin persistence、查找 DTO 与 Web 层是添加责任；不重建计价引擎。

## 2. 任务与依赖

每项任务都包含程序、相关测试及必要契约更新。P1＝阻挡该阶段交付。此轮不把未完成能力降成日后 TODO。人工作业粗估以熟悉 Go／React 的工程师计、包含测试与审查，范围可重叠；AI 运行时间没有量测数据，不列虚构加速倍数。

```text
T01 → T04 → T05 → T06 → T07
T02 → T09        T03 ↗        ↘ T10 → T11
T11＋T13 → T12
T04 → T08 ────────────────────↗        ↘ T13 → T18
T07＋T08＋T09 → T14 → T15
                 ├→ T16
                 └→ T17（另依赖 T10、T13）
T13＋T18 → T19；T06＋T07＋T09 → T20
全部功能 T10–T20 → T21 → T22
```

以下路径均为预计 ownership；同一路径的重构串行完成，避免多个工作者同时修改交易骨架。未启动任何 subagent、worktree 或分支。

- [x] **T01（P1，0.5–1 日）— 冻结契约与回归基线。** 来源 F09/F10。文件：`docs/design/06–08`、`lab/*_test.go`。盘点既有 S01–S12/P01–P03 fixture，创建 old-schema fixture 与 C01–C49 mapping。依赖：无。验收：A01；既有测试结果另行保存，任何新发现的 domain 缺陷先列出，不以新 UI 掩盖。
- [x] **T02（P1，0.5–1 日）— 创建 React 与嵌入式 build 骨架。** 来源 F08/F10。文件：`web/admin/`、`cmd/lab/adminassets/`、`cmd/lab/admin.go`。加 route shell、pnpm lock、TypeScript、Vite、Ant Design ConfigProvider／App 与主题 token、build tags。依赖：T01。验收：A02；有／无 dist build、深链接刷新、资产路径与错误页都可判定。
- [x] **T03（P1，1–2 日）— 实作 session、CSRF、权限与专用路由。** 来源 F02。文件：`api/admin/session.go`、`permissions.go`、`router.go`、`cmd/lab/admin.go`、`.env.example`、`.gitignore`。依赖：T01。验收：A03/A04；`.env` 缺值／格式／优先序、登录失败限流、session 期限、Host/Origin、未授权、重启与旧 v1 隔离。
- [x] **T04（P1，1–2 日）— 添加 commerce/provider 的 versioned admin schema 与升级。** 来源 F05/F10。文件：`lab/admin_schema.go`、`lab/admin_migration_test.go`。依赖：T01。验收：A05；新库、旧库、重跑、失败 rollback、financial history 不变及旧 CLI 兼容。
- [x] **T05（P1，2–4 日）— 抽出交易 helper，保留既有 public wrapper。** 来源 F03/F06。文件：`lab/lab.go`、`billing.go`、`corrections.go`、`refunds.go`、`subscription_changes.go`、`immediate.go`、`usage.go` 及其他写入模块。依赖：T04。验收：A01/A06；无 nested transaction、public 回归、业务与 receipt 同 commit。此任务不一次改变全部金额政策。
- [x] **T06（P1，2–3 日）— 命令 admission、worker、receipt 与中断恢复。** 来源 F03/F05。文件：`lab/admin_commands.go`、`admin_worker.go`、`api/admin/commands.go`。依赖：T03/T05。验收：A06/A07/A08；canonical hash、同 key 重播、lease generation、权限撤销及不同 crash points。
- [x] **T07（P1，1–2 日）— 管理预览与精确 API 值类型。** 来源 F04/F08。文件：`lab/admin_previews.go`、`api/admin/types.go`、`errors.go`。依赖：T06。验收：A09/A10；preview 单命令绑定、expiry、不同 actor、source change、金额上限、int64 边界与结构化错误。
- [x] **T08（P1，1–2 日）— 管理查找骨架与内核 DTO。** 来源 F01。文件：`lab/admin_queries.go`、`api/admin/queries.go`。依赖：T04。验收：A11；分页与 allowlist filters、取消查找、来源版本、去敏及新数据跨页语意。各功能模块的查找在对应任务补齐。
- [x] **T09（P1，1–2 日）— 操作台共用 UI。** 来源 F08。文件：`web/admin/src/app/`、`components/`、`api/`。依赖：T02/T03/T08。验收：A12；以 Ant Design Table／Form／Modal 等创建表格、时间轴、预览、命令进度、权限与 stale/error/empty 状态；检查键盘与焦点、query invalidation、金额字符串输入及不可把一般 Popconfirm 当作金融确认流程。
- [x] **T10（P1，1–2 日）— 报价与订阅全流程。** 来源 F04/F09；C01–C06（self-service/change）。文件：`api/admin/subscriptions.go`、`lab/admin_subscription_commands.go`、`web/admin/src/features/subscriptions/`。依赖：T07/T09。验收：A13/A14；quote 与 binding 原子、新购、下期变更、取消／恢复、即时升级、过期／revision／席次冲突。
- [x] **T11（P1，1–2 日）— 付款与退款操作。** 来源 F05/F06；C07–C10、C15–C17。文件：`api/admin/payments.go`、`refunds.go`、`lab/refunds.go`、对应 frontend features。依赖：T10。验收：A15/A16；按 ID 派送、原 key 查证、部分付款、确定失败重试、UNKNOWN 保留、UI 不误报成功。
- [x] **T12（P1，1–2 日）— 帐单更正、credit 与升级补偿。** 来源 F04/F09；C11–C14。文件：`api/admin/corrections.go`、`lab/admin_correction_commands.go`、invoice/credit features。依赖：T11/T13。验收：A17；来源可追查、额度并行、不可重复更正、延迟／未提供服务补偿与既有金额 oracle 一致。
- [x] **T13（P1，1–2 日）— 固定 membership 的 jobs 与营运操作。** 来源 F05/F06；C44/C45 及其他 batch 基础。文件：`lab/admin_jobs.go`、`api/admin/jobs.go`、jobs feature。依赖：T06/T07/T08/T09。验收：A18；到期续约、权益刷新、逐项 receipt、部分失败、重启、关闭时停止 claim、membership 不漂移。
- [x] **T14（P1，1–2 日）— 产品、meter、价格与选价。** 来源 F04/F09；C18–C21。文件：`api/admin/catalog.go`、catalog feature、相关 domain Tx helpers。依赖：T07/T09。验收：A19；所有组件呈现、不可修改已发布价格、重播不重复发布、cohort 生效时点。
- [x] **T15（P1，1–2 日）— 价格迁移工作台。** 来源 F04/F05；C22–C25。文件：`api/admin/price_migrations.go`、price-migrations feature。依赖：T13/T14。验收：A20；完整预览、固定逐户 ID、冲突、略过、暂停／恢复与反向新批量。
- [x] **T16（P1，1–2 日）— 用量事件、关帐、重算与 CreditNote。** 来源 F06/F09；C26–C30。文件：`api/admin/usage.go`、usage feature、domain usage helpers。依赖：T13/T14。验收：A21；事件重复／内容冲突、撤销来源、晚到差额、rating 历史、正负差额分流。
- [x] **T17（P1，1–2 日）— 企业合约与 Net30。** 来源 F04/F09；C31/C32 及 C01/C02 的 contract 分支。文件：`api/admin/contracts.go`、contracts feature。依赖：T10/T13/T14。验收：A22；条款版本、合约客户身分、先开通、到期收款、缺后续价 hold 与明确转换。
- [x] **T18（P1，1–2 日）— 对帐、修复及人工决议。** 来源 F04/F05；C33–C35。文件：`api/admin/reconciliation.go`、reconciliation feature。依赖：T11/T13。验收：A23；expected/actual、证据、revision、稳定修复键、事后再核对，人工决议不等同资金修正。
- [x] **T19（P1，1–2 日）— 帐户灰度迁移。** 来源 F04/F09；C36–C43。文件：`api/admin/account_migrations.go`、account-migrations feature。依赖：T13/T18。验收：A24；映射、shadow、来源、readiness、切读／切写／停止与 adapter 所有入口。
- [x] **T20（P1，1–2 日）— 实验室时钟与故障隔离。** 来源 F07；C46–C49。文件：`lab/admin_clock.go`、`api/admin/lab.go`、lab feature。依赖：T06/T07/T09/T11。验收：A25；per-command 时刻、wall clock lease、一次性 fault ticket、不同操作不受影响、UI 环境标示。
- [x] **T21（P1，2–3 日）— 完整 browser／并行／恢复／性能验收。** 来源 F01/F05/F09。文件：`web/admin/e2e/`、`api/admin/*_test.go`、`lab/admin_*_test.go`。依赖：T10–T20。验收：A01–A28；真 Go server＋隔离 SQLite，覆盖 49 命令，不用全部 mock 成功代替。
- [x] **T22（P1，0.5–1 日）— 交付 build 与操作文档。** 来源 F10。文件：README、`docs/implementation/web-admin.md`、build scripts、schema upgrade 指引。依赖：T21。验收：A29/A30；全新环境、既有 DB、建置 binary、登录到完整操作；核对 coverage，只有通过才更新完成追踪。

任务粗估不是交付承诺：主要风险集中在 T05–T07 的交易重构与恢复。T01 后依真实测试与 module 规模调整估时，保留功能范围，不以缩减 C01–C49 赶期限。

## 3. 五阶段交付范围

| 阶段 | tasks | 可以宣称完成的内容 |
| --- | --- | --- |
| W1 | T01–T09 | 管理基础、读取与命令框架；不宣称完整业务操作已完成 |
| W2 | T10 | self-service 订阅工作台；合约报价分支仍待 W4 |
| W3 | T11/T13/T12/T18 | 付款、退款、credit、更正、营运 jobs、对帐 |
| W4 | T14–T17 | 价格／meter／cohort／迁移、用量、企业合约 |
| W5 | T19–T22 | 帐户迁移、实验室、完整验收、可运行本机交付 |

可以独立准备的部分：T02 与 T04、T03；T14 后的 T15/T16/T17 UI 可按 feature 分开。前提是共享 DTO、transaction helper、command dispatcher 已稳定；`lab/admin_commands.go` 等骨架须单一 owner。这是未来分工策略，此次没有开始平行实作。

## 4. 运行与验证命令（拟添加）

下列前端 scripts 与 admin build target 已实作；本机验证、构建／升级／登录证据见 [实作记录](../implementation/web-admin.zh-CN.md)，命令保留供交付重跑。

```sh
go test ./...
go vet ./...
pnpm --dir web/admin install --frozen-lockfile
pnpm --dir web/admin typecheck
pnpm --dir web/admin test:run
pnpm --dir web/admin build
go build -tags adminui -o /tmp/billforge-admin ./cmd/lab
pnpm --dir web/admin test:e2e
```

前端 build script 将 dist 同步至 `cmd/lab/adminassets/dist`（生成物忽略，不提交），embed 文件由 build tag 选用；测试 build 缺失资产会明确失败，缺省 API build 有 fallback。E2E script 负责启动已 build 的 Go server、产生隔离 DB、通过 `.env` 测试帐密登录、退出及清理；禁止默认使用真实工作数据库。

schema 与 command 测试先用 targeted `-run`，跨 transaction refactor 后跑完整回归。race 测试只针对 worker／session／clock 共用状态的具体并行风险，Go SQLite 的交易一致性仍用确定性的 DB interleavings 验证。

## 5. 范围、风险与结案规则

不实作真实金流、税、多币别、正式总帐、公开部署、CRM、完整帐号管理、ARR dashboard。这些不是本次 49 项命令中的缺口，不需要另建空泛 TODO。

规划没有待用户决定的阻塞项；React 已确认，其余技术与预览政策采本文决策。若之后要公开部署、多名真实操作者或接 PSP，需另做需求与威胁模型，不能直接宣称本机方案已满足。

**设计与开发结案（2026-10-03）**：T01–T22 与 A01–A30 在列明的本机范围通过，49 项命令均有适用 contract 与 browser 证据。上方原验收条件不变；保留范围限制及首次 process 就绪超时根因未确认的记录。

## 6. 本轮规划检查纪录

2026-09-26 完成文档检查：C01–C49 共 49 项命令均有任务对应；T01–T22 依赖无循环；A01–A30 共 30 组验收均被任务引用；文档相对链接可解析。React 与 `.env` 单一管理帐密登录已纳入一致契约，旧 bootstrap 提案已被取代。

历史 checkpoint（已由上方最终本机验收取代）：以开始规划前的内容 hash 核对，60 个既有 Go／module 文件全部未变更。此次没有创建前端项目、修改功能程序、安装依赖、创建实际帐密或运行添加验收。上述检查证明规划产物一致，不代表 Web Admin 功能已完成。
