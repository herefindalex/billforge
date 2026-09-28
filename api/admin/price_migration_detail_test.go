package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"billforge/lab"
)

func TestPriceMigrationDetailReadsOnlyRequestedMigration(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "commerce.db")
	l, err := lab.Open(dbPath, filepath.Join(dir, "provider.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	if err := l.InitAdmin(context.Background()); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`INSERT INTO price_migrations(id,cohort,target_price_version_id,payload_hash,status,created_at)
		VALUES('migration-detail','default','basic-v1','fixture','active',1)`); err != nil {
		t.Fatal(err)
	}
	// The detail read must not depend on an unrelated table used by the full
	// State export. This isolated fixture makes the former full-scan path fail.
	if _, err := db.Exec(`DROP TABLE usage_events`); err != nil {
		t.Fatal(err)
	}
	s := &Server{lab: l}
	for _, check := range []struct {
		id   string
		want int
	}{
		{"migration-detail", http.StatusOK},
		{"missing", http.StatusNotFound},
	} {
		r := httptest.NewRequest(http.MethodGet, "/admin/api/price-migrations/"+check.id, nil)
		r.SetPathValue("id", check.id)
		w := httptest.NewRecorder()
		s.migrationDetail(w, r)
		if w.Code != check.want {
			t.Fatalf("%s returned %d: %s", check.id, w.Code, w.Body.String())
		}
		if check.want == http.StatusOK {
			var migration map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &migration); err != nil {
				t.Fatal(err)
			}
			if migration["ID"] != check.id || migration["Status"] != "active" {
				t.Fatalf("wrong migration detail: %v", migration)
			}
		}
	}
}
