package lab

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"testing"
	"time"
)

// A published price with a seat component must be quoteable for at least one
// seat. Component values that fit separately cannot overflow when combined.
func TestPublishedPricesRejectUnrepresentableMinimumUpfront(t *testing.T) {
	ctx := context.Background()
	l, _, _ := openTestLab(t)
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	from := fixedNow
	until := from.Add(30 * 24 * time.Hour)
	pro := ProPriceSpec{
		ID: "minimum-overflow-pro", Version: 99, FixedMinor: math.MaxInt64,
		SeatMinor: 1, IncludedTasks: 10, UsageRateNum: 1, UsageRateDen: 1,
		EffectiveFrom: from,
	}
	if _, err := l.PublishProPrice(ctx, pro); !errors.Is(err, ErrConflict) {
		t.Fatalf("unquoteable Pro price published: %v", err)
	}
	proPayload, _ := json.Marshal(adminProPricePayload{
		ID: pro.ID, Version: "99", FixedMinor: "9223372036854775807", SeatMinor: "1",
		IncludedTasks: "10", UsageRateNum: "1", UsageRateDen: "1",
		EffectiveFrom: from.Format(time.RFC3339Nano),
	})
	if _, err := l.AdminCreatePreview(ctx, "local-admin", "C18", "", proPayload); !errors.Is(err, ErrAdminInvalidCommand) {
		t.Fatalf("unquoteable Pro price preview accepted: %v", err)
	}

	metered := MeteredPriceSpec{
		ID: "minimum_overflow_metered", PlanID: "ai", Version: 99,
		FixedMinor: math.MaxInt64, SeatMinor: 1, MeterID: "tasks",
		IncludedQuantity: 10, UsageRateNum: 1, UsageRateDen: 1,
		EffectiveFrom: from,
	}
	if _, err := l.PublishMeteredPrice(ctx, metered); !errors.Is(err, ErrConflict) {
		t.Fatalf("unquoteable metered price published: %v", err)
	}
	meteredPayload, _ := json.Marshal(adminMeteredPricePayload{
		ID: metered.ID, PlanID: metered.PlanID, Version: "99",
		FixedMinor: "9223372036854775807", SeatMinor: "1", MeterID: "tasks",
		IncludedQuantity: "10", UsageRateNum: "1", UsageRateDen: "1",
		EffectiveFrom: from.Format(time.RFC3339Nano),
	})
	if _, err := l.AdminCreatePreview(ctx, "local-admin", "C20", "", meteredPayload); !errors.Is(err, ErrAdminInvalidCommand) {
		t.Fatalf("unquoteable metered price preview accepted: %v", err)
	}

	contract := ContractSpec{
		ID: "minimum-overflow-contract", CustomerID: "minimum-overflow-customer",
		Version: 1, BasePriceVersionID: "pro-v1", FixedMinor: math.MaxInt64,
		SeatMinor: 1, EffectiveFrom: from, EffectiveTo: until,
	}
	if _, err := l.PublishContract(ctx, contract); !errors.Is(err, ErrConflict) {
		t.Fatalf("unquoteable contract published: %v", err)
	}
	contractPayload, _ := json.Marshal(adminPublishContractPayload{
		ID: contract.ID, CustomerID: contract.CustomerID, Version: "1",
		BasePriceVersionID: contract.BasePriceVersionID,
		FixedMinor:         "9223372036854775807", SeatMinor: "1",
		EffectiveFrom: from.Format(time.RFC3339Nano),
		EffectiveTo:   until.Format(time.RFC3339Nano),
	})
	if _, err := l.AdminCreatePreview(ctx, "local-admin", "C31", "", contractPayload); !errors.Is(err, ErrAdminInvalidCommand) {
		t.Fatalf("unquoteable contract preview accepted: %v", err)
	}

	var versions, contracts int
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM price_versions WHERE id IN (?,?)`, pro.ID, metered.ID).Scan(&versions); err != nil {
		t.Fatal(err)
	}
	if err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM contract_versions WHERE id=?`, contract.ID).Scan(&contracts); err != nil {
		t.Fatal(err)
	}
	if versions != 0 || contracts != 0 {
		t.Fatalf("rejected price facts persisted: versions=%d contracts=%d", versions, contracts)
	}

	pro.ID, pro.Version, pro.FixedMinor = "minimum-boundary-pro", 100, math.MaxInt64-1
	proTerms, err := l.PublishProPrice(ctx, pro)
	if err != nil {
		t.Fatalf("representable Pro boundary rejected: %v", err)
	}
	if amount, err := proTerms.UpfrontMinor(1); err != nil || amount != math.MaxInt64 {
		t.Fatalf("Pro one-seat boundary: amount=%d err=%v", amount, err)
	}
	metered.ID, metered.Version, metered.FixedMinor = "minimum_boundary_metered", 100, math.MaxInt64-1
	meteredTerms, err := l.PublishMeteredPrice(ctx, metered)
	if err != nil {
		t.Fatalf("representable metered boundary rejected: %v", err)
	}
	if amount, err := meteredTerms.UpfrontMinor(1); err != nil || amount != math.MaxInt64 {
		t.Fatalf("metered one-seat boundary: amount=%d err=%v", amount, err)
	}
	contract.ID, contract.FixedMinor, contract.EffectiveTo = "minimum-boundary-contract", math.MaxInt64-1, from.Add(60*24*time.Hour)
	if _, err := l.PublishContract(ctx, contract); err != nil {
		t.Fatalf("representable contract boundary rejected: %v", err)
	}
	quote, err := l.CreateContractQuote(ctx, contract.CustomerID, contract.ID, 1)
	if err != nil || quote.AmountMinor != math.MaxInt64 {
		t.Fatalf("contract one-seat boundary: %+v err=%v", quote, err)
	}
}
