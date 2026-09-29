# F2：发票更正、已收款 credit 与退款保留

[English](phase-f2-corrections.md) | [繁體中文](phase-f2-corrections.zh-TW.md) | **简体中文**


日期：2026-09-26。这是 S11 的本机 Go＋SQLite 切片，使用独立 SQLite 文件仿真支付服务。金额单位皆为货币最小单位（USD cents）。它验证下列帐务不变量，不代表已具备正式会计或支付服务能力。

## 金额模型与事实来源

已定稿的 `invoices.total_minor` 不修改。每次减额更正写入不可变的 `corrections`，记录更正前后应收、原因、请求键与时间。已收付款保留在 `allocations`；若已收金额超过新应收，`allocation_releases` 逐笔指向原付款操作，并各自产生同额的 `credit_grants`。这条来源链让可用 credit 只来自实际已收的款项。

```text
应收 = 原发票金额 − 累计减额
净已付 = 原收款分配 − 已释放分配 + credit 应用
未付 = 应收 − 净已付

credit 可用 = grant 金额 − 已应用 − 已保留退款 − 已成功退款
```

`Balance` 与 `CreditBalance` 会计算这些数字并拒绝负值或超额状态。`Snapshot.AllocatedMinor` 仍是原始收款分配的**总额**；查看更正后的应收和净已付应使用 `Balance`。多次减额更正逐次保留各自的金额与来源。完全免除应收、增加发票金额、撤销更正与 chargeback 尚未实作。

## 操作与状态

1. `PostReduction` 用稳定请求键去重。若付款操作已送出或结果为 UNKNOWN，拒绝更正；未送出的操作会在同一个本地交易中取消，避免以旧金额扣款。更正后若初始发票已结清，订阅在同一交易中激活，权益投影可由 `RefreshEntitlements` 重建。
2. `CreatePayment` 可为剩余应收创建部分付款。它可替换结帐时创建、尚未送出的初始全额操作；若另一个由调用者创建的操作仍待送出，会拒绝新操作，保留原请求键与操作身分。
3. `ApplyCredit` 仅允许同客户、同货币、非来源发票的当期续约帐单，且不得超过 grant 可用额或该帐单未付额。它同样先处理尚未送出的旧金额付款操作；已送出或 UNKNOWN 时拒绝。成功后加入权益重建 outbox。
4. `ReserveRefund` 先在本地保留 grant 预算，再以稳定 provider key 向独立 fake provider 退款，且带上原始 capture key。失去回应时保持 UNKNOWN 与预算保留；查找或可信 webhook 确认成功后转为已退款，确定失败则释放保留。重送请求键只回传原操作，变更金额会冲突。

SQLite 约束与 trigger 防止减额超过原发票、分配或释放超额、没有来源的 grant、跨客户 credit 应用、超过目标发票未付额，以及 credit 应用与退款保留合计超过同一 grant。Fake provider 另限制同一 capture 的累计退款不超过实收金额。外部结果与本地帐务不共用交易；崩溃与 UNKNOWN 必须以原操作键查找和收敛。

## S11 金额 oracle

原发票 $100，更正后应收 $80：

| 已收 | 更正后未付 | 新产生的已收款 credit |
| ---: | ---: | ---: |
| $0 | $80 | $0 |
| $60 | $20 | $0 |
| $100 | $0 | $20 |

如果 $20 grant 已在下一期应用 $10，最多只能退款 $10。退款处于 UNKNOWN 时，这 $10 继续占用 grant；确定失败才可重新使用。另一个情况是已收 $60、发票减额至 $60：无 credit，应收结清，初始订阅和权益激活。

## 验证证据

- `go test -count=1 ./...`：涵盖上述三种付款位置、部分付款、退款成功／失败／UNKNOWN、请求键重播、跨客户拒绝、直接 SQL 超额插入拒绝、两个数据库连接同时保留的预算上限，以及 F1／E 回归。
- `go test -count=10 ./lab -run 'TestRefundFailureReleasesReservationAndConcurrentRequestsStayBounded|TestS11NewPaymentDoesNotSilentlyCancelCallerOperation'`：重复检查退款竞争与待付款操作身分。
- `go test -race -count=1 ./lab -run 'TestRefundFailureReleasesReservationAndConcurrentRequestsStayBounded|TestS11NewPaymentDoesNotSilentlyCancelCallerOperation'` 与 `go vet ./...`：检查并发测试及静态问题。
- `go run ./cmd/lab correction-demo`：Basic $20 收款，减额 $4；下一期应用 $2、退款 $2，最后 grant 可用 $0。输出的 JSON 同时列出两张发票的 balance、退款状态与 grant balance。

主要程序在 `lab/corrections.go`、`lab/money.go`、`lab/refunds.go`，数据库约束在 `lab/lab.go`，fake provider 在 `lab/provider.go`；回归测试在 `lab/corrections_test.go` 与 `lab/corrections_edges_test.go`。

## 刻意保留的边界

这个切片只处理已定稿固定价格发票的**减额**，没有 S07 计量计价、S09 价格版本切换、税、汇率、按比例计费、收入认列、总帐过帐、对帐批量或营运审批流程。信用额度只能用于本机模型中的后续续约；退款目的地与实际资金到帐仍由 fake provider 仿真。权益采可重建投影，需运行 `RefreshEntitlements`，目前没有背景调度保证其延迟。性能、可用性和跨部门流程仍未以真实工作负载验证。
