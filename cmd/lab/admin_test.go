package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestAdminAssetsDeepLinksAndMissingAssets(t *testing.T) {
	fsys := fstest.MapFS{
		"index.html":    {Data: []byte("<html>admin shell</html>")},
		"assets/app.js": {Data: []byte("console.log('app')")},
	}
	h := adminAssets(fsys)
	for _, test := range []struct {
		path   string
		status int
		body   string
	}{
		{"/admin/quotes/quote-1/accept", http.StatusOK, "<html>admin shell</html>"},
		{"/admin/assets/app.js", http.StatusOK, "console.log('app')"},
		{"/admin/assets/missing.js", http.StatusNotFound, ""},
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", test.path, nil))
		if w.Code != test.status {
			t.Fatalf("%s: got status %d, want %d", test.path, w.Code, test.status)
		}
		if test.body != "" && w.Body.String() != test.body {
			t.Fatalf("%s: got %q", test.path, w.Body.String())
		}
	}
}

func TestAdminHostGuardRejectsOtherAuthorities(t *testing.T) {
	called := 0
	h := adminHostGuard("127.0.0.1:8080", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called++
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, tc := range []struct {
		host string
		want int
	}{
		{"127.0.0.1:8080", http.StatusNoContent},
		{"attacker.example:8080", http.StatusForbidden},
		{"localhost:8080", http.StatusForbidden},
		{"127.0.0.1:9999", http.StatusForbidden},
	} {
		r := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/admin/api/session/csrf", nil)
		r.Host = tc.host
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("host %q: got %d, want %d", tc.host, w.Code, tc.want)
		}
	}
	if called != 1 {
		t.Fatalf("forwarded %d requests, want one", called)
	}
}

func TestAdminHostGuardAcceptsDefaultPortAuthority(t *testing.T) {
	for _, tc := range []struct {
		listen string
		host   string
		want   int
	}{
		{"127.0.0.1:80", "127.0.0.1", http.StatusNoContent},
		{"[::1]:80", "[::1]", http.StatusNoContent},
		{"127.0.0.1:8080", "127.0.0.1", http.StatusForbidden},
	} {
		h := adminHostGuard(tc.listen, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))
		r := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/admin/", nil)
		r.Host = tc.host
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("listen %q, host %q: got %d, want %d", tc.listen, tc.host, w.Code, tc.want)
		}
	}
}
