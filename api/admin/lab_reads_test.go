package admin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"billforge/lab"
)

func TestFaultTicketReadValidatesCursorAndReturnsBoundedPage(t *testing.T) {
	l, err := lab.Open(filepath.Join(t.TempDir(), "commerce.db"), filepath.Join(t.TempDir(), "provider.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := l.InitAdmin(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := &Server{lab: l}
	encoded := func(value string) string { return base64.RawURLEncoding.EncodeToString([]byte(value)) }
	for _, check := range []struct {
		name, query, code string
	}{
		{"invalid base64", "cursor=%25%25%25", "INVALID_CURSOR"},
		{"missing position", "cursor=" + encoded(`{"pending":true}`), "INVALID_CURSOR"},
		{"invalid row", "cursor=" + encoded(`{"pending":true,"row_id":-1}`), "INVALID_CURSOR"},
		{"unknown field", "cursor=" + encoded(`{"pending":true,"row_id":1,"extra":1}`), "INVALID_CURSOR"},
		{"invalid limit", "limit=101", "INVALID_LIMIT"},
	} {
		t.Run(check.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/admin/api/lab/faults?"+check.query, nil)
			w := httptest.NewRecorder()
			s.labFaults(w, r)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
			var response struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if response.Error.Code != check.code {
				t.Fatalf("error code %q, want %q", response.Error.Code, check.code)
			}
		})
	}
	w := httptest.NewRecorder()
	s.labFaults(w, httptest.NewRequest(http.MethodGet, "/admin/api/lab/faults?limit=20", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("empty page status %d: %s", w.Code, w.Body.String())
	}
	var page struct {
		Items      []any  `json:"items"`
		NextCursor string `json:"next_cursor"`
		ObservedAt string `json:"observed_at"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Items == nil || len(page.Items) != 0 || page.NextCursor != "" || page.ObservedAt == "" {
		t.Fatalf("empty fault page fields: %s", w.Body.String())
	}
}
