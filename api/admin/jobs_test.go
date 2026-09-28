package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"billforge/lab"
)

func TestJobsListHTTPBoundaryAndReadFailure(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	l, err := lab.Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	s := &Server{lab: l}
	for _, tc := range []struct {
		path   string
		status int
		code   string
	}{
		{"/admin/api/jobs?limit=1", http.StatusOK, ""},
		{"/admin/api/jobs?limit=0", http.StatusBadRequest, "INVALID_LIMIT"},
		{"/admin/api/jobs?cursor=0", http.StatusBadRequest, "INVALID_CURSOR"},
		{"/admin/api/jobs?cursor=01", http.StatusBadRequest, "INVALID_CURSOR"},
		{"/admin/api/jobs?unexpected=1", http.StatusBadRequest, "INVALID_FILTER"},
	} {
		w := httptest.NewRecorder()
		s.listJobs(w, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if w.Code != tc.status {
			t.Fatalf("%s: status=%d body=%s", tc.path, w.Code, w.Body.String())
		}
		if tc.code != "" {
			var failure struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &failure); err != nil || failure.Error.Code != tc.code {
				t.Fatalf("%s: wrong error: %s (%v)", tc.path, w.Body.String(), err)
			}
		} else {
			var page struct {
				Items      []json.RawMessage `json:"items"`
				NextCursor string            `json:"next_cursor"`
				ObservedAt string            `json:"observed_at"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || page.Items == nil || len(page.Items) != 0 || page.NextCursor != "" || page.ObservedAt == "" {
				t.Fatalf("%s: malformed empty page: %s (%v)", tc.path, w.Body.String(), err)
			}
		}
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.listJobs(w, httptest.NewRequest(http.MethodGet, "/admin/api/jobs", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("closed database reported as an empty job list: %d %s", w.Code, w.Body.String())
	}
}
