import { useQuery } from '@tanstack/react-query'
import { Alert, Button, Card, Descriptions, Result, Skeleton, Space, Table, Tag, Typography } from 'antd'
import { useNavigate, useParams } from 'react-router-dom'
import { api, canShowStaleRead, HttpError } from '../../api/client'
import Money from '../../components/Money'

export default function CustomerDetail() {
  const { id = '' } = useParams()
  const navigate = useNavigate()
  const query = useQuery({ queryKey: ['customer', id], queryFn: () => api.customer(id), enabled: id !== '' })
  if (query.isPending) return <Skeleton active />
  if (query.isError && (!query.data || !canShowStaleRead(query.error))) {
    const status = query.error instanceof HttpError ? query.error.status : 0
    return <Result status={status === 404 ? '404' : status === 403 ? '403' : 'error'} title={status === 404 ? '找不到客戶' : status === 403 ? '沒有權限查看客戶' : '客戶資料無法載入'} subTitle={query.error.message} extra={<Button onClick={() => void query.refetch()}>重試</Button>} />
  }
  if (!query.data) return null
  const stale = query.isError
  const customer = query.data
  const limit = BigInt(customer.RelatedLimit)
  const truncated = BigInt(customer.SubscriptionCount) > limit || BigInt(customer.QuoteCount) > limit || BigInt(customer.InvoiceCount) > limit || BigInt(customer.CreditCount) > limit
  return <div className="form-page">
    <Space align="center" wrap>
      <Typography.Title level={2} style={{ margin: 0 }}>客戶詳情</Typography.Title>
      {query.isFetching && <Tag>更新中</Tag>}
      <Button onClick={() => void query.refetch()}>重新整理</Button>
    </Space>
    {stale && <Alert type="warning" showIcon className="result-card" message="無法更新客戶；以下是上次成功讀取的資料" description={<Space wrap><span>上次讀取：{new Date(query.dataUpdatedAt).toLocaleString()}</span><Button onClick={() => void query.refetch()}>重試</Button></Space>} />}
    <Typography.Paragraph type="secondary">資料查詢時間：{new Date(query.dataUpdatedAt).toLocaleString()}</Typography.Paragraph>
    {truncated && <Alert type="warning" showIcon className="form-alert" message={`每類僅顯示最近 ${limit} 筆；請用資源列表繼續查詢。`} />}
    <Card title="客戶摘要">
      <Descriptions bordered size="small" column={1} items={[
        { key: 'id', label: '客戶 ID', children: <Typography.Text copyable>{customer.ID}</Typography.Text> },
        { key: 'subscriptions', label: '訂閱數', children: customer.SubscriptionCount },
        { key: 'quotes', label: '報價數', children: customer.QuoteCount },
        { key: 'invoices', label: '帳單數', children: customer.InvoiceCount },
        { key: 'credits', label: 'Credit 數', children: customer.CreditCount },
        { key: 'legacy', label: '舊帳戶 ID', children: customer.LegacyAccountID || '無' },
        { key: 'read', label: '讀取 Owner', children: customer.ReadOwner || '無' },
        { key: 'writer', label: '寫入 Owner', children: customer.WriterOwner || '無' },
      ]} />
      <Button type="primary" className="result-card" disabled={stale} onClick={() => navigate(`/quotes/new?customer_id=${encodeURIComponent(id)}`)}>為此客戶建立報價</Button>
    </Card>
    <Card title="訂閱" className="result-card">
      <Table size="small" pagination={false} rowKey="ID" dataSource={customer.Subscriptions ?? []} scroll={{ x: 650 }} columns={[
        { title: '訂閱 ID', dataIndex: 'ID', render: (value: string) => <Button type="link" onClick={() => navigate(`/subscriptions/${encodeURIComponent(value)}`)}>{value}</Button> },
        { title: '實際價格版本', dataIndex: 'PriceVersionID' },
        { title: '席次', dataIndex: 'SeatQuantity' },
        { title: 'Revision', dataIndex: 'Revision' },
        { title: '狀態', dataIndex: 'Status', render: (value: string) => <Tag>{value}</Tag> },
        { title: '權益', dataIndex: 'EntitlementStatus' },
      ]} />
    </Card>
    <Card title="報價" className="result-card">
      <Table size="small" pagination={false} rowKey="ID" dataSource={customer.Quotes ?? []} scroll={{ x: 600 }} columns={[
        { title: '報價 ID', dataIndex: 'ID', render: (value: string) => <Button type="link" disabled={stale} onClick={() => navigate(`/quotes/${encodeURIComponent(value)}/accept`)}>{value}</Button> },
        { title: '金額', key: 'amount', render: (_, row) => <Money minor={row.AmountMinor} currency={row.Currency} /> },
        { title: '合約版本', dataIndex: 'ContractVersionID', render: (value: string) => value || '無' },
        { title: '到期', dataIndex: 'ExpiresAt', render: (value: string) => new Date(value).toLocaleString() },
        { title: '狀態', dataIndex: 'Accepted', render: (value: boolean) => value ? '已接受' : '尚未接受' },
      ]} />
    </Card>
    <Card title="帳單與 Credit" className="result-card">
      <Space direction="vertical" style={{ width: '100%' }}>
        <Table size="small" pagination={false} rowKey="ID" dataSource={customer.Invoices ?? []} scroll={{ x: 500 }} columns={[
          { title: '帳單 ID', dataIndex: 'ID', render: (value: string) => <Typography.Text copyable>{value}</Typography.Text> },
          { title: '訂閱 ID', dataIndex: 'SubscriptionID', render: (value: string) => <Button type="link" onClick={() => navigate(`/subscriptions/${encodeURIComponent(value)}`)}>{value}</Button> },
          { title: '原始金額', key: 'total', render: (_, row) => <Money minor={row.TotalMinor} currency={row.Currency} /> },
        ]} />
        <Table size="small" pagination={false} rowKey="ID" dataSource={customer.Credits ?? []} scroll={{ x: 500 }} columns={[
          { title: 'Credit ID', dataIndex: 'ID', render: (value: string) => <Typography.Text copyable>{value}</Typography.Text> },
          { title: '來源帳單', dataIndex: 'InvoiceID' },
          { title: '金額', key: 'amount', render: (_, row) => <Money minor={row.AmountMinor} currency={row.Currency} /> },
        ]} />
      </Space>
    </Card>
  </div>
}
