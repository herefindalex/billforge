# Commerce MVP：A–D 設計推演

狀態：A–D 為設計與紙上推演；E、F1–F8、P01–P03 已有本機可執行切片。各項完成條件與限制見 [MVP 完成追蹤](../implementation/mvp-completion-tracker.md)；範圍依 [整體方案](../commerce-lab-plan.md)，34 份外部專案調查在 [研究索引](../research/README.md)。本機切片不代表整體已獲生產驗證。

| 階段 | 文件 | 審閱重點 |
| --- | --- | --- |
| A 政策 | [01 計價版本與業務政策](01-pricing-and-policies.md) | PriceVersion、D01–D12、捨入、比例計費、付款與權益時點 |
| B 模型 | [02 事實所有權與不變條件](02-domain-and-invariants.md) | 狀態機、原子邊界、I01–I21 的反例和保護點 |
| C 故障 | [03 情境、對帳與修復](03-scenarios-and-reconciliation.md) | S01–S12 的預期事實、金額及恢復行為 |
| D 平台 | [04 API、相容性與演進](04-platform-apis-and-migration.md) | P01–P03、兩種 consumer、價格遷移與灰度 |

閱讀順序：先確定政策，再讀事實所有權與場景，最後看 API 和遷移。所有時間是 UTC，服務／價格有效區間為 `[start, end)`。USD 金額展示為美元，持久化總額為整數 cents；單價與未捨入中間值用精確 decimal 或有理數。

這份設計的邊界：不涵蓋稅務、完整總帳、收入確認引擎、多幣別、任意促銷、所有 tier 計價法、真實 PSP 或部署。這些缺口不應被解讀為已驗證的能力。現有 Go＋SQLite 切片的驗證範圍與未完成項目，以實作文件及完成追蹤為準；設計主張不會自動轉成已驗證保證。


## Web Admin 擴充設計

[05 設計方案](05-web-admin.md)：描述 Web Admin、CLI／API、操作預覽、權限及命令恢復的需求。現有 Web Admin 已接線 C01–C49；整體驗收仍進行中，實際證據與缺口見 [Web Admin 實作紀錄](../implementation/web-admin.md)及[逐動作盤點](../implementation/web-admin-action-audit.md)。

- [06 Web Admin 工程契約](06-web-admin-contracts.md)：React、`.env` 帳密登入、49 項命令、資料表與恢復協議。
- [07 Web Admin 實作計畫](07-web-admin-implementation-plan.md)：22 項任務、依賴、階段與交付條件。
[08 驗收計畫](08-web-admin-test-plan.md)：30 組場景與逐命令矩陣；已有部分本機及瀏覽器驗證，尚未完成所有逐項簽核。完成與待辦以實作紀錄及逐動作盤點為準。
