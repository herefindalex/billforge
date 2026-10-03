# Web Admin Implementation Plan

**English** | [繁體中文](07-web-admin-implementation-plan.zh-TW.md) | [简体中文](07-web-admin-implementation-plan.zh-CN.md)


Status: T01-T22 and local A01-A30 acceptance are complete as of 2026-10-03. Checkboxes reflect verified implementation and acceptance; evidence and limitations are recorded in [the implementation record](../implementation/web-admin.md).

The following documents are available: [Functional design](05-web-admin.md), [Project contract](06-web-admin-contracts.md), [Acceptance Scheme](08-web-admin-test-plan.md). [JSONL](web-admin-tasks.jsonl) maintains the original task estimates and dependencies, and the current acceptance status is assigned to [Record performance](../implementation/web-admin.md).

A01-A30 passed within the documented local scope (30/30); T01-T22 are complete. The final independent review found no blocking or important findings. The 49-action record links each UI route, handler, domain helper, source guard, capability, preview policy, receipt/recovery policy, HTTP tests, browser case and execution evidence.

## 1. Conclusions of engineering review

The previous version had full functionality, but not enough to run directly. The four-dimensional design review of architecture, code quality, testing, performance was completed. There is no operation, no dependency on front end installation, no launch of new services or deployment.

|I found it.|Current evidence/gap|Selected Programs|
| --- | --- | --- |
|F01 Manage reading gaps|`api/v1.go` routing is limited; `lab/inspect.go` State returns the full quantity at once.|Add boundaries, stable sequences, traceable source management DTOs, and do not convert the entire JSON to State.|
|F02 Management Authorisation bypassed|The existing `serve` offers native v1 tokens, only internally operated.|The new `admin` server has dedicated session routing; Unattended unmanaged v1|
|F03 is written with command non-atomic.|The field method itself BeginTx, `Lab` restricts single connections; The outer wrapper will be locked or separated.|Draws Tx helpers, command receipt and local business commit and transaction|
|F04 preview Not committed|Quote Seats/revision Protected but without a general management preview or confirmation amount limit|Perpetuate preview, run re-checks, accurate amounts/limits policies, and first process idempotent rebroadcasts|
|F05 has not yet resumed management work.|Outbox and the provider key already exist but do not manage actor, command/job receipts|Add command, lease generation, job membership, results and restart recovery|
|F06 next item may drift.|`DispatchRefundNext`, part Run* method to find the next one or scan it all.|By-ID dispatcher/ individual helper; Preview Fixed membership to avoid running new entries|
|The F07 clock and the failure are CLI local capabilities.|menu clock is different from server wall clock; There are two types of failure patterns.|Fixing business time, session/lease and keeping a wall clock for each command; fault ticket separated by operation|
|F08 front-end liability unfixed|There are no React projects, shared state components, or DTOs yet.|Determine the workload of the front-end suite, decimal strings, status and stale/error behavior.|
|F09 UI capabilities are not accepted on a case-by-case basis|The only test available is CLI/domain/API.|For C01 and C49, create an HTTP behavior matrix with real-world browser settings.|
|F10 build/upgrade not defined|There are pure Go projects, there are SQLite files.|Adminui build tag, save the distless Go build, add schema + old DB fixture verification|

The `AcceptQuote`, the change command, balance, provider key, reconciliation and migration guard are all reused. Only transaction helpers, ID work, preview, admin persistence, search for DTOs and Web layers are responsible; I don't want to rebuild the engine.

## 2. Tasks and dependency

Each task includes procedures, related tests and necessary contract updates. P1 = blocking delivery at this stage. This round does not reduce incomplete capacity to TODO after the day. Human resources can be overlapped with the measurements of engineers familiar with Go/React, including testing and review; AI running time does not measure data, excluding fictitious acceleration multiples.

```text
T01 → T04 → T05 → T06 → T07
T02 → T09        T03 ↗        ↘ T10 → T11
T11＋T13 → T12
T04 → T08 ────────────────────↗        ↘ T13 → T18
T07＋T08＋T09 → T14 → T15
                 ├→ T16
                 └→ T17 (also depends on T10 and T13)
T13＋T18 → T19；T06＋T07＋T09 → T20
All features T10–T20 → T21 → T22
```

