# E. 最小可运行内核：S01、S04、S05

[English](phase-e.md) | [繁體中文](phase-e.zh-TW.md) | **简体中文**


状态：E 切片已在本机运行 `go test ./...` 和三种 CLI demo；后续的续约能力另见 [F1](phase-f1-renewals.zh-CN.md)。下表记录 E 当时的实作边界，不是目前所有程序能力；这也不是生产环境验证。来源设计见 [设计索引](../design/README.zh-CN.md)。

## 实作边界

| 已实作 | 暂未实作 |
| --- | --- |
| Basic v1 $20 固定费、钉选 Quote、一次性接受与 HTTP 命令幂等身分 | Pro、席次、用量、比例计费、合约与价格迁移 |
| Invoice／单一行核定、不可变触发器、单一 PaymentOperation 与 outbox | 更正、credit、refund、完整应收与总帐 |
| 独立 SQLite fake provider；相同 provider key／payload 仅一笔 capture | 真实 PSP、webhook 签章、网络重试与 provider SLA |
| UNKNOWN／崩溃后查原 key、inbox 去重、成功不倒退、allocation 唯一 | 终局失败、grace、取消与多期续约 |
| 权益由成功 operation＋完整 allocation 重建 | 复杂 EntitlementPolicyVersion 和多服务权益 |

两个文件各自提交交易。Commerce 在本地交易中同时保存 Quote 接受结果、核定 invoice、固定金额／币别／key 的 PaymentOperation、outbox 和 audit。之后才调用 fake provider。provider 先提交 capture，再回复 Commerce；故障注入点在两者之间。`lost_response` 会将本地 operation 记成 UNKNOWN；`crash_after_provider` 留在 SUBMITTED。恢复时查原 provider key，确认成功才入一笔 allocation 并排入权益重建工作。查不到交易不会自动推断「确定没有扣款」。

## 验证 oracle

| 测试 | 主要断言 |
| --- | --- |
| `TestS01NewBasicPurchase` | Quote $20；付款前 pending、无 allocation；成功后单笔 $20 allocation；权益由 outbox 重建。 |
| `TestS04LostResponseKeepsOriginalOperation` | provider 已有一笔 capture，本地 UNKNOWN；查原 key 后成功；重查仍只有一笔 provider capture 和一笔 allocation。 |
| `TestS05CrashWebhookOrderAndProjectionRecovery` | provider 提交后重开程序；success webhook 重送和旧 pending 晚到不倒退；再次重开后重建权益且无第二次财务效果。 |
| `TestQuoteExpiryIdentityAndPriceImmutability` | fingerprint／idempotency 冲突被拒，同 Quote 换 key 不能再收一次，已发布价格与付款 payload 不可改。 |
| `TestQuoteExpiresAndAbsentProviderResultIsNotFailure` | 过期 Quote 被拒；provider 查无原操作时保持未决，不凭空分配或开通。 |

CLI 的三条路径分别得到：正常 `pending → active → entitlement active`；lost response `unknown → provider lookup → succeeded`；崩溃 `submitted → provider lookup → succeeded`。三者最终都是一笔 $20 allocation。

## 后续进入 F 阶段前的缺口

这个切片的 PriceVersion 只有固定费字段，没有把 A 阶段的 charge definitions、rounding、proration、可购买生效区间完整落地；CatalogSelection 只提供 Basic default。没有 HTTP 认证、授权或 webhook 签章；测试使用可信的 in-process fake provider。SQLite trigger 锁住此切片的主要财务 payload，但尚无一般化 billing close、跨期差额、退款预留或 reconciliation engine。下一步应从 S02／S03 续约与欠款政策开始，沿用同一来源身分与 outbox 模式，之后才加入 S07–S11 的金额模型。
