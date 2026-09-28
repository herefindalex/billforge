package lab

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestQuotePreviewUsesRealTTLWithOlderFixedBusinessClock(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	l, err := Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}

	fixed := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	clockPayload, _ := json.Marshal(map[string]string{"mode": "fixed", "value_utc": fixed.Format(time.RFC3339Nano)})
	submitControl(t, l, "older-fixed-clock-001", "C46", "", clockPayload)
	create, _, err := l.AdminSubmitCommand(ctx, "local-admin", "older-clock-quote-001", "C01", "", json.RawMessage(`{"customer_id":"older-clock-customer","plan_id":"basic","cohort":"default","seats":"0"}`), "")
	if err != nil {
		t.Fatal(err)
	}
	create, err = l.AdminExecuteCommand(ctx, create.ID)
	if err != nil || create.Status != "succeeded" {
		t.Fatalf("quote command: %+v %v", create, err)
	}
	var refs map[string]string
	if err := json.Unmarshal(create.ResultRefs, &refs); err != nil {
		t.Fatal(err)
	}
	acceptPayload, _ := json.Marshal(map[string]string{"fingerprint": refs["fingerprint"]})
	started := time.Now().UTC()
	preview, err := l.AdminCreatePreview(ctx, "local-admin", "C02", refs["quote_id"], acceptPayload)
	if err != nil {
		t.Fatal(err)
	}
	expires, err := time.Parse(time.RFC3339Nano, preview.ExpiresAt)
	if err != nil {
		t.Fatal(err)
	}
	if !expires.After(started) || expires.After(started.Add(5*time.Minute+time.Second)) {
		t.Fatalf("real preview expiry %s outside five minute TTL from %s", expires, started)
	}
	if _, err := adminPreviewExpiry(started, fixed.Add(15*time.Minute), fixed.Add(15*time.Minute)); !errors.Is(err, ErrExpired) {
		t.Fatalf("business-expired quote should be rejected, got %v", err)
	}
}
