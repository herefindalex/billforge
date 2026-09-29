import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Alert, Button, Card, Descriptions, Result, Select, Skeleton, Space, Table, Tag, Typography } from 'antd'
import { useNavigate, useParams } from 'react-router-dom'
import { api, type MigrationItem } from '../../api/client'

const reasons: Record<string, string> = {
  scheduled_change: '已有下期變更排程',
  source_price_changed: '目前價格已不同於預覽來源',
  seat_quantity_changed: '席位數已變更',
  revision_changed: '訂閱 revision 已變更',
  subscription_inactive: '訂閱不再有效',
}

function reasonText(item: MigrationItem): string {
  if (item.ConflictReason) return reasons[item.ConflictReason] ?? item.ConflictReason
  return item.Status === 'conflicted' ? '舊資料未記錄原因，請檢查訂閱現況' : '—'
}

export default function PriceMigrationDetail() {
  const { id = '' } = useParams()
  const navigate = useNavigate()
  const [status, setStatus] = useState('')
  const [cursors, setCursors] = useState([''])
  const [page, setPage] = useState(0)
  const migration = useQuery({ queryKey: ['migration', id], queryFn: () => api.migration(id), enabled: id !== '' })
  const items = useQuery({
    queryKey: ['migration-items', id, status, cursors[page]],
    queryFn: () => api.migrationItems(id, status, cursors[page]),
    enabled: id !== '' && migration.isSuccess,
  })

  if (migration.isPending) return <Skeleton active className="form-page" />
  if (migration.isError || !migration.data) return <Result status="error" title="遷移批次無法載入" subTitle={migration.error?.message} extra={<Button onClick={() => void migration.refetch()}>重試</Button>} />

  const detail = migration.data
  const hasConflicts = detail.ConflictedCount !== '0'
  const refresh = () => {
    setCursors([''])
    setPage(0)
    void migration.refetch()
    void items.refetch()
  }
  return <div className="form-page">
    <Typography.Title level={2}>價格遷移批次</Typography.Title>
    <Card>
      <Descriptions bordered size="small" column={1} items={[
        { key: 'id', label: '批次 ID', children: <Typography.Text copyable>{detail.ID}</Typography.Text> },
        { key: 'cohort', label: 'Cohort', children: detail.Cohort },
        { key: 'target', label: '目標價格版本', children: detail.TargetPriceVersionID },
        { key: 'status', label: '狀態', children: <Tag>{detail.Status}</Tag> },
        { key: 'items', label: '訂閱數', children: detail.ItemCount },
        { key: 'pending', label: '待處理', children: detail.PendingCount },
        { key: 'applied', label: '已套用', children: detail.AppliedCount },
        { key: 'conflicted', label: '衝突', children: detail.ConflictedCount },
        { key: 'skipped', label: '已略過', children: detail.SkippedCount },
      ]} />
      <Space wrap className="result-card">
        <Button onClick={refresh}>更新狀態</Button>
        {detail.Status === 'active' && <Button onClick={() => navigate(`/price-migrations/${encodeURIComponent(id)}/pause`)}>暫停未完成項目</Button>}
        {detail.Status === 'paused' && <Button onClick={() => navigate(`/price-migrations/${encodeURIComponent(id)}/skip`)}>略過項目</Button>}
        {detail.Status === 'paused' && !hasConflicts && <Button onClick={() => navigate(`/price-migrations/${encodeURIComponent(id)}/resume`)}>恢復批次</Button>}
      </Space>
    </Card>
    {hasConflicts && <Alert className="result-card" type="warning" showIcon message={`${detail.ConflictedCount} 筆訂閱與遷移預覽衝突`} description="已套用項目不會撤銷。請檢查各項原因，再明確略過衝突項目。" />}
    <Card title="逐項結果" className="result-card" extra={<Space><Select aria-label="篩選遷移項目狀態" value={status} style={{ width: 140 }} options={[
      { value: '', label: '全部狀態' },
      { value: 'pending', label: '待處理' },
      { value: 'applied', label: '已套用' },
      { value: 'conflicted', label: '衝突' },
      { value: 'skipped', label: '已略過' },
    ]} onChange={(value) => { setStatus(value); setCursors(['']); setPage(0) }} /><Button onClick={refresh}>重新整理</Button></Space>}>
      {items.isPending ? <Skeleton active /> : items.isError ? <Result status="error" title="遷移項目無法載入" subTitle={items.error.message} extra={<Button onClick={() => void items.refetch()}>重試</Button>} /> : <>
        <Table<MigrationItem>
          rowKey="SubscriptionID"
          dataSource={items.data.items}
          pagination={false}
          scroll={{ x: 1040 }}
          columns={[
            { title: '訂閱', dataIndex: 'SubscriptionID', key: 'subscription', render: (value: string) => <Button type="link" onClick={() => navigate(`/subscriptions/${encodeURIComponent(value)}`)}>{value}</Button> },
            { title: '原價格', dataIndex: 'FromPriceVersionID', key: 'from' },
            { title: '目標價格', dataIndex: 'TargetPriceVersionID', key: 'target' },
            { title: '預期 revision', dataIndex: 'ExpectedRevision', key: 'revision' },
            { title: '原金額（最小單位）', dataIndex: 'PriorAmountMinor', key: 'prior' },
            { title: '目標金額（最小單位）', dataIndex: 'TargetAmountMinor', key: 'amount' },
            { title: '生效時間', dataIndex: 'EffectiveAt', key: 'effective' },
            { title: '狀態', dataIndex: 'Status', key: 'status', render: (value: string) => <Tag color={value === 'conflicted' ? 'orange' : value === 'applied' ? 'green' : undefined}>{value}</Tag> },
            { title: '原因', key: 'reason', render: (_, item) => reasonText(item) },
          ]}
        />
        <Space wrap className="result-card">
          <Button disabled={page === 0} onClick={() => setPage(page - 1)}>上一頁</Button>
          <Typography.Text>第 {page + 1} 頁</Typography.Text>
          <Button disabled={!items.data.next_cursor} onClick={() => { setCursors([...cursors.slice(0, page + 1), items.data.next_cursor]); setPage(page + 1) }}>下一頁</Button>
          <Typography.Text type="secondary">觀測時間：{items.data.observed_at}</Typography.Text>
        </Space>
      </>}
    </Card>
  </div>
}
