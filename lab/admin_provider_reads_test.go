package lab

import (
	"context"
	"errors"
	"testing"
)

func TestAdminProviderPagesKeepIndependentCursorAndExactAmounts(t *testing.T) {
	ctx := context.Background()
	l, _, _ := openTestLab(t)
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	const large = int64(9007199254740993)
	for _, item := range []struct {
		key    string
		amount int64
	}{
		{"capture-1", large},
		{"capture-2", 1000},
		{"capture-3", 500},
	} {
		if _, err := l.provider.Capture(ctx, item.key, item.amount, "USD"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := l.provider.Refund(ctx, "refund-1", "capture-2", 200, "USD"); err != nil {
		t.Fatal(err)
	}
	counts, _, err := l.AdminProviderCounts(ctx)
	if err != nil || counts.Captures != 3 || counts.Refunds != 1 {
		t.Fatalf("provider counts: %+v err=%v", counts, err)
	}
	first, cursor, _, err := l.AdminProviderOperationsPage(ctx, "captures", "", 0, 1)
	if err != nil || len(first) != 1 || first[0].ProviderKey != "capture-3" || cursor == 0 {
		t.Fatalf("first capture page: %+v cursor=%d err=%v", first, cursor, err)
	}
	if _, err := l.provider.Capture(ctx, "capture-4", 100, "USD"); err != nil {
		t.Fatal(err)
	}
	second, next, _, err := l.AdminProviderOperationsPage(ctx, "captures", "", cursor, 1)
	if err != nil || len(second) != 1 || second[0].ProviderKey != "capture-2" || next == 0 {
		t.Fatalf("new capture shifted old cursor: %+v cursor=%d err=%v", second, next, err)
	}
	third, _, _, err := l.AdminProviderOperationsPage(ctx, "captures", "", next, 1)
	if err != nil || len(third) != 1 || third[0].ProviderKey != "capture-1" || third[0].AmountMinor != large {
		t.Fatalf("large capture lost precision: %+v err=%v", third, err)
	}
	refunds, refundCursor, _, err := l.AdminProviderOperationsPage(ctx, "refunds", "succeeded", 0, 20)
	if err != nil || refundCursor != 0 || len(refunds) != 1 || refunds[0].SourceCaptureKey != "capture-2" || refunds[0].AmountMinor != 200 {
		t.Fatalf("refund page: %+v cursor=%d err=%v", refunds, refundCursor, err)
	}
	if _, _, _, err := l.AdminProviderOperationsPage(ctx, "refunds", "unknown", 0, 20); !errors.Is(err, ErrAdminInvalidCommand) {
		t.Fatalf("invalid status accepted: %v", err)
	}
	if _, _, _, err := l.AdminProviderOperationsPage(ctx, "other", "", 0, 20); !errors.Is(err, ErrAdminUnsupportedAction) {
		t.Fatalf("unknown provider table accepted: %v", err)
	}
	if err := l.provider.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := l.AdminProviderCounts(ctx); err == nil {
		t.Fatal("closed provider database reported healthy counts")
	}
}
