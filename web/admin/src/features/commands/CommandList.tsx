import { useMutation, useQuery } from '@tanstack/react-query'
import { Alert, Button, Card, Descriptions, Drawer, Result, Skeleton, Space, Table, Tag, Typography } from 'antd'
import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, type Command, type Session } from '../../api/client'
import ExternalOperationOutcome, { failedExternalOperation } from '../../components/ExternalOperationOutcome'

export default function CommandList({ session }: { session: Session }) {
  const navigate = useNavigate()
  const [cursors, setCursors] = useState([''])
  const [page, setPage] = useState(0)
  const cursor = cursors[page]
  const query = useQuery({ queryKey: ['commands', cursor], queryFn: () => api.commands(cursor), refetchInterval: 5000 })
  const [selectedID, setSelectedID] = useState<string | null>(null)
  const selectedQuery = useQuery({ queryKey: ['command', selectedID], queryFn: () => api.command(selectedID!), enabled: selectedID !== null })
  const selected = selectedQuery.data ?? query.data?.items.find((item) => item.id === selectedID) ?? null
  const selectedUpdatedAt = selectedQuery.data ? selectedQuery.dataUpdatedAt : query.dataUpdatedAt
  const resume = useMutation({
    mutationFn: (id: string) => api.resumeCommand(session.csrf_token, id),
    onSuccess: () => { void query.refetch(); void selectedQuery.refetch() },
  })
  if (query.isPending) return <Skeleton active />
  if (query.isError && !query.data) return <Result status="error" title="命令列表載入失敗" subTitle={query.error.message} extra={<Button onClick={() => void query.refetch()}>重試</Button>} />
  return <>
    {query.isError && <Alert type="warning" showIcon className="result-card" message="無法更新命令列表；以下是上次成功讀取的資料" description={<Space wrap><span>上次讀取：{new Date(query.dataUpdatedAt).toLocaleString()}</span><Button onClick={() => void query.refetch()}>重試</Button></Space>} />}
    <Card title="管理命令" extra={<Button onClick={() => void query.refetch()}>重新整理</Button>}>
      <Table rowKey="id" dataSource={query.data.items} pagination={false} scroll={{ x: 'max-content' }} columns={[
        { title: '命令 ID', dataIndex: 'id', render: (id: string) => <Typography.Text copyable>{id}</Typography.Text> },
        { title: '操作', dataIndex: 'action_id' },
        { title: '狀態', dataIndex: 'status', render: (status: string, record: Command) => <Space wrap><Tag>{status}</Tag>{failedExternalOperation(record) && <Tag color="error">{failedExternalOperation(record)}失敗</Tag>}</Space> },
        { title: '建立時間', dataIndex: 'created_at', render: (value: string) => new Date(value).toLocaleString() },
        { title: '', render: (_, record: Command) => <Button type="link" onClick={() => setSelectedID(record.id)}>詳情</Button> },
      ]} />
    </Card>
    <Space className="result-card" wrap>
      <Button disabled={page === 0} onClick={() => { setSelectedID(null); setPage(page - 1) }}>上一頁</Button>
      <Typography.Text>第 {page + 1} 頁</Typography.Text>
      <Button disabled={!query.data.next_cursor} onClick={() => {
        if (!cursors[page + 1]) setCursors([...cursors, query.data.next_cursor])
        setSelectedID(null)
        setPage(page + 1)
      }}>下一頁</Button>
      <Typography.Text type="secondary">依建立順序分頁；新命令會出現在第一頁，列表並非全域快照。</Typography.Text>
    </Space>
    <Drawer title="命令詳情" width={560} open={selectedID !== null} extra={<Button onClick={() => void selectedQuery.refetch()} disabled={!selectedID}>更新</Button>} onClose={() => { setSelectedID(null); resume.reset() }}>
      {selectedQuery.isError && <Alert type={selected ? 'warning' : 'error'} showIcon message={selected ? '無法更新命令詳情；以下是上次成功讀取的資料' : '命令詳情載入失敗'} description={<Space wrap>{selectedUpdatedAt > 0 && <span>上次讀取：{new Date(selectedUpdatedAt).toLocaleString()}</span>}<Button onClick={() => void selectedQuery.refetch()}>重試</Button></Space>} />}
      {selected && <ExternalOperationOutcome command={selected} />}
      {selected && <Descriptions bordered size="small" column={1} items={[
        { key: 'id', label: '命令 ID', children: <Typography.Text copyable>{selected.id}</Typography.Text> },
        { key: 'action', label: '操作', children: selected.action_id },
        { key: 'status', label: '狀態', children: selected.status },
        { key: 'target', label: '對象', children: selected.target_id || '—' },
        { key: 'result', label: '結果參照', children: <pre>{JSON.stringify(selected.result_refs ?? {}, null, 2)}</pre> },
        { key: 'error', label: '錯誤', children: selected.error_code || '無' },
      ]} />}
      {selected && <Button className="result-card" onClick={() => { setSelectedID(null); navigate(`/commands/${encodeURIComponent(selected.id)}`) }}>開啟命令頁面</Button>}
      {selected?.status === 'waiting_verification' && <Button onClick={() => resume.mutate(selected.id)} loading={resume.isPending} disabled={selectedQuery.isError || !selectedQuery.data}>重新查證</Button>}
      {resume.isError && <Alert type="error" showIcon message="目前無法查證，請稍後重試" description={resume.error.message} />}
      {selected && ['C13', 'C30', 'C32', 'C44', 'C45'].includes(selected.action_id) && <Button onClick={() => { setSelectedID(null); navigate(`/jobs/${encodeURIComponent(`job:${selected.id}`)}`) }}>查看逐項進度</Button>}
    </Drawer>
  </>
}
