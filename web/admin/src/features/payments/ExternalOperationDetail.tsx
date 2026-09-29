import { useQuery } from '@tanstack/react-query'
import { Alert, Button, Card, Descriptions, Result, Skeleton, Space, Tag, Typography } from 'antd'
import { useNavigate, useParams } from 'react-router-dom'
import { api, canShowStaleRead, HttpError } from '../../api/client'
import Money from '../../components/Money'

export default function ExternalOperationDetail({ kind }: { kind: 'payment' | 'refund' }) {
  const { id = '' } = useParams()
  const navigate = useNavigate()
  const payment = kind === 'payment'
  const plural = payment ? 'payments' : 'refunds'
  const title = payment ? '付款操作詳情' : '退款操作詳情'
  const query = useQuery({
    queryKey: ['external-operation-detail', kind, id],
    queryFn: () => api.externalOperation(payment ? 'C09' : 'C16', id),
    enabled: id !== '',
  })

  if (query.isPending && !query.data) return <Skeleton active />
  if (query.isError && (!query.data || !canShowStaleRead(query.error))) {
    const status = query.error instanceof HttpError ? query.error.status : 0
    return <Result
      status={status === 404 ? '404' : status === 403 ? '403' : 'error'}
      title={status === 404 ? `找不到${title}` : status === 403 ? `沒有權限查看${title}` : `${title}無法載入`}
      subTitle={query.error.message}
      extra={<Button onClick={() => void query.refetch()}>重試</Button>}
    />
  }
  if (!query.data) return null

  const operation = query.data.operation
  const sourcePath = payment ? `/invoices/${encodeURIComponent(operation.source_id)}` : `/credits/${encodeURIComponent(operation.source_id)}`
  const canDispatch = operation.status === 'created' && operation.outbox_status === 'pending'
  return <div className="form-page">
    <Space align="center" wrap>
      <Typography.Title level={2} style={{ margin: 0 }}>{title}</Typography.Title>
      {query.isFetching && <Tag>更新中</Tag>}
    </Space>
    <Typography.Paragraph type="secondary">資料查詢時間：{new Date(query.data.observed_at).toLocaleString()}。此頁顯示本地操作與派送紀錄；結果未明時請查證原操作。</Typography.Paragraph>
    {query.isError && <Alert type="warning" showIcon className="result-card" message="無法更新操作狀態，以下是上次成功讀取的資料" description={<Space wrap><span>上次讀取：{new Date(query.dataUpdatedAt).toLocaleString()}</span><Button onClick={() => void query.refetch()}>重試</Button></Space>} />}
    {operation.status === 'unknown' && <Alert type="warning" showIcon className="result-card" message="服務商結果尚未確認" description="請查證此操作；在結果確定前，不要建立第二筆付款或退款。" />}
    <Card title="操作與來源" className="result-card" extra={<Button onClick={() => void query.refetch()} loading={query.isFetching}>重新整理</Button>}>
      <Descriptions bordered size="small" column={1} items={[
        { key: 'id', label: '操作 ID', children: <Typography.Text copyable>{operation.id}</Typography.Text> },
        { key: 'source', label: payment ? '來源帳單' : '來源 Credit', children: <Button type="link" onClick={() => navigate(sourcePath)}>{operation.source_id}</Button> },
        { key: 'amount', label: '金額', children: <Money minor={operation.amount_minor} currency={operation.currency} /> },
        { key: 'status', label: '操作狀態', children: <Tag>{operation.status}</Tag> },
        { key: 'outbox', label: '派送狀態', children: <Tag>{operation.outbox_status}</Tag> },
      ]} />
    </Card>
    <Space className="result-card" wrap>
      {canDispatch && <Button type="primary" disabled={query.isError} onClick={() => navigate(`/${plural}/${encodeURIComponent(id)}/dispatch`)}>送出{payment ? '付款' : '退款'}</Button>}
      {payment && operation.status === 'definitively_failed' && <Button disabled={query.isError} onClick={() => navigate(`/payments/${encodeURIComponent(id)}/retry`)}>重試付款</Button>}
      <Button disabled={query.isError} onClick={() => navigate(`/${plural}/${encodeURIComponent(id)}/reconcile`)}>查證原操作</Button>
      <Button onClick={() => navigate(`/${plural}`)}>返回{payment ? '付款' : '退款'}列表</Button>
    </Space>
  </div>
}
