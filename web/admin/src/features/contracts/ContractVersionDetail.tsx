import { useQuery } from '@tanstack/react-query'
import { Alert, Button, Card, Descriptions, Empty, Result, Skeleton, Space, Tag, Typography } from 'antd'
import { useNavigate, useParams } from 'react-router-dom'
import { api, HttpError } from '../../api/client'
import Money from '../../components/Money'

export default function ContractVersionDetail() {
  const { id = '' } = useParams()
  const navigate = useNavigate()
  const query = useQuery({ queryKey: ['contract-version', id], queryFn: () => api.contract(id), enabled: id !== '' })

  if (query.isPending && !query.data) return <Skeleton active />
  if (query.isError && !query.data) {
    const status = query.error instanceof HttpError ? query.error.status : 0
    return <Result
      status={status === 404 ? '404' : status === 403 ? '403' : 'error'}
      title={status === 404 ? '找不到合約版本' : status === 403 ? '沒有權限查看合約版本' : '合約版本無法載入'}
      subTitle={query.error.message}
      extra={<Button onClick={() => void query.refetch()}>重試</Button>}
    />
  }
  if (!query.data) return null

  const contract = query.data.contract
  const quotePath = `/quotes/new?customer_id=${encodeURIComponent(contract.CustomerID)}&contract_version_id=${encodeURIComponent(contract.ID)}`
  return <div className="form-page contract-detail">
    <Space align="center" wrap>
      <Typography.Title level={2} style={{ margin: 0 }}>合約版本詳情</Typography.Title>
      {query.isFetching && <Tag>更新中</Tag>}
    </Space>
    <Typography.Paragraph type="secondary">資料查詢時間：{new Date(query.data.observed_at).toLocaleString()}。合約條款發布後不可修改；訂閱服務狀態與帳單收款狀態分開顯示。</Typography.Paragraph>
    {query.isError && <Alert type="warning" showIcon className="result-card" message="無法更新合約資料，以下是上次成功讀取的結果" description={query.error.message} />}
    {!contract.PostContractPriceVersionID && <Alert type="warning" showIcon className="result-card" message="尚未指定合約期滿後價格" description="期滿時若沒有可用後續價格，續約會進入人工審查；既有合約與帳單不會因此改寫。" />}
    <Card title="已發布條款" className="result-card" extra={<Button onClick={() => void query.refetch()} loading={query.isFetching}>重新整理</Button>}>
      <Descriptions bordered size="small" column={1} items={[
        { key: 'id', label: '合約版本 ID', children: <Typography.Text copyable>{contract.ID}</Typography.Text> },
        { key: 'customer', label: '客戶', children: <Button type="link" onClick={() => navigate(`/customers/${encodeURIComponent(contract.CustomerID)}`)}>{contract.CustomerID}</Button> },
        { key: 'version', label: '版本序號', children: contract.Version },
        { key: 'base', label: '基礎價格版本', children: <Button type="link" onClick={() => navigate(`/catalog/prices/${encodeURIComponent(contract.BasePriceVersionID)}`)}>{contract.BasePriceVersionID}</Button> },
        { key: 'fixed', label: '每期固定金額', children: <Money minor={contract.FixedMinor} currency={contract.Currency} /> },
        { key: 'seat', label: '每席金額', children: <Money minor={contract.SeatMinor} currency={contract.Currency} /> },
        { key: 'terms', label: '付款期限', children: `Net${contract.PaymentDays}` },
        { key: 'from', label: '生效起', children: new Date(contract.EffectiveFrom).toLocaleString() },
        { key: 'to', label: '生效止', children: new Date(contract.EffectiveTo).toLocaleString() },
        { key: 'post', label: '期滿後價格', children: contract.PostContractPriceVersionID ? <Button type="link" onClick={() => navigate(`/catalog/prices/${encodeURIComponent(contract.PostContractPriceVersionID)}`)}>{contract.PostContractPriceVersionID}</Button> : '未設定' },
        { key: 'checksum', label: 'Checksum', children: <Typography.Text copyable>{contract.Checksum}</Typography.Text> },
        { key: 'published', label: '發布時間', children: new Date(contract.PublishedAt).toLocaleString() },
      ]} />
    </Card>
    <Card title={`合約報價（${contract.QuoteCount}）`} className="result-card" extra={<Button onClick={() => navigate(`/quotes?contract_version_id=${encodeURIComponent(contract.ID)}`)}>查看全部報價</Button>}>
      {contract.Quotes.length === 0 && <Empty description="尚無合約報價" />}
      {contract.Quotes.map((quote) => <Card key={quote.ID} size="small" title={quote.ID} className="result-card">
        <Descriptions bordered size="small" column={1} items={[
          { key: 'seats', label: '席次', children: quote.SeatQuantity },
          { key: 'amount', label: '每期承諾金額', children: <Money minor={quote.AmountMinor} currency={quote.Currency} /> },
          { key: 'accepted', label: '接受狀態', children: quote.Accepted ? '已接受' : '未接受' },
          { key: 'expiry', label: '報價期限', children: new Date(quote.ExpiresAt).toLocaleString() },
        ]} />
        <Button onClick={() => navigate(`/quotes?id_prefix=${encodeURIComponent(quote.ID)}`)}>查看報價</Button>
      </Card>)}
      {contract.QuotesTruncated && <Alert type="warning" showIcon message="報價超過 20 筆；此頁僅顯示最近 20 筆，可到完整列表分頁查看" />}
    </Card>
    <Card title={`對應訂閱（${contract.SubscriptionCount}）`} className="result-card" extra={<Button onClick={() => navigate(`/subscriptions?contract_version_id=${encodeURIComponent(contract.ID)}`)}>查看全部訂閱</Button>}>
      {contract.Subscriptions.length === 0 && <Empty description="尚無已接受的合約訂閱" />}
      {contract.Subscriptions.map((sub) => <Card key={sub.ID} size="small" title={sub.ID} className="result-card">
        <Descriptions bordered size="small" column={1} items={[
          { key: 'status', label: '服務狀態', children: <Tag>{sub.Status}</Tag> },
          { key: 'seats', label: '席次', children: sub.SeatQuantity },
          { key: 'transition', label: '期滿轉價', children: sub.Transitioned ? '已轉換' : '尚未轉換' },
          { key: 'invoice', label: '最近帳單', children: sub.LatestInvoiceID ? <Button type="link" onClick={() => navigate(`/invoices/${encodeURIComponent(sub.LatestInvoiceID)}`)}>{sub.LatestInvoiceID}</Button> : '未知' },
          { key: 'due', label: '最近帳單到期日', children: sub.LatestDueAt ? new Date(sub.LatestDueAt).toLocaleString() : '未知' },
          { key: 'outstanding', label: '本地未結清金額', children: sub.InvoiceOutstandingMinor !== null ? <Money minor={sub.InvoiceOutstandingMinor} currency={sub.InvoiceCurrency} /> : '未知' },
        ]} />
        <Button onClick={() => navigate(`/subscriptions/${encodeURIComponent(sub.ID)}`)}>查看訂閱</Button>
      </Card>)}
      {contract.SubscriptionsTruncated && <Alert type="warning" showIcon message="訂閱超過 20 筆；此頁僅顯示前 20 筆，可到完整列表分頁查看" />}
    </Card>
    <Space className="result-card" wrap>
      <Button type="primary" onClick={() => navigate(quotePath)}>建立合約報價</Button>
      <Button onClick={() => navigate('/contracts')}>返回合約列表</Button>
    </Space>
  </div>
}
