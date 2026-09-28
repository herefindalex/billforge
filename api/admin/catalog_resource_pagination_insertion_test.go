package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"billforge/lab"
)

func TestFilteredCatalogPagesIncludeLaterInsertAndKeepUTCTime(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	l, err := lab.Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), func() time.Time { return at })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}

	publish := func(id string, version int64) {
		t.Helper()
		_, err := l.PublishProPrice(ctx, lab.ProPriceSpec{
			ID: id, Version: version, FixedMinor: 4000, SeatMinor: 700,
			IncludedTasks: 20000, UsageRateNum: 1, UsageRateDen: 100,
			EffectiveFrom: at,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	selectPrice := func(id string, hour int) {
		t.Helper()
		if err := l.SelectCatalogPrice(ctx, "pro", "page-cohort", at.Add(time.Duration(hour)*time.Hour), id); err != nil {
			t.Fatal(err)
		}
	}
	publish("pro-page-v2", 2)
	publish("pro-page-v3", 3)
	selectPrice("pro-page-v2", 0)
	selectPrice("pro-page-v3", 1)

	const token = "catalog-page-session"
	s := &Server{
		lab: l,
		sessions: map[[32]byte]session{
			hashToken(token): {capabilities: []string{"read"}, expires: time.Now().Add(time.Hour), idleUntil: time.Now().Add(time.Hour)},
		},
		now: time.Now,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/api/{resource}", s.protected(s.listResource))
	type page struct {
		Items      []map[string]any `json:"items"`
		Total      int              `json:"total"`
		NextCursor string           `json:"next_cursor"`
	}
	get := func(resource string, filters url.Values, cursor string) page {
		t.Helper()
		query := url.Values{"limit": {"1"}}
		for key, values := range filters {
			for _, value := range values {
				query.Add(key, value)
			}
		}
		if cursor != "" {
			query.Set("cursor", cursor)
		}
		request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/admin/api/"+resource+"?"+query.Encode(), nil)
		request.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s status=%d body=%s", resource, response.Code, response.Body.String())
		}
		var result page
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	cases := []struct {
		resource string
		filters  url.Values
		idField  string
	}{
		{"prices", url.Values{"plan_id": {"pro"}, "id_prefix": {"pro-page-"}}, "ID"},
		{"catalog-selections", url.Values{"plan_id": {"pro"}, "cohort": {"page-cohort"}}, "PriceVersionID"},
	}
	firstPages := make(map[string]page)
	for _, tc := range cases {
		first := get(tc.resource, tc.filters, "")
		if first.Total != 2 || len(first.Items) != 1 || first.Items[0][tc.idField] != "pro-page-v2" || first.NextCursor == "" {
			t.Fatalf("%s first page %+v", tc.resource, first)
		}
		if tc.resource == "catalog-selections" && first.Items[0]["EffectiveAt"] != at.Format(time.RFC3339Nano) {
			t.Fatalf("catalog effective time=%v", first.Items[0]["EffectiveAt"])
		}
		firstPages[tc.resource] = first
	}

	publish("pro-page-v4", 4)
	selectPrice("pro-page-v4", 2)
	if err := l.SelectCatalogPrice(ctx, "pro", "other-cohort", at.Add(2*time.Hour), "pro-page-v4"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		first := firstPages[tc.resource]
		got := []string{first.Items[0][tc.idField].(string)}
		cursor := first.NextCursor
		for cursor != "" {
			next := get(tc.resource, tc.filters, cursor)
			if next.Total != 3 || len(next.Items) != 1 {
				t.Fatalf("%s page after insert %+v", tc.resource, next)
			}
			got = append(got, next.Items[0][tc.idField].(string))
			cursor = next.NextCursor
			if len(got) > 3 {
				t.Fatalf("%s cursor repeated rows: %v", tc.resource, got)
			}
		}
		want := []string{"pro-page-v2", "pro-page-v3", "pro-page-v4"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s pages got %v, want %v", tc.resource, got, want)
		}
	}
}
