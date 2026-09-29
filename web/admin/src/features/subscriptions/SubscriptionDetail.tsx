import { useQuery } from '@tanstack/react-query'
import { Alert, Button, Card, Descriptions, Empty, Result, Skeleton, Space, Table, Tag, Timeline, Typography } from 'antd'
import { useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { api, canShowStaleRead, HttpError, type SubscriptionPeriod } from '../../api/client'
import Money from '../../components/Money'

function dateText(value?: string) {
  return value ? new Date(value).toLocaleString() : '未知'
}

const holdReasonNames: Record<string, string> = {
  contract_next_price_missing: '合約到期後缺少後續價格，續約已暫停。',
  contract_partial_period_requires_review: '合約無法涵蓋完整的下一帳期，續約需要人工審核。',
  worker_missed_grace: '續約工作已超過允許期限，需要人工審核。',
  immediate_change_unresolved: '立即變更尚未解決，續約已暫停。',
  price_migration_paused: '價格遷移已暫停，續約已暫停。',
  usage_credit_note_required: '待處理的用量減額尚未產生 Credit Note，續約已暫停。',
}

function holdReasonText(reason: string) { return holdReasonNames[reason] ?? `續約已暫停：${reason}` }

const eventNames: Record<string, string> = {
  period_started: '帳期開始',
  invoice_finalized: '帳單核定',
  payment_created: '付款操作建立',
  payment_retry_created: '付款重試建立',
  provider_succeeded: '付款服務商確認成功',
  provider_definitively_failed: '付款服務商確認失敗',
  provider_unknown: '付款服務商結果待查',
  schedule_created: '排程建立',
  schedule_applied: '排程套用',
  schedule_planned: '排程預計生效',
  entitlement_updated: '權益更新',
  renewal_hold: '續約暫停',
  subscription_ended: '訂閱結束',
  contract_transition: '合約轉換',
  immediate_change_requested: '立即變更請求',
  immediate_change_activated: '立即變更啟用',
  quote_accepted: '報價接受',
  contract_quote_accepted: '合約報價接受',
}

function HistoryPager({ page, next, onPrevious, onNext }: { page: number; next: string; onPrevious: () => void; onNext: () => void }) {
  return <Space wrap className="result-card">
    <Button disabled={page === 0} onClick={onPrevious}>上一頁</Button>
    <Typography.Text>第 {page + 1} 頁</Typography.Text>
    <Button disabled={!next} onClick={onNext}>下一頁</Button>
  </Space>
}

function SubscriptionHistory({ id }: { id: string }) {
  const navigate = useNavigate()
  const [periodCursors, setPeriodCursors] = useState([''])
  const [periodPage, setPeriodPage] = useState(0)
  const [timelineCursors, setTimelineCursors] = useState([''])
  const [timelinePage, setTimelinePage] = useState(0)
  const periods = useQuery({ queryKey: ['subscription-periods', id, periodCursors[periodPage]], queryFn: () => api.subscriptionPeriods(id, periodCursors[periodPage]) })
  const timeline = useQuery({ queryKey: ['subscription-timeline', id, timelineCursors[timelinePage]], queryFn: () => api.subscriptionTimeline(id, timelineCursors[timelinePage]) })
  const entitlement = useQuery({ queryKey: ['subscription-entitlement', id], queryFn: () => api.subscriptionEntitlement(id) })
  return <>
    <Card title="帳期歷史" className="result-card" extra={<Button onClick={() => void periods.refetch()}>重新整理</Button>}>
      {periods.isPending ? <Skeleton active /> : periods.isError ? <Alert type="error" showIcon message="帳期歷史載入失敗" description={<Button onClick={() => void periods.refetch()}>重試</Button>} /> : <>
        <Table<SubscriptionPeriod> rowKey="Index" dataSource={periods.data.items} pagination={false} scroll={{ x: 'max-content' }} locale={{ emptyText: <Empty description="尚無帳期" /> }} columns={[
          { title: '序號', dataIndex: 'Index', render: (value: string) => <Button type="link" onClick={() => navigate(`/usage-periods/${encodeURIComponent(id)}/${encodeURIComponent(value)}`)}>{value} · 用量詳情</Button> },
          { title: '開始', dataIndex: 'Start', render: dateText },
          { title: '結束', dataIndex: 'End', render: dateText },
          { title: '付款到期', dataIndex: 'DueAt', render: dateText },
          { title: '帳單', dataIndex: 'InvoiceID', render: (invoiceID: string) => <Button type="link" href={`/admin/invoices/${encodeURIComponent(invoiceID)}`}>{invoiceID}</Button> },
        ]} />
        <HistoryPager page={periodPage} next={periods.data.next_cursor} onPrevious={() => setPeriodPage(periodPage - 1)} onNext={() => { setPeriodCursors([...periodCursors.slice(0, periodPage + 1), periods.data.next_cursor]); setPeriodPage(periodPage + 1) }} />
      </>}
    </Card>
    <Card title="事件時間軸" className="result-card" extra={<Button onClick={() => void timeline.refetch()}>重新整理</Button>}>
      <Typography.Paragraph type="secondary">依記錄時間排序；顯示順序不代表跨系統因果順序。</Typography.Paragraph>
      {timeline.isPending ? <Skeleton active /> : timeline.isError ? <Alert type="error" showIcon message="時間軸載入失敗" description={<Button onClick={() => void timeline.refetch()}>重試</Button>} /> : <>
        {timeline.data.items.length === 0 ? <Empty description="尚無事件" /> : <Timeline items={timeline.data.items.map((event) => ({
          children: <Space direction="vertical" size={0}>
            <Typography.Text strong>{eventNames[event.Kind] ?? event.Kind} <Tag>{event.Status}</Tag></Typography.Text>
            <Typography.Text type="secondary">{dateText(event.At)}</Typography.Text>
            {event.Reference && <Typography.Text copyable>來源：{event.Reference}</Typography.Text>}
          </Space>,
        }))} />}
        <HistoryPager page={timelinePage} next={timeline.data.next_cursor} onPrevious={() => setTimelinePage(timelinePage - 1)} onNext={() => { setTimelineCursors([...timelineCursors.slice(0, timelinePage + 1), timeline.data.next_cursor]); setTimelinePage(timelinePage + 1) }} />
      </>}
    </Card>
    <Card title="權益來源" className="result-card" extra={<Button onClick={() => void entitlement.refetch()}>重新整理</Button>}>
      {entitlement.isPending ? <Skeleton active /> : entitlement.isError ? <Alert type="error" showIcon message="權益來源載入失敗" description={<Button onClick={() => void entitlement.refetch()}>重試</Button>} /> : entitlement.data.entitlement ? <Descriptions bordered size="small" column={1} items={[
        { key: 'status', label: '狀態', children: <Tag>{entitlement.data.entitlement.Status}</Tag> },
        { key: 'reason', label: '原因', children: entitlement.data.entitlement.Reason || '無' },
        { key: 'operation', label: '來源操作', children: entitlement.data.entitlement.SourceOperationID || '無' },
        { key: 'invoice', label: '來源帳單', children: entitlement.data.entitlement.SourceInvoiceID || '無' },
        { key: 'revision', label: '來源 Revision', children: entitlement.data.entitlement.SourceRevision },
        { key: 'updated', label: '更新時間', children: dateText(entitlement.data.entitlement.UpdatedAt) },
        { key: 'grace', label: '寬限截止', children: entitlement.data.entitlement.GraceDeadline ? dateText(entitlement.data.entitlement.GraceDeadline) : '無' },
      ]} /> : <Empty description="權益尚未投影" />}
    </Card>
  </>
}

export default function SubscriptionDetail() {
  const { id = '' } = useParams()
  const navigate = useNavigate()
  const query = useQuery({ queryKey: ['subscription', id], queryFn: () => api.subscription(id), enabled: id !== '' })
  if (query.isPending) return <Skeleton active />
  if (query.isError && (!query.data || !canShowStaleRead(query.error))) {
    const status = query.error instanceof HttpError ? query.error.status : 0
    return <Result status={status === 404 ? '404' : status === 403 ? '403' : 'error'} title={status === 404 ? '找不到訂閱' : status === 403 ? '沒有權限查看訂閱' : '訂閱無法載入'} subTitle={query.error.message} extra={<Button onClick={() => void query.refetch()}>重試</Button>} />
  }
  if (!query.data) return null
  const subscription = query.data
  const baseline = (BigInt(subscription.ActualFixedMinor) + BigInt(subscription.ActualSeatMinor) * BigInt(subscription.SeatQuantity)).toString()
  return <div className="form-page">
    <Space align="center" wrap>
      <Typography.Title level={2} style={{ margin: 0 }}>訂閱詳情</Typography.Title>
      <Tag>{subscription.Status}</Tag>
      {query.isFetching && <Tag>更新中</Tag>}
      <Button onClick={() => void query.refetch()} loading={query.isFetching}>更新訂閱</Button>
    </Space>
    {query.isError && <Alert type="warning" showIcon className="result-card" message="無法更新訂閱；以下是上次成功讀取的資料" description={<Space wrap><span>上次讀取：{new Date(query.dataUpdatedAt).toLocaleString()}</span><Button onClick={() => void query.refetch()}>重試</Button></Space>} />}
    <Typography.Paragraph type="secondary">資料查詢時間：{new Date(query.dataUpdatedAt).toLocaleString()}。金額為固定費與席次費基準額，未包含用量或更正。</Typography.Paragraph>
    {subscription.HoldReason && <Alert type="warning" showIcon className="form-alert" message="續約需要處理" description={<Space direction="vertical"><span>{holdReasonText(subscription.HoldReason)}</span><Typography.Text code>{subscription.HoldReason}</Typography.Text></Space>} />}
    <Card title="目前狀態" className="result-card">
      <Descriptions bordered size="small" column={1} items={[
        { key: 'id', label: '訂閱 ID', children: <Typography.Text copyable>{subscription.ID}</Typography.Text> },
        { key: 'customer', label: '客戶 ID', children: <Button type="link" onClick={() => navigate(`/customers/${encodeURIComponent(subscription.CustomerID)}`)}>{subscription.CustomerID}</Button> },
        { key: 'price', label: '實際價格版本', children: subscription.PriceVersionID },
        { key: 'plan', label: '方案', children: subscription.PricePlanID },
        { key: 'contract', label: '合約版本', children: subscription.ContractVersionID || '無' },
        { key: 'seats', label: '席次', children: subscription.SeatQuantity },
        { key: 'fixed', label: '固定費', children: <Money minor={subscription.ActualFixedMinor} currency={subscription.Currency} /> },
        { key: 'seat', label: '每席費', children: <Money minor={subscription.ActualSeatMinor} currency={subscription.Currency} /> },
        { key: 'baseline', label: '固定＋席次基準額', children: <Money minor={baseline} currency={subscription.Currency} /> },
        { key: 'revision', label: 'Revision', children: subscription.Revision },
        { key: 'entitlement', label: '權益狀態', children: <Tag>{subscription.EntitlementStatus || '尚未投影'}</Tag> },
        { key: 'entitlement_reason', label: '權益原因', children: subscription.EntitlementReason || '無' },
        { key: 'entitlement_revision', label: '權益來源 Revision', children: subscription.EntitlementSourceRevision },
      ]} />
    </Card>
    <Card title="當前帳期" className="result-card">
      {subscription.CurrentPeriod ? <Descriptions bordered size="small" column={1} items={[
        { key: 'index', label: '帳期序號', children: <Button type="link" onClick={() => navigate(`/usage-periods/${encodeURIComponent(id)}/${encodeURIComponent(subscription.CurrentPeriod!.Index)}`)}>{subscription.CurrentPeriod.Index} · 用量詳情</Button> },
        { key: 'start', label: '開始', children: dateText(subscription.CurrentPeriod.Start) },
        { key: 'end', label: '結束', children: dateText(subscription.CurrentPeriod.End) },
        { key: 'due', label: '付款到期', children: dateText(subscription.CurrentPeriod.DueAt) },
        { key: 'invoice', label: '帳單 ID', children: <Typography.Text copyable>{subscription.CurrentPeriod.InvoiceID}</Typography.Text> },
      ]} /> : <Typography.Text type="secondary">尚無帳期</Typography.Text>}
    </Card>
    <Card title="下期安排" className="result-card">
      <Descriptions bordered size="small" column={1} items={[
        { key: 'change', label: '價格與席次變更', children: subscription.ScheduledChange ? `${subscription.ScheduledChange.TargetPriceVersionID}／${subscription.ScheduledChange.SeatQuantity} 席，${dateText(subscription.ScheduledChange.EffectiveAt)}` : '無' },
        { key: 'cancel', label: '取消排程', children: subscription.ScheduledCancel ? dateText(subscription.ScheduledCancel.EffectiveAt) : '無' },
        { key: 'hold', label: '續約阻擋原因', children: subscription.HoldReason ? holdReasonText(subscription.HoldReason) : '無' },
      ]} />
    </Card>
    <SubscriptionHistory id={id} />
    {subscription.Status === 'active' && <Space wrap className="result-card">
      <Button disabled={query.isError} onClick={() => navigate(`/subscriptions/${encodeURIComponent(id)}/schedule-plan`)}>排程下期變更</Button>
      <Button disabled={query.isError} onClick={() => navigate(`/subscriptions/${encodeURIComponent(id)}/upgrade`)}>立即升級</Button>
      <Button disabled={query.isError} onClick={() => navigate(`/subscriptions/${encodeURIComponent(id)}/cancel`)}>{subscription.ScheduledCancel ? '恢復取消排程' : '排程取消'}</Button>
    </Space>}
  </div>
}
