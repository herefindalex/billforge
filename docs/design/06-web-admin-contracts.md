# Web Admin Contracts

**English** | [繁體中文](06-web-admin-contracts.zh-TW.md) | [简体中文](06-web-admin-contracts.zh-CN.md)


Status: Web Admin implementation and local A01-A30 acceptance are complete as of 2026-10-03. The original design scope remains unchanged; current evidence and limits are in [the implementation record](../implementation/web-admin.md).

## 1. Defined architecture

- The front end: React, TypeScript, Vite, React Router, TanStack Query, Ant Design and Zod. Ant Design Form is responsible for interactive forms and instant field prompts. Zod verifies API DTO with input formats such as amount, date, etc. The client rendering is used without introducing SSR or secondary business backend. The UI is not confused with React Hook Form, Radix UI, Refinine or any other component framework.
- Package management: pnpm; choose compatible stable versions and submit a lockfile during implementation, do not install packages or pretend unresolved version numbers during the planning phase.
- Front of the line:`web/admin/`Go manages HTTP:`api/admin/`Search, command, and migration are still ongoing.`lab/`It's easy to reuse an existing unimported transaction helper to avoid secondary logics.
- Visual and interaction benchmarks: with Ant Design's ConfigProvider/App unified theme tokens, spaces, fonts, locals and message containers; The page uses existing components such as Layout, Menu, Table, Form, Descriptions, Alert, Modal, Drawer, Result, Empty. The business components DataTable, Money, StatusBadge, ObjectLink, RevisionNotice, Timeline, CommandProgress, PreviewDiff, ConfirmAction, FieldError, EmptyState, and ErrorState are the language fields that contain them; It's easy to use a small amount of local CSS to align layout with amount. The state presents text and icons at the same time, not just by color.
- Financial Input and Confirmation: The amount and high-precision rates used for string input and explicit format analysis, without determining the financial value by a JavaScript number or Ant Design InputNumber. ConfirmAction must display the server preview, version and impact range, and send and restore progress according to the command contract; Ant Design Modal/Popconfirm only offers interactive shades, and does not assume authorization, idempotent or financial correctness.
- Added start entry: `lab admin commerce.db provider.db [127.0.0.1:8080]`. This server only loads static pages, sessions and managing APIs; ** Do not install a currently unauthorized v1 writing path**** Keep existing `lab serve` as an independent native API experimental input.
- In addition to the above, there is a list of the most commonly used names for the `/admin/api` API. The following pages link to `/admin`: It supports explicit IPv4/IPv6 loopback. CLI, v1, and admin will ultimately re-check the same domain, but the native file owner will not be isolated from Web RBAC; Don't use SQLite when multiple tenants are at the border.
- Release build using the `adminui` build tag to embed the front-end asset; `go test ./...`/Pure API build does not require dist. When running a binary without embedded assets, `admin` clearly indicates a lack of setup steps. It's not a good idea to go back and forth on empty pages.
- Vite develops a loopback-only, proxy-managed API; Only a clearly defined development origin can be used. The session and Origin checks are still valid.

The front end does not directly call `State()`, no internal API tokens, no receipt amounts, no expired previews as authorization basis.

## 2. Page contracts with the frontend boundary

| Route (without `/admin`) | Primary data and operations | Related panels |
| --- | --- | --- |
| `/` |Operating hours and data observation times|Link to the selected list|
| `/customers`, `/customers/:id` | Customer summary, subscription list, and new purchase | Invoices, credits, and legacy mapping |
| `/quotes`、`/quotes/:id` |It also includes the offer, components, uses, deadlines and fingerprints. Accept or create new offers.|Customer, binding subscriptions, the necessary client capabilities|
| `/subscriptions`、`/subscriptions/:id` |The actual and final prices, seats, revisions; Offer/Upgrade/Cancel/Restore|Billing period, payment, entitlement source, timeline|
| `/invoices`、`/invoices/:id`、`/credits` |Source of bills, credit allowance; Repair / Application / Compensation|Lines, allocations, sources of refunds|
| `/payments`、`/payments/:id`、`/refunds` |It's the responsibility of the providers, the providers, the observers. Send out / verify|Command and the source of the original funding.|
| `/catalog/prices`、`/catalog/prices/:id`、`/catalog/meters` |This is a list of all the different types of content that you can find on the website.|Checksum, scope of application|
| `/price-migrations`、`/price-migrations/:id` |This is a great way to get started. In the meantime, we're going to have to make a decision.|New and old assignments and conflicts|
| `/usage`、`/usage/:subscriptionId/:periodIndex` | Events, ratings, late fee; closing/recalculation/credit note | Invoice line cancellation |
| `/contracts`、`/contracts/:id` | Contract terms, Net30, and subsequent pricing; posting/bidding/receipt at maturity | Corresponds to subscriptions and invoices |
| `/reconciliation`、`/discrepancies/:id` |It's not just a question of what's going on. Repair/Artificial Resolution|Repair before and after revision/run|
| `/account-migrations`, `/account-migrations/:legacyId` | Mapping, shadow comparison, backfill, readiness, owners, cutover, and stop controls | Legacy provenance and adapter view |
| `/commands`、`/commands/:id`、`/jobs/:id` |Command/job status, results by item|The field objects generated.|
| `/lab` |Simulated clocks, fake providers, and support failures.|Commands/operations affected|

