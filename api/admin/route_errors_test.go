package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"billforge/lab"
)

func TestUnknownAdminAPIRoutesKeepSessionGuardAndRequestIDEnvelope(t *testing.T) {
	dir := t.TempDir()
	l, err := lab.Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	h, err := New(l, Config{Username: "admin", Password: "local-test-password"})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(h)
	defer server.Close()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar, Timeout: 10 * time.Second}

	checkError := func(response *http.Response, wantStatus int, wantCode string) string {
		t.Helper()
		defer response.Body.Close()
		var body struct {
			RequestID string `json:"request_id"`
			Error     struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		id := response.Header.Get("X-Request-ID")
		if response.StatusCode != wantStatus || body.Error.Code != wantCode || body.RequestID == "" || body.RequestID != id || !strings.HasPrefix(id, "req_") {
			t.Fatalf("status=%d code=%s body_id=%q header_id=%q, want %d/%s with matching ID", response.StatusCode, body.Error.Code, body.RequestID, id, wantStatus, wantCode)
		}
		return id
	}

	unauthorized, err := http.NewRequest(http.MethodGet, server.URL+"/admin/api/no-such-route", nil)
	if err != nil {
		t.Fatal(err)
	}
	unauthorized.Header.Set("X-Request-ID", "req_client_forged")
	response, err := client.Do(unauthorized)
	if err != nil {
		t.Fatal(err)
	}
	firstID := checkError(response, http.StatusUnauthorized, "SESSION_REQUIRED")
	if firstID == "req_client_forged" {
		t.Fatal("client selected its own request ID")
	}

	csrfResponse, err := client.Get(server.URL + "/admin/api/session/csrf")
	if err != nil {
		t.Fatal(err)
	}
	var nonce struct {
		Token string `json:"csrf_token"`
	}
	if err := json.NewDecoder(csrfResponse.Body).Decode(&nonce); err != nil {
		csrfResponse.Body.Close()
		t.Fatal(err)
	}
	csrfResponse.Body.Close()
	login, err := http.NewRequest(http.MethodPost, server.URL+"/admin/api/session", bytes.NewBufferString(`{"username":"admin","password":"local-test-password"}`))
	if err != nil {
		t.Fatal(err)
	}
	login.Header.Set("Origin", server.URL)
	login.Header.Set("Content-Type", "application/json")
	login.Header.Set("X-CSRF-Token", nonce.Token)
	loginResponse, err := client.Do(login)
	if err != nil {
		t.Fatal(err)
	}
	loginResponse.Body.Close()
	if loginResponse.StatusCode != http.StatusOK {
		t.Fatalf("login status=%d", loginResponse.StatusCode)
	}

	response, err = client.Get(server.URL + "/admin/api/no-such-route")
	if err != nil {
		t.Fatal(err)
	}
	secondID := checkError(response, http.StatusNotFound, "NOT_FOUND")
	if secondID == firstID {
		t.Fatal("two requests shared one request ID")
	}

	wrongMethod, err := http.NewRequest(http.MethodPost, server.URL+"/admin/api/overview", bytes.NewBufferString(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	wrongMethod.Header.Set("Origin", server.URL)
	wrongMethod.Header.Set("Content-Type", "application/json")
	response, err = client.Do(wrongMethod)
	if err != nil {
		t.Fatal(err)
	}
	if response.Header.Get("Allow") != "GET, HEAD" {
		t.Fatalf("wrong method Allow=%q", response.Header.Get("Allow"))
	}
	checkError(response, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED")
}
