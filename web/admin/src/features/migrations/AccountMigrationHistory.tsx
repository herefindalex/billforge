import { useQuery } from '@tanstack/react-query'
import { Alert, Button, Card, Descriptions, Empty, Result, Skeleton, Space, Table, Tag, Typography } from 'antd'
import { useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { api, HttpError, type AccountMigrationDetail } from '../../api/client'

function Pager({ page, next, previous, advance }: { page: number; next: string; previous: () => void; advance: () => void }) {
  return <Space wrap className="result-card">
    <Button disabled={page === 0} onClick={previous}>上一頁</Button>
    <Typography.Text>第 {page + 1} 頁</Typography.Text>
    <Button disabled={!next} onClick={advance}>下一頁</Button>
  </Space>
}

function ShadowHistory({ id }: { id: string }) {
  const navigate = useNavigate()
  const [cursors, setCursors] = useState([''])
  const [page, setPage] = useState(0)
  const query = useQuery({ queryKey: ['account-shadows', id, cursors[page]], queryFn: () => api.accountMigrationShadows(id, cursors[page]) })
  if (query.isPending) return <Skeleton active />
  if (query.isError) {
    const status = query.error instanceof HttpError ? query.error.status : 0
    return <Result status={status === 404 ? '404' : 'error'} title={status === 404 ? '找不到帳戶遷移' : 'Shadow 歷史無法載入'} subTitle={query.error.message} extra={<Button onClick={() => void query.refetch()}>重試</Button>} />
  }
  return <div className="form-page">
    <Typography.Title level={2}>Shadow 比對歷史</Typography.Title>
    <Button onClick={() => navigate(`/account-migrations/${encodeURIComponent(id)}`)}>返回帳戶遷移</Button>
    <Card className="result-card" extra={<Button onClick={() => void query.refetch()}>重新整理</Button>}>
      <Typography.Paragraph type="secondary">依觀測時間及記錄 ID 排序；新比對不會使已讀頁重複。</Typography.Paragraph>
      <Table<AccountMigrationDetail['Shadows'][number]> rowKey="ID" dataSource={query.data.items} pagination={false} scroll={{ x: 'max-content' }} locale={{ emptyText: <Empty description="尚無比對" /> }} columns={[
        { title: '記錄 ID', dataIndex: 'ID', render: (value: string) => <Typography.Text copyable>{value}</Typography.Text> },
        { title: '類型', dataIndex: 'Kind' }, { title: '對象', dataIndex: 'ObjectID' },
        { title: '一致', dataIndex: 'Matched', render: (value: boolean) => <Tag color={value ? 'success' : 'warning'}>{value ? '一致' : '不一致'}</Tag> },
        { title: '延遲', dataIndex: 'LatencyMillis', render: (value: string) => `${value} ms` },
        { title: '觀測時間', dataIndex: 'ObservedAt', render: (value: string) => new Date(value).toLocaleString() },
      ]} expandable={{ expandedRowRender: (item) => <Descriptions bordered size="small" column={1} items={[
        { key: 'expected', label: 'Expected', children: item.Expected }, { key: 'actual', label: 'Actual', children: item.Actual },
      ]} /> }} />
      <Pager page={page} next={query.data.next_cursor} previous={() => setPage(page - 1)} advance={() => { setCursors([...cursors.slice(0, page + 1), query.data.next_cursor]); setPage(page + 1) }} />
    </Card>
  </div>
}

function ProvenanceHistory({ id }: { id: string }) {
  const navigate = useNavigate()
  const [cursors, setCursors] = useState([''])
  const [page, setPage] = useState(0)
  const query = useQuery({ queryKey: ['account-provenance', id, cursors[page]], queryFn: () => api.accountMigrationProvenance(id, cursors[page]) })
  if (query.isPending) return <Skeleton active />
  if (query.isError) {
    const status = query.error instanceof HttpError ? query.error.status : 0
    return <Result status={status === 404 ? '404' : 'error'} title={status === 404 ? '找不到帳戶遷移' : '來源歷史無法載入'} subTitle={query.error.message} extra={<Button onClick={() => void query.refetch()}>重試</Button>} />
  }
  return <div className="form-page">
    <Typography.Title level={2}>既有帳務來源歷史</Typography.Title>
    <Button onClick={() => navigate(`/account-migrations/${encodeURIComponent(id)}`)}>返回帳戶遷移</Button>
    <Card className="result-card" extra={<Button onClick={() => void query.refetch()}>重新整理</Button>}>
      <Table<AccountMigrationDetail['Provenance'][number]> rowKey="LegacyInvoiceID" dataSource={query.data.items} pagination={false} scroll={{ x: 'max-content' }} locale={{ emptyText: <Empty description="尚無來源記錄" /> }} columns={[
        { title: '既有帳單', dataIndex: 'LegacyInvoiceID' },
        { title: '既有訂閱', dataIndex: 'LegacySubscriptionID' },
        { title: 'Commerce 訂閱', dataIndex: 'CommerceSubscriptionID' },
        { title: 'Commerce 帳單', dataIndex: 'CommerceInvoiceID' },
        { title: '價格版本', dataIndex: 'PriceVersionID' },
        { title: '狀態', dataIndex: 'Status', render: (value: string) => <Tag>{value}</Tag> },
        { title: '證據', dataIndex: 'Evidence' },
        { title: '操作', render: (_, item) => item.Status === 'manual_review' && <Button type="link" onClick={() => navigate(`/account-migrations/${encodeURIComponent(id)}/provenance/${encodeURIComponent(item.LegacyInvoiceID)}/resolve`)}>處理來源</Button> },
      ]} />
      <Pager page={page} next={query.data.next_cursor} previous={() => setPage(page - 1)} advance={() => { setCursors([...cursors.slice(0, page + 1), query.data.next_cursor]); setPage(page + 1) }} />
      {query.isFetching && <Alert type="info" showIcon message="更新中" />}
    </Card>
  </div>
}

export default function AccountMigrationHistory({ kind }: { kind: 'shadows' | 'provenance' }) {
  const { id = '' } = useParams()
  return kind === 'shadows' ? <ShadowHistory id={id} /> : <ProvenanceHistory id={id} />
}
