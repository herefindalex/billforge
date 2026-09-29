# Phase F2: Corrections and Credits

**English** | [繁體中文](phase-f2-corrections.zh-TW.md) | [简体中文](phase-f2-corrections.zh-CN.md)


The date is 2026-09-26. This is a native Go+SQLite snippet of S11, using an independent SQLite file simulation payment service. The unit of amount is the smallest unit of currency (USD cents). It verifies the following accounting variables and does not represent that it has formal accounting or payment service capability.

## Quantity models and sources of facts

`invoices.total_minor` has not been modified. Each time the deduction is corrected, the unchanging `corrections` is written, recording the reason, the request and the time before and after the correction. Payments have been retained at `allocations`; If the amount received exceeds the new amount received, the `allocation_releases` is written in the original payment operation and the `credit_grants` is generated in the same amount. This source chain makes credit available only from actual receipts.

```text
Amount due = original invoice amount − cumulative reductions
Net paid = original payment allocations − released allocations + applied credit
Outstanding = amount due − net paid

Available credit = grant amount − applied − reserved refunds − successful refunds
```

`Balance` and `CreditBalance` will calculate these numbers and reject negative or excess status. `Snapshot.AllocatedMinor` is still the total** of the original receipts allocated**; See the corrected receipts and net payments for `Balance`. Several reductions have been corrected, with the respective amounts and sources being kept in step. Full exemption from receipts, increased invoice amounts, cancellation of corrections and chargeback have not yet been implemented.

## Operations and status

1. `PostReduction` is re-weighted with a stable request key. If the payment operation has been sent or the result is UNKNOWN, refuse to correct; Unsubmitted transactions are cancelled in the same local transaction to avoid withdrawals in old currency. If the initial invoice is cleared and the subscription is activated in the same transaction, the entitlement projection can be reconstructed by `RefreshEntitlements`.
2. `CreatePayment` can be partially paid for the remaining receivables created. It can replace the initial full-scale operations that have not yet been sent when the accounts are created; If another operation created by the caller is still to be sent, the new operation will be rejected and the original request key and operation identity will be retained.
3. `ApplyCredit` only allows current renewal of invoices issued by the same customer, currency, and non-source, and may not exceed the amount available for grant or the amount not paid for the invoice. It also handles previously unsubmitted transactions in old money; It was sent out or rejected when it was unknown. After successfully joining entitlement, rebuild outbox.
4. `ReserveRefund` retains the grant budget locally, then returns the money to the independent fake provider with a stable provider key and the original capture key. Keep UNKNOWN and budget withholding when the response is lost; Search or trusted webhook Returns to Refunds after Confirmation of Success, and Returns for Confirmation of Failure. Re-send request keys only re-send the original operation, and the amount of change will conflict.

SQLite binds and triggers to prevent deductions from exceeding the original issuance, allocating or releasing excesses, non-source grants, cross-customer credit applications, exceeding the targeted amount of outstanding receipts, and credit applications and withdrawal retention totals exceeding the same grant. Fake providers also limit the cumulative refund of the same capture to no more than the actual amount. External results are not shared with local accounts; To crash with UNKNOWN, you have to find and match it with the original key.

## S11 amount of oracle

The original invoice is $100, and $80 after correction:

|Received|It was not repaired.|Credit for newly born children|
| ---: | ---: | ---: |
| $0 | $80 | $0 |
| $60 | $20 | $0 |
| $100 | $0 | $20 |

If the $20 grant has been applied for the next $10, the maximum amount can be $10 only. While the refund is UNKNOWN, the $10 continues to occupy the grant; It's not possible to re-use it if it fails. Another case is that $60 has been received, with a discount to $60: no credit, must be closed, initial subscriptions and entitlement activated.

## Verification of evidence

- `go test -count=1 ./...`: covers the three payment locations, partial payments, successful/failed/UNKNOWN refunds, request key playback, cross-customer rejection, direct SQL over-insert rejection, the budget caps for two database connections to be maintained at the same time, and F1/E regression.
- `go test -count=10 ./lab -run 'TestRefundFailureReleasesReservationAndConcurrentRequestsStayBounded|TestS11NewPaymentDoesNotSilentlyCancelCallerOperation'`: Re-check the identity of the refund competition and the pending payment operations.
- `go test -race -count=1 ./lab -run 'TestRefundFailureReleasesReservationAndConcurrentRequestsStayBounded|TestS11NewPaymentDoesNotSilentlyCancelCallerOperation'` and `go vet ./...`: Check and release testing and static problems.
- `go run ./cmd/lab correction-demo`: Basic $20 receipt, with a deduction of $4; The next round applies $2, the refund $2, and the last grant is $0. The JSON output simultaneously lists the balance of the two invoices, the refund status, and the grant balance.

The main programs are at `lab/corrections.go`, `lab/money.go`, `lab/refunds.go`, the database is restricted at `lab/lab.go`, the fake provider at `lab/provider.go`; The return test was conducted at `lab/corrections_test.go` and `lab/corrections_edges_test.go`.

## Intentionally maintained borders

This slice only deals with the ** deductions** of fixed-price invoices that have been drawn up, with no S07 measurement, S09 price version switching, taxes, exchange rates, proportional billing, revenue alignment, over-the-counter, reconciliation batch or operating approval process. credit limits are only applicable to subsequent renewals in the native model; Fake providers are still imitating the destination of the refunds and the actual funds in the accounts. The entitlement can be reconstructed and the project needs to run `RefreshEntitlements`, and there is currently no background scheduling to guarantee its delay. Performance, availability and cross-sectoral processes have not yet been verified with real workloads.
