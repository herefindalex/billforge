# F3：Pro 席次价格与下期订阅调度

[English](phase-f3-schedules.md) | [繁體中文](phase-f3-schedules.zh-TW.md) | **简体中文**


日期：2026-09-26。这个切片完成 S06 的下期升降级、期末取消及取消前恢复，并创建后续用量／价格迁移共用的价格组件基础。

## 已落地的行为

- 内置 Pro v1 为每期固定 $50 加每付费席 $10；五席的前付费发票为 $100，`fixed` 与 `seats` 分别成为不可变 invoice line。Basic 维持 $20。Pro v1 同时记录 20,000 tasks allowance 和每 task $0.001 的后付费组件，但本切片**尚未产生用量发票**。
- `PriceVersion` 具有 `draft/published` 状态和有效区间；已发布版本及其组件由 SQLite trigger 阻止原地修改。报价固定版本、席次、金额与 fingerprint。旧数据库打开时补字段与初始 assignment；原 Basic 订阅金额和付款身分保持不变。
- `ScheduleNextPlan` 与 `ScheduleCancel` 使用预期 subscription revision、稳定 request key 与单一待生效调度约束。下一期价格版本在调度时选定；当期 plan 不提前改动。同一边界的取消与方案变更互斥。
- `ResumeCancel` 仅在取消生效前成立，保留取消及恢复的 audit。期界 `RunRenewals` 在同一交易中关闭原 pricing assignment、打开新 assignment、核定新价格发票，或过帐 `subscription_ends` 而不产生续约发票。重跑不会产第二张。
- 订阅结束后状态查找显示 `ended`，权益重建显示 `expired/cancel_at`。已结束订阅不再续约；重新购买是另一笔新订阅。

## 验收证据

- `TestProFiveSeatsKeepsVersionAndLinesAcrossRenewal`：$50＋$10×5＝$100，续约仍是 $100；已发布价和组件不可修改。
- `TestS06NextPeriodChangeUsesRevisionAndAssignmentHistory`：旧 Basic 当期不变；10-01 才改 Pro 五席、开 $100 续约，重跑不重复；重播／不同 payload／旧 revision／竞争调度的结果固定。
- `TestS06CancelResumeAndBoundaryExpiry`：取消与升级冲突、取消前恢复、再次调度取消、期界无续约、权益过期及结束后拒绝 resume。
- `TestMenuSchedulesProAtNextBoundary`：菜单可完成 Basic 购买、排下期 Pro、推进时钟与续约。旧 E schema 升级测试及全部既有场景保持通过。

此阶段没有 S07 的期中 proration、S08 的逐户版本迁移、S09 的用量关帐或 S10 合约。价格组件及 assignment 为它们提供来源，但不能单凭数据表宣称那些情境已完成。