The following routes are all expected ownership; The same route was reconstructed in sequence, avoiding multiple workers modifying the transaction skeleton simultaneously. No subagent, worktree or branch has been activated.

- [x] **T01 (P1, 0.5–1 day): Freeze contracts and the regression baseline.** Sources F09/F10. Files: `docs/design/06–08`, `lab/*_test.go`. Inventory S01–S12/P01–P03 fixtures, add an old-schema fixture, and map C01–C49. Dependencies: none. Acceptance: A01; preserve existing test results and record domain defects rather than hiding them behind the new UI.
- [x] **T02 (P1, 0.5–1 day): Create the React and embedded-build skeleton.** Sources F08/F10. Files: `web/admin/`, `cmd/lab/adminassets/`, `cmd/lab/admin.go`. Add the route shell, pnpm lockfile, TypeScript, Vite, Ant Design ConfigProvider/App, theme tokens, and build tags. Dependency: T01. Acceptance: A02; test builds with and without `dist`, deep-link refresh, asset paths, and error pages.
- [x] **T03 (P1, 1–2 days): Implement sessions, CSRF, permissions, and dedicated routes.** Source F02. Files: `api/admin/session.go`, `permissions.go`, `router.go`, `cmd/lab/admin.go`, `.env.example`, `.gitignore`. Dependency: T01. Acceptance: A03/A04; cover `.env` values and precedence, login rate limiting, session expiry, Host/Origin checks, unauthorized access, restart, and isolation from v1.
- [x] **T04 (P1, 1–2 days): Add a versioned commerce/provider admin schema and migrations.** Sources F05/F10. Files: `lab/admin_schema.go`, `lab/admin_migration_test.go`. Dependency: T01. Acceptance: A05; cover new and old databases, reruns, rollback on failure, unchanged financial history, and CLI compatibility.
- [x] **T05 (P1, 2–4 days): Extract transaction helpers while retaining public wrappers.** Sources F03/F06. Files: `lab/lab.go`, `billing.go`, `corrections.go`, `refunds.go`, `subscription_changes.go`, `immediate.go`, `usage.go`, and other writing modules. Dependencies: T04. Acceptance: A01/A06; avoid nested transactions and commit business facts with their receipts.
- [x] **T06 (P1, 2–3 days): Implement command admission, workers, receipts, and interrupted-command recovery.** Sources F03/F05. Files: `lab/admin_commands.go`, `admin_worker.go`, `api/admin/commands.go`. Dependencies: T03/T05. Acceptance: A06/A07/A08; cover canonical request hashes, same-key replay, lease generations, permission revocation, and crash points.
- [x] **T07 (P1, 1–2 days): Implement admin previews and precise API value types.** Sources F04/F08. Files: `lab/admin_previews.go`, `api/admin/types.go`, `errors.go`. Dependency: T06. Acceptance: A09/A10; bind each preview to one command and actor, enforce expiry and source revisions, and validate amounts and int64 boundaries.
- [x] **T08 (P1, 1–2 days): Build resource queries and core DTOs.** Source F01. Files: `lab/admin_queries.go`, `api/admin/queries.go`. Dependency: T04. Acceptance: A11; cover paging, allowlisted filters, source versions, and sensitive-data handling across pages.
- [x] **T09 (P1, 1–2 days): Build the shared admin UI.** Source F08. Files: `web/admin/src/app/`, `components/`, `api/`. Dependencies: T02/T03/T08. Acceptance: A12; show stale, error, and empty states consistently.
- [x] **T10 (P1, 1–2 days): Add subscription and plan-change workflows.** Sources F04/F09; C01–C06. Files: `api/admin/subscriptions.go`, `lab/admin_subscription_commands.go`, `web/admin/src/features/subscriptions/`. Dependencies: T07/T09. Acceptance: A13/A14; bind quotes to revisions and recover commands after a lost response.
- [x] **T11 (P1, 1–2 days): Add payment and refund operations.** Sources F05/F06; C07–C10 and C15–C17. Files: `api/admin/payments.go`, `refunds.go`, `lab/refunds.go`, and corresponding UI features. Dependency: T10. Acceptance: A15/A16; verify provider IDs, partial payments, definitive-failure retries, and UNKNOWN outcomes without claiming success prematurely.
- [x] **T12 (P1, 1–2 days): Add invoice corrections, credits, and upgrade compensation.** Sources F04/F09; C11–C14. Files: `api/admin/corrections.go`, `lab/admin_correction_commands.go`, and invoice/credit UI features. Dependencies: T11/T13. Acceptance: A17; preserve source provenance, prevent duplicate corrections, and keep compensation consistent when service starts late or fails to start.
- [x] **T13 (P1, 1–2 days): Add fixed-membership jobs and operations.** Sources F05/F06; C44/C45 and other batch foundations. Files: `lab/admin_jobs.go`, `api/admin/jobs.go`, jobs UI. Dependencies: T06/T07/T08/T09. Acceptance: A18; cover due renewals, entitlement refresh, per-item receipts, partial failure, restart, shutdown claim stopping, and stable membership.
- [x] **T14 (P1, 1–2 days): Add product, meter, price, and catalog selection.** Sources F04/F09; C18–C21. Files: `api/admin/catalog.go`, catalog UI, domain transaction helpers. Dependencies: T07/T09. Acceptance: A19; display all components, keep published prices immutable, avoid duplicate publication on replay, and honor cohort effective times.
- [x] **T15 (P1, 1–2 days): Add the price-migration workbench.** Sources F04/F05; C22–C25. Files: `api/admin/price_migrations.go`, price-migration UI. Dependencies: T13/T14. Acceptance: A20; cover complete previews, fixed per-account IDs, conflicts, skips, pause/resume, and reverse migrations as new batches.
- [x] **T16 (P1, 1–2 days): Add usage events, period close, rerating, and CreditNote.** Sources F06/F09; C26–C30. Files: `api/admin/usage.go`, usage UI, domain usage helpers. Dependencies: T13/T14. Acceptance: A21; cover duplicate and conflicting events, reversals, late deltas, rating history, and positive/negative adjustment paths.
- [x] **T17 (P1, 1–2 days): Add enterprise contracts and Net30.** Sources F04/F09; C31/C32 and the contract branches of C01/C02. Files: `api/admin/contracts.go`, contracts UI. Dependencies: T10/T13/T14. Acceptance: A22; keep contractual obligations, invoicing, and service activation traceable.
- [x] **T18 (P1, 1–2 days): Add reconciliation, repair, and manual resolution.** Sources F04/F05; C33–C35. Files: `api/admin/reconciliation.go`, reconciliation UI. Dependencies: T11/T13. Acceptance: A23; compare expected and actual facts, retain evidence and revisions, use stable repair keys, reconcile again afterward, and distinguish a manual decision from a funds correction.
- [x] **T19 (P1, 1–2 days): Add staged account migration.** Sources F04/F09; C36–C43. Files: `api/admin/account_migrations.go`, account-migration UI. Dependencies: T13/T18. Acceptance: A24; cover mapping, shadow comparison, source facts, readiness, adapter reads and writes, and stop conditions.
- [x] **T20 (P1, 1–2 days): Add isolated clock and fault-injection controls.** Source F07; C46–C49. Files: `lab/admin_clock.go`, `api/admin/lab.go`, lab UI. Dependencies: T06/T07/T09/T11. Acceptance: A25; bind faults to individual commands, keep other operations unaffected, and label the lab environment in the UI.
- [x] **T21 (P1, 2–3 days): Complete browser, concurrency, recovery, and performance acceptance.** Sources F01/F05/F09. Files: `web/admin/e2e/`, `api/admin/*_test.go`, `lab/admin_*_test.go`. Dependencies: T10–T20. Acceptance: A01–A28; use a real Go server and isolated SQLite databases to cover all 49 commands and lost responses.
- [x] **T22 (P1, 0.5–1 day): Deliver build and operations documentation.** Source F10. Files: README, `docs/implementation/web-admin.md`, build scripts, and database upgrade instructions. Dependency: T21. Acceptance: A29/A30; verify fresh and existing databases, embedded builds, login, operation flows, and coverage records.

