package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"billforge/lab"
)

func TestPriceVersionDetailReturnsPublishedComponentsAndSelectionScope(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	l, err := lab.Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	s := &Server{lab: l}
	r := httptest.NewRequest(http.MethodGet, "/admin/api/prices/pro-v1", nil)
	r.SetPathValue("id", "pro-v1")
	w := httptest.NewRecorder()
	s.priceDetail(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("price detail status=%d body=%s", w.Code, w.Body.String())
	}
	var body struct {
		Price struct {
			ID                  string
			PlanID              string
			Version             string
			Currency            string
			FixedMinor          string
			Checksum            string
			PublicationState    string
			EffectiveFrom       string
			SelectionCount      string
			ComponentsTruncated bool
			Components          []struct {
				Code        string
				Kind        string
				AmountMinor string
				Quantity    string
				RateNum     string
				RateDen     string
				MeterID     string
			}
		}
		ObservedAt string `json:"observed_at"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	p := body.Price
	if p.ID != "pro-v1" || p.PlanID != "pro" || p.Version != "1" || p.Currency != "USD" || p.FixedMinor != "5000" || p.PublicationState != "published" || p.Checksum == "" || p.SelectionCount != "1" || p.EffectiveFrom == "" || body.ObservedAt == "" {
		t.Fatalf("price version=%+v observed_at=%q", p, body.ObservedAt)
	}
	if p.ComponentsTruncated || len(p.Components) != 3 {
		t.Fatalf("price components=%+v truncated=%t", p.Components, p.ComponentsTruncated)
	}
	if p.Components[0].Code != "seats" || p.Components[0].Kind != "per_seat" || p.Components[0].AmountMinor != "1000" || p.Components[1].Code != "tasks_included" || p.Components[1].Quantity != "20000" || p.Components[2].Code != "tasks_overage" || p.Components[2].RateNum != "1" || p.Components[2].RateDen != "10" {
		t.Fatalf("price components=%+v", p.Components)
	}
}

func TestPriceVersionDetailRequiresSessionAndReturnsNotFound(t *testing.T) {
	h := newTestHandler(t)
	if got := request(h, http.MethodGet, "/admin/api/prices/pro-v1", "").Code; got != http.StatusUnauthorized {
		t.Fatalf("price detail without session=%d", got)
	}
	dir := t.TempDir()
	l, err := lab.Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	s := &Server{lab: l}
	r := httptest.NewRequest(http.MethodGet, "/admin/api/prices/missing", nil)
	r.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	s.priceDetail(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing price status=%d body=%s", w.Code, w.Body.String())
	}
}
