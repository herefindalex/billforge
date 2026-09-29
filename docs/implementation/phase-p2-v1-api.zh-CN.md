# P02：两种 consumer 与 v1 API 兼容

[English](phase-p2-v1-api.md) | [繁體中文](phase-p2-v1-api.zh-TW.md) | **简体中文**


本机 API 以 `go run ./cmd/lab serve commerce.db provider.db [127.0.0.1:8080]` 启动，只接受明确的 loopback 地址。`BILLFORGE_INTERNAL_TOKEN` 设置内部操作凭证。这是本机验证入口，没有完整用户身分验证或外部网络部署设置。

| 契约 | 行为 |
| --- | --- |
| `POST /v1/quotes` | 公开价格或受内部凭证保护的合约报价；回固定、席次、内含量与超额费率、版本 checksum、现在应付、未来固定承诺、有效期限及必要 client 能力。若指定订阅、mode 与 expected revision，创建绑定该订阅的变更报价。 |
| `POST /v1/subscriptions` | 使用 quote ID、fingerprint、`Idempotency-Key`；检查必要 client 能力，再创建订阅及付款操作。合约接受另需内部凭证。 |
| `POST /v1/subscriptions/{id}/changes` | 只接受绑定原订阅及 revision 的报价；在创建调度／期中变更的同一数据库交易中确认钉选价格、席次及 quote expiry。 |
| `GET /v1/subscriptions/{id}` | 显示实际价格、revision、调度意图、发票、付款及权益来源 revision；权益投影未跟上时显示 pending。内部凭证额外显示 provider key 与请求键。 |
| `GET /v1/entitlements/{beneficiary}`、`GET /v1/invoices/{id}` | 查权益状态及来源，或核定帐单 line、更正及余额。此 MVP 将 beneficiary 对应到 customer ID，feature 为通用 `service`。 |
| `POST /v1/usage-events` | 每个事件独立返回 accepted／rejected；核对 tenant、source、meter、时间与事件身分。 |
| 内部 `POST /v1/contracts`、`/v1/migrations`、`/v1/repairs` | 需内部凭证；沿用同一领域服务发布合约、预览或创建迁移批量、运行指定差异修复。 |

变更报价的 `recurring_committed_minor` 是目标方案的整期费用。下期调度的 `due_now_minor` 为 0；期中升级的 `due_now_minor` 是依报价时刻计算的按比例估值，`due_now_estimated=true`。运行变更时会在同一交易中重新计算实际应付金额，回应中的 `pending_amount_minor` 才是新建付款义务的金额。报价席次与运行席次必须一致。

v1 既有 client 已知 `fixed`、`per_seat` 与 tasks 用量。新 meter 或 Net30 合约会在报价附上 `required_client_capabilities`。接受报价时必须通过 `X-Client-Capabilities` 声明对应能力；未声明回 `409 CLIENT_CAPABILITY_REQUIRED`，且不创建订阅。新 meter 的费率仍以原始分子／分母及 meter ID 呈现，不伪装成固定费。内部合约操作另外要求 `X-Lab-Internal-Token`。

API 合约测试覆盖 Basic 购买与重播、付款 UNKNOWN 后的 pending 权益、确认后开通、发票与权益查找、AI tokens 旧 client 拒绝与新 client 接受、Net30 的应付时点与能力拒绝、绑定 revision 的订阅变更、报价与运行席次不一致、已购或过期报价重用、调度与期中变更的应付时点、报价后未来目标价改变的拒绝、grace 与更正读取。

限制：变更报价使用当前可售价格，若期界有另一个有效价格，命令会明确冲突并要求重新报价。API 不提供税、完整 beneficiary／billing account 分离、正式授权、速率限制或跨服务部署。`as_of` 是本机时钟观测值；需搭配来源 revision 解读权益投影。
