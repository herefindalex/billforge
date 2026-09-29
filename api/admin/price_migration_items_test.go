package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"

	"billforge/lab"
)

func TestPriceMigrationItemsAreBoundedAndScoped(t *testing.T) {
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
	for _, id := range []string{"large", "other"} {
		if _, err := db.Exec(`INSERT INTO price_migrations(id,cohort,target_price_version_id,payload_hash,status,created_at) VALUES(?, 'default', 'basic-v1', 'fixture', 'paused', 1)`, id); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 123; i++ {
		status := "applied"
		if i%3 == 0 {
			status = "conflicted"
		}
		_, err := db.Exec(`INSERT INTO price_migration_items(migration_id,subscription_id,from_price_version_id,target_price_version_id,seat_quantity,expected_revision,prior_amount_minor,target_amount_minor,current_entitlement_status,projected_entitlement_rule,effective_at,status,conflict_reason)
			VALUES('large',?,'basic-v1','basic-v1',1,1,9007199254740993,9007199254740995,'active','pro',1,?,'')`, fmt.Sprintf("sub-%03d", i), status)
		if err != nil {
			t.Fatal(err)
		}
	}
	s := &Server{lab: l}
	request := func(id, query string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "/admin/api/price-migrations/"+id+"/items"+query, nil)
		r.SetPathValue("id", id)
		w := httptest.NewRecorder()
		s.migrationItems(w, r)
		return w
	}
	decode := func(w *httptest.ResponseRecorder) struct {
		Items []map[string]any `json:"items"`
		Next  string           `json:"next_cursor"`
		At    string           `json:"observed_at"`
	} {
		t.Helper()
		if w.Code != http.StatusOK {
			t.Fatalf("unexpected status %d: %s", w.Code, w.Body.String())
		}
		var page struct {
			Items []map[string]any `json:"items"`
			Next  string           `json:"next_cursor"`
			At    string           `json:"observed_at"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		return page
	}
	first := decode(request("large", "?limit=50"))
	if len(first.Items) != 50 || first.Next == "" || first.At == "" || first.Items[0]["PriorAmountMinor"] != "9007199254740993" {
		t.Fatalf("bad first page: count=%d next=%q at=%q amount=%v", len(first.Items), first.Next, first.At, first.Items[0]["PriorAmountMinor"])
	}
	second := decode(request("large", "?limit=50&cursor="+url.QueryEscape(first.Next)))
	third := decode(request("large", "?limit=50&cursor="+url.QueryEscape(second.Next)))
	if len(second.Items) != 50 || len(third.Items) != 23 || third.Next != "" || first.Items[49]["SubscriptionID"] != "sub-049" || second.Items[0]["SubscriptionID"] != "sub-050" {
		t.Fatalf("bad keyset pages: %d/%d, second first=%v", len(second.Items), len(third.Items), second.Items[0]["SubscriptionID"])
	}
	filtered := decode(request("large", "?limit=20&status=conflicted"))
	if len(filtered.Items) != 20 || filtered.Next == "" {
		t.Fatalf("bad filtered page: %d %q", len(filtered.Items), filtered.Next)
	}
	if w := request("large", "?status=applied&cursor="+url.QueryEscape(filtered.Next)); w.Code != http.StatusBadRequest {
		t.Fatalf("cursor accepted under another filter: %d", w.Code)
	}
	if w := request("other", "?status=conflicted&cursor="+url.QueryEscape(filtered.Next)); w.Code != http.StatusBadRequest {
		t.Fatalf("cursor accepted under another migration: %d", w.Code)
	}
	for _, query := range []string{"?limit=101", "?status=unknown", "?cursor=invalid", "?limit=1&limit=2"} {
		if w := request("large", query); w.Code != http.StatusBadRequest {
			t.Fatalf("%q: status %d", query, w.Code)
		}
	}
	if w := request("missing", ""); w.Code != http.StatusNotFound {
		t.Fatalf("missing migration: status %d", w.Code)
	}
	detailRequest := httptest.NewRequest(http.MethodGet, "/admin/api/price-migrations/large", nil)
	detailRequest.SetPathValue("id", "large")
	detail := httptest.NewRecorder()
	s.migrationDetail(detail, detailRequest)
	var summary map[string]any
	if detail.Code != http.StatusOK || json.Unmarshal(detail.Body.Bytes(), &summary) != nil || summary["ItemCount"] != "123" || summary["ConflictedCount"] != "41" || summary["Items"] != nil {
		t.Fatalf("bad bounded detail: %d %s", detail.Code, detail.Body.String())
	}
	if _, err := db.Exec(`UPDATE price_migration_items SET status='skipped' WHERE migration_id='large' AND subscription_id='sub-060'`); err != nil {
		t.Fatal(err)
	}
	changed := decode(request("large", "?limit=20&status=conflicted&cursor="+url.QueryEscape(filtered.Next)))
	if len(changed.Items) != 20 || changed.Items[0]["SubscriptionID"] != "sub-063" {
		t.Fatalf("status change was not reflected in next page: %v", changed.Items)
	}
	detail = httptest.NewRecorder()
	s.migrationDetail(detail, detailRequest)
	if detail.Code != http.StatusOK || json.Unmarshal(detail.Body.Bytes(), &summary) != nil || summary["ConflictedCount"] != "40" || summary["SkippedCount"] != "1" {
		t.Fatalf("stale status counts: %d %s", detail.Code, detail.Body.String())
	}
	if _, err := db.Exec(`DROP TABLE price_migration_items`); err != nil {
		t.Fatal(err)
	}
	if w := request("large", ""); w.Code != http.StatusInternalServerError {
		t.Fatalf("database failure: status %d", w.Code)
	}
}
