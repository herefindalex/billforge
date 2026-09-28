import { Button, Result, Space } from 'antd'
import { HttpError } from '../../api/client'

type Props = {
  error: Error | null
  onRetry: () => void
  onClear: () => void
}

export function isCommandNotFound(error: unknown): boolean {
  return error instanceof HttpError && error.status === 404
}

export default function CommandReadRecovery({ error, onRetry, onClear }: Props) {
  const missing = isCommandNotFound(error)

  return <Result
    status="error"
    title="命令狀態無法載入"
    subTitle={missing ? '目前資料庫中找不到這筆命令。清除本頁記錄後，請重新檢查輸入並開始操作。' : undefined}
    extra={<Space wrap>
      <Button onClick={onRetry}>重試</Button>
      {missing && <Button onClick={onClear}>清除本頁無效命令記錄</Button>}
    </Space>}
  />
}
