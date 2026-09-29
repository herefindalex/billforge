# Phase F3: Scheduled Plan Changes

**English** | [繁體中文](phase-f3-schedules.zh-TW.md) | [简体中文](phase-f3-schedules.zh-CN.md)


The date is 2026-09-26. This slice completes the late upgrade of the S06, the cancellation and pre-cancellation recovery of the S06, and creates the basis for a shared price component for subsequent usage/price migration.

## The Landing Act

- The built-in Pro v1 is fixed at $50 per period plus $10 per paid seat; The five prepaid bills are $100, `fixed` and `seats` respectively becoming the unchanging invoice line. Basic maintains $20.. Pro v1 simultaneously records 20,000 tasks allowance and postpaid components of $0.001 per task, but this piece** has not yet been invoiced for use**.
- `PriceVersion` has `draft/published` status and effective range; Released versions and their components are blocked by a SQLite trigger. It offers a fixed version, seats, amount and fingerprint. When the old database is opened, fill in the fields with the initial assignment; The original Basic subscription amount and payment identity remained unchanged.
- `ScheduleNextPlan` and `ScheduleCancel` use the expected subscription revision, stable request key, and single scheduled constraints to be effective. The next price version is selected at the time of scheduling; The timely plan has not been changed in advance. The cancellation of the same border and changes in the program are mutually exclusive.
- `ResumeCancel` is established only before the cancellation takes effect and retains the cancelled and restored audits. `RunRenewals` closes the original pricing assignment in the same transaction, opens a new assignment, validates new price invoices, or transfers `subscription_ends` without generating renewal invoices. We're not going to get a second one.
- After the subscription is over, look for `ended`, and the entitlement rebuild shows `expired/cancel_at`. The subscription is terminated and no longer renewable; The new purchase is another new subscription.

## Acceptance of evidence

- `TestProFiveSeatsKeepsVersionAndLinesAcrossRenewal`: $50+$10×5=$100, renewal is still $100; The prices and components have been published and cannot be changed.
- `TestS06NextPeriodChangeUsesRevisionAndAssignmentHistory`: The old Basic is up to date; 10-01 changed the Pro's five seats, renewed the contract for $100, and ran unrepeatedly. Results of reruns/different payloads/old revision/competition schedules are fixed.
- `TestS06CancelResumeAndBoundaryExpiry`: Canceling conflicts with upgrades, resuming before cancelling, rescheduling cancellations, no extension of deadlines, expiration of entitlement and refusal to resume after the end.
- `TestMenuSchedulesProAtNextBoundary`: Menu to complete Basic purchases, drop-off Pro, advance clock and renewals. The old E schema upgrade test and all existing scenarios remained valid.

At this stage, there is no intermediate proration for S07, no per-hour version migration for S08, no supply shutdown for S09, or no S10 contract. The price components and assignments provide a source for them, but the data table alone cannot declare those situations completed.
