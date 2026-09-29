# D. API 契约、兼容性与平台演进

[English](04-platform-apis-and-migration.md) | [繁體中文](04-platform-apis-and-migration.zh-TW.md) | **简体中文**


状态：设计契约；[本机 v1 API](../implementation/phase-p2-v1-api.zh-CN.md)与[帐户迁移演练](../implementation/phase-p3-account-cutover.zh-CN.md)已对应其中的 MVP 切片，两份纪录说明已实作行为与限制。依 [A 的价格解析](01-pricing-and-policies.zh-CN.md)、[B 的事实所有权](02-domain-and-invariants.zh-CN.md)、[C 的故障 trace](03-scenarios-and-reconciliation.zh-CN.md)。API 的目标是让产品团队按公开定价组件接入新 SKU，同时让 Commerce 对应收、付款与权益保持单一写入责任。

## 两种 consumer 与命令／查找

Consumer 1 是自助购买／结帐 UI，需看「现在确定要收多少、日后哪些费用依用量变动、何时会开通」。Consumer 2 是内部 Sales／Support／财务工具，需看合约来源、Net30、变更原因、修复权限与证据。两者共用价格与订阅契约，内部端通过授权的合约／修复命令取得额外字段，不绕过领域服务改 DB。

| 契约 | 必要输入或输出 | 边界与错误 |
| --- | --- | --- |
| `POST /v1/quotes` | customer、billing account、beneficiary、plan ID、seat quantity、期望 effective_at；回 `quote_id`、expiry、context fingerprint、PriceVersion ID／checksum、ContractVersion、逐组件现在／未来费用、用量费率、tax 尚不支持标示 | 不接受由 client 传单价；无唯一可选版本、缺 meter、合约不适用时明确 `PRICE_UNAVAILABLE`／`CONTRACT_CONFLICT`。 |
| `POST /v1/subscriptions` | quote ID、fingerprint、`Idempotency-Key`；回 subscription、invoice、payment operation 与 `pending/active` | 检查 quote expiry、customer、revision 与钉选版本仍可售；选价切换本身不替换有效 quote 的价格。同 key／同 payload 回原资源，不同 payload `409 IDEMPOTENCY_CONFLICT`。 |
| `POST /v1/subscriptions/{id}/changes` | quote ID、expected subscription revision、change mode `next_period/immediate`、`Idempotency-Key`；回 change intent、待支付额和 `requested/effective` 状态 | `409 REVISION_CONFLICT` 要重报价；付款 UNKNOWN 回 `PAYMENT_PENDING_VERIFICATION`，不可默认切换。 |
| `GET /v1/subscriptions/{id}` | effective plan／pricing assignment、scheduled change／cancel_at、invoice／payment 状态、权益 reason 与 grace deadline、revision | 读模型可延迟，但须标 source revision／`as_of`；不能把 `requested_plan` 当 `effective_plan`。 |
| `POST /v1/usage-events` | tenant、source、event ID、meter、quantity、event_at，批量每项独立结果 | 相同 event ID＋payload 回原结果；不同 payload `409 EVENT_CONFLICT`；不默默丢弃。 |
| `GET /v1/entitlements/{beneficiary}` | feature、`active/grace/suspended/expired`、effective interval、reason、来源 assignment／invoice／policy revision | 消费者依权益 API 决定服务；读取延迟显示 `as_of`，不从付款布尔值推断。 |
| `GET /v1/invoices/{id}` | final line 的 price／contract／period／quantity／rate／amount 与更正引用 | 核定内容不变；更正是另列资源。 |
| 内部 `POST /v1/contracts`、`/v1/migrations`、`/v1/repairs` | 版本化合约、preview＋cohort＋target PriceVersion、discrepancy＋前置 revision | 授权／audit；禁止直接改已核定发票、已发布价格或手填 credit balance。 |

