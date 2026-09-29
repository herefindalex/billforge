# Phase F4: Immediate Upgrades

**English** | [繁體中文](phase-f4-immediate-upgrade.zh-TW.md) | [简体中文](phase-f4-immediate-upgrade.zh-CN.md)


## Scope of operation

`RequestImmediateProUpgrade` creates a supplementary gap invoice to subscribe to revision CAS and request the key. Only valid and paid Basic subscriptions are accepted; Subscriptions to be rescheduled, canceled and upgraded during other indefinite periods are mutually exclusive. The price is based on the Pro catalog selection request, which was issued at the time. The original Basic assignment remained valid until payment was confirmed.

The amount is calculated in terms of the ratio of the remaining NACE/Service Period NACE, and divided by half-even to cents, then by the remaining Pro price minus the Basic unused price. From 2026-09-01 to 2026-10-01  Basic $20  Pro five seats $100 for example, 09-16 request to create an independent $40 invoice with a stable provider operation. Basic bills are still in circulation.

Payment `UNKNOWN` maintains original operation and maintains Basic. After verifying the original payment successfully, it is recalculated to the actual opening time: Pro $43.33  Basic without using $8.67, actual receipt $34.66  The system atoms shut down the Basic assignment, open the Pro assignment, add the subscription revision, and use the permanent outbox to make $5.34 of the reduction correction. Correction of only the release of funded credit from the allocation received; It's not going to be sent back to customers.

At the time of the deadline, the upgrades were unresolved to prevent the subsequent renewal of the price, leaving the `renewal_holds`. If the payment is confirmed after expiration, the upgrade is marked `needs_review` and does not replace the expired Pro service. Operators can run from the CLI to handle "sub-differentiation for non-upgrading services"; This special command will roll back the full amount of the supplementary spread invoice obligation, generating credit on actual receipt, followed by the existing credit refund process. The processor key is fixed to change ID, and the crash re-run will return the same correction. After processing is completed, the original Basic price can be renewed.

## Operations and verification

CLI's "Subscription Scheduling and Cancellation" offer mid-term upgrades; "Payment" provides a confirmation of the shipment and original operation; "Invoice correction and credit" provides delayed opening correction and non-service compensation; "State Search" provides changes, amounts, status and processing labels.

`go test -count=1 ./...` has been approved. S07 oracle covers $40, payments UNKNOWN maintains Basic, 09-18 corrects $5.34 and only generates $5.34 funded credit, no repeat runs, deadlines suspended with full compensation. Restrictions on retention: This slice only deals with Basic → Pro and the add-on gap is positive; Medium-term downgrades, additional usage, enterprise contracts and multi-currency segments are handled by subsequent situations.
