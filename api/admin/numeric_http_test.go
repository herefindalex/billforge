package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"billforge/lab"
)

func TestMoneyAndTimePayloadBoundariesRejectBeforeHTTPAdmission(t *testing.T) {
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
	const token = "numeric-boundary-session"
	const csrf = "numeric-boundary-csrf"
	s := &Server{
		lab: l,
		sessions: map[[32]byte]session{
			hashToken(token): {
				csrf: csrf, expires: time.Now().Add(time.Hour), idleUntil: time.Now().Add(time.Hour),
				capabilities: []string{"finance.adjust", "catalog.publish", "reconciliation.repair"},
			},
		},
		now: time.Now,
	}
	commands := s.protectedWrite(s.submitCommand)
	previews := s.protectedWrite(s.createPreview)
	invalidAmount := []struct {
		name, value string
	}{
		{"zero", `"0"`},
		{"negative", `"-1"`},
		{"overflow", `"9223372036854775808"`},
		{"decimal", `"1.5"`},
		{"exponent", `"1e3"`},
		{"json-number", `500`},
		{"null", `null`},
	}
	type actionCase struct {
		action  string
		name    string
		payload string
		preview bool
	}
	var cases []actionCase
	for _, value := range invalidAmount {
		cases = append(cases,
			actionCase{"C07", value.name, `{"amount_minor":` + value.value + `}`, true},
			actionCase{"C11", value.name, `{"reduction_minor":` + value.value + `,"reason":"correction"}`, true},
			actionCase{"C12", value.name, `{"invoice_id":"missing-invoice","amount_minor":` + value.value + `}`, true},
			actionCase{"C15", value.name, `{"amount_minor":` + value.value + `}`, true},
		)
	}
	for _, value := range []struct{ name, raw string }{
		{"zero", `"0"`}, {"negative", `"-1"`}, {"overflow", `"9223372036854775808"`},
		{"decimal", `"1.5"`}, {"exponent", `"1e3"`}, {"json-number", `500`},
	} {
		cases = append(cases, actionCase{
			action: "C18", name: value.name, preview: true,
			payload: `{"id":"rate-boundary","version":"1","fixed_minor":"100","seat_minor":"1","included_tasks":"0","usage_rate_num":"1","usage_rate_den":` + value.raw + `,"effective_from":"2026-09-26T12:00:00Z"}`,
		})
	}
	for _, value := range []struct{ name, raw string }{
		{"offset", `"2026-10-01T01:00:00+01:00"`}, {"invalid-date", `"2026-02-30T00:00:00Z"`},
		{"too-precise", `"2026-10-01T00:00:00.1234567890Z"`}, {"json-number", `123`},
	} {
		cases = append(cases, actionCase{"C33", value.name, `{"as_of":` + value.raw + `}`, false})
	}
	for i, tc := range cases {
		paths := []struct {
			name, code string
			handler    http.HandlerFunc
		}{
			{"command", "INVALID_COMMAND", commands},
		}
		if tc.preview {
			paths = append(paths, struct {
				name, code string
				handler    http.HandlerFunc
			}{"preview", "INVALID_PREVIEW", previews})
		}
		for _, path := range paths {
			t.Run(fmt.Sprintf("%s/%02d-%s/%s", tc.action, i, tc.name, path.name), func(t *testing.T) {
				body := fmt.Sprintf(`{"action_id":%q,"target_id":"missing-source","payload":%s}`, tc.action, tc.payload)
				r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/admin/api/"+path.name+"s", strings.NewReader(body))
				r.Header.Set("Origin", "http://127.0.0.1:8080")
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set("X-CSRF-Token", csrf)
				r.Header.Set("Idempotency-Key", fmt.Sprintf("numeric-boundary-%d", i))
				r.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
				w := httptest.NewRecorder()
				path.handler(w, r)
				if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), `"code":"`+path.code+`"`) {
					t.Fatalf("status=%d body=%s; want 422 %s", w.Code, w.Body.String(), path.code)
				}
			})
		}
	}
	for _, action := range []string{"C07", "C15"} {
		t.Run(action+"/large-string-reaches-source-lookup", func(t *testing.T) {
			body := fmt.Sprintf(`{"action_id":%q,"target_id":"missing-source","payload":{"amount_minor":"9007199254740993"}}`, action)
			r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/admin/api/previews", strings.NewReader(body))
			r.Header.Set("Origin", "http://127.0.0.1:8080")
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-CSRF-Token", csrf)
			r.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
			w := httptest.NewRecorder()
			previews(w, r)
			if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), `"code":"NOT_FOUND"`) {
				t.Fatalf("valid large string did not reach source lookup: status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
	t.Run("C18/large-denominator-remains-exact-in-http-preview", func(t *testing.T) {
		body := `{"action_id":"C18","payload":{"id":"pro-den-http","version":"99","fixed_minor":"100","seat_minor":"1","included_tasks":"0","usage_rate_num":"1","usage_rate_den":"9007199254740993","effective_from":"2026-08-31T23:00:00Z"}}`
		r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/admin/api/previews", strings.NewReader(body))
		r.Header.Set("Origin", "http://127.0.0.1:8080")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
		w := httptest.NewRecorder()
		previews(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("large denominator preview: status=%d body=%s", w.Code, w.Body.String())
		}
		var preview struct {
			Impact map[string]string `json:"impact"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &preview); err != nil || preview.Impact["usage_rate_den"] != "9007199254740993" {
			t.Fatalf("HTTP preview lost denominator precision: %q err=%v", preview.Impact["usage_rate_den"], err)
		}
	})
	accepted, _, err := l.AdminCommandsPage(ctx, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(accepted) != 0 {
		t.Fatalf("invalid money or UTC input admitted %d command(s)", len(accepted))
	}
}
