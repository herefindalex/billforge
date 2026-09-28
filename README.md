# Billforge Commerce Correctness Lab

English | [Traditional Chinese](README.zh-TW.md) | [Simplified Chinese](README.zh-CN.md)

Billforge is a local lab for testing commerce system correctness. It uses Go and two separate SQLite databases to exercise price versions, subscriptions and contracts, payments and entitlements, usage closing, adjustments, refunds, reconciliation, and account migration. See the [platform plan](docs/commerce-lab-plan.md) and [A–D design](docs/design/README.md) for the design, and the [MVP completion tracker](docs/implementation/mvp-completion-tracker.md) for implementation evidence.

## What this lab checks

- Published prices and contracts retain their versions so later changes do not silently rewrite existing obligations.
- Administrative changes use previews, source version checks, idempotency keys, and command receipts. When a payment or refund dispatch preview becomes stale, the Web Admin shows the previous preview alongside the current operation state.
- The commerce database and fake provider database are separate, allowing retries, lost responses, and reconciliation to be tested across an external boundary.

## Requirements and tests

You need Go 1.27, CGO, and a C compiler. The Web Admin also needs Node.js and pnpm. The end-to-end tests need Python 3 and Chrome or Chromium, or a Playwright-installed Chromium browser. Payments use a local fake provider; no real payment service is contacted.

```sh
go test ./...
pnpm --dir web/admin install --frozen-lockfile
pnpm --dir web/admin build
pnpm --dir web/admin test:e2e
go build -tags admin_ui -o /tmp/billforge-admin ./cmd/lab
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

To limit administrator permissions, optionally set `BILLFORGE_ADMIN_CAPABILITIES=read` or provide other comma-separated capabilities. Without this setting, the local administrator has full permissions. Restart to apply capability changes. Commands being recovered are re-evaluated against the new permissions and any existing external obligations; see the [Web Admin implementation record](docs/implementation/web-admin.md).

To embed frontend assets in the binary, build the frontend first and then run the `go build -tags admin_ui` command above. Without that tag, the development server reads built assets from `cmd/lab/adminassets/dist`. See the [Web Admin guide and acceptance record](docs/implementation/web-admin.md) for operations, upgrades, and recovery.

The A01–A30 Web Admin acceptance matrix and its remaining checks are tracked in the [Web Admin acceptance record](docs/implementation/web-admin.md).

This project does not cover real payments, taxes, multiple currencies, a production general ledger, or public deployment.
