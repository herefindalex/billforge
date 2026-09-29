# Billforge

**English** | [繁體中文](README.zh-TW.md) | [简体中文](README.zh-CN.md)

Billforge is a local lab for testing commerce and monetization correctness. A Go backend uses separate SQLite databases for commerce state and a fake payment provider. The lab exercises price versions, subscriptions, contracts, payments, entitlements, usage period closing, adjustments, refunds, reconciliation, and account migration.

## Current status

The interactive CLI, local API, and React and Ant Design Web Admin are available. Web Admin acceptance cases A01–A30 are still in progress. The [acceptance record](docs/implementation/web-admin.md) distinguishes verified cases from remaining checks.

## What the lab exercises

- Published price and contract versions preserve the terms behind existing obligations.
- Administrative commands use server previews, source revision checks, idempotency keys, and command receipts. New clock, provider decision, and fault controls also require a server preview. If a payment or refund dispatch preview becomes stale, the Web Admin shows both the original preview and the current operation state for review.
- Separate commerce and fake provider databases make retries, lost responses, crashes, and reconciliation across an external boundary observable.
- Reconciliation checks a provider amount mismatch against the payment being repaired. That payment is blocked while unrelated payments can still recover. If the browser loses a successful repair response, the administrator can retrieve the command using its original request key without repeating the repair.

Read the [platform plan](docs/commerce-lab-plan.md) and [A–D design](docs/design/README.md) for the design rationale. The [MVP completion tracker](docs/implementation/mvp-completion-tracker.md) records implementation evidence.

## Requirements and checks

You need Go 1.27, CGO, and a C compiler. The Web Admin also needs Node.js and pnpm. End-to-end browser tests need Python 3 and Chrome or Chromium, including Playwright's installed Chromium. Payment operations use only the local fake provider; they do not contact a real payment service.

```sh
go test ./...
pnpm --dir web/admin install --frozen-lockfile
pnpm --dir web/admin build
pnpm --dir web/admin test:e2e
go build -tags admin_ui -o /tmp/billforge-admin ./cmd/lab
```

To check that browser tests submitted every C01–C49 action and received a successful command receipt, capture and audit the action cases:

```sh
audit_file="$(mktemp /tmp/billforge-action-cases.XXXXXX)"
BILLFORGE_E2E_ACTION_CASE_AUDIT="$audit_file" pnpm --dir web/admin test:e2e
node web/admin/e2e/audit-action-cases.mjs "$audit_file"
```

## CLI and local API

```sh
mkdir -p ./billforge-data
go run ./cmd/lab
go run ./cmd/lab menu ./billforge-data/commerce.db ./billforge-data/provider.db
go run ./cmd/lab serve ./billforge-data/commerce.db ./billforge-data/provider.db 127.0.0.1:8080
go run ./cmd/lab demo
go run ./cmd/lab renewal-demo
go run ./cmd/lab correction-demo
go run ./cmd/lab demo /tmp/billforge-commerce.db /tmp/billforge-provider.db lost_response
go run ./cmd/lab demo /tmp/billforge-crash-commerce.db /tmp/billforge-crash-provider.db crash_after_provider
```

The interactive menu runs operations and shows current state; see the [CLI guide](docs/implementation/interactive-cli.md). `serve` accepts only a loopback address. Some internal HTTP operations require `BILLFORGE_INTERNAL_TOKEN`; see the [v1 API guide](docs/implementation/phase-p2-v1-api.md).

## Web Admin

Copy `.env.example` to `.env`, then set `BILLFORGE_ADMIN_USERNAME` and `BILLFORGE_ADMIN_PASSWORD` (12–72 bytes). Keep the password in the server environment or `.env`, never in a `VITE_` variable, and restrict local access to `.env`. Use two **different**, persistent database files.

```sh
mkdir -p ./billforge-data
pnpm --dir web/admin install --frozen-lockfile
pnpm --dir web/admin build
go run ./cmd/lab admin ./billforge-data/commerce.db ./billforge-data/provider.db 127.0.0.1:8080
```

Open `http://127.0.0.1:8080/admin/`. The admin server binds to an explicit loopback IP and checks that the request Host matches it. To use another environment file, put `--env-file path` before the database paths. Process environment variables take precedence over file values.

Contract version details show published terms, related quotes, and subscriptions. The detail page shows up to 20 quotes and 20 subscriptions; its links open the corresponding paginated lists filtered by contract version. A new quote can be started with the customer and contract version already filled in.

The batch jobs page shows recent jobs, item counts, and detail links. Cursor pagination keeps previously read pages stable when new jobs arrive. Price migration details show server-calculated status totals and a paginated, status-filtered item list. Monetary amounts remain in exact minor units. After applying or skipping an item or resuming a batch, refresh the details to see the latest counts and pages.

On a temporary read failure, detail pages and command results label previously loaded data as stale and disable actions that depend on it. Access denial hides cached data, including lists and overview counts. A successful retry can show fresh data again.

With `lab.control` permission, Lab Controls shows fake provider status and paginated capture and refund records from the separate local database. New control commands require a server preview and final confirmation. To restrict the local administrator, set `BILLFORGE_ADMIN_CAPABILITIES=read` or another comma-separated capability list. With no capability setting, the administrator has full local permissions. Restart the server to apply capability changes; recovered commands are checked against the current policy and existing external obligations.

To embed the frontend assets in a binary, build the frontend first and use the `admin_ui` build tag shown above. Without the tag, the development server reads built assets from `cmd/lab/adminassets/dist`. The [Web Admin guide and acceptance record](docs/implementation/web-admin.md) cover operations, upgrades, and recovery behavior.

## Boundaries

Billforge does not include real payment processing, taxes, multiple currencies, a production general ledger, or public deployment.
