import { useEffect, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Alert, App as AntApp, Button, Card, Descriptions, Form, Input, Skeleton, Space, Typography } from 'antd'
import { useLocation, useParams } from 'react-router-dom'
import { api, canShowStaleRead, HttpError, type Command, type Preview, type Session } from '../../api/client'
import { useStoredCommandID } from '../commands/useStoredCommandID'
import CommandReadRecovery from '../commands/CommandReadRecovery'
import Money from '../../components/Money'
import PreviewWarnings from '../../components/PreviewWarnings'
import ReadFailureWithRecovery from '../../components/ReadFailureWithRecovery'
import { canConfirmPreview, usePreviewExpired } from '../../components/usePreviewExpiry'

type Input = { quote_id: string; fingerprint: string }
type Pending = { key: string; previewID: string; payload: { quote_id: string; fingerprint: string; revision: string } }
type PreviewRequest = { input: Pending['payload']; revision: number }
type StaleAttempt = { preview: Preview | null; payload: Pending['payload'] }
function storageKey(id: string, actionID: string) { return `billforge:admin:${actionID}:${id}` }
function loadPending(id: string, actionID: string): Pending | null {
  try { const value = sessionStorage.getItem(storageKey(id, actionID)); return value ? JSON.parse(value) as Pending : null } catch { return null }
}

