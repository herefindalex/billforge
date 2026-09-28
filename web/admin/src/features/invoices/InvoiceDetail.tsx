import { useQuery } from '@tanstack/react-query'
import { Alert, Button, Card, Descriptions, Empty, Result, Skeleton, Space, Table, Tag, Typography } from 'antd'
import { useNavigate, useParams } from 'react-router-dom'
import { api, HttpError, type InvoiceDetail as InvoiceRecord } from '../../api/client'
import Money from '../../components/Money'

function dateText(value?: string | null) {
  return value ? new Date(value).toLocaleString() : '無'
}

export default function InvoiceDetail() {
  const { id = '' } = useParams()
  const navigate = useNavigate()
  const query = useQuery({ queryKey: ['invoice', id], queryFn: () => api.invoice(id), enabled: id !== '' })
  if (query.isPending) return <Skeleton active />
  if (query.isError) {
    const status = query.error instanceof HttpError ? query.error.status : 0
    return <Result status={status === 404 ? '404' : status === 403 ? '403' : 'error'} title={status === 404 ? '找不到帳單' : status === 403 ? '沒有權限查看帳單' : '帳單無法載入'} subTitle={query.error.message} extra={<Button onClick={() => void query.refetch()}>重試</Button>} />
  }
  const invoice: InvoiceRecord = query.data.invoice
  const balance = invoice.Balance
  const money = (minor: string) => <Money minor={minor} currency={balance.Currency} />
  return <div className="form-page">
    <Space align="center" wrap>
      <Typography.Title level={2} style={{ margin: 0 }}>帳單詳情</Typography.Title>
      <Tag>{invoice.FinalizedAt ? '已核定' : '未核定'}</Tag>
      {query.isFetching && <Tag>更新中</Tag>}
    </Space>
    <Typography.Paragraph type="secondary">資料查詢時間：{new Date(query.data.observed_at).toLocaleString()}。金額為目前帳務快照；付款操作的狀態另行列示。</Typography.Paragraph>
    <Card title="帳單與應收" className="result-card" extra={<Button onClick={() => void query.refetch()}>重新整理</Button>}>
      <Descriptions bordered size="small" column={1} items={[
        { key: 'id', label: '帳單 ID', children: <Typography.Text copyable>{invoice.ID}</Typography.Text> },
        { key: 'subscription', label: '訂閱', children: <Button type="link" onClick={() => navigate(`/subscriptions/${encodeURIComponent(invoice.SubscriptionID)}`)}>{invoice.SubscriptionID}</Button> },
        { key: 'finalized', label: '核定時間', children: dateText(invoice.FinalizedAt) },
        { key: 'period', label: '帳期', children: invoice.Period ? `${invoice.Period.Index}：${dateText(invoice.Period.Start)} ～ ${dateText(invoice.Period.End)}` : '非週期帳單' },
        { key: 'due', label: '付款到期', children: invoice.Period ? dateText(invoice.Period.DueAt) : '依帳單條件' },
        { key: 'original', label: '原始金額', children: money(balance.OriginalMinor) },
        { key: 'reductions', label: '減額更正', children: money(balance.ReductionsMinor) },
        { key: 'obligation', label: '目前應收', children: money(balance.ObligationMinor) },
        { key: 'captured', label: '已確認收款', children: money(balance.GrossCapturedMinor) },
        { key: 'released', label: '已釋出款項', children: money(balance.ReleasedMinor) },
        { key: 'credit', label: '已應用 Credit', children: money(balance.CreditAppliedMinor) },
        { key: 'net', label: '淨已支付', children: money(balance.NetAppliedMinor) },
        { key: 'outstanding', label: '尚待支付', children: money(balance.OutstandingMinor) },
      ]} />
    </Card>
    <Card title="帳單明細" className="result-card">
      <Table rowKey={(_, index) => String(index)} dataSource={invoice.Lines} pagination={false} scroll={{ x: 'max-content' }} locale={{ emptyText: <Empty description="尚無明細" /> }} columns={[
        { title: '價格版本', dataIndex: 'price_version_id' },
        { title: '元件', dataIndex: 'component_code' },
        { title: '金額', dataIndex: 'amount_minor', render: money },
      ]} />
    </Card>
    {(invoice.Corrections.length > 0 || invoice.CorrectionsTruncated) && <Card title="減額更正" className="result-card" extra={<Button onClick={() => navigate(`/invoices/${encodeURIComponent(id)}/history/corrections`)}>查看完整歷史</Button>}>
      {invoice.CorrectionsTruncated && <Alert type="warning" showIcon message="僅顯示最近 100 筆減額更正" />}
      <Table rowKey="ID" dataSource={invoice.Corrections} pagination={{ pageSize: 10 }} scroll={{ x: 'max-content' }} columns={[
        { title: '更正 ID', dataIndex: 'ID', render: (value: string) => <Typography.Text copyable>{value}</Typography.Text> },
        { title: '理由', dataIndex: 'Reason' },
        { title: '來源類型', dataIndex: 'OriginKind', render: (value: string) => ({ admin_command: '人工減額命令', delayed_change: '延遲開通更正', unfulfilled_change: '未履行升級決議' })[value as 'admin_command' | 'delayed_change' | 'unfulfilled_change'] ?? '其他領域操作' },
        { title: '來源 ID', dataIndex: 'OriginID', render: (value: string, record: InvoiceRecord['Corrections'][number]) => record.OriginKind === 'admin_command'
          ? <Button type="link" onClick={() => navigate(`/commands/${encodeURIComponent(value)}`)}>{value}</Button>
          : <Typography.Text copyable>{value}</Typography.Text> },
        { title: '減額', dataIndex: 'ReductionMinor', render: money },
        { title: '減額前應收', dataIndex: 'PriorObligationMinor', render: money },
        { title: '減額後應收', dataIndex: 'NewObligationMinor', render: money },
        { title: '建立時間', dataIndex: 'CreatedAt', render: dateText },
      ]} />
    </Card>}
    {(invoice.CreditApplications.length > 0 || invoice.CreditApplicationsTruncated) && <Card title="抵扣本帳單的 Credit" className="result-card" extra={<Button onClick={() => navigate(`/invoices/${encodeURIComponent(id)}/history/applications`)}>查看完整歷史</Button>}>
      {invoice.CreditApplicationsTruncated && <Alert type="warning" showIcon message="僅顯示最近 100 筆 Credit 抵扣" />}
      <Table rowKey="ID" dataSource={invoice.CreditApplications} pagination={{ pageSize: 10 }} scroll={{ x: 'max-content' }} columns={[
        { title: '抵扣 ID', dataIndex: 'ID', render: (value: string) => <Typography.Text copyable>{value}</Typography.Text> },
        { title: '來源帳單', dataIndex: 'SourceInvoiceID', render: (value: string) => <Button type="link" onClick={() => navigate(`/invoices/${encodeURIComponent(value)}`)}>{value}</Button> },
        { title: 'Credit ID', dataIndex: 'GrantID', render: (value: string) => <Typography.Text copyable>{value}</Typography.Text> },
        { title: '抵扣金額', dataIndex: 'AmountMinor', render: money },
        { title: '建立時間', dataIndex: 'CreatedAt', render: dateText },
      ]} />
    </Card>}
    {(invoice.CreditGrants.length > 0 || invoice.CreditGrantsTruncated) && <Card title="本帳單釋出的 Credit" className="result-card" extra={<Button onClick={() => navigate(`/invoices/${encodeURIComponent(id)}/history/grants`)}>查看完整歷史</Button>}>
      {invoice.CreditGrantsTruncated && <Alert type="warning" showIcon message="僅顯示最近 100 筆 Credit" />}
      <Table rowKey="ID" dataSource={invoice.CreditGrants} pagination={{ pageSize: 10 }} scroll={{ x: 'max-content' }} columns={[
        { title: 'Credit ID', dataIndex: 'ID', render: (value: string) => <Typography.Text copyable>{value}</Typography.Text> },
        { title: '更正 ID', dataIndex: 'CorrectionID', render: (value: string) => <Typography.Text copyable>{value}</Typography.Text> },
        { title: '來源付款操作', dataIndex: 'SourceOperationID', render: (value: string) => <Typography.Text copyable>{value}</Typography.Text> },
        { title: '釋出金額', dataIndex: 'AmountMinor', render: money },
        { title: '建立時間', dataIndex: 'CreatedAt', render: dateText },
        { title: '操作', render: (_: unknown, record: InvoiceRecord['CreditGrants'][number]) => <Space>
          <Button type="link" onClick={() => navigate(`/credits/${encodeURIComponent(record.ID)}/apply`)}>抵扣帳單</Button>
          <Button type="link" onClick={() => navigate(`/credits/${encodeURIComponent(record.ID)}/refunds/new`)}>預留退款</Button>
        </Space> },
      ]} />
    </Card>}
    {(invoice.Refunds.length > 0 || invoice.RefundsTruncated) && <Card title="此來源額度的退款" className="result-card" extra={<Button onClick={() => navigate(`/invoices/${encodeURIComponent(id)}/history/refunds`)}>查看完整歷史</Button>}>
      {invoice.RefundsTruncated && <Alert type="warning" showIcon message="僅顯示最近 100 筆退款" />}
      <Table rowKey="ID" dataSource={invoice.Refunds} pagination={{ pageSize: 10 }} scroll={{ x: 'max-content' }} columns={[
        { title: '退款 ID', dataIndex: 'ID', render: (value: string) => <Typography.Text copyable>{value}</Typography.Text> },
        { title: 'Credit ID', dataIndex: 'GrantID', render: (value: string) => <Typography.Text copyable>{value}</Typography.Text> },
        { title: '金額', dataIndex: 'AmountMinor', render: (value: string, record) => <Money minor={value} currency={record.Currency} /> },
        { title: '狀態', dataIndex: 'Status', render: (value: string) => <Tag>{value}</Tag> },
        { title: '建立時間', dataIndex: 'CreatedAt', render: dateText },
        { title: '操作', render: (_: unknown, record: InvoiceRecord['Refunds'][number]) => <Space>
          {record.Status === 'created' && <Button type="link" onClick={() => navigate(`/refunds/${encodeURIComponent(record.ID)}/dispatch`)}>送出</Button>}
          {['submitted', 'unknown'].includes(record.Status) && <Button type="link" onClick={() => navigate(`/refunds/${encodeURIComponent(record.ID)}/reconcile`)}>查證</Button>}
        </Space> },
      ]} />
    </Card>}
    <Card title="付款操作" className="result-card">
      {invoice.PaymentsTruncated && <Alert type="warning" showIcon message="僅顯示前 100 筆付款操作" />}
      <Table rowKey="ID" dataSource={invoice.Payments} pagination={false} scroll={{ x: 'max-content' }} locale={{ emptyText: <Empty description="尚無付款操作" /> }} columns={[
        { title: '操作 ID', dataIndex: 'ID', render: (paymentID: string) => <Typography.Text copyable>{paymentID}</Typography.Text> },
        { title: '金額', dataIndex: 'AmountMinor', render: (minor: string, record) => <Money minor={minor} currency={record.Currency} /> },
        { title: '狀態', dataIndex: 'Status', render: (status: string) => <Tag>{status}</Tag> },
        { title: '操作', render: (_, record) => <Space>
          {record.Status === 'created' && <Button type="link" onClick={() => navigate(`/payments/${encodeURIComponent(record.ID)}/dispatch`)}>送出</Button>}
          {['submitted', 'unknown'].includes(record.Status) && <Button type="link" onClick={() => navigate(`/payments/${encodeURIComponent(record.ID)}/reconcile`)}>查證</Button>}
          {record.Status === 'definitively_failed' && <Button type="link" onClick={() => navigate(`/payments/${encodeURIComponent(record.ID)}/retry`)}>重試</Button>}
        </Space> },
      ]} />
    </Card>
    <Space wrap className="result-card">
      <Button disabled={!invoice.FinalizedAt} onClick={() => navigate(`/invoices/${encodeURIComponent(id)}/payments/new`)}>建立付款操作</Button>
      <Button disabled={!invoice.FinalizedAt} onClick={() => navigate(`/invoices/${encodeURIComponent(id)}/reductions/new`)}>新增減額更正</Button>
    </Space>
  </div>
}
