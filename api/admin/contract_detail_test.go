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

func TestContractDetailShowsNet30ObligationAndLinkedSources(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	due := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	l, err := lab.Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	contract, err := l.PublishContract(ctx, lab.ContractSpec{ID: "acme-detail-v1", CustomerID: "acme-detail", Version: 1, BasePriceVersionID: "pro-v1", FixedMinor: 4000, SeatMinor: 700, EffectiveFrom: now, EffectiveTo: due})
	if err != nil {
		t.Fatal(err)
	}
	quote, err := l.CreateContractQuote(ctx, "acme-detail", contract.ID, 5)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := l.AcceptContractQuote(ctx, quote.ID, quote.Fingerprint, "contract-detail-accept")
	if err != nil {
		t.Fatal(err)
	}

	s := &Server{lab: l}
	r := httptest.NewRequest(http.MethodGet, "/admin/api/contracts/"+contract.ID, nil)
	r.SetPathValue("id", contract.ID)
	w := httptest.NewRecorder()
	s.contractDetail(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("contract detail status=%d body=%s", w.Code, w.Body.String())
	}
	var body struct {
		Contract struct {
			ID                         string
			CustomerID                 string
			Currency                   string
			FixedMinor                 string
			SeatMinor                  string
			PaymentDays                string
			PostContractPriceVersionID string
			Checksum                   string
			QuoteCount                 string
			SubscriptionCount          string
			Quotes                     []struct {
				ID           string
				AmountMinor  string
				SeatQuantity string
				Accepted     bool
			}
			Subscriptions []struct {
				ID                      string
				Status                  string
				SeatQuantity            string
				LatestInvoiceID         string
				LatestDueAt             string
				InvoiceOutstandingMinor string
				InvoiceCurrency         string
			}
		}
		ObservedAt string `json:"observed_at"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	c := body.Contract
	if c.ID != contract.ID || c.CustomerID != "acme-detail" || c.Currency != "USD" || c.FixedMinor != "4000" || c.SeatMinor != "700" || c.PaymentDays != "30" || c.PostContractPriceVersionID != "" || c.Checksum == "" || c.QuoteCount != "1" || c.SubscriptionCount != "1" || body.ObservedAt == "" {
		t.Fatalf("contract terms=%+v observed_at=%q", c, body.ObservedAt)
	}
	if len(c.Quotes) != 1 || c.Quotes[0].ID != quote.ID || c.Quotes[0].AmountMinor != "7500" || c.Quotes[0].SeatQuantity != "5" || !c.Quotes[0].Accepted {
		t.Fatalf("contract quotes=%+v", c.Quotes)
	}
	if len(c.Subscriptions) != 1 || c.Subscriptions[0].ID != receipt.SubscriptionID || c.Subscriptions[0].Status != "active" || c.Subscriptions[0].LatestInvoiceID != receipt.InvoiceID || c.Subscriptions[0].InvoiceOutstandingMinor != "7500" || c.Subscriptions[0].InvoiceCurrency != "USD" || c.Subscriptions[0].LatestDueAt != due.Format(time.RFC3339) {
		t.Fatalf("contract subscriptions=%+v", c.Subscriptions)
	}
}

func TestContractDetailRequiresSessionAndReturnsNotFound(t *testing.T) {
	h := newTestHandler(t)
	if got := request(h, http.MethodGet, "/admin/api/contracts/missing", "").Code; got != http.StatusUnauthorized {
		t.Fatalf("contract detail without session=%d", got)
	}
	dir := t.TempDir()
	l, err := lab.Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	s := &Server{lab: l}
	r := httptest.NewRequest(http.MethodGet, "/admin/api/contracts/missing", nil)
	r.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	s.contractDetail(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing contract status=%d body=%s", w.Code, w.Body.String())
	}
}
