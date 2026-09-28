import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Alert, App as AntApp, Button, Card, Descriptions, Space, Typography } from 'antd'
import { useParams } from 'react-router-dom'
import { api, HttpError, type Command, type Preview, type Session } from '../../api/client'
import Money from '../../components/Money'
import PreviewWarnings from '../../components/PreviewWarnings'
import { canConfirmPreview, usePreviewExpired } from '../../components/usePreviewExpiry'
import { useStoredCommandID } from '../commands/useStoredCommandID'
import CommandReadRecovery from '../commands/CommandReadRecovery'

type Pending = { key: string; previewID: string; invoiceID?: string; amountMinor?: string; currency?: string }
type StaleRetry = { preview: Preview | null; invoiceID: string; amountMinor: string; currency: string }
function storageKey(id: string) { return `billforge:admin:retry-payment:${id}` }
function loadPending(id: string): Pending | null {
  try { const value = sessionStorage.getItem(storageKey(id)); return value ? JSON.parse(value) as Pending : null } catch { return null }
}

export default function RetryPayment({ session }: { session: Session }) {
  const { id = '' } = useParams()
  const { modal } = AntApp.useApp()
  const queryClient = useQueryClient()
  const [preview, setPreview] = useState<Preview | null>(null)
  const previewExpired = usePreviewExpired(preview?.expires_at)
  const [staleRetry, setStaleRetry] = useState<StaleRetry | null>(null)
  const [pending, setPending] = useState<Pending | null>(() => loadPending(id))
  const [commandID, setCommandID] = useStoredCommandID(session.actor_id, 'C08', id)
  const command = useQuery<Command>({ queryKey: ['command', commandID], queryFn: () => api.command(commandID!), enabled: commandID !== null, refetchInterval: (query) => query.state.data?.status === 'accepted' || query.state.data?.status === 'running' ? 1500 : false })
  const invoice = useQuery({
    queryKey: ['invoice', staleRetry?.invoiceID],
    queryFn: () => api.invoice(staleRetry!.invoiceID),
    enabled: Boolean(staleRetry?.invoiceID),
  })
  const createPreview = useMutation({ mutationFn: () => api.createPreview(session.csrf_token, 'C08', id, {}), onSuccess: setPreview })
  const submit = useMutation({
    mutationFn: (intent: Pending) => api.submitCommand(session.csrf_token, intent.key, { action_id: 'C08', target_id: id, preview_id: intent.previewID, payload: {} }),
    onSuccess: (result) => { setCommandID(result.id); setPending(null); setPreview(null); setStaleRetry(null); sessionStorage.removeItem(storageKey(id)) },
    onError: (error, intent) => {
      if (error instanceof HttpError && (error.status === 400 || error.status === 422 || error.code === 'PREVIEW_STALE')) {
        sessionStorage.removeItem(storageKey(id))
        setPending(null)
        setPreview(null)
        if (error.code === 'PREVIEW_STALE') {
          const invoiceID = preview?.impact.invoice_id ?? intent.invoiceID ?? ''
          setStaleRetry({
            preview,
            invoiceID,
            amountMinor: preview?.impact.retry_amount_minor ?? intent.amountMinor ?? '',
            currency: preview?.impact.currency ?? intent.currency ?? '',
          })
          if (invoiceID) void queryClient.invalidateQueries({ queryKey: ['invoice', invoiceID] })
          createPreview.mutate()
        } else {
          setStaleRetry(null)
        }
      }
    },
  })
  const confirm = () => {
    if (!preview || !canConfirmPreview(preview)) return
    const intent: Pending = { key: crypto.randomUUID(), previewID: preview.preview_id, invoiceID: preview.impact.invoice_id, amountMinor: preview.impact.retry_amount_minor, currency: preview.impact.currency }
    modal.confirm({
      title: '確認重試付款',
      content: <Space direction="vertical"><span>原付款操作：{id}</span><span>帳單：{preview.impact.invoice_id}</span><span>新付款金額：<Money minor={preview.impact.retry_amount_minor} currency={preview.impact.currency} /></span></Space>,
      okText: '建立重試', cancelText: '返回檢查',
      onOk: () => {
        if (!canConfirmPreview(preview)) return
        sessionStorage.setItem(storageKey(id), JSON.stringify(intent))
        setPending(intent)
        submit.mutate(intent)
      },
    })
  }
  const currentInvoice = invoice.data?.invoice
  const currentOperation = currentInvoice?.Payments.find((payment) => payment.ID === id)
  const otherOperations = currentInvoice?.Payments.filter((payment) => payment.ID !== id) ?? []
  return <div className="form-page">
    <Typography.Title level={2}>重試確定失敗的付款</Typography.Title>
    <Card><Typography.Paragraph>原付款操作 ID：<Typography.Text copyable>{id}</Typography.Text></Typography.Paragraph><Button type="primary" onClick={() => createPreview.mutate()} loading={createPreview.isPending} disabled={pending !== null || commandID !== null}>預覽重試</Button></Card>
    {staleRetry && <Alert type="warning" showIcon className="result-card" message="原重試預覽已失效，請檢查帳單的最新狀態" description={<Space direction="vertical" style={{ width: '100%' }}>
      <Descriptions column={1} size="small" items={[
        { key: 'amount', label: '重試金額', children: <Space wrap><span>原先：{staleRetry.amountMinor ? <Money minor={staleRetry.amountMinor} currency={staleRetry.currency} /> : '未知'}</span><span>現在：{preview ? <Money minor={preview.impact.retry_amount_minor} currency={preview.impact.currency} /> : '尚無新預覽'}</span></Space> },
        { key: 'balance', label: '目前未清餘額', children: currentInvoice ? <Money minor={currentInvoice.Balance.OutstandingMinor} currency={currentInvoice.Balance.Currency} /> : invoice.isError ? '讀取失敗' : staleRetry.invoiceID ? '讀取中…' : '缺少帳單 ID' },
        { key: 'status', label: '原付款操作狀態', children: `${staleRetry.preview?.source_versions.failed_operation_status ?? '未知'} → ${currentOperation?.Status ?? (invoice.isError ? '讀取失敗' : currentInvoice ? currentInvoice.PaymentsTruncated ? '列表已截斷' : '未找到' : staleRetry.invoiceID ? '讀取中…' : '缺少帳單 ID')}` },
        { key: 'others', label: '其他付款操作', children: currentInvoice ? `${otherOperations.slice(0, 5).map((payment) => `${payment.ID}（${payment.Status}）`).join('、') || '無'}${otherOperations.length > 5 || currentInvoice.PaymentsTruncated ? '；列表已截斷，請查看帳單詳情' : ''}` : invoice.isError ? '讀取失敗' : staleRetry.invoiceID ? '讀取中…' : '缺少帳單 ID' },
        { key: 'next', label: '下一步', children: preview ? '請比較新舊金額，再次確認後才會建立重試。' : createPreview.isError ? '目前無法建立新預覽；請先檢查既有付款操作。' : '正在重建預覽。' },
      ]} />
      {staleRetry.invoiceID && <Button href={`/admin/invoices/${encodeURIComponent(staleRetry.invoiceID)}`}>查看帳單詳情</Button>}
      {invoice.isError && <Button onClick={() => void invoice.refetch()}>重新讀取帳單</Button>}
    </Space>} />}
    {pending && !commandID && <Alert type="warning" showIcon className="result-card" message="原重試命令的結果尚未確認" description={<Button onClick={() => submit.mutate(pending)} loading={submit.isPending}>用原 request key 查詢</Button>} />}
    {createPreview.isError && <Alert type="error" showIcon className="result-card" message="無法建立預覽" description={createPreview.error.message} />}
    {preview && <Card title="重試預覽" className="result-card">
      <PreviewWarnings preview={preview} expired={previewExpired} />
      <Descriptions column={1} bordered size="small" items={[
        { key: 'invoice', label: '帳單 ID', children: preview.impact.invoice_id },
        { key: 'amount', label: '重試金額', children: <Money minor={preview.impact.retry_amount_minor} currency={preview.impact.currency} /> },
        { key: 'expiry', label: '預覽有效至', children: new Date(preview.expires_at).toLocaleString() },
      ]} />
      <Button className="result-card" onClick={confirm} disabled={!canConfirmPreview(preview) || previewExpired || pending !== null}>確認重試</Button>
    </Card>}
    {submit.isError && <Alert type={submit.error instanceof HttpError && submit.error.code === 'PREVIEW_STALE' ? 'warning' : 'error'} showIcon className="result-card" message={submit.error instanceof HttpError && submit.error.code === 'PREVIEW_STALE' ? '原預覽已失效，請重新預覽' : submit.error instanceof HttpError && (submit.error.status === 400 || submit.error.status === 422) ? '命令未被接受，請檢查輸入' : '命令結果尚未確認'} description={submit.error.message} />}
    {commandID && <Card title="命令結果" className="result-card">
      {command.isPending && <Typography.Text>正在查詢命令狀態…</Typography.Text>}
      {command.isError && <CommandReadRecovery error={command.error} onRetry={() => void command.refetch()} onClear={() => { setCommandID(null); setPreview(null); setStaleRetry(null) }} />}
      {command.data && <Descriptions column={1} bordered size="small" items={[
        { key: 'id', label: '命令 ID', children: <Typography.Text copyable>{command.data.id}</Typography.Text> },
        { key: 'status', label: '狀態', children: command.data.status },
        { key: 'operation', label: '新付款操作 ID', children: command.data.result_refs?.operation_id ?? '尚未建立' },
        { key: 'error', label: '錯誤', children: command.data.error_code || '無' },
      ]} />}
      <Button className="result-card" href={`/admin/commands/${encodeURIComponent(commandID)}`}>開啟命令頁面</Button>
      {command.data?.status === 'failed' && <Button className="result-card" disabled={command.isError} onClick={() => { setCommandID(null); setPreview(null); setStaleRetry(null) }}>重新預覽重試</Button>}
    </Card>}
  </div>
}
