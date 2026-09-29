# A. Price Versions and Business Policies

**English** | [繁體中文](01-pricing-and-policies.zh-TW.md) | [简体中文](01-pricing-and-policies.zh-CN.md)


Status: Billforge lab design decision. The input is [The overall programme](../commerce-lab-plan.md) and 34 studies; These rules are the lab's choice, and do not claim that all external products follow the same policy. All time in UTC,[start, end] indicates that the start point is included, not the end point.

## Responsibilities of PriceVersion

Product responds with "what to sell"; Plan responded with "long-term identification codes like Basic or Pro". **PriceVersion answers how to charge for a published version**** It is operational rule data, interpreted by pricing engines. The economic content of the published version is unchanged.

The smallest structure is as follows; The field name is a concept contract, not a SQL schema:

|Fields|Types/examples|The rules|
| --- | --- | --- |
| `id`, `plan_id`, `version` | `pro-v2`, `pro`, `2` | `id` is globally unique. (`plan_id`, `version`) is unique and increases monotonically. Do not rename a plan to represent a price change. |
| publication_state | draft／published |draft can be repaired; Economic payload cannot be changed after publication. Stop selling with alternative audited availability event.  |
| `effective_from`, `effective_to` | `2026-10-01T00:00Z`, nullable | Defines the selection interval for new subscriptions or explicit reassignment only. It does not migrate existing subscriptions automatically. |
| currency、billing_cadence | USD、calendar_month |This lab is only USD/month. The billing period is focused on the Subscription, not all customers share the PriceVersion field.  |
| charge_definitions |In the table below, the version of the intrinsic object|Use a stable component_code to save the measurement type, unit, numerical value, time point, and necessary meter.  |
| rounding_policy_ref | component-period-segment／HALF_EVEN |In the first case, the value of the aggregate will be calculated by multiplying the value by cents, and then the value of the aggregate will be reduced by cents. I can't wait to see what happens next.  |
| proration_policy_ref | elapsed_utc_seconds／HALF_EVEN |The changes in pricing need to be revised by version to reflect the policy at the time; It is also clear that the components that have not changed during the period are marked.  |
| `supersedes_id`, `published_at`, `checksum` | `pro-v1`, publication time, payload hash | Support provenance and quote validation. A checksum does not replace database immutability constraints. |

ChargeDefinition contains at least component_code, kind, unit, rate/amount, charge_timing, aggregation_policy and whether it can be calculated proportionally. Subject to category:

| Kind |This is a list of the most commonly used methods of calculating the value of a currency.|Calculation|
| --- | --- | --- |
| FixedCharge | amount_minor=5000、UPFRONT |Fifty dollars fixed during the period.  |
| PerSeatCharge | unit_amount_minor=1000、quantity_min=1、unit=seat、UPFRONT |The service area has been promised paid seats.  |
| IncludedQuantity | meter_id=tasks、quantity=20000、unit=task |Free billing allowance of 20,000 tasks at the time; It's not a service stop limit.  |
| UsageOverage | meter_id=tasks、unit_rate_decimal=`0.001`、ARREARS |max(during tasks − allowance, 0) × accurate single price, only paid after the merger.  |

Each `charge_definitions` project also requires the only `component_code`, `kind`, `unit`, `charge_timing` in the version; Useful projects are required to specify `meter_id`. The amount field is kept in cents as an integer, the fixed/seat fee is kept in cents, the extra single price is kept in ten-digit strings, and binary float is prohibited. `IncludedQuantity` is the quoted allowance, separated from the entitlement quota. Legal combinations of fields should be checked at the time of publication, for example `UsageOverage` must refer to the allowance definition of the same version, the same meter; If you don't, you're not going to publish.

Pro v2 is available for FixedCharge $50; PerSeatCharge $10; IncludedQuantity 20,000 tasks; UsageOverage $0.001/task. Basic v1 is only $20 for FixedCharge. Component codes such as base, seat, tasks_included, tasks_overage must be unique in the version, and invoices can refer to the original code and version ID.

** Data not included in the PriceVersion**: customer seating and event size, specific customer discounts or contracts, current payment status, entitlement open, actual billing period focal points, decision to move a customer. These are the changing commercial facts and other policy versions. EntitlementPolicyVersion Independent, Subscription Pricing Assignment also refers to both the pricing version and the entitlement policy version; Enterprise ContractVersion can be written and paid in the permitted components.

Quote Save customer, subscription revision, selected PriceVersion ID/checksum, ContractVersion, seat quantity, effective_at, component by component, future usage rates, rounding policy, expiration time and fingerprint. InvoiceLine retrieves the components, duration, quantity, accuracy, value, amount and source of the invoice. Quote is a deadline preview; The fact that the invoice is validated constitutes an immutable financial fact.

### Price analysis and release version

