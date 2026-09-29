# F1. 月续约、失败收款与权益宽限

[English](phase-f1-renewals.md) | [繁體中文](phase-f1-renewals.zh-TW.md) | **简体中文**


状态：Go＋SQLite 本机切片，对应 [S02／S03 设计](../design/03-scenarios-and-reconciliation.zh-CN.md)。已运行 `go test ./...`、`go vet ./...` 与 `renewal-demo`；仍不是完整 Commerce MVP 或生产保证。

## 持久事实与时间政策

- `billing_periods` 保存每个订阅的 `period_index`、`[period_start, period_end)`、`due_at`、`invoice_id`，同订阅／index 与同订阅／start 各自唯一。初购是 index 0。新一期只有前一期 invoice 已全额分配、订阅有效，且 worker 在该期开始后的 7 天内运行时才产生。
- 周期锚点是初次**确认付款并激活订阅**时的 UTC 月日与时分秒；接受 Quote 到确认收款之间的等待不消耗服务月份。31 日遇短月截至月末，之后回 31 日；例如 2027-01-31 → 02-28 → 03-31 → 04-30。续约读订阅钉选的 PriceVersion，不从现在的 CatalogSelection 重新选价。已生效帐期不可回写。
- 续约 invoice 于期初核定，固定费 $20 预付，`due_at=period_start`。付款操作有自己的固定 provider key／payload；确定失败不改原操作，而由带 request key 的 `RetryFailedPayment` 创建新 operation。相同 request key 回原 operation；不同 key 在尚有未决／成功操作时拒绝。
- Subscription 的商务状态仍为 `active`。权益投影用当前帐期、invoice allocation、7 天 grace 与时钟计算：本期已付 `active`；未付且在 grace 内 `grace`；grace 届满且无待查付款 `suspended`。`submitted/unknown` 超过 deadline 时暂留 `grace`，理由是 `payment_verification_pending`，直到查原 provider 操作或人工处理。超出帐期且没有新的可服务期间则 `suspended`。
- 续约 worker 到达或晚于 grace 截止才运行，不补开可能未提供服务期间的全额票据，而写唯一的 `renewal_holds`。已结束帐期的失败付款不能直接重试收全额。续约首次付款或失败后新操作若晚于 grace 截止才送出，也停止为 `late payment requires service correction review`；本切片只自动处理截止时刻以前（含截止瞬间）的全额补款。未付前一期也不自动产下一期预付帐单。

## E → F1 数据升级

旧 E schema 曾限制每订阅一张 invoice、每 invoice 一个 PaymentOperation。`Open` 先在交易内拷贝这两张表，移除限制并保留既有 ID／行数据；再补建 index 0 的 billing period、权益来源字段及新 trigger。fake provider 旧的成功 capture 也升级为可记录确定失败的结果表。升级后运行 `foreign_key_check`。测试用旧形状的 SQLite 文件验证原 invoice line、operation 与 provider 结果仍可恢复，且旧订阅能续约。旧 E 数据没有独立保存实际初次开通时刻，回填只能以 `created_at` 作锚点；若旧订阅曾延迟开通，必须另行查证与更正，不能宣称回填已恢复真实服务期间。

## 可重现的证据

```sh
go test ./...
go vet ./...
go run ./cmd/lab renewal-demo
```

`renewal-demo` 的四个状态是：初期收款成功／权益 active；10 月续约确定失败／grace；第 7 天到期／suspended；用新 operation 补款／active。输出包含两期发票和两个 provider key 的来源身分。

| 测试 | 验证点 |
| --- | --- |
| `TestS02MonthEndAnchorAndIdempotentRenewal` | 01-31 → 02-28 → 03-31 → 04-30、同一期 worker 重跑不重复产票、付费后权益恢复。 |
| `TestDelayedInitialConfirmationMovesServiceAnchor` | 9/1 发起且 provider 已收、9/3 才确认并激活，首期为 9/3–10/3；不在 10/1 提早续约。 |
| `TestS03FailedRenewalGraceSuspensionAndRecovery` | 确定失败的原操作留存，重开 DB 后 grace 到期暂停；带稳定 request key 的新操作补款且只分配一次。 |
| `TestUnknownRenewalRetainsVerificationGrace` | provider 已收但本地 UNKNOWN，超过 7 天不当作失败；查原操作后成为 active。 |
| `TestMissedRenewalGraceCreatesHoldInsteadOfBackBilling` | 迟到 worker 不补收未提供期间，重跑只留下同一 hold。 |
| `TestFailedPeriodCannotBeChargedAfterItEnds` | 帐期结束后禁止用原月全额重试。 |
| `TestLateFullAmountDispatchRequiresReview` | grace 后尚未送出的全额续约操作不得首次扣款，避免收取未提供期间。 |
| `TestUpgradeStageEDatabasesPreservesFacts` | 旧 schema 的发票行、付款身分与 provider 结果保留，补建帐期后仍可续约。 |

## 下一个实作边界

这个切片只有 Basic 固定费，没有更正、credit／refund、晚到用量、合约或 cohort 迁移。`renewal_holds` 是可稽核的停止信号，尚无人工解除与差额计算流程；`payment_verification_pending` 也需要调度查找与停滞告警。若付款已确认但权益 projector 停滞，实际服务比订阅激活晚，还需要服务延迟更正；目前只记录 pending 投影。下一步应先做不可变更正与 funded credit 的来源／保留额，再加入用量关帐和 PriceVersion 的完整组件计价。
