import { useMutation, useQuery } from '@tanstack/react-query'
import { Alert, Button, Card, Descriptions, Result, Skeleton, Space, Tag, Typography } from 'antd'
import { useNavigate, useParams } from 'react-router-dom'
import { api, type Session } from '../../api/client'
import ExternalOperationOutcome from '../../components/ExternalOperationOutcome'

const jobActions = new Set(['C13', 'C30', 'C32', 'C44', 'C45'])

export default function CommandDetail({ session }: { session: Session }) {
  const { id = '' } = useParams()
  const navigate = useNavigate()
  const command = useQuery({
    queryKey: ['command', id],
    queryFn: () => api.command(id),
    enabled: !!id,
  refetchInterval: (query) => {
    if (query.state.data?.error_code === 'PERMISSION_REVOKED_REVIEW') return false
    const status = query.state.data?.status
      return status === 'accepted' || status === 'running' ? 1500 : status === 'waiting_verification' ? 5000 : false
    },
  })
  const resume = useMutation({
    mutationFn: () => api.resumeCommand(session.csrf_token, id),
    onSuccess: () => { void command.refetch() },
  })

  if (command.isPending) return <Skeleton active />
  if (command.isError && !command.data) return <Result status="error" title="命令狀態無法載入" subTitle={command.error.message} extra={<Button onClick={() => void command.refetch()}>重試</Button>} />

  const item = command.data
  return <Card title="命令詳情" extra={<Button onClick={() => void command.refetch()}>更新</Button>}>
    {command.isError && <Alert type="warning" showIcon className="result-card" message="無法更新命令狀態；以下是上次成功讀取的資料" description={<Space wrap><span>上次讀取：{new Date(command.dataUpdatedAt).toLocaleString()}</span><Button onClick={() => void command.refetch()}>重試</Button></Space>} />}
    <ExternalOperationOutcome command={item} />
    <Descriptions bordered size="small" column={1} items={[
      { key: 'id', label: '命令 ID', children: <Typography.Text copyable>{item.id}</Typography.Text> },
      { key: 'request', label: '受理請求 ID', children: item.request_id ? <Typography.Text copyable>{item.request_id}</Typography.Text> : '歷史資料未記錄' },
      { key: 'action', label: '操作', children: item.action_id },
      { key: 'target', label: '對象', children: item.target_id || '—' },
      { key: 'status', label: '狀態', children: <Tag>{item.status}</Tag> },
      { key: 'result', label: '結果參照', children: <pre>{JSON.stringify(item.result_refs ?? {}, null, 2)}</pre> },
      { key: 'error', label: '錯誤', children: item.error_code || '無' },
      { key: 'created', label: '建立時間', children: new Date(item.created_at).toLocaleString() },
      { key: 'updated', label: '更新時間', children: new Date(item.updated_at).toLocaleString() },
    ]} />
    <Space className="result-card" wrap>
      {item.status === 'accepted' && item.error_code !== 'PERMISSION_REVOKED_REVIEW' && <Button type="primary" onClick={() => resume.mutate()} loading={resume.isPending} disabled={command.isError}>繼續原命令</Button>}
      {item.status === 'waiting_verification' && <Button type="primary" onClick={() => resume.mutate()} loading={resume.isPending} disabled={command.isError}>重新查證</Button>}
      {item.error_code === 'PERMISSION_REVOKED_REVIEW' && <Button type="primary" onClick={() => resume.mutate()} loading={resume.isPending} disabled={command.isError}>檢查既有收據</Button>}
      {jobActions.has(item.action_id) && <Button onClick={() => navigate(`/jobs/${encodeURIComponent(`job:${item.id}`)}`)}>查看逐項進度</Button>}
      <Button onClick={() => navigate('/commands')}>返回命令列表</Button>
    </Space>
    {item.error_code === 'PERMISSION_REVOKED_REVIEW' && <Alert type="warning" showIcon className="result-card" message="權限已變更，命令暫停待查證" description="可以檢查原命令是否已有收據；檢查不會啟動新操作。若仍無收據，需恢復操作權限後重啟服務才能繼續。" />}
    {resume.isError && <Alert type="error" showIcon className="result-card" message="目前無法查證，請稍後重試" description={resume.error.message} />}
  </Card>
}
