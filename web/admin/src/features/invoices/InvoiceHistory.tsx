import { useQuery } from '@tanstack/react-query'
import { Alert, Button, Card, Descriptions, Empty, Result, Skeleton, Space, Table, Tag, Typography } from 'antd'
import { useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { api, HttpError, type InvoiceHistoryKind, type InvoiceHistoryRow } from '../../api/client'
import Money from '../../components/Money'

const titles: Record<InvoiceHistoryKind, string> = {
  corrections: '減額更正歷史',
  applications: 'Credit 抵扣歷史',
  grants: '釋出 Credit 歷史',
  refunds: '來源退款歷史',
}

const originLabels: Record<string, string> = {
  admin_command: '人工減額命令',
  delayed_change: '延遲開通更正',
  unfulfilled_change: '未履行升級決議',
  domain_request: '其他領域操作',
}

export default function InvoiceHistory({ kind }: { kind: InvoiceHistoryKind }) {
  const { id = '' } = useParams()
  const navigate = useNavigate()
  const [cursors, setCursors] = useState([''])
  const [page, setPage] = useState(0)
  const query = useQuery({
    queryKey: ['invoice-history', id, kind, cursors[page]],
    queryFn: () => api.invoiceHistory<InvoiceHistoryRow>(id, kind, cursors[page]),
    enabled: id !== '',
  })
  if (query.isPending) return <Skeleton active />
  if (query.isError) {
    const status = query.error instanceof HttpError ? query.error.status : 0
    return <Result status={status === 404 ? '404' : 'error'} title={status === 404 ? '找不到帳單歷史' : '帳單歷史無法載入'} subTitle={query.error.message} extra={<Button onClick={() => void query.refetch()}>重試</Button>} />
  }
  const money = (minor: string) => <Money minor={minor} currency={query.data.currency} />
  const source = (item: InvoiceHistoryRow) => {
    if ('ReductionMinor' in item) return <Space wrap>{originLabels[item.OriginKind] ?? '其他領域操作'}{item.OriginKind === 'admin_command'
      ? <Button type="link" onClick={() => navigate(`/commands/${encodeURIComponent(item.OriginID)}`)}>{item.OriginID}</Button>
      : <Typography.Text copyable>{item.OriginID}</Typography.Text>}</Space>
    if ('SourceInvoiceID' in item) return <Button type="link" onClick={() => navigate(`/invoices/${encodeURIComponent(item.SourceInvoiceID)}`)}>{item.SourceInvoiceID}</Button>
    if ('CorrectionID' in item) return <Typography.Text copyable>{item.CorrectionID}</Typography.Text>
    return <Typography.Text copyable>{item.GrantID}</Typography.Text>
  }
  const details = (item: InvoiceHistoryRow) => {
    if ('ReductionMinor' in item) return [
      { key: 'reason', label: '理由', children: item.Reason },
      { key: 'before', label: '更正前應收', children: money(item.PriorObligationMinor) },
      { key: 'after', label: '更正後應收', children: money(item.NewObligationMinor) },
    ]
    if ('SourceInvoiceID' in item) return [
      { key: 'grant', label: 'Credit ID', children: <Typography.Text copyable>{item.GrantID}</Typography.Text> },
      { key: 'invoice', label: '來源帳單', children: source(item) },
    ]
    if ('CorrectionID' in item) return [
      { key: 'correction', label: '更正 ID', children: <Typography.Text copyable>{item.CorrectionID}</Typography.Text> },
      { key: 'release', label: '釋出 ID', children: <Typography.Text copyable>{item.ReleaseID}</Typography.Text> },
      { key: 'payment', label: '來源付款操作', children: <Typography.Text copyable>{item.SourceOperationID}</Typography.Text> },
    ]
    return [
      { key: 'grant', label: 'Credit ID', children: <Typography.Text copyable>{item.GrantID}</Typography.Text> },
      { key: 'status', label: '狀態', children: <Tag>{item.Status}</Tag> },
    ]
  }
  return <div className="form-page">
    <Typography.Title level={2}>{titles[kind]}</Typography.Title>
    <Button onClick={() => navigate(`/invoices/${encodeURIComponent(id)}`)}>返回帳單詳情</Button>
    <Card className="result-card" extra={<Button onClick={() => void query.refetch()}>重新整理</Button>}>
      <Typography.Paragraph type="secondary">依建立時間與記錄 ID 由新到舊排序；新紀錄不會讓已讀頁重複。</Typography.Paragraph>
      <Table<InvoiceHistoryRow> rowKey="ID" dataSource={query.data.items} pagination={false} scroll={{ x: 'max-content' }} locale={{ emptyText: <Empty description="尚無紀錄" /> }} columns={[
        { title: '記錄 ID', dataIndex: 'ID', render: (value: string) => <Typography.Text copyable>{value}</Typography.Text> },
        { title: '金額', render: (_, item) => money('ReductionMinor' in item ? item.ReductionMinor : item.AmountMinor) },
        { title: '來源', render: (_, item) => source(item) },
        { title: '狀態', render: (_, item) => 'Status' in item ? <Tag>{item.Status}</Tag> : '—' },
        { title: '建立時間', dataIndex: 'CreatedAt', render: (value: string) => new Date(value).toLocaleString() },
      ]} expandable={{ expandedRowRender: (item) => <Descriptions bordered size="small" column={1} items={details(item)} /> }} />
      <Space wrap className="result-card">
        <Button disabled={page === 0} onClick={() => setPage(page - 1)}>上一頁</Button>
        <Typography.Text>第 {page + 1} 頁</Typography.Text>
        <Button disabled={!query.data.next_cursor} onClick={() => { setCursors([...cursors.slice(0, page + 1), query.data.next_cursor]); setPage(page + 1) }}>下一頁</Button>
      </Space>
      {query.isFetching && <Alert type="info" showIcon message="更新中" />}
    </Card>
  </div>
}