Quote 回应需把 `due_now`、`recurring_committed`、`metered_estimate_or_rate` 分开；未来用量未知就只给费率与 allowance，不把预估值宣称为已承诺总额。Server 持有计价和舍入政策；client 只呈现 line items。`Idempotency-Key` 范围是 tenant＋endpoint／命令类型＋key，保留 payload hash 与原回应。外部 PaymentOperation 使用另一个稳定 provider key，不能直接用 HTTP attempt ID。

## 兼容性规则（P02）

1. v1 response 添加可选字段可向前扩充；既有必填字段的语义、整数 cents 单位、状态含义不能静默改。新的 charge kind 先用 `components[]` 的已知 `kind`／`display_label` 表示；旧 client 若无法理解新计价种类，quote 标 `requires_client_capability`，接受时检查声明能力并拒绝，不让它盲签。不要把新 kind 假装成 FixedCharge。
2. 未知 enum 值在只读画面可显示通用状态与 server 提供的说明；对创建订阅或金额确认的命令，未知值需 fail closed。旧 client 不能通过自己计算价格或忽略新 line 来绕过 server quote。
3. `PriceVersion`、`EntitlementPolicyVersion`、ContractVersion、meter schema 分别版本化。Quote 暴露解析结果与来源引用；任何不可兼容变更用 `/v2` 或新能力协商。旧 API client 的契约测试至少涵盖 Basic／Pro、UNKNOWN、grace、合约 quote、新组件拒绝与晚到更正的读取。
4. 延迟读模型不得对客户宣称「已开通」而来源尚未提交；回 `pending` 和可轮询操作 ID。Support 可查看来源 revision 与 provider observation，避免把查找缓存当最终金流真相。

## P01：三天推出 AI tokens SKU 的设计推演

若 AI tokens 同样是「每期包含 N、超额每 token 固定精确单价」，先注册 `meter_id=ai_tokens`、数据源与去重规则，再创建新 Plan／PriceVersion 的 `IncludedQuantity`＋`UsageOverage` 组件、权益政策、文案和测试 fixture；Pricing／Billing／Payments 内核无需为 SKU 分支。发布前验证组件完整性、quote 和 rating 金额 oracle、老 client 能力协商、shadow 数据差异与 Finance 签核。三天是目标情境，未有实测推出时间。若要求阶梯价、阶段性促销、税务或即时 hard quota，本 lab 组件不足，必须另做设计与程序变更，不能宣称配置即可解决。

## P03：成熟单体拆界与价格灰度 ADR

**决定**：Account 保有认证、客户与组织身分；Commerce 拥有 price selection、assignment、rating、invoice、payment obligation 和权益决策。初始以既有单体中的 adapter 调用 Commerce 契约；迁移期间每个事实只有一个 writer，不能两套系统同时收款或核定发票。

**顺序**：先创建来源 ID 映射与唯读 shadow quote／entitlement 比较，按 tenant、plan、合约／无合约及旧版本 cohort 分桶量差；不以 shadow 结果对客户收款。回填历史 assignment／invoice provenance，将缺数据列 discrepancy 人工处理。再以 feature flag 小 cohort 激活新读路径，确认延迟与差异；最后逐项把 command writer 交给 Commerce，交接点有唯一 owner 记录和可回放 outbox。每一步记录 p95 quote latency、差异率、UNKNOWN 停滞时间、对帐缺口及客服事件，设停止门槛，但数值门槛需取得基线后设置。

**回复**：配置／selection 出错时停止新 cohort 选用，未接受 quote 可重新报价；已接受 quote、已送 provider operation、已核定 invoice 不因回退而重算。writer 切换出错时先停新命令并对帐，确认未决 operation 后才切回；不能在同一义务上让旧 writer 再建一笔 capture。已迁移订阅若需回 v1，创建新的未来 assignment 和审计决策，不更改 v2 历史。

**代价**：shadow 比较、来源映射、双读与单 writer 协调增加短期工作量；换得能在历史正确性可查的前提下逐步拆开帐户与 Commerce。文档、能力协商及可观测性是产品团队接入 SKU 的必要交付，不是之后补的说明。
