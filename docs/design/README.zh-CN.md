# Commerce MVP：A–D 设计推演

[English](README.md) | [繁體中文](README.zh-TW.md) | **简体中文**

A–D 是设计与纸上推演。E、F1–F8、P01–P03 已有本机实作，但整体 MVP 尚未经生产验证。实作证据与剩余完成条件见 [MVP 完成追踪](../implementation/mvp-completion-tracker.zh-CN.md)；范围见[平台计划](../commerce-lab-plan.zh-CN.md)。

| 阶段 | 文档 | 审阅重点 |
| --- | --- | --- |
| A 政策 | [01 计价版本与业务政策](01-pricing-and-policies.zh-CN.md) | `PriceVersion`、D01–D12、舍入、比例计费，以及付款与权益时点 |
| B 模型 | [02 事实所有权与不变条件](02-domain-and-invariants.zh-CN.md) | 状态机、原子边界，以及 I01–I21 的反例与保护点 |
| C 故障 | [03 情境、对帐与修复](03-scenarios-and-reconciliation.zh-CN.md) | S01–S12 的预期事实、金额与恢复行为 |
| D 平台 | [04 API、兼容性与演进](04-platform-apis-and-migration.zh-CN.md) | P01–P03、两种用户、价格迁移与渐进推出 |

建议先读政策，再读事实所有权与故障情境，最后读 API 与迁移设计。所有时间使用 UTC；服务与价格的有效区间为半开区间 `[start, end)`。除非另有注明，金额均使用 USD 最小单位。

设计不包含税务、生产总帐、收入认列引擎、多币别、任意促销、完整阶梯定价或真实支付服务。解读本机测试结果时也应遵守这些边界。

## Web Admin 设计

- [05 Web Admin 设计方案](05-web-admin.zh-CN.md)说明管理接口、CLI／API 关系、预览、权限与命令恢复。实作证据见 [Web Admin 指南](../implementation/web-admin.zh-CN.md)及[逐动作盘点](../implementation/web-admin-action-audit.zh-CN.md)。
- [06 工程契约](06-web-admin-contracts.zh-CN.md)定义 React 接口、`.env` 管理员登录、API 契约与 C01–C49 命令矩阵。
- [07 实作计划](07-web-admin-implementation-plan.zh-CN.md)记录 22 项实作任务及其依赖。
- [08 验收计划](08-web-admin-test-plan.zh-CN.md)定义 A01–A30 验收检查。
