export type ApiFailure = { error?: { code?: string; message?: string; retryable?: boolean }; command_id?: string; request_id?: string }
export const sessionExpiredEvent = 'billforge:session-expired'
let sessionGeneration = 0

export function advanceSessionGeneration(): void {
  sessionGeneration += 1
}

export type Session = {
  username: string
  actor_id: string
  capabilities: string[]
  csrf_token: string
}

export type Overview = {
 observed_at: string
 counts: Record<string, number>
}

export type ExternalOperationDetail = {
 operation: { id: string; status: string; outbox_status: string; amount_minor: string; currency: string; source_id: string }
 observed_at: string
}

export type PriceVersionDetail = {
 ID: string
 PlanID: string
 Version: string
 Currency: string
 FixedMinor: string
 Checksum: string
 PublicationState: 'draft' | 'published'
 PublishedAt: string
 EffectiveFrom: string
 EffectiveTo: string | null
 SelectionCount: string
 Components: { Code: string; Kind: string; AmountMinor: string; Quantity: string; RateNum: string; RateDen: string; MeterID: string }[]
 ComponentsTruncated: boolean
}

export type ContractVersionDetail = {
 ID: string
 CustomerID: string
 Version: string
 BasePriceVersionID: string
 Currency: string
 FixedMinor: string
 SeatMinor: string
 PaymentDays: string
 EffectiveFrom: string
 EffectiveTo: string
 PostContractPriceVersionID: string
 Checksum: string
 PublishedAt: string
 QuoteCount: string
 SubscriptionCount: string
 Quotes: { ID: string; SeatQuantity: string; AmountMinor: string; Currency: string; ExpiresAt: string; Accepted: boolean }[]
 QuotesTruncated: boolean
 Subscriptions: { ID: string; Status: string; SeatQuantity: string; LatestInvoiceID: string; LatestDueAt: string | null; InvoiceOutstandingMinor: string | null; InvoiceCurrency: string; Transitioned: boolean }[]
 SubscriptionsTruncated: boolean
}

export type LabClock = { mode: 'real' | 'fixed'; value_utc?: string; revision: number; business_time: string }
export type LabFaultTicket = { id: string; operation_kind: string; operation_id: string; mode: string; claimed_command_id?: string; created_at: string }
export type LabStatus = { status: { clock: Omit<LabClock, 'revision'> & { revision: string }; pending_fault_tickets: string; provider: { captures: string; refunds: string; capture_decisions: string; refund_decisions: string } }; observed_at: string; provider_observed_at: string }
export type ProviderOperation = { provider_key: string; source_capture_key?: string; amount_minor: string; currency: string; status: string }

export type ResourcePage = {
  items: Record<string, unknown>[]
  total: number
  next_cursor: string
  observed_at: string
}

export type Command = {
  id: string
  actor_id: string
  idempotency_key?: string
  request_id?: string
  action_id: string
  target_id: string
  status: 'accepted' | 'running' | 'succeeded' | 'failed' | 'waiting_verification'
  result_refs?: Record<string, string>
  error_code?: string
  created_at: string
  updated_at: string
}

export type JobItem = { id: string; target_type: string; target_id: string; period_key: string; status: string; result_refs?: Record<string, unknown>; error_code?: string; updated_at: string }
export type Job = { id: string; command_id: string; kind: string; status: string; created_at: string; updated_at: string; items: JobItem[] }
export type JobSummary = { id: string; command_id: string; kind: string; status: string; created_at: string; updated_at: string; total_items: string; succeeded_items: string; attention_items: string }

export type QuoteDetail = {
  ID: string
  CustomerID: string
  PriceVersionID: string
  ContractVersionID: string
  AmountMinor: string
  Currency: string
  ExpiresAt: string
  Fingerprint: string
  Accepted: boolean
  ChangeMode: '' | 'next_period' | 'immediate'
  ChangeSubscriptionID: string
  BindingFingerprint: string
  DueNowMinor: string | null
  NextFullTermFixedMinor: string
  UsageMeterID: string
  IncludedQuantity: string
  UsageRateNum: string
  UsageRateDen: string
}

export type Preview = {
  preview_id: string
  actor_id: string
  action_id: string
  target_id: string
  expires_at: string
  source_versions: Record<string, string>
  impact: Record<string, string>
  blocking_reasons: string[]
}

export type MigrationDetail = {
  ID: string
  Cohort: string
  TargetPriceVersionID: string
  Status: string
  ItemCount: string
  PendingCount: string
  AppliedCount: string
  ConflictedCount: string
  SkippedCount: string
}

