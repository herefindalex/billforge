package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"billforge/lab"
)

func TestFinancialResourceKeysRequireFinanceCapability(t *testing.T) {
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
	quote, err := l.CreateQuote(ctx, "resource-key-customer", "basic")
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := l.AcceptQuote(ctx, quote.ID, quote.Fingerprint, "resource-key-checkout")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.DispatchCapture(ctx, accepted.OperationID, ""); err != nil {
		t.Fatal(err)
	}
	correction, err := l.PostReduction(ctx, accepted.InvoiceID, 1000, "resource key test", "resource-key-reduction")
	if err != nil || len(correction.GrantIDs) != 1 {
		t.Fatalf("correction=%+v error=%v", correction, err)
	}
	refundID, err := l.ReserveRefund(ctx, correction.GrantIDs[0], 200, "resource-key-refund")
	if err != nil {
		t.Fatal(err)
	}

	const readToken = "resource-key-read-session"
	const financeToken = "resource-key-finance-session"
	s := &Server{
		lab: l,
		sessions: map[[32]byte]session{
			hashToken(readToken):    {capabilities: []string{"read"}, expires: time.Now().Add(time.Hour), idleUntil: time.Now().Add(time.Hour)},
			hashToken(financeToken): {capabilities: []string{"read", "finance.adjust"}, expires: time.Now().Add(time.Hour), idleUntil: time.Now().Add(time.Hour)},
		},
		now: time.Now,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/api/{resource}", s.protected(s.listResource))
	get := func(resource, token string) ([]map[string]any, string) {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/admin/api/"+resource, nil)
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s status=%d body=%s", resource, w.Code, w.Body.String())
		}
		var page struct {
			Items []map[string]any `json:"items"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 1 {
			t.Fatalf("GET %s items=%d body=%s", resource, len(page.Items), w.Body.String())
		}
		return page.Items, w.Body.String()
	}

	for _, tc := range []struct {
		resource string
		id       string
		keys     []string
	}{
		{"payments", accepted.OperationID, []string{"ProviderKey"}},
		{"refunds", refundID, []string{"ProviderKey", "SourceProviderKey", "RequestKey"}},
	} {
		readItems, readBody := get(tc.resource, readToken)
		financeItems, _ := get(tc.resource, financeToken)
		if readItems[0]["ID"] != tc.id || financeItems[0]["ID"] != tc.id {
			t.Fatalf("%s changed identity across permissions", tc.resource)
		}
		for _, key := range tc.keys {
			if _, exists := readItems[0][key]; exists {
				t.Fatalf("read-only %s leaked %s: %s", tc.resource, key, readBody)
			}
			if value, exists := financeItems[0][key]; !exists || value == "" || value == nil {
				t.Fatalf("finance %s missing %s: %+v", tc.resource, key, financeItems[0])
			}
		}
	}
}
