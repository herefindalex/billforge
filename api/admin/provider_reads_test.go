package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"billforge/lab"
)

func TestProviderReadsRequireLabCapabilityAndPreserveMoney(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	providerPath := filepath.Join(dir, "provider.db")
	l, err := lab.Open(filepath.Join(dir, "commerce.db"), providerPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	providerDB, err := sql.Open("sqlite3", providerPath)
	if err != nil {
		t.Fatal(err)
	}
	defer providerDB.Close()
	for _, row := range []struct {
		key    string
		amount int64
	}{
		{"large-capture", 9007199254740993}, {"recent-capture", 1000},
	} {
		if _, err := providerDB.ExecContext(ctx, `INSERT INTO captures(provider_key,amount_minor,currency,status) VALUES(?,?,'USD','succeeded')`, row.key, row.amount); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := providerDB.ExecContext(ctx, `INSERT INTO refunds(provider_key,source_capture_key,amount_minor,currency,status) VALUES('refund-1','recent-capture',200,'USD','succeeded')`); err != nil {
		t.Fatal(err)
	}

	const readerToken = "provider-reader-session"
	const controllerToken = "provider-control-session"
	expiry := time.Now().Add(time.Hour)
	s := &Server{lab: l, now: time.Now, sessions: map[[32]byte]session{
		hashToken(readerToken):     {expires: expiry, idleUntil: expiry, capabilities: []string{"read"}},
		hashToken(controllerToken): {expires: expiry, idleUntil: expiry, capabilities: []string{"read", "lab.control"}},
	}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/api/lab/status", s.protectedCapability("lab.control", s.labStatus))
	mux.HandleFunc("GET /admin/api/lab/provider-captures", s.protectedCapability("lab.control", s.providerCaptures))
	mux.HandleFunc("GET /admin/api/lab/provider-refunds", s.protectedCapability("lab.control", s.providerRefunds))
	get := func(path, token string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080"+path, nil)
		if token != "" {
			r.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	for _, path := range []string{"/admin/api/lab/status", "/admin/api/lab/provider-captures", "/admin/api/lab/provider-refunds"} {
		if w := get(path, ""); w.Code != http.StatusUnauthorized {
			t.Fatalf("%s without session: %d", path, w.Code)
		}
		if w := get(path, readerToken); w.Code != http.StatusForbidden {
			t.Fatalf("%s without lab.control: %d", path, w.Code)
		}
	}

	statusResponse := get("/admin/api/lab/status", controllerToken)
	if statusResponse.Code != http.StatusOK {
		t.Fatalf("status: %d %s", statusResponse.Code, statusResponse.Body.String())
	}
	var status struct {
		Status struct {
			PendingFaultTickets string `json:"pending_fault_tickets"`
			Provider            struct {
				Captures string `json:"captures"`
				Refunds  string `json:"refunds"`
			} `json:"provider"`
		} `json:"status"`
		ProviderObservedAt string `json:"provider_observed_at"`
	}
	if err := json.Unmarshal(statusResponse.Body.Bytes(), &status); err != nil || status.Status.Provider.Captures != "2" || status.Status.Provider.Refunds != "1" || status.Status.PendingFaultTickets != "0" || status.ProviderObservedAt == "" {
		t.Fatalf("provider status: %s (%v)", statusResponse.Body.String(), err)
	}

	firstResponse := get("/admin/api/lab/provider-captures?limit=1", controllerToken)
	if firstResponse.Code != http.StatusOK {
		t.Fatalf("captures: %d %s", firstResponse.Code, firstResponse.Body.String())
	}
	var first struct {
		Items []struct {
			ProviderKey string `json:"provider_key"`
			AmountMinor string `json:"amount_minor"`
		} `json:"items"`
		NextCursor string `json:"next_cursor"`
	}
	if err := json.Unmarshal(firstResponse.Body.Bytes(), &first); err != nil || len(first.Items) != 1 || first.Items[0].ProviderKey != "recent-capture" || first.NextCursor == "" {
		t.Fatalf("first capture page: %s (%v)", firstResponse.Body.String(), err)
	}
	secondResponse := get("/admin/api/lab/provider-captures?limit=1&cursor="+first.NextCursor, controllerToken)
	var second struct {
		Items []struct {
			AmountMinor string `json:"amount_minor"`
		} `json:"items"`
	}
	if secondResponse.Code != http.StatusOK || json.Unmarshal(secondResponse.Body.Bytes(), &second) != nil || len(second.Items) != 1 || second.Items[0].AmountMinor != "9007199254740993" {
		t.Fatalf("large capture lost precision: %d %s", secondResponse.Code, secondResponse.Body.String())
	}
	if w := get("/admin/api/lab/provider-refunds?cursor="+first.NextCursor, controllerToken); w.Code != http.StatusBadRequest {
		t.Fatalf("capture cursor reused for refunds: %d %s", w.Code, w.Body.String())
	}
	if w := get("/admin/api/lab/provider-captures?status=definitively_failed&cursor="+first.NextCursor, controllerToken); w.Code != http.StatusBadRequest {
		t.Fatalf("capture cursor reused with another filter: %d %s", w.Code, w.Body.String())
	}
	if w := get("/admin/api/lab/provider-captures?status=unknown", controllerToken); w.Code != http.StatusBadRequest {
		t.Fatalf("invalid provider status accepted: %d %s", w.Code, w.Body.String())
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	if w := get("/admin/api/lab/status", controllerToken); w.Code != http.StatusInternalServerError {
		t.Fatalf("closed database reported healthy lab status: %d %s", w.Code, w.Body.String())
	}
}