export type MigrationItem = {
  MigrationID: string
  SubscriptionID: string
  FromPriceVersionID: string
  TargetPriceVersionID: string
  SeatQuantity: string
  ExpectedRevision: string
  PriorAmountMinor: string
  TargetAmountMinor: string
  CurrentEntitlementStatus: string
  ProjectedEntitlementRule: string
  EffectiveAt: string
  Status: string
  ConflictReason: string
}

export type SubscriptionDetail = {
  ID: string
  CustomerID: string
  PriceVersionID: string
  SeatQuantity: string
  Revision: string
  Status: string
  EntitlementStatus: string
  EntitlementReason: string
  EntitlementSourceRevision: string
  QuoteID: string
  ContractVersionID: string
  PricePlanID: string
  Currency: string
  ActualFixedMinor: string
  ActualSeatMinor: string
  CurrentPeriod?: { Index: string; Start: string; End: string; DueAt: string; InvoiceID: string }
  ScheduledChange?: { ID: string; TargetPriceVersionID: string; SeatQuantity: string; EffectiveAt: string; Status: string }
  ScheduledCancel?: { ID: string; EffectiveAt: string; Status: string }
  HoldReason: string
}

export type SubscriptionPeriod = {
  Index: string
  Start: string
  End: string
  DueAt: string
  InvoiceID: string
}

export type SubscriptionTimelineEvent = {
  At: string
  Kind: string
  Reference: string
  Status: string
}

export type SubscriptionEntitlement = {
  SubscriptionID: string
  Status: string
  Reason: string
  SourceOperationID: string
  SourceInvoiceID: string
  SourceRevision: string
  UpdatedAt: string
  GraceDeadline?: string | null
}

export type CursorPage<T> = { items: T[]; next_cursor: string; observed_at: string }

export type UsageRating = {
 ID: string
 SubscriptionID: string
 PeriodIndex: string
 Revision: string
 PriceVersionID: string
 Quantity: string
 IncludedQuantity: string
 OverageQuantity: string
 ExactMinorNumerator: string
 ExactMinorDenominator: string
 RoundedMinor: string
 DeltaMinor: string
 RatedAt: string
}

export type UsagePeriodDetail = {
 SubscriptionID: string
 PeriodIndex: string
 PeriodStart: string
 PeriodEnd: string
 PriceVersionID: string
 Currency: string
 Status: 'estimated' | 'finalized' | 'rerated'
 CutoffAt: string | null
 ClosedAt: string | null
 RatedMinor: string
 BilledMinor: string
 CreditedMinor: string
 AppliedCount: string
 CreditNoteCount: string
 CreditNoteMinor: string
 Estimate: UsageRating | null
 LatestRating: UsageRating | null
}

export type InvoiceDetail = {
  ID: string
  SubscriptionID: string
  FinalizedAt: string | null
  Period: SubscriptionPeriod | null
  Balance: {
    Currency: string
    OriginalMinor: string
    ReductionsMinor: string
    ObligationMinor: string
    GrossCapturedMinor: string
    ReleasedMinor: string
    CreditAppliedMinor: string
    NetAppliedMinor: string
    OutstandingMinor: string
  }
  Lines: Array<{ price_version_id: string; component_code: string; amount_minor: string }>
  Payments: Array<{ ID: string; AmountMinor: string; Currency: string; Status: string }>
  PaymentsTruncated: boolean
  Corrections: Array<{ ID: string; ReductionMinor: string; PriorObligationMinor: string; NewObligationMinor: string; Reason: string; OriginKind: string; OriginID: string; CreatedAt: string }>
  CorrectionsTruncated: boolean
  CreditApplications: Array<{ ID: string; GrantID: string; SourceInvoiceID: string; AmountMinor: string; CreatedAt: string }>
  CreditApplicationsTruncated: boolean
  CreditGrants: Array<{ ID: string; CorrectionID: string; ReleaseID: string; SourceOperationID: string; AmountMinor: string; CreatedAt: string }>
  CreditGrantsTruncated: boolean
  Refunds: Array<{ ID: string; GrantID: string; AmountMinor: string; Currency: string; Status: string; CreatedAt: string }>
  RefundsTruncated: boolean
}

export type CreditDetail = {
  ID: string
  ReleaseID: string
  SourceCorrectionID: string
  SourceOperationID: string
  SourceInvoiceID: string
  CreatedAt: string
  Balance: {
    GrantID: string
    Currency: string
    GrantedMinor: string
    AppliedMinor: string
    ReservedMinor: string
    RefundedMinor: string
    AvailableMinor: string
  }
}

