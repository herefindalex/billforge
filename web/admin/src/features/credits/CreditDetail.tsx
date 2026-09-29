import { useQuery } from '@tanstack/react-query'
import { Alert, Button, Card, Descriptions, Result, Skeleton, Space, Tag, Typography } from 'antd'
import { useNavigate, useParams } from 'react-router-dom'
import { api, canShowStaleRead, HttpError } from '../../api/client'
import Money from '../../components/Money'

export default function CreditDetail() {
  const { id = '' } = useParams()
  const navigate = useNavigate()
  const query = useQuery({ queryKey: ['credit', id], queryFn: () => api.credit(id), enabled: id !== '' })

  if (query.isPending) return <Skeleton active />
  if (query.isError && (!query.data || !canShowStaleRead(query.error))) {
    const status = query.error instanceof HttpError ? query.error.status : 0
    return <Result status={status === 404 ? '404' : status === 403 ? '403' : 'error'} title={status === 404 ? '找不到 Credit' : status === 403 ? '沒有權限查看 Credit' : 'Credit 無法載入'} subTitle={query.error.message} extra={<Button onClick={() => void query.refetch()}>重試</Button>} />
  }

  if (!query.data) return null
  const credit = query.data.credit
  const balance = credit.Balance
  const money = (minor: string) => <Money minor={minor} currency={balance.Currency} />
  const refundList = `/refunds?grant_id=${encodeURIComponent(id)}`

  return <div className="form-page">
    <Space align="center" wrap>
      <Typography.Title level={2} style={{ margin: 0 }}>Credit 詳情</Typography.Title>
      {query.isFetching && <Tag>更新中</Tag>}
    </Space>
    {query.isError && <Alert type="warning" showIcon className="result-card" message="無法更新 Credit；以下是上次成功讀取的資料" description={<Space wrap><span>上次讀取：{new Date(query.dataUpdatedAt).toLocaleString()}</span><Button onClick={() => void query.refetch()}>重試</Button></Space>} />}
    <Typography.Paragraph type="secondary">資料查詢時間：{new Date(query.data.observed_at).toLocaleString()}。待派送或待查證的退款仍佔用保留額，不能再次抵扣或預留。</Typography.Paragraph>
    <Card title="來源與額度" className="result-card" extra={<Button onClick={() => void query.refetch()}>重新整理</Button>}>
      <Descriptions bordered size="small" column={1} items={[
        { key: 'id', label: 'Credit ID', children: <Typography.Text copyable>{credit.ID}</Typography.Text> },
        { key: 'invoice', label: '來源帳單', children: <Button type="link" onClick={() => navigate(`/invoices/${encodeURIComponent(credit.SourceInvoiceID)}`)}>{credit.SourceInvoiceID}</Button> },
        { key: 'operation', label: '來源付款操作', children: <Typography.Text copyable>{credit.SourceOperationID}</Typography.Text> },
        { key: 'correction', label: '來源帳單更正', children: <Typography.Text copyable>{credit.SourceCorrectionID}</Typography.Text> },
        { key: 'release', label: '釋出紀錄', children: <Typography.Text copyable>{credit.ReleaseID}</Typography.Text> },
        { key: 'created', label: '建立時間', children: new Date(credit.CreatedAt).toLocaleString() },
        { key: 'granted', label: '原始額度', children: money(balance.GrantedMinor) },
        { key: 'applied', label: '已抵扣', children: money(balance.AppliedMinor) },
        { key: 'reserved', label: '退款保留', children: money(balance.ReservedMinor) },
        { key: 'refunded', label: '已退款', children: money(balance.RefundedMinor) },
        { key: 'available', label: '可用額度', children: money(balance.AvailableMinor) },
      ]} />
    </Card>
    {balance.ReservedMinor !== '0' && <Alert className="result-card" type="info" showIcon message="仍有退款占用保留額" description="請在退款列表查看待派送或待查證的操作；在服務商結果確定前，保留額不會回到可用額度。" action={<Button onClick={() => navigate(refundList)}>查看退款</Button>} />}
    {balance.AvailableMinor === '0' && <Alert className="result-card" type="info" showIcon message="目前沒有可用額度" />}
    <Space className="result-card" wrap>
      <Button onClick={() => navigate(`/credits/${encodeURIComponent(id)}/apply`)} disabled={query.isError || balance.AvailableMinor === '0'}>抵扣帳單</Button>
      <Button onClick={() => navigate(`/credits/${encodeURIComponent(id)}/refunds/new`)} disabled={query.isError || balance.AvailableMinor === '0'}>預留退款</Button>
      <Button onClick={() => navigate(refundList)}>查看退款</Button>
    </Space>
  </div>
}
