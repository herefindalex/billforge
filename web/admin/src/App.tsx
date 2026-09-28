import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Alert, App as AntApp, Button, Card, Descriptions, Drawer, Empty, Form,
 Input, Layout, Menu, Result, Select, Skeleton, Space, Statistic, Table, Tag, Typography,
} from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { Navigate, Route, Routes, useLocation, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { advanceSessionGeneration, api, HttpError, sessionExpiredEvent, type Session } from './api/client'
import { isExactAdminUTC } from './api/validation'
import CreateQuote from './features/quotes/CreateQuote'
import AcceptQuote from './features/quotes/AcceptQuote'
import CommandList from './features/commands/CommandList'
import CommandDetail from './features/commands/CommandDetail'
import JobDetails from './features/jobs/JobDetails'
import PauseMigration from './features/migrations/PauseMigration'
import PriceMigrationDetail from './features/migrations/PriceMigrationDetail'
import RecordUsage from './features/usage/RecordUsage'
import UsagePeriodDetail from './features/usage/UsagePeriodDetail'
import ScheduleCancel from './features/subscriptions/ScheduleCancel'
import SchedulePlan from './features/subscriptions/SchedulePlan'
import SubscriptionDetail from './features/subscriptions/SubscriptionDetail'
import CustomerDetail from './features/customers/CustomerDetail'
import InvoiceDetail from './features/invoices/InvoiceDetail'
import CreditDetail from './features/credits/CreditDetail'
import InvoiceHistory from './features/invoices/InvoiceHistory'
import DiscrepancyDetail from './features/reconciliation/DiscrepancyDetail'
import ReconciliationRunDetail from './features/reconciliation/ReconciliationRunDetail'
import AccountMigrationDetail from './features/migrations/AccountMigrationDetail'
import AccountMigrationHistory from './features/migrations/AccountMigrationHistory'
import CreatePayment from './features/payments/CreatePayment'
import RetryPayment from './features/payments/RetryPayment'
import ExternalOperationDetail from './features/payments/ExternalOperationDetail'
import ActionForm, { type ActionConfig } from './components/ActionForm'
import Money from './components/Money'

const { Header, Content, Sider } = Layout
const { Title, Text } = Typography

const resources = [
  ['Customers', '客戶', 'customers'],
  ['Quotes', '報價', 'quotes'], ['Subscriptions', '訂閱', 'subscriptions'], ['Invoices', '帳單', 'invoices'],
  ['Payments', '付款', 'payments'], ['ImmediateChanges', '立即升級', 'immediate-changes'], ['Credits', 'Credit', 'credits'], ['Refunds', '退款', 'refunds'],
  ['Prices', '價格版本', 'prices'], ['CatalogSelections', '目錄選價', 'catalog-selections'],
  ['PriceMigrations', '價格遷移', 'price-migrations'], ['Contracts', '合約', 'contracts'],
  ['UsageEvents', '用量事件', 'usage-events'], ['UsagePeriods', '用量帳期', 'usage-periods'],
 ['Meters', '計量表', 'meters'], ['AccountMigrations', '帳戶遷移', 'account-migrations'],
 ['LegacyProvenance', '舊帳單來源', 'legacy-provenance'],
 ['ReconciliationRuns', '對帳執行紀錄', 'reconciliation-runs'], ['Discrepancies', '對帳差異', 'discrepancies'],
  ['PendingOutbox', '待處理 Outbox', 'outbox'],
] as const

const resourceRoute: Record<string, string> = {
  customers: '/customers', quotes: '/quotes', subscriptions: '/subscriptions', invoices: '/invoices',
  payments: '/payments', credits: '/credits', refunds: '/refunds',
  prices: '/catalog/prices', meters: '/catalog/meters', 'price-migrations': '/price-migrations',
  contracts: '/contracts', 'usage-events': '/usage', 'reconciliation-runs': '/reconciliation',
  discrepancies: '/discrepancies', 'account-migrations': '/account-migrations', outbox: '/outbox',
}

const resourceFilterKeys: Record<string, string[]> = {
  prices: ['id_prefix', 'plan_id'],
  'catalog-selections': ['plan_id', 'cohort'],
  'price-migrations': ['id_prefix', 'cohort', 'status', 'created_from', 'created_before'],
  contracts: ['id_prefix', 'customer_id'],
 'usage-events': ['subscription_id', 'period_index', 'source'],
  'usage-periods': ['subscription_id'],
  quotes: ['id_prefix', 'customer_id'],
  subscriptions: ['id_prefix', 'customer_id', 'status', 'created_from', 'created_before'],
  invoices: ['id_prefix', 'subscription_id'],
  payments: ['id_prefix', 'invoice_id', 'status'],
  'immediate-changes': ['id_prefix', 'subscription_id', 'status'],
  credits: ['id_prefix', 'source_invoice_id', 'created_from', 'created_before'],
  refunds: ['id_prefix', 'grant_id', 'status', 'created_from', 'created_before'],
  outbox: ['id_prefix', 'kind'],
  meters: ['id_prefix', 'source'],
  'account-migrations': ['id_prefix', 'customer_id', 'cohort', 'created_from', 'created_before'],
  'legacy-provenance': ['id_prefix', 'legacy_account_id', 'status'],
  'reconciliation-runs': ['id_prefix', 'created_from', 'created_before'],
  discrepancies: ['id_prefix', 'object_id', 'status', 'kind'],
}
const resourceFilterLabels: Record<string, string> = {
  id_prefix: 'ID 前綴', plan_id: '方案 ID', cohort: 'Cohort', customer_id: '客戶 ID',
 subscription_id: '訂閱 ID', invoice_id: '帳單 ID', source_invoice_id: '來源帳單 ID',
 period_index: '帳期序號',
  grant_id: 'Credit ID', legacy_account_id: '舊帳戶 ID', object_id: '對象 ID',
  status: '狀態', kind: '種類', source: '來源',
  created_from: '建立時間起（UTC）', created_before: '建立時間前（UTC）',
}
const resourceStatusOptions: Record<string, string[]> = {
  'price-migrations': ['active', 'paused', 'completed'],
  subscriptions: ['pending', 'active'],
  payments: ['created', 'submitted', 'unknown', 'succeeded', 'definitively_failed', 'cancelled'],
  'immediate-changes': ['requested', 'active', 'definitively_failed', 'needs_review'],
  refunds: ['created', 'submitted', 'unknown', 'succeeded', 'definitively_failed'],
  'legacy-provenance': ['complete', 'manual_review'],
  discrepancies: ['open', 'investigating', 'resolved'],
}

const reductionAction: ActionConfig = {
  actionID: 'C11', title: '減少帳單義務', confirmLabel: '確認減額', preview: true,
  description: '減額可能釋放已付款項並產生可用 Credit；請核對預覽金額。',
  fields: [
    { name: 'reduction_minor', label: '減額（最小貨幣單位）', required: true, pattern: /^[1-9]\d*$/ },
    { name: 'reason', label: '減額理由', required: true },
  ],
}

const applyCreditAction: ActionConfig = {
  actionID: 'C12', title: '使用 Credit 抵扣帳單', confirmLabel: '確認抵扣', preview: true,
  description: '只能抵扣同一客戶的後續帳單；可用額度與帳單餘額由伺服器核對。',
  fields: [
    { name: 'invoice_id', label: '目標帳單 ID', required: true },
    { name: 'amount_minor', label: '抵扣金額（最小貨幣單位）', required: true, pattern: /^[1-9]\d*$/ },
  ],
}

const resolveUnfulfilledAction: ActionConfig = {
  actionID: 'C14', title: '處理未履行的立即升級', confirmLabel: '確認建立更正', preview: true,
  description: '此操作會將未提供的升級服務帳單歸零，並將已收款項釋放為 Credit。',
  fields: [],
}

const changeCorrectionsAction: ActionConfig = {
  actionID: 'C13', title: '執行升級差額更正', confirmLabel: '確認執行更正', preview: true,
  description: '預覽會固定目前待處理的升級項目；確認後只會執行這批項目。',
  fields: [],
}

const reserveRefundAction: ActionConfig = {
  actionID: 'C15', title: '預留退款', confirmLabel: '確認預留退款', preview: true,
  description: '預留會占用 Credit 可用額度；退款送出與查證需要後續操作。',
  fields: [{ name: 'amount_minor', label: '退款金額（最小貨幣單位）', required: true, pattern: /^[1-9]\d*$/ }],
}

const dispatchPaymentAction: ActionConfig = {
  actionID: 'C09', title: '送出付款操作', confirmLabel: '確認送出付款', preview: true,
  description: '送出可能呼叫付款提供者；結果未定時請使用重新查證。', fields: [],
}
const reconcilePaymentAction: ActionConfig = {
  actionID: 'C10', title: '查證付款操作', confirmLabel: '確認查證', preview: false,
  description: '查詢提供者已保存的結果，並更新本地付款狀態。', fields: [],
}
const dispatchRefundAction: ActionConfig = {
  actionID: 'C16', title: '送出退款操作', confirmLabel: '確認送出退款', preview: true,
  description: '只送出指定退款 ID；結果未定時請使用重新查證。', fields: [],
}
const reconcileRefundAction: ActionConfig = {
  actionID: 'C17', title: '查證退款操作', confirmLabel: '確認查證', preview: false,
  description: '查詢提供者已保存的退款結果，並更新本地狀態。', fields: [],
}

const publishProPriceAction: ActionConfig = {
  actionID: 'C18', title: '發布 Pro 價格版本', confirmLabel: '確認發布價格', preview: true,
  description: '價格版本發布後不可修改；請核對完整規格與 checksum。',
  fields: [
    { name: 'id', label: '價格版本 ID', required: true },
    { name: 'version', label: '版本號', required: true, pattern: /^[1-9]\d*$/ },
    { name: 'fixed_minor', label: '固定金額（最小單位）', required: true, pattern: /^[1-9]\d*$/ },
    { name: 'seat_minor', label: '每席金額（最小單位）', required: true, pattern: /^[1-9]\d*$/ },
    { name: 'included_tasks', label: '包含任務量', required: true, pattern: /^\d+$/ },
    { name: 'usage_rate_num', label: '超額費率分子', required: true, pattern: /^\d+$/ },
    { name: 'usage_rate_den', label: '超額費率分母', required: true, pattern: /^[1-9]\d*$/ },
    { name: 'effective_from', label: '生效起點（UTC）', required: true, placeholder: '2026-10-01T00:00:00Z' },
  ],
}
const registerMeterAction: ActionConfig = {
  actionID: 'C19', title: '註冊計量表', confirmLabel: '確認註冊', preview: true,
  fields: [
    { name: 'id', label: '計量表 ID', required: true },
    { name: 'source', label: '事件來源', required: true, placeholder: 'api 或 *' },
    { name: 'unit', label: '計量單位', required: true },
    { name: 'schema_version', label: 'Schema 版本', required: true, pattern: /^[1-9]\d*$/ },
  ],
}
const publishMeteredPriceAction: ActionConfig = {
  actionID: 'C20', title: '發布計量價格版本', confirmLabel: '確認發布價格', preview: true,
  description: '需先註冊計量表；價格版本發布後不可修改。',
  fields: [
    { name: 'id', label: '價格版本 ID', required: true },
    { name: 'plan_id', label: '方案 ID', required: true },
    { name: 'version', label: '版本號', required: true, pattern: /^[1-9]\d*$/ },
    { name: 'fixed_minor', label: '固定金額（最小單位）', required: true, pattern: /^[1-9]\d*$/ },
    { name: 'seat_minor', label: '每席金額（最小單位）', required: true, pattern: /^\d+$/ },
    { name: 'meter_id', label: '計量表 ID', required: true },
    { name: 'included_quantity', label: '包含用量', required: true, pattern: /^[1-9]\d*$/ },
    { name: 'usage_rate_num', label: '超額費率分子', required: true, pattern: /^[1-9]\d*$/ },
    { name: 'usage_rate_den', label: '超額費率分母', required: true, pattern: /^[1-9]\d*$/ },
    { name: 'effective_from', label: '生效起點（UTC）', required: true, placeholder: '2026-10-01T00:00:00Z' },
  ],
}
const selectCatalogPriceAction: ActionConfig = {
  actionID: 'C21', title: '選擇目錄價格', confirmLabel: '確認選價', preview: true,
  fields: [
    { name: 'plan_id', label: '方案 ID', required: true },
    { name: 'cohort', label: 'Cohort', required: true },
    { name: 'effective_at', label: '生效時間（UTC）', required: true, placeholder: '2026-10-01T00:00:00Z' },
    { name: 'price_version_id', label: '價格版本 ID', required: true },
  ],
}

const planMigrationAction: ActionConfig = {
  actionID: 'C22', title: '規劃價格遷移', confirmLabel: '確認建立遷移批次', preview: true,
  description: '預覽固定訂閱清單、目前版本、目標版本與下期金額；確認時逐項重查。',
  fields: [
    { name: 'id', label: '遷移批次 ID', required: true },
    { name: 'cohort', label: 'Cohort', required: true },
    { name: 'target_price_version_id', label: '目標價格版本 ID', required: true },
    { name: 'subscription_ids', label: '訂閱 ID（逗號或換行分隔）', required: true, csv: true },
  ],
}
const skipMigrationAction: ActionConfig = {
  actionID: 'C24', title: '略過價格遷移項目', confirmLabel: '確認略過', preview: true,
  fields: [
    { name: 'subscription_id', label: '訂閱 ID', required: true },
    { name: 'reason', label: '略過理由', required: true },
  ],
}
const resumeMigrationAction: ActionConfig = {
  actionID: 'C25', title: '恢復價格遷移', confirmLabel: '確認恢復', preview: true,
  description: '只有沒有衝突項目的暫停批次可以恢復。', fields: [],
}

const adjustUsageAction: ActionConfig = {
  actionID: 'C27', title: '調整用量事件', confirmLabel: '確認反向調整', preview: true,
  description: '反向調整會引用原事件並保留原帳期與價格來源。',
  fields: [
    { name: 'source', label: '新事件來源', required: true },
    { name: 'event_id', label: '新事件 ID', required: true },
    { name: 'subscription_id', label: '訂閱 ID', required: true },
    { name: 'original_source', label: '原事件來源', required: true },
    { name: 'original_event_id', label: '原事件 ID', required: true },
    { name: 'reverse_quantity', label: '反向數量', required: true, pattern: /^[1-9]\d*$/ },
  ],
}
const closeUsageAction: ActionConfig = {
  actionID: 'C28', title: '關閉用量帳期', confirmLabel: '確認關帳', preview: true,
  fields: [
    { name: 'period_index', label: '帳期序號', required: true, pattern: /^\d+$/ },
    { name: 'cutoff', label: '接收截止時間（UTC）', required: true, placeholder: '2026-10-01T00:00:00Z' },
  ],
}
const rerateUsageAction: ActionConfig = {
  actionID: 'C29', title: '重算用量帳期', confirmLabel: '確認重算', preview: true,
  fields: [{ name: 'period_index', label: '帳期序號', required: true, pattern: /^\d+$/ }],
}

const usageCreditNotesAction: ActionConfig = {
  actionID: 'C30', title: '產生用量 Credit Note', confirmLabel: '確認產生', preview: true,
  fields: [], description: '預覽會固定本次需減額的帳期與帳單，確認前請檢查金額及來源。',
}

const publishContractAction: ActionConfig = {
  actionID: 'C31', title: '發布合約版本', confirmLabel: '確認發布合約', preview: true,
  description: '金額以最小貨幣單位輸入；生效時間請使用 UTC。',
  fields: [
    { name: 'id', label: '合約版本 ID', required: true },
    { name: 'customer_id', label: '客戶 ID', required: true },
    { name: 'version', label: '版本序號', required: true, pattern: /^[1-9]\d*$/ },
    { name: 'base_price_version_id', label: '基礎價格版本 ID', required: true },
    { name: 'fixed_minor', label: '固定金額', required: true, pattern: /^[1-9]\d*$/ },
    { name: 'seat_minor', label: '每席金額', required: true, pattern: /^[1-9]\d*$/ },
    { name: 'effective_from', label: '生效起點（UTC）', required: true, placeholder: '2026-10-01T00:00:00Z' },
    { name: 'effective_to', label: '生效終點（UTC）', required: true, placeholder: '2027-10-01T00:00:00Z' },
    { name: 'post_contract_price_version_id', label: '合約結束後價格版本 ID' },
  ],
}

const collectContractInvoicesAction: ActionConfig = {
  actionID: 'C32', title: '收取到期合約帳單', confirmLabel: '確認建立收款工作', preview: true,
  fields: [], description: '預覽固定本次到期的付款操作；新到期帳單需另建預覽。',
}

const runReconciliationAction: ActionConfig = {
  actionID: 'C33', title: '執行對帳', confirmLabel: '確認執行對帳', preview: false,
  fields: [{ name: 'as_of', label: '核對截止時間（UTC）', required: true, placeholder: '2026-10-01T00:00:00Z' }],
}

const repairDiscrepancyAction: ActionConfig = {
  actionID: 'C34', title: '修復對帳差異', confirmLabel: '確認修復', preview: true,
  fields: [
    { name: 'source_revision', label: '來源修訂版', required: true, pattern: /^\d+$/ },
    { name: 'evidence', label: '來源證據', required: true },
  ],
}

const manualDecisionAction: ActionConfig = {
  actionID: 'C35', title: '記錄人工決議', confirmLabel: '確認記錄決議', preview: true,
  fields: [
    { name: 'decision', label: '決議', required: true },
    { name: 'reason', label: '原因', required: true },
  ], description: '人工決議會留下稽核紀錄；資金及對帳差異仍需後續查證。',
}

const linkAccountAction: ActionConfig = {
  actionID: 'C36', title: '連結既有帳戶', confirmLabel: '確認連結帳戶', preview: true,
  fields: [
    { name: 'legacy_account_id', label: '既有帳戶 ID', required: true },
    { name: 'customer_id', label: 'Commerce 客戶 ID', required: true },
    { name: 'beneficiary_id', label: '受益人 ID', required: true },
    { name: 'cohort', label: '價格 Cohort', required: true },
    { name: 'has_history', label: '是否有歷史資料', required: true, options: [{ value: 'true', label: '有' }, { value: 'false', label: '沒有' }] },
  ],
}

const shadowQuoteAction: ActionConfig = {
  actionID: 'C37', title: '比對報價', confirmLabel: '確認記錄比對', preview: false,
  fields: [
    { name: 'plan_id', label: '方案 ID', required: true },
    { name: 'seats', label: '席位數', required: true, pattern: /^\d+$/ },
    { name: 'legacy_amount_minor', label: '既有報價金額', required: true, pattern: /^\d+$/ },
    { name: 'legacy_currency', label: '幣別', required: true },
  ],
}

const shadowEntitlementAction: ActionConfig = {
  actionID: 'C38', title: '比對權益', confirmLabel: '確認記錄比對', preview: false,
  fields: [
    { name: 'subscription_id', label: '訂閱 ID', required: true },
    { name: 'legacy_status', label: '既有權益狀態', required: true },
  ],
}

const backfillProvenanceAction: ActionConfig = {
  actionID: 'C39', title: '回填舊帳單來源', confirmLabel: '確認回填', preview: true,
  fields: [
    { name: 'legacy_invoice_id', label: '舊帳單 ID', required: true },
    { name: 'legacy_subscription_id', label: '舊訂閱 ID', required: true },
    { name: 'commerce_subscription_id', label: 'Commerce 訂閱 ID', required: true },
    { name: 'commerce_invoice_id', label: 'Commerce 帳單 ID', required: true },
    { name: 'price_version_id', label: '價格版本 ID', required: true },
  ],
}

const resolveProvenanceAction: ActionConfig = {
  actionID: 'C40', title: '審核舊帳單來源', confirmLabel: '確認來源映射', preview: true,
  targetParam: 'legacyInvoiceId',
  fields: [
    { name: 'commerce_subscription_id', label: 'Commerce 訂閱 ID', required: true },
    { name: 'commerce_invoice_id', label: 'Commerce 帳單 ID', required: true },
    { name: 'price_version_id', label: '價格版本 ID', required: true },
    { name: 'decision', label: '審核決議', required: true },
  ],
}

const cutoverFields: ActionConfig['fields'] = [
  { name: 'max_quote_p95_millis', label: '報價 P95 上限（毫秒）', required: true, pattern: /^[1-9]\d*$/ },
  { name: 'max_unknown_payments', label: '未知付款上限', required: true, pattern: /^\d+$/ },
  { name: 'max_open_discrepancies', label: '未結對帳差異上限', required: true, pattern: /^\d+$/ },
]

const switchReadAction: ActionConfig = {
  actionID: 'C41', title: '切換帳戶讀取來源', confirmLabel: '確認切換讀取', preview: true, fields: cutoverFields,
}

const switchWriterAction: ActionConfig = {
  actionID: 'C42', title: '切換帳戶寫入來源', confirmLabel: '確認切換寫入', preview: true, fields: cutoverFields,
}

const stopMigrationAction: ActionConfig = {
  actionID: 'C43', title: '停止帳戶遷移', confirmLabel: '確認停止', preview: true,
  fields: [{ name: 'reason', label: '停止原因', required: true }],
  description: '停止後保留既有讀寫 owner 與金融歷史，但禁止後續新寫入。',
}

const runRenewalsAction: ActionConfig = {
  actionID: 'C44', title: '執行到期續約', confirmLabel: '確認續約批次', preview: true,
  fields: [], description: '預覽固定訂閱和帳期；每筆結果會顯示建立帳單或略過原因。',
}

const refreshEntitlementsAction: ActionConfig = {
 actionID: 'C45', title: '刷新權益', confirmLabel: '確認刷新權益', preview: true,
 fields: [], description: '預覽固定本次訂閱清單，並以現有帳單與付款事實重新投影權益。',
}

const clockAction: ActionConfig = {
 actionID: 'C46', title: '設定實驗時鐘', confirmLabel: '確認設定時鐘', preview: false,
 fields: [
  { name: 'mode', label: '模式', required: true, options: [{ value: 'real', label: '實際時間' }, { value: 'fixed', label: '固定時間' }] },
  { name: 'value_utc', label: '固定 UTC 時間（固定模式必填）', placeholder: '2026-09-26T12:00:00Z' },
 ],
 description: '業務時間持久化於本機資料庫；登入期限及命令租約仍使用實際時間。',
}
const paymentDecisionAction: ActionConfig = {
 actionID: 'C47', title: '設定假付款結果', confirmLabel: '確認付款結果', preview: false,
 fields: [{ name: 'status', label: '結果', required: true, options: [{ value: 'succeeded', label: '成功' }, { value: 'definitively_failed', label: '確定失敗' }] }],
}
const refundDecisionAction: ActionConfig = {
 actionID: 'C48', title: '設定假退款結果', confirmLabel: '確認退款結果', preview: false,
 fields: paymentDecisionAction.fields,
}
const faultAction: ActionConfig = {
 actionID: 'C49', title: '建立一次性故障票據', confirmLabel: '確認故障票據', preview: false,
 fields: [
  { name: 'operation_kind', label: '操作種類', required: true, options: [{ value: 'payment', label: '付款' }, { value: 'refund', label: '退款' }] },
  { name: 'mode', label: '故障模式', required: true, options: [{ value: 'lost_response', label: '回應遺失' }, { value: 'crash_after_provider', label: '提供者完成後中斷' }] },
 ], description: '票據只在指定操作下一次管理派送時消耗。',
}

function LabControls() {
  const navigate = useNavigate()
  const [operationID, setOperationID] = useState('')
  const [faultCursor, setFaultCursor] = useState('')
  const [faultBack, setFaultBack] = useState<string[]>([])
  const clock = useQuery({ queryKey: ['lab-clock'], queryFn: api.clock })
  const faults = useQuery({ queryKey: ['lab-faults', faultCursor], queryFn: () => api.faults(faultCursor) })
 const [kind, setKind] = useState('payment')
 const id = operationID.trim()
 return <Space direction="vertical" size="large" style={{ width: '100%' }}>
  <Typography.Title level={2}>實驗控制</Typography.Title>
  <Typography.Text type="secondary">本機實驗環境 · 假付款服務。固定時鐘只影響商務時間。</Typography.Text>
  <Card title="目前業務時鐘" extra={<Button onClick={() => clock.refetch()}>更新</Button>}>
   {clock.isLoading ? <Skeleton active /> : clock.isError ? <Alert type="error" message="無法讀取時鐘" /> :
    <Descriptions column={1} items={[
     { key: 'mode', label: '模式', children: clock.data?.mode === 'fixed' ? '固定時間' : '實際時間' },
     { key: 'time', label: '業務時間（UTC）', children: clock.data?.business_time },
     { key: 'revision', label: '版本', children: clock.data?.revision },
    ]} />}
   <Button type="primary" onClick={() => navigate('/lab/clock')}>設定時鐘</Button>
  </Card>
  <Card title="指定操作結果與故障">
   <Space wrap>
    <Input aria-label="付款或退款操作 ID" style={{ width: 300 }} placeholder="付款或退款操作 ID" value={operationID} onChange={(e) => setOperationID(e.target.value)} />
    <Select aria-label="操作種類" value={kind} onChange={setKind} options={[{ value: 'payment', label: '付款' }, { value: 'refund', label: '退款' }]} />
    <Button disabled={!id} onClick={() => navigate(`/lab/${kind === 'payment' ? 'payment' : 'refund'}-decisions/${encodeURIComponent(id)}`)}>設定結果</Button>
    <Button disabled={!id} onClick={() => navigate(`/lab/faults/${encodeURIComponent(id)}`, { state: { operation_kind: kind } })}>建立故障票據</Button>
   </Space>
  </Card>
  <Card title="待使用與最近的故障票據" extra={<Button onClick={() => faults.refetch()}>更新</Button>}>
   {faults.isError && <Alert type="error" message="無法讀取故障票據" />}
   <Table size="small" rowKey="id" loading={faults.isLoading} dataSource={faults.data?.items ?? []} pagination={false} scroll={{ x: 700 }} columns={[
    { title: '建立時間', dataIndex: 'created_at', key: 'created_at' },
    { title: '種類', dataIndex: 'operation_kind', key: 'operation_kind' },
    { title: '操作 ID', dataIndex: 'operation_id', key: 'operation_id', render: (value: string) => <Text copyable>{value}</Text> },
    { title: '故障模式', dataIndex: 'mode', key: 'mode' },
    { title: '狀態', key: 'status', render: (_: unknown, row: { claimed_command_id?: string }) => row.claimed_command_id ? <Tag color="default">已使用</Tag> : <Tag color="orange">待使用</Tag> },
   ]} />
   <Typography.Text type="secondary">票據狀態可能在翻頁期間改變；列表不是跨頁快照。</Typography.Text>
   <Space wrap>
    <Button disabled={faultBack.length === 0 || faults.isFetching} onClick={() => {
      setFaultCursor(faultBack[faultBack.length - 1])
      setFaultBack((previous) => previous.slice(0, -1))
    }}>上一頁故障票據</Button>
    <Typography.Text>第 {faultBack.length + 1} 頁</Typography.Text>
    <Button disabled={!faults.data?.next_cursor || faults.isFetching} onClick={() => {
      if (!faults.data?.next_cursor) return
      setFaultBack((previous) => [...previous, faultCursor])
      setFaultCursor(faults.data.next_cursor)
    }}>下一頁故障票據</Button>
    {faults.data?.observed_at && <Typography.Text type="secondary">觀測時間：{faults.data.observed_at}</Typography.Text>}
   </Space>
  </Card>
 </Space>
}

function loginReturnPath(state: unknown): string {
  const from = (state as { from?: unknown } | null)?.from
  return typeof from === 'string' && from.startsWith('/') && !from.startsWith('//') && from !== '/login' ? from : '/'
}

function Login({ onLogin }: { onLogin: (session: Session) => void }) {
  const location = useLocation()
  const navigate = useNavigate()
  const { message } = AntApp.useApp()
  const mutation = useMutation({
    mutationFn: ({ username, password }: { username: string; password: string }) => api.login(username, password),
    onSuccess: (session) => {
      advanceSessionGeneration()
      onLogin(session)
      navigate(loginReturnPath(location.state), { replace: true })
    },
    onError: (error) => { void message.error(error instanceof Error ? error.message : '登入失敗') },
  })
  return <div className="login-page">
    <Card className="login-card">
      <div className="brand-mark">B</div>
      <Title level={2}>Billforge 管理介面</Title>
      <Text type="secondary">請使用本機設定的管理帳號登入。</Text>
      <Form layout="vertical" className="login-form" onFinish={mutation.mutate}>
        <Form.Item label="帳號" name="username" rules={[{ required: true, message: '請輸入帳號' }]}>
          <Input autoComplete="username" autoFocus />
        </Form.Item>
        <Form.Item label="密碼" name="password" rules={[{ required: true, message: '請輸入密碼' }]}>
          <Input.Password autoComplete="current-password" />
        </Form.Item>
        {mutation.isError && <Alert type="error" showIcon message={mutation.error instanceof HttpError && mutation.error.status === 429 ? '嘗試次數過多，請稍後再試。' : '帳號或密碼無法驗證。'} className="form-alert" />}
        <Button type="primary" htmlType="submit" loading={mutation.isPending} block>登入</Button>
      </Form>
    </Card>
  </div>
}

function valueText(value: unknown): string {
  if (value === null || value === undefined) return '未知'
  if (typeof value === 'object') return JSON.stringify(value)
  return String(value)
}

function resourceRowKey(name: string, record: Record<string, unknown>): string {
  if (record.ID != null) return valueText(record.ID)
  if (name === 'UsagePeriods' && record.SubscriptionID != null && record.PeriodIndex != null) {
    return JSON.stringify([record.SubscriptionID, record.PeriodIndex])
  }
  if (name === 'UsageEvents' && record.TenantID != null && record.Source != null && record.EventID != null) {
    return JSON.stringify([record.TenantID, record.Source, record.EventID])
  }
  if (name === 'CatalogSelections' && record.PlanID != null && record.Cohort != null && record.EffectiveAt != null) {
    return JSON.stringify([record.PlanID, record.Cohort, record.EffectiveAt])
  }
  return valueText(record.LegacyAccountID ?? record.LegacyInvoiceID ?? record.SubscriptionID ?? JSON.stringify(record))
}

function StateTable({ rows, name, total, observedAt, stale, refreshing, onRefresh }: { rows: Record<string, unknown>[]; name: string; total: number; observedAt: string; stale: boolean; refreshing: boolean; onRefresh: () => void }) {
  const [selectedKey, setSelectedKey] = useState<string | null>(null)
  const selected = selectedKey === null ? null : rows.find((row) => resourceRowKey(name, row) === selectedKey) ?? null
  useEffect(() => {
    if (selectedKey !== null && selected === null) setSelectedKey(null)
  }, [selectedKey, selected])
  const navigate = useNavigate()
  const preferredKeys = name === 'Contracts'
    ? ['ID', 'CustomerID', 'EffectiveFrom', 'EffectiveTo', 'FixedMinor', 'SeatMinor']
    : ['ID', 'LegacyAccountID', 'ObjectID', 'CustomerID', 'SubscriptionID', 'InvoiceID', 'Status', 'EntitlementStatus', 'PeriodIndex', 'Kind', 'Classification', 'AmountMinor', 'Currency']
  const keys = rows.length ? [...preferredKeys.filter((key) => key in rows[0]), ...Object.keys(rows[0]).filter((key) => !preferredKeys.includes(key))].slice(0, 6) : []
  const columns: ColumnsType<Record<string, unknown>> = keys.map((key) => ({
    title: key,
    dataIndex: key,
    key,
    ellipsis: true,
    render: (value: unknown, record) => {
      if (key.toLowerCase().includes('status')) return <Tag>{valueText(value)}</Tag>
      if (key.toLowerCase().endsWith('minor')) return <Money minor={value} currency={record.Currency} />
      return valueText(value)
    },
  }))
  columns.push({ title: '', key: 'detail', width: 170, render: (_, record) => <Space>
        {(name === 'Payments' || name === 'Refunds') && typeof record.ID === 'string' && <Button type="link" onClick={() => navigate(`/${name === 'Payments' ? 'payments' : 'refunds'}/${encodeURIComponent(record.ID as string)}`)}>開啟操作</Button>}
        <Button type="link" onClick={() => name === 'Subscriptions' && typeof record.ID === 'string' ? navigate(`/subscriptions/${encodeURIComponent(record.ID)}`) : name === 'Customers' && typeof record.ID === 'string' ? navigate(`/customers/${encodeURIComponent(record.ID)}`) : name === 'Invoices' && typeof record.ID === 'string' ? navigate(`/invoices/${encodeURIComponent(record.ID)}`) : name === 'Credits' && typeof record.ID === 'string' ? navigate(`/credits/${encodeURIComponent(record.ID)}`) : name === 'Discrepancies' && typeof record.ID === 'string' ? navigate(`/discrepancies/${encodeURIComponent(record.ID)}`) : name === 'ReconciliationRuns' && typeof record.ID === 'string' ? navigate(`/reconciliation-runs/${encodeURIComponent(record.ID)}`) : name === 'AccountMigrations' && typeof record.LegacyAccountID === 'string' ? navigate(`/account-migrations/${encodeURIComponent(record.LegacyAccountID)}`) : name === 'PriceMigrations' && typeof record.ID === 'string' ? navigate(`/price-migrations/${encodeURIComponent(record.ID)}`) : name === 'UsagePeriods' && typeof record.SubscriptionID === 'string' && record.PeriodIndex !== undefined ? navigate(`/usage-periods/${encodeURIComponent(record.SubscriptionID)}/${encodeURIComponent(String(record.PeriodIndex))}`) : setSelectedKey(resourceRowKey(name, record))}>詳情</Button>
    {name === 'Quotes' && !record.Accepted && typeof record.ID === 'string' && <Button type="link" onClick={() => navigate(`/quotes/${encodeURIComponent(record.ID as string)}/accept`)}>接受</Button>}
    {name === 'PriceMigrations' && record.Status === 'active' && typeof record.ID === 'string' && <Button type="link" onClick={() => navigate(`/price-migrations/${encodeURIComponent(record.ID as string)}/pause`)}>暫停</Button>}
    {name === 'PriceMigrations' && record.Status === 'paused' && typeof record.ID === 'string' && <Button type="link" onClick={() => navigate(`/price-migrations/${encodeURIComponent(record.ID as string)}/skip`)}>略過項目</Button>}
    {name === 'PriceMigrations' && record.Status === 'paused' && typeof record.ID === 'string' && <Button type="link" onClick={() => navigate(`/price-migrations/${encodeURIComponent(record.ID as string)}/resume`)}>恢復</Button>}
    {name === 'Subscriptions' && record.Status === 'active' && typeof record.ID === 'string' && <Button type="link" onClick={() => navigate(`/subscriptions/${encodeURIComponent(record.ID as string)}/cancel`)}>管理取消</Button>}
    {name === 'Subscriptions' && record.Status === 'active' && typeof record.ID === 'string' && <Button type="link" onClick={() => navigate(`/subscriptions/${encodeURIComponent(record.ID as string)}/schedule-plan`)}>下期變更</Button>}
    {name === 'Subscriptions' && record.Status === 'active' && typeof record.ID === 'string' && <Button type="link" onClick={() => navigate(`/subscriptions/${encodeURIComponent(record.ID as string)}/upgrade`)}>立即升級</Button>}
    {name === 'Subscriptions' && record.Status === 'active' && typeof record.ID === 'string' && <Button type="link" onClick={() => navigate(`/usage-periods/${encodeURIComponent(record.ID as string)}/close`)}>關閉用量</Button>}
    {name === 'Invoices' && typeof record.ID === 'string' && <Button type="link" onClick={() => navigate(`/invoices/${encodeURIComponent(record.ID as string)}/payments/new`)}>建立付款</Button>}
    {name === 'Invoices' && typeof record.ID === 'string' && <Button type="link" onClick={() => navigate(`/invoices/${encodeURIComponent(record.ID as string)}/reductions/new`)}>減額</Button>}
    {name === 'Credits' && typeof record.ID === 'string' && <Button type="link" onClick={() => navigate(`/credits/${encodeURIComponent(record.ID as string)}/apply`)}>抵扣帳單</Button>}
    {name === 'Credits' && typeof record.ID === 'string' && <Button type="link" onClick={() => navigate(`/credits/${encodeURIComponent(record.ID as string)}/refunds/new`)}>預留退款</Button>}
    {name === 'Payments' && record.Status === 'definitively_failed' && typeof record.ID === 'string' && <Button type="link" onClick={() => navigate(`/payments/${encodeURIComponent(record.ID as string)}/retry`)}>重試</Button>}
    {name === 'Payments' && record.Status === 'created' && typeof record.ID === 'string' && <Button type="link" onClick={() => navigate(`/payments/${encodeURIComponent(record.ID as string)}/dispatch`)}>送出</Button>}
    {name === 'Payments' && typeof record.ID === 'string' && <Button type="link" onClick={() => navigate(`/payments/${encodeURIComponent(record.ID as string)}/reconcile`)}>查證</Button>}
    {name === 'Refunds' && record.Status === 'created' && typeof record.ID === 'string' && <Button type="link" onClick={() => navigate(`/refunds/${encodeURIComponent(record.ID as string)}/dispatch`)}>送出</Button>}
    {name === 'Refunds' && typeof record.ID === 'string' && <Button type="link" onClick={() => navigate(`/refunds/${encodeURIComponent(record.ID as string)}/reconcile`)}>查證</Button>}
    {name === 'ImmediateChanges' && record.Status === 'needs_review' && typeof record.ID === 'string' && <Button type="link" onClick={() => navigate(`/changes/${encodeURIComponent(record.ID as string)}/resolve-unfulfilled`)}>處理未履行</Button>}
    {name === 'UsagePeriods' && typeof record.SubscriptionID === 'string' && record.PeriodIndex !== undefined && <Button type="link" onClick={() => navigate(`/usage-periods/${encodeURIComponent(record.SubscriptionID as string)}/rerate`, { state: { period_index: String(record.PeriodIndex) } })}>重算</Button>}
    {name === 'Discrepancies' && record.Status !== 'resolved' && typeof record.ID === 'string' && <Button type="link" onClick={() => navigate(`/discrepancies/${encodeURIComponent(record.ID as string)}/repair`, { state: { source_revision: String(record.SourceRevision ?? ''), evidence: String(record.Evidence ?? '') } })}>修復</Button>}
    {name === 'Discrepancies' && record.Status !== 'resolved' && (record.Classification === 'MANUAL_REVIEW' || record.Classification === 'UNSAFE_TO_REPAIR') && typeof record.ID === 'string' && <Button type="link" onClick={() => navigate(`/discrepancies/${encodeURIComponent(record.ID as string)}/manual-decisions`)}>人工決議</Button>}
    {name === 'AccountMigrations' && typeof record.LegacyAccountID === 'string' && <Button type="link" onClick={() => navigate(`/account-migrations/${encodeURIComponent(record.LegacyAccountID as string)}/shadow-quotes`)}>比對報價</Button>}
    {name === 'AccountMigrations' && typeof record.LegacyAccountID === 'string' && <Button type="link" onClick={() => navigate(`/account-migrations/${encodeURIComponent(record.LegacyAccountID as string)}/shadow-entitlements`)}>比對權益</Button>}
    {name === 'AccountMigrations' && typeof record.LegacyAccountID === 'string' && <Button type="link" onClick={() => navigate(`/account-migrations/${encodeURIComponent(record.LegacyAccountID as string)}/provenance`)}>回填來源</Button>}
    {name === 'LegacyProvenance' && record.Status === 'manual_review' && typeof record.LegacyInvoiceID === 'string' && typeof record.LegacyAccountID === 'string' && <Button type="link" onClick={() => navigate(`/account-migrations/${encodeURIComponent(record.LegacyAccountID as string)}/provenance/${encodeURIComponent(record.LegacyInvoiceID as string)}/resolve`, { state: { commerce_subscription_id: String(record.CommerceSubscriptionID ?? ''), commerce_invoice_id: String(record.CommerceInvoiceID ?? ''), price_version_id: String(record.PriceVersionID ?? '') } })}>審核修正</Button>}
    {name === 'AccountMigrations' && !record.Stopped && record.ReadOwner === 'legacy' && typeof record.LegacyAccountID === 'string' && <Button type="link" onClick={() => navigate(`/account-migrations/${encodeURIComponent(record.LegacyAccountID as string)}/switch-read`)}>切換讀取</Button>}
    {name === 'AccountMigrations' && !record.Stopped && record.ReadOwner === 'commerce' && record.WriterOwner === 'legacy' && typeof record.LegacyAccountID === 'string' && <Button type="link" onClick={() => navigate(`/account-migrations/${encodeURIComponent(record.LegacyAccountID as string)}/switch-writer`)}>切換寫入</Button>}
    {name === 'AccountMigrations' && !record.Stopped && typeof record.LegacyAccountID === 'string' && <Button type="link" onClick={() => navigate(`/account-migrations/${encodeURIComponent(record.LegacyAccountID as string)}/stop`)}>停止</Button>}
  </Space> })
  return <>
    <Card title={resources.find(([key]) => key === name)?.[1] ?? name} extra={<Space><Text type="secondary">觀測：{new Date(observedAt).toLocaleString()}</Text><Tag>共 {total} 筆</Tag></Space>}>
      <Table
        rowKey={(record) => resourceRowKey(name, record)}
        columns={columns}
        dataSource={rows}
        locale={{ emptyText: <Empty description="目前沒有資料" /> }}
        scroll={{ x: 'max-content' }}
        pagination={false}
      />
    </Card>
    <Drawer title="資料詳情" open={selected !== null} onClose={() => setSelectedKey(null)} extra={<Button onClick={onRefresh} loading={refreshing}>更新</Button>} width={560}>
      {stale && <Alert type="warning" showIcon message="資料更新失敗，以下是上次讀取的結果" />}
      <Text type="secondary">觀測：{new Date(observedAt).toLocaleString()}</Text>
      {selected && <Descriptions column={1} bordered size="small" items={Object.entries(selected).map(([key, value]) => ({ key, label: key, children: <Text copyable>{valueText(value)}</Text> }))} />}
    </Drawer>
  </>
}

function Dashboard() {
  const query = useQuery({ queryKey: ['overview'], queryFn: api.overview })
  if (query.isPending) return <Skeleton active />
  if (!query.data) return <Result status="error" title="概覽載入失敗" subTitle={query.error?.message} extra={<Button onClick={() => void query.refetch()}>重試</Button>} />
  const labels: Record<string, string> = {
    quotes: '報價', subscriptions: '訂閱', invoices: '帳單', payments: '付款',
    refunds: '退款', credits: 'Credit', price_versions: '價格版本', pending_outbox: '待處理 Outbox',
  }
  return <>
    <Space align="center"><Title level={2}>營運概覽</Title><Button onClick={() => void query.refetch()} loading={query.isFetching}>更新資料</Button></Space>
    {query.isError && <Alert type="warning" showIcon message="資料更新失敗，顯示上次讀取結果" description={query.error.message} className="result-card" />}
    <Text type="secondary">資料觀測時間：{new Date(query.data.observed_at).toLocaleString()}</Text>
    <div className="stat-grid">{Object.entries(query.data.counts).map(([key, value]) =>
      <Card key={key}><Statistic title={labels[key] ?? key} value={value} /></Card>)}</div>
  </>
}

function ResourcePage() {
  const { name = '' } = useParams()
  const match = resources.find(([key, , path]) => key === name || path === name)
  if (!match) return <Result status="404" title="找不到頁面" />
  return <ResourceView key={match[0]} name={match[0]} path={match[2]} />
}

function ResourceView({ name, path }: { name: string; path: string }) {
  const [searchParams, setSearchParams] = useSearchParams()
  const filterKeys = resourceFilterKeys[path] ?? []
  const filters: Record<string, string> = {}
  for (const key of filterKeys) {
    const value = searchParams.get(key)
    if (value) filters[key] = value
  }
  const filterSignature = JSON.stringify(filters)
  const cursor = searchParams.get('cursor') ?? ''
  const previousCursors = searchParams.getAll('back')
  const query = useQuery({
    queryKey: ['resource', path, cursor, filterSignature],
    queryFn: () => api.resource(path, cursor, filters),
  })
  const applyFilters = (values: Record<string, string | undefined>) => {
    const next = new URLSearchParams()
    for (const key of filterKeys) {
      const value = values[key]?.trim()
      if (value) next.set(key, value)
    }
    setSearchParams(next)
  }
  const previousPage = () => {
    const history = [...previousCursors]
    const prior = history.pop()
    const next = new URLSearchParams(searchParams)
    next.delete('back')
    for (const value of history) next.append('back', value)
    if (prior) next.set('cursor', prior)
    else next.delete('cursor')
    setSearchParams(next)
  }
  const nextPage = () => {
    if (!query.data?.next_cursor) return
    const next = new URLSearchParams(searchParams)
    next.append('back', cursor)
    next.set('cursor', query.data.next_cursor)
    setSearchParams(next)
  }
  return <>
    <Button onClick={() => void query.refetch()} loading={query.isFetching} className="result-card">更新資料</Button>
    {filterKeys.length > 0 && <Form key={`${path}:${filterSignature}`} layout="inline" initialValues={filters} onFinish={applyFilters} className="resource-filters">
      {filterKeys.map((key) => <Form.Item key={key} name={key} label={resourceFilterLabels[key] ?? key} rules={key === 'created_from' || key === 'created_before' ? [{ validator: async (_rule, value: string | undefined) => {
        const timestamp = value?.trim()
        if (timestamp && !isExactAdminUTC(timestamp)) throw new Error('請輸入有效 UTC 時間（最多 9 位小數秒）')
      } }] : undefined}>
        {key === 'status' && resourceStatusOptions[path]
          ? <Select allowClear options={resourceStatusOptions[path].map((value) => ({ value, label: value }))} style={{ minWidth: 160 }} />
          : <Input allowClear maxLength={256} placeholder={key.startsWith('created_') ? '2026-09-26T00:00:00Z' : undefined} style={{ minWidth: key.startsWith('created_') ? 230 : 170 }} />}
      </Form.Item>)}
      <Form.Item><Space><Button type="primary" htmlType="submit">套用篩選</Button><Button onClick={() => setSearchParams(new URLSearchParams())}>清除篩選</Button></Space></Form.Item>
    </Form>}
    {query.isPending && <Skeleton active />}
    {query.isError && !query.data && <Result status="error" title="資料載入失敗" subTitle={query.error.message} extra={<Button onClick={() => void query.refetch()}>重試</Button>} />}
    {query.isError && query.data && <Alert type="warning" showIcon message="資料更新失敗，顯示上次讀取結果" description={query.error.message} className="result-card" />}
      {query.data && <>
      <StateTable rows={query.data.items} name={name} total={query.data.total} observedAt={query.data.observed_at} stale={query.isError} refreshing={query.isFetching} onRefresh={() => { void query.refetch() }} />
        <Text type="secondary">每頁依查詢當下的資料產生；翻頁不是全域快照。</Text>
        <Space className="pager">
        <Button disabled={previousCursors.length === 0} onClick={previousPage}>上一頁</Button>
        <Text>第 {previousCursors.length + 1} 頁</Text>
        <Button disabled={!query.data.next_cursor} onClick={nextPage}>下一頁</Button>
      </Space>
    </>}
  </>
}

function AdminShell({ session, onLogout }: { session: Session; onLogout: () => void }) {
  const navigate = useNavigate()
  const location = useLocation()
  const [collapsed, setCollapsed] = useState(false)
  const [mobileOpen, setMobileOpen] = useState(false)
  const menuItems = [
    { key: '/', label: '營運概覽' },
    { key: '/quotes/new', label: '建立報價' },
    { key: '/usage-events/new', label: '記錄用量' },
    { key: '/jobs/change-corrections', label: '升級差額更正' },
    { key: '/prices/pro/new', label: '發布 Pro 價格' },
    { key: '/meters/new', label: '註冊計量表' },
    { key: '/prices/metered/new', label: '發布計量價格' },
    { key: '/catalog-selections/new', label: '目錄選價' },
    { key: '/price-migrations/new', label: '規劃價格遷移' },
    { key: '/usage-adjustments/new', label: '調整用量' },
    { key: '/jobs/usage-credit-notes', label: '用量 Credit Note' },
    { key: '/contracts/new', label: '發布合約版本' },
    { key: '/jobs/contract-collections', label: '到期合約收款' },
    { key: '/reconciliation-runs/new', label: '執行對帳' },
    { key: '/account-migrations/new', label: '連結既有帳戶' },
    { key: '/jobs/renewals', label: '到期續約' },
 { key: '/jobs/entitlement-refresh', label: '刷新權益' },
 { key: '/lab/controls', label: '實驗控制' },
    ...resources.map(([, label, path]) => ({ key: resourceRoute[path] ?? `/data/${path}`, label })),
    { key: '/commands', label: '管理命令' },
  ]
  const menu = <Menu theme="dark" mode="inline" selectedKeys={[location.pathname]} items={menuItems} onClick={({ key }) => { navigate(key); setMobileOpen(false) }} />
  return <Layout className="app-layout">
    <Sider collapsible collapsed={collapsed} onCollapse={setCollapsed} className="desktop-sider" width={230}>
      <div className="sidebar-brand">{collapsed ? 'B' : 'BILLFORGE'}</div>{menu}
    </Sider>
    <Layout>
      <Header className="app-header">
        <Space>
          <Button className="mobile-menu-button" onClick={() => setMobileOpen(true)} aria-label="開啟選單">☰</Button>
          <Text strong>Commerce Admin</Text>
        </Space>
        <Space><Text type="secondary">{session.username}</Text><Button onClick={onLogout}>登出</Button></Space>
      </Header>
      <Content className="app-content">
        <Routes>
          <Route path="/" element={<Dashboard />} />
          <Route path="/customers" element={<ResourceView name="Customers" path="customers" />} />
          <Route path="/quotes" element={<ResourceView name="Quotes" path="quotes" />} />
          <Route path="/subscriptions" element={<ResourceView name="Subscriptions" path="subscriptions" />} />
          <Route path="/invoices" element={<ResourceView name="Invoices" path="invoices" />} />
          <Route path="/payments" element={<ResourceView name="Payments" path="payments" />} />
          <Route path="/payments/:id" element={<ExternalOperationDetail kind="payment" />} />
      <Route path="/credits" element={<ResourceView name="Credits" path="credits" />} />
      <Route path="/credits/:id" element={<CreditDetail />} />
          <Route path="/refunds" element={<ResourceView name="Refunds" path="refunds" />} />
          <Route path="/refunds/:id" element={<ExternalOperationDetail kind="refund" />} />
          <Route path="/catalog/prices" element={<ResourceView name="Prices" path="prices" />} />
          <Route path="/catalog/meters" element={<ResourceView name="Meters" path="meters" />} />
          <Route path="/price-migrations" element={<ResourceView name="PriceMigrations" path="price-migrations" />} />
          <Route path="/contracts" element={<ResourceView name="Contracts" path="contracts" />} />
          <Route path="/usage" element={<ResourceView name="UsageEvents" path="usage-events" />} />
          <Route path="/reconciliation" element={<ResourceView name="ReconciliationRuns" path="reconciliation-runs" />} />
          <Route path="/discrepancies" element={<ResourceView name="Discrepancies" path="discrepancies" />} />
          <Route path="/account-migrations" element={<ResourceView name="AccountMigrations" path="account-migrations" />} />
          <Route path="/outbox" element={<ResourceView name="PendingOutbox" path="outbox" />} />
          <Route path="/quotes/new" element={<CreateQuote session={session} />} />
          <Route path="/quotes/:id/accept" element={<AcceptQuote session={session} />} />
          <Route path="/customers/:id" element={<CustomerDetail />} />
          <Route path="/price-migrations/:id" element={<PriceMigrationDetail />} />
          <Route path="/price-migrations/:id/pause" element={<PauseMigration session={session} />} />
          <Route path="/usage-events/new" element={<RecordUsage session={session} />} />
          <Route path="/usage-periods/:id/:index" element={<UsagePeriodDetail />} />
          <Route path="/subscriptions/:id/cancel" element={<ScheduleCancel session={session} />} />
          <Route path="/subscriptions/:id" element={<SubscriptionDetail />} />
          <Route path="/invoices/:id" element={<InvoiceDetail />} />
          <Route path="/invoices/:id/history/corrections" element={<InvoiceHistory kind="corrections" />} />
          <Route path="/invoices/:id/history/applications" element={<InvoiceHistory kind="applications" />} />
          <Route path="/invoices/:id/history/grants" element={<InvoiceHistory kind="grants" />} />
          <Route path="/invoices/:id/history/refunds" element={<InvoiceHistory kind="refunds" />} />
          <Route path="/discrepancies/:id" element={<DiscrepancyDetail />} />
          <Route path="/reconciliation-runs/:id" element={<ReconciliationRunDetail />} />
          <Route path="/account-migrations/:id" element={<AccountMigrationDetail />} />
          <Route path="/account-migrations/:id/shadow-history" element={<AccountMigrationHistory kind="shadows" />} />
          <Route path="/account-migrations/:id/provenance-history" element={<AccountMigrationHistory kind="provenance" />} />
          <Route path="/subscriptions/:id/schedule-plan" element={<SchedulePlan session={session} />} />
          <Route path="/subscriptions/:id/upgrade" element={<SchedulePlan session={session} immediate />} />
          <Route path="/invoices/:id/payments/new" element={<CreatePayment session={session} />} />
          <Route path="/invoices/:id/reductions/new" element={<ActionForm session={session} config={reductionAction} />} />
          <Route path="/credits/:id/apply" element={<ActionForm session={session} config={applyCreditAction} />} />
          <Route path="/changes/:id/resolve-unfulfilled" element={<ActionForm session={session} config={resolveUnfulfilledAction} />} />
          <Route path="/jobs/change-corrections" element={<ActionForm session={session} config={changeCorrectionsAction} />} />
          <Route path="/credits/:id/refunds/new" element={<ActionForm session={session} config={reserveRefundAction} />} />
          <Route path="/payments/:id/dispatch" element={<ActionForm session={session} config={dispatchPaymentAction} />} />
          <Route path="/payments/:id/reconcile" element={<ActionForm session={session} config={reconcilePaymentAction} />} />
          <Route path="/refunds/:id/dispatch" element={<ActionForm session={session} config={dispatchRefundAction} />} />
          <Route path="/refunds/:id/reconcile" element={<ActionForm session={session} config={reconcileRefundAction} />} />
          <Route path="/prices/pro/new" element={<ActionForm session={session} config={publishProPriceAction} />} />
          <Route path="/meters/new" element={<ActionForm session={session} config={registerMeterAction} />} />
          <Route path="/prices/metered/new" element={<ActionForm session={session} config={publishMeteredPriceAction} />} />
          <Route path="/catalog-selections/new" element={<ActionForm session={session} config={selectCatalogPriceAction} />} />
          <Route path="/price-migrations/new" element={<ActionForm session={session} config={planMigrationAction} />} />
          <Route path="/price-migrations/:id/skip" element={<ActionForm session={session} config={skipMigrationAction} />} />
          <Route path="/price-migrations/:id/resume" element={<ActionForm session={session} config={resumeMigrationAction} />} />
          <Route path="/usage-adjustments/new" element={<ActionForm session={session} config={adjustUsageAction} />} />
          <Route path="/usage-periods/:id/close" element={<ActionForm session={session} config={closeUsageAction} />} />
          <Route path="/usage-periods/:id/rerate" element={<ActionForm session={session} config={rerateUsageAction} />} />
          <Route path="/jobs/usage-credit-notes" element={<ActionForm session={session} config={usageCreditNotesAction} />} />
          <Route path="/contracts/new" element={<ActionForm session={session} config={publishContractAction} />} />
          <Route path="/jobs/contract-collections" element={<ActionForm session={session} config={collectContractInvoicesAction} />} />
          <Route path="/reconciliation-runs/new" element={<ActionForm session={session} config={runReconciliationAction} />} />
          <Route path="/discrepancies/:id/repair" element={<ActionForm session={session} config={repairDiscrepancyAction} />} />
          <Route path="/discrepancies/:id/manual-decisions" element={<ActionForm session={session} config={manualDecisionAction} />} />
          <Route path="/account-migrations/new" element={<ActionForm session={session} config={linkAccountAction} />} />
          <Route path="/account-migrations/:id/shadow-quotes" element={<ActionForm session={session} config={shadowQuoteAction} />} />
          <Route path="/account-migrations/:id/shadow-entitlements" element={<ActionForm session={session} config={shadowEntitlementAction} />} />
          <Route path="/account-migrations/:id/provenance" element={<ActionForm session={session} config={backfillProvenanceAction} />} />
          <Route path="/account-migrations/:accountId/provenance/:legacyInvoiceId/resolve" element={<ActionForm session={session} config={resolveProvenanceAction} />} />
          <Route path="/account-migrations/:id/switch-read" element={<ActionForm session={session} config={switchReadAction} />} />
          <Route path="/account-migrations/:id/switch-writer" element={<ActionForm session={session} config={switchWriterAction} />} />
          <Route path="/account-migrations/:id/stop" element={<ActionForm session={session} config={stopMigrationAction} />} />
          <Route path="/jobs/renewals" element={<ActionForm session={session} config={runRenewalsAction} />} />
 <Route path="/jobs/entitlement-refresh" element={<ActionForm session={session} config={refreshEntitlementsAction} />} />
 <Route path="/jobs/:id" element={<JobDetails />} />
 <Route path="/lab/controls" element={<LabControls />} />
 <Route path="/lab/clock" element={<ActionForm session={session} config={clockAction} />} />
 <Route path="/lab/payment-decisions/:id" element={<ActionForm session={session} config={paymentDecisionAction} />} />
 <Route path="/lab/refund-decisions/:id" element={<ActionForm session={session} config={refundDecisionAction} />} />
 <Route path="/lab/faults/:id" element={<ActionForm session={session} config={faultAction} />} />
          <Route path="/payments/:id/retry" element={<RetryPayment session={session} />} />
          <Route path="/commands" element={<CommandList session={session} />} />
          <Route path="/commands/:id" element={<CommandDetail session={session} />} />
          <Route path="/data/:name" element={<ResourcePage />} />
          <Route path="*" element={<Result status="404" title="找不到頁面" />} />
        </Routes>
      </Content>
    </Layout>
    <Drawer title="Billforge" placement="left" open={mobileOpen} onClose={() => setMobileOpen(false)} styles={{ body: { padding: 0, background: '#122b31' } }} width={250}>{menu}</Drawer>
  </Layout>
}

export default function App() {
  const queryClient = useQueryClient()
  const location = useLocation()
  const navigate = useNavigate()
  const { message } = AntApp.useApp()
  useEffect(() => {
    const onSessionExpired = () => {
      advanceSessionGeneration()
      queryClient.clear()
      queryClient.setQueryData<Session | null>(['session'], null)
      navigate('/login', { replace: true, state: { from: location.pathname } })
    }
    window.addEventListener(sessionExpiredEvent, onSessionExpired)
    return () => window.removeEventListener(sessionExpiredEvent, onSessionExpired)
  }, [location.pathname, navigate, queryClient])
  const sessionQuery = useQuery({ queryKey: ['session'], queryFn: api.session, retry: false })
  const logout = useMutation({
    mutationFn: () => api.logout(sessionQuery.data!.csrf_token),
    onSuccess: () => { advanceSessionGeneration(); queryClient.clear(); navigate('/login', { replace: true }) },
    onError: (error) => {
      if (error instanceof HttpError && error.status === 401) {
        window.dispatchEvent(new Event(sessionExpiredEvent))
        return
      }
      void message.error(error instanceof Error ? error.message : '登出失敗')
    },
  })
  if (sessionQuery.isPending) return <div className="startup"><Skeleton active paragraph={{ rows: 3 }} /></div>
  if (sessionQuery.isError && (!(sessionQuery.error instanceof HttpError) || sessionQuery.error.status !== 401)) {
    return <Result status="error" title="無法連線到管理服務" subTitle={sessionQuery.error.message} extra={<Button onClick={() => void sessionQuery.refetch()}>重試</Button>} />
  }
  if (!sessionQuery.data) {
    return location.pathname === '/login'
      ? <Login onLogin={(session) => queryClient.setQueryData(['session'], session)} />
      : <Navigate to="/login" state={{ from: location.pathname }} replace />
  }
  if (location.pathname === '/login') return <Navigate to={loginReturnPath(location.state)} replace />
  return <AdminShell session={sessionQuery.data} onLogout={() => logout.mutate()} />
}
