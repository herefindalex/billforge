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

func TestStateReadPreservesLargeIntegers(t *testing.T) {
	value := map[string]any{"AmountMinor": json.Number("9007199254740993"), "Nested": []any{json.Number("9223372036854775807")}}
	safe := stringNumbers(value).(map[string]any)
	if safe["AmountMinor"] != "9007199254740993" {
		t.Fatalf("amount lost precision: %v", safe["AmountMinor"])
	}
	if safe["Nested"].([]any)[0] != "9223372036854775807" {
		t.Fatalf("nested amount lost precision: %v", safe["Nested"])
	}
}

func TestOverviewDatabaseFailureIsNotReportedAsEmptyData(t *testing.T) {
	l, err := lab.Open(filepath.Join(t.TempDir(), "commerce.db"), filepath.Join(t.TempDir(), "provider.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	s := &Server{lab: l}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/admin/api/overview", nil)
	s.overview(w, r)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("database failure status = %d: %s", w.Code, w.Body.String())
	}
	var response struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Error.Code != "QUERY_FAILED" {
		t.Fatalf("database failure was hidden: %s", w.Body.String())
	}
}

func TestFinancialReadsReportDatabaseFailureInsteadOfEmptyOrMissingData(t *testing.T) {
	l, err := lab.Open(filepath.Join(t.TempDir(), "commerce.db"), filepath.Join(t.TempDir(), "provider.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	s := &Server{lab: l}
	checks := []struct {
		name, path, resource string
		handle               func(http.ResponseWriter, *http.Request)
	}{
		{"lab faults", "/admin/api/lab/faults", "", s.labFaults},
		{"subscriptions list", "/admin/api/subscriptions", "subscriptions", s.listResource},
		{"prices list", "/admin/api/prices", "prices", s.listResource},
		{"catalog selections list", "/admin/api/catalog-selections", "catalog-selections", s.listResource},
		{"price migrations list", "/admin/api/price-migrations", "price-migrations", s.listResource},
		{"contracts list", "/admin/api/contracts", "contracts", s.listResource},
		{"usage list", "/admin/api/usage-events", "usage-events", s.listResource},
		{"usage periods list", "/admin/api/usage-periods", "usage-periods", s.listResource},
		{"quotes list", "/admin/api/quotes", "quotes", s.listResource},
		{"invoices list", "/admin/api/invoices", "invoices", s.listResource},
		{"payments list", "/admin/api/payments", "payments", s.listResource},
		{"immediate changes list", "/admin/api/immediate-changes", "immediate-changes", s.listResource},
		{"refunds list", "/admin/api/refunds", "refunds", s.listResource},
		{"credits list", "/admin/api/credits", "credits", s.listResource},
		{"outbox list", "/admin/api/outbox", "outbox", s.listResource},
		{"meters list", "/admin/api/meters", "meters", s.listResource},
		{"account migrations list", "/admin/api/account-migrations", "account-migrations", s.listResource},
		{"legacy provenance list", "/admin/api/legacy-provenance", "legacy-provenance", s.listResource},
		{"reconciliation runs list", "/admin/api/reconciliation-runs", "reconciliation-runs", s.listResource},
		{"credit detail", "/admin/api/credits/object-1", "", s.creditDetail},
		{"price detail", "/admin/api/prices/object-1", "", s.priceDetail},
		{"payment detail", "/admin/api/payments/object-1", "", s.paymentDetail},
		{"refund detail", "/admin/api/refunds/object-1", "", s.refundDetail},
		{"discrepancies list", "/admin/api/discrepancies", "discrepancies", s.listResource},
		{"customers list", "/admin/api/customers", "", s.listCustomers},
		{"customer detail", "/admin/api/customers/object-1", "", s.customerDetail},
		{"quote detail", "/admin/api/quotes/object-1", "", s.quoteDetail},
		{"subscription detail", "/admin/api/subscriptions/object-1", "", s.subscriptionDetail},
		{"subscription periods", "/admin/api/subscriptions/object-1/periods", "", s.subscriptionPeriods},
		{"subscription timeline", "/admin/api/subscriptions/object-1/timeline", "", s.subscriptionTimeline},
		{"subscription entitlement", "/admin/api/subscriptions/object-1/entitlement", "", s.subscriptionEntitlement},
		{"invoice detail", "/admin/api/invoices/object-1", "", s.invoiceDetail},
		{"invoice history", "/admin/api/invoices/object-1/history/corrections", "", s.invoiceHistory},
		{"usage period detail", "/admin/api/usage-periods/object-1/0", "", s.usagePeriodDetail},
		{"usage rating history", "/admin/api/usage-periods/object-1/0/ratings", "", s.usagePeriodRatings},
		{"discrepancy detail", "/admin/api/discrepancies/object-1", "", s.discrepancyDetail},
		{"reconciliation run detail", "/admin/api/reconciliation-runs/object-1", "", s.reconciliationRunDetail},
		{"price migration detail", "/admin/api/price-migrations/object-1", "", s.migrationDetail},
		{"account migration detail", "/admin/api/account-migrations/object-1", "", s.accountMigrationDetail},
		{"account migration readiness", "/admin/api/account-migrations/object-1/readiness?max_quote_p95_millis=5000&max_unknown_payments=0&max_open_discrepancies=0", "", s.accountMigrationReadiness},
		{"account migration entitlement", "/admin/api/account-migrations/object-1/entitlements/sub-1", "", s.accountMigrationEntitlement},
		{"account migration shadows", "/admin/api/account-migrations/object-1/shadows", "", s.accountMigrationShadows},
		{"account migration provenance", "/admin/api/account-migrations/object-1/provenance", "", s.accountMigrationProvenance},
		{"commands list", "/admin/api/commands", "", s.listCommands},
		{"command detail", "/admin/api/commands/object-1", "", s.getCommand},
		{"preview detail", "/admin/api/previews/object-1", "", s.getPreview},
		{"job detail", "/admin/api/jobs/object-1", "", s.getJob},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, check.path, nil)
			r.SetPathValue("resource", check.resource)
			r.SetPathValue("id", "object-1")
			r.SetPathValue("index", "0")
			r.SetPathValue("kind", "corrections")
			r.SetPathValue("subscriptionId", "sub-1")
			w := httptest.NewRecorder()
			check.handle(w, r)
			if w.Code != http.StatusInternalServerError {
				t.Fatalf("database failure status = %d: %s", w.Code, w.Body.String())
			}
			var response struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if response.Error.Code != "QUERY_FAILED" {
				t.Fatalf("database failure was hidden: %s", w.Body.String())
			}
		})
	}
}

func TestPreviewAndJobReadsDistinguishMissingRecordsFromDatabaseFailures(t *testing.T) {
	l, err := lab.Open(filepath.Join(t.TempDir(), "commerce.db"), filepath.Join(t.TempDir(), "provider.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := l.InitAdmin(context.Background()); err != nil {
		t.Fatal(err)
	}
	s := &Server{lab: l}
	for _, check := range []struct {
		name, path string
		handle     func(http.ResponseWriter, *http.Request)
	}{
		{"preview", "/admin/api/previews/missing", s.getPreview},
		{"job", "/admin/api/jobs/missing", s.getJob},
	} {
		t.Run(check.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, check.path, nil)
			r.SetPathValue("id", "missing")
			w := httptest.NewRecorder()
			check.handle(w, r)
			if w.Code != http.StatusNotFound {
				t.Fatalf("missing record status = %d: %s", w.Code, w.Body.String())
			}
			var response struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if response.Error.Code != "NOT_FOUND" {
				t.Fatalf("missing record classification = %s", w.Body.String())
			}
		})
	}
}