export default function SchedulePlan({ session, immediate = false }: { session: Session; immediate?: boolean }) {
  const { id = '' } = useParams()
  const actionID = immediate ? 'C04' : 'C03'
  const { state } = useLocation()
  const initial = (state ?? {}) as Partial<Input>
  const { modal } = AntApp.useApp()
  const [form] = Form.useForm<Input>()
  const queryClient = useQueryClient()
  const [preview, setPreview] = useState<Preview | null>(null)
  const previewExpired = usePreviewExpired(preview?.expires_at)
  const [staleAttempt, setStaleAttempt] = useState<StaleAttempt | null>(null)
  const [previewInvalidated, setPreviewInvalidated] = useState(false)
  const [restoreFocus, setRestoreFocus] = useState(false)
  const formRevision = useRef(0)
  const [payload, setPayload] = useState<Pending['payload'] | null>(null)
  const [pending, setPending] = useState<Pending | null>(() => loadPending(id, actionID))
  const [commandID, setCommandID] = useStoredCommandID(session.actor_id, actionID, id)
  const subscription = useQuery({ queryKey: ['subscription', id], queryFn: () => api.subscription(id), enabled: id !== '' })
  const command = useQuery<Command>({
    queryKey: ['command', commandID], queryFn: () => api.command(commandID!), enabled: commandID !== null,
    refetchInterval: (query) => query.state.data?.status === 'accepted' || query.state.data?.status === 'running' ? 1500 : false,
  })
  const createPreview = useMutation({
    mutationFn: ({ input }: PreviewRequest) => api.createPreview(session.csrf_token, actionID, id, input),
    onSuccess: (result, { input, revision }) => {
      if (revision !== formRevision.current) return
      setPreview(result)
      setPayload(input)
      setPreviewInvalidated(false)
    },
  })
  const submit = useMutation({
    mutationFn: (intent: Pending) => api.submitCommand(session.csrf_token, intent.key, {
      action_id: actionID, target_id: id, preview_id: intent.previewID, payload: intent.payload,
    }),
    onSuccess: (result) => {
      setCommandID(result.id)
      setPending(null)
      setPreview(null)
      setStaleAttempt(null)
      setPreviewInvalidated(false)
      sessionStorage.removeItem(storageKey(id, actionID))
      void queryClient.invalidateQueries({ queryKey: ['subscription', id] })
      void queryClient.invalidateQueries({ queryKey: ['resource', 'subscriptions'] })
      void queryClient.invalidateQueries({ queryKey: ['commands'] })
    },
    onError: (error, intent) => {
      if (error instanceof HttpError && (error.status === 400 || error.status === 422 || error.code === 'PREVIEW_STALE')) {
        sessionStorage.removeItem(storageKey(id, actionID))
        setPending(null)
        setPreview(null)
        setPayload(null)
        if (error.code === 'PREVIEW_STALE') {
          const revision = formRevision.current
          form.setFieldsValue({ quote_id: intent.payload.quote_id, fingerprint: intent.payload.fingerprint })
          setStaleAttempt({ preview, payload: intent.payload })
          void subscription.refetch().then((refreshed) => {
            if (revision !== formRevision.current || refreshed.data?.Revision !== intent.payload.revision) {
              setRestoreFocus(true)
              return
            }
            createPreview.mutate({ input: intent.payload, revision }, { onSettled: () => setRestoreFocus(true) })
          })
        } else {
          setStaleAttempt(null)
          setRestoreFocus(true)
        }
      }
    },
  })
  useEffect(() => {
    if (!restoreFocus || pending !== null || subscription.isFetching || createPreview.isPending) return
    const button = document.getElementById(`${actionID}-${preview ? 'confirm' : 'preview'}`) as HTMLButtonElement | null
    if (!button || button.disabled) return
    const frame = requestAnimationFrame(() => {
      if (button.isConnected && !button.disabled) {
        button.focus()
        setRestoreFocus(false)
      }
    })
    return () => cancelAnimationFrame(frame)
  }, [restoreFocus, pending, preview, subscription.isFetching, createPreview.isPending, actionID])
  const confirm = () => {
    if (!preview || !payload || !canConfirmPreview(preview)) return
    const intent: Pending = { key: crypto.randomUUID(), previewID: preview.preview_id, payload }
    modal.confirm({
      title: immediate ? '確認立即升級' : '確認下期方案變更',
      content: <Space direction="vertical">
        <span>訂閱：{id}</span><span>報價：{payload.quote_id}</span>
        <span>{immediate ? '目前帳期結束' : '生效時間'}：{new Date(preview.impact.effective_at ?? preview.impact.period_end).toLocaleString()}</span>
        <span>價格版本：{preview.impact.price_version_id ?? preview.impact.target_price_version_id}</span>
        {immediate ? <>
          <span>本期升級預估應付上限：<Money minor={preview.impact.estimated_amount_minor ?? preview.impact.net_minor} currency={preview.impact.currency} /></span>
          <span>估算時刻：{new Date(preview.impact.estimated_at).toLocaleString()}</span>
          <span>提交時重新計算；若應付金額增加，需重新預覽確認。</span>
        </> : <Money minor={preview.impact.amount_minor} currency={preview.impact.currency} />}
      </Space>,
      okText: immediate ? '確認升級' : '確認排程', cancelText: '返回檢查',
      onCancel: () => setRestoreFocus(true),
      onOk: () => {
        if (!canConfirmPreview(preview)) return
        sessionStorage.setItem(storageKey(id, actionID), JSON.stringify(intent))
        setPending(intent)
        submit.mutate(intent)
      },
    })
  }
  const onValuesChange = () => {
    formRevision.current += 1
    setStaleAttempt(null)
    if (preview || createPreview.isPending) {
      setPreview(null)
      setPayload(null)
      setPreviewInvalidated(true)
    }
  }
  if (subscription.isPending) return <Skeleton active />
  if (subscription.isError) return <ReadFailureWithRecovery title="訂閱無法載入" message={subscription.error.message} onRetryRead={() => { void subscription.refetch() }} hasPendingCommand={pending !== null} onRecoverCommand={() => { if (pending) submit.mutate(pending) }} recovering={submit.isPending} commandID={commandID} recoveryError={submit.isError ? submit.error.message : null} />
  const visibleCommand = command.isError && !canShowStaleRead(command.error) ? undefined : command.data

  return <div className="form-page">
    <Typography.Title level={2}>{immediate ? '立即升級 Pro' : '排程下期方案變更'}</Typography.Title>
    <Card>
      <Descriptions column={1} bordered size="small" items={[
        { key: 'id', label: '訂閱 ID', children: subscription.data.ID },
        { key: 'customer', label: '客戶', children: subscription.data.CustomerID },
        { key: 'price', label: '目前價格版本', children: subscription.data.PriceVersionID },
        { key: 'revision', label: 'Revision', children: subscription.data.Revision },
        { key: 'status', label: '狀態', children: subscription.data.Status },
      ]} />
      <Form form={form} key={id} layout="vertical" className="result-card" initialValues={initial} disabled={pending !== null || commandID !== null || (staleAttempt !== null && subscription.isFetching)} onValuesChange={onValuesChange} onFinish={(values: Input) => { setPreviewInvalidated(false); createPreview.mutate({ input: { quote_id: values.quote_id.trim(), fingerprint: values.fingerprint.trim(), revision: subscription.data.Revision }, revision: formRevision.current }) }}>
        <Form.Item label="已綁定的報價 ID" name="quote_id" rules={[{ required: true, message: '請輸入報價 ID' }]}><Input /></Form.Item>
        <Form.Item label="變更綁定 Fingerprint" name="fingerprint" rules={[{ required: true, message: '請輸入 Fingerprint' }]}><Input /></Form.Item>
        <Button id={`${actionID}-preview`} type="primary" htmlType="submit" loading={createPreview.isPending} disabled={subscription.data.Status !== 'active' || pending !== null || commandID !== null || (staleAttempt !== null && subscription.isFetching)}>{immediate ? '預覽立即升級' : '預覽下期變更'}</Button>
      </Form>
    </Card>
    {staleAttempt && <Alert type="warning" showIcon className="result-card" message="原方案變更預覽已失效，請檢查最新來源" description={<Descriptions column={1} size="small" items={[
      { key: 'quote', label: '保留的報價 ID', children: staleAttempt.payload.quote_id },
      { key: 'fingerprint', label: '保留的綁定 Fingerprint', children: staleAttempt.payload.fingerprint },
      { key: 'revision', label: '訂閱 revision', children: `${staleAttempt.payload.revision} → ${subscription.isFetching ? '重新讀取中…' : subscription.data.Revision}` },
      ...(staleAttempt.preview ? [{ key: 'amount', label: immediate ? '本期升級預估應付上限' : '報價金額', children: <Space wrap><span>原先：<Money minor={staleAttempt.preview.impact.amount_minor ?? staleAttempt.preview.impact.net_minor} currency={staleAttempt.preview.impact.currency} /></span><span>現在：{preview ? <Money minor={preview.impact.amount_minor ?? preview.impact.net_minor} currency={preview.impact.currency} /> : '尚無新預覽'}</span></Space> }] : []),
      { key: 'next', label: '下一步', children: subscription.isFetching ? '正在重新讀取訂閱。' : subscription.data.Revision !== staleAttempt.payload.revision ? '訂閱 revision 已改變，請建立對應新 revision 的變更報價與綁定。' : preview ? '請比較新舊預覽，再次確認後才會執行。' : createPreview.isError ? '來源已變更或報價不可用，請檢查錯誤並重新建立預覽。' : '正在建立新的預覽。' },
    ]} />} />}
    {previewInvalidated && !preview && !pending && !commandID && <Alert type="warning" showIcon className="result-card" message="變更輸入已修改，請重新預覽" />}
    {pending && !commandID && <Alert type="warning" showIcon className="result-card" message="原排程命令的結果尚未確認" description={<Button onClick={() => submit.mutate(pending)} loading={submit.isPending}>用原 request key 查詢</Button>} />}
    {createPreview.isError && !previewInvalidated && <Alert
      type="error"
      showIcon
      className="result-card"
      message={createPreview.error instanceof HttpError && createPreview.error.code === 'CHANGE_QUOTE_BINDING_MISMATCH' ? '報價與綁定資料不一致' : '無法建立預覽'}
      description={createPreview.error instanceof HttpError && createPreview.error.code === 'CHANGE_QUOTE_BINDING_MISMATCH'
        ? '報價 ID、綁定 Fingerprint 或訂閱不相符。請從正確的報價詳情重新進入方案變更。'
        : createPreview.error.message}
    />}
    {preview && <Card title="變更預覽" className="result-card">
      <PreviewWarnings preview={preview} expired={previewExpired} />
      <Descriptions column={1} bordered size="small" items={[
        { key: 'plan', label: '目標方案', children: preview.impact.plan_id ?? 'pro' },
        { key: 'price', label: '價格版本', children: preview.impact.price_version_id ?? preview.impact.target_price_version_id },
        { key: 'seats', label: '席次', children: preview.impact.seats },
        { key: 'amount', label: immediate ? '本期升級預估應付上限' : '報價金額', children: <Money minor={preview.impact.amount_minor ?? preview.impact.net_minor} currency={preview.impact.currency} /> },
        ...(immediate ? [
          { key: 'estimated', label: '估算時刻', children: new Date(preview.impact.estimated_at).toLocaleString() },
          { key: 'recalculate', label: '付款義務', children: '提交時重新計算；若應付金額增加，需重新預覽確認。' },
        ] : []),
        { key: 'effective', label: immediate ? '帳期結束' : '生效時間', children: new Date(preview.impact.effective_at ?? preview.impact.period_end).toLocaleString() },
        { key: 'expiry', label: '預覽有效至', children: new Date(preview.expires_at).toLocaleString() },
      ]} />
      <Button id={`${actionID}-confirm`} className="result-card" onClick={confirm} disabled={!canConfirmPreview(preview) || previewExpired || pending !== null}>{immediate ? '確認升級' : '確認排程'}</Button>
    </Card>}
    {submit.isError && <Alert type={submit.error instanceof HttpError && submit.error.code === 'PREVIEW_STALE' ? 'warning' : 'error'} showIcon className="result-card" message={submit.error instanceof HttpError && submit.error.code === 'PREVIEW_STALE' ? '原預覽已失效，請重新預覽' : submit.error instanceof HttpError && (submit.error.status === 400 || submit.error.status === 422) ? '命令未被接受，請檢查輸入' : '命令結果尚未確認'} description={submit.error.message} />}
    {commandID && <Card title="命令結果" className="result-card">
      {command.isPending && <Typography.Text>正在查詢命令狀態…</Typography.Text>}
      {command.isError && !visibleCommand && <CommandReadRecovery error={command.error} onRetry={() => void command.refetch()} onClear={() => { setCommandID(null); setPreview(null); setStaleAttempt(null); setPayload(null); setPreviewInvalidated(false); form.resetFields() }} />}
      {command.isError && visibleCommand && <Alert type="warning" showIcon className="result-card" message="無法更新命令狀態；以下是上次成功讀取的資料" description={<Space wrap><span>上次讀取：{new Date(command.dataUpdatedAt).toLocaleString()}</span><Button onClick={() => void command.refetch()}>重試</Button></Space>} />}
      {visibleCommand && <Descriptions column={1} bordered size="small" items={[
        { key: 'id', label: '命令 ID', children: <Typography.Text copyable>{visibleCommand.id}</Typography.Text> },
        { key: 'status', label: '狀態', children: visibleCommand.status },
        { key: 'schedule', label: '排程 ID', children: visibleCommand.result_refs?.schedule_id ?? '尚未建立' },
        { key: 'change', label: '升級 ID', children: visibleCommand.result_refs?.change_id ?? '未建立' },
        { key: 'invoice', label: '帳單 ID', children: visibleCommand.result_refs?.invoice_id ?? '未建立' },
        ...(immediate && visibleCommand.status === 'succeeded' ? [
          { key: 'estimated_amount', label: '預覽估算金額', children: visibleCommand.result_refs?.estimated_amount_minor ? <Money minor={visibleCommand.result_refs.estimated_amount_minor} currency={visibleCommand.result_refs.currency} /> : '歷史命令未記錄' },
          { key: 'pending_amount', label: '實際待付款義務', children: <Money minor={visibleCommand.result_refs?.pending_amount_minor ?? visibleCommand.result_refs?.net_minor} currency={visibleCommand.result_refs?.currency} /> },
        ] : []),
        { key: 'error', label: '錯誤', children: visibleCommand.error_code || '無' },
      ]} />}
      <Button className="result-card" href={`/admin/commands/${encodeURIComponent(commandID)}`}>開啟命令頁面</Button>
      {(visibleCommand?.status === 'succeeded' || visibleCommand?.status === 'failed') && <Button className="result-card" disabled={command.isError} onClick={() => { setCommandID(null); setPreview(null); setStaleAttempt(null); setPayload(null); setPreviewInvalidated(false); form.resetFields() }}>開始另一個變更</Button>}
    </Card>}
  </div>
}
