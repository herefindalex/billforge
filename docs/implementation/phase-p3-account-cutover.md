# Phase P3 (P03): Account Cutover

**English** | [繁體中文](phase-p3-account-cutover.zh-TW.md) | [简体中文](phase-p3-account-cutover.zh-CN.md)


## Models of local migration

`account_links` Saves old account IDs, Commerce customer IDs, beneficiary, cohort, historical data tags, read sources and the only command writer. Accounts with existing Commerce invoices must be marked with historical data and back to source. Migrate the initial read and write source to `legacy`; New purchases, contract acceptance, subscription changes and new usage events will check entries in Commerce transactions to avoid creating new business facts on both sides at the same time. The same requested keys can still be replayed.

`ShadowQuote` is a read-only quote based on the price and account cohort that has been published so far, and preserves the amounts/currencies provided by the old system, the results of the Commerce, and whether the results correspond to the local computing delays. `ShadowEntitlement` preserves the old entitlement status and Commerce subscribes to the projection results. Shadow only records comparative evidence, and does not create receivers or call providers.

`BackfillLegacyProvenance` will connect the old subscription/invoice ID to the Commerce subscription, invoice and PriceVersion. Only clients, invoices, billing periods and non-variable invoice lines that match each other are marked `complete`; The remainder of the code `manual_review`. The reviewer can use `ResolveLegacyProvenance` to specify the correct mapping of the verified and leave the resolution, original issuance and assignment unaltered. The same Commerce invoice cannot be authenticated by two old invoice IDs. Accounts with historical data must complete the mapping of all existing receipts.

`MigrationReadiness` requires the latest shadow quote and entitlement to match, replenish in full, the last reconciliation not earlier than shadow, payment without `created/submitted/unknown` pending operations, account-related reconciliation differences within the set threshold, and quote p95 does not exceed the baseline set up ceiling. The fixed payment requirement is zero. First, switch the read source, then check the threshold for trading in the same database and switch the only writer. `StopAccountMigration` records the cause of the stop and blocks the new Commerce command; The accepted obligation remains to retain the original writers and readers for reconciliation, verification and correction.

CLI "Account boundaries and gray migration" can be mapped, shadow, replenished, manually modified, readied, read switches, write switches, stop and adapter entitlement search. "State Search" lists the owners and readiness of each account. `GET /v1/internal/accounts/{account}/entitlements/{subscription}`, which is protected by an internal certificate, will return entitlement as currently read from the source.

## Acceptance and scope

`go test ./lab -run TestP03 -count=1` verifies shadow mismatch, stops switching, legacy writer, rejects new purchases, reads the original request before writing switching, rebroadcasts the original request after switching, rejects new commands after stopping, unknown payment and missing sources, refills stop switching, restores readiness after artificial verification. API contract testing verification adapter read switches and internal credentials.

This is a migration exercise in a single SQLite program, and the old system observation values are provided by the operator. It does not connect real units or measure production flow. Shadow's readiness is currently updated for each account, each type; The actual rollout requires a representative sample based on tenant, plan, contract and old price cohort, setting a true delay and differential baseline, and configuring a formal identity verification, authorization and audit retention policy.
