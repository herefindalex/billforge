# F8：對帳差異與修復

[English](phase-f8-reconciliation.md) | **繁體中文** | [简体中文](phase-f8-reconciliation.zh-CN.md)


## 範圍

`RunReconciliation` 讀取本地訂閱、權益、assignment、final invoice line、付款與 outbox，再讀 fake provider capture。每次保存 run ID、比較時間及差異證據。相同種類與物件沿用差異 ID，後續比較能更新證據或標記 resolved；舊紀錄保留供稽核。

CLI 主選單「對帳、修復與人工決議」可執行比較、查差異、依差異 ID 修復或記錄人工決議；「狀態查詢」也能列出差異。每個修復必須提供穩定 request key。相同 key 重播回原操作；不同差異共用 key 會衝突。

## 分類與動作

| 差異 | 分類 | 動作 |
| --- | --- | --- |
| 缺失或舊版權益投影、待處理權益 outbox | `SAFE_AUTO_REPAIR` | 依訂閱與帳單來源重建投影，重新比較。 |
| 待處理 capture outbox | `RETRY_REQUIRED`；若付款已 submitted／unknown，為 `EXTERNAL_LOOKUP_REQUIRED` | 指定原 payment operation 重送；未知結果只查原 provider key。 |
| provider 成功但本地未觀測、付款結果未知 | `EXTERNAL_LOOKUP_REQUIRED` | 查原 key 並套用金額／幣別檢查。 |
| 未知 provider capture、金額／幣別不符、缺合約後續價 | `MANUAL_REVIEW` | 阻止自動修復，保存人工決議與審查者。 |
| assignment 與訂閱價不一致、final invoice line 金額不一致 | `UNSAFE_TO_REPAIR` | 阻止自動修復，保存人工決議。 |

修復計畫保存 expected、actual、來源 revision。執行前重新對帳；來源證據、分類或 revision 改變便 blocked，必須用新 request key。執行後再次對帳，原差異消失才標 verified；仍存在則保持 executed／waiting，供後續調查。若 provider 金額不符，付款修復會 blocked。人工決議只記錄審查，不會改寫付款、發票或價格事實。

## 驗證與限制

`go test ./lab -run TestS12 -count=1` 覆蓋缺權益重建、重播、來源 revision 改變、指定 capture 重送、UNKNOWN 原 key 查詢，以及陌生或金額不符的 provider capture。`go test ./...` 覆蓋既有商務情境。

比較是本地與 fake provider 的當前觀測，不提供任意歷史時間點的快照。`cutoff_at` 是此次比較的水位標記；`local_observed_at` 與 `provider_observed_at` 分別記錄實際讀取時間。待處理 outbox 會出現在差異中；目前沒有年齡門檻，因此它可表示正常排隊工作，操作員需按原流程或指定修復執行。此實作不連線真實支付系統，也不取代財務總帳對帳。
