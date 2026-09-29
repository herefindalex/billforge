# Commerce MVP：A–D 設計推演

[English](README.md) | **繁體中文** | [简体中文](README.zh-CN.md)

A–D 是設計與紙上推演。E、F1–F8、P01–P03 已有本機實作，但整體 MVP 尚未經生產驗證。實作證據與剩餘完成條件見 [MVP 完成追蹤](../implementation/mvp-completion-tracker.zh-TW.md)；範圍見[平台計畫](../commerce-lab-plan.zh-TW.md)。

| 階段 | 文件 | 審閱重點 |
| --- | --- | --- |
| A 政策 | [01 計價版本與業務政策](01-pricing-and-policies.zh-TW.md) | `PriceVersion`、D01–D12、捨入、比例計費，以及付款與權益時點 |
| B 模型 | [02 事實所有權與不變條件](02-domain-and-invariants.zh-TW.md) | 狀態機、原子邊界，以及 I01–I21 的反例與保護點 |
| C 故障 | [03 情境、對帳與修復](03-scenarios-and-reconciliation.zh-TW.md) | S01–S12 的預期事實、金額與恢復行為 |
| D 平台 | [04 API、相容性與演進](04-platform-apis-and-migration.zh-TW.md) | P01–P03、兩種使用者、價格遷移與漸進推出 |

建議先讀政策，再讀事實所有權與故障情境，最後讀 API 與遷移設計。所有時間使用 UTC；服務與價格的有效區間為半開區間 `[start, end)`。除非另有註明，金額均使用 USD 最小單位。

設計不包含稅務、生產總帳、收入認列引擎、多幣別、任意促銷、完整階梯定價或真實支付服務。解讀本機測試結果時也應遵守這些邊界。

## Web Admin 設計

- [05 Web Admin 設計方案](05-web-admin.zh-TW.md)說明管理介面、CLI／API 關係、預覽、權限與命令恢復。實作證據見 [Web Admin 指南](../implementation/web-admin.zh-TW.md)及[逐動作盤點](../implementation/web-admin-action-audit.zh-TW.md)。
- [06 工程契約](06-web-admin-contracts.zh-TW.md)定義 React 介面、`.env` 管理員登入、API 契約與 C01–C49 命令矩陣。
- [07 實作計畫](07-web-admin-implementation-plan.zh-TW.md)記錄 22 項實作任務及其依賴。
- [08 驗收計畫](08-web-admin-test-plan.zh-TW.md)定義 A01–A30 驗收檢查。
