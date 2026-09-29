# A. 计价版本与业务政策

[English](01-pricing-and-policies.md) | [繁體中文](01-pricing-and-policies.zh-TW.md) | **简体中文**


状态：Billforge lab 的设计决策。输入是 [整体方案](../commerce-lab-plan.zh-CN.md)及 34 份研究；这些规则是本 lab 的选择，不宣称外部产品都采同一政策。所有时间用 UTC，[start, end) 表示包含起点、不包含终点。

## PriceVersion 的责任

Product 回答「卖什么」；Plan 回答「Basic 或 Pro 等长期识别码」；**PriceVersion 回答一个已发布版本如何收费**。它是可运行的规则数据，由定价引擎解读。已发布版本的经济内容不可更动。

最小结构如下；字段名是概念契约，尚不是 SQL schema：

| 字段 | 类型／例子 | 规则 |
| --- | --- | --- |
| id、plan_id、version | pro-v2、pro、2 | id 全域唯一；(plan_id, version) 唯一且递增。Plan 不改名来代替价格变更。 |
| publication_state | draft／published | draft 可修；published 之后经济 payload 不可修改。停止出售用另行审计的 availability event。 |
| effective_from、effective_to | 2026-10-01T00:00Z、可空 | **只界定新订阅／明确重新指派的可选区间**；不代表旧订阅在该时刻自动迁移。 |
| currency、billing_cadence | USD、calendar_month | 本 lab 仅 USD／月。帐期锚点在 Subscription，非所有客户共用的 PriceVersion 字段。 |
| charge_definitions | 下表的版本内值对象 | 使用稳定 component_code，保存计价种类、单位、数值、时点及必要 meter。 |
| rounding_policy_ref | component-period-segment／HALF_EVEN | 先以精确值聚合，再于明确范围舍入至 cents；不能逐事件先舍入。 |
| proration_policy_ref | elapsed_utc_seconds／HALF_EVEN | 变更计价需由版本能找回当时政策；没有期中变更的组件也明确标示。 |
| supersedes_id、published_at、checksum | pro-v1、时间、payload hash | 供追溯与 quote 验证；checksum 不能取代数据库不可变约束。 |

ChargeDefinition 在一个 PriceVersion 内至少包含 component_code、kind、unit、rate／amount、charge_timing、aggregation_policy 及是否可按比例计算。按种类补字段：

| Kind | 必要字段与例子 | 计算 |
| --- | --- | --- |
| FixedCharge | amount_minor=5000、UPFRONT | 期间固定 $50。 |
| PerSeatCharge | unit_amount_minor=1000、quantity_min=1、unit=seat、UPFRONT | $10 × 该服务区段已承诺付费席次。 |
| IncludedQuantity | meter_id=tasks、quantity=20000、unit=task | 当期有 20,000 tasks 的免费计价 allowance；它不是服务停止上限。 |
| UsageOverage | meter_id=tasks、unit_rate_decimal=`0.001`、ARREARS | max(期间 tasks − allowance, 0) × 精确单价，聚合后才舍入。 |

每个 `charge_definitions` 项目还需要版本内唯一的 `component_code`、`kind`、`unit`、`charge_timing`；有用量的项目需指定 `meter_id`。金额字段以整数 cents 保存固定／席次费，超额单价以十进位字符串保存，禁止 binary float。`IncludedQuantity` 是计价 allowance，与权益配额分离。字段的合法组合应在发布时检查，例如 `UsageOverage` 必须指向同版本、同 meter 的 allowance 定义；否则发布失败。

Pro v2 可由 FixedCharge $50、PerSeatCharge $10、IncludedQuantity 20,000 tasks、UsageOverage $0.001／task 组成。Basic v1 只有 FixedCharge $20。组件代号例如 base、seat、tasks_included、tasks_overage 必须在版本内唯一，发票行项能引用原代号及版本 ID。

**不放进 PriceVersion 的数据**：客户席次与事件用量、特定客户折扣或合约、目前付款状态、权益是否已打开、帐期实际锚点、对某客户的迁移决定。这些属于变动的商业事实与其他政策版本。EntitlementPolicyVersion 独立，SubscriptionPricingAssignment 同时指向定价版本及权益政策版本；Enterprise ContractVersion 可在允许的组件上覆写价与付款条款。

Quote 保存 customer、subscription revision、选到的 PriceVersion ID／checksum、ContractVersion、seat quantity、effective_at、逐组件金额、未来用量费率、rounding policy、过期时间及 fingerprint。InvoiceLine 再保存核定时的组件、期间、数量、精确率、未舍入值、舍入金额与来源引用。Quote 是有期限的预览；核定发票才形成不可改的财务事实。

### 价格解析与版本发布

1. 客户和 API 指出 plan，不自行算价。对新指派，以购买时间找可用的缺省 PriceVersion；既有订阅先读自己钉选的 assignment。任何 cohort 实验使用明确的 eligibility／assignment 记录。
   `effective_from`／`effective_to` 是版本的候选区间，不是唯一缺省的充分条件。另以追加式 `CatalogSelection` 切换事件（plan、cohort、`effective_at`、price_version_id、发布人及时间）指定缺省：每个 scope 在某时刻采最后一个已生效事件，直到下一事件；同 scope＋`effective_at` 不许两个目标。发布 v2 只追加切换事件，不修改 v1。已产生的 Quote 在短期限内仍可接受其钉选版本，前提是版本仍属候选区间、没有明示停止出售事件，且订阅 revision／fingerprint 未变；不能在接受时暗换成新缺省。旧订阅仍依原 assignment 计价。
