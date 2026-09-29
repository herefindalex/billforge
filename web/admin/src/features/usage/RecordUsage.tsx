import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Alert, App as AntApp, Button, Card, Descriptions, Form, Input, Space, Typography } from 'antd'
import { api, HttpError, type Command, type Session } from '../../api/client'
import { isExactAdminUTC, isNonNegativeInt64String } from '../../api/validation'
import { useStoredCommandID } from '../commands/useStoredCommandID'
import CommandReadRecovery from '../commands/CommandReadRecovery'

type UsageInput = {
  source: string
  event_id: string
  subscription_id: string
  meter_id: string
  occurred_at: string
  quantity: string
}
type Pending = { key: string; payload: UsageInput }
const storageKey = 'billforge:admin:record-usage:pending'

function loadPending(): Pending | null {
  try {
    const value = sessionStorage.getItem(storageKey)
    return value ? JSON.parse(value) as Pending : null
  } catch { return null }
}

export default function RecordUsage({ session }: { session: Session }) {
  const { modal } = AntApp.useApp()
  const [form] = Form.useForm<UsageInput>()
  const queryClient = useQueryClient()
  const [pending, setPending] = useState<Pending | null>(loadPending)
  const [commandID, setCommandID] = useStoredCommandID(session.actor_id, 'C26')
  const command = useQuery<Command>({
    queryKey: ['command', commandID], queryFn: () => api.command(commandID!), enabled: commandID !== null,
    refetchInterval: (query) => query.state.data?.status === 'accepted' || query.state.data?.status === 'running' ? 1500 : false,
  })
  const submit = useMutation({
    mutationFn: (intent: Pending) => api.submitCommand(session.csrf_token, intent.key, { action_id: 'C26', payload: intent.payload }),
    onSuccess: (result) => {
      setCommandID(result.id)
      sessionStorage.removeItem(storageKey)
      setPending(null)
      void queryClient.invalidateQueries({ queryKey: ['resource', 'usage-events'] })
      void queryClient.invalidateQueries({ queryKey: ['commands'] })
    },
    onError: (error) => {
      // Bad requests are rejected before admission; uncertain responses keep
      // the original intent and request key until they can be recovered.
      if (error instanceof HttpError && (error.status === 400 || error.status === 422)) {
        sessionStorage.removeItem(storageKey)
        setPending(null)
      }
    },
  })
  useEffect(() => {
    if (!(submit.error instanceof HttpError) || (submit.error.status !== 400 && submit.error.status !== 422) || pending !== null) return
    const warning = document.getElementById('usage-input-rejection')
    if (!warning) return
    const frame = requestAnimationFrame(() => { if (warning.isConnected) warning.focus() })
    return () => cancelAnimationFrame(frame)
  }, [submit.error, pending])
  const confirm = (values: UsageInput) => {
    if (pending || commandID) return
    const payload: UsageInput = {
      source: values.source.trim(), event_id: values.event_id.trim(),
      subscription_id: values.subscription_id.trim(), meter_id: values.meter_id.trim(),
      occurred_at: values.occurred_at.trim(), quantity: values.quantity.trim(),
    }
    const intent: Pending = { key: crypto.randomUUID(), payload }
    modal.confirm({
      title: '確認記錄用量事件',
      content: <Space direction="vertical"><span>訂閱：{payload.subscription_id}</span><span>Meter：{payload.meter_id}</span><span>數量：{payload.quantity}</span><span>發生時間：{payload.occurred_at}</span><span>來源事件：{payload.source} / {payload.event_id}</span></Space>,
      okText: '確認記錄', cancelText: '返回修改',
      onOk: () => {
        sessionStorage.setItem(storageKey, JSON.stringify(intent))
        setPending(intent)
        submit.mutate(intent)
      },
    })
  }
  return <div className="form-page">
    <Typography.Title level={2}>記錄用量事件</Typography.Title>
    <Typography.Paragraph type="secondary">來源與事件 ID 組成重送識別；相同事件的數量或時間不同時，伺服器會拒絕。</Typography.Paragraph>
    {pending && !commandID && <Alert type="warning" showIcon className="form-alert" message="原事件的命令結果尚未確認" description={<Button onClick={() => submit.mutate(pending)} loading={submit.isPending}>用原 request key 查詢</Button>} />}
    <Card>
      <Form form={form} layout="vertical" onFinish={confirm} disabled={pending !== null || commandID !== null}>
        <Form.Item label="訂閱 ID" name="subscription_id" rules={[{ required: true, message: '請輸入訂閱 ID' }]}><Input /></Form.Item>
        <Form.Item label="Meter ID" name="meter_id" rules={[{ required: true, message: '請輸入 Meter ID' }]}><Input /></Form.Item>
        <Form.Item label="來源" name="source" rules={[{ required: true, message: '請輸入來源' }]}><Input /></Form.Item>
        <Form.Item label="事件 ID" name="event_id" rules={[{ required: true, message: '請輸入事件 ID' }]}><Input /></Form.Item>
        <Form.Item label="發生時間（UTC）" name="occurred_at" rules={[{ required: true, message: '請輸入 UTC 時間' }, { validator: async (_: unknown, value: string | undefined) => {
          if (value && !isExactAdminUTC(value.trim())) throw new Error('請輸入有效的 UTC 時間，且在可儲存範圍內（最多 9 位小數秒）')
        } }]}><Input placeholder="2026-09-29T12:00:00Z" /></Form.Item>
        <Form.Item label="數量" name="quantity" rules={[{ required: true, message: '請輸入數量' }, { pattern: /^[1-9]\d*$/, message: '請輸入正整數' }, { validator: async (_: unknown, value: string | undefined) => {
          if (value && !isNonNegativeInt64String(value.trim())) throw new Error('數量不可超過 int64 上限')
        } }]}><Input inputMode="numeric" /></Form.Item>
        {submit.isError && <div id="usage-input-rejection" tabIndex={-1} aria-label="用量輸入未被接受" className="form-alert"><Alert type="error" showIcon message={submit.error instanceof HttpError && (submit.error.status === 400 || submit.error.status === 422) ? '輸入未被接受，請修改後重試' : '命令結果尚未確認'} description={submit.error.message} /></div>}
        <Button type="primary" htmlType="submit" loading={submit.isPending} disabled={pending !== null || commandID !== null}>檢查並記錄</Button>
      </Form>
    </Card>
    {commandID && <Card title="命令結果" className="result-card">
      {command.isPending && <Typography.Text>正在查詢命令狀態…</Typography.Text>}
      {command.isError && <CommandReadRecovery error={command.error} onRetry={() => void command.refetch()} onClear={() => { setCommandID(null); form.resetFields() }} />}
      {command.data && <Descriptions column={1} bordered size="small" items={[
        { key: 'id', label: '命令 ID', children: <Typography.Text copyable>{command.data.id}</Typography.Text> },
        { key: 'status', label: '狀態', children: command.data.status },
        { key: 'event', label: '事件 ID', children: command.data.result_refs?.event_id ?? '尚未記錄' },
        { key: 'period', label: '帳期', children: command.data.result_refs?.period_index ?? '尚未確定' },
        { key: 'error', label: '錯誤', children: command.data.error_code || '無' },
      ]} />}
      <Button className="result-card" href={`/admin/commands/${encodeURIComponent(commandID)}`}>開啟命令頁面</Button>
      {(command.data?.status === 'succeeded' || command.data?.status === 'failed') && <Button className="result-card" disabled={command.isError} onClick={() => { setCommandID(null); form.resetFields() }}>記錄另一筆事件</Button>}
    </Card>}
  </div>
}
