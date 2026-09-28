package lab

import (
	"context"
	"encoding/json"
	"testing"
)

func TestAdminMoneyPayloadKeepsLargeIntegerExactAndRejectsUnsafeForms(t *testing.T) {
	for _, actionID := range []string{"C07", "C15"} {
		for name, amount := range map[string]string{
			"above-js-safe": "9007199254740993",
			"int64-max":     "9223372036854775807",
		} {
			t.Run(actionID+"/"+name, func(t *testing.T) {
				canonical, err := canonicalAdminPayload(actionID, json.RawMessage(`{"amount_minor":"`+amount+`"}`))
				if err != nil {
					t.Fatal(err)
				}
				if string(canonical) != `{"amount_minor":"`+amount+`"}` {
					t.Fatalf("large integer changed: %s", canonical)
				}
			})
		}
		for name, payload := range map[string]string{
			"zero":          `{"amount_minor":"0"}`,
			"negative":      `{"amount_minor":"-1"}`,
			"overflow":      `{"amount_minor":"9223372036854775808"}`,
			"decimal":       `{"amount_minor":"1.5"}`,
			"exponent":      `{"amount_minor":"1e3"}`,
			"json-number":   `{"amount_minor":9007199254740993}`,
			"unknown-field": `{"amount_minor":"1","currency":"USD"}`,
		} {
			t.Run(actionID+"/"+name, func(t *testing.T) {
				if canonical, err := canonicalAdminPayload(actionID, json.RawMessage(payload)); err == nil {
					t.Fatalf("unsafe money payload accepted: %s", canonical)
				}
			})
		}
	}
}

func TestAdminReconciliationCutoffRequiresValidUTC(t *testing.T) {
	for name, payload := range map[string]string{
		"offset":       `{"as_of":"2026-09-26T12:00:00+01:00"}`,
		"invalid-date": `{"as_of":"2026-02-30T12:00:00Z"}`,
		"numeric":      `{"as_of":1790424000}`,
		"missing":      `{}`,
	} {
		t.Run(name, func(t *testing.T) {
			if canonical, err := canonicalAdminPayload("C33", json.RawMessage(payload)); err == nil {
				t.Fatalf("invalid UTC cutoff accepted: %s", canonical)
			}
		})
	}
}

func TestAdminUTCRejectsPrecisionLossAndUnixNanoOverflow(t *testing.T) {
	for _, value := range []string{
		"2026-09-26T12:00:00.1234567891Z", // Go time.Parse otherwise truncates this digit.
		"2262-04-11T23:47:16.854775808Z",  // One nanosecond above int64 maximum.
		"1677-09-21T00:12:43.145224191Z",  // One nanosecond below int64 minimum.
		"9999-12-31T23:59:59Z",
	} {
		t.Run(value, func(t *testing.T) {
			if _, err := canonicalCatalogUTC(value); err == nil {
				t.Fatal("catalog time silently accepted unrepresentable timestamp")
			}
			for _, action := range []struct {
				id      string
				payload string
			}{
				{"C26", `{"source":"api","event_id":"precision","subscription_id":"sub","meter_id":"meter","occurred_at":"` + value + `","quantity":"1"}`},
				{"C28", `{"period_index":"0","cutoff":"` + value + `"}`},
				{"C31", `{"id":"contract","customer_id":"customer","version":"1","base_price_version_id":"pro-v1","fixed_minor":"1","seat_minor":"1","effective_from":"2025-01-01T00:00:00Z","effective_to":"` + value + `"}`},
				{"C33", `{"as_of":"` + value + `"}`},
				{"C46", `{"mode":"fixed","value_utc":"` + value + `"}`},
			} {
				if _, err := canonicalAdminPayload(action.id, json.RawMessage(action.payload)); err == nil {
					t.Fatalf("%s accepted unrepresentable timestamp", action.id)
				}
			}
		})
	}
	for _, value := range []string{
		"2026-09-26T12:00:00Z",
		"2026-09-26T12:00:00.123456789Z",
		"2262-04-11T23:47:16.854775807Z",
		"1677-09-21T00:12:43.145224192Z",
	} {
		if _, err := canonicalCatalogUTC(value); err != nil {
			t.Fatalf("valid nanosecond boundary %s: %v", value, err)
		}
	}
}

func TestAdminRateDenominatorUsesExactPositiveInt64(t *testing.T) {
	payload := func(denominator string) json.RawMessage {
		return json.RawMessage(`{"id":"rate-boundary","version":"1","fixed_minor":"100","seat_minor":"1","included_tasks":"0","usage_rate_num":"1","usage_rate_den":"` + denominator + `","effective_from":"2026-09-26T12:00:00Z"}`)
	}
	for _, invalid := range []string{"0", "-1", "1e3", "9223372036854775808"} {
		if _, err := canonicalAdminPayload("C18", payload(invalid)); err == nil {
			t.Fatalf("denominator %q was accepted", invalid)
		}
	}
	for _, valid := range []string{"1", "9007199254740993", "9223372036854775807"} {
		canonical, err := canonicalAdminPayload("C18", payload(valid))
		if err != nil {
			t.Fatalf("denominator %q: %v", valid, err)
		}
		var decoded struct {
			UsageRateDen string `json:"usage_rate_den"`
		}
		if err := json.Unmarshal(canonical, &decoded); err != nil || decoded.UsageRateDen != valid {
			t.Fatalf("denominator changed: %q, %v", decoded.UsageRateDen, err)
		}
	}
}

func TestAdminRateDenominatorPersistsWithoutPrecisionLoss(t *testing.T) {
	ctx := context.Background()
	l, _, _ := openTestLab(t)
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	const denominator = int64(9007199254740993)
	payload := json.RawMessage(`{"id":"pro-den-precision","version":"99","fixed_minor":"100","seat_minor":"1","included_tasks":"0","usage_rate_num":"1","usage_rate_den":"9007199254740993","effective_from":"2026-08-31T23:00:00Z"}`)
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C18", "", payload)
	if err != nil {
		t.Fatal(err)
	}
	var impact map[string]string
	if err := json.Unmarshal(preview.Impact, &impact); err != nil || impact["usage_rate_den"] != "9007199254740993" {
		t.Fatalf("preview denominator: %q, %v", impact["usage_rate_den"], err)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "pro-den-precision-001", "C18", "", payload, preview.ID)
	if err != nil {
		t.Fatal(err)
	}
	command, err = l.AdminExecuteCommand(ctx, command.ID)
	if err != nil || command.Status != "succeeded" {
		t.Fatalf("publish price: %+v, %v", command, err)
	}
	terms, err := loadPriceTerms(ctx, l.db, "pro-den-precision")
	if err != nil || terms.UsageRateDen != denominator {
		t.Fatalf("stored denominator: %+v, %v", terms, err)
	}
}
