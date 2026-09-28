import { useQuery } from '@tanstack/react-query'
import { Alert, Button, Card, Descriptions, Empty, Form, Input, Result, Skeleton, Space, Table, Tag, Typography } from 'antd'
import { useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { api, HttpError, type MigrationThresholds } from '../../api/client'
import { isNonNegativeInt64String } from '../../api/validation'

const int64Rule = { validator: async (_: unknown, value: string | undefined) => {
  if (value && !isNonNegativeInt64String(value.trim())) throw new Error('不可超過 int64 上限')
} }

function dateText(value: string) {
  return new Date(value).toLocaleString()
}

function yesNo(value: boolean) {
  return <Tag color={value ? 'success' : 'warning'}>{value ? '通過' : '未通過'}</Tag>
}

function recordedText(value: string) {
  return <pre style={{ margin: 0, whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{value || '無'}</pre>
}

export default function AccountMigrationDetail() {
  const { id = '' } = useParams()
  const navigate = useNavigate()
  const [limits, setLimits] = useState<MigrationThresholds | null>(null)
  const [subscriptionID, setSubscriptionID] = useState<string | null>(null)
  const query = useQuery({ queryKey: ['account-migration', id], queryFn: () => api.accountMigration(id), enabled: id !== '' })
  const readiness = useQuery({ queryKey: ['account-migration-readiness', id, limits], queryFn: () => api.accountMigrationReadiness(id, limits!), enabled: id !== '' && limits !== null })
  const entitlement = useQuery({ queryKey: ['account-migration-entitlement', id, subscriptionID], queryFn: () => api.accountMigrationEntitlement(id, subscriptionID!), enabled: id !== '' && subscriptionID !== null })
  if (query.isPending) return <Skeleton active />
  if (query.isError) {
    const status = query.error instanceof HttpError ? query.error.status : 0
    return <Result status={status === 404 ? '404' : status === 403 ? '403' : 'error'} title={status === 404 ? '找不到帳戶遷移' : status === 403 ? '沒有權限查看帳戶遷移' : '帳戶遷移無法載入'} subTitle={query.error.message} extra={<Button onClick={() => void query.refetch()}>重試</Button>} />
  }
  const detail = query.data.migration
  const link = detail.Link
  return <div className="form-page">
    <Space align="center" wrap>
      <Typography.Title level={2} style={{ margin: 0 }}>帳戶遷移詳情</Typography.Title>
      {link.Stopped && <Tag color="error">已停止</Tag>}
      {query.isFetching && <Tag>更新中</Tag>}
    </Space>
    <Typography.Paragraph type="secondary">資料查詢時間：{dateText(query.data.observed_at)}。讀取來源與寫入來源分開列示；停止遷移會阻擋新操作，既有 owner 與金融歷史仍保留。</Typography.Paragraph>
    {link.Stopped && <Alert type="warning" showIcon className="form-alert" message="遷移已停止" description={`原因：${link.StopReason || '未記錄'}`} />}
    <Card title="帳戶映射與 owner" className="result-card" extra={<Button onClick={() => void query.refetch()}>重新整理</Button>}>
      <Descriptions bordered size="small" column={1} items={[
        { key: 'legacy', label: '既有帳戶 ID', children: <Typography.Text copyable>{link.LegacyAccountID}</Typography.Text> },
        { key: 'customer', label: 'Commerce 客戶 ID', children: <Button type="link" onClick={() => navigate(`/customers/${encodeURIComponent(link.CustomerID)}`)}>{link.CustomerID}</Button> },
        { key: 'beneficiary', label: '權益受益者 ID', children: link.BeneficiaryID },
        { key: 'cohort', label: 'Cohort', children: link.Cohort },
        { key: 'history', label: '有既有帳務歷史', children: link.HasHistory ? '有' : '無' },
        { key: 'read', label: '讀取 owner', children: <Tag>{link.ReadOwner}</Tag> },
        { key: 'writer', label: '寫入 owner', children: <Tag>{link.WriterOwner}</Tag> },
      ]} />
    </Card>
    <Card title="切換門檻與 Readiness" className="result-card">
      <Typography.Paragraph type="secondary">輸入這次評估要使用的門檻；結果只反映查詢當下，切換命令會再次核對來源版本。</Typography.Paragraph>
      <Form layout="vertical" onFinish={(values: MigrationThresholds) => setLimits(values)}>
        <Form.Item label="報價 P95 上限（毫秒）" name="max_quote_p95_millis" rules={[{ required: true, message: '請輸入報價 P95 上限' }, { pattern: /^[1-9]\d*$/, message: '請輸入正整數' }, int64Rule]}><Input inputMode="numeric" /></Form.Item>
        <Form.Item label="未知付款上限" name="max_unknown_payments" rules={[{ required: true, message: '請輸入未知付款上限' }, { pattern: /^\d+$/, message: '請輸入非負整數' }, int64Rule]}><Input inputMode="numeric" /></Form.Item>
        <Form.Item label="未結對帳差異上限" name="max_open_discrepancies" rules={[{ required: true, message: '請輸入未結差異上限' }, { pattern: /^\d+$/, message: '請輸入非負整數' }, int64Rule]}><Input inputMode="numeric" /></Form.Item>
        <Button type="primary" htmlType="submit">計算 Readiness</Button>
      </Form>
      {readiness.isFetching && <Skeleton active className="result-card" />}
      {readiness.isError && <Alert type="error" showIcon className="form-alert" message="Readiness 無法載入" description={<Button onClick={() => void readiness.refetch()}>重試</Button>} />}
      {readiness.data && !readiness.isFetching && <>
        <Typography.Paragraph className="result-card">使用門檻：P95 ≤ {limits?.max_quote_p95_millis} ms、未知付款 ≤ {limits?.max_unknown_payments}、未結差異 ≤ {limits?.max_open_discrepancies}。觀測時間：{dateText(readiness.data.observed_at)}</Typography.Paragraph>
        <Descriptions bordered size="small" column={1} items={[
          { key: 'ready', label: '可切換', children: yesNo(readiness.data.readiness.Ready) },
          { key: 'reconciled', label: '最近對帳', children: yesNo(readiness.data.readiness.Reconciled) },
          { key: 'quote', label: '報價 Shadow 一致', children: yesNo(readiness.data.readiness.QuoteMatches) },
          { key: 'entitlement', label: '權益 Shadow 一致', children: yesNo(readiness.data.readiness.EntitlementMatches) },
          { key: 'provenance', label: '來源完整', children: yesNo(readiness.data.readiness.ProvenanceComplete) },
          { key: 'p95', label: '報價 P95', children: `${readiness.data.readiness.QuoteP95Millis} ms` },
          { key: 'unknown', label: '未知付款', children: readiness.data.readiness.UnknownPayments },
          { key: 'discrepancies', label: '未結差異', children: readiness.data.readiness.OpenDiscrepancies },
        ]} />
      </>}
    </Card>
    <Card title="Adapter 權益讀取" className="result-card">
      <Typography.Paragraph type="secondary">輸入訂閱 ID，查詢此帳戶目前的讀取 owner 與適配器實際回傳的權益狀態。</Typography.Paragraph>
      <Form layout="vertical" onFinish={(values: { subscription_id: string }) => setSubscriptionID(values.subscription_id)}>
        <Form.Item label="訂閱 ID" name="subscription_id" rules={[{ required: true, message: '請輸入訂閱 ID' }]}><Input autoComplete="off" /></Form.Item>
        <Button htmlType="submit">查詢權益</Button>
      </Form>
      {entitlement.isFetching && <Skeleton active className="result-card" />}
      {entitlement.isError && <Alert type="error" showIcon className="form-alert" message="Adapter 權益無法載入" description={<Button onClick={() => void entitlement.refetch()}>重試</Button>} />}
      {entitlement.data && !entitlement.isFetching && <Descriptions bordered size="small" column={1} className="result-card" items={[
        { key: 'id', label: '訂閱 ID', children: subscriptionID },
        { key: 'owner', label: '讀取 owner', children: <Tag>{entitlement.data.entitlement.Owner}</Tag> },
        { key: 'status', label: '權益狀態', children: entitlement.data.entitlement.Status },
        { key: 'revision', label: '來源 Revision', children: entitlement.data.entitlement.SourceRevision },
        { key: 'at', label: '讀取時間', children: dateText(entitlement.data.entitlement.AsOf) },
      ]} />}
    </Card>
    <Space wrap className="result-card">
      <Button disabled={link.Stopped} onClick={() => navigate(`/account-migrations/${encodeURIComponent(id)}/shadow-quotes`)}>比對報價</Button>
      <Button disabled={link.Stopped} onClick={() => navigate(`/account-migrations/${encodeURIComponent(id)}/shadow-entitlements`)}>比對權益</Button>
      <Button disabled={link.Stopped} onClick={() => navigate(`/account-migrations/${encodeURIComponent(id)}/provenance`)}>回填來源</Button>
      <Button disabled={link.Stopped || link.ReadOwner === 'commerce'} onClick={() => navigate(`/account-migrations/${encodeURIComponent(id)}/switch-read`)}>切換讀取</Button>
      <Button disabled={link.Stopped || link.ReadOwner !== 'commerce' || link.WriterOwner === 'commerce'} onClick={() => navigate(`/account-migrations/${encodeURIComponent(id)}/switch-writer`)}>切換寫入</Button>
      <Button danger disabled={link.Stopped} onClick={() => navigate(`/account-migrations/${encodeURIComponent(id)}/stop`)}>停止遷移</Button>
    </Space>
    <Card title="Shadow 比對" className="result-card" extra={<Button onClick={() => navigate(`/account-migrations/${encodeURIComponent(id)}/shadow-history`)}>查看完整歷史</Button>}>
      {detail.ShadowsTruncated && <Alert type="warning" showIcon message="僅顯示最近 100 筆比對" />}
      <Table rowKey="ID" dataSource={detail.Shadows} pagination={false} scroll={{ x: 'max-content' }} locale={{ emptyText: <Empty description="尚無比對" /> }} columns={[
        { title: '類型', dataIndex: 'Kind' }, { title: '對象', dataIndex: 'ObjectID' },
        { title: '一致', dataIndex: 'Matched', render: yesNo },
        { title: '延遲', dataIndex: 'LatencyMillis', render: (value: string) => `${value} ms` },
        { title: '觀測時間', dataIndex: 'ObservedAt', render: dateText },
      ]} expandable={{ expandedRowRender: (item) => <Descriptions bordered size="small" column={1} items={[
        { key: 'expected', label: 'Expected', children: recordedText(item.Expected) },
        { key: 'actual', label: 'Actual', children: recordedText(item.Actual) },
      ]} /> }} />
    </Card>
    <Card title="既有帳務來源" className="result-card" extra={<Button onClick={() => navigate(`/account-migrations/${encodeURIComponent(id)}/provenance-history`)}>查看完整記錄</Button>}>
      {detail.ProvenanceTruncated && <Alert type="warning" showIcon message="僅顯示前 100 筆來源記錄" />}
      <Table rowKey="LegacyInvoiceID" dataSource={detail.Provenance} pagination={false} scroll={{ x: 'max-content' }} locale={{ emptyText: <Empty description="尚無來源記錄" /> }} columns={[
        { title: '既有帳單', dataIndex: 'LegacyInvoiceID' },
        { title: 'Commerce 帳單', dataIndex: 'CommerceInvoiceID' },
        { title: '價格版本', dataIndex: 'PriceVersionID' },
        { title: '狀態', dataIndex: 'Status', render: (value: string) => <Tag>{value}</Tag> },
        { title: '證據', dataIndex: 'Evidence' },
        { title: '操作', render: (_, item) => item.Status === 'manual_review' && <Button type="link" onClick={() => navigate(`/account-migrations/${encodeURIComponent(id)}/provenance/${encodeURIComponent(item.LegacyInvoiceID)}/resolve`)}>處理來源</Button> },
      ]} />
    </Card>
    <Card title="遷移事件" className="result-card">
      {detail.EventsTruncated && <Alert type="warning" showIcon message="僅顯示最近 100 筆事件" />}
      <Table rowKey="ID" dataSource={detail.Events} pagination={false} scroll={{ x: 'max-content' }} locale={{ emptyText: <Empty description="尚無事件" /> }} columns={[
        { title: '事件', dataIndex: 'Kind' }, { title: '內容', dataIndex: 'Detail' }, { title: '時間', dataIndex: 'At', render: dateText },
      ]} />
    </Card>
  </div>
}
