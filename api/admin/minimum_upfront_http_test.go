package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"billforge/lab"
)

func TestPriceMinimumUpfrontBoundaryAtHTTPPreview(t *testing.T) {
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
	const token, csrf = "minimum-upfront-session", "minimum-upfront-csrf"
	s := &Server{
		lab: l,
		sessions: map[[32]byte]session{hashToken(token): {
			csrf: csrf, expires: time.Now().Add(time.Hour), idleUntil: time.Now().Add(time.Hour),
			capabilities: []string{"catalog.publish", "contract.manage"},
		}},
		now: time.Now,
	}
	previewHandler := s.protectedWrite(s.createPreview)
	post := func(action, payload string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/admin/api/previews", strings.NewReader(`{"action_id":"`+action+`","payload":`+payload+`}`))
		r.Header.Set("Origin", "http://127.0.0.1:8080")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
		w := httptest.NewRecorder()
		previewHandler(w, r)
		return w
	}
	const max = "9223372036854775807"
	for _, tc := range []struct {
		action, payload string
	}{
		{"C18", `{"id":"pro-minimum-http","version":"99","fixed_minor":"` + max + `","seat_minor":"1","included_tasks":"0","usage_rate_num":"1","usage_rate_den":"1","effective_from":"2026-09-26T12:00:00Z"}`},
		{"C20", `{"id":"metered_minimum_http","plan_id":"ai","version":"99","fixed_minor":"` + max + `","seat_minor":"1","meter_id":"tasks","included_quantity":"1","usage_rate_num":"1","usage_rate_den":"1","effective_from":"2026-09-26T12:00:00Z"}`},
		{"C31", `{"id":"contract-minimum-http","customer_id":"minimum-http-customer","version":"1","base_price_version_id":"pro-v1","fixed_minor":"` + max + `","seat_minor":"1","effective_from":"2026-09-26T12:00:00Z","effective_to":"2026-10-30T12:00:00Z"}`},
	} {
		t.Run(tc.action, func(t *testing.T) {
			invalid := post(tc.action, tc.payload)
			if invalid.Code != http.StatusUnprocessableEntity || !strings.Contains(invalid.Body.String(), `"code":"INVALID_PREVIEW"`) {
				t.Fatalf("overflowed minimum price reached preview: status=%d body=%s", invalid.Code, invalid.Body.String())
			}
			valid := post(tc.action, strings.Replace(tc.payload, max, "9223372036854775806", 1))
			if valid.Code != http.StatusOK {
				t.Fatalf("representable boundary preview rejected: status=%d body=%s", valid.Code, valid.Body.String())
			}
			var result struct {
				Impact map[string]string `json:"impact"`
			}
			if err := json.Unmarshal(valid.Body.Bytes(), &result); err != nil || result.Impact["fixed_minor"] != "9223372036854775806" {
				t.Fatalf("boundary amount lost in preview: impact=%v err=%v", result.Impact, err)
			}
		})
	}
	commands, _, err := l.AdminCommandsPage(ctx, 0, 1)
	if err != nil || len(commands) != 0 {
		t.Fatalf("price preview created a command: count=%d err=%v", len(commands), err)
	}
}
