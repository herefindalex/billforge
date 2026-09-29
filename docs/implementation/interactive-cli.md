# Interactive CLI

**English** | [繁體中文](interactive-cli.zh-TW.md) | [简体中文](interactive-cli.zh-CN.md)


The date is 2026-09-26. This interface operates Billforge ** which has been implemented** on S01 and S12, P01 and P03 autocuts; It uses two durable SQLite files, where Commerce and fake providers are independent. There is no real payment connection.

## Starting

```sh
go run ./cmd/lab
# Or specify the databases to continue using
go run ./cmd/lab menu ./billforge-data/commerce.db ./billforge-data/provider.db
```

When there is no path, the program asks for the location of the two files, using `./billforge-data/commerce.db` and `./billforge-data/provider.db`. The same path opens again so you can continue to operate the original data. The main menu is left on `0`; The sub-menu was returned by `0`. Objects can be entered with a list number or full ID, and left blank when selecting an object can be returned.

|The menu|What can be done?|
| --- | --- |
|Find the current status.|View offers, subscriptions, billing periods, receipts/net payments/unpaid receipts, payments, corrections, credit, credit applications, refunds and outbox processing; Check the details by object, or check the fake provider results separately.  |
|Offerings and new purchases|Create a Basic、Pro or a published program/cohort offer, accept the offer and create an initial payment operation.  |
|Payments|Create a partial payment, retry to identify the failed payment, send the next payment, verify UNKNOWN with the original operation, set the false provider result.  |
|Renewal and entitlement|Running expiration renewals, rebuilding entitlement based on submitted accounting facts.  |
|In the case of a bank account, a bank account is a bank account.|The amount of overdrafted invoices shall be deducted from the originating credit for subsequent renewal of invoices.  |
|Refunds|Save the refund amount, send out, verify UNKNOWN, set the fake provider results.  |
|Set up a simulation time.|Enter RFC3339 UTC time, such as `2026-10-01T00:00:00Z`, or enter `real` recovery system time.  |
|Subscription schedules and cancellations|With the current revision, the Basic/Pro changes are deleted at the end of the current revision period, or restored before they take effect.  |
|Price version and cohort migration|Release Pro or configured measurement prices, register meter, specify cohort, and preview/manage subscription migration.  |
|How much is the amount of food?|According to the subscription meter, recording events, cancellation events, closing accounts, delay to recalculation and negative difference CreditNote  |
|Business contracts with Net30|It is the process of issuing contracts, making offers, accepting and opening services, and dealing with expire receipts.  |
|Reconciliation, Repair and Artificial Resolution|Preserving evidence of differences, specifying security repairs, checking payment operations, or recording artificial resolutions.  |
|Account boundaries and gray migration|Create source ID mapping, only read shadow comparisons, historical source replenishment, read/write switching and stop operations.  |

Amount of input for the smallest total currency unit**: USD 20.00 Please enter `2000`. The request key is the identity of the business operation; When the same operation retry, the same key must be entered. When a payment or refund displays a UNKNOWN/simulator crash, do not re-send it with a new request key. `lost_response` and `crash_after_provider` can be selected when sending operations.

## A handy path

1. In "Set Simulation Time", enter the `2026-09-01T00:00:00Z`.
2. Create and accept the Basic offer and press the request button on "Offer and New Purchase".
3. In the "payment" the initial payment is sent; Re-create entitlement in "Renew and entitlement".
4. In "Fixing Corrections and Credit", the $20 Fixing Reduction `400`, which generated $4 credit.
5. In the "return" and sending `200`; In "Set Simulation Time", move to `2026-10-01T00:00:00Z`, and then run the extension.
6. The remaining `200` credit will be used for renewal invoices; Create and send new payments for `1800` and reconstruct entitlement.
7. In order to find the current status, the initial confirmation of `1600` must be received, the credit must be renewed, the `200` must be applied, the `200` must be withdrawn, the `0` grant is available.

The simulation time exists only at the current CLI working stage and returns to system time after reopening; The facts of the written accounts are preserved for the long term. Commerce summaries are read in a single read-only transaction; The fake provider uses a different database, and the image is separated to show that the two sides are not the same across the database. This small lab's status search for unseparated pages, not as an official operating interface.
