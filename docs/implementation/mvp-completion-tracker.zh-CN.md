# Billforge MVP 完成追踪

[English](mvp-completion-tracker.md) | [繁體中文](mvp-completion-tracker.zh-TW.md) | **简体中文**


此清单依 [A–D 设计](../design/README.zh-CN.md)与 [总计划](../commerce-lab-plan.zh-CN.md)验收，避免把文档推演误记成可运行能力。每项只有在实作、金额 oracle／故障测试、CLI 或 API 操作入口，以及限制说明均齐备时才标完成。

| 范围 | 目前状态 | 下一个可验收结果 |
| --- | --- | --- |
| A–D 政策、模型、故障、平台推演 | 文档完成 | 随实作保持设计与证据同步。 |
| S01、S04、S05：Basic 新购、UNKNOWN、webhook／权益恢复 | E 切片完成 | 保持回归。 |
| S02、S03：续约、grace、暂停与恢复 | F1 切片完成 | 保持回归。 |
| S11：减额更正、funded credit、退款 | F2 切片完成 | 保持回归；进一步补服务延迟更正所需的来源。 |
| 操作入口 | 交互式菜单 CLI 完成 | 新功能同步加入菜单和状态查找。 |
| S06：下期升降级、取消、恢复 | [F3 切片完成](phase-f3-schedules.zh-CN.md) | 保持期界重播、revision 冲突及旧数据库升级回归。 |
| S07：期中升级与 proration | [F4 切片完成](phase-f4-immediate-upgrade.zh-CN.md) | 保持 $40 补差额、UNKNOWN 不提前切换、延迟开通后 $5.34 更正及期满未服务全额补偿回归。 |
| S08：Pro v1/v2 与 cohort 迁移 | [F5 切片完成](phase-f5-price-migrations.zh-CN.md) | 保持已发布价格不可变、旧客钉选、预览、逐户 CAS、部分暂停与反向迁移回归。 |
| S09：用量、关帐与晚到重算 | [F6 切片完成](phase-f6-usage.zh-CN.md) | 保持 20,003→$0.00、20,010→下一期 $0.01、负差额 CreditNote 与事件冲突／重播回归。 |
| S10：企业合约与 Net30 | [F7 切片完成](phase-f7-contracts.zh-CN.md) | 保持 Acme 五席 $75、先开通、Net30 到期收款、缺后续价 hold 及明确后续价转换回归。 |
| S12：对帐差异与修复 | [F8 切片完成](phase-f8-reconciliation.zh-CN.md) | 保持差异分类、证据、前置 revision、稳定修复键与事后再核对回归。 |
| P01：配置式新 SKU | [计量 SKU 切片完成](phase-p1-metered-sku.zh-CN.md) | 保持 AI tokens 新购、事件、关帐、续约发票与旧 tasks 计价回归。 |
| P02：多 consumer 与 v1 API 兼容 | [本机 v1 API 切片完成](phase-p2-v1-api.zh-CN.md) | 保持旧 client 新费用拒绝、合约能力、UNKNOWN／grace、订阅变更的价格／席次钉选、应付时点估值及更正读取契约回归。 |
| P03：Account／Commerce 边界与灰度迁移 | [本机迁移演练完成](phase-p3-account-cutover.zh-CN.md) | 保持来源 ID 映射、shadow、历史回填、单一写入者、停止条件及 adapter 读取回归。 |
| Web Admin：登录、查找、C01–C49 操作 | [本机交付完成](web-admin.zh-CN.md) | 最新限定验收：30／30 个场景通过，T01–T22 全完成，Web Admin 本机交付完成。可重跑证据与限制见 [动作证据盘点](web-admin-action-audit.zh-CN.md)。 |

可发布的 `PriceVersion` 组件、席次快照及订阅 revision／assignment 已在 F3 落地；期中升级与补偿已在 F4 落地；cohort 与既有客户价迁移已在 F5 落地；tasks 用量关帐与更正已在 F6 落地；企业合约与 Net30 已在 F7 落地；S12 对帐修复、P01 新 meter、P02 本机 v1 API 与 P03 帐户迁移演练亦有程序及验收测试。真实支付、税、多币别、正式总帐与生产指针仍属原计划明定的 MVP 外范围。

2026-10-03 验收更新：两个既有 C07／C08 浏览器竞争案例已补实际收款、固定金额、allocation 与收据核对，2／2 通过，详见 [动作证据盘点](web-admin-action-audit.zh-CN.md)。 最终验收见下方。

最新限定验收：30／30 个场景通过，T01–T22 全完成，Web Admin 本机交付完成。可重跑证据与限制见 [动作证据盘点](web-admin-action-audit.zh-CN.md)。
