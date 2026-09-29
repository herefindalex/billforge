# Billforge

**English** | [繁體中文](README.zh-TW.md) | [简体中文](README.zh-CN.md)

Billforge is a local lab for testing commerce and monetization correctness. A Go backend uses separate SQLite databases for commerce state and a fake payment provider. It exercises price versions, subscriptions, contracts, payments, entitlements, usage period closing, adjustments, refunds, reconciliation, and account migration. It includes an interactive CLI, a local API, and a React and Ant Design Web Admin.

The Web Admin acceptance matrix is still in progress. The [acceptance record](docs/implementation/web-admin.md) identifies verified cases and remaining work.

## What it exercises

- Published price and contract versions keep the terms behind existing obligations stable.
- Administrative commands use server previews, source checks, idempotency keys, and receipts. Clock, provider decision, and fault controls also require previews before new commands are accepted.
- Separate commerce and fake provider databases make retries, lost responses, crashes, and reconciliation across an external boundary observable.
- Reconciliation checks the affected payment's provider amount before repair. A mismatch blocks that payment's recovery while unrelated payments can continue. A lost browser response can be recovered with its original request key without repeating the repair.

Read the [platform plan](docs/commerce-lab-plan.md) and [A–D design](docs/design/README.md) for the rationale. The [MVP completion tracker](docs/implementation/mvp-completion-tracker.md) records implementation evidence.

## Requirements and checks

You need Go 1.27, CGO, and a C compiler. The Web Admin also needs Node.js and pnpm. Browser tests need Python 3 and Chrome or Chromium, including Playwright's installed Chromium. Payment operations use only the local fake provider; they do not contact a real payment service.

```sh
go test ./...
pnpm --dir web/admin install --frozen-lockfile
pnpm --dir web/admin build
pnpm --dir web/admin test:e2e
go build -tags admin_ui -o /tmp/billforge-admin ./cmd/lab
```

To verify that browser tests submitted every C01–C49 action and received a successful command receipt:

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

Copy `.env.example` to `.env`, then set `BILLFORGE_ADMIN_USERNAME` and `BILLFORGE_ADMIN_PASSWORD`. The password must be 12–72 bytes. Keep it in the server environment or `.env`, never in a `VITE_` variable, and restrict local access to `.env`. Use two **different**, persistent database files.

```sh
mkdir -p ./billforge-data
pnpm --dir web/admin install --frozen-lockfile
pnpm --dir web/admin build
go run ./cmd/lab admin ./billforge-data/commerce.db ./billforge-data/provider.db 127.0.0.1:8080
```

Open `http://127.0.0.1:8080/admin/`. The admin server binds to an explicit loopback IP and checks that the request Host matches it. To use another environment file, put `--env-file path` before the database paths. Process environment variables take precedence over file values.

The admin provides paginated resource lists and detail views. Contract versions show published terms and related quotes and subscriptions. Batch jobs show item counts and results; price migrations show server-calculated totals and item status. Amounts are displayed in exact minor units. Temporary read failures label previously loaded data as stale and disable actions based on it. Access denial hides cached data, including lists and overview counts; a successful retry can show fresh data again.

Lab controls show fake provider status and paginated capture and refund records. New control commands require a server preview and final confirmation. To restrict the local administrator, set `BILLFORGE_ADMIN_CAPABILITIES=read` or another comma-separated capability list. With no capability setting, the administrator has full local permissions. Restart the server to apply capability changes; recovered commands are checked against the current policy.

For an embedded frontend, build the frontend first and use the `admin_ui` build tag shown above. Without the tag, the development server reads built assets from `cmd/lab/adminassets/dist`. The [Web Admin guide and acceptance record](docs/implementation/web-admin.md) cover operations, upgrades, and recovery behavior.

## Boundaries

Billforge does not include real payment processing, taxes, multiple currencies, a production general ledger, or public deployment.