1. Customers and APIs point to plan, not self-calculation. PriceVersion is available for new directories to buy time for. There is also a subscription to read your chosen assignment. Any cohort experiment uses a clear eligibility/assignment record.
`effective_from`/`effective_to` is the version of the candidate zone, and not the only missing condition. Additional `CatalogSelection` switching events (plan、cohort、`effective_at`、price_version_id、 publisher and time) specify the default: each scope takes the last event that is in effect at some point until the next event; The scope+`effective_at` is not allowed for two targets. Release v2 only adds switching events and does not modify v1. The resulting Quote is still acceptable for a short period of time, provided that the version is still in the candidate zone, there is no explicit discontinuation of the sale event, and the revision/fingerprint subscription remains unchanged; It is not possible to change the new defects in acceptance. Old subscriptions are still priced according to the original assignment.
2. If there is a ContractVersion, verify the validity of the component_code, currency, payment terms and customer identity. The contract was rejected when the necessary components were missing, and was not returned to the current catalog.
3. Using component definitions, the quantity and measurement of customer commitments, the actual cost. A missing meter, missing component, or multiple conflicts are all rejected.
4. Check the timing, context fingerprint and subscription revisions when you receive the Quote; Payment operations using frozen amounts and a stable operation ID. The invoice is validated to retain the same source and intermediate value.
5. V3 is available only for new purchases. The old client retains v2; Migrations must have preview, cohort, effective_at, subscription-based identity and stop loss capabilities.

Release verification: no negative value, no repeat component_code, meter with unit correspondence, included quantity non-negative, allowance of the same meter unambiguous, accurate decimal within system limits. For the default new purchase version of the same plan, the time window cannot produce two unprioritized candidates; The experimental cohort must be explicitly selected. A new availability event is recorded if a version has been released before the sale can be stopped; It doesn't change the payload at the original price and the existing assignment.

This design is based on OpenMeter version is selected, Kill Bill comes into effect, Lotus's plan to migrate, Shopware rules in the context; The above practices are not the same in detail, so Billforge's parsing sequence and migration policy must be self-explanatory.

## List of policy decisions

| ID |The policy adopted by the lab|The main reasons and the deliberate costs.|
| --- | --- | --- |
| D01 |fixed fees and seat advances; Payment after dose.  |Quote: It is clear that the fees should be charged now and in the future. It is necessary to retain the billing period identity of the two charge groups.  |
| D02 | UTC monthly periods retain the original day-of-month anchor; shorter months end on their last day. Intervals are [start, end). | Renewals can be replayed; test boundaries even when months have different lengths. |
| D03 |The median price is decimal/ratio, and the half-even is reduced to cents after the "component x service area" is added.  |The final cents of the $0.001 task can be reconstructed; The cross-sectional sum may differ from the previous sum and must be a standard version.  |
| D04 |PriceVersion can be used for new purchases; Both customers are screwed up, and migration is clear.  |the speed and historical accuracy of the publication; It's important to maintain assignment and rollout.  |
| D05 |Conflicts with the same usage event identity, different payloads; The same result is returned with the same payload.  |The timestamp should be changed to prevent it from being retrieved. It is necessary to perpetuate the original fingerprint.  |
| D06 |The balance shall be returned to the original service period after the closing of the accounts, but shall be corrected by the difference in the next period; The original invoice is unchanged.  |At the same time, retain service attribution and historical accounts; It is necessary to recalculate the cumulative difference with the original PriceVersion.  |
| D07 |the new purchase of self-help payments is activated only after confirmation; In the meantime, there is a seven-day grace period. Net30 contracts are activated under these terms.  |Entitlement has an independent policy that shows the reasons and deadlines.  |
| D08 |The self-help upgrade payments during the period are valid until the validity of the scheme is confirmed. The receipt was successful, but the opening was delayed, and further corrections were made for non-delivered periods.  |Basic is sustainable when UNKNOWN; It is necessary to be able to deal with the difference in prices of prepaid and postpaid services.  |
| D09 |Net prolation using independent supplementary difference invoice, the old invoice unchanged; The negative lines have been corrected in the document, and no other credits can be made.  |Prevent double use of the same coupon.  |
| D10 |the correction of unpaid arrears before deduction; Only the part that has been received and can be issued can be credited with a source of funding. Refund Reserves and credits using a shared budget.  |The limitation on the retrieval of records shall be limited to the retrieval/retrieval; The total accounting is not declared.  |
| D11 |The same provider operation continues to use the same key and payload; Unknowledge is over-recorded and the original operation is checked.  |The key is to avoid remotely withdrawn funds and replace the key with the key. I can't imagine that failure is a failure.  |
| D12 |Contract validity/expiration aligns with the billing period boundary, with the expiry deadline automatically terminating the renewal and generating a manageable discrepancy.  |Avoid not knowing what price to pay after expiration; Initial support for any price segment during the contract period was not available.  |

### S07 mid-range upgrading decisions

Basic $20 has been collected for [09-01, 10-01]. 09-16 00:00 Z Accepted Quote for five Pro seats, remaining 15/30: Not using Basic −$10, five Pro seats +$50, supplemental difference invoice $40. The original Basic invoice is unchanged, and the two signed lines must retain the source. The payment operation for $40 is unknown, the valid program and entitlement are maintained by Basic, and the new key can no longer be used to deduct $40.

If the 09-18 00:00Z verifies that the provider has deducted $40, then Pro is activated. Basic service is 17 days, Pro service is 13 days. The actual supplementary difference is −$8.67+$43.33=$34.66. Keeping the original surcharge invoice at $40, and making a correction of -$5.34, the surcharge will be converted to the customer credit from the original payment source, and refunded if necessary. The price is not recalculated using the current price. If the provider confirms that there is no deduction at all, Basic will continue to serve with the correction of the elimination of the liability for the compensation gap; If you can't find the truth, keep UNKNOWN and upgrade the artificial censorship.

Business products may choose to open Pro first and then take the risk of bankruptcy; The lab chose the above policies in order to clearly project the source of UNKNOWN, compensation and funding. The price of the option is visible opening delays.
