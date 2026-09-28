import { Tooltip, Typography } from 'antd'

export default function Money({ minor, currency }: { minor: unknown; currency: unknown }) {
  const text = minor === null || minor === undefined ? '' : String(minor)
  if (!/^-?\d+$/.test(text)) return <Typography.Text type="secondary">未知</Typography.Text>
  if (currency !== 'USD') return <Tooltip title={`${text} minor units`}><Typography.Text>{String(currency ?? '未知')} {text}</Typography.Text></Tooltip>
  const amount = BigInt(text)
  const negative = amount < 0n
  const absolute = negative ? -amount : amount
  const dollars = (absolute / 100n).toString().replace(/\B(?=(\d{3})+(?!\d))/g, ',')
  const cents = (absolute % 100n).toString().padStart(2, '0')
  return <Tooltip title={`${text} minor units`}><Typography.Text>{negative ? '−' : ''}USD {dollars}.{cents}</Typography.Text></Tooltip>
}
