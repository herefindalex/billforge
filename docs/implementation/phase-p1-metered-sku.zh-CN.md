# P01：配置式计量 SKU

[English](phase-p1-metered-sku.md) | [繁體中文](phase-p1-metered-sku.zh-TW.md) | **简体中文**


## 交付路径

`meter_schemas` 记录 meter ID、事件来源、单位与 schema 版本。`RegisterMeter` 为同一 ID 保留不可变设置；事件来源对应到记录用量时的 `source`。既有 `tasks` meter 为兼容旧数据使用 `*` 来源。

`PublishMeteredPrice` 接受方案、价格版本、固定费、可选席次费、meter、内含量、超额精确单价及生效时间。发布时确认 meter 已注册，将所有组件与不可变 checksum 一起提交。`SelectCatalogPrice` 将已发布版本指派给 cohort。购买仍使用原有 Quote、AcceptQuote、付款与权益路径；关帐、重算、续约则按价格版本的 meter 评价及写入发票项目。

交互菜单「价格版本与 cohort 迁移」可注册 meter、发布计量价格、指定 cohort；「状态查找」显示 meter、内含量与超额费率；「用量与关帐」依订阅有效价格选择 meter。

## 金额 oracle

AI 方案例子：每期固定 3000 cents、内含 100 个 `ai_tokens`、超额每 5 个 token 收 1 cent。105 个 token 的超额量是 5，关帐结果为 1 cent。续约发票包含固定费 3000 cents 与 `usage:ai_tokens:period:0` 1 cent，合计 3001 cents。既有 tasks 案例仍保留 20,003→0 cents、20,010→1 cent 的回归。

## 边界

一个价格版本目前支持一个用量 meter；复合 meter、阶梯价、预付储值、税与多币别需要额外模型。用量事件必须匹配该订阅在事件时间的价格版本及 meter，且来源符合 meter 注册信息。新 SKU 的权益仍使用现有订阅权益投影；每个 SKU 的 feature 集合与独立权益政策版本尚未建模。`MeteredPriceSpec` 是本机管理入口，不是外部自助发布 API。

验收：`go test ./lab -run TestP01 -count=1` 覆盖价格发布重播与不可变性、来源与 meter 拒绝、事件去重、精确计价及发票来源项目；`go test ./...` 保持既有案例回归。
