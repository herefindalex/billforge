# C. 商务情境、故障点与对帐修复

[English](03-scenarios-and-reconciliation.md) | [繁體中文](03-scenarios-and-reconciliation.zh-TW.md) | **简体中文**


状态：纸上 oracle，尚未运行测试。政策依 [A](01-pricing-and-policies.zh-CN.md)，保护点依 [B](02-domain-and-invariants.zh-CN.md)。除特别注明，时间为 UTC、币别为 USD；月份使用 `[start,end)`。每次 trace 都要能由持久事实重播，不靠程序内存中的「成功」旗标。

## S01–S12 trace

| 情境 | 事实顺序、金额及失败时答案 |
| --- | --- |
| **S01 新 Basic** | 2026-09-01 Quote 引用 Basic v1、固定 $20、有效期限及 fingerprint；接受后创建 $20 invoice 与固定 key 的 capture operation。发送前崩溃由 outbox 重送同一 operation；capture `succeeded` 且 allocation 入帐后 Subscription 才 `active`，权益投影依来源打开。Quote 本身不是应收或付款证据。 |
| **S02 正常续约** | 帐期由原月日锚点算 `[09-30,10-30)`、`[10-30,11-30)` 等；31 日锚点于 2 月截月底，3 月回 31 日。`subscription+period+charge_group+revision` 唯一；固定预付 $20 在新期核定一次，capture operation 也只对应一次义务。重跑关帐／worker 回原结果。 |
| **S03 续约失败与 grace** | 10-01 Basic $20 invoice 到期，确定失败仍保留 open 应收；依 due_at 起 7 天 grace，权益先 `grace`，到 deadline 才 `suspended`。补款 $20 确认后分配到原 invoice，权益回 `active`，保留中断历史。是否在 suspended 时继续新期计费由明示服务政策决定；本 lab 暂停添加预付服务期，未付历史不抹除。 |
| **S04 provider 已成功，本地回应遗失** | $20 capture 使用既存 provider key 发出，timeout 后 operation 为 `unknown`，本地不可声称失败或创建另一 key。查原 key：若确证成功，入一笔观察／allocation／activation；若查找仍无终局，维持 unknown 并告警；仅 provider 的终局无扣款证据能释放保留。 |
| **S05 webhook 重复、乱序及崩溃** | 同 event ID 的 success 只消费一次；较晚收到的 pending 不让 success 倒退。若成功观察与 allocation 已提交而权益 worker 崩溃，重建权益投影；若崩在提交前，重新消费 inbox／查 provider。同一 capture 对同一义务仍只分配一次。 |
| **S06 下期变更、取消／恢复** | Basic → Pro 五席排到 10-01，当期仍 Basic；同一边界只允一项调度，由 `subscription revision` CAS 拒绝竞争写入。`cancel_at=10-01` 与下期升级冲突时本 lab 不猜优先序，要求先撤销取消再接受升级；取消前 resume 清除调度且保留 audit。10-01 已结束后不叫 resume，重新购买。 |
| **S07 期中 Pro 升级** | 9 月 `[09-01,10-01)`，Basic 已预付 $20，Pro 五席为 `$50+$10×5=$100`。09-16 立即切换的剩余 15/30 天：Basic 未提供段退款行 `−$20×15/30=−$10`，Pro 新段 `$100×15/30=$50`，补差额 invoice **$40**；负行已抵正行，不另生可花 credit。若 $40 capture 为 UNKNOWN，Basic 持续、Pro 不先开。09-18 才证实已收并激活，剩 13/30：`−$20×13/30=−$8.67`，`$100×13/30=$43.33`，实际补差额 **$34.66**。保留原 $40 invoice，另作 `+$1.33` Basic 回收修正和 `−$6.67` Pro 服务修正，净更正 `−$5.34`；已收 $40 中发布的 **$5.34** 才成为 funded credit，可抵下期或申请退款，两者共用额度。 |
| **S08 新价与 cohort** | 发布 Pro v2 并设置 cohort A 的 CatalogSelection；原订阅仍指 Pro v1。迁移先预览每户下一期金额、权益和差额，再以 `migration_id+subscription_id+target_version` 做唯一命令；在期界 CAS 关闭 v1 assignment、开 v2 assignment。部分完成时停止新批量，已成功者保留明确历史；回退用反向新 assignment，不改旧帐或已发布价格。 |
| **S09 用量与晚到** | Pro 当期包含 20,000 tasks，超额 $0.001/task。关帐 cutoff 前共 20,003，精确超额 $0.003，按当期同组件段 half-even 为 **$0.00**；invoice line 仍保存原量及未舍入值。晚到同原期 7 tasks 后累计 20,010，精确 $0.010、累计应收 $0.01，减已入帐 $0.00，下一张票的原期 debit **$0.01**。相同 event 重送 delta $0；同 event ID 内容变动回 conflict。负差额走 CreditNote，不能改原 invoice。 |
| **S10 Acme 合约** | Acme 的 ContractVersion 覆写白名单固定费 `$40`、席次 `$7×5`，当期总 **$75**、Net30。PriceVersion 提供组件与舍入等基底，合约引用价源并保存覆写；条款允许核定后先开服务，非「已付款」。合约到期及后续指派在期界；若缺明确后续价，不暗用 catalog current price，停止自动续价、报 discrepancy 与人工处理。 |
| **S11 更正与退款竞争** | 原 invoice $100 更正净义务为 $80。未付时只将 open 应收降至 $80，credit=$0；已收 $60 时剩余应收 $20，credit=$0；已收 $100 时发布 capture allocation $20 成 funded credit，应收 $0。若同时申请两笔各 $15 refund，第一笔预留 $15 后第二笔因可用只剩 $5 而拒绝或缩额；若先抵未来帐 $20，退款可用为 $0。退款 UNKNOWN 时保留额仍占用。 |
| **S12 对帐缺口** | 对帐比较已核定 invoice／应收、allocation、provider observations、subscription assignment 和权益来源 revision。单纯缺投影可重建；未送 outbox 重跑原工作；provider timeout 查原 key；未知 provider transaction 或金额／币别矛盾先封存证据并人工审阅。RepairOperation 记 `expected/actual`、前置 revision、稳定 key、运行与再核对；重跑不创第二效果。 |

