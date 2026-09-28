package lab

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
)

func TestAdminCommandsPageIsStableWhenNewCommandsArrive(t *testing.T) {
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
	payload := json.RawMessage(`{"customer_id":"page-customer","plan_id":"basic","cohort":"default","seats":"0"}`)
	ids := make([]string, 0, 6)
	for i := 0; i < 5; i++ {
		command, _, err := l.AdminSubmitCommand(ctx, "local-admin", fmt.Sprintf("page-command-%03d", i), "C01", "", payload, "")
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, command.ID)
	}
	first, cursor, err := l.AdminCommandsPage(ctx, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 || first[0].ID != ids[4] || first[1].ID != ids[3] || cursor <= 0 {
		t.Fatalf("first page=%+v cursor=%d", first, cursor)
	}
	newCommand, _, err := l.AdminSubmitCommand(ctx, "local-admin", "page-command-005", "C01", "", payload, "")
	if err != nil {
		t.Fatal(err)
	}
	second, next, err := l.AdminCommandsPage(ctx, cursor, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 2 || second[0].ID != ids[2] || second[1].ID != ids[1] || next <= 0 {
		t.Fatalf("second page=%+v cursor=%d", second, next)
	}
	third, last, err := l.AdminCommandsPage(ctx, next, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(third) != 1 || third[0].ID != ids[0] || last != 0 {
		t.Fatalf("third page=%+v cursor=%d", third, last)
	}
	fresh, _, err := l.AdminCommandsPage(ctx, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if fresh[0].ID != newCommand.ID {
		t.Fatalf("new command did not appear on first page: %+v", fresh)
	}
	if _, _, err := l.AdminCommandsPage(ctx, -1, 2); err == nil {
		t.Fatal("negative cursor accepted")
	}
}
