# F6：tasks 用量、關帳、晚到與更正

[English](phase-f6-usage.md) | **繁體中文** | [简体中文](phase-f6-usage.zh-CN.md)


## 事件與歸期

`RecordUsage` 以 tenant＋source＋event ID 去重，payload 指紋包含訂閱、meter、數量與事件時間；相同內容重送回原事件，內容改動衝突。事件時間決定帳期與當時的 PriceVersion assignment，接收時間決定是否進入關帳 cutoff。Basic 或無有效 assignment 的 tasks 事件會拒絕。用量更正使用新事件 ID、引用原事件並保留原事件時間；不能撤銷超過原量。

`CloseUsagePeriod` 保存 cutoff 和首次 rating revision；`RerateUsagePeriod` 以持久事件重算原期總量。每個 revision 保存原量、allowance、超額量、未捨入的 cents 有理數、half-even 捨入值及相對前版的差額；無新事實時重跑回原 revision。

## 金額 oracle

Pro 包含 20,000 tasks，超額每 task $0.001。原期收到 20,003 tasks 時，精確超額是 $0.003，捨入 $0.00；下期續約發票保留金額為 0 的原期用量行。晚到同原期的 7 tasks 後，累計 20,010 tasks，精確 $0.010，累計應收 $0.01；扣除已入帳 $0.00，下一張續約發票增加標明原帳期的 $0.01 debit。原發票及其行項保持不可變。

若後續更正使已開票用量高於重新評價金額，`RunUsageCreditNotes` 對實際承載該用量的發票過帳減額更正，留下 usage CreditNote 與 Correction 關聯；funded credit 仍依原收款 allocation 釋放。若負差額尚未更正，續約不會默默把它併作新期收費。CLI「用量與關帳」提供事件、撤銷、關帳、重算及 CreditNote 操作；「狀態查詢」提供事件、rating revision、累計已開票與已更正金額。

驗證包含同 ID 重送及內容衝突、20,003→$0.00、晚到 7 tasks→下一張 $0.01、重算／續約重跑、撤銷 7 tasks→對原收費發票減額 $0.01，以及僅釋放實收 $0.01 credit。

限制：目前一個帳期只支援一個 tasks 價格版本；若同帳期出現兩個不同用量費率，關帳會拒絕並要求定義跨 assignment 的 allowance 政策。其他 meter 及新的 SKU 元件由 P01 擴充。
