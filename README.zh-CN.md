# Billforge 商务系统正确性实验室

[English](README.md) | [繁體中文](README.zh-TW.md) | 简体中文

Billforge 是一个在本地运行的商务系统正确性实验项目。它使用 Go 和两个独立的 SQLite 数据库，演练定价版本、订阅与合同、付款与权益、用量关账、更正、退款、对账及账户迁移。设计背景见[平台计划](docs/commerce-lab-plan.md)和 [A–D 设计推演](docs/design/README.md)；实现证据见 [MVP 完成追踪](docs/implementation/mvp-completion-tracker.md)。

## 这个实验项目验证什么

- 已发布的价格和合同保留版本，后续变更不会悄悄改写既有应收义务。
- 管理操作使用预览、来源版本检查、幂等键和命令收据。付款或退款派送预览失效时，Web Admin 会显示原预览和当前操作状态，供管理员核对。
- 商务数据库与模拟支付服务数据库彼此独立，可跨越外部系统边界测试重试、响应丢失和对账。

## 环境要求与测试

需要 Go 1.27、CGO 和 C 编译器；Web Admin 还需要 Node.js 和 pnpm。端到端测试还需要 Python 3，以及 Chrome／Chromium 或通过 Playwright 安装的 Chromium。付款使用本地 fake provider，不连接真实支付服务。

```sh
go test ./...
pnpm --dir web/admin install --frozen-lockfile
pnpm --dir web/admin build
pnpm --dir web/admin test:e2e
go build -tags admin_ui -o /tmp/billforge-admin ./cmd/lab
```

## CLI 与本地 API

```sh
go run ./cmd/lab
go run ./cmd/lab menu ./billforge-data/commerce.db ./billforge-data/provider.db
go run ./cmd/lab serve ./billforge-data/commerce.db ./billforge-data/provider.db 127.0.0.1:8080
go run ./cmd/lab demo
go run ./cmd/lab renewal-demo
go run ./cmd/lab correction-demo
go run ./cmd/lab demo /tmp/billforge-commerce.db /tmp/billforge-provider.db lost_response
go run ./cmd/lab demo /tmp/billforge-crash-commerce.db /tmp/billforge-crash-provider.db crash_after_provider
```

交互式菜单可执行操作并查询当前状态，见 [CLI 使用说明](docs/implementation/interactive-cli.md)。`serve` 仅接受 loopback 地址；部分内部 HTTP 操作需要设置 `BILLFORGE_INTERNAL_TOKEN`，见 [v1 API 说明](docs/implementation/phase-p2-v1-api.md)。

## Web Admin

复制 `.env.example` 为 `.env`，设置 `BILLFORGE_ADMIN_USERNAME` 和 `BILLFORGE_ADMIN_PASSWORD`（12–72 bytes）。密码只存放在服务器环境或 `.env` 中，不要使用 `VITE_` 前缀。限制 `.env` 只供本地用户读取。构建前端后，使用两个**不同**的持久化数据库文件启动：

```sh
pnpm --dir web/admin install --frozen-lockfile
pnpm --dir web/admin build
go run ./cmd/lab admin ./billforge-data/commerce.db ./billforge-data/provider.db 127.0.0.1:8080
```

打开 `http://127.0.0.1:8080/admin/`。管理服务器仅绑定明确的 loopback IP，并要求请求的 Host 与监听地址一致。如需使用其他环境文件，将 `--env-file path` 放在数据库路径之前。进程环境变量优先于文件中的值。

合同列表可打开合同版本详情，查看已发布条款、相关报价与订阅。详情页的报价和订阅各最多显示 20 条；使用“查看全部报价”或“查看全部订阅”可打开按该合同版本筛选的分页列表。也可以从详情页创建已预填客户与合同版本的报价。

如需限制管理员权限，可设置 `BILLFORGE_ADMIN_CAPABILITIES=read`，或提供其他以逗号分隔的权限。未设置时，本地管理员拥有完整权限。权限变更需要重启；恢复中的命令会根据新权限和现有外部义务重新判定，详见 [Web Admin 实现记录](docs/implementation/web-admin.md)。

如需将前端资源嵌入二进制文件，先构建前端，再运行上方的 `go build -tags admin_ui`。不带此 tag 的开发运行会从 `cmd/lab/adminassets/dist` 读取构建产物。完整操作、升级与恢复流程见 [Web Admin 使用与验收记录](docs/implementation/web-admin.md)。

Web Admin 的 A01–A30 验收矩阵及剩余检查列于 [Web Admin 验收记录](docs/implementation/web-admin.md)。

本项目不涵盖真实支付、税务、多币种、正式总账或公开部署。
