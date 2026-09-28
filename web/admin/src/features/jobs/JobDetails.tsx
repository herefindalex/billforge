import { useQuery } from '@tanstack/react-query'
import { Alert, Button, Card, Descriptions, Space, Table, Tag, Typography } from 'antd'
import { useParams } from 'react-router-dom'
import { api, type JobItem } from '../../api/client'

function countStatuses(items: JobItem[]) {
  const counts = { succeeded: 0, failed: 0, conflicted: 0, waiting_verification: 0, skipped: 0, active: 0 }
  for (const item of items) {
    if (item.status === 'succeeded') counts.succeeded++
    else if (item.status === 'failed') counts.failed++
    else if (item.status === 'conflicted') counts.conflicted++
    else if (item.status === 'waiting_verification') counts.waiting_verification++
    else if (item.status === 'skipped') counts.skipped++
    else counts.active++
  }
  return counts
}

export default function JobDetails() {
 const { id = '' } = useParams()
 const job = useQuery({ queryKey: ['job', id], queryFn: () => api.job(id), enabled: !!id, refetchInterval: (query) => query.state.data?.status === 'running' ? 1500 : false })
  if (job.isError && !job.data) return <Alert type="error" showIcon message="無法載入工作狀態" description={<Button onClick={() => void job.refetch()}>重試</Button>} />
  const items = job.data?.items ?? []
  const counts = countStatuses(items)
  const completed = counts.succeeded + counts.failed + counts.conflicted + counts.skipped
  return <Card title="批次工作" extra={<Button onClick={() => void job.refetch()}>更新</Button>} loading={job.isLoading}>
  {job.isError && <Alert type="warning" showIcon className="result-card" message="無法更新工作進度；以下是上次成功讀取的資料" description={<Space wrap><span>上次讀取：{new Date(job.dataUpdatedAt).toLocaleString()}</span><Button onClick={() => void job.refetch()}>重試</Button></Space>} />}
  {job.data && <>
   <Descriptions column={1} bordered size="small" items={[
    { key: 'id', label: '工作 ID', children: <Typography.Text copyable>{job.data.id}</Typography.Text> },
    { key: 'command', label: '命令 ID', children: <Typography.Text copyable>{job.data.command_id}</Typography.Text> },
    { key: 'kind', label: '操作', children: job.data.kind },
    { key: 'status', label: '狀態', children: <Tag>{job.data.status}</Tag> },
    { key: 'progress', label: '已完成 / 總數', children: `${completed} / ${items.length}` },
    { key: 'success', label: '成功', children: counts.succeeded },
    { key: 'failed', label: '失敗', children: counts.failed },
    { key: 'conflicted', label: '衝突', children: counts.conflicted },
    { key: 'waiting', label: '待查證', children: counts.waiting_verification },
    { key: 'skipped', label: '略過', children: counts.skipped },
    { key: 'active', label: '執行中', children: counts.active },
   ]} />
   <Table className="result-card" rowKey="id" size="small" dataSource={job.data.items} pagination={{ pageSize: 20 }} scroll={{ x: 800 }} columns={[
    { title: '對象', dataIndex: 'target_id', key: 'target_id', render: (value: string) => <Typography.Text copyable>{value}</Typography.Text> },
    { title: '帳期', dataIndex: 'period_key', key: 'period_key' },
    { title: '狀態', dataIndex: 'status', key: 'status', render: (value: string) => <Tag>{value}</Tag> },
    { title: '錯誤', dataIndex: 'error_code', key: 'error_code', render: (value?: string) => value || '—' },
    { title: '結果', key: 'result', render: (_: unknown, row: JobItem) => row.result_refs ? <pre>{JSON.stringify(row.result_refs, null, 2)}</pre> : '—' },
   ]} />
  </>}
 </Card>
}
