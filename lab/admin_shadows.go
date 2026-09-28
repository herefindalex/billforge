package lab

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
)

type adminShadowQuotePayload struct {
	PlanID            string `json:"plan_id"`
	Seats             string `json:"seats"`
	LegacyAmountMinor string `json:"legacy_amount_minor"`
	LegacyCurrency    string `json:"legacy_currency"`
}

type adminShadowEntitlementPayload struct {
	SubscriptionID string `json:"subscription_id"`
	LegacyStatus   string `json:"legacy_status"`
}

func canonicalAdminShadowPayload(actionID string, raw json.RawMessage) ([]byte, error) {
	if actionID == "C37" {
		var p adminShadowQuotePayload
		if err := decodeAdminPayload(raw, &p); err != nil {
			return nil, err
		}
		if p.PlanID == "" || p.LegacyCurrency == "" {
			return nil, ErrAdminInvalidCommand
		}
		for _, value := range []*string{&p.Seats, &p.LegacyAmountMinor} {
			n, err := strconv.ParseInt(*value, 10, 64)
			if err != nil || n < 0 {
				return nil, ErrAdminInvalidCommand
			}
			*value = strconv.FormatInt(n, 10)
		}
		return json.Marshal(p)
	}
	var p adminShadowEntitlementPayload
	if err := decodeAdminPayload(raw, &p); err != nil {
		return nil, err
	}
	if p.SubscriptionID == "" || p.LegacyStatus == "" {
		return nil, ErrAdminInvalidCommand
	}
	return json.Marshal(p)
}

func (l *Lab) executeAdminShadowTx(ctx context.Context, tx *sql.Tx, actionID, accountID string, canonical []byte) (any, error) {
	if actionID == "C37" {
		var p adminShadowQuotePayload
		if err := json.Unmarshal(canonical, &p); err != nil {
			return nil, err
		}
		seats, _ := strconv.ParseInt(p.Seats, 10, 64)
		amount, _ := strconv.ParseInt(p.LegacyAmountMinor, 10, 64)
		shadow, err := l.shadowQuoteTx(ctx, tx, accountID, p.PlanID, seats, amount, p.LegacyCurrency)
		if err != nil {
			return nil, err
		}
		return map[string]any{"shadow_id": shadow.ID, "matched": shadow.Matched, "expected": shadow.Expected, "actual": shadow.Actual}, nil
	}
	var p adminShadowEntitlementPayload
	if err := json.Unmarshal(canonical, &p); err != nil {
		return nil, err
	}
	shadow, err := l.shadowEntitlementTx(ctx, tx, accountID, p.SubscriptionID, p.LegacyStatus)
	if err != nil {
		return nil, err
	}
	return map[string]any{"shadow_id": shadow.ID, "matched": shadow.Matched, "expected": shadow.Expected, "actual": shadow.Actual}, nil
}
