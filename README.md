# Billforge

**English** | [繁體中文](README.zh-TW.md) | [简体中文](README.zh-CN.md)

Billforge is a local lab for testing the correctness of commerce and monetization workflows. Its Go backend keeps commerce state and the fake payment provider in separate SQLite databases, so failures across that boundary can be observed and reconciled.

The lab covers price versions, subscriptions, contracts, payments, entitlements, usage period close, corrections, credits, refunds, reconciliation, and account migration. It provides an interactive CLI, a local API, and a React and Ant Design administration interface.

## Current status

The local CLI, API, and Web Admin built with React and Ant Design are available. Web Admin A01–A30 acceptance remains in progress. The [implementation and acceptance guide](docs/implementation/web-admin.md) separates verified cases from remaining checks, and the [MVP completion tracker](docs/implementation/mvp-completion-tracker.md) identifies what has been implemented and verified.

The [implementation and acceptance index](docs/implementation/README.md) links every delivery record, operator guide, and evidence audit.

## What the lab tests

- Published price versions preserve the terms behind existing obligations.
- Administrative commands use server-side previews, source revision checks, idempotency keys, and durable receipts. Clock, provider decision, and fault controls also require previews.
- Separate commerce and provider databases expose retries, lost responses, crashes, and reconciliation across an external boundary.
- Reconciliation detects mismatches between expected payments and provider observations. A lost response to a successful repair can be recovered with the original request key without applying the repair again.

For the design rationale, see the [platform plan](docs/commerce-lab-plan.md) and [A–D design index](docs/design/README.md).

## Requirements and checks

The Go backend requires Go 1.27, CGO, and a C compiler. Web Admin requires Node.js and pnpm. Browser tests require Python 3 and Chrome or Chromium, including Playwright's installed Chromium.

```sh
go test ./...
pnpm --dir web/admin install --frozen-lockfile
pnpm --dir web/admin build
pnpm --dir web/admin test:e2e
go build -tags admin_ui -o /tmp/billforge-admin ./cmd/lab
```

To audit whether browser tests submitted every C01–C49 action and received a successful command receipt:

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

The interactive menu runs operations and displays current state; see the [CLI guide](docs/implementation/interactive-cli.md). `serve` accepts a loopback address only. Some internal HTTP operations require `BILLFORGE_INTERNAL_TOKEN`; see the [v1 API guide](docs/implementation/phase-p2-v1-api.md).

## Web Admin

Copy `.env.example` to `.env`, then set `BILLFORGE_ADMIN_USERNAME` and `BILLFORGE_ADMIN_PASSWORD`. The password must be 12–72 bytes. Keep it in the server environment or `.env`, never in a `VITE_` variable, and restrict access to `.env`. Use two **different**, persistent SQLite files.

```sh
mkdir -p ./billforge-data
pnpm --dir web/admin install --frozen-lockfile
pnpm --dir web/admin build
go run ./cmd/lab admin ./billforge-data/commerce.db ./billforge-data/provider.db 127.0.0.1:8080
```

Open `http://127.0.0.1:8080/admin/`. The admin server binds to an explicit loopback IP and checks that the request Host matches it. To use another environment file, place `--env-file path` before the database paths. Process environment variables take precedence over values in that file.

The interface shows published price terms and related quotes and subscriptions. It provides paginated resource lists, batch job and price migration details, and exact monetary amounts in minor units. After applying or skipping a migration item or resuming a batch, refresh the details to see current counts. On a temporary read failure, the interface marks cached data as stale and disables actions that depend on it. On access denial, it hides cached data, including overview counts.

An administrator with the `lab.control` capability can inspect fake provider capture and refund records and use previewed lab control commands. Set `BILLFORGE_ADMIN_CAPABILITIES=read` or another comma-separated capability list to restrict the local administrator. If the variable is unset, the administrator has all local capabilities. Restart the server after changing capabilities; recovered commands are checked against the current policy.

To embed frontend assets in the binary, build the frontend first and use the `admin_ui` build tag shown above. Without the tag, the development server reads built assets from `cmd/lab/adminassets/dist`. The [Web Admin guide](docs/implementation/web-admin.md) describes operations, database upgrades, and recovery behavior.

## Boundaries

Billforge uses a local fake payment provider. It does not process real payments or include tax calculation, multiple currencies, a production general ledger, or public deployment.
