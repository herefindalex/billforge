import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Alert, Button, Card, Checkbox, Descriptions, Form, Input, Select, Space, Typography } from 'antd'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { api, HttpError, type Command, type Session } from '../../api/client'
import { isNonNegativeInt64String } from '../../api/validation'
import { useStoredCommandID } from '../commands/useStoredCommandID'
import CommandReadRecovery from '../commands/CommandReadRecovery'

const pendingKey = 'billforge:admin:create-quote:pending'
type QuoteInput = { customer_id: string; plan_id?: string; contract_version_id?: string; cohort?: string; seats: string; change_subscription_id?: string; mode?: string; revision?: string }
type Pending = { key: string; payload: QuoteInput }

function readPending(): Pending | null {
  try {
    const text = sessionStorage.getItem(pendingKey)
    if (!text) return null
    const parsed = JSON.parse(text) as Pending
    return parsed.key && parsed.payload ? parsed : null
  } catch { return null }
}

export default function CreateQuote({ session }: { session: Session }) {
  const [form] = Form.useForm<QuoteInput>()
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const [changeQuote, setChangeQuote] = useState(false)
 const [quoteKind, setQuoteKind] = useState<'plan' | 'contract'>(searchParams.has('contract_version_id') ? 'contract' : 'plan')
  const queryClient = useQueryClient()
  const [pending, setPending] = useState<Pending | null>(readPending)
  const [commandID, setCommandID] = useStoredCommandID(session.actor_id, 'C01')
  const command = useQuery<Command>({
    queryKey: ['command', commandID],
    queryFn: () => api.command(commandID!),
    enabled: commandID !== null,
    refetchInterval: (query) => {
      const status = query.state.data?.status
      return status === 'accepted' || status === 'running' || status === 'waiting_verification' ? 1500 : false
    },
  })
  const submit = useMutation({
    mutationFn: ({ key, payload }: Pending) => api.submitCommand(session.csrf_token, key, { action_id: 'C01', payload }),
    onSuccess: (result) => {
      setCommandID(result.id)
      sessionStorage.removeItem(pendingKey)
      setPending(null)
      void queryClient.invalidateQueries({ queryKey: ['overview'] })
      void queryClient.invalidateQueries({ queryKey: ['resource', 'quotes'] })
      void queryClient.invalidateQueries({ queryKey: ['commands'] })
    },
    onError: (error) => {
      // The server returns 400/422 before command admission. A lost response
      // or a 5xx still needs the original key for safe recovery.
      if (error instanceof HttpError && (error.status === 400 || error.status === 422)) {
        sessionStorage.removeItem(pendingKey)
        setPending(null)
      }
    },
  })
  useEffect(() => {
    if (!(submit.error instanceof HttpError) || (submit.error.status !== 400 && submit.error.status !== 422) || pending !== null) return
    const warning = document.getElementById('quote-input-rejection')
    if (!warning) return
    const frame = requestAnimationFrame(() => { if (warning.isConnected) warning.focus() })
    return () => cancelAnimationFrame(frame)
  }, [submit.error, pending])
  const send = (values: QuoteInput) => {
    if (pending || commandID) return
    const payload: QuoteInput = {
      customer_id: values.customer_id.trim(),
      seats: values.seats?.trim() || (quoteKind === 'contract' ? '1' : '0'),
    }
    if (quoteKind === 'contract') {
      payload.contract_version_id = values.contract_version_id?.trim()
    } else {
      payload.plan_id = values.plan_id?.trim()
      payload.cohort = values.cohort?.trim() || 'default'
    }
    if (quoteKind === 'plan' && changeQuote) {
      payload.change_subscription_id = values.change_subscription_id?.trim()
      payload.mode = values.mode
      payload.revision = values.revision?.trim()
    }
    const intent: Pending = { key: crypto.randomUUID(), payload }
    sessionStorage.setItem(pendingKey, JSON.stringify(intent))
    setPending(intent)
    submit.mutate(intent)
  }
  return <div className="form-page">
    <Typography.Title level={2}>建立報價</Typography.Title>
    <Typography.Paragraph type="secondary">報價會鎖定當前可用的價格版本，有效期限及金額由伺服器計算。</Typography.Paragraph>
    {pending && !commandID && <Alert
      type="warning" showIcon className="form-alert"
      message="有一筆送出結果尚未確認"
      description={<Space direction="vertical"><span>可用原 request key 查詢同一筆操作，避免建立第二筆報價。</span><Button onClick={() => submit.mutate(pending)} loading={submit.isPending}>查詢原操作</Button></Space>}
    />}
    <Card>
      <Form form={form} layout="vertical" initialValues={{ customer_id: searchParams.get('customer_id') ?? '', contract_version_id: searchParams.get('contract_version_id') ?? '', cohort: 'default', seats: searchParams.has('contract_version_id') ? '1' : '0' }} onFinish={send} disabled={pending !== null || commandID !== null}>
        <Form.Item label="客戶 ID" name="customer_id" rules={[{ required: true, message: '請輸入客戶 ID' }]}><Input autoComplete="off" /></Form.Item>
        <Form.Item label="報價種類"><Select aria-label="報價種類" value={quoteKind} onChange={(value: 'plan' | 'contract') => { setQuoteKind(value); setChangeQuote(false); form.setFieldValue('seats', value === 'contract' ? '1' : '0') }} options={[{ value: 'plan', label: '一般方案' }, { value: 'contract', label: '企業合約' }]} /></Form.Item>
        {quoteKind === 'plan' ? <>
          <Form.Item label="方案 ID" name="plan_id" rules={[{ required: true, message: '請輸入方案 ID' }]}><Input placeholder="basic 或 pro" /></Form.Item>
          <Form.Item label="Cohort" name="cohort" rules={[{ required: true, message: '請輸入 cohort' }]}><Input /></Form.Item>
        </> : <>
          <Form.Item label="合約版本 ID" name="contract_version_id" rules={[{ required: true, message: '請輸入已發布的合約版本 ID' }]}><Input /></Form.Item>
          <Typography.Paragraph type="secondary">合約報價依已發布的合約版本計算固定費與席次費，接受後建立 Net30 帳單。</Typography.Paragraph>
        </>}
        <Form.Item label="席次" name="seats" rules={[{ required: true, message: '請輸入席次' }, { pattern: quoteKind === 'contract' ? /^[1-9]\d*$/ : /^\d+$/, message: quoteKind === 'contract' ? '請輸入正整數' : '請輸入非負整數' }, { validator: async (_: unknown, value: string | undefined) => {
          if (value && !isNonNegativeInt64String(value.trim())) throw new Error('席次不可超過 int64 上限')
        } }]}><Input inputMode="numeric" /></Form.Item>
        {quoteKind === 'plan' && <Checkbox checked={changeQuote} onChange={(event) => setChangeQuote(event.target.checked)}>這是現有訂閱的變更報價</Checkbox>}
        {quoteKind === 'plan' && changeQuote && <>
          <Form.Item label="訂閱 ID" name="change_subscription_id" rules={[{ required: true, message: '請輸入訂閱 ID' }]}><Input /></Form.Item>
          <Form.Item label="變更方式" name="mode" rules={[{ required: true, message: '請選擇變更方式' }]}><Select options={[{ value: 'next_period', label: '下期變更' }, { value: 'immediate', label: '立即升級' }]} /></Form.Item>
          <Form.Item label="目前 Revision" name="revision" rules={[{ required: true, message: '請輸入 Revision' }, { pattern: /^[1-9]\d*$/, message: '請輸入正整數' }, { validator: async (_: unknown, value: string | undefined) => {
            if (value && !isNonNegativeInt64String(value.trim())) throw new Error('Revision 不可超過 int64 上限')
          } }]}><Input inputMode="numeric" /></Form.Item>
        </>}
        {submit.isError && <div id="quote-input-rejection" tabIndex={-1} aria-label="報價輸入未被接受" className="form-alert"><Alert type="error" showIcon message={submit.error instanceof HttpError && (submit.error.status === 400 || submit.error.status === 422) ? '輸入未被接受，請修改後重試' : '操作結果尚未確認'} description={submit.error.message} /></div>}
        <Button type="primary" htmlType="submit" loading={submit.isPending} disabled={pending !== null || commandID !== null}>建立報價</Button>
      </Form>
    </Card>
    {commandID && <Card title="命令結果" className="result-card">
      {command.isPending && <Typography.Text>正在查詢命令狀態…</Typography.Text>}
      {command.isError && <CommandReadRecovery error={command.error} onRetry={() => void command.refetch()} onClear={() => { setCommandID(null); setQuoteKind('plan'); setChangeQuote(false); form.resetFields() }} />}
      {command.data && <Descriptions column={1} bordered size="small" items={[
        { key: 'id', label: '命令 ID', children: <Typography.Text copyable>{command.data.id}</Typography.Text> },
        { key: 'status', label: '狀態', children: command.data.status },
        { key: 'quote', label: '報價 ID', children: command.data.result_refs?.quote_id ?? '尚未產生' },
        { key: 'price', label: '價格版本', children: command.data.result_refs?.price_version_id ?? '尚未確定' },
        { key: 'contract', label: '合約版本', children: command.data.result_refs?.contract_version_id ?? '非合約報價' },
        { key: 'binding', label: '變更綁定 Fingerprint', children: command.data.result_refs?.binding_fingerprint ? <Typography.Text copyable>{command.data.result_refs.binding_fingerprint}</Typography.Text> : '非變更報價' },
        { key: 'error', label: '錯誤', children: command.data.error_code || '無' },
      ]} />}
      {command.data?.status === 'succeeded' && command.data.result_refs?.mode === 'next_period' && <Button className="result-card" disabled={command.isError} onClick={() => navigate(`/subscriptions/${encodeURIComponent(command.data!.result_refs!.change_subscription_id)}/schedule-plan`, { state: { quote_id: command.data!.result_refs!.quote_id, fingerprint: command.data!.result_refs!.binding_fingerprint } })}>前往排程下期變更</Button>}
      {command.data?.status === 'succeeded' && command.data.result_refs?.mode === 'immediate' && <Button className="result-card" disabled={command.isError} onClick={() => navigate(`/subscriptions/${encodeURIComponent(command.data!.result_refs!.change_subscription_id)}/upgrade`, { state: { quote_id: command.data!.result_refs!.quote_id, fingerprint: command.data!.result_refs!.binding_fingerprint } })}>前往立即升級</Button>}
      {command.data?.status === 'succeeded' && command.data.result_refs?.quote_id && !command.data.result_refs?.mode && <Button className="result-card" disabled={command.isError} onClick={() => navigate(`/quotes/${encodeURIComponent(command.data!.result_refs!.quote_id)}/accept`)}>前往接受報價</Button>}
      <Button className="result-card" href={`/admin/commands/${encodeURIComponent(commandID)}`}>開啟命令頁面</Button>
      {(command.data?.status === 'succeeded' || command.data?.status === 'failed') && <Button className="result-card" disabled={command.isError} onClick={() => { setCommandID(null); setQuoteKind('plan'); setChangeQuote(false); form.resetFields() }}>建立另一筆報價</Button>}
    </Card>}
  </div>
}
