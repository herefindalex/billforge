import { Alert, Button, Result, Space } from 'antd'

type Props = {
  title: string
  message: string
  onRetryRead: () => void
  hasPendingCommand: boolean
  onRecoverCommand: () => void
  recovering: boolean
  commandID: string | null
  recoveryError: string | null
}

export default function ReadFailureWithRecovery({ title, message, onRetryRead, hasPendingCommand, onRecoverCommand, recovering, commandID, recoveryError }: Props) {
  return <div className="form-page">
    <Result status="error" title={title} subTitle={message} extra={<Button onClick={onRetryRead}>重試讀取</Button>} />
    {hasPendingCommand && !commandID && <Alert type="warning" showIcon className="result-card" message="原命令的結果尚未確認" description={<Space direction="vertical"><span>使用原 request key 查詢同一命令。</span><Button onClick={onRecoverCommand} loading={recovering}>查詢原命令</Button></Space>} />}
    {recoveryError && <Alert type="error" showIcon className="result-card" message="原命令查詢未成功" description={recoveryError} />}
    {commandID && <Button className="result-card" href={`/admin/commands/${encodeURIComponent(commandID)}`}>開啟命令頁面</Button>}
  </div>
}
