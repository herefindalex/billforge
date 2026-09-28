import { useQuery } from '@tanstack/react-query'
import { Alert, Button, Card, Result, Skeleton, Space, Table, Tag, Typography } from 'antd'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { api, type JobSummary } from '../../api/client'

export default function JobList() {
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const cursor = searchParams.get('cursor') ?? ''
  const back = searchParams.getAll('back')
  const query = useQuery({ queryKey: ['jobs', cursor], queryFn: () => api.jobs(cursor) })

  const previousPage = () => {
    const history = [...back]
    const previous = history.pop()
    const next = new URLSearchParams()
    for (const entry of history) next.append('back', entry)
    if (previous) next.set('cursor', previous)
    setSearchParams(next)
  }

  const nextPage = () => {
    if (!query.data?.next_cursor) return
    const next = new URLSearchParams(searchParams)
    next.append('back', cursor)
    next.set('cursor', query.data.next_cursor)
    setSearchParams(next)
  }

  if (query.isPending) return <Skeleton active />
  if (query.isError && !query.data) return <Result status="error" title="批次工作載入失敗" subTitle={query.error.message} extra={<Button onClick={() => void query.refetch()}>重試</Button>} />

  return <>
    {query.isError && <Alert type="warning" showIcon className="result-card" message="無法更新批次工作；以下是上次成功讀取的資料" description={query.error.message} />}
    <Card title="批次工作" extra={<Button onClick={() => void query.refetch()} loading={query.isFetching}>重新整理</Button>}>
      <Table rowKey="id" dataSource={query.data.items} pagination={false} scroll={{ x: 'max-content' }} columns={[
        { title: '工作 ID', dataIndex: 'id', render: (id: string) => <Typography.Text copyable>{id}</Typography.Text> },
        { title: '操作', dataIndex: 'kind' },
        { title: '狀態', dataIndex: 'status', render: (status: string) => <Tag>{status}</Tag> },
        { title: '逐項完成', render: (_, job: JobSummary) => `${job.succeeded_items} / ${job.total_items}` },
        { title: '需處理', dataIndex: 'attention_items', render: (count: string) => count === '0' ? '0' : <Tag color="warning">{count}</Tag> },
        { title: '更新時間', dataIndex: 'updated_at', render: (value: string) => new Date(value).toLocaleString() },
        { title: '', render: (_, job: JobSummary) => <Button type="link" onClick={() => navigate(`/jobs/${encodeURIComponent(job.id)}`)}>查看逐項進度</Button> },
      ]} />
    </Card>
    <Space className="result-card" wrap>
      <Button disabled={back.length === 0} onClick={previousPage}>上一頁</Button>
      <Typography.Text>第 {back.length + 1} 頁</Typography.Text>
      <Button disabled={!query.data.next_cursor} onClick={nextPage}>下一頁</Button>
      <Typography.Text type="secondary">觀測時間：{new Date(query.data.observed_at).toLocaleString()}；新增工作不會移動已讀頁的游標。</Typography.Text>
    </Space>
  </>
}
