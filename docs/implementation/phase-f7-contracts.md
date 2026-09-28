# F7：企業合約、Net30 與到期價源

`PublishContract` 將客戶、版本、基底 PriceVersion、允許覆寫的固定費及席次費、有效區間、Net30 與可選後續價寫成不可變的 ContractVersion。基底價格仍提供幣別及用量元件；合約只覆寫固定與席次價。合約報價及接受有獨立入口與指紋，普通自助接受命令不能誤處理合約報價。

Acme 合約固定 $40、每席 $7、五席，核定發票 $75，`invoice_contracts` 記價源。服務在合約及發票核定後立即開通，權益理由為 `enterprise_contract_net30`；沒有付款成功的虛構事實。付款操作持久化但不立即送 provider。到期時 `CollectDueContractInvoices` 才以原操作建立 outbox，收款重跑不新增 capture。合約收款可在服務期結束後執行，因 Net30 應收不會隨服務期消失。

合約仍有效時，下期使用同一覆寫價和新 Net30 發票。合約到期若缺後續價格，留下 `contract_next_price_missing` hold，停止自動續價，權益投影改為 suspended；其後收妥舊應收也不會擅自選 catalog 現價。若合約預先指定已發布且到期時有效的後續價格，期界以新 assignment 和 subscription revision 切換，`contract_transitions` 防止重複切換；後續按一般自助價格續期。跨到合約到期日但下一個服務期只部分落在合約內時停止自動開票，要求人工定義分段政策。

CLI「企業合約與 Net30」提供發布、報價、接受及到期收款；狀態查詢展示版本與訂閱的合約來源。驗證覆蓋 $75、未付款即開通、Net30 前沒有 capture、到期後依原 operation 收款、缺後續價 hold，以及明確後續價只在期界轉換一次。
