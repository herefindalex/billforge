# Billforge 商务系统正确性实验室

[English](README.md) | [繁體中文](README.zh-TW.md) | 简体中文

Billforge 是一个在本地运行的商务系统正确性实验项目。它使用 Go 和两个独立的 SQLite 数据库，演练定价版本、订阅与合同、付款与权益、用量关账、更正、退款、对账及账户迁移。设计背景见[平台计划](docs/commerce-lab-plan.zh-CN.md)和 [A–D 设计推演](docs/design/README.zh-CN.md)；实现证据见 [MVP 完成追踪](docs/implementation/mvp-completion-tracker.zh-CN.md)。

[实施与验收文档索引](docs/implementation/README.zh-CN.md)收录所有交付记录、操作指南与证据盘点。

## 当前状态

本地 CLI、API，以及使用 React 和 Ant Design 构建的 Web Admin 已可使用。Web Admin 的 A01–A30 验收仍在进行；[验收记录](docs/implementation/web-admin.zh-CN.md)区分已验证案例与剩余检查。

## 这个实验项目验证什么

- 已发布的价格和合同保留版本，后续变更不会悄悄改写既有应收义务。
- 管理操作使用预览、来源版本检查、幂等键和命令收据。时钟、支付服务决策和故障票据等实验室控制操作，在受理新命令前也必须取得服务端预览。付款或退款派送预览失效时，Web Admin 会显示原预览和当前操作状态，供管理员核对。
- 商务数据库与模拟支付服务数据库彼此独立，可跨越外部系统边界测试重试、响应丢失和对账。
- 对账修复会将支付服务金额不符的检查限定在待修复的付款；该付款会被阻止，但其他付款仍可沿原操作恢复。如果浏览器丢失修复成功的响应，管理员可以使用原 request key 找回命令，而不会重复执行修复。

## 环境要求与测试

需要 Go 1.27、CGO 和 C 编译器；Web Admin 还需要 Node.js 和 pnpm。端到端测试还需要 Python 3，以及 Chrome／Chromium 或通过 Playwright 安装的 Chromium。付款使用本地 fake provider，不连接真实支付服务。

```sh
go test ./...
pnpm --dir web/admin install --frozen-lockfile
pnpm --dir web/admin build
pnpm --dir web/admin test:e2e
go build -tags admin_ui -o /tmp/billforge-admin ./cmd/lab
```

如需确认 C01–C49 每项管理操作都由浏览器提交，并生成成功的命令回执，请在运行端到端测试时记录验收文件：

```sh
audit_file="$(mktemp /tmp/billforge-action-cases.XXXXXX)"
BILLFORGE_E2E_ACTION_CASE_AUDIT="$audit_file" pnpm --dir web/admin test:e2e
node web/admin/e2e/audit-action-cases.mjs "$audit_file"
```

## CLI 与本地 API

```sh
mkdir -p ./billforge-data
go run ./cmd/lab
go run ./cmd/lab menu ./billforge-data/commerce.db ./billforge-data/provider.db
go run ./cmd/lab serve ./billforge-data/commerce.db ./billforge-data/provider.db 127.0.0.1:8080
go run ./cmd/lab demo
go run ./cmd/lab renewal-demo
go run ./cmd/lab correction-demo
go run ./cmd/lab demo /tmp/billforge-commerce.db /tmp/billforge-provider.db lost_response
go run ./cmd/lab demo /tmp/billforge-crash-commerce.db /tmp/billforge-crash-provider.db crash_after_provider
```

交互式菜单可执行操作并查询当前状态，见 [CLI 使用说明](docs/implementation/interactive-cli.zh-CN.md)。`serve` 仅接受 loopback 地址；部分内部 HTTP 操作需要设置 `BILLFORGE_INTERNAL_TOKEN`，见 [v1 API 说明](docs/implementation/phase-p2-v1-api.zh-CN.md)。

## Web Admin

复制 `.env.example` 为 `.env`，设置 `BILLFORGE_ADMIN_USERNAME` 和 `BILLFORGE_ADMIN_PASSWORD`（12–72 bytes）。密码只存放在服务器环境或 `.env` 中，不要使用 `VITE_` 前缀。限制 `.env` 只供本地用户读取。构建前端后，使用两个**不同**的持久化数据库文件启动：

```sh
mkdir -p ./billforge-data
pnpm --dir web/admin install --frozen-lockfile
pnpm --dir web/admin build
go run ./cmd/lab admin ./billforge-data/commerce.db ./billforge-data/provider.db 127.0.0.1:8080
```

打开 `http://127.0.0.1:8080/admin/`。管理服务器仅绑定明确的 loopback IP，并要求请求的 Host 与监听地址一致。如需使用其他环境文件，将 `--env-file path` 放在数据库路径之前。进程环境变量优先于文件中的值。

合同列表可打开合同版本详情，查看已发布条款、相关报价与订阅。详情页的报价和订阅各最多显示 20 条；使用“查看全部报价”或“查看全部订阅”可打开按该合同版本筛选的分页列表。也可以从详情页创建已预填客户与合同版本的报价。

“批次工作”页面列出近期工作、逐项计数和详情链接；游标分页可避免新工作插入后移动已读页面。

“价格迁移”详情显示由服务器计算的各项状态总数。逐项表格每页最多读取 20 条，支持按状态筛选，并以精确的最小货币单位显示金额。刷新失败时会以警示标明上次成功读取的数据，并停用依赖旧批次状态的操作。应用或跳过项目、恢复批次后，请刷新详情以查看最新计数并重新分页。

账单、订阅、Credit、付款、退款、价格和合同详情也遵循相同的读取失败规则：临时故障会标明缓存数据并停用依赖这些数据的操作；权限被拒时会隐藏缓存数据，包括列表和总览计数。重新读取成功后才会显示最新数据。

共用操作表单和订阅方案变更的命令结果也遵循这一规则。暂时无法读取时，界面会用警示标明上次成功读取的结果，并停用依赖命令状态的操作。权限被拒时，缓存的命令结果会隐藏；重新读取成功后才会再次显示。

具备 `lab.control` 权限时，“实验控制”可查看 fake provider 状态，以及分页的收款和退款记录。这些数据来自独立的本地数据库；API 金额仍以精确的最小货币单位字符串返回。

如需限制管理员权限，可设置 `BILLFORGE_ADMIN_CAPABILITIES=read`，或提供其他以逗号分隔的权限。未设置时，本地管理员拥有完整权限。权限变更需要重启；恢复中的命令会根据新权限和现有外部义务重新判定，详见 [Web Admin 实现记录](docs/implementation/web-admin.zh-CN.md)。

如需将前端资源嵌入二进制文件，先构建前端，再运行上方的 `go build -tags admin_ui`。不带此 tag 的开发运行会从 `cmd/lab/adminassets/dist` 读取构建产物。完整操作、升级与恢复流程见 [Web Admin 使用与验收记录](docs/implementation/web-admin.zh-CN.md)。

## 范围边界

本项目不涵盖真实支付、税务、多币种、正式总账或公开部署。
