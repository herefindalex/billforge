# F6：tasks 用量、关帐、晚到与更正

[English](phase-f6-usage.md) | [繁體中文](phase-f6-usage.zh-TW.md) | **简体中文**


## 事件与归期

`RecordUsage` 以 tenant＋source＋event ID 去重，payload 指纹包含订阅、meter、数量与事件时间；相同内容重送回原事件，内容改动冲突。事件时间决定帐期与当时的 PriceVersion assignment，接收时间决定是否进入关帐 cutoff。Basic 或无有效 assignment 的 tasks 事件会拒绝。用量更正使用新事件 ID、引用原事件并保留原事件时间；不能撤销超过原量。

`CloseUsagePeriod` 保存 cutoff 和首次 rating revision；`RerateUsagePeriod` 以持久事件重算原期总量。每个 revision 保存原量、allowance、超额量、未舍入的 cents 有理数、half-even 舍入值及相对前版的差额；无新事实时重跑回原 revision。

## 金额 oracle

Pro 包含 20,000 tasks，超额每 task $0.001。原期收到 20,003 tasks 时，精确超额是 $0.003，舍入 $0.00；下期续约发票保留金额为 0 的原期用量行。晚到同原期的 7 tasks 后，累计 20,010 tasks，精确 $0.010，累计应收 $0.01；扣除已入帐 $0.00，下一张续约发票增加标明原帐期的 $0.01 debit。原发票及其行项保持不可变。

若后续更正使已开票用量高于重新评价金额，`RunUsageCreditNotes` 对实际承载该用量的发票过帐减额更正，留下 usage CreditNote 与 Correction 关联；funded credit 仍依原收款 allocation 释放。若负差额尚未更正，续约不会默默把它并作新期收费。CLI「用量与关帐」提供事件、撤销、关帐、重算及 CreditNote 操作；「状态查找」提供事件、rating revision、累计已开票与已更正金额。

验证包含同 ID 重送及内容冲突、20,003→$0.00、晚到 7 tasks→下一张 $0.01、重算／续约重跑、撤销 7 tasks→对原收费发票减额 $0.01，以及仅释放实收 $0.01 credit。

限制：目前一个帐期只支持一个 tasks 价格版本；若同帐期出现两个不同用量费率，关帐会拒绝并要求定义跨 assignment 的 allowance 政策。其他 meter 及新的 SKU 组件由 P01 扩充。
