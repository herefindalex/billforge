# F5：价格版本、cohort 与既有订阅迁移

[English](phase-f5-price-migrations.md) | [繁體中文](phase-f5-price-migrations.zh-TW.md) | **简体中文**


## 价格发布及新购

`PublishProPrice` 创建 draft、写入固定费、席次、tasks allowance 与超额费率组件，再原子发布。已发布版本及组件由数据库 trigger 禁止修改；同 payload 重送回原版本，不同 payload 使用同 ID 会冲突。`SelectCatalogPrice` 在指定生效时间创建 cohort selection；既有 selection 不可覆写。`CreateQuoteForCohort` 只从该 cohort 的已发布且已生效版本报价；原订阅仍保持原 PriceVersion。

S08 oracle 使用 Pro v1 固定 $50＋五席 $50＝$100，Pro v2 固定 $60＋五席 $50＝$110。cohort A 的新报价是 $110；default 仍是 $100。

## 迁移及故障处理

`PreviewPriceMigration` 对每户展示原／目标版本、席次、下期原价／新价、现有权益状态及「同方案，续约付款决定权益」规则。`PlanPriceMigration` 以 cohort、目标版本与排序后订阅清单形成稳定批量；每户保存原版本、revision、价格快照和下一期界。已调度变更、未决期中升级与其他未决价迁移互斥。

期界续约在同一交易内用 revision 和原版本 CAS 关闭 v1 assignment、开 v2 assignment、创建 v2 发票。重跑不重开。若某户状态与预览快照不符，该户标为 `conflicted` 并暂停批量；其他尚未处理的户暂停续价。操作员可查看差异、略过冲突户，或在仍有待处理户时恢复批量。略过的客户保留旧价。回退使用新的反向迁移与 assignment；不修改已核定发票或旧价格版本。

CLI「价格版本与 cohort 迁移」提供发布、选用、预览、建批量、暂停、略过、恢复；「状态查找」可读价格、cohort 与逐户迁移状态。验证涵盖两户 $110 续约、期界重跑、第二户冲突时第一户已成功的部分批量、明确略过后旧价续约，以及下一期反向迁移至 v1。

限制：此切片的发布入口只支持现有 Pro 组件语汇与 tasks meter；AI tokens SKU 的添加组件与 API 兼容性仍由 P01／P02 处理。此为本地单 writer lab，迁移暂停属显式操作状态，不仿真跨区域 worker 协调。
