# Commerce MVP: A–D Design

**English** | [繁體中文](README.zh-TW.md) | [简体中文](README.zh-CN.md)

A–D are design exercises. Local implementations of E, F1–F8, and P01–P03 exist, but the overall MVP has not been validated for production. Use the [MVP completion tracker](../implementation/mvp-completion-tracker.md) for implementation evidence and remaining conditions, and the [platform plan](../commerce-lab-plan.md) for scope.

| Stage | Document | Review focus |
| --- | --- | --- |
| A: Policy | [01 Pricing Versions and Business Policies](01-pricing-and-policies.md) | `PriceVersion`, D01–D12, rounding, proration, and payment and entitlement timing |
| B: Domain model | [02 Fact Ownership and Invariants](02-domain-and-invariants.md) | State machines, atomic boundaries, and I01–I21 counterexamples and safeguards |
| C: Failures | [03 Scenarios, Reconciliation, and Repair](03-scenarios-and-reconciliation.md) | Expected facts, amounts, and recovery behavior for S01–S12 |
| D: Platform | [04 APIs, Compatibility, and Evolution](04-platform-apis-and-migration.md) | P01–P03, two consumer types, price migration, and gradual rollout |

Read the policies first, then fact ownership and failure scenarios, and finally API and migration design. All times are UTC. Service and price intervals are half-open: `[start, end)`. Monetary amounts are in USD minor units unless stated otherwise.

The design excludes taxes, a production general ledger, a revenue recognition engine, multiple currencies, arbitrary promotions, complete tiered pricing, and real payment processors. These limits also apply when interpreting local test results.

## Web Admin design

- [05 Web Admin Design](05-web-admin.md) describes the administration experience, its relationship to the CLI and API, previews, permissions, and command recovery. Implementation evidence is in the [Web Admin guide](../implementation/web-admin.md) and [action audit](../implementation/web-admin-action-audit.md).
- [06 Engineering Contracts](06-web-admin-contracts.md) defines the React interface, `.env` administrator login, API contracts, and the C01–C49 command matrix.
- [07 Implementation Plan](07-web-admin-implementation-plan.md) records 22 implementation tasks and their dependencies.
- [08 Acceptance Plan](08-web-admin-test-plan.md) defines A01–A30 acceptance checks.
