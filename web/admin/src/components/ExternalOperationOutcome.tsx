import { Alert } from 'antd'
import type { Command } from '../api/client'

type OutcomeCommand = Pick<Command, 'action_id' | 'status' | 'result_refs'>

export function failedExternalOperation(command: OutcomeCommand): '付款' | '退款' | null {
  if (command.status !== 'succeeded' || command.result_refs?.operation_status !== 'definitively_failed') return null
  if (command.action_id === 'C09') return '付款'
  if (command.action_id === 'C16') return '退款'
  return null
}

export default function ExternalOperationOutcome({ command }: { command: OutcomeCommand }) {
  const operation = failedExternalOperation(command)
  if (!operation) return null
  return <Alert type="warning" showIcon className="result-card" message={`送出命令已完成，但${operation}操作確定失敗`} description={operation === '退款' ? '請檢查原退款及 Credit 可用額度；若仍需退款，請建立新的退款預留。' : '請檢查帳單餘額；若仍需收款，請建立新的付款操作。'} />
}