The task roughly does not include delivery commitments: the main risk is concentrated in the transaction restructuring and recovery of the T05 and T07. When adjusting for real test and module scale estimates after T01, the range of functionality is retained without reducing the C01 and C49 deadlines.

## 3. Scope of delivery in five phases

|Stages| tasks |It's the only thing that can be said to be done.|
| --- | --- | --- |
| W1 | T01–T09 |Manage the basics, read and command frameworks; It is not claimed that the entire operation is complete.|
| W2 | T10 |Self-service subscription workshops; The contract offer branch remains at W4.|
| W3 | T11/T13/T12/T18 |Payments, refunds, credit, repairs, jobs, reconciliation|
| W4 | T14–T17 |Price/meter/cohort/migration, quantity, enterprise contracts|
| W5 | T19–T22 |Accounts migrated, labs, full acceptance, can run in-house delivery.|

Parts that can be prepared independently: T02 and T04 ̊T03; The T15/T16/T17 UI after T14 can be broken down by feature. The premise is that the shared DTO, transaction helper, command dispatcher is stable; The `lab/admin_commands.go` skeleton is a single owner. This is a future division of labor strategy, and this time it has not started in parallel.

## 4. Run with verification command (will be added)

The frontend scripts and admin build target below are implemented. Their local validation and build/upgrade/login evidence are recorded in [the implementation record](../implementation/web-admin.md); the commands remain the reproducible handoff.

