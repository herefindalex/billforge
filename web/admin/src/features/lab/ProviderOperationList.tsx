import { useQuery } from '@tanstack/react-query'
import { Alert, Button, Card, Result, Select, Skeleton, Space, Table, Tag, Typography } from 'antd'
import { useSearchParams } from 'react-router-dom'
import { api, canShowStaleRead, type ProviderOperation } from '../../api/client'
import Money from '../../components/Money'

export default function ProviderOperationList({ kind }: { kind: 'captures' | 'refunds' }) {
  const [searchParams, setSearchParams] = useSearchParams()
  const cursor = searchParams.get('cursor') ?? ''
  const back = searchParams.getAll('back')
  const status = searchParams.get('status') ?? ''
  const query = useQuery({ queryKey: ['provider', kind, cursor, status], queryFn: () => api.providerOperations(kind, cursor, status) })

  const previousPage = () => {
    const history = [...back]
    const previous = history.pop()
    const next = new URLSearchParams()
    if (status) next.set('status', status)
    for (const entry of history) next.append('back', entry)
    if (previous) next.set('cursor', previous)
    setSearchParams(next)
  }
  const nextPage = () => {
    if (!query.data?.next_cursor) return
    const next = new URLSearchParams(searchParams)
    next.append('back', cursor)
    next.set('cursor', query.data.next_cursor)
    setSearchParams(next)
  }

  if (query.isPending) return <Skeleton active />
  if (query.isError && (!query.data || !canShowStaleRead(query.error))) return <Result status="error" title="無法讀取模擬提供者資料" subTitle={query.error.message} extra={<Button onClick={() => void query.refetch()}>重試</Button>} />

  return <>
    {query.isError && <Alert type="warning" showIcon className="result-card" message="更新失敗；以下是上次成功讀取的資料" description={query.error.message} />}
    <Card title={kind === 'captures' ? 'Fake provider 收款' : 'Fake provider 退款'} extra={<Button onClick={() => void query.refetch()} loading={query.isFetching}>更新資料</Button>}>
      <Space wrap className="result-card">
        <Typography.Text>狀態：</Typography.Text>
        <Select aria-label="提供者狀態" value={status} style={{ width: 190 }} options={[
          { value: '', label: '全部' },
          { value: 'succeeded', label: '成功' },
          { value: 'definitively_failed', label: '確定失敗' },
        ]} onChange={(value) => setSearchParams(value ? new URLSearchParams({ status: value }) : new URLSearchParams())} />
      </Space>
      <Table rowKey="provider_key" dataSource={query.data.items} pagination={false} scroll={{ x: 'max-content' }} columns={[
        { title: 'Provider key', dataIndex: 'provider_key', render: (value: string) => <Typography.Text copyable>{value}</Typography.Text> },
        ...(kind === 'refunds' ? [{ title: '來源收款 key', dataIndex: 'source_capture_key', render: (value: string) => <Typography.Text copyable>{value}</Typography.Text> }] : []),
        { title: '金額', render: (_: unknown, row: ProviderOperation) => <Money minor={row.amount_minor} currency={row.currency} /> },
        { title: '狀態', dataIndex: 'status', render: (value: string) => <Tag>{value}</Tag> },
      ]} />
    </Card>
    <Space className="result-card" wrap>
      <Button disabled={back.length === 0} onClick={previousPage}>上一頁</Button>
      <Typography.Text>第 {back.length + 1} 頁</Typography.Text>
      <Button disabled={query.isError || !query.data.next_cursor} onClick={nextPage}>下一頁</Button>
      <Typography.Text type="secondary">提供者資料獨立觀測於 {new Date(query.data.observed_at).toLocaleString()}；跨頁不是全域快照。</Typography.Text>
    </Space>
  </>
}