The front-end data is searched by resource+ID+filters+cursor as a Query key, after the mutation has been completed; Financial writing is not an optimistic success. List searches debounce 250ms, URLs are saved, filtered, limit missing 25, up to 100. The command page starts the routing in 2 seconds, with no change to the back to 10 seconds. The page stopped questioning and returned to page re-verification. When refreshing fails, keep the old data and display "Expired/Last Update".

Cache old data for temporary reading failure (network, 408, 429, 5xx) only and must indicate the time of the last successful reading and stop using the state-dependent input; 401/403/404 must hide sensitive content that has been cached and display the corresponding status. The UI state at least covers loading, empty, loaded, stale, forbidden, not-found, error, submitting, waiting-verification. Double-click form protection is only UX, and the real idempotent is the server. Only keep the request key with the same unfinished intent; It is a new intention to modify the payload or re-confirm it.

`next_actions` relay actions, permissions and blocked reason codes for the UI to display; It is still in operation and re-checked. The page must not hide UNKNOWN payments because entitlement active.

## 3. Session and Authorities

The user has specified a single management account password using `.env` to replace a one-time bootstrap. Added `/admin/login`, containing account numbers, passwords, login buttons, dispatch and verification failure status. The account uses autocomplete=username and the password is type=password/autocomplete=current-password; It supports Enter sending out, logging in successfully redirecting the original page of the same site, and banning external redirect URLs.

The setting key is `BILLFORGE_ADMIN_USERNAME`, `BILLFORGE_ADMIN_PASSWORD`, and the option `BILLFORGE_ADMIN_CAPABILITIES` is for tightening and must contain `read`. In addition to the `lab admin --env-file .env commerce.db provider.db [127.0.0.1:8080]`, the `lab admin --env-file .env commerce.db provider.db [127.0.0.1:8080]` was added. The `.env`, which is missing from the current workbook, has clearly set the process environment with the same name as the priority key. The map analyzer using `godotenv` retains the quoted value/special characters and does not log the entire file individually. The number 1 is 64 characters, not containing control characters; The code is 12x72 UTF-8 bytes (bcrypt has explicit input restrictions), suggesting at least 16 characters. Lack, blank, or format error failure to boot, without providing a missing log seal.

When Go is started, `golang.org/x/crypto/bcrypt` cost 12 generates a salted hash in memory, and authenticates the hash when logged in; Do not write plaintext into SQLite or any response. `.env` itself still retains the user-defined password, so in practice, `.env`/`.env.*` is ignored, and only `.env.example` that does not contain a valid certificate is allowed to be submitted, and the operating document specifies the permissions of the native file. The password cannot use the `VITE_` prefix, cannot access the build-time env, local storage, URL or log at the front end. It is not possible to claim that the Go process memory has safely deleted plaintext.

session suspended for 30 minutes, up to 8 hours, cancellation cancellation; Reset server after session failed. Changes to the `.env` account password must be restarted and the existing session can be cancelled. Forget the password after the `.env` has been updated by the homeowner; No registration, password, email or account management features are added. This is a planning-only round, not creating `.env` or any actual accounting seals.

cookie：HttpOnly、SameSite=Strict、Path=/admin； Secure on official HTTPS. Accept only Host that is set up to prevent an unexpected Host from pointing to the host service. All entries contain session-bound CSRF tokens and verify Origin, JSON content type, missing request body up to 1 MiB; No CORS wildcard. Login also verifies Origin/Host. Before logging in, GET `/session/csrf` obtained nonce tied to short-term pre-auth cookie, POST `/session` successfully verified the session ID and CSRF token after rotation to prevent unlogged sessions along the way. An unknown account is also a dummy bcrypt verifier, which uniquely displays "account or password errors". Each source/account combination fails a maximum of 5 times in 15 minutes, and each process has a maximum of 20 login verifications per minute, running only one bcrypt verification at a time; Extreme back 429 and retry-after, not permanently locked accounts. These state-of-the-art flow limits are reset after rebooting. General business readings also require session.

The following are the permissions: `read`, `subscription.manage`, `finance.adjust`, `catalog.publish`, `migration.manage`, `reconciliation.repair`, `operations.run`, `usage.manage`, `contract.manage`, `lab.control`. Currently, a single operator has full capacity; The test fixture can be configured with different actors and capabilities without developing an account management UI.

