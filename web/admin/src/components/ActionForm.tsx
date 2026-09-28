import { useRef, useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { Alert, App as AntApp, Button, Card, Descriptions, Form, Input, Select, Space, Typography } from 'antd'
import { useLocation, useNavigate, useParams } from 'react-router-dom'
import { api, HttpError, type Command, type Preview, type Session } from '../api/client'
import { isExactAdminUTC, isNonNegativeInt64String, minimumUpfrontFits } from '../api/validation'
import { useStoredCommandID } from '../features/commands/useStoredCommandID'
import CommandReadRecovery, { isCommandNotFound } from '../features/commands/CommandReadRecovery'
import Money from './Money'
import ExternalOperationOutcome from './ExternalOperationOutcome'
import PreviewWarnings from './PreviewWarnings'
import { canConfirmPreview, usePreviewExpired } from './usePreviewExpiry'

const utcFields = new Set(['effective_from', 'effective_to', 'effective_at', 'cutoff', 'as_of', 'value_utc'])
const int64Patterns = new Set([/^\d+$/.source, /^[1-9]\d*$/.source])

export type ActionField = {
  name: string
  label: string
  placeholder?: string
  required?: boolean
  pattern?: RegExp
  options?: { value: string; label: string }[]
  csv?: boolean
}

export type ActionConfig = {
  actionID: string
  title: string
  confirmLabel: string
  fields: ActionField[]
  preview: boolean
  description?: string
  targetParam?: string
}

type Pending = { key: string; previewID?: string; payload: Record<string, unknown> }
type PreviewRequest = { input: Record<string, unknown>; revision: number }
function storageKey(actionID: string, targetID: string) { return `billforge:admin:${actionID}:${targetID}` }
function loadPending(actionID: string, targetID: string): Pending | null {
  try { const value = sessionStorage.getItem(storageKey(actionID, targetID)); return value ? JSON.parse(value) as Pending : null } catch { return null }
}
function displayValue(key: string, value: unknown, currency: string | undefined) {
  if (key.endsWith('_minor')) return <Money minor={value} currency={currency} />
  if (Array.isArray(value)) return value.join(', ') || '無'
  if (value && typeof value === 'object') return JSON.stringify(value)
  return String(value ?? '')
}

function collectionChangeNote(impact: Record<string, string>, actionID: string) {
  const cancelled = Number(impact.created_payments_to_cancel ?? '0')
  const remaining = impact.invoice_outstanding_after_minor ?? '0'
  const actionName = actionID === 'C11' ? '減額' : '抵扣'
  return <Space direction="vertical">
    <span>{cancelled > 0 ? `將取消 ${cancelled} 筆尚未送出的付款操作。` : '沒有尚未送出的付款操作需要取消。'}</span>
    <span>{actionName}後尚待支付：<Money minor={remaining} currency={impact.currency} /></span>
    {remaining !== '0' && <span>若要收取剩餘金額，請建立新的付款操作。</span>}
  </Space>
}

export default function ActionForm({ config, session }: { config: ActionConfig; session: Session }) {
 const params = useParams()
 const id = params[config.targetParam ?? 'id'] ?? ''
 return <ActionFormInstance key={`${config.actionID}:${id}`} config={config} session={session} id={id} />
}

function ActionFormInstance({ config, session, id }: { config: ActionConfig; session: Session; id: string }) {
 const location = useLocation()
 const navigate = useNavigate()
 const initialValues = (location.state ?? {}) as Record<string, string>
 const { modal } = AntApp.useApp()
 const [form] = Form.useForm()
  const [preview, setPreview] = useState<Preview | null>(null)
  const previewExpired = usePreviewExpired(preview?.expires_at)
  const [stalePreview, setStalePreview] = useState<Preview | null>(null)
  const [previewInvalidated, setPreviewInvalidated] = useState(false)
  const formRevision = useRef(0)
  const [payload, setPayload] = useState<Record<string, unknown> | null>(null)
  const [pending, setPending] = useState<Pending | null>(() => loadPending(config.actionID, id))
  const [commandID, setCommandID] = useStoredCommandID(session.actor_id, config.actionID, id)
  const command = useQuery<Command>({
    queryKey: ['command', commandID], queryFn: () => api.command(commandID!), enabled: commandID !== null,
    refetchInterval: (query) => query.state.data?.status === 'accepted' || query.state.data?.status === 'running' ? 1500 : false,
  })
  const createPreview = useMutation({
    mutationFn: ({ input }: PreviewRequest) => api.createPreview(session.csrf_token, config.actionID, id, input),
    onSuccess: (result, { input, revision }) => {
      if (revision !== formRevision.current) return
      setPreview(result)
      setPayload(input)
      setPreviewInvalidated(false)
    },
  })
  const submit = useMutation({
    mutationFn: (intent: Pending) => api.submitCommand(session.csrf_token, intent.key, {
      action_id: config.actionID, target_id: id, preview_id: intent.previewID, payload: intent.payload,
    }),
    onSuccess: (result) => {
      setCommandID(result.id)
      setPending(null)
      setPreview(null)
      setStalePreview(null)
      setPreviewInvalidated(false)
      sessionStorage.removeItem(storageKey(config.actionID, id))
    },
    onError: (error, intent) => {
      if (!(error instanceof HttpError)) return
    if (error.code === 'PREVIEW_STALE') {
      setStalePreview(preview)
      setPreview(null)
      setPending(null)
      sessionStorage.removeItem(storageKey(config.actionID, id))
      createPreview.mutate({ input: intent.payload, revision: formRevision.current })
    } else if (error.code === 'COMMAND_PENDING_RETRY' && error.commandID) {
      setCommandID(error.commandID)
      setPending(null)
      sessionStorage.removeItem(storageKey(config.actionID, id))
    } else if (error.code === 'IDEMPOTENCY_CONFLICT' || error.status === 422 || error.status === 400) {
        setPending(null)
        sessionStorage.removeItem(storageKey(config.actionID, id))
      }
    },
  })
  const resume = useMutation({
    mutationFn: () => api.resumeCommand(session.csrf_token, commandID!),
    onSuccess: () => { void command.refetch() },
  })
  const confirm = (input: Record<string, unknown>, currentPreview: Preview | null) => {
    if (currentPreview && !canConfirmPreview(currentPreview)) return
    const intent: Pending = { key: crypto.randomUUID(), previewID: currentPreview?.preview_id, payload: input }
    modal.confirm({
      title: config.confirmLabel,
      content: <Space direction="vertical"><span>對象：{id}</span>{currentPreview && Object.entries(currentPreview.impact).map(([key, value]) => <span key={key}>{key}：{displayValue(key, value, currentPreview.impact.currency)}</span>)}{(config.actionID === 'C11' || config.actionID === 'C12') && currentPreview && collectionChangeNote(currentPreview.impact, config.actionID)}</Space>,
      okText: config.confirmLabel, cancelText: '返回檢查',
      onOk: () => {
        if (currentPreview && !canConfirmPreview(currentPreview)) return
        sessionStorage.setItem(storageKey(config.actionID, id), JSON.stringify(intent))
        setPending(intent)
        submit.mutate(intent)
      },
    })
  }
  const onFormFinish = (values: Record<string, string>) => {
    setStalePreview(null)
    setPreviewInvalidated(false)
    const input: Record<string, unknown> = {}
    for (const field of config.fields) {
      const value = values[field.name]
      if (value === undefined) continue
      input[field.name] = field.csv ? String(value).split(/[\n,]/).map((item) => item.trim()).filter(Boolean) : String(value).trim()
    }
  if (config.actionID === 'C46') {
    if (input.mode === 'fixed' && (!input.value_utc || !isExactAdminUTC(String(input.value_utc)))) {
      form.setFields([{ name: 'value_utc', errors: ['請輸入有效的 UTC 時間（最多 9 位小數秒），例如 2026-09-26T12:00:00Z'] }])
    return
   }
   if (input.mode === 'real') delete input.value_utc
  }
    if (config.preview) createPreview.mutate({ input, revision: formRevision.current })
    else confirm(input, null)
  }
  const onValuesChange = () => {
    formRevision.current += 1
    if (preview || createPreview.isPending) {
      setPreview(null)
      setPayload(null)
      setStalePreview(null)
      setPreviewInvalidated(true)
    }
    submit.reset()
    createPreview.reset()
  }
 const impactChanges = stalePreview && preview ? Array.from(new Set([
  ...Object.keys(stalePreview.impact), ...Object.keys(preview.impact),
 ])).filter((key) => JSON.stringify(stalePreview.impact[key]) !== JSON.stringify(preview.impact[key])) : []
  const sourceChanges = stalePreview && preview ? Array.from(new Set([
  ...Object.keys(stalePreview.source_versions), ...Object.keys(preview.source_versions),
  ])).filter((key) => stalePreview.source_versions[key] !== preview.source_versions[key]) : []
  return <div className="form-page">
    <Typography.Title level={2}>{config.title}</Typography.Title>
    {config.description && <Typography.Paragraph type="secondary">{config.description}</Typography.Paragraph>}
    <Card>
      {id && <Typography.Paragraph>對象 ID：<Typography.Text copyable>{id}</Typography.Text></Typography.Paragraph>}
      <Form form={form} key={`${config.actionID}:${id}`} layout="vertical" initialValues={initialValues} onFinish={onFormFinish} onValuesChange={onValuesChange} disabled={pending !== null || commandID !== null}>
        {config.fields.map((field) => <Form.Item key={field.name} name={field.name} label={field.label} dependencies={field.name === 'seat_minor' && ['C18', 'C20', 'C31'].includes(config.actionID) ? ['fixed_minor'] : undefined} rules={[
          ...(field.required ? [{ required: true, message: `請輸入${field.label}` }] : []),
          ...(field.pattern ? [{ pattern: field.pattern, message: `${field.label}格式不正確` }] : []),
          ...(field.pattern && int64Patterns.has(field.pattern.source) ? [{ validator: async (_: unknown, value: string | undefined) => {
            if (value && !isNonNegativeInt64String(value.trim())) throw new Error(`${field.label}不可超過 int64 上限`)
          } }] : []),
          ...(field.name === 'seat_minor' && ['C18', 'C20', 'C31'].includes(config.actionID) ? [{ validator: async (_: unknown, value: string | undefined) => {
            const fixed = form.getFieldValue('fixed_minor') as string | undefined
            if (fixed && value && isNonNegativeInt64String(fixed) && isNonNegativeInt64String(value) && !minimumUpfrontFits(fixed, value)) {
              throw new Error('固定金額加上一席費用不可超過 int64 上限')
            }
          } }] : []),
          ...(utcFields.has(field.name) ? [{ validator: async (_: unknown, value: string | undefined) => {
            if (value && !isExactAdminUTC(value.trim())) throw new Error(`${field.label}必須是有效 UTC 時間，且在可儲存範圍內（最多 9 位小數秒）`)
          } }] : []),
        ]}>
          {field.options ? <Select options={field.options} /> : field.csv ? <Input.TextArea rows={4} placeholder={field.placeholder} /> : <Input autoComplete="off" placeholder={field.placeholder} inputMode={field.name.endsWith('_minor') ? 'numeric' : undefined} />}
        </Form.Item>)}
        <Button type="primary" htmlType="submit" loading={createPreview.isPending} disabled={pending !== null || commandID !== null}>{config.preview ? '建立預覽' : config.confirmLabel}</Button>
      </Form>
    </Card>
    {previewInvalidated && !preview && !pending && !commandID && <Alert type="warning" showIcon className="result-card" message="輸入已變更，請重新建立預覽" />}
    {pending && !commandID && <Alert type="warning" showIcon className="result-card" message="原命令的結果尚未確認" description={<Button onClick={() => submit.mutate(pending)} loading={submit.isPending}>用原 request key 查詢</Button>} />}
    {createPreview.isError && !previewInvalidated && <Alert type="error" showIcon className="result-card" message="無法建立預覽" description={createPreview.error.message} />}
    {preview && <Card title="操作預覽" className="result-card">
      <PreviewWarnings preview={preview} expired={previewExpired} />
      {stalePreview && <Alert type="warning" showIcon message="原預覽已失效，請檢查新預覽並再次確認" description={
        <Descriptions column={1} size="small" items={[
          ...sourceChanges.map((key) => ({ key: `source:${key}`, label: `來源 ${key}`, children: `${stalePreview.source_versions[key] ?? '未知'} → ${preview.source_versions[key] ?? '未知'}` })),
          ...impactChanges.map((key) => ({ key: `impact:${key}`, label: key, children: <Space><span>原先：{displayValue(key, stalePreview.impact[key], stalePreview.impact.currency)}</span><span>現在：{displayValue(key, preview.impact[key], preview.impact.currency)}</span></Space> })),
          ...(!sourceChanges.length && !impactChanges.length ? [{ key: 'same-values', label: '數值', children: '數值相同；原預覽的期限或來源已失效，仍需重新確認。' }] : []),
        ]} />
      } />}
      {(config.actionID === 'C11' || config.actionID === 'C12') && <Alert type="info" showIcon message="調整後的收款安排" description={collectionChangeNote(preview.impact, config.actionID)} />}
      {['C13', 'C30', 'C32', 'C44', 'C45'].includes(config.actionID) && preview.impact.has_more_candidates === 'true' && <Alert type="info" showIcon message="本批之外仍有候選項目" description="本次僅處理預覽列出的項目。完成後請建立新的預覽與批次，繼續處理其餘項目。" />}
      <Descriptions column={1} bordered size="small" items={Object.entries(preview.impact).map(([key, value]) => ({ key, label: key, children: displayValue(key, value, preview.impact.currency) }))} />
      <Typography.Paragraph className="result-card" type="secondary">有效至：{new Date(preview.expires_at).toLocaleString()}</Typography.Paragraph>
      <Button onClick={() => payload && confirm(payload, preview)} disabled={!canConfirmPreview(preview) || previewExpired || pending !== null}>{config.confirmLabel}</Button>
    </Card>}
    {submit.isError && command.data?.status !== 'succeeded' && command.data?.status !== 'failed' && <Alert type={submit.error instanceof HttpError && submit.error.code === 'PREVIEW_STALE' ? 'warning' : 'error'} showIcon className="result-card" message={submit.error instanceof HttpError && submit.error.code === 'PREVIEW_STALE' ? '原預覽已失效' : submit.error instanceof HttpError && (submit.error.status === 400 || submit.error.status === 422 || submit.error.code === 'IDEMPOTENCY_CONFLICT') ? '命令未被接受，請檢查輸入' : '命令結果尚未確認'} description={submit.error.message} />}
    {commandID && <Card title="命令結果" className="result-card" extra={<Button onClick={() => void command.refetch()} loading={command.isFetching}>更新</Button>}>
      {command.isPending && <Typography.Text>正在查詢命令狀態…</Typography.Text>}
      {command.isError && (!command.data || isCommandNotFound(command.error)) && <CommandReadRecovery error={command.error} onRetry={() => void command.refetch()} onClear={() => { setCommandID(null); setPreview(null); setStalePreview(null); setPayload(null); setPreviewInvalidated(false); submit.reset(); resume.reset(); createPreview.reset(); form.resetFields() }} />}
      {command.isError && command.data && <Alert type="warning" showIcon className="result-card" message="無法更新命令狀態；以下是上次成功讀取的資料" description={<Space wrap><span>上次讀取：{new Date(command.dataUpdatedAt).toLocaleString()}</span><Button onClick={() => void command.refetch()}>重試</Button></Space>} />}
      {command.data && <ExternalOperationOutcome command={command.data} />}
      {command.data && <Descriptions column={1} bordered size="small" items={[
        { key: 'id', label: '命令 ID', children: <Typography.Text copyable>{command.data.id}</Typography.Text> },
        { key: 'status', label: '狀態', children: command.data.status },
        ...Object.entries(command.data.result_refs ?? {}).map(([key, value]) => ({ key, label: key, children: displayValue(key, value, command.data?.result_refs?.currency) })),
        { key: 'error', label: '錯誤', children: command.data.error_code || '無' },
      ]} />}
      {config.actionID === 'C34' && command.data?.result_refs?.repair_status === 'blocked' && <Alert type="warning" showIcon className="result-card" message="修復未執行，需檢查最新對帳證據" description={command.data.result_refs.verification || '請重新執行對帳並檢查來源狀態。'} action={<Button onClick={() => navigate(`/discrepancies/${encodeURIComponent(id)}`)}>查看差異</Button>} />}
      {commandID && ['C13', 'C30', 'C32', 'C44', 'C45'].includes(config.actionID) && <Button className="result-card" onClick={() => navigate(`/jobs/${encodeURIComponent(`job:${commandID}`)}`)}>查看逐項進度</Button>}
      {commandID && <Button className="result-card" onClick={() => navigate(`/commands/${encodeURIComponent(commandID)}`)}>開啟命令頁面</Button>}
      {(command.data?.status === 'succeeded' || command.data?.status === 'failed') && <Button className="result-card" onClick={() => { setCommandID(null); setPreview(null); setStalePreview(null); setPayload(null); setPreviewInvalidated(false); submit.reset(); resume.reset(); createPreview.reset(); form.resetFields() }} disabled={command.isError}>執行另一個操作</Button>}
      {command.data?.status === 'accepted' && command.data.error_code !== 'PERMISSION_REVOKED_REVIEW' && <Button className="result-card" onClick={() => resume.mutate()} loading={resume.isPending} disabled={command.isError}>繼續原命令</Button>}
      {command.data?.status === 'waiting_verification' && <Button className="result-card" onClick={() => resume.mutate()} loading={resume.isPending} disabled={command.isError}>重新查證</Button>}
      {resume.isError && <Alert type="error" showIcon className="result-card" message="目前無法查證，請稍後重試" description={resume.error.message} />}
    </Card>}
  </div>
}
