# Phase F8: Reconciliation

**English** | [繁體中文](phase-f8-reconciliation.zh-TW.md) | [简体中文](phase-f8-reconciliation.zh-CN.md)


## Scope

`RunReconciliation` read the local subscription, entitlement, assignment, final invoice line, payment and outbox, and read the fake provider capture. Each time the run ID is saved, time and proof of difference are compared. Identification of the same type and object along the difference, and subsequent comparisons can update the proof or mark resolved; The old records are kept for inspection.

The CLI main menu "reconciliation, repair and artificial resolutions" can run comparisons, differences, differences IDs, or record artificial resolutions; "Status search" can also list differences. Each repair must provide a stable request key. The same key is used to play back the original operation. Differences between shared keys can conflict.

## Classification and Action

|The difference.|Classified|The action|
| --- | --- | --- |
|Missing or outdated entitlement projections, pending processing entitlement outbox| `SAFE_AUTO_REPAIR` |Rebuild projections and re-compare them based on subscription and account sources.  |
|Capture outbox to be processed| `RETRY_REQUIRED`； If payment has been submitted/unknown, please contact `EXTERNAL_LOOKUP_REQUIRED`|Specify the original payment operation and redirect; Unknown results only look for the original provider key.  |
|A successful provider, but not locally observed, payment results unknown.| `EXTERNAL_LOOKUP_REQUIRED` |Check the original key and use the amount/currency check.  |
|Unknown provider capture, amount/coin not matched, lack of contract follow-up price| `MANUAL_REVIEW` |Preventing automated repair and preservation of artificial resolutions and reviewers.  |
|Assignment is not consistent with subscription price, final invoice line is not consistent with amount| `UNSAFE_TO_REPAIR` |It is also important to keep in mind that the resolution is not automatically repaired or preserved.  |

The repair program saves expected, actual, source revision. Re-conciliation before the operation; If the source evidence, classification or revision changes are blocked, a new request key must be used. Once reconciliation is completed, the original difference is verified. The remainder are executed/waiting for further investigation. If the provider does not match the amount, the payment repair will be blocked. Artificial Resolutions only record reviews and do not change the payment, invoice or price facts.

## Verification and Restrictions

`go test ./lab -run TestS12 -count=1` covers missing entitlement reconstruction, replay, source revision, change, specify capture re-transmission, unknown original key search, and unknown or non-quantitative provider capture. `go test ./...` covers existing business situations.

The comparison is current observations of local and fake providers, without providing any snapshot of any historical time points. `cutoff_at` is the watermark for this comparison; `local_observed_at` and `provider_observed_at` respectively record the actual reading time. The outbox to be processed will appear in the difference; Currently, there is no age limit, so it can indicate normal line work, and operators must run according to the original process or specify repairs. This does not connect to the real payment system, nor does it replace financial accounts reconciliation.
