# P03：Account／Commerce 边界与灰度迁移

[English](phase-p3-account-cutover.md) | [繁體中文](phase-p3-account-cutover.zh-TW.md) | **简体中文**


## 本机迁移模型

`account_links` 保存旧帐户 ID、Commerce customer ID、beneficiary、cohort、历史数据标记、读取来源与唯一命令写入者。既有 Commerce 发票的帐户必须标示历史数据并回填来源。迁移初始读取及写入来源为 `legacy`；新购、合约接受、订阅变更与新用量事件会在 Commerce 交易中检查写入者，避免两边同时创建新商务事实。同一请求键的已提交操作仍可重播。

`ShadowQuote` 依目前已发布价格与帐户 cohort 做唯读报价，保存旧系统提供的金额／币别、Commerce 结果、是否相符及本机计算延迟。`ShadowEntitlement` 保存旧权益状态与 Commerce 订阅投影结果。Shadow 只记录比较证据，不创建应收或调用 provider。

`BackfillLegacyProvenance` 将旧订阅／发票 ID 连到 Commerce 订阅、发票与 PriceVersion。只有客户、发票、帐期内有效的 assignment 及不可变 invoice line 相互吻合才标 `complete`；其余标 `manual_review`。审查者可用 `ResolveLegacyProvenance` 指定已核实的正确映射并留下决议，原发票与 assignment 不改写。同一 Commerce 发票不可被两个旧发票 ID 重复认领。有历史数据的帐户必须完成其全部现有发票映射。

`MigrationReadiness` 要求最新 shadow quote 和 entitlement 相符、回填完整、最后一次对帐不早于 shadow、付款没有 `created/submitted/unknown` 未决操作、帐户相关对帐差异在设置门槛内，且 quote p95 不超过依基线设置的上限。未决付款固定要求为零。先切换读取来源，再以同一数据库交易核对门槛与切换唯一写入者。`StopAccountMigration` 记录停止原因并阻止新的 Commerce 命令；已接受的义务仍保留原写入者和读取来源，供对帐、查证与更正。

CLI「帐户边界与灰度迁移」可操作映射、shadow、回填、人工修正、准备度、读取切换、写入切换、停止及 adapter 权益查找。「状态查找」列出每个帐户的拥有者与准备度。受内部凭证保护的 `GET /v1/internal/accounts/{account}/entitlements/{subscription}` 会按目前读取来源返回权益。

## 验收与范围

`go test ./lab -run TestP03 -count=1` 验证 shadow mismatch 停止切换、legacy writer 拒绝新购、读取先于写入切换、切换后原请求可重播、停止后拒绝新命令、UNKNOWN 付款及缺来源回填阻止切换、人工核实后恢复准备度。API 契约测试验证 adapter 读取切换及内部凭证。

这是单一 SQLite 程序中的迁移演练，旧系统观测值由操作者提供。它没有连接真实单体或测量生产流量。Shadow 准备度目前采每个帐户、每种类型的最新比较；实际 rollout 需依 tenant、plan、合约和旧价格 cohort 取得具代表性的样本，设置真实延迟与差异率基线，并配置正式身分验证、权限及审计保存政策。
