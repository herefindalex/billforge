import { useQuery } from '@tanstack/react-query'
import { Alert, Button, Card, Descriptions, Empty, Result, Skeleton, Space, Table, Tag, Typography } from 'antd'
import { useEffect, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { api, canShowStaleRead, HttpError, type UsageRating } from '../../api/client'
import Money from '../../components/Money'

const statusLabels = {
 estimated: '估算中',
 finalized: '已關帳',
 rerated: '已重算',
}

function dateText(value: string | null) {
 return value ? new Date(value).toLocaleString() : '—'
}

export default function UsagePeriodDetail() {
 const { id = '', index = '' } = useParams()
 const navigate = useNavigate()
 const [cursors, setCursors] = useState([''])
 const [page, setPage] = useState(0)
 useEffect(() => {
  setCursors([''])
  setPage(0)
 }, [id, index])
 const detail = useQuery({
  queryKey: ['usage-period', id, index],
  queryFn: () => api.usagePeriod(id, index),
  enabled: id !== '' && index !== '',
 })
 const history = useQuery({
  queryKey: ['usage-period-ratings', id, index, cursors[page]],
  queryFn: () => api.usagePeriodRatings(id, index, cursors[page]),
  enabled: id !== '' && index !== '',
 })

 if (detail.isPending) return <Skeleton active />
 if (detail.isError && (!detail.data || !canShowStaleRead(detail.error))) {
  const status = detail.error instanceof HttpError ? detail.error.status : 0
  return <Result status={status === 404 ? '404' : status === 403 ? '403' : 'error'} title={status === 404 ? '找不到用量帳期' : status === 403 ? '沒有權限查看用量帳期' : '用量帳期載入失敗'} subTitle={detail.error.message} extra={<Button onClick={() => void detail.refetch()}>重試</Button>} />
 }
 if (!detail.data) return null

 const period = detail.data.period
 const current = period.Estimate ?? period.LatestRating
 const money = (minor: string) => <Money minor={minor} currency={period.Currency} />
 const ratingColumns = [
  { title: '修訂', dataIndex: 'Revision', key: 'revision' },
  { title: '用量', dataIndex: 'Quantity', key: 'quantity' },
  { title: '超額用量', dataIndex: 'OverageQuantity', key: 'overage' },
  { title: '計價金額', dataIndex: 'RoundedMinor', key: 'rounded', render: (value: string) => money(value) },
  { title: '本次差額', dataIndex: 'DeltaMinor', key: 'delta', render: (value: string) => money(value) },
  { title: '計算時間', dataIndex: 'RatedAt', key: 'at', render: (value: string) => dateText(value) },
 ]

 return <div className="form-page">
  <Space align="center" wrap>
   <Typography.Title level={2} style={{ margin: 0 }}>用量帳期詳情</Typography.Title>
   <Tag color={period.Status === 'estimated' ? 'processing' : period.Status === 'rerated' ? 'orange' : 'success'}>{statusLabels[period.Status]}</Tag>
   {detail.isFetching && <Tag>更新中</Tag>}
  </Space>
  <Typography.Paragraph type="secondary">資料查詢時間：{dateText(detail.data.observed_at)}。帳期估算依目前已收到的事件計算，並非已收款或已開立帳單的金額。</Typography.Paragraph>
  {detail.isError && <Alert type="warning" showIcon className="result-card" message="帳期更新失敗，顯示上次讀取結果" description={detail.error.message} />}
  <Space wrap className="result-card">
   <Button onClick={() => navigate(`/subscriptions/${encodeURIComponent(id)}`)}>返回訂閱</Button>
   <Button onClick={() => { void detail.refetch(); void history.refetch() }}>重新整理</Button>
   <Button onClick={() => navigate(`/data/usage-events?subscription_id=${encodeURIComponent(id)}&period_index=${encodeURIComponent(index)}`)}>查看用量事件</Button>
   {period.Status === 'estimated'
    ? <Button disabled={detail.isError} onClick={() => navigate(`/usage-periods/${encodeURIComponent(id)}/close`, { state: { period_index: index } })}>關閉用量帳期</Button>
    : <Button disabled={detail.isError} onClick={() => navigate(`/usage-periods/${encodeURIComponent(id)}/rerate`, { state: { period_index: index } })}>重算用量</Button>}
  </Space>

  <Card title="帳期狀態" className="result-card">
   <Descriptions bordered size="small" column={1} items={[
    { key: 'subscription', label: '訂閱 ID', children: <Typography.Text copyable>{period.SubscriptionID}</Typography.Text> },
    { key: 'index', label: '帳期序號', children: period.PeriodIndex },
    { key: 'range', label: '帳期範圍', children: `${dateText(period.PeriodStart)} ～ ${dateText(period.PeriodEnd)}` },
    { key: 'price', label: '價格版本', children: <Typography.Text copyable>{period.PriceVersionID}</Typography.Text> },
    { key: 'cutoff', label: '事件截止', children: dateText(period.CutoffAt) },
    { key: 'closed', label: '關帳時間', children: dateText(period.ClosedAt) },
   ]} />
  </Card>

  <Card title={period.Status === 'estimated' ? '目前估算' : '已保存的計價結果'} className="result-card">
   {period.Status === 'estimated' && <Alert type="info" showIcon message="此金額只是估算；關帳後才會保存修訂版本。" className="result-card" />}
   {current ? <Descriptions bordered size="small" column={1} items={[
    { key: 'quantity', label: '收到用量', children: current.Quantity },
    { key: 'included', label: '包含用量', children: current.IncludedQuantity },
    { key: 'overage', label: '超額用量', children: current.OverageQuantity },
    { key: 'exact', label: '精確金額（最小貨幣單位）', children: `${current.ExactMinorNumerator} ÷ ${current.ExactMinorDenominator}` },
    { key: 'rounded', label: '半數取偶數捨入後金額', children: money(current.RoundedMinor) },
    { key: 'delta', label: '相對前次計價差額', children: period.Status === 'estimated' ? '尚未關帳' : money(current.DeltaMinor) },
    { key: 'revision', label: '計價修訂', children: period.Status === 'estimated' ? '尚未保存' : current.Revision },
   ]} /> : <Empty description="此價格版本沒有用量計價規則" />}
  </Card>

  {period.Status !== 'estimated' && <Card title="帳務套用與減額來源" className="result-card">
   <Descriptions bordered size="small" column={1} items={[
    { key: 'rated', label: '最新計價總額', children: money(period.RatedMinor) },
    { key: 'billed', label: '已列帳金額', children: money(period.BilledMinor) },
    { key: 'credited', label: '已減額金額', children: money(period.CreditedMinor) },
    { key: 'applied', label: '套用次數', children: period.AppliedCount },
    { key: 'notes', label: 'Credit Note 筆數', children: period.CreditNoteCount },
    { key: 'noteAmount', label: 'Credit Note 減額合計', children: money(period.CreditNoteMinor) },
   ]} />
   <Typography.Paragraph type="secondary" style={{ marginTop: 12 }}>每個修訂的「本次差額」來自該修訂的總計價金額與前次結果之差；下方歷史保留用量、超額量、精確分數與價格版本，可追查差額。</Typography.Paragraph>
  </Card>}

  <Card title="計價修訂歷史" className="result-card" extra={<Button onClick={() => void history.refetch()}>重新整理</Button>}>
   {history.isPending ? <Skeleton active /> : history.isError && !canShowStaleRead(history.error) ? <Alert type="error" showIcon message={history.error instanceof HttpError && history.error.status === 403 ? '沒有權限查看計價修訂歷史' : '修訂歷史無法載入'} description={<Button onClick={() => void history.refetch()}>重試</Button>} /> : !history.data ? <Alert type="error" showIcon message="修訂歷史載入失敗" description={<Button onClick={() => void history.refetch()}>重試</Button>} /> : <>
    {history.isError && <Alert type="warning" showIcon message="修訂歷史更新失敗，顯示上次讀取結果" description={history.error.message} className="result-card" />}
    <Table<UsageRating> rowKey="ID" dataSource={history.data.items} columns={ratingColumns} pagination={false} scroll={{ x: 'max-content' }} locale={{ emptyText: <Empty description="尚無已保存計價修訂" /> }} expandable={{ expandedRowRender: (rating) => <Descriptions bordered size="small" column={1} items={[
     { key: 'id', label: '計價 ID', children: <Typography.Text copyable>{rating.ID}</Typography.Text> },
     { key: 'price', label: '價格版本', children: rating.PriceVersionID },
     { key: 'included', label: '包含用量', children: rating.IncludedQuantity },
     { key: 'exact', label: '精確金額（最小貨幣單位）', children: `${rating.ExactMinorNumerator} ÷ ${rating.ExactMinorDenominator}` },
     { key: 'rounded', label: '計價總額', children: money(rating.RoundedMinor) },
     { key: 'delta', label: '相對前次差額', children: money(rating.DeltaMinor) },
    ]} /> }} />
    <Space wrap className="result-card">
     <Button disabled={page === 0} onClick={() => setPage(page - 1)}>上一頁</Button>
     <Typography.Text>第 {page + 1} 頁</Typography.Text>
     <Button disabled={history.isError || !history.data.next_cursor} onClick={() => { setCursors([...cursors.slice(0, page + 1), history.data.next_cursor]); setPage(page + 1) }}>下一頁</Button>
    </Space>
   </>}
  </Card>
 </div>
}
