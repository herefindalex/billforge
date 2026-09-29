# E. 最小可執行核心：S01、S04、S05

[English](phase-e.md) | **繁體中文** | [简体中文](phase-e.zh-CN.md)


狀態：E 切片已在本機執行 `go test ./...` 和三種 CLI demo；後續的續約能力另見 [F1](phase-f1-renewals.zh-TW.md)。下表記錄 E 當時的實作邊界，不是目前所有程式能力；這也不是生產環境驗證。來源設計見 [設計索引](../design/README.zh-TW.md)。

## 實作邊界

| 已實作 | 暫未實作 |
| --- | --- |
| Basic v1 $20 固定費、釘選 Quote、一次性接受與 HTTP 命令冪等身分 | Pro、席次、用量、比例計費、合約與價格遷移 |
| Invoice／單一行核定、不可變觸發器、單一 PaymentOperation 與 outbox | 更正、credit、refund、完整應收與總帳 |
| 獨立 SQLite fake provider；相同 provider key／payload 僅一筆 capture | 真實 PSP、webhook 簽章、網路重試與 provider SLA |
| UNKNOWN／崩潰後查原 key、inbox 去重、成功不倒退、allocation 唯一 | 終局失敗、grace、取消與多期續約 |
| 權益由成功 operation＋完整 allocation 重建 | 複雜 EntitlementPolicyVersion 和多服務權益 |

兩個檔案各自提交交易。Commerce 在本地交易中同時保存 Quote 接受結果、核定 invoice、固定金額／幣別／key 的 PaymentOperation、outbox 和 audit。之後才呼叫 fake provider。provider 先提交 capture，再回覆 Commerce；故障注入點在兩者之間。`lost_response` 會將本地 operation 記成 UNKNOWN；`crash_after_provider` 留在 SUBMITTED。恢復時查原 provider key，確認成功才入一筆 allocation 並排入權益重建工作。查不到交易不會自動推斷「確定沒有扣款」。

## 驗證 oracle

| 測試 | 主要斷言 |
| --- | --- |
| `TestS01NewBasicPurchase` | Quote $20；付款前 pending、無 allocation；成功後單筆 $20 allocation；權益由 outbox 重建。 |
| `TestS04LostResponseKeepsOriginalOperation` | provider 已有一筆 capture，本地 UNKNOWN；查原 key 後成功；重查仍只有一筆 provider capture 和一筆 allocation。 |
| `TestS05CrashWebhookOrderAndProjectionRecovery` | provider 提交後重開程式；success webhook 重送和舊 pending 晚到不倒退；再次重開後重建權益且無第二次財務效果。 |
| `TestQuoteExpiryIdentityAndPriceImmutability` | fingerprint／idempotency 衝突被拒，同 Quote 換 key 不能再收一次，已發布價格與付款 payload 不可改。 |
| `TestQuoteExpiresAndAbsentProviderResultIsNotFailure` | 過期 Quote 被拒；provider 查無原操作時保持未決，不憑空分配或開通。 |

CLI 的三條路徑分別得到：正常 `pending → active → entitlement active`；lost response `unknown → provider lookup → succeeded`；崩潰 `submitted → provider lookup → succeeded`。三者最終都是一筆 $20 allocation。

## 後續進入 F 階段前的缺口

這個切片的 PriceVersion 只有固定費欄位，沒有把 A 階段的 charge definitions、rounding、proration、可購買生效區間完整落地；CatalogSelection 只提供 Basic default。沒有 HTTP 認證、授權或 webhook 簽章；測試使用可信的 in-process fake provider。SQLite trigger 鎖住此切片的主要財務 payload，但尚無一般化 billing close、跨期差額、退款預留或 reconciliation engine。下一步應從 S02／S03 續約與欠款政策開始，沿用同一來源身分與 outbox 模式，之後才加入 S07–S11 的金額模型。