export type InvoiceHistoryKind = 'corrections' | 'applications' | 'grants' | 'refunds'
export type InvoiceHistoryRow = InvoiceDetail['Corrections'][number] | InvoiceDetail['CreditApplications'][number] | InvoiceDetail['CreditGrants'][number] | InvoiceDetail['Refunds'][number]

export type DiscrepancyDetail = {
  Discrepancy: {
    ID: string
    Kind: string
    ObjectID: string
    Classification: string
    Expected: string
    Actual: string
    Evidence: string
    SourceRevision: string
    Status: string
  }
  FirstSeenAt: string
  LastSeenAt: string
  Resolution: string
  Runs: Array<{ ID: string; CutoffAt: string; LocalObservedAt: string; ProviderObservedAt: string }>
  Repairs: Array<{ ID: string; Action: string; Expected: string; Actual: string; PreconditionRevision: string; Status: string; Verification: string; CreatedAt: string; ExecutedAt: string | null }>
  Decisions: Array<{ ID: string; Reviewer: string; Decision: string; Reason: string; CreatedAt: string }>
  RunsTruncated: boolean
  RepairsTruncated: boolean
  DecisionsTruncated: boolean
}

export type ReconciliationRunPage = {
  Run: { ID: string; CutoffAt: string; LocalObservedAt: string; ProviderObservedAt: string; FindingCount: string }
  Findings: Array<{
    DiscrepancyID: string
    Kind: string
    ObjectID: string
    Classification: string
    Expected: string
    Actual: string
    Evidence: string
    SourceRevision: string
    SnapshotQuality: 'recorded' | 'backfilled_current'
    CurrentStatus: string
  }>
  NextID: string
}

export type AccountMigrationDetail = {
  Link: { LegacyAccountID: string; CustomerID: string; BeneficiaryID: string; Cohort: string; HasHistory: boolean; ReadOwner: string; WriterOwner: string; Stopped: boolean; StopReason: string }
  Shadows: Array<{ ID: string; Kind: string; ObjectID: string; Expected: string; Actual: string; Matched: boolean; LatencyMillis: string; ObservedAt: string }>
  Provenance: Array<{ LegacyInvoiceID: string; LegacySubscriptionID: string; CommerceSubscriptionID: string; CommerceInvoiceID: string; PriceVersionID: string; Status: string; Evidence: string }>
  Events: Array<{ ID: string; Kind: string; Detail: string; At: string }>
  ShadowsTruncated: boolean
  ProvenanceTruncated: boolean
  EventsTruncated: boolean
}

export type MigrationReadiness = {
  LegacyAccountID: string
  Reconciled: boolean
  QuoteMatches: boolean
  EntitlementMatches: boolean
  ProvenanceComplete: boolean
  QuoteP95Millis: string
  UnknownPayments: string
  OpenDiscrepancies: string
  Ready: boolean
}

export type MigrationThresholds = { max_quote_p95_millis: string; max_unknown_payments: string; max_open_discrepancies: string }
export type AccountEntitlementRead = { Owner: string; Status: string; SourceRevision: string; AsOf: string }

export type CustomerDetail = {
  ID: string
  SubscriptionCount: string
  QuoteCount: string
  InvoiceCount: string
  CreditCount: string
  RelatedLimit: string
  LegacyAccountID: string
  ReadOwner: string
  WriterOwner: string
  Subscriptions: Array<{ ID: string; PriceVersionID: string; SeatQuantity: string; Revision: string; Status: string; EntitlementStatus: string }>
  Quotes: Array<{ ID: string; AmountMinor: string; Currency: string; ExpiresAt: string; Accepted: boolean; ContractVersionID: string }>
  Invoices: Array<{ ID: string; SubscriptionID: string; TotalMinor: string; Currency: string }>
  Credits: Array<{ ID: string; InvoiceID: string; AmountMinor: string; Currency: string }>
}

export class HttpError extends Error {
  constructor(public status: number, public code: string, message: string, public commandID?: string, public requestID?: string) {
    super(message)
  }
}

