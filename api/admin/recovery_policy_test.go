package admin

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"billforge/lab"
)

func TestServerRestartRevokesUnstartedCommandUnderCurrentCapabilities(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	l, err := lab.Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	command, _, err := l.AdminSubmitCommand(ctx, "local-admin", "capability-restart-001", "C01", "", json.RawMessage(`{"customer_id":"capability-restart","plan_id":"basic","seats":"0"}`), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(l, Config{Username: "admin", Password: "long-enough-password", Capabilities: []string{"read"}}); err != nil {
		t.Fatal(err)
	}
	result, err := l.AdminCommand(ctx, command.ID)
	if err != nil || result.Status != "failed" || result.ErrorCode != "PERMISSION_REVOKED" {
		t.Fatalf("revoked after restart: %+v, %v", result, err)
	}
	if _, err := New(l, Config{Username: "admin", Password: "long-enough-password", Capabilities: []string{"read", "read"}}); err == nil {
		t.Fatal("duplicate capability must be rejected")
	}
}
