# Phase F1: Renewals, Failed Collections, and Grace Periods

**English** | [繁體中文](phase-f1-renewals.zh-TW.md) | [简体中文](phase-f1-renewals.zh-CN.md)


Status: Go+SQLite native snippet, corresponding to [S02/S03 Design](../design/03-scenarios-and-reconciliation.md). `go test ./...`, `go vet ./...` and `renewal-demo` are already running; It's not a complete Commerce MVP or production warranty.

## Lasting Facts and Time Policy

- `billing_periods` Save each subscription for `period_index`、 `[period_start, period_end)`、 `due_at`、 `invoice_id`, with the subscription/index and the subscription/start respectively being unique. The first purchase is index 0. Only the first invoice has been distributed in full, the subscription is valid, and the worker is generated when it runs within 7 days after the start of the period.
- The cyclical focal point is the UTC month and hourly seconds of the initial** confirmation of payment and activation of subscriptions**; Wait for non-consumption service months between acceptance of Quote and confirmation of receipt. 31st of the month ending at the end of the month, and then on the 31st; For example, 2027-01-31 → 02-28 → 03-31 → 04-30 Renewed the PriceVersion subscription selection without renewing the catalog selection from the current CatalogSelection. The current billing period cannot be rewritten.
- Renewal invoice validated at the beginning of the term, fixed fee of $20 prepaid, `due_at=period_start`. Payment operations have their own fixed provider key/payload; The `RetryFailedPayment` with the request key was used to create a new operation. the same request key as the original operation; A different key is rejected when the operation is still unresolved/successful.
- Subscription is still in business status as `active`. entitlement projection using the current billing period, invoice allocation, 7 days grace and clock calculation: `active` has been paid for this period; Unpaid and in grace `grace`; Grace has arrived and no check on payment `suspended`. `submitted/unknown` is suspended for `grace` when the deadline is exceeded, on the grounds that `payment_verification_pending` is suspended until the original provider is checked for operation or manual processing. `suspended` is available after the billing period and without a new service period.
- The renewal worker will not be able to run until the grace deadline is reached or later, and will not be able to replace the full bills that may not have been provided during the service, but will write the only `renewal_holds`. A default payment at the end of the billing period cannot be collected directly by retry. If a new operation is sent after the first payment has been renewed or failed, it will be terminated for `late payment requires service correction review` if it is sent later than the grace deadline; This slice will only automatically process the full refund before the deadline (with the deadline). The first unpaid invoice does not automatically generate the next unpaid invoice.

## E → F1 data upgrades

The old E scheme restricted one invoice per subscription, one PaymentOperation per invoice. `Open` copies the two tables before the transaction, removing the restrictions and retaining the existing ID/line data; Add the billing period, entitlement source field and new trigger to index 0. The old fake provider success capture has also been upgraded to a record-breaking list of failures. After the upgrade, `foreign_key_check` is running. Testing old-form SQLite files to verify that the original invoice line, operation and provider results are still recoverable and that old subscriptions can be renewed. The old E data does not independently save the actual initial opening time, and can only be replenished with `created_at` as a point of reference; If an old subscription has been delayed, it must be verified and corrected alternately, and cannot be claimed to have been replenished during the period of actual service restored.

## Repeatable evidence

```sh
go test ./...
go vet ./...
go run ./cmd/lab renewal-demo
```

The four states of `renewal-demo` are: Initial receipt success/entitlement active; Renewed in October for failure/grace determination; expired/suspended on day 7; Additional support for the new operation/active. The output contains two invoices and the source identity of two provider keys.

|Testing|Check points|
| --- | --- |
| `TestS02MonthEndAnchorAndIdempotentRenewal` |01-31 → 02-28 → 03-31 → 04-30, same period worker Returns not to repeat production tickets, entitlement restored after payment.  |
| `TestDelayedInitialConfirmationMovesServiceAnchor` |9/1 is initiated and the provider has received 9/3 until confirmed and activated, with the first version being 9/310/3; It's not an early 10/1 renewal.  |
| `TestS03FailedRenewalGraceSuspensionAndRecovery` |Determine if the original operation has failed and suspend the grace deadline after reopening DB; The new operating supplement with a stable request key and only allocated once.  |
| `TestUnknownRenewalRetainsVerificationGrace` |The provider has been receiving but locally UNKNOWN, for more than 7 days as a misappropriated failure; After the operation of the charger, it becomes active.  |
| `TestMissedRenewalGraceCreatesHoldInsteadOfBackBilling` |If the worker is late, the re-run will remain the same for the remainder of the unpaid period.  |
| `TestFailedPeriodCannotBeChargedAfterItEnds` |After the billing period, full monthly retirement is prohibited.  |
| `TestLateFullAmountDispatchRequiresReview` |Full renewal operations not yet sent after grace shall not be deducted for the first time, to avoid collection during the period not provided.  |
| `TestUpgradeStageEDatabasesPreservesFacts` |The invoicing, payment identity and provider results of the old scheme are retained and can be renewed after the additional billing period.  |

## The next frontier

This slice is for a Basic fixed fee only, with no repair, credit/refund, delayed use, contract or cohort migration. `renewal_holds` is a verifiable stop signal with no man-made deactivation and gap calculation process; `payment_verification_pending` also needs to be coordinated to find and stop alarms. If the payment has been confirmed but the entitlement projector is stagnant, the actual service is later activated than the subscription, and service delays need to be corrected; Currently, only pending projections are recorded. The next step is to make a non-negotiable adjustment to the source/reserve amount of the funded credit, and then add the volume clearance and the full component pricing of the PriceVersion.
