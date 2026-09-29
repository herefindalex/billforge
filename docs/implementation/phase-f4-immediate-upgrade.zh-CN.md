# F4：期中 Basic → Pro 升级

[English](phase-f4-immediate-upgrade.md) | [繁體中文](phase-f4-immediate-upgrade.zh-TW.md) | **简体中文**


## 可运行范围

`RequestImmediateProUpgrade` 以订阅 revision CAS 和请求键创建补差额发票。只接受有效且当期已付清的 Basic 订阅；同一订阅的下期调度、取消及其他未决期中升级互斥。价格来源是请求当时已发布、已生效的 Pro catalog selection。原 Basic assignment 在款项确认前保持有效。

金额采各价款分别按剩余奈秒／服务期奈秒比例计算，并以 half-even 舍入到 cents，再以 Pro 剩余价款减 Basic 未使用价款。以 2026-09-01 至 2026-10-01、Basic $20、Pro 五席 $100 为例，09-16 请求创建 $40 的独立发票与稳定 provider operation。原当期 Basic 发票不改。

付款 `UNKNOWN` 保留原操作并维持 Basic。09-18 查证原付款成功后，以实际开通时间重算：Pro $43.33、Basic 未用 $8.67，实际应收 $34.66。系统原子关闭 Basic assignment、开 Pro assignment、增加订阅 revision，再用持久 outbox 过帐 $5.34 减额更正。更正只从已收 allocation 释放 funded credit；重跑不会重复发给客户。

期界时未决升级阻止下期续价，并留下 `renewal_holds`。若付款在期满后才确认，升级标记 `needs_review`，不补开已过期的 Pro 服务。操作员可从 CLI 运行「处理未提供升级服务的补差额」；此专用命令将补差额发票义务全额冲回，按实收产生 credit，之后可走既有 credit 退款流程。处理请求键固定为 change ID，崩溃重跑会回传同一更正。处理完成后可按原 Basic 价继续续期。

## 操作与验证

CLI 的「订阅调度与取消」提供期中升级；「付款」提供送出与原操作查证；「发票更正与 credit」提供延迟开通更正、未服务补偿；「状态查找」提供变更、金额、状态与处理标记。

`go test -count=1 ./...` 通过。S07 oracle 覆盖 $40、付款 UNKNOWN 维持 Basic、09-18 更正 $5.34 并只产生 $5.34 funded credit、重跑不重复、期界暂停与期满补偿重跑。保留的限制：本切片只处理 Basic → Pro 且补差额为正；期中降级、额外用量、企业合约及多币别由后续情境处理。
