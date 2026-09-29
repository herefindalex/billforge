# Phase F6: Usage

**English** | [繁體中文](phase-f6-usage.zh-TW.md) | [简体中文](phase-f6-usage.zh-CN.md)


## Events and Periods

`RecordUsage` is loaded with tenant+source+event ID, and the payload fingerprint contains subscriptions, meters, number and time of events; The same content returns to the original event, changing the conflict. The time of the event determines the billing period and the PriceVersion assignment at the time, and the time of receipt determines whether the account is cutoff. Basic or non-valid assignment tasks will be rejected. The user uses the new event ID to correct the reference to the original event and retains the timing of the original event. It is not possible to withdraw more than the original quantity.

`CloseUsagePeriod` maintains cutoff and first rating revision; `RerateUsagePeriod` is calculated as the total duration of the last event. Each revision preserves the original quantity, allowance, excess quantity, undelivered cents with a reasonable number, half-even deferred value and the relative difference between the previous edition; In the case of the original revision, no new facts are revealed.

## amount of oracle

Pro contains 20,000 tasks, which is $0.001 per task. The precise surplus was $0.003 when the initial period received 20,003 tasks, and the surplus was $0.003 when the initial period received $0.00; Subsequent renewal of the invoice retains the original use of the amount of 0. After 7 tasks late in the same period, a cumulative total of 20,010 tasks, accurate to $0.010, is collected at $0.01; The deduction is $0.00 and the next renewal invoice adds a $0.01 debit of the original billing period. The original invoices and their items remain unchanged.

If the subsequent correction makes the amount of invoices that have been issued higher than the amount re-evaluated, `RunUsageCreditNotes` corrects the invoice deduction on the actual carrying amount of the invoice, leaving the usage CreditNote associated with Correction; Funded credit is still released according to the original receipts allocation. If the negative difference has not been corrected, the renewal will not silence it and charge it for a new term. CLI "Quantity and Accounts" provides events, cancellations, cancellations, recalculations, and operations on CreditNote; "State Search" provides events, rating revisions, cumulative voting and adjusted amounts.

Verification includes the same ID re-send and content conflict, 20,003→$0.00, late to 7 tasks→ the next $0.01, re-calculation/renewal re-run, cancellation of 7 tasks→ original fee discount of $0.01, and only release of actual receipts of $0.01 credit.

Limitations: Currently, a billing period only supports one task price version; If there are two different usage rates for the same billing period, the account will reject and require that an allowance policy be defined across assignments. Other meters and new SKU components were expanded by P01.
