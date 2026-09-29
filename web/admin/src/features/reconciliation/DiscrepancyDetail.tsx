import { useQuery } from '@tanstack/react-query'
import { Alert, Button, Card, Descriptions, Empty, Result, Skeleton, Space, Table, Tag, Typography } from 'antd'
import { useNavigate, useParams } from 'react-router-dom'
import { api, canShowStaleRead, HttpError } from '../../api/client'

function dateText(value?: string | null) {
  return value ? new Date(value).toLocaleString() : '無'
}

function recordedText(value: string) {
  return <pre style={{ margin: 0, whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{value || '無'}</pre>
}

export default function DiscrepancyDetail() {
  const { id = '' } = useParams()
  const navigate = useNavigate()
  const query = useQuery({ queryKey: ['discrepancy', id], queryFn: () => api.discrepancy(id), enabled: id !== '' })
  if (query.isPending) return <Skeleton active />
  if (query.isError && (!query.data || !canShowStaleRead(query.error))) {
    const status = query.error instanceof HttpError ? query.error.status : 0
    return <Result status={status === 404 ? '404' : status === 403 ? '403' : 'error'} title={status === 404 ? '找不到對帳差異' : status === 403 ? '沒有權限查看對帳差異' : '對帳差異無法載入'} subTitle={query.error.message} extra={<Button onClick={() => void query.refetch()}>重試</Button>} />
  }
  if (!query.data) return null
  const detail = query.data.discrepancy
  const finding = detail.Discrepancy
  return <div className="form-page">
    <Space align="center" wrap>
      <Typography.Title level={2} style={{ margin: 0 }}>對帳差異詳情</Typography.Title>
      <Tag>{finding.Status}</Tag>
      <Tag>{finding.Classification}</Tag>
      {query.isFetching && <Tag>更新中</Tag>}
    </Space>
    {query.isError && <Alert type="warning" showIcon className="result-card" message="無法更新對帳差異；以下是上次成功讀取的資料" description={<Space wrap><span>上次讀取：{new Date(query.dataUpdatedAt).toLocaleString()}</span><Button onClick={() => void query.refetch()}>重試</Button></Space>} />}
    <Typography.Paragraph type="secondary">資料查詢時間：{dateText(query.data.observed_at)}。expected、actual 與證據是對帳時的記錄；修復或人工決議後仍需核對最新事實。</Typography.Paragraph>
    <Card title="差異與證據" className="result-card" extra={<Button onClick={() => void query.refetch()}>重新整理</Button>}>
      <Descriptions bordered size="small" column={1} items={[
        { key: 'id', label: '差異 ID', children: <Typography.Text copyable>{finding.ID}</Typography.Text> },
        { key: 'kind', label: '類型', children: finding.Kind },
        { key: 'object', label: '對象 ID', children: <Typography.Text copyable>{finding.ObjectID}</Typography.Text> },
        { key: 'class', label: '分類', children: finding.Classification },
        { key: 'revision', label: '來源 Revision', children: finding.SourceRevision },
        { key: 'expected', label: 'Expected', children: recordedText(finding.Expected) },
        { key: 'actual', label: 'Actual', children: recordedText(finding.Actual) },
        { key: 'evidence', label: '證據', children: recordedText(finding.Evidence) },
        { key: 'first', label: '首次發現', children: dateText(detail.FirstSeenAt) },
        { key: 'last', label: '最近發現', children: dateText(detail.LastSeenAt) },
        { key: 'resolution', label: '處理記錄', children: detail.Resolution || '尚未處理' },
      ]} />
    </Card>
    {finding.Status !== 'resolved' && <Space wrap className="result-card">
      <Button disabled={query.isError} onClick={() => navigate(`/discrepancies/${encodeURIComponent(id)}/repair`, { state: { source_revision: String(finding.SourceRevision), evidence: finding.Evidence } })}>規劃修復</Button>
      <Button disabled={query.isError} onClick={() => navigate(`/discrepancies/${encodeURIComponent(id)}/manual-decisions`)}>記錄人工決議</Button>
    </Space>}
    <Card title="發現此差異的對帳執行" className="result-card">
      {detail.RunsTruncated && <Alert type="warning" showIcon message="僅顯示最近 100 次對帳執行" />}
      <Table rowKey="ID" dataSource={detail.Runs} pagination={false} scroll={{ x: 'max-content' }} locale={{ emptyText: <Empty description="尚無執行記錄" /> }} columns={[
        { title: '執行 ID', dataIndex: 'ID', render: (value: string) => <Button type="link" onClick={() => navigate(`/reconciliation-runs/${encodeURIComponent(value)}`)}>{value}</Button> },
        { title: '截止時間', dataIndex: 'CutoffAt', render: dateText },
        { title: '本地觀測', dataIndex: 'LocalObservedAt', render: dateText },
        { title: '服務商觀測', dataIndex: 'ProviderObservedAt', render: dateText },
      ]} />
    </Card>
    <Card title="修復操作" className="result-card">
      {detail.RepairsTruncated && <Alert type="warning" showIcon message="僅顯示最近 100 筆修復操作" />}
      <Table rowKey="ID" dataSource={detail.Repairs} pagination={false} scroll={{ x: 'max-content' }} locale={{ emptyText: <Empty description="尚無修復操作" /> }} columns={[
        { title: '操作 ID', dataIndex: 'ID', render: (value: string) => <Typography.Text copyable>{value}</Typography.Text> },
        { title: '動作', dataIndex: 'Action' },
        { title: '前置 Revision', dataIndex: 'PreconditionRevision' },
        { title: '狀態', dataIndex: 'Status', render: (value: string) => <Tag>{value}</Tag> },
        { title: '查證結果', dataIndex: 'Verification' },
        { title: '建立時間', dataIndex: 'CreatedAt', render: dateText },
      ]} />
    </Card>
    <Card title="人工決議" className="result-card">
      <Typography.Paragraph type="secondary">人工決議是審核記錄，不代表資金已調整。</Typography.Paragraph>
      {detail.DecisionsTruncated && <Alert type="warning" showIcon message="僅顯示最近 100 筆人工決議" />}
      <Table rowKey="ID" dataSource={detail.Decisions} pagination={false} scroll={{ x: 'max-content' }} locale={{ emptyText: <Empty description="尚無人工決議" /> }} columns={[
        { title: '決議 ID', dataIndex: 'ID', render: (value: string) => <Typography.Text copyable>{value}</Typography.Text> },
        { title: '審核者', dataIndex: 'Reviewer' },
        { title: '決定', dataIndex: 'Decision' },
        { title: '原因', dataIndex: 'Reason' },
        { title: '記錄時間', dataIndex: 'CreatedAt', render: dateText },
      ]} />
    </Card>
  </div>
}