export function canShowStaleRead(error: unknown): boolean {
  return !(error instanceof HttpError) || error.status >= 500 || error.status === 408 || error.status === 429
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const requestGeneration = sessionGeneration
  const read = (init?.method ?? 'GET').toUpperCase() === 'GET'
  const timeoutController = new AbortController()
  const timeoutID = window.setTimeout(() => timeoutController.abort(), read ? 10_000 : 20_000)
  const signal = init?.signal ? AbortSignal.any([init.signal, timeoutController.signal]) : timeoutController.signal
  try {
    const response = await fetch(`/admin/api${path}`, {
      credentials: 'same-origin',
      cache: 'no-store',
      ...init,
      signal,
      headers: {
        ...(init?.body ? { 'Content-Type': 'application/json' } : {}),
        ...init?.headers,
      },
    })
    if (!response.ok) {
      let failure: ApiFailure = {}
      try { failure = (await response.json()) as ApiFailure } catch (error) {
        if (timeoutController.signal.aborted) throw error
        // Keep the HTTP status when a response has no JSON body.
      }
      if (response.status === 401 && path !== '/session' && path !== '/session/csrf' && requestGeneration === sessionGeneration) {
        window.dispatchEvent(new Event(sessionExpiredEvent))
      }
      throw new HttpError(
        response.status,
        failure.error?.code ?? 'HTTP_ERROR',
        failure.error?.message ?? `HTTP ${response.status}`,
        failure.error?.code === 'COMMAND_PENDING_RETRY' && typeof failure.command_id === 'string' && failure.command_id !== ''
          ? failure.command_id
          : undefined,
        failure.request_id,
      )
    }
    if (response.status === 204) return undefined as T
    return (await response.json()) as T
  } catch (error) {
    if (timeoutController.signal.aborted) {
      throw new HttpError(0, 'REQUEST_TIMEOUT', read ? '讀取逾時，請重新讀取' : '回應逾時，請查詢原操作狀態')
    }
    throw error
  } finally {
    window.clearTimeout(timeoutID)
  }
}

