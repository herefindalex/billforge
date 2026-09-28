import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Alert, App as AntApp, Button, Card, Descriptions, Skeleton, Space, Typography } from 'antd'
import { useParams } from 'react-router-dom'
import { api, type Command, type Session } from '../../api/client'
import ReadFailureWithRecovery from '../../components/ReadFailureWithRecovery'
import { useStoredCommandID } from '../commands/useStoredCommandID'
import CommandReadRecovery, { isCommandNotFound } from '../commands/CommandReadRecovery'

function storedKey(id: string) { return `billforge:admin:pause-migration:${id}` }

export default function PauseMigration({ session }: { session: Session }) {
  const { id = '' } = useParams()
  const { modal } = AntApp.useApp()
  const queryClient = useQueryClient()
  const [pendingKey, setPendingKey] = useState<string | null>(() => sessionStorage.getItem(storedKey(id)))
  const [commandID, setCommandID] = useStoredCommandID(session.actor_id, 'C23', id)
  const migration = useQuery({ queryKey: ['migration', id], queryFn: () => api.migration(id), enabled: id !== '' })
  const command = useQuery<Command>({
    queryKey: ['command', commandID], queryFn: () => api.command(commandID!), enabled: commandID !== null,
    refetchInterval: (query) => query.state.data?.status === 'accepted' || query.state.data?.status === 'running' ? 1500 : false,
  })
  const submit = useMutation({
    mutationFn: (key: string) => api.submitCommand(session.csrf_token, key, { action_id: 'C23', target_id: id, payload: {} }),
    onSuccess: (result) => {
      setCommandID(result.id)
      setPendingKey(null)
      sessionStorage.removeItem(storedKey(id))
      void queryClient.invalidateQueries({ queryKey: ['migration', id] })
      void queryClient.invalidateQueries({ queryKey: ['resource', 'price-migrations'] })
      void queryClient.invalidateQueries({ queryKey: ['commands'] })
    },
  })
  const confirm = () => {
    if (!migration.data || migration.isError) return
    modal.confirm({
      title: '暫停價格遷移',
      content: <Space direction="vertical"><span>批次：{migration.data.ID}</span><span>將停止尚未執行的項目；已完成項目不會撤銷。</span></Space>,
      okText: '確認暫停', cancelText: '返回',
      onOk: () => {
        const key = crypto.randomUUID()
        sessionStorage.setItem(storedKey(id), key)
        setPendingKey(key)
        submit.mutate(key)
      },
    })
  }
  if (migration.isPending) return <Skeleton active />
  if (migration.isError && !migration.data) return <ReadFailureWithRecovery title="遷移批次無法載入" message={migration.error.message} onRetryRead={() => { void migration.refetch() }} hasPendingCommand={pendingKey !== null} onRecoverCommand={() => { if (pendingKey) submit.mutate(pendingKey) }} recovering={submit.isPending} commandID={commandID} recoveryError={submit.isError ? submit.error.message : null} />
  return <div className="form-page">
    <Typography.Title level={2}>價格遷移批次</Typography.Title>
    {migration.isError && <Alert type="warning" showIcon className="result-card" message="無法更新遷移批次；以下是上次成功讀取的資料" description={<Space wrap><span>上次讀取：{new Date(migration.dataUpdatedAt).toLocaleString()}</span><Button onClick={() => void migration.refetch()}>重試</Button></Space>} />}
    <Card extra={<Button onClick={() => void migration.refetch()}>更新</Button>}>
      <Descriptions column={1} bordered size="small" items={[
        { key: 'id', label: '批次 ID', children: migration.data.ID },
        { key: 'cohort', label: 'Cohort', children: migration.data.Cohort },
        { key: 'target', label: '目標價格版本', children: migration.data.TargetPriceVersionID },
        { key: 'status', label: '狀態', children: migration.data.Status },
        { key: 'items', label: '項目數', children: migration.data.Items?.length ?? 0 },
      ]} />
      {migration.data.Status === 'active' && !commandID && <Button className="result-card" danger onClick={confirm} disabled={pendingKey !== null || migration.isError}>暫停未完成項目</Button>}
    </Card>
    {pendingKey && !commandID && <Alert type="warning" showIcon className="result-card" message="暫停命令的結果尚未確認" description={<Button onClick={() => submit.mutate(pendingKey)} loading={submit.isPending}>用原 request key 查詢</Button>} />}
    {submit.isError && <Alert type="error" showIcon className="result-card" message="命令結果尚未確認" description={submit.error.message} />}
    {commandID && <Card title="命令結果" className="result-card">
      {command.isPending && <Typography.Text>正在查詢命令狀態…</Typography.Text>}
      {command.isError && (!command.data || isCommandNotFound(command.error)) && <CommandReadRecovery error={command.error} onRetry={() => void command.refetch()} onClear={() => { setCommandID(null); void migration.refetch() }} />}
      {command.isError && command.data && <Alert type="warning" showIcon message="無法更新命令狀態；以下是上次成功讀取的資料" description={<Space wrap><span>上次讀取：{new Date(command.dataUpdatedAt).toLocaleString()}</span><Button onClick={() => void command.refetch()}>重試</Button></Space>} />}
      {command.data && <Descriptions column={1} bordered size="small" items={[
        { key: 'id', label: '命令 ID', children: <Typography.Text copyable>{command.data.id}</Typography.Text> },
        { key: 'status', label: '狀態', children: command.data.status },
        { key: 'error', label: '錯誤', children: command.data.error_code || '無' },
      ]} />}
      <Button className="result-card" href={`/admin/commands/${encodeURIComponent(commandID)}`}>開啟命令頁面</Button>
      {command.data?.status === 'failed' && migration.data.Status === 'active' && <Button className="result-card" disabled={command.isError} onClick={() => { setCommandID(null); void migration.refetch() }}>依最新批次狀態重新操作</Button>}
    </Card>}
  </div>
}
