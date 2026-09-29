# Phase P1 (P01): Metered SKUs

**English** | [繁體中文](phase-p1-metered-sku.zh-TW.md) | [简体中文](phase-p1-metered-sku.zh-CN.md)


## Delivery routes

`meter_schemas` records the meter ID, source of the event, unit and schema version. `RegisterMeter` is set to remain unchanged for the same ID; The source of the incident corresponds to the `source` at the time of recording. The `tasks` meter uses the `*` source to match the old data.

`PublishMeteredPrice` Acceptance Scheme, Price Version, Fixed Fee, Selectable Seat Fee, Meters, Inner Content, Extra-Precise Single Price and Effective Duration Confirm that the meter is registered at the time of publication and submit all components together with the unchanging checksum. `SelectCatalogPrice` will assign the published version to the cohort. Buy using the original Quote, AcceptQuote, payment and entitlement paths; Accounts, recalculations, and renewals are evaluated and written to the project based on the price version of the meter.

The interactive menu "price version and cohort migration" can register the meter, publish the measured price, specify the cohort; "Status search" shows the meter, the internal content and the excess rate; "Quantity and billing" is a subscription-based, valid price for the meter.

## amount of oracle

An example of an AI program: Fixed 3000 cents per period, with 100 `ai_tokens` in it, extra 1 cent for every 5 tokens. The excess of 105 tokens is 5 and the closing result is 1 cent. The renewal invoice includes a fixed fee of 3000 cents and `usage:ai_tokens:period:0` 1 cents, totalling 3001 cents. In both cases, the return of 20,003→0 cents, 20,010→1 cents is retained.

## The border

A price version currently supports a volume meter; Additional models are needed to combine meters, ladder prices, prepaid inventory, taxes and multi-currency. The consumption event must match the price version of the subscription and the meter at the time of the event, and the source is in accordance with meter registration information. The new SKU entitlement still uses the existing subscription entitlement projection; Each SKU's feature set and independent entitlement policy version have not yet been modeled. `MeteredPriceSpec` is an intrinsically managed input, not an external self-publishing API.

Acceptance: `go test ./lab -run TestP01 -count=1` covers the price of the broadcast and the invariance, the source and the meter, the rejection, the weighting of the event, the accuracy of the calculation and the source of the invoice; `go test ./...` maintains the return of the existing cases.
