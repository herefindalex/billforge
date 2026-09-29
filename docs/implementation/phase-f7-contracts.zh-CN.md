# F7：企业合约、Net30 与到期价源

[English](phase-f7-contracts.md) | [繁體中文](phase-f7-contracts.zh-TW.md) | **简体中文**


`PublishContract` 将客户、版本、基底 PriceVersion、允许覆写的固定费及席次费、有效区间、Net30 与可选后续价写成不可变的 ContractVersion。基底价格仍提供币别及用量组件；合约只覆写固定与席次价。合约报价及接受有独立入口与指纹，普通自助接受命令不能误处理合约报价。

Acme 合约固定 $40、每席 $7、五席，核定发票 $75，`invoice_contracts` 记价源。服务在合约及发票核定后立即开通，权益理由为 `enterprise_contract_net30`；没有付款成功的虚构事实。付款操作持久化但不立即送 provider。到期时 `CollectDueContractInvoices` 才以原操作创建 outbox，收款重跑不添加 capture。合约收款可在服务期结束后运行，因 Net30 应收不会随服务期消失。

合约仍有效时，下期使用同一覆写价和新 Net30 发票。合约到期若缺后续价格，留下 `contract_next_price_missing` hold，停止自动续价，权益投影改为 suspended；其后收妥旧应收也不会擅自选 catalog 现价。若合约预先指定已发布且到期时有效的后续价格，期界以新 assignment 和 subscription revision 切换，`contract_transitions` 防止重复切换；后续按一般自助价格续期。跨到合约到期日但下一个服务期只部分落在合约内时停止自动开票，要求人工定义分段政策。

CLI「企业合约与 Net30」提供发布、报价、接受及到期收款；状态查找展示版本与订阅的合约来源。验证覆盖 $75、未付款即开通、Net30 前没有 capture、到期后依原 operation 收款、缺后续价 hold，以及明确后续价只在期界转换一次。
