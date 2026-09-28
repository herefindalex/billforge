package lab

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestAdminLeaseGenerationFencesStaleWorker(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	commerce, provider := filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db")
	first, err := Open(commerce, provider, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if err := first.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	command, _, err := first.AdminSubmitCommand(ctx, "local-admin", "lease-fence-001", "C01", "", json.RawMessage(`{"customer_id":"lease-customer","plan_id":"basic","seats":"0"}`), "")
	if err != nil {
		t.Fatal(err)
	}
	firstGeneration, err := first.adminAcquireLease(ctx, command.ID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Open(commerce, provider, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if err := second.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := second.adminAcquireLease(ctx, command.ID); !errors.Is(err, ErrAdminLeaseBusy) {
		t.Fatalf("live lease was taken: %v", err)
	}
	if _, err := first.db.ExecContext(ctx, `UPDATE admin_commands SET lease_until=? WHERE id=?`, time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano), command.ID); err != nil {
		t.Fatal(err)
	}
	secondGeneration, err := second.adminAcquireLease(ctx, command.ID)
	if err != nil || secondGeneration <= firstGeneration {
		t.Fatalf("takeover generation %d %v", secondGeneration, err)
	}
	staleCtx := context.WithValue(ctx, adminLeaseContextKey{}, firstGeneration)
	if err := first.adminFailCommand(staleCtx, command.ID, "local-admin", "C01", "", "STALE"); !errors.Is(err, ErrAdminLeaseLost) {
		t.Fatalf("stale worker wrote result: %v", err)
	}
	currentCtx := context.WithValue(ctx, adminLeaseContextKey{}, secondGeneration)
	if err := second.adminFailCommand(currentCtx, command.ID, "local-admin", "C01", "", "TEST_FAILURE"); err != nil {
		t.Fatal(err)
	}
	result, err := second.AdminCommand(ctx, command.ID)
	if err != nil || result.Status != "failed" || result.ErrorCode != "TEST_FAILURE" {
		t.Fatalf("new worker result %+v %v", result, err)
	}
}

func TestAdminBusinessClockChangeDoesNotExpireWorkerLease(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	commerce, provider := filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db")
	first, err := Open(commerce, provider, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if err := first.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	command, _, err := first.AdminSubmitCommand(ctx, "local-admin", "business-clock-lease", "C01", "", json.RawMessage(`{"customer_id":"clock-lease-customer","plan_id":"basic","seats":"0"}`), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.adminAcquireLease(ctx, command.ID); err != nil {
		t.Fatal(err)
	}
	submitControl(t, first, "advance-business-clock", "C46", "", json.RawMessage(`{"mode":"fixed","value_utc":"2040-01-01T00:00:00Z"}`))
	second, err := Open(commerce, provider, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if err := second.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := second.adminAcquireLease(ctx, command.ID); !errors.Is(err, ErrAdminLeaseBusy) {
		t.Fatalf("business clock change invalidated live wall-clock lease: %v", err)
	}
}
