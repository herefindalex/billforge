package admin

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"billforge/lab"
)

func TestUsagePeriodDetailRoutesValidateAndProtectReads(t *testing.T) {
	h := newTestHandler(t)
	for _, path := range []string{
		"/admin/api/usage-periods/sub-1/0",
		"/admin/api/usage-periods/sub-1/0/ratings",
	} {
		if got := request(h, http.MethodGet, path, "").Code; got != http.StatusUnauthorized {
			t.Fatalf("unauthenticated %s = %d", path, got)
		}
	}

	dir := t.TempDir()
	l, err := lab.Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	s := &Server{lab: l}
	crossPeriod, err := json.Marshal(lab.AdminUsageRatingCursor{SubscriptionID: "other", PeriodIndex: 0, Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	checks := []struct {
		name   string
		path   string
		index  string
		handle http.HandlerFunc
		want   int
	}{
		{"missing detail", "/admin/api/usage-periods/sub-1/0", "0", s.usagePeriodDetail, http.StatusNotFound},
		{"missing ratings", "/admin/api/usage-periods/sub-1/0/ratings", "0", s.usagePeriodRatings, http.StatusNotFound},
		{"invalid index", "/admin/api/usage-periods/sub-1/invalid", "invalid", s.usagePeriodDetail, http.StatusBadRequest},
		{"negative index", "/admin/api/usage-periods/sub-1/-1/ratings", "-1", s.usagePeriodRatings, http.StatusBadRequest},
		{"unexpected detail query", "/admin/api/usage-periods/sub-1/0?extra=1", "0", s.usagePeriodDetail, http.StatusBadRequest},
		{"invalid limit", "/admin/api/usage-periods/sub-1/0/ratings?limit=0", "0", s.usagePeriodRatings, http.StatusBadRequest},
		{"invalid cursor", "/admin/api/usage-periods/sub-1/0/ratings?cursor=invalid", "0", s.usagePeriodRatings, http.StatusBadRequest},
		{"cross-period cursor", "/admin/api/usage-periods/sub-1/0/ratings?cursor=" + base64.RawURLEncoding.EncodeToString(crossPeriod), "0", s.usagePeriodRatings, http.StatusBadRequest},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, check.path, nil)
			r.SetPathValue("id", "sub-1")
			r.SetPathValue("index", check.index)
			w := httptest.NewRecorder()
			check.handle(w, r)
			if w.Code != check.want {
				t.Fatalf("%s: got %d, want %d: %s", check.path, w.Code, check.want, w.Body.String())
			}
		})
	}
}