export const api = {
  session: () => request<Session>('/session'),
  csrf: () => request<{ csrf_token: string }>('/session/csrf'),
  async login(username: string, password: string): Promise<Session> {
    const { csrf_token } = await this.csrf()
    return request<Session>('/session', {
      method: 'POST',
      headers: { 'X-CSRF-Token': csrf_token },
      body: JSON.stringify({ username, password }),
    })
  },
  logout: (csrfToken: string) => request<void>('/session', { method: 'DELETE', headers: { 'X-CSRF-Token': csrfToken } }),
 overview: () => request<Overview>('/overview'),
 clock: () => request<LabClock>('/lab/clock'),
  faults: (cursor = '') => request<CursorPage<LabFaultTicket>>(`/lab/faults?limit=20${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ''}`),
  labStatus: () => request<LabStatus>('/lab/status'),
  providerOperations: (kind: 'captures' | 'refunds', cursor = '', status = '') => {
    const params = new URLSearchParams({ limit: '20' })
    if (cursor) params.set('cursor', cursor)
    if (status) params.set('status', status)
    return request<CursorPage<ProviderOperation>>(`/lab/provider-${kind}?${params}`)
  },
  job: (id: string) => request<Job>(`/jobs/${encodeURIComponent(id)}`),
  jobs: (cursor = '') => request<CursorPage<JobSummary>>(`/jobs?limit=20${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ''}`),
  resource: (name: string, cursor = '', filters: Record<string, string> = {}) => {
    const params = new URLSearchParams({ limit: '20' })
    if (cursor) params.set('cursor', cursor)
    for (const [key, value] of Object.entries(filters)) {
      if (value) params.set(key, value)
    }
    return request<ResourcePage>(`/${name}?${params.toString()}`)
  },
 quote: (id: string) => request<QuoteDetail>(`/quotes/${encodeURIComponent(id)}`),
 price: (id: string) => request<{ price: PriceVersionDetail; observed_at: string }>(`/prices/${encodeURIComponent(id)}`),
 contract: (id: string) => request<{ contract: ContractVersionDetail; observed_at: string }>(`/contracts/${encodeURIComponent(id)}`),
  subscription: (id: string) => request<SubscriptionDetail>(`/subscriptions/${encodeURIComponent(id)}`),
 subscriptionPeriods: (id: string, cursor = '') => request<CursorPage<SubscriptionPeriod>>(`/subscriptions/${encodeURIComponent(id)}/periods?limit=20${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ''}`),
 usagePeriod: (id: string, index: string) => request<{ period: UsagePeriodDetail; observed_at: string }>(`/usage-periods/${encodeURIComponent(id)}/${encodeURIComponent(index)}`),
 usagePeriodRatings: (id: string, index: string, cursor = '') => request<CursorPage<UsageRating>>(`/usage-periods/${encodeURIComponent(id)}/${encodeURIComponent(index)}/ratings?limit=20${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ''}`),
  subscriptionTimeline: (id: string, cursor = '') => request<CursorPage<SubscriptionTimelineEvent>>(`/subscriptions/${encodeURIComponent(id)}/timeline?limit=20${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ''}`),
  subscriptionEntitlement: (id: string) => request<{ entitlement: SubscriptionEntitlement | null; observed_at: string }>(`/subscriptions/${encodeURIComponent(id)}/entitlement`),
  invoice: (id: string) => request<{ invoice: InvoiceDetail; observed_at: string }>(`/invoices/${encodeURIComponent(id)}`),
  credit: (id: string) => request<{ credit: CreditDetail; observed_at: string }>(`/credits/${encodeURIComponent(id)}`),
  invoiceHistory: <T extends InvoiceHistoryRow>(id: string, kind: InvoiceHistoryKind, cursor = '') => request<CursorPage<T> & { currency: string }>(`/invoices/${encodeURIComponent(id)}/history/${kind}?limit=20${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ''}`),
  discrepancy: (id: string) => request<{ discrepancy: DiscrepancyDetail; observed_at: string }>(`/discrepancies/${encodeURIComponent(id)}`),
  reconciliationRun: (id: string, cursor = '') => request<{ page: ReconciliationRunPage; next_cursor: string; observed_at: string }>(`/reconciliation-runs/${encodeURIComponent(id)}?limit=20${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ''}`),
  accountMigration: (id: string) => request<{ migration: AccountMigrationDetail; observed_at: string }>(`/account-migrations/${encodeURIComponent(id)}`),
  accountMigrationReadiness: (id: string, limits: MigrationThresholds) => request<{ readiness: MigrationReadiness; observed_at: string }>(`/account-migrations/${encodeURIComponent(id)}/readiness?${new URLSearchParams(limits).toString()}`),
  accountMigrationEntitlement: (id: string, subscriptionID: string) => request<{ entitlement: AccountEntitlementRead; observed_at: string }>(`/account-migrations/${encodeURIComponent(id)}/entitlements/${encodeURIComponent(subscriptionID)}`),
  accountMigrationShadows: (id: string, cursor = '') => request<CursorPage<AccountMigrationDetail['Shadows'][number]>>(`/account-migrations/${encodeURIComponent(id)}/shadows?limit=20${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ''}`),
  accountMigrationProvenance: (id: string, cursor = '') => request<CursorPage<AccountMigrationDetail['Provenance'][number]>>(`/account-migrations/${encodeURIComponent(id)}/provenance?limit=20${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ''}`),
  customer: (id: string) => request<CustomerDetail>(`/customers/${encodeURIComponent(id)}`),
  migration: (id: string) => request<MigrationDetail>(`/price-migrations/${encodeURIComponent(id)}`),
  migrationItems: (id: string, status = '', cursor = '') => request<CursorPage<MigrationItem>>(`/price-migrations/${encodeURIComponent(id)}/items?limit=20${status ? `&status=${encodeURIComponent(status)}` : ''}${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ''}`),
  createPreview: (csrfToken: string, actionID: string, targetID: string, payload: unknown) =>
    request<Preview>('/previews', {
      method: 'POST',
      headers: { 'X-CSRF-Token': csrfToken },
      body: JSON.stringify({ action_id: actionID, target_id: targetID, payload }),
    }),
  preview: (id: string) => request<Preview>(`/previews/${encodeURIComponent(id)}`),
  commands: (cursor = '') => request<{ items: Command[]; next_cursor: string }>(`/commands?limit=20${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ''}`),
 command: (id: string) => request<Command>(`/commands/${encodeURIComponent(id)}`),
 externalOperation: (actionID: 'C09' | 'C16', id: string) => request<ExternalOperationDetail>(`/${actionID === 'C09' ? 'payments' : 'refunds'}/${encodeURIComponent(id)}`),
  resumeCommand: (csrfToken: string, id: string) => request<Command>(`/commands/${encodeURIComponent(id)}/resume`, {
    method: 'POST', headers: { 'X-CSRF-Token': csrfToken }, body: JSON.stringify({}),
  }),
  submitCommand: (csrfToken: string, key: string, body: { action_id: string; target_id?: string; preview_id?: string; payload: unknown }) =>
    request<Command>('/commands', {
      method: 'POST',
      headers: { 'X-CSRF-Token': csrfToken, 'Idempotency-Key': key },
      body: JSON.stringify(body),
    }),
}
