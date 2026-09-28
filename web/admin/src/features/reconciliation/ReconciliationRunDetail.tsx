import { useQuery } from '@tanstack/react-query'
import { Alert, Button, Card, Descriptions, Empty, Result, Skeleton, Space, Table, Tag, Typography } from 'antd'
import { useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { api, HttpError, type ReconciliationRunPage } from '../../api/client'

function dateText(value: string) {
  return new Date(value).toLocaleString()
}

function evidenceText(value: string) {
  return <pre style={{ margin: 0, whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{value || '無'}</pre>
}

export default function ReconciliationRunDetail() {
  const { id = '' } = useParams()
  const navigate = useNavigate()
  const [cursors, setCursors] = useState([''])
  const [pageNumber, setPageNumber] = useState(0)
  const cursor = cursors[pageNumber]
  const query = useQuery({ queryKey: ['reconciliation-run', id, cursor], queryFn: () => api.reconciliationRun(id, cursor), enabled: id !== '' })
  if (query.isPending) return <Skeleton active />
  if (query.isError) {
    const status = query.error instanceof HttpError ? query.error.status : 0
    return <Result status={status === 404 ? '404' : status === 403 ? '403' : 'error'} title={status === 404 ? '找不到對帳執行' : status === 403 ? '沒有權限查看對帳執行' : '對帳執行無法載入'} subTitle={query.error.message} extra={<Button onClick={() => void query.refetch()}>重試</Button>} />
  }
  const page: ReconciliationRunPage = query.data.page
  return <div className="form-page">
    <Space align="center" wrap>
      <Typography.Title level={2} style={{ margin: 0 }}>對帳執行詳情</Typography.Title>
      <Tag>{page.Run.FindingCount} 項差異</Tag>
      {query.isFetching && <Tag>更新中</Tag>}
    </Space>
    <Typography.Paragraph type="secondary">資料查詢時間：{dateText(query.data.observed_at)}。下列 expected／actual 是當次執行保存的記錄；目前狀態另行顯示。</Typography.Paragraph>
    <Card title="執行範圍" className="result-card" extra={<Button onClick={() => void query.refetch()}>重新整理</Button>}>
      <Descriptions bordered size="small" column={1} items={[
        { key: 'id', label: '執行 ID', children: <Typography.Text copyable>{page.Run.ID}</Typography.Text> },
        { key: 'cutoff', label: '核對截止', children: dateText(page.Run.CutoffAt) },
        { key: 'local', label: '本地觀測', children: dateText(page.Run.LocalObservedAt) },
        { key: 'provider', label: '服務商觀測', children: dateText(page.Run.ProviderObservedAt) },
        { key: 'count', label: '差異數', children: page.Run.FindingCount },
      ]} />
    </Card>
    <Card title="逐項差異" className="result-card">
      {page.Findings.some((item) => item.SnapshotQuality === 'backfilled_current') && <Alert type="warning" showIcon className="form-alert" message="部分舊執行只有補錄值" description="這些列以既有差異的當前值補錄，無法證明當次執行看到的原始 expected／actual。" />}
      <Table rowKey="DiscrepancyID" dataSource={page.Findings} pagination={false} scroll={{ x: 'max-content' }} locale={{ emptyText: <Empty description="本次沒有差異" /> }} columns={[
        { title: '差異 ID', dataIndex: 'DiscrepancyID', render: (value: string) => <Button type="link" onClick={() => navigate(`/discrepancies/${encodeURIComponent(value)}`)}>{value}</Button> },
        { title: '類型', dataIndex: 'Kind' },
        { title: '對象', dataIndex: 'ObjectID' },
        { title: '分類', dataIndex: 'Classification' },
        { title: '目前狀態', dataIndex: 'CurrentStatus', render: (value: string) => <Tag>{value}</Tag> },
        { title: '記錄品質', dataIndex: 'SnapshotQuality', render: (value: string) => value === 'recorded' ? '原始記錄' : '舊資料補錄' },
      ]} expandable={{ expandedRowRender: (item) => <Descriptions bordered size="small" column={1} items={[
        { key: 'expected', label: 'Expected', children: evidenceText(item.Expected) },
        { key: 'actual', label: 'Actual', children: evidenceText(item.Actual) },
        { key: 'evidence', label: '證據', children: evidenceText(item.Evidence) },
        { key: 'revision', label: '來源 Revision', children: item.SourceRevision },
      ]} /> }} />
      <Space wrap className="result-card">
        <Button disabled={pageNumber === 0} onClick={() => setPageNumber(pageNumber - 1)}>上一頁</Button>
        <Typography.Text>第 {pageNumber + 1} 頁</Typography.Text>
        <Button disabled={!query.data.next_cursor} onClick={() => { setCursors([...cursors.slice(0, pageNumber + 1), query.data.next_cursor]); setPageNumber(pageNumber + 1) }}>下一頁</Button>
      </Space>
    </Card>
  </div>
}
