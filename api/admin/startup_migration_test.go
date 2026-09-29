package admin

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"billforge/lab"
)

func TestAdminStartupFailsClosedWhenSchemaUpgradeFails(t *testing.T) {
	dir := t.TempDir()
	commercePath := filepath.Join(dir, "commerce.db")
	l, err := lab.Open(commercePath, filepath.Join(dir, "provider.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	db, err := sql.Open("sqlite3", commercePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE VIEW admin_commands AS SELECT 1 AS id`); err != nil {
		t.Fatal(err)
	}

	cfg := Config{Username: "admin", Password: "valid-admin-password"}
	handler, err := New(l, cfg)
	if err == nil || !strings.Contains(err.Error(), "admin_commands") {
		t.Fatalf("startup error = %v; want schema blocker", err)
	}
	if handler != nil {
		t.Fatal("failed schema upgrade exposed an HTTP handler")
	}
	var ledgerCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name='admin_schema_versions'`).Scan(&ledgerCount); err != nil {
		t.Fatal(err)
	}
	if ledgerCount != 0 {
		t.Fatalf("failed startup left %d schema ledgers", ledgerCount)
	}

	ctx := context.Background()
	quote, err := l.CreateQuote(ctx, "business-write-after-failed-admin-startup", "basic")
	if err != nil {
		t.Fatalf("business quote after failed admin startup: %v", err)
	}
	if _, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "business-after-admin-failure-001"); err != nil {
		t.Fatalf("business acceptance after failed admin startup: %v", err)
	}

	if _, err := db.Exec(`DROP VIEW admin_commands`); err != nil {
		t.Fatal(err)
	}
	handler, err = New(l, cfg)
	if err != nil || handler == nil {
		t.Fatalf("startup after removing schema blocker: handler=%v err=%v", handler != nil, err)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/admin/api/session/csrf", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("healthy admin handler returned %d: %s", w.Code, w.Body.String())
	}
}
