# Phase F5: Price Migrations

**English** | [繁體中文](phase-f5-price-migrations.zh-TW.md) | [简体中文](phase-f5-price-migrations.zh-CN.md)


## Prices released and new purchases

`PublishProPrice` Creates draft, writes down fixed fees, seat fees, task allowances and excess rate components, and releases atoms. Released versions and components are prohibited from being modified by a database trigger; The same payload will be returned to the original version, and different payloads will conflict with the same ID. `SelectCatalogPrice` to create a cohort selection at the specified time of entry into force; Selection is not overwritten. `CreateQuoteForCohort` offers only published and in force versions from this cohort; The original subscription remains the original PriceVersion.

S08 oracle uses Pro v1 fixed $50+ five seats $50=$100, Pro v2 fixed $60+ five seats $50=$110 The new offer for cohort A is $110; The default is still $100.

## Migration and Troubleshooting

`PreviewPriceMigration` has established the status of the existing entitlement and the rules for each showcase of the original/targeted version, seat, upcoming original/new price, and "the same scheme, renewal of payment determines entitlement". `PlanPriceMigration` constitutes a stable batch with a cohort, targeted version and subscription list after sorting; Each user keeps the original version, revision, price snapshot and next edition boundaries. Changes have been scheduled, upgrades in the unfinished period are mutually exclusive with other unfinished price migrations.

Renew the deadline in the same transaction with revision and the original version CAS, close v1 assignment, open v2 assignment, create v2 invoice. I can't run again. If the status of a household is not consistent with the preview snapshot, the household is labelled `conflicted` and the batch is suspended; Other non-processed customers suspended their renewal. Operators can view the differences, slightly overcome the conflicting user or restore the batch while the user is still undergoing processing. Some customers have kept their old prices. Returns with new reverse migrations and assignments; Not modified validated invoices or old price versions.

CLI "price version and cohort migration" provides release, selection, preview, build batch, pause, shorten, restore; "State search" is read price, cohort with the status of the individual migration. The verification covers two $110 renewals, a deadline overrun, a portion of the first batch that has been successful in the second batch conflict, a clear summary of past price renewals, and a reverse migration to v1 for the next batch.

Restrictions: The release entry for this clip only supports existing Pro component syntax and tasks meter; Additional components of SKU's AI tokens and API compatibility are still processed by P01/P02. This is a local single writer lab, with migration suspended to explicit operating status, not simulated cross-regional worker coordination.
