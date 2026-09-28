import { useRef, useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { Alert, App as AntApp, Button, Card, Descriptions, Form, Input, Result, Space, Typography } from 'antd'
import { useParams } from 'react-router-dom'
import { api, HttpError, type Command, type Preview, type Session } from '../../api/client'
import { isNonNegativeInt64String } from '../../api/validation'
import Money from '../../components/Money'
import PreviewWarnings from '../../components/PreviewWarnings'
import { canConfirmPreview, usePreviewExpired } from '../../components/usePreviewExpiry'
import { useStoredCommandID } from '../commands/useStoredCommandID'

type Pending = { key: string; previewID: string; amountMinor: string }
type PreviewRequest = { amountMinor: string; revision: number }
function storageKey(id: string) { return `billforge:admin:payment:${id}` }
function loadPending(id: string): Pending | null {
  try { const value = sessionStorage.getItem(storageKey(id)); return value ? JSON.parse(value) as Pending : null } catch { return null }
}

export default function CreatePayment({ session }: { session: Session }) {
  const { id = '' } = useParams()
  const { modal } = AntApp.useApp()
  const [form] = Form.useForm<{ amount_minor: string }>()
  const [preview, setPreview] = useState<Preview | null>(null)
  const previewExpired = usePreviewExpired(preview?.expires_at)
  const [stalePreview, setStalePreview] = useState<Preview | null>(null)
  const [previewInvalidated, setPreviewInvalidated] = useState(false)
  const formRevision = useRef(0)
  const [pending, setPending] = useState<Pending | null>(() => loadPending(id))
  const [commandID, setCommandID] = useStoredCommandID(session.actor_id, 'C07', id)
  const command = useQuery<Command>({
    queryKey: ['command', commandID], queryFn: () => api.command(commandID!), enabled: commandID !== null,
    refetchInterval: (query) => query.state.data?.status === 'accepted' || query.state.data?.status === 'running' ? 1500 : false,
  })
  const createPreview = useMutation({
    mutationFn: ({ amountMinor }: PreviewRequest) => api.createPreview(session.csrf_token, 'C07', id, { amount_minor: amountMinor }),
    onSuccess: (result, { revision }) => {
      if (revision !== formRevision.current) return
      setPreview(result)
      setPreviewInvalidated(false)
    },
  })
  const submit = useMutation({
    mutationFn: (intent: Pending) => api.submitCommand(session.csrf_token, intent.key, { action_id: 'C07', target_id: id, preview_id: intent.previewID, payload: { amount_minor: intent.amountMinor } }),
    onSuccess: (result) => { setCommandID(result.id); setPending(null); setPreview(null); setStalePreview(null); setPreviewInvalidated(false); sessionStorage.removeItem(storageKey(id)) },
    onError: (error, intent) => {
      if (error instanceof HttpError && (error.status === 400 || error.status === 422 || error.code === 'PREVIEW_STALE')) {
        // These responses are emitted before admission; an uncertain result
        // still retains the original key and amount for recovery.
        sessionStorage.removeItem(storageKey(id))
        setPending(null)
        setPreview(null)
        if (error.code === 'PREVIEW_STALE') {
          form.setFieldsValue({ amount_minor: intent.amountMinor })
          setStalePreview(preview)
          createPreview.mutate({ amountMinor: intent.amountMinor, revision: formRevision.current })
        } else {
          setStalePreview(null)
        }
      }
    },
  })
  const onValuesChange = () => {
    formRevision.current += 1
    setStalePreview(null)
    if (preview || createPreview.isPending) {
      setPreview(null)
      setPreviewInvalidated(true)
    }
  }
  const confirm = () => {
    if (!preview || !canConfirmPreview(preview)) return
    const intent: Pending = { key: crypto.randomUUID(), previewID: preview.preview_id, amountMinor: preview.impact.amount_minor }
    modal.confirm({
      title: '確認建立付款',
      content: <Space direction="vertical"><span>帳單：{id}</span><span>新付款金額：<Money minor={preview.impact.amount_minor} currency={preview.impact.currency} /></span><span>建立後仍需處理付款派送。</span></Space>,
      okText: '確認建立', cancelText: '返回檢查',
      onOk: () => {
        if (!canConfirmPreview(preview)) return
        sessionStorage.setItem(storageKey(id), JSON.stringify(intent))
        setPending(intent)
        submit.mutate(intent)
      },
    })
  }
  return <div className="form-page">
    <Typography.Title level={2}>建立付款</Typography.Title>
    <Card>
      <Typography.Paragraph>帳單 ID：<Typography.Text copyable>{id}</Typography.Text></Typography.Paragraph>
      <Form form={form} layout="vertical" disabled={pending !== null || commandID !== null} onValuesChange={onValuesChange} onFinish={(values: { amount_minor: string }) => { setPreviewInvalidated(false); createPreview.mutate({ amountMinor: values.amount_minor.trim(), revision: formRevision.current }) }}>
        <Form.Item label="付款金額（最小貨幣單位）" name="amount_minor" rules={[{ required: true, message: '請輸入金額' }, { pattern: /^[1-9]\d*$/, message: '請輸入正整數' }, { validator: async (_: unknown, value: string | undefined) => {
          if (value && !isNonNegativeInt64String(value.trim())) throw new Error('付款金額不可超過 int64 上限')
        } }]}><Input inputMode="numeric" /></Form.Item>
        <Button type="primary" htmlType="submit" loading={createPreview.isPending} disabled={pending !== null || commandID !== null}>預覽付款</Button>
      </Form>
    </Card>
    {previewInvalidated && !preview && !pending && !commandID && <Alert type="warning" showIcon className="result-card" message="付款金額已變更，請重新預覽" />}
    {pending && !commandID && <Alert type="warning" showIcon className="result-card" message="原付款命令的結果尚未確認" description={<Button onClick={() => submit.mutate(pending)} loading={submit.isPending}>用原 request key 查詢</Button>} />}
    {createPreview.isError && !previewInvalidated && <Alert type="error" showIcon className="result-card" message="無法建立預覽" description={createPreview.error.message} />}
      {preview && <Card title="付款預覽" className="result-card">
      <PreviewWarnings preview={preview} expired={previewExpired} />
      {stalePreview && <Alert type="warning" showIcon message="原預覽已失效，請檢查新預覽並再次確認" description={<Descriptions column={1} size="small" items={[
        { key: 'outstanding', label: '未清餘額', children: <Space><span>原先：<Money minor={stalePreview.impact.outstanding_before_minor} currency={stalePreview.impact.currency} /></span><span>現在：<Money minor={preview.impact.outstanding_before_minor} currency={preview.impact.currency} /></span></Space> },
        ...Array.from(new Set([...Object.keys(stalePreview.source_versions), ...Object.keys(preview.source_versions)])).filter((key) => stalePreview.source_versions[key] !== preview.source_versions[key]).map((key) => ({ key: `source:${key}`, label: `來源 ${key}`, children: `${stalePreview.source_versions[key] ?? '未知'} → ${preview.source_versions[key] ?? '未知'}` })),
      ]} />} />}
      <Descriptions column={1} bordered size="small" items={[
        { key: 'amount', label: '新付款金額', children: <Money minor={preview.impact.amount_minor} currency={preview.impact.currency} /> },
        { key: 'outstanding', label: '目前未清餘額', children: <Money minor={preview.impact.outstanding_before_minor} currency={preview.impact.currency} /> },
        { key: 'expiry', label: '預覽有效至', children: new Date(preview.expires_at).toLocaleString() },
      ]} />
      <Button className="result-card" onClick={confirm} disabled={!canConfirmPreview(preview) || previewExpired || pending !== null}>確認建立付款</Button>
    </Card>}
    {submit.isError && <Alert type={submit.error instanceof HttpError && submit.error.code === 'PREVIEW_STALE' ? 'warning' : 'error'} showIcon className="result-card" message={submit.error instanceof HttpError && submit.error.code === 'PREVIEW_STALE' ? '原預覽已失效，請重新預覽' : submit.error instanceof HttpError && (submit.error.status === 400 || submit.error.status === 422) ? '命令未被接受，請檢查輸入' : '命令結果尚未確認'} description={submit.error.message} />}
    {commandID && <Card title="命令結果" className="result-card">
      {command.isPending && <Typography.Text>正在查詢命令狀態…</Typography.Text>}
      {command.isError && <Result status="error" title="命令狀態無法載入" extra={<Button onClick={() => void command.refetch()}>重試</Button>} />}
      {command.data && <Descriptions column={1} bordered size="small" items={[
        { key: 'id', label: '命令 ID', children: <Typography.Text copyable>{command.data.id}</Typography.Text> },
        { key: 'status', label: '狀態', children: command.data.status },
        { key: 'operation', label: '付款操作 ID', children: command.data.result_refs?.operation_id ?? '尚未建立' },
        { key: 'error', label: '錯誤', children: command.data.error_code || '無' },
      ]} />}
      <Button className="result-card" href={`/admin/commands/${encodeURIComponent(commandID)}`}>開啟命令頁面</Button>
      {(command.data?.status === 'succeeded' || command.data?.status === 'failed') && <Button className="result-card" onClick={() => { setCommandID(null); setPreview(null); setStalePreview(null); setPreviewInvalidated(false); form.resetFields() }}>建立另一筆付款</Button>}
    </Card>}
  </div>
}
