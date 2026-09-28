import { useQuery } from '@tanstack/react-query'
import { Alert, Button, Card, Descriptions, Empty, Result, Skeleton, Space, Tag, Typography } from 'antd'
import { useNavigate, useParams } from 'react-router-dom'
import { api, HttpError } from '../../api/client'
import Money from '../../components/Money'

export default function PriceVersionDetail() {
  const { id = '' } = useParams()
  const navigate = useNavigate()
  const query = useQuery({ queryKey: ['price-version', id], queryFn: () => api.price(id), enabled: id !== '' })

  if (query.isPending && !query.data) return <Skeleton active />
  if (query.isError && !query.data) {
    const status = query.error instanceof HttpError ? query.error.status : 0
    return <Result
      status={status === 404 ? '404' : status === 403 ? '403' : 'error'}
      title={status === 404 ? '找不到價格版本' : status === 403 ? '沒有權限查看價格版本' : '價格版本無法載入'}
      subTitle={query.error.message}
      extra={<Button onClick={() => void query.refetch()}>重試</Button>}
    />
  }
  if (!query.data) return null

  const price = query.data.price
  return <div className="form-page">
    <Space align="center" wrap>
      <Typography.Title level={2} style={{ margin: 0 }}>價格版本詳情</Typography.Title>
      <Tag color={price.PublicationState === 'published' ? 'success' : 'default'}>{price.PublicationState}</Tag>
      {query.isFetching && <Tag>更新中</Tag>}
    </Space>
    <Typography.Paragraph type="secondary">資料查詢時間：{new Date(query.data.observed_at).toLocaleString()}。已發布版本不可修改；選價紀錄各有自己的生效時間。</Typography.Paragraph>
    {query.isError && <Alert type="warning" showIcon className="result-card" message="無法更新價格版本，以下是上次成功讀取的資料" description={query.error.message} />}
    <Card title="版本與生效範圍" className="result-card" extra={<Button onClick={() => void query.refetch()} loading={query.isFetching}>重新整理</Button>}>
      <Descriptions bordered size="small" column={1} items={[
        { key: 'id', label: '版本 ID', children: <Typography.Text copyable>{price.ID}</Typography.Text> },
        { key: 'plan', label: '方案 ID', children: price.PlanID },
        { key: 'version', label: '版本序號', children: price.Version },
        { key: 'currency', label: '幣別', children: price.Currency },
        { key: 'fixed', label: '固定金額', children: <Money minor={price.FixedMinor} currency={price.Currency} /> },
        { key: 'checksum', label: 'Checksum', children: <Typography.Text copyable>{price.Checksum || '尚未發布'}</Typography.Text> },
        { key: 'published', label: '發布時間', children: price.PublicationState === 'published' ? new Date(price.PublishedAt).toLocaleString() : '尚未發布' },
        { key: 'from', label: '生效起', children: new Date(price.EffectiveFrom).toLocaleString() },
        { key: 'to', label: '生效止', children: price.EffectiveTo ? new Date(price.EffectiveTo).toLocaleString() : '未設定' },
        { key: 'selections', label: '選價紀錄數', children: price.SelectionCount },
      ]} />
    </Card>
    <Card title="價格元件" className="result-card">
      {price.Components.length === 0 && <Empty description="沒有價格元件" />}
      {price.Components.map((component) => <Card key={component.Code} size="small" title={component.Code} className="result-card">
        <Descriptions bordered size="small" column={1} items={[
          { key: 'kind', label: '類型', children: component.Kind },
          { key: 'amount', label: '金額', children: <Money minor={component.AmountMinor} currency={price.Currency} /> },
          { key: 'quantity', label: '包含數量', children: component.Quantity },
          { key: 'rate', label: '精確費率（分子／分母）', children: `${component.RateNum} / ${component.RateDen}` },
          { key: 'meter', label: 'Meter ID', children: component.MeterID || '無' },
        ]} />
      </Card>)}
      {price.ComponentsTruncated && <Alert type="warning" showIcon message="元件超過 50 筆；此頁只顯示前 50 筆" />}
    </Card>
    <Space className="result-card" wrap>
      <Button onClick={() => navigate(`/catalog-selections?price_version_id=${encodeURIComponent(price.ID)}`)}>查看此版本選價</Button>
      <Button onClick={() => navigate('/catalog/prices')}>返回價格列表</Button>
    </Space>
  </div>
}