```sh
go test ./...
go vet ./...
pnpm --dir web/admin install --frozen-lockfile
pnpm --dir web/admin typecheck
pnpm --dir web/admin test:run
pnpm --dir web/admin build
go build -tags adminui -o /tmp/billforge-admin ./cmd/lab
pnpm --dir web/admin test:e2e
```

The front-end build script will dist sync to `cmd/lab/adminassets/dist`(generator ignored, not submitted), and the embed file will be selected by the build tag; Testing builds failed, and missing API builds had a fallback. The E2E script is responsible for launching built-in Go servers, generating isolated DBs, testing logins, exits and cleansers via `.env`; It is illegal to use a real work database by default.

The schema and command test are preceded by the targeted `-run`, followed by the complete return across the transaction refactor. The race test only targets specific parallel risks of the worker/session/clock shared status, while the transaction consistency of Go SQLite is still verified by definitive DB interleavings.

## 5. Scope, risk and settlement rules

It's not real money, it's not tax, it's multi-currency, it's official accounts, it's publicly deployed, it's CRM, it's full account management, it's ARR dashboard. These are not gaps in the 49 commands, and there is no need to create an additional TODO.

Planning blocks not decided by the user; React has confirmed that the remainder of the technology and preview policies will be adopted in this document. If you want to publicly deploy multiple real operators or take a PSP, and then you need to do a need-threat model, you can't directly declare that the in-house program is satisfied.

**Design and development closure (2026-10-03)**: T01-T22 and A01-A30 passed within the documented local scope. All 49 commands have applicable contract and browser evidence. The original acceptance conditions above remain unchanged; scope limits and the unconfirmed initial process-ready timeout remain recorded.

## 6. This round of planning and inspection records

2026-09-26 Complete document inspection: C01 C49 has 49 commands with a task correspondence; T01 T22 relies on non-circulation; A01 A30 A30 Total of 30 Group Acceptance are all cited for assignment; Relative links can be analyzed. React has entered into a common contract with `.env` for a single management account password, and the old bootstrap proposal has been replaced.

Historical checkpoint, superseded by the final local acceptance above: In the pre-planning hash verification, 60 existing Go/module files remained unchanged. This time, there was no creation of front-end projects, modification of functional procedures, installation of dependencies, creation of actual logs or running of additional receipts. The above checks prove that the product planning is consistent and do not mean that the Web Admin function has been completed.
