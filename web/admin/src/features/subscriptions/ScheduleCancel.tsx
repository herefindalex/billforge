import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Alert, App as AntApp, Button, Card, Descriptions, Skeleton, Space, Typography } from 'antd'
import { useParams } from 'react-router-dom'
import { api, HttpError, type Command, type Preview, type Session } from '../../api/client'
import { useStoredCommandID } from '../commands/useStoredCommandID'
import CommandReadRecovery from '../commands/CommandReadRecovery'
import PreviewWarnings from '../../components/PreviewWarnings'
import ReadFailureWithRecovery from '../../components/ReadFailureWithRecovery'
import { canConfirmPreview, usePreviewExpired } from '../../components/usePreviewExpiry'

type Pending = { key: string; previewID: string; revision: string; actionID: 'C05' | 'C06' }
type StaleIntent = { actionID: 'C05' | 'C06'; revision: string; scheduledAt: string | null }
function storageKey(id: string) { return `billforge:admin:cancel:${id}` }
function loadPending(id: string): Pending | null {
  try { const value = sessionStorage.getItem(storageKey(id)); return value ? JSON.parse(value) as Pending : null } catch { return null }
}

export default function ScheduleCancel({ session }: { session: Session }) {
  const { id = '' } = useParams()
  const { modal } = AntApp.useApp()
  const queryClient = useQueryClient()
  const [preview, setPreview] = useState<Preview | null>(null)
  const previewExpired = usePreviewExpired(preview?.expires_at)
  const [staleIntent, setStaleIntent] = useState<StaleIntent | null>(null)
  const [pending, setPending] = useState<Pending | null>(() => loadPending(id))
  const [commandID, setCommandID] = useStoredCommandID(session.actor_id, 'cancel', id)
  const subscription = useQuery({ queryKey: ['subscription', id], queryFn: () => api.subscription(id), enabled: id !== '' })
  const actionID: 'C05' | 'C06' = subscription.data?.ScheduledCancel ? 'C06' : 'C05'
  const resuming = actionID === 'C06'
  const command = useQuery<Command>({
    queryKey: ['command', commandID], queryFn: () => api.command(commandID!), enabled: commandID !== null,
    refetchInterval: (query) => query.state.data?.status === 'accepted' || query.state.data?.status === 'running' ? 1500 : false,
  })
  const createPreview = useMutation({
    mutationFn: () => api.createPreview(session.csrf_token, actionID, id, { revision: subscription.data!.Revision }),
    onSuccess: setPreview,
  })
  const submit = useMutation({
    mutationFn: (intent: Pending) => api.submitCommand(session.csrf_token, intent.key, {
      action_id: intent.actionID ?? 'C05', target_id: id, preview_id: intent.previewID, payload: { revision: intent.revision },
    }),
    onSuccess: (result) => {
      setCommandID(result.id)
      setPreview(null)
      setStaleIntent(null)
      sessionStorage.removeItem(storageKey(id))
      setPending(null)
      void queryClient.invalidateQueries({ queryKey: ['subscription', id] })
      void queryClient.invalidateQueries({ queryKey: ['resource', 'subscriptions'] })
      void queryClient.invalidateQueries({ queryKey: ['commands'] })
    },
    onError: (error, intent) => {
      if (error instanceof HttpError && (error.status === 400 || error.status === 422 || error.code === 'PREVIEW_STALE')) {
        sessionStorage.removeItem(storageKey(id))
        setPending(null)
        setPreview(null)
        if (error.code === 'PREVIEW_STALE') {
          setStaleIntent({
            actionID: intent.actionID,
            revision: preview?.source_versions.subscription_revision ?? intent.revision,
            scheduledAt: intent.actionID === 'C06' ? preview?.impact.previous_cancel_at ?? null : null,
          })
          void subscription.refetch()
        } else {
          setStaleIntent(null)
        }
      }
    },
  })
  const confirm = () => {
    if (!preview || !subscription.data || !canConfirmPreview(preview)) return
    const previewResuming = preview.action_id === 'C06'
    const intent: Pending = { key: crypto.randomUUID(), previewID: preview.preview_id, revision: preview.source_versions.subscription_revision, actionID: previewResuming ? 'C06' : 'C05' }
    modal.confirm({
      title: previewResuming ? '確認撤銷取消排程' : '確認排程取消',
      content: <Space direction="vertical"><span>訂閱：{id}</span><span>原取消時間：{new Date(preview.impact.effective_at ?? preview.impact.previous_cancel_at).toLocaleString()}</span><span>{previewResuming ? '這會撤銷尚未生效的取消排程。' : '這會建立下期取消排程。'}</span></Space>,
      okText: previewResuming ? '確認恢復' : '確認排程', cancelText: '返回檢查',
      onOk: () => {
        if (!canConfirmPreview(preview)) return
        sessionStorage.setItem(storageKey(id), JSON.stringify(intent))
        setPending(intent)
        submit.mutate(intent)
      },
    })
  }
  if (subscription.isPending) return <Skeleton active />
  if (subscription.isError) return <ReadFailureWithRecovery title="訂閱無法載入" message={subscription.error.message} onRetryRead={() => { void subscription.refetch() }} hasPendingCommand={pending !== null} onRecoverCommand={() => { if (pending) submit.mutate(pending) }} recovering={submit.isPending} commandID={commandID} recoveryError={submit.isError ? submit.error.message : null} />
  return <div className="form-page">
    <Typography.Title level={2}>{resuming ? '恢復取消排程' : '排程取消訂閱'}</Typography.Title>
    <Card>
      <Descriptions column={1} bordered size="small" items={[
        { key: 'id', label: '訂閱 ID', children: subscription.data.ID },
        { key: 'customer', label: '客戶', children: subscription.data.CustomerID },
        { key: 'price', label: '當前價格版本', children: subscription.data.PriceVersionID },
        { key: 'status', label: '狀態', children: subscription.data.Status },
        { key: 'revision', label: 'Revision', children: subscription.data.Revision },
        { key: 'scheduled', label: '取消排程', children: subscription.data.ScheduledCancel ? new Date(subscription.data.ScheduledCancel.EffectiveAt).toLocaleString() : '無' },
      ]} />
      {subscription.data.Status === 'active' && !commandID && <Button className="result-card" onClick={() => createPreview.mutate()} loading={createPreview.isPending} disabled={pending !== null || (staleIntent !== null && subscription.isFetching)}>{resuming ? '預覽恢復取消' : '預覽取消'}</Button>}
    </Card>
    {staleIntent && <Alert type="warning" showIcon className="result-card" message="原取消預覽已失效，請檢查訂閱的最新狀態" description={<Descriptions column={1} size="small" items={[
      { key: 'intent', label: '原操作意圖', children: staleIntent.actionID === 'C06' ? '恢復取消排程' : '排程取消訂閱' },
      { key: 'current', label: '目前可預覽操作', children: subscription.isFetching ? '重新讀取中…' : resuming ? '恢復取消排程' : '排程取消訂閱' },
      { key: 'revision', label: '來源 revision', children: `${staleIntent.revision} → ${subscription.isFetching ? '重新讀取中…' : subscription.data.Revision}` },
      { key: 'schedule', label: '取消排程', children: `${staleIntent.scheduledAt ? new Date(staleIntent.scheduledAt).toLocaleString() : '無'} → ${subscription.isFetching ? '重新讀取中…' : subscription.data.ScheduledCancel ? new Date(subscription.data.ScheduledCancel.EffectiveAt).toLocaleString() : '無'}` },
    ]} />} />}
    {pending && !commandID && <Alert type="warning" showIcon className="result-card" message="原取消命令的結果尚未確認" description={<Button onClick={() => submit.mutate(pending)} loading={submit.isPending}>用原 request key 查詢</Button>} />}
    {createPreview.isError && <Alert type="error" showIcon className="result-card" message={createPreview.error instanceof HttpError && createPreview.error.code === 'ACCOUNT_MIGRATION_STOPPED' ? '帳戶遷移已停止，無法新增取消排程' : '無法建立預覽'} description={createPreview.error.message} />}
    {preview && !commandID && <Card title={preview.action_id === 'C06' ? '恢復預覽' : '取消預覽'} className="result-card">
      <PreviewWarnings preview={preview} expired={previewExpired} />
      <Descriptions column={1} bordered size="small" items={[
        { key: 'effective', label: '原取消時間', children: new Date(preview.impact.effective_at ?? preview.impact.previous_cancel_at).toLocaleString() },
        { key: 'revision', label: '來源 revision', children: preview.source_versions.subscription_revision },
        { key: 'expiry', label: '預覽有效至', children: new Date(preview.expires_at).toLocaleString() },
      ]} />
      <Button className="result-card" danger={preview.action_id !== 'C06'} onClick={confirm} disabled={!canConfirmPreview(preview) || previewExpired || pending !== null}>{preview.action_id === 'C06' ? '確認恢復取消' : '確認排程取消'}</Button>
    </Card>}
    {submit.isError && <Alert type={submit.error instanceof HttpError && submit.error.code === 'PREVIEW_STALE' ? 'warning' : 'error'} showIcon className="result-card" message={submit.error instanceof HttpError && submit.error.code === 'PREVIEW_STALE' ? '原預覽已失效，請重新預覽' : submit.error instanceof HttpError && (submit.error.status === 400 || submit.error.status === 422) ? '命令未被接受，請檢查輸入' : '命令結果尚未確認'} description={submit.error.message} />}
    {commandID && <Card title="命令結果" className="result-card">
      {command.isPending && <Typography.Text>正在查詢命令狀態…</Typography.Text>}
      {command.isError && <CommandReadRecovery error={command.error} onRetry={() => void command.refetch()} onClear={() => { setCommandID(null); setPreview(null); setStaleIntent(null); createPreview.reset(); void subscription.refetch() }} />}
      {command.data && <Descriptions column={1} bordered size="small" items={[
        { key: 'id', label: '命令 ID', children: <Typography.Text copyable>{command.data.id}</Typography.Text> },
        { key: 'status', label: '狀態', children: command.data.status },
        { key: 'schedule', label: '排程 ID', children: command.data.result_refs?.schedule_id ?? '尚未建立' },
        { key: 'error', label: '錯誤', children: command.data.error_code || '無' },
      ]} />}
      <Button className="result-card" href={`/admin/commands/${encodeURIComponent(commandID)}`}>開啟命令頁面</Button>
      {(command.data?.status === 'succeeded' || command.data?.status === 'failed') && <Button className="result-card" disabled={command.isError} onClick={() => { setCommandID(null); setPreview(null); setStaleIntent(null); createPreview.reset(); void subscription.refetch() }}>依最新訂閱狀態繼續操作</Button>}
    </Card>}
  </div>
}