S07 的 09-18 日期以实际开通时刻为服务边界；如果 provider 事后证实成功时间早于 09-18，仍按实际提供 Pro 权益的时刻核算，不用 PSP 时间冒充服务时间。这是此 lab 的客户服务政策，后续需让产品／财务审阅。

## 故障点到恢复路径

| 故障点 | 持久事实 | 恢复及禁止事项 |
| --- | --- | --- |
| Quote 接受交易前崩溃 | 无新 intent | 客户可用同 idempotency key 重试；重新验证 quote 的到期与 revision。 |
| 接受交易后、provider 发送前 | intent、operation、outbox 已提交 | 送同 key／同 payload；不重算 current price。 |
| provider 收到后 timeout | operation unknown、原 key 在案 | 查原 key，未查明不换 key；保留应收／退款预留。 |
| success webhook 后、allocation 提交前 | inbox 未完成或 pending | 重播观察并验证义务；交易唯一键保证一笔 allocation。 |
| allocation 后、权益投影前 | 成功收款及 allocation 已在案 | 依政策重建权益，不再 capture。 |
| invoice 核定后、通知前 | 完整 invoice、audit、outbox | 重送通知；不重产 invoice。 |
| refund 发送后 timeout | refund operation unknown，来源预留仍在 | 查原 refund key；不释放也不另退。 |
| migration 批量中断 | 各户 assignment revision 和 migration result | 从未完成户继续；已完成户不以旧指派再套一次。 |

## 对帐分类与修复矩阵

| 分类 | 典型证据 | 允许的动作 | 修复后验证 |
| --- | --- | --- | --- |
| SAFE_AUTO_REPAIR | 成功 allocation 和有效 assignment 存在，仅 entitlement projection 缺失 | 按钉选 policy 重建投影；不改财务事实 | 权益 reason／source revision 与期望一致 |
| RETRY_REQUIRED | 已提交 outbox 未送达 | 原 job／operation key 重跑 | provider reference 或成功送达纪录唯一 |
| EXTERNAL_LOOKUP_REQUIRED | provider timeout／互斥的非终局回报 | 查原 operation 和远程状态，等待可信终局 | 本地 observation 能指向外部原操作 |
| MANUAL_REVIEW | 远程金额／币别不符、未知外部交易、合约到期后无指派 | 暂停相关自动金流或续价；收集 provider、quote、invoice 证据 | 人工决议与核准者、后续命令有引用 |
| UNSAFE_TO_REPAIR | 缺历史 capture 或互相矛盾的票据来源 | 不覆写原数据；调查后另开可稽核更正 | 新更正完整指回原对象与决议 |

每次 ReconciliationRun 保存 cutoff、检查集合、`expected`、`actual`、证据来源及观察时间。Discrepancy 有稳定身分及状态 `open/investigating/resolved`；「没有查到 provider 交易」只代表当次查找结果，不足以单独声明扣款未发生。修复的安全前提是来源事实完整；证据不足时分类升级，不自动凑平。
