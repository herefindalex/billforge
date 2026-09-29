# A01 领域回归与管理入口比对

[English](a01-domain-baseline.md) | [繁體中文](a01-domain-baseline.zh-TW.md) | **简体中文**


状态：部分完成。2026-09-27 以隔离的 commerce／provider SQLite 运行 `go test ./... -count=1`，950／950 通过；`go vet ./...` 通过。通过代表下列既有路径在本机 fixture 中保持原行为；只有「直接比对」栏列出的案例，才证明管理命令与直接领域入口产生相同的金融事实。

| 情境 | 领域或 API 基线 | 关联管理操作 | 直接比对 |
| --- | --- | --- | --- |
| S01 新购买 | `lab/lab_test.go` 的 `TestS01NewBasicPurchase` | C01／C02／C09／C45 | C01／C02 Pro 五席购买：`lab/admin_financial_parity_test.go` |
| S02 月底续约 | `lab/billing_test.go` 的 `TestS02MonthEndAnchorAndIdempotentRenewal` | C44／C09 | C44 月底帐期、帐单、付款义务及实收：同上 |
| S03 续约失败与宽限 | `lab/billing_test.go` 的 `TestS03FailedRenewalGraceSuspensionAndRecovery` 与 `TestUnknownRenewalRetainsVerificationGrace` | C44／C09／C08／C10／C45 | 确定失败、宽限／暂停、C08 补款：`lab/admin_failed_renewal_parity_test.go`；未知结果超过宽限期限仍保留查证宽限、C10 查证后恢复：`lab/admin_unknown_renewal_parity_test.go` |
| S04 收款回应遗失 | `lab/lab_test.go` 的 `TestS04LostResponseKeepsOriginalOperation` | C09／C10／C49 | C49 注入回应遗失后，C09 保留原操作待查证，C10 查证并完成原命令：`lab/admin_lost_response_parity_test.go` |
| S05 中断、Webhook 次序与投影恢复 | `lab/lab_test.go` 的 `TestS05CrashWebhookOrderAndProjectionRecovery` | C09／C45／C49 | C49 注入 provider 提交后中断，C09 复原原命令，重启后重复及过期 Webhook 与 C45 投影重建：`lab/admin_crash_webhook_parity_test.go` |
| S06 下期变更与取消 | `lab/subscription_changes_test.go` 的两个 `TestS06*` | C03／C05／C06／C44／C45 | C01 变更报价＋C03 调度＋C44 续约比对：`lab/admin_financial_parity_test.go`；C05／C06 取消、恢复、再次取消与 C44／C45 边界到期比对：`lab/admin_cancel_resume_parity_test.go` |
| S07 期中升级与待查证 | `lab/immediate_test.go` 的两个 `TestS07*` | C04／C09／C10／C13／C14／C44／C49 | C04 正常付款的期中金额与权益：`lab/admin_financial_parity_test.go`；C49／C09 回应遗失、C10 沿原付款查证、C13 延迟更正，以及跨帐期 C44 hold、C14 未履行决议后 Basic 续约：`lab/admin_s07_parity_test.go`。两个隔离 SQLite／假服务商案例逐阶段比对升级状态、帐单义务／实收／发布／未清、funded grant、收据、provider capture 及续约帐单。 |
| S08 价格版本与迁移 | `lab/catalog_admin_test.go`、`lab/price_migration_test.go` 的 `TestS08*` | C18／C21–C25／C44 | C18 发布、C21 选价、C22 计划、C23 暂停、C25 恢复与 C44 边界套用及回迁；另验 revision 冲突令批量暂停、C24 明确跳过后 C44 以旧价续约：`lab/admin_price_migration_parity_test.go` |
| S09 用量关帐与晚到事件 | `lab/usage_test.go` 的 `TestS09UsageCloseLateEventAndOriginalPeriodDebit` | C26–C30／C44 | C26 用量、C28 关帐、C29 重算、C27 调整、C30 credit note 及 C44 续约／暂停：`lab/admin_usage_parity_test.go` |
| S10 企业合约与 Net30 | `lab/contracts_test.go` 的两个 `TestS10*` | C01／C02／C31／C32／C44 | C31／C01／C02 创建、C44 缺后续价格时暂停续约、C32 到期收款：`lab/admin_contract_parity_test.go`；指定后续价格、两期合约收款与转为自助价格：`lab/admin_contract_transition_parity_test.go` |
| S11 减额、抵扣与退款额度 | `lab/corrections_test.go`、`lab/corrections_edges_test.go` 的 `TestS11*` | C11／C12／C15–C17 | C11 部分付款及 C11／C12 funded credit：`lab/admin_financial_parity_test.go`；C11／C12 与 C15／C16／C17 共用 funded credit 预算、未知退款保留预留额及原 capture 来源：`lab/admin_refund_budget_parity_test.go`；未知收款时封锁 C11、C10 查证原 C09 后才可减额：`lab/admin_uncertain_reduction_parity_test.go`；部分收款加减额结清并激活权益：`lab/admin_reduction_activation_parity_test.go`；C12 拒绝跨客户使用 credit：`lab/admin_foreign_credit_parity_test.go`；未付款先减额、C07／C09 收取剩余金额、C45 权益投影及四项命令使用原请求键重放：`lab/admin_unpaid_reduction_parity_test.go`；其余边界情境仍待比对 |
| S12 对帐与修复 | `lab/reconciliation_test.go` 的 `TestS12*` | C08–C10／C33–C35 | C33 产生分类、C34 安全投影修复／原收款重试／未知付款查证／来源 revision 改变时封锁、C35 人工决策：`lab/admin_reconciliation_parity_test.go`；C08–C10 的付款入口另见 S03／S04 |
| P01 AI token SKU | `lab/meter_catalog_test.go` 的 `TestP01AITokensSKUUsesGenericMeteredPath` | C19／C20／C21／C01／C02／C26／C28／C44 | C19 注册 meter、C20 发布、C21 选价、C01／C02 购买、C26 记录、C28 关帐与 C44 续约的同一路径金额比对：`lab/admin_ai_meter_parity_test.go`；来源／meter 拒绝、重算与 credit note 边界仍待直接比对 |
| P02 旧 client 的费用可见性 | `api/v1_test.go` 的 `TestV1LegacyClientRejectsUnshownAITokenCharge` | C19／C20／C21 发布后的 `/v1` 消费者 | `TestV1LegacyClientRejectsAdminPublishedUnshownCharge` 直接验管理发布、旧 client 拒绝及新 client 接受 |
| P03 帐户边界迁移 | `lab/account_migration_test.go` 的两个 `TestP03*` | C36–C43 | C36 链接、C37／C38 shadow、C41／C42 先读后写切换、C43 stop；另验 C39 错误历史凭证须人工审查、C40 更正，以及未知付款由 C10 查证后才可切换：`lab/admin_account_cutover_parity_test.go` |

