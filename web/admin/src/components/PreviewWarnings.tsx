import { Alert } from 'antd'
import type { Preview } from '../api/client'

export default function PreviewWarnings({ preview, expired }: { preview: Preview; expired: boolean }) {
  return <>
    {preview.blocking_reasons?.length > 0 && <Alert type="warning" showIcon message="目前無法確認此操作" description={<ul>{preview.blocking_reasons.map((reason, index) => <li key={`${index}:${reason}`}>{reason}</li>)}</ul>} />}
    {expired && <Alert type="warning" showIcon message="預覽已過期，請重新建立預覽" />}
  </>
}
