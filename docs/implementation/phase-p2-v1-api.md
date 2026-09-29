# Phase P2 (P02): v1 API

**English** | [繁體中文](phase-p2-v1-api.zh-TW.md) | [简体中文](phase-p2-v1-api.zh-CN.md)


The native API is started with `go run ./cmd/lab serve commerce.db provider.db [127.0.0.1:8080]` and only accepts explicit loopback addresses. `BILLFORGE_INTERNAL_TOKEN` sets up the internal operating credential. This is a native authentication input, without full user identity verification or external network deployment settings.

|The Covenant|What is the meaning of this?|
| --- | --- |
| `POST /v1/quotes` |publicly priced or contract offerings protected by internal credentials; Returns, seats, internal content and override rates, version checksum, now deals, future fixed commitments, validity and required client capabilities. If you specify a subscription, create a change offer that binds the subscription if you specify a mode with expected revision.  |
| `POST /v1/subscriptions` | Use quote ID, fingerprint, `Idempotency-Key`; check necessary client capabilities, then create subscriptions and payment operations. Contract to accept additional internal certification. |
| `POST /v1/subscriptions/{id}/changes` |accept only offers that are tied to original subscriptions and revisions; In the same database transaction that created the schedule/period changes, pin catalog selection patterns, seats and quote expiry.  |
| `GET /v1/subscriptions/{id}` |Displays actual prices, revisions, scheduling intentions, invoices, payments and revisions of entitlement sources; The entitlement projection shows pending when not followed. The internal credentials also display the provider key and the request key.  |
| `GET /v1/entitlements/{beneficiary}`、`GET /v1/invoices/{id}` |Check entitlement status and source, or verify invoice line, corrections and balances. This MVP will match the beneficiary to the customer ID, a feature for the general `service`.  |
| `POST /v1/usage-events` |Each event returns accepted/rejected independently; Verify the tenant, source, meter, time and event identity.  |
|In addition, the `POST /v1/contracts`, `/v1/migrations`, `/v1/repairs`, and the `/v1/repairs`, which is also available on the `POST /v1/contracts`, are also available on the `/v1/migrations`.|Internal credentials are required; Release contracts, preview or create migration batches, run specified variance fixes along the same service area.  |

The `recurring_committed_minor` is the full-term cost of the targeted program. The `due_now_minor` for the following period is 0; The mid-term upgrade `due_now_minor` is a proportional valuation calculated at the time of the bid, `due_now_estimated=true`. The actual amount of payment in the same transaction is recalculated when the changes are executed, and the `pending_amount_minor` in the response is the amount of the new payment obligation. The offer for the seats must be consistent with the operating seats.

v1 The client is known for `fixed`, `per_seat`, and the use of tasks. The new meter or Net30 contract will be added to the `required_client_capabilities` offer. When accepting an offer, it is necessary to declare competence through `X-Client-Capabilities`; `409 CLIENT_CAPABILITY_REQUIRED` has not been declared and no subscription has been created. The new meter rates are still presented in the original molecule/sequence and meter ID, not disguised as a fixed fee. Internal contract operations also require `X-Lab-Internal-Token`.

API contract testing covers Basic purchases with replay, pending entitlement after payment UNKNOWN, confirmation after opening, invoice and entitlement search, AI tokens, old client refusal to accept new clients, refusal to accept new clients, refusal to accept Net30's response times and capabilities, refusal to bind revisions to subscription changes, disagreement with operating seats, re-schedule of purchased or expired bids, refusal to accept changes to future target prices after an offer, grace and better reading.

Restrictions: To change the bid using the current selling price, if another valid price exists, the command will clarify the conflict and request a new bid. The API does not offer tax, full beneficiary/billing account separation, formal authorization, speed limitation or cross-service deployment. `as_of` is a clock observation value of the local clock; It is necessary to accompany the source revision to interpret the entitlement projection.
