# Billforge MVP 完成追蹤

[English](mvp-completion-tracker.md) | **繁體中文** | [简体中文](mvp-completion-tracker.zh-CN.md)


此清單依 [A–D 設計](../design/README.zh-TW.md)與 [總計畫](../commerce-lab-plan.zh-TW.md)驗收，避免把文件推演誤記成可執行能力。每項只有在實作、金額 oracle／故障測試、CLI 或 API 操作入口，以及限制說明均齊備時才標完成。

| 範圍 | 目前狀態 | 下一個可驗收結果 |
| --- | --- | --- |
| A–D 政策、模型、故障、平台推演 | 文件完成 | 隨實作保持設計與證據同步。 |
| S01、S04、S05：Basic 新購、UNKNOWN、webhook／權益恢復 | E 切片完成 | 保持回歸。 |
| S02、S03：續約、grace、暫停與恢復 | F1 切片完成 | 保持回歸。 |
| S11：減額更正、funded credit、退款 | F2 切片完成 | 保持回歸；進一步補服務延遲更正所需的來源。 |
| 操作入口 | 互動式選單 CLI 完成 | 新功能同步加入選單和狀態查詢。 |
| S06：下期升降級、取消、恢復 | [F3 切片完成](phase-f3-schedules.zh-TW.md) | 保持期界重播、revision 衝突及舊資料庫升級回歸。 |
| S07：期中升級與 proration | [F4 切片完成](phase-f4-immediate-upgrade.zh-TW.md) | 保持 $40 補差額、UNKNOWN 不提前切換、延遲開通後 $5.34 更正及期滿未服務全額補償回歸。 |
| S08：Pro v1/v2 與 cohort 遷移 | [F5 切片完成](phase-f5-price-migrations.zh-TW.md) | 保持已發布價格不可變、舊客釘選、預覽、逐戶 CAS、部分暫停與反向遷移回歸。 |
| S09：用量、關帳與晚到重算 | [F6 切片完成](phase-f6-usage.zh-TW.md) | 保持 20,003→$0.00、20,010→下一期 $0.01、負差額 CreditNote 與事件衝突／重播回歸。 |
| S10：企業合約與 Net30 | [F7 切片完成](phase-f7-contracts.zh-TW.md) | 保持 Acme 五席 $75、先開通、Net30 到期收款、缺後續價 hold 及明確後續價轉換回歸。 |
| S12：對帳差異與修復 | [F8 切片完成](phase-f8-reconciliation.zh-TW.md) | 保持差異分類、證據、前置 revision、穩定修復鍵與事後再核對回歸。 |
| P01：配置式新 SKU | [計量 SKU 切片完成](phase-p1-metered-sku.zh-TW.md) | 保持 AI tokens 新購、事件、關帳、續約發票與舊 tasks 計價回歸。 |
| P02：多 consumer 與 v1 API 相容 | [本機 v1 API 切片完成](phase-p2-v1-api.zh-TW.md) | 保持舊 client 新費用拒絕、合約能力、UNKNOWN／grace、訂閱變更的價格／席次釘選、應付時點估值及更正讀取契約回歸。 |
| P03：Account／Commerce 邊界與灰度遷移 | [本機遷移演練完成](phase-p3-account-cutover.zh-TW.md) | 保持來源 ID 映射、shadow、歷史回填、單一寫入者、停止條件及 adapter 讀取回歸。 |
| Web Admin：登入、查詢、C01–C49 操作 | [本機交付完成](web-admin.zh-TW.md) | 最新限定驗收：30／30 個場景通過，T01–T22 全完成，Web Admin 本機交付完成。可重跑證據與限制見 [動作證據盤點](web-admin-action-audit.zh-TW.md)。 |

可發布的 `PriceVersion` 元件、席次快照及訂閱 revision／assignment 已在 F3 落地；期中升級與補償已在 F4 落地；cohort 與既有客戶價遷移已在 F5 落地；tasks 用量關帳與更正已在 F6 落地；企業合約與 Net30 已在 F7 落地；S12 對帳修復、P01 新 meter、P02 本機 v1 API 與 P03 帳戶遷移演練亦有程式及驗收測試。真實支付、稅、多幣別、正式總帳與生產指標仍屬原計畫明定的 MVP 外範圍。

2026-10-03 驗收更新：兩個既有 C07／C08 瀏覽器競爭案例已補實際收款、固定金額、allocation 與收據核對，2／2 通過，詳見 [動作證據盤點](web-admin-action-audit.zh-TW.md)。 最終驗收見下方。

最新限定驗收：30／30 個場景通過，T01–T22 全完成，Web Admin 本機交付完成。可重跑證據與限制見 [動作證據盤點](web-admin-action-audit.zh-TW.md)。
