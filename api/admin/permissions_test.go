package admin

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"billforge/lab"
)

func TestAllCommandsHaveCapabilityAndLabControlIsEnforced(t *testing.T) {
	for i := 1; i <= 49; i++ {
		if got := actionCapability(fmt.Sprintf("C%02d", i)); got == "" {
			t.Fatalf("C%02d has no capability", i)
		}
	}
	if got := actionCapability("C49"); got != "lab.control" {
		t.Fatalf("C49 capability = %s", got)
	}
	token := "limited-session"
	s := &Server{sessions: map[[32]byte]session{hashToken(token): {capabilities: []string{"read", "subscription.manage"}, expires: time.Now().Add(time.Hour), idleUntil: time.Now().Add(time.Hour)}}, now: time.Now}
	r := httptest.NewRequest("POST", "http://127.0.0.1:8080/admin/api/commands", nil)
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
	w := httptest.NewRecorder()
	if s.authorizeAction(w, r, "C49") || w.Code != http.StatusForbidden {
		t.Fatalf("lab control was allowed: %d", w.Code)
	}
	w = httptest.NewRecorder()
	if !s.authorizeAction(w, r, "C01") {
		t.Fatalf("subscription command denied: %d", w.Code)
	}
	w = httptest.NewRecorder()
	if !s.authorizeAction(w, r, "C02") {
		t.Fatalf("ordinary quote denied: %d", w.Code)
	}
	w = httptest.NewRecorder()
	if s.authorizeActionIntent(w, r, "C01", "", []byte(`{"contract_version_id":"contract-1"}`)) || w.Code != http.StatusForbidden {
		t.Fatalf("contract quote creation allowed without contract capability: %d", w.Code)
	}
	item := s.sessions[hashToken(token)]
	item.capabilities = append(item.capabilities, "contract.manage")
	s.sessions[hashToken(token)] = item
	w = httptest.NewRecorder()
	if !s.authorizeActionIntent(w, r, "C01", "", []byte(`{"contract_version_id":"contract-1"}`)) {
		t.Fatalf("contract quote denied with both capabilities: %d", w.Code)
	}
}

func TestEveryActionRequiresItsCapability(t *testing.T) {
	now := time.Now()
	token := "matrix-session"
	s := &Server{
		sessions: map[[32]byte]session{hashToken(token): {
			expires: now.Add(time.Hour), idleUntil: now.Add(time.Hour),
		}},
		now: func() time.Time { return now },
	}
	request := func(authenticated bool) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/admin/api/commands", nil)
		if authenticated {
			r.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
		}
		return r
	}
	for i := 1; i <= 49; i++ {
		actionID := fmt.Sprintf("C%02d", i)
		t.Run(actionID, func(t *testing.T) {
			w := httptest.NewRecorder()
			if s.authorizeAction(w, request(false), actionID) || w.Code != http.StatusUnauthorized {
				t.Fatalf("missing session status = %d", w.Code)
			}
			w = httptest.NewRecorder()
			if s.authorizeAction(w, request(true), actionID) || w.Code != http.StatusForbidden {
				t.Fatalf("missing capability status = %d", w.Code)
			}
			item := s.sessions[hashToken(token)]
			item.capabilities = []string{actionCapability(actionID)}
			s.sessions[hashToken(token)] = item
			w = httptest.NewRecorder()
			allowed := s.authorizeAction(w, request(true), actionID)
			if !allowed {
				t.Fatalf("required capability denied: %d", w.Code)
			}
			item.capabilities = nil
			s.sessions[hashToken(token)] = item
		})
	}
}

func TestContractQuoteAcceptRequiresContractCapability(t *testing.T) {
	dir := t.TempDir()
	l, err := lab.Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	ctx := context.Background()
	now := time.Now().UTC()
	_, err = l.PublishContract(ctx, lab.ContractSpec{
		ID: "permission-contract", CustomerID: "permission-customer", Version: 1,
		BasePriceVersionID: "pro-v1", FixedMinor: 4000, SeatMinor: 700,
		EffectiveFrom: now.Add(-time.Hour), EffectiveTo: now.Add(90 * 24 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	contractQuote, err := l.CreateContractQuote(ctx, "permission-customer", "permission-contract", 5)
	if err != nil {
		t.Fatal(err)
	}
	ordinaryQuote, err := l.CreateQuote(ctx, "permission-other", "basic")
	if err != nil {
		t.Fatal(err)
	}
	token := "contract-permission-session"
	s := &Server{lab: l, sessions: map[[32]byte]session{
		hashToken(token): {capabilities: []string{"read", "subscription.manage"}, expires: now.Add(time.Hour), idleUntil: now.Add(time.Hour)},
	}, now: time.Now}
	r := httptest.NewRequest(http.MethodPost, "/admin/api/previews", nil)
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
	w := httptest.NewRecorder()
	if !s.authorizeActionIntent(w, r, "C02", ordinaryQuote.ID, nil) {
		t.Fatalf("ordinary quote denied: %d", w.Code)
	}
	w = httptest.NewRecorder()
	if s.authorizeActionIntent(w, r, "C02", contractQuote.ID, nil) || w.Code != http.StatusForbidden {
		t.Fatalf("contract quote accepted without contract capability: %d", w.Code)
	}
	item := s.sessions[hashToken(token)]
	item.capabilities = append(item.capabilities, "contract.manage")
	s.sessions[hashToken(token)] = item
	w = httptest.NewRecorder()
	if !s.authorizeActionIntent(w, r, "C02", contractQuote.ID, nil) {
		t.Fatalf("contract quote denied with both capabilities: %d", w.Code)
	}
}
