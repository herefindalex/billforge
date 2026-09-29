# Billforge Commerce Correctness Lab

English | [Traditional Chinese](README.zh-TW.md) | [Simplified Chinese](README.zh-CN.md)

Billforge is a local lab for testing commerce system correctness. It uses Go and two separate SQLite databases to exercise price versions, subscriptions and contracts, payments and entitlements, usage closing, adjustments, refunds, reconciliation, and account migration. See the [platform plan](docs/commerce-lab-plan.md) and [A–D design](docs/design/README.md) for the design, and the [MVP completion tracker](docs/implementation/mvp-completion-tracker.md) for implementation evidence.

## Current status

The local CLI, API, and React + Ant Design Web Admin are available. The Web Admin A01–A30 acceptance matrix is still in progress; its [acceptance record](docs/implementation/web-admin.md) distinguishes verified cases from remaining checks.

## What this lab checks

- Published prices and contracts retain their versions so later changes do not silently rewrite existing obligations.
- Administrative changes use previews, source version checks, idempotency keys, and command receipts. When a payment or refund dispatch preview becomes stale, the Web Admin shows the previous preview alongside the current operation state.
- The commerce database and fake provider database are separate, allowing retries, lost responses, and reconciliation to be tested across an external boundary.
- Reconciliation repair checks provider amount mismatches against the payment being repaired. A mismatch blocks that payment's recovery without stalling unrelated payments. If the browser loses a successful repair response, the administrator can retrieve the original command by its request key without repeating the repair.

## Requirements and tests

You need Go 1.27, CGO, and a C compiler. The Web Admin also needs Node.js and pnpm. The end-to-end tests need Python 3 and Chrome or Chromium, or a Playwright-installed Chromium browser. Payments use a local fake provider; no real payment service is contacted.

```sh
go test ./...
pnpm --dir web/admin install --frozen-lockfile
pnpm --dir web/admin build
pnpm --dir web/admin test:e2e
go build -tags admin_ui -o /tmp/billforge-admin ./cmd/lab
```

To verify that every C01–C49 administrative action was submitted from the browser and produced a successful command receipt, run the end-to-end suite with an audit file:

```sh
audit_file="$(mktemp /tmp/billforge-action-cases.XXXXXX)"
BILLFORGE_E2E_ACTION_CASE_AUDIT="$audit_file" pnpm --dir web/admin test:e2e
node web/admin/e2e/audit-action-cases.mjs "$audit_file"
```

## CLI and local API

```sh
go run ./cmd/lab
go run ./cmd/lab menu ./billforge-data/commerce.db ./billforge-data/provider.db
go run ./cmd/lab serve ./billforge-data/commerce.db ./billforge-data/provider.db 127.0.0.1:8080
go run ./cmd/lab demo
go run ./cmd/lab renewal-demo
go run ./cmd/lab correction-demo
go run ./cmd/lab demo /tmp/billforge-commerce.db /tmp/billforge-provider.db lost_response
go run ./cmd/lab demo /tmp/billforge-crash-commerce.db /tmp/billforge-crash-provider.db crash_after_provider
```

The interactive menu lets you run operations and inspect current state; see the [CLI guide](docs/implementation/interactive-cli.md). `serve` only accepts a loopback address. Some internal HTTP operations require `BILLFORGE_INTERNAL_TOKEN`; see the [v1 API guide](docs/implementation/phase-p2-v1-api.md).

## Web Admin

Copy `.env.example` to `.env`, then set `BILLFORGE_ADMIN_USERNAME` and `BILLFORGE_ADMIN_PASSWORD` (12–72 bytes). Keep the password in the server environment or `.env`; never use a `VITE_` prefix. Restrict `.env` so only the local user can read it. Build the frontend and start the admin server with two **different** persistent database files:

```sh
pnpm --dir web/admin install --frozen-lockfile
pnpm --dir web/admin build
go run ./cmd/lab admin ./billforge-data/commerce.db ./billforge-data/provider.db 127.0.0.1:8080
```

Open `http://127.0.0.1:8080/admin/`. The admin server binds only to an explicit loopback IP, and the request Host must match the listening address. To use another environment file, place `--env-file path` before the database paths. Process environment variables take precedence over file values.

The contracts list opens a contract version detail page with its published terms, related quotes, and subscriptions. The detail page shows up to 20 related records in each section; its list buttons open paginated quotes and subscriptions filtered to that contract version. From the detail page, you can also start a quote with the customer and contract version already filled in.

The **Batch jobs** page lists recent jobs with item counts and links to their detailed results. Its cursor pagination keeps earlier pages stable when new jobs are created.

**Price migration** detail shows server-calculated totals for each item status. Its item table reads at most 20 records per page, supports status filtering, and displays exact minor-unit amounts. If a refresh fails, the last successful data stays visible with a warning, and actions based on stale batch status are disabled. After applying or skipping items, or resuming a batch, refresh the detail to see current counts and restart pagination.

Invoice, subscription, credit, payment, refund, price, and contract details follow the same read-failure rule: a temporary failure labels cached data and disables actions based on it; an access denial hides cached data.

Command results in shared action forms and subscription plan changes follow the same rule. A temporary read failure shows the last successful result with a warning and disables state-changing controls. Access denial hides the cached command result; retrying the read can restore it.

With `lab.control` permission, **Lab controls** shows fake-provider status and paginated capture and refund records. These are observations of a separate local database, and monetary amounts remain exact minor-unit strings in the API.

To limit administrator permissions, optionally set `BILLFORGE_ADMIN_CAPABILITIES=read` or provide other comma-separated capabilities. Without this setting, the local administrator has full permissions. Restart to apply capability changes. Commands being recovered are re-evaluated against the new permissions and any existing external obligations; see the [Web Admin implementation record](docs/implementation/web-admin.md).

To embed frontend assets in the binary, build the frontend first and then run the `go build -tags admin_ui` command above. Without that tag, the development server reads built assets from `cmd/lab/adminassets/dist`. See the [Web Admin guide and acceptance record](docs/implementation/web-admin.md) for operations, upgrades, and recovery.

This project does not cover real payments, taxes, multiple currencies, a production general ledger, or public deployment.