The live actor uses a stable `local-admin` ID to separate it from a random session ID; Re-login and restart without changing the idempotent scope of the command. worker uses the server-side capability registry, and cannot rely on expired cookie storage permissions.

the ability to check before each command admission and worker runs; The management command only allows the original actor or the corresponding operator to read, and `read` can see sensitive shared business results. withdrawal of permission to stop the non-operating command; The submitted financial facts and recovery verification will continue to be completed by the system and cannot be lost due to the expiration of the session.

## 4. Shared API agreements

session routing is called GET `/session/csrf` ((nonce before logging in)  POST `/session`(username/password) GET `/session` (current identity and permissions)  DELETE `/session` (cancelled, CSRF required)  Only login pages, static assets, nonce and session creation, other GETs, POSTs are all protected by session management before logging in. Quantities and values use ten-digit integers, which prohibit sub-numbers, exponents, NaN, negative values beyond the field policy and beyond the int64 range. The interest rate is `rate_num`/`rate_den` string, the denominator is positive. revision and the period_index for writing the command are also integers with ten digits; The number of pages/limit is a bounded JSON integer. The time is RFC 3339 UTC, which allows for 0 to 9 bits of seconds and must fall within the range of int64 Unix nanoseconds to be represented; In addition to the above, it is important to note that this is not the case with the super-precision or the super-range. The currency uses the MVP's USD, and cannot be introduced to new currency expansion policies from the front end.

In the envelope: `{data, as_of, source_revision?, next_cursor?}`. the fixed sequence `(created_at,id)` or the resource-determined stability key; The cursor binds the filters to the sequence and does not provide any SQL sort. Multi-page data is not the same full-domain snapshot; The impact range of the batch cannot come from page-by-page browsing and must be fixed by preview/job snapshot.

The wrong envelope:`{error:{code,message,field_errors?,retryable,current_revision?},request_id,command_id?}`400 format; 401 session； 403 Authorization/CSRF; 404 object or unknown API path; 405 API methods do not support back and forth.`Allow`409 State, revision, key or preview conflicts; 422 Business fields are invalid; 429 There is a boundary flow; 500/503 system or temporarily unavailable. No SQL, stack or credentials.

`request_id` is generated by a server-by-server HTTP request and simultaneously placed in the `X-Request-ID` response header with the error JSON; It is not possible to accept the user's own ID. The command saves the receiver request ID, and the subsequent audit event saves the request ID running at this stage; Background recovery using the original command receiver ID. The old data, which remained `NULL` after the v8 upgrade, is irreplaceable as a fictional historical source.

When the command is received, if the saved results are not confirmed, return 500 `COMMAND_ADMISSION_UNKNOWN`、 `retryable: true`, without providing the unconfirmed `command_id`. The client must retain the original payload and idempotency keys, verify or resend with the same key after logging in again; It's not possible to create a new key for 500. When the command is accepted but temporarily fails to run, return 503 `COMMAND_PENDING_RETRY` and attach the original `command_id`, with subsequent verification and retry still following the original command.

All command POSTs require `Idempotency-Key`, while financial and bulk operations require `preview_id`; `reason` is 500 words long. Actor, request ID, actual running time from the server. Refusing unknown fields. The canonical payload hash is calculated by the typed DTO that has been analyzed, with the default number/time/default, including the kind、target and preview ID.

All commands are first received back to `202 {command_id,status,location}`; The same key, the same actor, the same canonical payload, back to the same command, and back to 200 when completed, even if the original preview has expired or the revision has changed. It's the same as the `409 IDEMPOTENCY_CONFLICT`. It is not possible to do a preview calculation based on the current state, and then look at the idempotent record.

Get the command back.`status`、`result_refs`、`domain_outcome`、`error`、`created_at/updated_at`Capture was explicitly denied by the provider that the command could have been completed, but`domain_outcome=declined`The UI still shows a failure to pay.`failed`Indicates that the management command itself is not completed; It's not the first time we've had an external effect.`waiting_verification`。

### 4.1 Reading resources

GET endpoints: `/overview`; `/quotes`, `/quotes/{id}`; `/customers`, `/customers/{id}`; `/subscriptions`, `/subscriptions/{id}` and their `/periods`, `/timeline`, `/entitlement`; `/invoices`, `/invoices/{id}`; `/credits`, `/credits/{id}`; `/payments`, `/payments/{id}`; `/refunds`, `/refunds/{id}`; `/prices`, `/prices/{id}`; `/meters`; `/catalog-selections`; `/price-migrations`, `/price-migrations/{id}` and `/items`; `/usage-events`; `/usage-periods/{subscriptionId}/{periodIndex}`; `/contracts`, `/contracts/{id}`; `/reconciliation-runs`, `/reconciliation-runs/{id}`; `/discrepancies`, `/discrepancies/{id}`; `/account-migrations`, `/account-migrations/{legacyId}` and `/provenance`, `/shadows`, `/readiness`, `/entitlements/{subscriptionId}`; `/commands`, `/commands/{id}`; `/jobs`, `/jobs/{id}`; `/outbox`; `/lab/status`, `/lab/provider-captures`, `/lab/provider-refunds` (paginated; `lab.control` required).

`GET /price-migrations/{id}`In the case of the recycling of batch fields and`ItemCount`、`PendingCount`、`AppliedCount`、`ConflictedCount`、`SkippedCount`It's not built into an array of endless projects.`GET /price-migrations/{id}/items`Acceptance`limit`(Lack of 20, range 1 to 100)`status`（`pending`、`applied`、`conflicted`、`skipped`(A) and`cursor`Back to you.`items`、`next_cursor`、`observed_at`The optical character binds the batch ID and the status filter; The first page of the page is searched again after the status changes. The quantity and the counting are all produced in a precise 10-digit string.

The list only allows white-list filters such as customer_id, subscription_id, status, time range, ID prefix to be used for the resource. Ready/readiness is simply reading observations with time; The switch command is still validated in the transaction. Required fields:

| DTO |What is needed?|
| --- | --- |
| quote |Customer, use, price/contract version, components, fingerprint, expiration, required capabilities, due_now/estimated, recurring commitment, change binding|
| subscription |Customer, actual price/checksum/seats, revision, period, scheduled intention, billing/payment/service status and entitlement source revision|
| invoice |In addition to the original/obligation/allocated/outstanding minor, immutable lines, price/contract/usage references, corrections and credit applications.|
| credit/refund |source invoice/correction、funded amount、available/applied/reserved/refunded、currency、source operation; Unknown reserves are not available.|
| price |the publication state, all components, meter, validity period, checksum, cohort use; Published for reading only|
| migration/job |frozen targets, counts, revision/results/blocked reason, pause scope; It's not a success story.|
| discrepancy | kind、expected、actual、evidence refs、source revision、run、repair/manual decisions、verification |
| account migration |legacy/customer/beneficiary、history、read/writer owner、stopped/reason、readiness|

The term balance struct is used directly in the context of the quantity field. It is not allowed to write the balance of payments on its own with the "original amount - refund". Source missing shows unknown, not corrected 0.

`GET /commands` with 1 ton 100 pens for one page (missing 50), retweeted `items` and `next_cursor`; The cursor uses the rowid command to write the order, and adding the command does not make the page read repeated. `GET /commands/{id}` is available to open the old command, and the list page is not a full-domain snapshot. The cursor is not working and returns to `INVALID_CURSOR`.

### 4.2 Preview

POST `/previews` accepts `{action_id,target_id?,payload,expected_revision?}`. The action ID must be listed in the table below and is validated with a typed switch, without reflective function calls. It returns `preview_id,expires_at,source_versions,impact,blocking_reasons`. A preview lasts five minutes and never beyond the referenced quote’s expiry. GET `/previews/{id}` is available only to the original actor or a user with the corresponding capability.

preview not retaining payment/return amounts, not calling providers, not running domains; The only entry is a preview record. The content preserves the canonical intent, source version, amount, and individual objectives, confirming the total number of commands from server records and untrusted browsers retransmitted.

When the amount-sensitive command is running, it is recalculated in the same transaction: refunds, credits, corrections must be in line with the exact amount confirmed; Medium-term upgrades are allowed to be reduced after the original policy is recalculated and not exceed the confirmed ceiling; Add a change in the item, price, seat, billing period or scope of `409 PREVIEW_STALE`. The price release confirms the full canonical spec/checksum. Scheduling confirms the actual catalog selection when it comes into effect in the future; The price of the quoted shares is not consistent with the quoted prices.

C02 preview Refer to `409 QUOTE_EXPIRED` for expired offers, without creating a command or payment obligation. Accepting the page showing the original offer and the reason for the expiry and providing new offer inputs to the original customer; The new offer must be recalculated and not renewed.

C03/C04 preview If the offer ID, binding Fingerprint, target subscription or change mode does not match, return to `409 CHANGE_QUOTE_BINDING_MISMATCH` without creating a preview or command. The screen should indicate the wrong tie-in to allow the operator to re-enter the offer from the correct details.

C01 When creating a change bid, if the bound subscription revision has changed, the command failed with `CHANGE_QUOTE_REVISION_CHANGED`; The bid and the bond are rolled back on the same transaction without a successful receipt. C03/C04 preview If the binding or subscription revision has changed, go to `409 CHANGE_QUOTE_REVISION_CHANGED`; If the price version is no longer a current catalog selection, please return to `409 CHANGE_QUOTE_PRICE_SUPERSEDED`. Neither created a preview or command, and the interface indicated that it would be necessary to re-offer according to the latest subscription status or price.

Fixed target IDs+source versions+ expected amount; Job cannot be run until later than the eligible subject. Clear conflict-by-conflict can be kept for a new preview, and cannot be stealthily expanded. C13/C30/C32/C44/C45 A maximum of 100 candidates per batch is fixed; Preview read the 101st candidate to determine if there are any remaining projects, and write only the first 100 members as permanent members. All five jobs are in the final round of the previous fixed list of jobs created, returning to the beginning when necessary, to avoid conflict or to prevent other candidates from repeatedly obstructing verification items. Only re-open the preview without moving the lights. C32's capture outbox will be removed from the candidate collection. The `has_more_candidates` of the preview impact is the string `true`/`false`, indicating that the preview currently has candidates outside this batch; The UI must remind the operator to create new batches.

### 4.3 List of commands

`route` below records the originally planned resource meaning path. The written interface unified by `POST /admin/api/commands`, and the movements in the `action_id`、 `target_id` and typed payload selection tables; Call `POST /admin/api/previews` if you need a preview. Single command input shared session, CSRF, capabilities, idempotent and receipt processing, actual UI path and evidence see [The Action Plan](../implementation/web-admin-action-audit.md). R = Preview required; N = No preview confirmation, but still need to be authorized, idempotent and field checked. Inputs in the table do not duplicate common fields such as reason, preview_id, expected_revision; `target_id` must be a resource object in the corresponding table.

| ID | route |Input/Department Services|The authorization.|Preview|
| --- | --- | --- | --- | --- |
| C01 | `/quotes` | customer_id； This is a list of the most commonly used names for the name of the company. You can select change_subscription_id+mode+revision. `CreateQuoteForCohort/CreateContractQuote/BindChangeQuote` | subscription.manage； Contract.manage is also required.| N |
| C02 | `/quotes/{id}/accept` | fingerprint； `AcceptQuote/AcceptContractQuote`, refusing to change the quote| subscription.manage； Contract.manage is also required.| R |
| C03 | `/subscriptions/{id}/schedule-plan` | quote_id、binding fingerprint、revision； From quote, take plan/seats. `ScheduleNextPlanAtPrice` | subscription.manage | R |
| C04 | `/subscriptions/{id}/upgrade` | quote_id、binding fingerprint、revision； `RequestImmediateProUpgradeAtPrice` | subscription.manage | R |
| C05 | `/subscriptions/{id}/cancel` | revision； `ScheduleCancel` | subscription.manage | R |
| C06 | `/subscriptions/{id}/resume` | revision； `ResumeCancel` | subscription.manage | R |
| C07 | `/invoices/{id}/payments` | amount_minor； `CreatePayment` | finance.adjust | R |
| C08 | `/payments/{id}/retry` |The original operation ID analyzes the invoice_id in the transaction and checks for failure; `RetryFailedPayment` actually receives invoice_id| finance.adjust | R |
| C09 | `/payments/{id}/dispatch` |Fixed operation ID; `DispatchCapture` | finance.adjust | R |
| C10 | `/payments/{id}/reconcile` |the original operation; `ReconcilePayment` | finance.adjust | N |
| C11 | `/invoices/{id}/reductions` | reduction_minor、reason； `PostReduction` | finance.adjust | R |
| C12 | `/credits/{id}/applications` | invoice_id、amount_minor； `ApplyCredit` | finance.adjust | R |
| C13 | `/jobs/change-corrections` |Preview Fixed change IDs; The `RunChangeCorrections` is being demolished.| finance.adjust | R |
| C14 | `/changes/{id}/resolve-unfulfilled` | change ID； `ResolveUnfulfilledImmediateChange` | finance.adjust | R |
| C15 | `/credits/{id}/refunds` | amount_minor； `ReserveRefund` | finance.adjust | R |
| C16 | `/refunds/{id}/dispatch` |Fixed refund ID; `DispatchRefundNext` extracted the helper to run by ID.| finance.adjust | R |
| C17 | `/refunds/{id}/reconcile` |the original repayment; `ReconcileRefund` | finance.adjust | N |
| C18 | `/prices/pro` | ProPriceSpec； `PublishProPrice` | catalog.publish | R |
| C19 | `/meters` | id、source、unit、schema_version； `RegisterMeter` | catalog.publish | R |
| C20 | `/prices/metered` | MeteredPriceSpec； `PublishMeteredPrice` | catalog.publish | R |
| C21 | `/catalog-selections` | plan_id、cohort、effective_at、price_version_id； `SelectCatalogPrice` | catalog.publish | R |
| C22 | `/price-migrations` | id, cohort, target_price_version_id, fixed subscription_ids; `PreviewPriceMigration/PlanPriceMigration` | migration.manage | R |
| C23 | `/price-migrations/{id}/pause` | batch ID； `PausePriceMigration` | migration.manage | N |
| C24 | `/price-migrations/{id}/items/{subId}/skip` |clear objects and reasons; `SkipPriceMigrationItem` | migration.manage | R |
| C25 | `/price-migrations/{id}/resume` |the batch after re-examination; `ResumePriceMigration` | migration.manage | R |
| C26 | `/usage-events` | source、event_id、subscription_id、meter_id、occurred_at、quantity； `RecordUsage` | usage.manage | N |
| C27 | `/usage-adjustments` | source/event_id/subscription_id、original_source/original_event_id、reverse_quantity； `RecordUsageAdjustment` | usage.manage | R |
| C28 | `/usage-periods/{subId}/{index}/close` | cutoff； `CloseUsagePeriod` | usage.manage | R |
| C29 | `/usage-periods/{subId}/{index}/rerate` |billing period; `RerateUsagePeriod` | usage.manage | R |
| C30 | `/jobs/usage-credit-notes` | Preview a fixed set of adjustment items; decompose `RunUsageCreditNotes` into per-item commands. | `finance.adjust` | R |
| C31 | `/contracts` | ContractSpec； `PublishContract` | contract.manage | R |
| C32 | `/jobs/contract-collections` | Preview a fixed set of due contract invoices; decompose `CollectDueContractInvoices` into per-item commands. | `finance.adjust` | R |
| C33 | `/reconciliation-runs` | as_of； `RunReconciliation` | reconciliation.repair | N |
| C34 | `/discrepancies/{id}/repair` |Discrepancy ID, proof of source; `RepairDiscrepancy` | reconciliation.repair | R |
| C35 | `/discrepancies/{id}/manual-decisions` | decision、reason； Reviewer from the session `RecordManualDecision` | reconciliation.repair | R |
| C36 | `/account-migrations` | legacy_account_id、customer_id、beneficiary_id、cohort、has_history； `LinkLegacyAccount` | migration.manage | R |
| C37 | `/account-migrations/{id}/shadow-quotes` | plan_id、seats、legacy_amount_minor、legacy_currency； `ShadowQuote` | migration.manage | N |
| C38 | `/account-migrations/{id}/shadow-entitlements` | subscription_id、legacy_status； `ShadowEntitlement` | migration.manage | N |
| C39 | `/account-migrations/{id}/provenance` | LegacyProvenance mapping； `BackfillLegacyProvenance` | migration.manage | R |
| C40 | `/account-migrations/{id}/provenance/{legacyInvoiceId}/resolve` | corrected mapping、decision； Reviewer from the session `ResolveLegacyProvenance` | migration.manage | R |
| C41 | `/account-migrations/{id}/switch-read` |MigrationThresholds+ is the source version; `SwitchAccountRead` | migration.manage | R |
| C42 | `/account-migrations/{id}/switch-writer` |MigrationThresholds+ is the source version; `SwitchAccountWriter` | migration.manage | R |
| C43 | `/account-migrations/{id}/stop` | reason； `StopAccountMigration` | migration.manage | R |
| C44 | `/jobs/renewals` | Preview a fixed set of due subscriptions and billing periods; decompose `RunRenewals` into per-item commands. | `operations.run` | R |
| C45 | `/jobs/entitlement-refresh` |Fixed subscriptions; The `RefreshEntitlements` is being demolished along with the fact that it was used as a source.| operations.run | R |
| C46 | `/lab/clock` |UTC instant or mode=real; clock revision | lab.control | R |
| C47 | `/lab/payment-decisions` | operation_id、status=`succeeded`or`definitively_failed`；`SetFakePaymentDecision` | lab.control | R |
| C48 | `/lab/refund-decisions` | refund_id、status=`succeeded`or`definitively_failed`；`SetFakeRefundDecision` | lab.control | R |
| C49 | `/lab/faults` |The operation type/ID、mode=`lost_response` or `crash_after_provider`, one-time fault ticket| lab.control | R |

The UI "send next" lists and confirms the specific operation, submitting C09/C16; It is not possible to change the queue to another one. The C49 ticket can only be used by operators with `lab.control` when the operation is dispatched; Normal financial pages do not accept any fault strings.

All previews must remain valid before they are first run; Re-broadcast results submitted prior to receipt, not expiry. After the validity of the fixed membership has been confirmed and sustained, the project must still be subject to source and amount checks without extending or withdrawing the scope mid-term due to running time exceeding the preview period. The uninitiated expired command returns to PREVIEW_STALE and cannot be silently extended.

After C43 has been stopped, additional business entries controlled by the account must be rejected and cannot be changed to stop misreporting as a source revision. C02、C05、C06 New preview back to `409 ACCOUNT_MIGRATION_STOPPED`; C03, C04 also checks for write permissions when creating a preview. If the preview is stopped, the original command can be checked back, but the result is `failed/ACCOUNT_MIGRATION_STOPPED`, no subscription can be created, no scheduling can be cancelled or successful receipt can be received, and no cancellation can be restored. The same request key is played back to the original command.

C01's creation and change binding must be completed in one transaction; Contract/purchase/change are used interchangeably. Front-end capabilities are presenting capabilities that cannot replace authorization. The meter/Net30 components that are actually supported by the management end can accept the corresponding quote.

### 4.4 Payload schema

ProPriceSpec：`id,version,fixed_minor,seat_minor,included_tasks,usage_rate_num,usage_rate_den,effective_from`。 MeteredPriceSpec：`id,plan_id,version,fixed_minor,seat_minor,meter_id,included_quantity,usage_rate_num,usage_rate_den,effective_from`。 ContractSpec：`id,customer_id,version,base_price_version_id,fixed_minor,seat_minor,effective_from,effective_to,post_contract_price_version_id?`。 Field policies are shared by field validators; It's not possible to release the HTTP layer.

With a price and contract for each seat, `fixed_minor + seat_minor` must be specified by `int64` to ensure that at least one seat is available. The measured price without per seat fee shall be valid only if the fixed fee is itself valid; More seats are still on offer and are being checked over and over. Management forms and previews reject non-qualified combinations before creating commands, and the direct field release paths also run the same limitation.

LegacyProvenance：`legacy_invoice_id,legacy_subscription_id,legacy_account_id,commerce_subscription_id,commerce_invoice_id,price_version_id`； Status/evidence is determined by the domain and cannot be retrieved from the browser. MigrationThresholds：`max_quote_p95_millis,max_unknown_payments,max_open_discrepancies`； Following the current unknown must-zero switching policy, even the UI does not allow viewing thresholds to be loosened.

Publish/map without a single subscription revision, preview binding the full source fingerprint to the corresponding version; Running transaction re-checking, not creating a revision that applies to all objects.

## 5. Trading, command recovery and working model

```text
typed request → auth/CSRF → look up original request key → parse / validate intent
    → admission transaction：command + preview claim + audit(accepted)
    → single local worker：claim lease + pre-execution permission check
        → domain transaction：revalidate source + business facts + receipt + audit
        → provider/outbox：same original operation key, outside the transaction
        → record observation → update command result → UI polling
```

command：`accepted → running → succeeded | failed | waiting_verification`； Wait can only verify the results with the default key. The command note `failed/PERMISSION_REVOKED`, which has not yet been activated, is tightened at the time of the resumption of the capability policy; Provider obligations are still verified. When it is not possible to safely determine whether a cross-database operation has taken effect and has not yet received a receipt, retain `accepted/PERMISSION_REVOKED_REVIEW`, do not restart the operation, wait for the receipt or continue with the original command after authorization is restored. Every claim has lease generation, and old workers cannot cover new results with expired generation. The first version of the same process, single worker, lease, is still used for restarting recovery; The timing of the lease can't be adjusted, the time of the lease can't be adjusted, the time of the lease/session can't be adjusted using a wall clock, the business billing period can't be adjusted using a business clock.

After the ability is revoked, the `read` session can verify the requirements of the existing `waiting_verification` and `PERMISSION_REVOKED_REVIEW` commands; The operating end must first receive a permanent operation/repair/provider control receipt to prove that this path will not send new operations. Verification can add native observations and receipts, but still requires session, origin and CSRF.

For purely local amounts, the existing public method will be extracted into `...Tx` helper, the public wrapper retains the current interface. The new executive management executor has the most external transaction, business facts, command receipt, audit commit; It is not possible to call the BeginTx method again in an external transaction with a single connection to DB. Select the entire admin command transaction guarantee, prohibiting arbitrary weakening of "post-conclusion records" per endpoint.

Manage calls using the stable `admin:<command_id>` as the domain request key; The provider key is still generated by the obligation of the original domain; the method of the public request key already exists along key correspondence. No key release, switch, shadow, artificial resolution, etc. is required to avoid re-entry via command receipt plus domain business key. Returns the receipt first; Local commands without receipt or commit can be re-entered. The provider side effect cannot be inferred without a receipt and is verified by the original operation.

After the payment/refund dispatch command selects the operation ID, the transaction marks the legitimate dispatch status, calls the fake provider and observes the landing. Response lost or process crash → command waiting, keeping the original reservation with key. The verification of no terminal evidence continues to wait and no refund amount can be released or capture added.

C47/C48 writes to another SQLite provider and cannot claim transaction with the commerce command. Add `provider_control_receipts(command_id UNIQUE,payload_hash,target_key,result)` to the fake provider control and submit the same provider transaction update with that decision; Restore the receipt before checking it, and do not fail the setup error report because the capture has already occurred. This is only used to control fake providers and not to change the existing capture/refund financial facts.

reconciliation with shadow is an observational command: collect source observations and time/version, then refer evidence and results to the command receipt in a commerce transaction. It is not possible to obtain a single atomic snapshot across two SQLites, and it is necessary to record the time observed by each source; Repair is still re-verified source, and observation run success does not represent consistent funding.

job fixed membership and receipt per item; The renewal/receipt/correction class does not retry the entire database once. Job summary is divided into succeeded/failed/conflicted/waiting/skipped; The server stops claiming new projects when the server is normally closed, and the effect is restored by the original operation; The UI suspended the C23's price migration, with the original version not adding a general job pause command. Management UI does not provide any general-purpose over-run job; Financial conflicts require a new preview, UNKNOWN obligation to check originally.

`business_time` is fixed at the start of a command and provides a preview of the valuation moment; Clock revision changes will disable unconfirmed previews. Job items keep the business hours of the job. C46 coordinates with the running gate of the process, not changing the clock during a command. Domain amount/revision protection is still in DB transaction and cannot rely on front-end or single worker to replace parallel checks.

## 6. Planning to add tables and migrations

| table |Nuclear fields and constraints|Save / Restore|
| --- | --- | --- |
| admin_schema_migrations | version PK、checksum、applied_at |In order, the checksum is not rejected.|
| admin_commands | id PK、actor_id、idempotency_key、kind、target、payload_json/hash、preview_id、status、business_time、clock_revision、lease_owner/generation/until、result_refs、error_code、created/updated | UNIQUE(actor_id,idempotency_key)； status CHECK； Financial results are not automatically eliminated.|
| admin_previews | id PK、actor_id、kind、intent_hash/json、source_versions、impact_json、expires_at、claimed_command_id | immutable； A preview can only bind to the same command; Expiry checks are not new.|
| admin_command_receipts | command_id PK/FK、domain_request_key、result_refs、committed_at |Commit with local business transactions; It's not overwritten|
| admin_jobs / admin_job_items | job ID、command ID、frozen as_of； item target、source fingerprint、status、receipt、failure | UNIQUE(job_id,target_type,target_id,period_key)； Freeze membership|
| admin_audit_events | id、command_id、actor、action、target、reason、before/after refs、request_id、wall time | append-only； Not keeping secrets; Save source citations instead of copying the entire Gold Stream payload.|
| admin_lab_settings / admin_fault_tickets | clock mode/value/revision； fault target/kind/mode/claimed command |Only available in the lab profile; One-time ticket verification and target verification|
| provider_control_receipts（provider DB） | command_id PK、payload_hash、target_key、result、committed_at |The same transaction is updated with fake decision; For C47/C48 to check the original results.|

The session/login limits the memory flow in the native process, and the restart is invalid; Command/job/audit was not cancelled. Source financial table with immutable history not destructive migration. schema upgrade is completed before HTTP is accepted, with a single transaction that adds tables/indexes, and any failure to keep the old DB can be opened by the old binary; Commerce/provider matching files are backed up in advance. For the first time running with the new temporary DB; Upgrade testing using existing schema fixture.

If the field is added to an existing table, it must be compatible with the old CLI/v1 type (with default/nullable or common wrapper); It is forbidden to falsify old facts as having an admin actor history. Initially read-only access can be disabled using the admin server and returned to the existing CLI; In the case of new financial facts, the old DB was not restored as a rollback.

## 7. Design substitution that has been solved before implementation

|The question is:|Decisions and Costs|
| --- | --- |
|There is no full session/RBAC in the current version.|The admin server does not install it to avoid circumventing administrative permissions; DTOs need to be maintained independently, but shared domain|
|After wrapper, business commits and commands may be separated.|Transaction helpers, receipts and commit; In the meantime, we're going to have to make more changes in exchange for verifiable crash recovery.|
|The batch, "all working", will include new objects after the preview.|Fixed membership + source guard per item; Multi-level job tables and recovery logic.|
|This is a fake clock pollution session/lease.|The wall clock is separated from the business clock and the time is fixed within the command. It's a clock test that can be injected.|
|The next dispatch could change the target.|Preview Select ID, execute-by-ID; Added a refund dispatcher helper|
|SQLite single connection and UI frequent readings|There are split pages, short transactions, single worker, and unseen page routines. Pre-testing and optimization, prohibiting copying all states to the Web|

Not included: real PSP, taxes, multi-currency, accounts, open network deployment, full user lifecycle, MRR/ARR, new subscription policies. The reason for this is that there is an over-expression of the goal of having a native MVP and a Web operating layer. The above extension will not remove any existing CLI capabilities of C01 and C49.

Scale limit: a maximum of 1,000 goals per preview/job, going beyond clear mistakes and requiring batch, silent interruption. It's a natural operating line; All the objects are still available for operation in batches. Login and command stream limits do not consume financial amounts and do not trigger new payments.

The schema upgrade of the two SQLite files is done automatically in the database transaction, without claiming to be cross-database atomic. The migration remains additive/replaceable, and the administrator must complete both versions of the library until the start; If the second repository fails to accept HTTP, the next boot will continue from the completed version. The old CLI compatibility with the matching file backup was verified by A05.
