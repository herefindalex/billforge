# Billforge

English | [繁體中文](README.zh-TW.md) | [简体中文](README.zh-CN.md)

Billforge is a local commerce correctness lab. It uses Go and separate SQLite databases for commerce data and a fake payment provider to exercise pricing, subscriptions, contracts, payments, entitlements, usage period closing, adjustments, refunds, reconciliation, and account migration.

The CLI, local API, and React + Ant Design Web Admin are available. The Web Admin acceptance matrix is still in progress; see the [acceptance record](docs/implementation/web-admin.md) for verified cases and remaining work.

## What it exercises

- Published price and contract versions preserve the terms behind existing obligations.
- Administrative commands use previews, source checks, idempotency keys, and receipts. Lab controls for the clock, provider decisions, and fault tickets also require a server preview before a new command is accepted.
- Separate commerce and fake-provider databases expose retries, lost responses, crashes, and reconciliation across an external boundary.
- Reconciliation repair checks the affected payment's provider amount. A mismatch blocks that payment's recovery while unrelated payments can continue. A lost browser response can be recovered with the original request key without repeating the repair.

For the rationale and design, read the [platform plan](docs/commerce-lab-plan.md) and [A–D design](docs/design/README.md). The [MVP completion tracker](docs/implementation/mvp-completion-tracker.md) records implementation evidence.

## Requirements and checks

You need Go 1.27, CGO, and a C compiler. The Web Admin needs Node.js and pnpm. End-to-end tests also need Python 3 and Chrome or Chromium, including Playwright's installed Chromium. All payment operations use the local fake provider; no real payment service is contacted.

```sh
go test ./...
pnpm --dir web/admin install --frozen-lockfile
pnpm --dir web/admin build
pnpm --dir web/admin test:e2e
go build -tags admin_ui -o /tmp/billforge-admin ./cmd/lab
```

To audit that browser tests submitted every C01–C49 action and received a successful command receipt:

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

The interactive menu runs operations and shows current state. See the [CLI guide](docs/implementation/interactive-cli.md). `serve` accepts only a loopback address. Some internal HTTP operations require `BILLFORGE_INTERNAL_TOKEN`; see the [v1 API guide](docs/implementation/phase-p2-v1-api.md).

## Web Admin

Copy `.env.example` to `.env` and set `BILLFORGE_ADMIN_USERNAME` and `BILLFORGE_ADMIN_PASSWORD` (12–72 bytes). Keep the password in the server environment or `.env`, never in a `VITE_` variable. Restrict access to `.env` to your local user. Use two **different**, persistent database files:

```sh
pnpm --dir web/admin install --frozen-lockfile
pnpm --dir web/admin build
go run ./cmd/lab admin ./billforge-data/commerce.db ./billforge-data/provider.db 127.0.0.1:8080
```

Open `http://127.0.0.1:8080/admin/`. The admin server binds to an explicit loopback IP and checks that the request Host matches it. To use another environment file, place `--env-file path` before the database paths. Process environment variables take precedence over the file.

The contracts list opens version details with published terms, related quotes, and subscriptions. Each related section shows up to 20 records; its list links open paginated, version-filtered results. You can also start a quote with the customer and contract version already filled in.

**Batch jobs** lists recent jobs with item counts and links to their results. Cursor pagination keeps earlier pages stable when new jobs are created. **Price migration** details show server-calculated totals by item status. Items are paginated at 20 per page, support status filtering, and display exact minor-unit amounts. After applying or skipping items, or resuming a batch, refresh to see current counts and restart pagination.

Invoice, subscription, credit, payment, refund, price, and contract details retain the last successful data when a refresh fails temporarily. The UI labels that data stale and disables actions based on it; access denial hides cached data. Shared action forms and subscription plan changes apply the same rule to command results. A successful retry can restore the result.

Lab controls show fake-provider status and paginated capture and refund records. New control commands require a server preview and final confirmation. Optionally set `BILLFORGE_ADMIN_CAPABILITIES=read` or another comma-separated capability list to restrict the local administrator. With no capability setting, the administrator has full local permissions. Restart the server to apply capability changes; recovered commands are re-evaluated against the current policy.

For an embedded frontend, build the frontend first, then use the `admin_ui` build tag shown above. Without that tag, the development server reads built assets from `cmd/lab/adminassets/dist`. See the [Web Admin guide and acceptance record](docs/implementation/web-admin.md) for operations, upgrades, and recovery behavior.

## Boundaries

Billforge does not include real payment processing, taxes, multiple currencies, a production general ledger, or public deployment.
