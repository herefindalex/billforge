import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Alert, App as AntApp, Button, Card, Descriptions, Skeleton, Space, Typography } from 'antd'
import { useNavigate, useParams } from 'react-router-dom'
import { api, HttpError, previewSourceString, type Command, type Preview, type Session } from '../../api/client'
import Money from '../../components/Money'
import PreviewWarnings from '../../components/PreviewWarnings'
import ReadFailureWithRecovery from '../../components/ReadFailureWithRecovery'
import { canConfirmPreview, usePreviewExpired } from '../../components/usePreviewExpiry'
import { useStoredCommandID } from '../commands/useStoredCommandID'
import CommandReadRecovery from '../commands/CommandReadRecovery'

type Pending = { key: string; previewID: string; quoteID: string; fingerprint: string }
type StaleAcceptance = { fingerprint: string }

function pendingStorage(quoteID: string) { return `billforge:admin:accept-quote:${quoteID}` }

function loadPending(quoteID: string): Pending | null {
  try {
    const value = sessionStorage.getItem(pendingStorage(quoteID))
    return value ? JSON.parse(value) as Pending : null
  } catch { return null }
}

export default function AcceptQuote({ session }: { session: Session }) {
  const { id = '' } = useParams()
  const navigate = useNavigate()
  const { modal } = AntApp.useApp()
  const queryClient = useQueryClient()
  const [preview, setPreview] = useState<Preview | null>(null)
  const previewExpired = usePreviewExpired(preview?.expires_at)
  const [staleAcceptance, setStaleAcceptance] = useState<StaleAcceptance | null>(null)
  const [focusStaleAcceptance, setFocusStaleAcceptance] = useState(false)
  const [focusPreviewAfterRejection, setFocusPreviewAfterRejection] = useState(false)
  const [pending, setPending] = useState<Pending | null>(() => loadPending(id))
  const [commandID, setCommandID] = useStoredCommandID(session.actor_id, 'C02', id)
  const quote = useQuery({ queryKey: ['quote', id], queryFn: () => api.quote(id), enabled: id !== '' })
  const command = useQuery<Command>({
    queryKey: ['command', commandID],
    queryFn: () => api.command(commandID!), enabled: commandID !== null,
    refetchInterval: (query) => {
      const status = query.state.data?.status
      return status === 'accepted' || status === 'running' || status === 'waiting_verification' ? 1500 : false
    },
  })
  const createPreview = useMutation({
    mutationFn: () => api.createPreview(session.csrf_token, 'C02', id, { fingerprint: quote.data!.Fingerprint }),
    onSuccess: setPreview,
  })
  const submit = useMutation({
    mutationFn: (intent: Pending) => api.submitCommand(session.csrf_token, intent.key, {
      action_id: 'C02', target_id: intent.quoteID, preview_id: intent.previewID,
      payload: { fingerprint: intent.fingerprint },
    }),
    onSuccess: (result) => {
      setCommandID(result.id)
      setPreview(null)
      setStaleAcceptance(null)
      sessionStorage.removeItem(pendingStorage(id))
      setPending(null)
      void queryClient.invalidateQueries({ queryKey: ['quote', id] })
      void queryClient.invalidateQueries({ queryKey: ['resource', 'quotes'] })
      void queryClient.invalidateQueries({ queryKey: ['resource', 'subscriptions'] })
      void queryClient.invalidateQueries({ queryKey: ['resource', 'invoices'] })
      void queryClient.invalidateQueries({ queryKey: ['overview'] })
      void queryClient.invalidateQueries({ queryKey: ['commands'] })
    },
    onError: (error, intent) => {
      if (error instanceof HttpError && (error.status === 400 || error.status === 422 || error.code === 'PREVIEW_STALE')) {
        sessionStorage.removeItem(pendingStorage(id))
        setPending(null)
        setPreview(null)
        if (error.code === 'PREVIEW_STALE') {
          setStaleAcceptance({ fingerprint: intent.fingerprint })
          void quote.refetch().finally(() => setFocusStaleAcceptance(true))
        } else {
          setStaleAcceptance(null)
          setFocusPreviewAfterRejection(true)
        }
      }
    },
  })
  useEffect(() => {
    if (!focusStaleAcceptance || !staleAcceptance || quote.isFetching) return
    const alert = document.getElementById('stale-quote-acceptance')
    if (!alert) return
    const frame = requestAnimationFrame(() => {
      if (alert.isConnected) {
        alert.focus()
        setFocusStaleAcceptance(false)
      }
    })
    return () => cancelAnimationFrame(frame)
  }, [focusStaleAcceptance, staleAcceptance, quote.isFetching])
  useEffect(() => {
    if (!focusPreviewAfterRejection || quote.isFetching || pending !== null) return
    const button = document.getElementById('quote-accept-preview') as HTMLButtonElement | null
    if (!button || button.disabled) return
    const frame = requestAnimationFrame(() => {
      if (button.isConnected && !button.disabled) {
        button.focus()
        setFocusPreviewAfterRejection(false)
      }
    })
    return () => cancelAnimationFrame(frame)
  }, [focusPreviewAfterRejection, quote.isFetching, pending, preview])
  const confirm = () => {
    if (!preview || !quote.data || !canConfirmPreview(preview)) return
    const fingerprint = previewSourceString(preview, 'quote_fingerprint')
    if (!fingerprint) return
    const intent: Pending = { key: crypto.randomUUID(), quoteID: id, previewID: preview.preview_id, fingerprint }
    modal.confirm({
      title: '確認接受報價',
      content: <Space direction="vertical"><span>報價：{id}</span><span>將建立付款義務：</span><Money minor={preview.impact.amount_minor} currency={preview.impact.currency} /><span>價格版本：{preview.impact.price_version_id}</span>{preview.impact.contract_version_id && <span>合約版本：{preview.impact.contract_version_id}（Net30）</span>}</Space>,
      okText: '確認接受', cancelText: '返回檢查',
      onOk: () => {
        if (!canConfirmPreview(preview)) return
        sessionStorage.setItem(pendingStorage(id), JSON.stringify(intent))
        setPending(intent)
        submit.mutate(intent)
      },
    })
  }
  if (quote.isPending) return <Skeleton active />
  if (quote.isError) return <ReadFailureWithRecovery title="報價無法載入" message={quote.error.message} onRetryRead={() => { void quote.refetch() }} hasPendingCommand={pending !== null} onRecoverCommand={() => { if (pending) submit.mutate(pending) }} recovering={submit.isPending} commandID={commandID} recoveryError={submit.isError ? submit.error.message : null} />
  return <div className="form-page">
    <Typography.Title level={2}>接受報價</Typography.Title>
    <Card>
      <Descriptions column={1} bordered size="small" items={[
        { key: 'id', label: '報價 ID', children: quote.data.ID },
        { key: 'customer', label: '客戶', children: quote.data.CustomerID },
        { key: 'price', label: '價格版本', children: quote.data.PriceVersionID },
        { key: 'contract', label: '合約版本', children: quote.data.ContractVersionID || '非合約報價' },
        { key: 'terms', label: '付款條款', children: quote.data.ContractVersionID ? 'Net30，到期後才送出收款' : '一般付款流程' },
        { key: 'amount', label: '報價金額', children: <Money minor={quote.data.AmountMinor} currency={quote.data.Currency} /> },
        ...(quote.data.ChangeMode ? [{ key: 'change_mode', label: '變更方式', children: quote.data.ChangeMode === 'immediate' ? '立即升級' : '下期變更' }] : []),
        { key: 'due_now', label: '報價接受時現在應付', children: quote.data.DueNowMinor === null ? '待立即升級預覽估算' : <Money minor={quote.data.DueNowMinor} currency={quote.data.Currency} /> },
        { key: 'next_full_term', label: '下一整期固定承諾（依此報價）', children: <Money minor={quote.data.NextFullTermFixedMinor} currency={quote.data.Currency} /> },
        { key: 'usage_rate', label: '用量費率', children: quote.data.UsageMeterID ? `包含 ${quote.data.IncludedQuantity} ${quote.data.UsageMeterID}；超額每 ${quote.data.UsageMeterID} ${quote.data.UsageRateNum}/${quote.data.UsageRateDen} 最小貨幣單位` : '未設定用量收費' },
        { key: 'tax', label: '稅務', children: '未支援；報價未包含稅額' },
        { key: 'expiry', label: '報價到期', children: new Date(quote.data.ExpiresAt).toLocaleString() },
        { key: 'status', label: '狀態', children: quote.data.Accepted ? '已接受' : '尚未接受' },
      ]} />
      {quote.data.ChangeMode && <Alert type="info" showIcon className="result-card" message="這是現有訂閱的變更報價" description="請在原訂閱執行方案變更；此報價不能作為新購接受。" />}
      {!quote.data.Accepted && !commandID && quote.data.ChangeMode && <Button className="result-card" type="primary" onClick={() => navigate(`/subscriptions/${encodeURIComponent(quote.data.ChangeSubscriptionID)}/${quote.data.ChangeMode === 'immediate' ? 'upgrade' : 'schedule-plan'}`, { state: { quote_id: id, fingerprint: quote.data.BindingFingerprint } })}>{quote.data.ChangeMode === 'immediate' ? '前往立即升級' : '前往下期變更'}</Button>}
      {!quote.data.Accepted && !commandID && !quote.data.ChangeMode && <Button id="quote-accept-preview" className="result-card" type="primary" onClick={() => createPreview.mutate()} loading={createPreview.isPending} disabled={pending !== null || (staleAcceptance !== null && quote.isFetching)}>預覽接受</Button>}
    </Card>
    {staleAcceptance && <div id="stale-quote-acceptance" tabIndex={-1} aria-label="原接受預覽已失效，請檢查報價的最新狀態" className="result-card"><Alert type="warning" showIcon message="原接受預覽已失效，請檢查報價的最新狀態" description={<Descriptions column={1} size="small" items={[
      { key: 'intent', label: '原操作意圖', children: '接受報價' },
      { key: 'accepted', label: '接受狀態', children: `尚未接受 → ${quote.isFetching ? '重新讀取中…' : quote.data.Accepted ? '已接受' : '尚未接受'}` },
      { key: 'fingerprint', label: '來源 Fingerprint', children: `${staleAcceptance.fingerprint} → ${quote.isFetching ? '重新讀取中…' : quote.data.Fingerprint}` },
      { key: 'next', label: '下一步', children: quote.isFetching ? '正在確認最新狀態…' : quote.data.Accepted ? '報價已接受；請檢查既有訂閱與命令。' : '來源或預覽期限已變更，請重新預覽後再次確認。' },
    ]} />} /></div>}
    {pending && !commandID && <Alert type="warning" showIcon className="result-card" message="原命令的結果尚未確認" description={<Space direction="vertical"><span>重試會使用相同 request key 查詢同一命令。</span><Button onClick={() => submit.mutate(pending)} loading={submit.isPending}>查詢原命令</Button></Space>} />}
      {createPreview.isError && <Alert
        type="error"
        showIcon
        className="result-card"
        message={createPreview.error instanceof HttpError && createPreview.error.code === 'QUOTE_EXPIRED' ? '報價已過期' : '無法建立預覽'}
        description={createPreview.error.message}
        action={createPreview.error instanceof HttpError && createPreview.error.code === 'QUOTE_EXPIRED' ? <Button onClick={() => {
          const params = new URLSearchParams({ customer_id: quote.data.CustomerID })
          if (quote.data.ContractVersionID) params.set('contract_version_id', quote.data.ContractVersionID)
          navigate(`/quotes/new?${params.toString()}`)
        }}>建立新報價</Button> : undefined}
      />}
    {preview && !commandID && <Card title="操作預覽" className="result-card">
      <PreviewWarnings preview={preview} expired={previewExpired} />
      <Descriptions column={1} bordered size="small" items={[
        { key: 'amount', label: '將建立的付款義務', children: <Money minor={preview.impact.amount_minor} currency={preview.impact.currency} /> },
        ...(preview.impact.payment_terms === 'net30' ? [
          { key: 'due_now', label: '接受時應付', children: <Money minor={preview.impact.due_now_minor} currency={preview.impact.currency} /> },
          { key: 'due_at', label: '首期預計到期日', children: new Date(preview.impact.first_due_at_estimated).toLocaleString() },
        ] : []),
        { key: 'price', label: '價格版本', children: preview.impact.price_version_id },
        { key: 'contract', label: '合約版本', children: preview.impact.contract_version_id || '非合約報價' },
        { key: 'terms', label: '付款條款', children: preview.impact.payment_terms === 'net30' ? 'Net30，到期後才送出收款' : '一般付款流程' },
        ...(previewSourceString(preview, 'contract_checksum') ? [{ key: 'contract_checksum', label: '合約 checksum', children: <Typography.Text copyable>{previewSourceString(preview, 'contract_checksum')}</Typography.Text> }] : []),
        { key: 'checksum', label: '價格 checksum', children: <Typography.Text copyable>{previewSourceString(preview, 'price_checksum') ?? '未知'}</Typography.Text> },
        { key: 'expiry', label: '預覽有效至', children: new Date(preview.expires_at).toLocaleString() },
      ]} />
      <Button className="result-card" type="primary" danger onClick={confirm} disabled={!canConfirmPreview(preview) || !previewSourceString(preview, 'quote_fingerprint') || previewExpired || pending !== null}>確認接受並建立付款義務</Button>
    </Card>}
    {submit.isError && <Alert type={submit.error instanceof HttpError && submit.error.code === 'PREVIEW_STALE' ? 'warning' : 'error'} showIcon className="result-card" message={submit.error instanceof HttpError && submit.error.code === 'PREVIEW_STALE' ? '原預覽已失效，請重新預覽' : submit.error instanceof HttpError && (submit.error.status === 400 || submit.error.status === 422) ? '命令未被接受，請檢查輸入' : '操作結果尚未確認'} description={submit.error.message} />}
    {commandID && <Card title="命令結果" className="result-card">
      {command.isPending && <Typography.Text>正在查詢命令狀態…</Typography.Text>}
      {command.isError && <CommandReadRecovery error={command.error} onRetry={() => void command.refetch()} onClear={() => { setCommandID(null); setPreview(null); setStaleAcceptance(null); void quote.refetch() }} />}
      {command.data && <Descriptions column={1} bordered size="small" items={[
        { key: 'id', label: '命令 ID', children: <Typography.Text copyable>{command.data.id}</Typography.Text> },
        { key: 'status', label: '狀態', children: command.data.status },
        { key: 'subscription', label: '訂閱 ID', children: command.data.result_refs?.subscription_id ?? '尚未產生' },
        { key: 'invoice', label: '帳單 ID', children: command.data.result_refs?.invoice_id ?? '尚未產生' },
        { key: 'operation', label: '付款操作 ID', children: command.data.result_refs?.operation_id ?? '尚未產生' },
        ...(command.data.result_refs?.first_due_at ? [
          { key: 'due_now', label: '目前應付', children: <Money minor={command.data.result_refs.due_now_minor} currency={command.data.result_refs.currency} /> },
          { key: 'due_at', label: '首期到期日', children: new Date(command.data.result_refs.first_due_at).toLocaleString() },
        ] : []),
        { key: 'error', label: '錯誤', children: command.data.error_code || '無' },
      ]} />}
      <Button className="result-card" href={`/admin/commands/${encodeURIComponent(commandID)}`}>開啟命令頁面</Button>
      {command.data?.status === 'failed' && !quote.data.Accepted && <Button className="result-card" disabled={command.isError} onClick={() => { setCommandID(null); setPreview(null); setStaleAcceptance(null); void quote.refetch() }}>依最新報價重新預覽</Button>}
    </Card>}
  </div>
}