## 已直接比对的金融不变量

金额以 USD 最小单位的整数保存。上述直接比对使用两个独立 SQLite 实例，以相同业务时间及相同输入分别走领域与管理路径；随机产生的 ID 不参与数值比较。

| 不变量 | 本机证据 | 结论范围 |
| --- | --- | --- |
| 接受报价后帐单、付款与权益归属一致 | C01／C02 Pro 五席比较帐单明细、帐期、价格指派、付款及权益 | 只涵盖该购买 fixture |
| 月底续约不改变帐期界线或金额 | C44 比较 1 月 31 日起的下一帐期、到期日、帐单明细、付款操作、outbox 与 capture 后实收 2000 | 只涵盖单户正常续约 |
| 续约确定失败后的宽限与补款 | C44／C09／C45／C08 与直接领域路径比对 2000 帐单、原付款义务确定失败、宽限到期暂停、同键重播只创建一笔新付款义务、补款后权益恢复与实收 2000 | 涵盖确定失败的单户分支 |
| 续约结果未知时持续查证 | C49 注入回应遗失；C44／C09／C45 与直接领域路径均在第八天保持 `grace` 且未入帐，C10 查证原操作后各只入帐一次，权益恢复 | 涵盖单户未知结果分支与指定时间点 |
| 收款回应遗失不重复请款 | C49 注入一次回应遗失；直接领域路径与 C09／C10 均保持原付款操作 `unknown` 且零入帐，查证后各只形成一笔 provider capture 与一笔 2000 allocation；原管理派送命令及查证命令各一笔收据 | 涵盖单一付款的回应遗失与重播；其他故障次序另待比对 |
| provider 提交后中断可由来源事实恢复 | 直接领域路径与 C09／C49 均在本地 allocation 前中断；两次重启后，重复成功及过期 pending Webhook 不覆写已成功付款；C45 重建权益，原 C09 命令完成且仅有一笔收据与 provider capture | 涵盖单户单付款的指定中断点与 Webhook 次序 |
| 取消与恢复不得在边界开出新帐单 | C05 安排取消、C06 恢复、再次 C05 取消后，C44 在 10 月 1 日不添加帐期或帐单；C45 令权益到期，状态与直接领域路径一致，再次 C44 仍不续约 | 涵盖单户调度取消／恢复顺序；边界后人工恢复另待比对 |
| 价格迁移只在帐期边界生效 | C18／C21／C22／C44 与直接领域路径比较两户 10 月 Pro v2 帐单各 11000、付款及实收；11 月只让指定一户回迁 v1，两户帐单分别为 10000／11000，价格指派来源与笔数一致 | 涵盖正常迁移与回迁 |
| 迁移冲突不得替另一户套新价 | 两户迁移中一户 revision 改变；C44 与直接领域路径只让未冲突者以 11000 续约，批量暂停且冲突者尚无新帐期；C24 明确跳过后第二次 C44 才以原价 10000 续约，最终批量完成 | 涵盖 revision 冲突与人工跳过；其他冲突理由仍待比对 |
| 人工暂停与恢复保留迁移目标 | C23 暂停、C25 恢复与直接领域路径维持同一迁移项；恢复后 C44 才在下期产生 11000 Pro v2 帐单且批量完成 | 涵盖一户未冲突的暂停／恢复 |
| 晚到用量归原帐期并受 credit note 守卫 | C26–C30／C44 与直接领域路径比对 20003→20010→20003 的 rating revision、原帐期晚到事件的次期 1 个最小货币单位 debit、反向调整后暂停续约、credit note 资金来源 1 及恢复后 10000 帐单 | 涵盖 tasks 计量单户与指定事件次序；其他 meter 与部分关帐仍待比对 |
| 对帐修复不得创造第二笔收款 | C33／C34 与直接领域路径比对待派送 outbox 与 provider 已成功但本地未知两种分类；修复及重播后均只保留原付款操作、单笔 provider capture 与 2000 入帐 | 涵盖单户两种付款修复 fixture |
| 修复必须服从来源版本与人工审查 | 权益投影缺失由 C34 安全重建且同命令重播仅一笔修复及收据；来源 revision 改变后 C34 与直接领域路径均封锁；provider 金额不符维持 `MANUAL_REVIEW`，C34 不改付款与帐单，C35 留下人工决策 | 涵盖投影、revision 变动与金额不符 fixture |
| 新计量 SKU 沿通用帐单与用量路径 | C19／C20／C21 发布 AI token 费率后，C01／C02 以 3000 创建与结清首期；C26 记录 105 tokens，C28 扣除含量 100 后计价 1，C44 次期帐单 3001 并标记原帐期用量来源；与直接领域路径逐项一致 | 涵盖单一 AI token SKU；来源验证与晚到调整仍待比对 |
| 帐户切换维持唯一付款 writer | C36／C37／C38／C41／C42／C43 与直接领域路径比对 legacy writer 时零新订阅、shadow 完成后先切读再切写、切换后首笔 2000 帐单及唯一 capture、stop 后新收费被拒而原接受命令可重播 | 涵盖单户无历史数据的切换顺序 |
| 历史凭证与未知付款阻挡切换 | 有历史帐单时，未知付款与错误价格凭证均阻挡 readiness；C39 错误 backfill 留在人工审查，C10 查证原收款后仍不能切换，C40 以原帐单 basic-v1 更正后才 ready；两路均保留原 2000 入帐与单笔 capture | 涵盖单一历史帐单与指定审查决策 |
| funded credit 的套用与退款共用预算 | C11 创建 1000 funded grant，C12 套用 500 后 C15 只保留其余 500；C16 回应遗失期间仍保留预留额且拒绝额外退款，C17 查证后转为已退 500，provider 仅一笔退款并指向原帐单 capture；两路金融事实一致 | 涵盖单一 grant、单次用款与未知退款分支 |
| 下期方案变更不得提前改变已生效价格 | C01／C03 确认前后原 Basic 价格仍生效且 revision 为 2；C44 到期后两路均用 Pro 五席产生 10000 的帐单与同样的付款义务 | 尚未比对取消、恢复或并行调度 |
| 部分付款减额只降低应收，不凭空创建 funded credit | C11 比较原额 10000、已收 6000、减额 2000、未收 2000，确认旧全额付款操作取消；补收后两路各只有两笔 capture | 只涵盖此付款次序与金额 |
| 未知收款不得先减额或创建 credit | 直接领域入口与 C11 预览均拒绝回应遗失期间的 2000 减额；SQLite 无更正、grant 或 C11 命令。C10 查证并完成原 C09 后，两路帐单义务 8000、实收 10000、funded grant 2000，provider 各仅一笔 capture；浏览器验 HTTP 409、画面错误及无 C11 写入 | 涵盖单户全额收款回应遗失与一次减额；不推论其他中断点或并行命令 |
| 减额结清首期应收时激活权益 | 两路先收 6000 仍保持 pending，再减额 4000 后帐单义务与实收均为 6000、未清为零，订阅与权益均 active；没有 funded grant，provider 各只有一笔 capture | 涵盖单户首期部分付款后一次减额，不推论续约或并行减额 |
| funded credit 不得跨客户挪用 | 两路从已付款帐单创建 1000 grant；直接套用及 C12 预览对外客户帐单的 500 抵扣均拒绝。grant 可用额仍为 1000、目标帐单未清 10000，没有 credit application 或 C12 命令 | 涵盖单一外客户帐单与单次套用，未验跨客户退款或数据库直写 |
| 未付款减额须取消旧收款，只收剩余应收 | 两路先将 10000 帐单减额 2000，原付款操作与 outbox 均取消、provider 零 capture、无 funded grant；直接付款与 C07／C09 各创建新 8000 操作并只 capture 一次。最终义务与实收均 8000、未清为零，刷新后权益 active | 涵盖单户未付款减额与一次补收；重播键冲突与跨进程并行仍待比对 |
| 已付款减额的发布与跨帐单抵扣同源且不超支 | C11／C12 比较来源帐单发布 1000、grant 1000、套用 500、剩余 500，以及目标帐单未收 1500 | 单次套用与后续退款已直接比对；并行争用的领域及管理测试另列 A16 |
| Net30 在到期前无 capture 义务，到期后只收一次 | C31／C01／C02 比较合约来源、7500 帐单与 2026-10-01 到期日；到期前 outbox 为零，C32 与直接收款各创建一笔 outbox，capture 后各实收 7500 且未收为零 | 只涵盖单一合约帐单 |
| 缺后续价格时不得凭空续约 | 到期时直接 `RunRenewals` 与 C44 均只留下 `contract_next_price_missing`，不创建下一帐期，权益暂停；既有 Net30 应收仍可由 C32 收回 | 涵盖该合约 fixture 的缺价分支 |
| 明确后续价格只切换一次 | C31 设置后续价格；C44／C32 与直接领域路径逐期比对合约 7500、两张 Net30 帐单到期收款、转价后 10000 帐单、付款义务与实收；11 月 1 日指派添加一次，12 月 1 日仍只有两笔价格指派与一笔合约转换 | 涵盖该合约 fixture 的指定后续价格分支 |
| 新费率不得让未展示费用的旧 client 创建义务 | C19／C20／C21 通过管理命令发布 AI token SKU；`/v1` 报价揭露三个组件与当下应付 3000；无 `meter:ai_tokens_admin` 能力声明回 409 且零订阅，有声明才接受 | 涵盖本机该 SKU 与旧 API client；未验所有 consumer 版本 |

这份对照不把原有领域测试通过推论成所有管理入口等价。未标示直接比对的情境仍须核对其管理命令是否沿用相同领域交易、来源守卫与恢复规则；A01 因此保持部分完成。