2. 若有 ContractVersion，核对有效区间、允许覆写的 component_code、币别、付款条款与客户身分。合约缺少必要组件时拒绝，不悄悄回落到 current catalog。
3. 使用 component definitions、客户承诺的 quantity 和计量事实计价。缺 meter、缺 component 或多个冲突的缺省版本均拒绝。
4. 接受 Quote 时检查时效、context fingerprint 与订阅 revision；支付操作使用冻结金额与稳定 operation ID。发票核定保存同一价源和中间值。
5. 发布 v3 只改新购缺省。旧客保留 v2；迁移要有 preview、cohort、effective_at、逐订阅操作身分及停损能力。

发布验证：无负价、同版本无重复 component_code、meter 与 unit 对应、included quantity 非负、同一 meter 的 allowance 不含糊、精确 decimal 可在系统上限内计算。对同一 plan 的缺省新购版本，时间窗不可产生两个无优先序的候选；实验 cohort 必须显式选择。已发布版本若要提前停卖，记新的 availability event；它不改原定价 payload 和既有 assignment。

此设计借鉴 OpenMeter 的版本钉选、Kill Bill 的生效日期、Lotus 的方案迁移、Shopware 的规则上下文；以上做法在细节上并不相同，故 Billforge 的解析顺序和迁移政策必须自己明订。

## 政策决策表

| ID | Lab 采用的政策 | 主要理由与刻意接受的成本 |
| --- | --- | --- |
| D01 | 固定费及席次预付；用量后付。 | Quote 能明说现在应收与未来变动费。需保留两种 charge group 的帐期身分。 |
| D02 | UTC 月周期按原月日锚点，短月份截月底；区间 [start, end)。 | 续约可重播，月长不同时仍需测界线。 |
| D03 | 定价中间值为精确 decimal／有理数，按「组件 × 服务区段」聚合后 half-even 舍入至 cents。 | 可重建 $0.001 task 的最终 cents；跨区段加总可能与先总计再舍入不同，必须标版本。 |
| D04 | 新购使用 PriceVersion 可选区间；既有客户钉选，迁移明示。 | 发布速度与历史准确性兼顾；需维护 assignment 及 rollout。 |
| D05 | 同 usage event 身分、不同 payload 报冲突；同 payload 重送回同一结果。 | 防止改 timestamp 再收一次；需要持久化原 fingerprint。 |
| D06 | 关帐后晚到用量归原服务期，但通过下一期更正差额；原 invoice 不改。 | 同时保留服务归属与历史帐单；需能以原 PriceVersion 重算累计差额。 |
| D07 | 新购自助付款确认才激活；续约逾期有 7 天 grace；Net30 合约依条款先激活。 | Entitlement 有独立政策，显示原因及截止时间。 |
| D08 | 期中自助升级付款确认才切有效方案。收款成功但开通延迟，针对未提供期间另发更正。 | UNKNOWN 时 Basic 可持续；需能处理先收后服务的价差。 |
| D09 | 正净额 proration 使用独立补差额 invoice，旧 invoice 不改；负行已在该文档抵正行，不另造可花 credit。 | 防止同一抵扣双重使用。 |
| D10 | 更正先减未付应收；只有已收且可发布的部分能成为有资金来源的 credit。Refund 保留额与 credit 使用共用预算。 | 有限应收纪录仍能防重退／重抵；未宣称完整总帐。 |
| D11 | 相同 provider operation 持续使用同 key 与 payload；逾时记 UNKNOWN 并查原操作。 | 避免远程已扣款却换 key 再扣；不能假设查无即失败。 |
| D12 | 合约生效／到期对齐帐期边界，缺到期后指派就停自动续价并产生可处理的 discrepancy。 | 避免过期后不知用哪个价；初期不支持合约期中任意价段。 |

### S07 的期中升级决策

Basic $20 已为 [09-01, 10-01) 收款。09-16 00:00Z 接受 Pro 五席的 Quote，剩余 15／30：未用 Basic −$10，Pro 五席 +$50，补差额 invoice $40。原 Basic invoice 不改，两条 signed lines 必须保留来源。支付 operation 对 $40 发出后如结果 UNKNOWN，有效方案与权益先维持 Basic，不能再用新 key 扣 $40。

若 09-18 00:00Z 才查实 provider 已扣 $40，此时才激活 Pro。Basic 实际服务 17 天，Pro 实际服务 13 天；实际补差额是 −$8.67＋$43.33＝$34.66。保持原补差额 invoice $40，另建 −$5.34 的更正，将多收部分转为有原付款来源的 customer credit，必要时再走 refund。其间不以 current price 重算原价。若确认 provider 根本没有扣款，则用更正取消补差额义务，Basic 继续服务；若一直无法取得真相，保持 UNKNOWN 并升级人工审查。

实务产品可能选择先开通 Pro、再承担坏帐风险；本 lab 选择上述政策，是为了能清楚推演 UNKNOWN、补偿及资金来源。选择的代价是可见的开通延迟。
