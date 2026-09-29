# Phase E: Minimum Executable Core (S01/S04/S05)

**English** | [繁體中文](phase-e.zh-TW.md) | [简体中文](phase-e.zh-CN.md)


Status: The E slice passed `go test ./...` locally and ran three CLI demos. Later renewal capabilities are covered in [F1](phase-f1-renewals.md). The table below records the implementation boundary of E at that time, rather than every capability now in the codebase. This was not production validation. See the [design index](../design/README.md).

## Making the border

|It has already been done.|It has not yet been implemented.|
| --- | --- |
|Basic v1 $20 Fixed fee, pin quotes, one-time acceptance and HTTP commandidempotent identity|Pro, seats, usage, proportional billing, contract and price migration|
|Invoice/ single-line authentication, unchanging triggers, single Payment Operation and outbox|Rectification, credit, refund, completeness must be included in the balance sheet.|
|It's a free and open source SQLite fake provider. The same provider key/payload is just a capture.|Real PSP, webhook signatures, network retry and provider SLA|
| UNKNOWN outcomes, original-key lookup after crashes, inbox deduplication, monotonic success, and unique allocation | Definitive failure, grace periods, cancellation, and multi-period renewals |
|entitlement was reconstructed by successful operation + full allocation.|Complex EntitlementPolicyVersion and multi-service entitlement|

The two documents each submitted a transaction. Commerce keeps the Quote receipt, invoice confirmation, fixed amount/currency/key Payment Operation, outbox and audit at the same time in local transactions. After that, they called the fake provider. The provider first submits the capture and then responds to Commerce; It's a faulty injection point between the two. `lost_response` will record local operations as UNKNOWN; `crash_after_provider` remained in SUBMITTED. When restored, check the original provider key to confirm that an allocation has been successfully entered and then sort it into the entitlement reconstruction work. The transaction does not automatically infer "certain no deductions".

## Verify the oracle

|Testing|The main claim|
| --- | --- |
| `TestS01NewBasicPurchase` | Quote $20； pending, no allocation before payment; A single $20 allocation after success; entitlement was rebuilt by outbox.  |
| `TestS04LostResponseKeepsOriginalOperation` |The provider has already captured, locally unknown; It was a success after the original key was checked. Rechecking only has one provider capture and one allocation.  |
| `TestS05CrashWebhookOrderAndProjectionRecovery` |Provider submits a post-reboot process; Success webhook Returns and old pending late to unreturn; Re-opened and rebuilt entitlement without a second financial effect.  |
| `TestQuoteExpiryIdentityAndPriceImmutability` |The fingerprint/idempotency conflict is rejected, the key can't be retrieved with Quote, the price has been released and the payload is unchanged.  |
| `TestQuoteExpiresAndAbsentProviderResultIsNotFailure` |The expired quote is rejected; Providers remain undefined when searching for non-original operations, not randomly allocated or open.  |

The CLI has three paths: normal `pending → active → entitlement active`; lost response `unknown → provider lookup → succeeded`； The `submitted → provider lookup → succeeded` crashed. The three ended up with a $20 allocation.

## Gaps before the next phase F

The PriceVersion of this slice has a fixed fee field only, and does not fully land the charge definitions, rounding, proration, available for purchase in the effective range of phase A. CatalogSelection only provides Basic default. There is no HTTP authentication, authorization or webhook signature; Testing with a trusted in-process fake provider. SQLite trigger locks the main financial payload on this slice, but there is no generalised billing close, gap, withdrawal reservation or reconciliation engine. The next step is to start with the S02/S03 renewal and debt policy, using the same source identity and outbox model, and then add the S07 S11's amount model.
