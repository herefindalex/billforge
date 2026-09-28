package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"billforge/lab"
)

func openAPI(t *testing.T) (*lab.Lab, *time.Time, http.Handler) {
	t.Helper()
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	l, err := lab.Open(filepath.Join(dir, "commerce.db"), filepath.Join(dir, "provider.db"), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	return l, &now, New(l, "internal-secret")
}

func apiRequest(t *testing.T, h http.Handler, method, path string, body any, headers map[string]string) (int, map[string]any) {
	t.Helper()
	var encoded []byte
	if body != nil {
		var err error
		encoded, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(encoded))
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("%s %s: status %d body %s: %v", method, path, w.Code, w.Body.String(), err)
	}
	return w.Code, out
}

func TestV1BasicPurchaseAndSourceReads(t *testing.T) {
	l, _, h := openAPI(t)
	status, q := apiRequest(t, h, "POST", "/v1/quotes", map[string]any{"customer_id": "basic-customer", "plan_id": "basic"}, nil)
	if status != 201 || q["due_now_minor"] != float64(2000) || q["requires_client_capability"] != false {
		t.Fatalf("basic quote: %d %+v", status, q)
	}
	body := map[string]any{"quote_id": q["quote_id"], "context_fingerprint": q["context_fingerprint"]}
	status, accepted := apiRequest(t, h, "POST", "/v1/subscriptions", body, map[string]string{"Idempotency-Key": "basic-checkout"})
	if status != 201 || accepted["status"] != "pending" || accepted["service_status"] != "pending" {
		t.Fatalf("purchase: %d %+v", status, accepted)
	}
	status, replay := apiRequest(t, h, "POST", "/v1/subscriptions", body, map[string]string{"Idempotency-Key": "basic-checkout"})
	if status != 201 || replay["subscription_id"] != accepted["subscription_id"] {
		t.Fatalf("replay: %d %+v", status, replay)
	}
	subID := accepted["subscription_id"].(string)
	status, sub := apiRequest(t, h, "GET", "/v1/subscriptions/"+subID, nil, nil)
	if status != 200 || sub["service_status"] != "pending" {
		t.Fatalf("unfunded service: %d %+v", status, sub)
	}
	if _, exposed := sub["payments"].([]any)[0].(map[string]any)["provider_key"]; exposed {
		t.Fatalf("public read exposed provider key: %+v", sub["payments"])
	}
	_, internal := apiRequest(t, h, "GET", "/v1/subscriptions/"+subID, nil, map[string]string{"X-Lab-Internal-Token": "internal-secret"})
	if internal["payments"].([]any)[0].(map[string]any)["provider_key"] == nil {
		t.Fatalf("internal evidence missing: %+v", internal["payments"])
	}
	if _, err := l.DispatchNext(context.Background(), "lost_response"); err != lab.ErrPaymentUnknown {
		t.Fatalf("unknown: %v", err)
	}
	_, sub = apiRequest(t, h, "GET", "/v1/subscriptions/"+subID, nil, nil)
	if sub["service_status"] != "pending" {
		t.Fatalf("unknown activated service: %+v", sub)
	}
	if found, err := l.ReconcilePayment(context.Background(), accepted["payment_operation_id"].(string)); err != nil || !found {
		t.Fatalf("original lookup: %t %v", found, err)
	}
	if err := l.RebuildEntitlements(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, sub = apiRequest(t, h, "GET", "/v1/subscriptions/"+subID, nil, nil)
	if sub["service_status"] != "active" || sub["entitlement_source_revision"] != sub["revision"] {
		t.Fatalf("funded service: %+v", sub)
	}
	_, ent := apiRequest(t, h, "GET", "/v1/entitlements/basic-customer", nil, nil)
	items := ent["entitlements"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["status"] != "active" {
		t.Fatalf("entitlements: %+v", ent)
	}
	_, invoice := apiRequest(t, h, "GET", "/v1/invoices/"+accepted["invoice_id"].(string), nil, nil)
	if invoice["original_minor"] != float64(2000) || len(invoice["lines"].([]any)) != 1 {
		t.Fatalf("invoice: %+v", invoice)
	}
}

func TestV1LegacyClientRejectsAdminPublishedUnshownCharge(t *testing.T) {
	l, clock, h := openAPI(t)
	ctx := context.Background()
	if err := l.InitAdmin(ctx); err != nil {
		t.Fatal(err)
	}
	effective := clock.UTC().Format(time.RFC3339Nano)
	for _, item := range []struct {
		action  string
		key     string
		payload string
	}{
		{"C19", "admin-ai-meter", `{"id":"ai_tokens_admin","source":"ai_gateway","unit":"token","schema_version":"1"}`},
		{"C20", "admin-ai-price", `{"id":"ai_admin_v1","plan_id":"ai_admin","version":"1","fixed_minor":"3000","seat_minor":"0","meter_id":"ai_tokens_admin","included_quantity":"100","usage_rate_num":"1","usage_rate_den":"5","effective_from":"` + effective + `"}`},
		{"C21", "admin-ai-selection", `{"plan_id":"ai_admin","cohort":"default","effective_at":"` + effective + `","price_version_id":"ai_admin_v1"}`},
	} {
		payload := json.RawMessage(item.payload)
		preview, err := l.AdminCreatePreview(ctx, "local-admin", item.action, "", payload)
		if err != nil {
			t.Fatalf("%s preview: %v", item.action, err)
		}
		command, _, err := l.AdminSubmitCommand(ctx, "local-admin", item.key, item.action, "", payload, preview.ID)
		if err != nil {
			t.Fatalf("%s submit: %v", item.action, err)
		}
		command, err = l.AdminExecuteCommand(ctx, command.ID)
		if err != nil || command.Status != "succeeded" {
			t.Fatalf("%s execute: %+v err=%v", item.action, command, err)
		}
	}
	status, quote := apiRequest(t, h, "POST", "/v1/quotes", map[string]any{"customer_id": "ai-admin-customer", "plan_id": "ai_admin"}, nil)
	if status != 201 || quote["requires_client_capability"] != true || quote["due_now_minor"] != float64(3000) {
		t.Fatalf("admin-published quote: %d %+v", status, quote)
	}
	components, ok := quote["components"].([]any)
	if !ok || len(components) != 3 {
		t.Fatalf("admin-published quote omitted fee disclosure: %+v", quote["components"])
	}
	body := map[string]any{"quote_id": quote["quote_id"], "context_fingerprint": quote["context_fingerprint"]}
	status, rejected := apiRequest(t, h, "POST", "/v1/subscriptions", body, map[string]string{"Idempotency-Key": "admin-ai-buy"})
	if status != 409 || rejected["error"] != "CLIENT_CAPABILITY_REQUIRED" {
		t.Fatalf("legacy client accepted unshown admin fee: %d %+v", status, rejected)
	}
	state, err := l.State(ctx)
	if err != nil || len(state.Subscriptions) != 0 {
		t.Fatalf("legacy rejection wrote subscription: count=%d err=%v", len(state.Subscriptions), err)
	}
	status, accepted := apiRequest(t, h, "POST", "/v1/subscriptions", body, map[string]string{"Idempotency-Key": "admin-ai-buy", "X-Client-Capabilities": "meter:ai_tokens_admin"})
	if status != 201 || accepted["status"] != "pending" {
		t.Fatalf("capable client could not accept disclosed admin fee: %d %+v", status, accepted)
	}
}

func TestV1LegacyClientRejectsUnshownAITokenCharge(t *testing.T) {
	l, clock, h := openAPI(t)
	ctx := context.Background()
	if err := l.RegisterMeter(ctx, lab.MeterSpec{ID: "ai_tokens", Source: "ai_gateway", Unit: "token", SchemaVersion: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.PublishMeteredPrice(ctx, lab.MeteredPriceSpec{ID: "ai_v1", PlanID: "ai", Version: 1, FixedMinor: 3000, MeterID: "ai_tokens", IncludedQuantity: 100, UsageRateNum: 1, UsageRateDen: 5, EffectiveFrom: *clock}); err != nil {
		t.Fatal(err)
	}
	if err := l.SelectCatalogPrice(ctx, "ai", "default", *clock, "ai_v1"); err != nil {
		t.Fatal(err)
	}
	status, q := apiRequest(t, h, "POST", "/v1/quotes", map[string]any{"customer_id": "ai-customer", "plan_id": "ai"}, nil)
	if status != 201 || q["requires_client_capability"] != true || q["due_now_minor"] != float64(3000) {
		t.Fatalf("AI quote: %d %+v", status, q)
	}
	components := q["components"].([]any)
	if len(components) != 3 {
		t.Fatalf("missing fee disclosure: %+v", components)
	}
	body := map[string]any{"quote_id": q["quote_id"], "context_fingerprint": q["context_fingerprint"]}
	status, rejected := apiRequest(t, h, "POST", "/v1/subscriptions", body, map[string]string{"Idempotency-Key": "ai-buy"})
	if status != 409 || rejected["error"] != "CLIENT_CAPABILITY_REQUIRED" {
		t.Fatalf("legacy accepted unknown fee: %d %+v", status, rejected)
	}
	state, err := l.State(ctx)
	if err != nil || len(state.Subscriptions) != 0 {
		t.Fatalf("rejected request created subscription: %d %v", len(state.Subscriptions), err)
	}
	status, accepted := apiRequest(t, h, "POST", "/v1/subscriptions", body, map[string]string{"Idempotency-Key": "ai-buy", "X-Client-Capabilities": "meter:ai_tokens"})
	if status != 201 || accepted["status"] != "pending" {
		t.Fatalf("capable client purchase: %d %+v", status, accepted)
	}
	*clock = time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	status, usage := apiRequest(t, h, "POST", "/v1/usage-events", map[string]any{"events": []any{
		map[string]any{"tenant_id": "other", "subscription_id": accepted["subscription_id"], "source": "ai_gateway", "event_id": "event-1", "meter_id": "ai_tokens", "quantity": 105, "event_at": "2026-09-29T12:00:00Z"},
		map[string]any{"tenant_id": "ai-customer", "subscription_id": accepted["subscription_id"], "source": "ai_gateway", "event_id": "event-1", "meter_id": "ai_tokens", "quantity": 105, "event_at": "2026-09-29T12:00:00Z"},
	}}, nil)
	results := usage["results"].([]any)
	if status != 200 || results[0].(map[string]any)["status"] != "rejected" || results[1].(map[string]any)["status"] != "accepted" {
		t.Fatalf("usage per-item result: %d %+v", status, usage)
	}
}

func TestV1InternalContractRequiresCredentialAndCapability(t *testing.T) {
	_, clock, h := openAPI(t)
	contract := map[string]any{"id": "acme_contract", "customer_id": "acme", "version": 1, "base_price_version_id": "pro-v1", "fixed_minor": 4000, "seat_minor": 700, "effective_from": clock.Format(time.RFC3339), "effective_to": "2026-10-01T00:00:00Z"}
	status, _ := apiRequest(t, h, "POST", "/v1/contracts", contract, nil)
	if status != 403 {
		t.Fatalf("internal contract unauthenticated: %d", status)
	}
	status, _ = apiRequest(t, h, "POST", "/v1/contracts", contract, map[string]string{"X-Lab-Internal-Token": "internal-secret"})
	if status != 201 {
		t.Fatalf("contract publish: %d", status)
	}
	status, q := apiRequest(t, h, "POST", "/v1/quotes", map[string]any{"customer_id": "acme", "contract_version_id": "acme_contract", "seat_quantity": 5}, map[string]string{"X-Lab-Internal-Token": "internal-secret"})
	if status != 201 || q["due_now_minor"] != float64(0) || q["recurring_committed_minor"] != float64(7500) {
		t.Fatalf("Net30 quote: %d %+v", status, q)
	}
	body := map[string]any{"quote_id": q["quote_id"], "context_fingerprint": q["context_fingerprint"]}
	status, rejected := apiRequest(t, h, "POST", "/v1/subscriptions", body, map[string]string{"Idempotency-Key": "acme-buy", "X-Lab-Internal-Token": "internal-secret"})
	if status != 409 || rejected["error"] != "CLIENT_CAPABILITY_REQUIRED" {
		t.Fatalf("contract capability: %d %+v", status, rejected)
	}
	status, accepted := apiRequest(t, h, "POST", "/v1/subscriptions", body, map[string]string{"Idempotency-Key": "acme-buy", "X-Lab-Internal-Token": "internal-secret", "X-Client-Capabilities": "contract:net30"})
	if status != 201 || accepted["status"] != "active" {
		t.Fatalf("Net30 activation: %d %+v", status, accepted)
	}
}

func activeBasicForChange(t *testing.T, l *lab.Lab, h http.Handler) (string, int64) {
	t.Helper()
	_, q := apiRequest(t, h, "POST", "/v1/quotes", map[string]any{"customer_id": "change-customer", "plan_id": "basic"}, nil)
	_, receipt := apiRequest(t, h, "POST", "/v1/subscriptions", map[string]any{"quote_id": q["quote_id"], "context_fingerprint": q["context_fingerprint"]}, map[string]string{"Idempotency-Key": "change-customer:buy"})
	if _, err := l.DispatchNext(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if err := l.RebuildEntitlements(context.Background()); err != nil {
		t.Fatal(err)
	}
	subID := receipt["subscription_id"].(string)
	_, sub := apiRequest(t, h, "GET", "/v1/subscriptions/"+subID, nil, nil)
	return subID, int64(sub["revision"].(float64))
}

func TestChangeQuoteRejectsDifferentSeatQuantityInDomainTransaction(t *testing.T) {
	for _, mode := range []string{"next_period", "immediate"} {
		t.Run(mode, func(t *testing.T) {
			l, _, h := openAPI(t)
			subID, revision := activeBasicForChange(t, l, h)
			status, quote := apiRequest(t, h, "POST", "/v1/quotes", map[string]any{
				"customer_id": "change-customer", "plan_id": "pro", "seat_quantity": 5,
				"change_subscription_id": subID, "change_mode": mode, "expected_revision": revision,
			}, nil)
			if status != http.StatusCreated {
				t.Fatalf("change quote: %d %+v", status, quote)
			}
			quoteID := quote["quote_id"].(string)
			fingerprint := quote["context_fingerprint"].(string)
			var err error
			if mode == "next_period" {
				_, err = l.ScheduleNextPlanAtPrice(context.Background(), subID, "pro", 6, revision, "wrong-seats", quoteID, fingerprint)
			} else {
				_, err = l.RequestImmediateProUpgradeAtPrice(context.Background(), subID, 6, revision, "wrong-seats", quoteID, fingerprint)
			}
			if err != lab.ErrConflict {
				t.Fatalf("changed quoted seats: want conflict, got %v", err)
			}
			_, sub := apiRequest(t, h, "GET", "/v1/subscriptions/"+subID, nil, nil)
			if got := int64(sub["revision"].(float64)); got != revision {
				t.Fatalf("rejected change advanced revision from %d to %d", revision, got)
			}
		})
	}
}

func TestChangeQuoteShowsActualPaymentTiming(t *testing.T) {
	for _, tc := range []struct {
		mode      string
		dueNow    float64
		estimated bool
	}{
		{mode: "next_period", dueNow: 0},
		{mode: "immediate", dueNow: 8000, estimated: true},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			l, now, h := openAPI(t)
			subID, revision := activeBasicForChange(t, l, h)
			status, quote := apiRequest(t, h, "POST", "/v1/quotes", map[string]any{
				"customer_id": "change-customer", "plan_id": "pro", "seat_quantity": 5,
				"change_subscription_id": subID, "change_mode": tc.mode, "expected_revision": revision,
			}, nil)
			if status != http.StatusCreated || quote["due_now_minor"] != tc.dueNow || quote["due_now_estimated"] != tc.estimated || quote["recurring_committed_minor"] != float64(10000) {
				t.Fatalf("change quote payment timing: %d %+v", status, quote)
			}
			if tc.mode == "immediate" {
				*now = now.Add(5 * time.Minute)
				body := map[string]any{"quote_id": quote["quote_id"], "context_fingerprint": quote["context_fingerprint"], "change_mode": tc.mode, "expected_revision": revision}
				status, result := apiRequest(t, h, "POST", "/v1/subscriptions/"+subID+"/changes", body, map[string]string{"Idempotency-Key": "changed-after-preview"})
				if status != http.StatusOK || result["pending_amount_minor"].(float64) >= tc.dueNow {
					t.Fatalf("execution did not recalculate after clock advanced: %d %+v", status, result)
				}
			}
		})
	}
}

func TestChangeQuoteCannotReusePurchaseOrExpiredQuote(t *testing.T) {
	l, now, h := openAPI(t)
	subID, revision := activeBasicForChange(t, l, h)
	status, purchaseQuote := apiRequest(t, h, "POST", "/v1/quotes", map[string]any{"customer_id": "change-customer", "plan_id": "pro", "seat_quantity": 5}, nil)
	if status != http.StatusCreated {
		t.Fatalf("purchase quote: %d %+v", status, purchaseQuote)
	}
	status, receipt := apiRequest(t, h, "POST", "/v1/subscriptions", map[string]any{"quote_id": purchaseQuote["quote_id"], "context_fingerprint": purchaseQuote["context_fingerprint"]}, map[string]string{"Idempotency-Key": "second-purchase"})
	if status != http.StatusCreated {
		t.Fatalf("accept purchase quote: %d %+v", status, receipt)
	}
	if _, err := l.BindChangeQuote(context.Background(), purchaseQuote["quote_id"].(string), subID, "next_period", revision); err != lab.ErrConflict {
		t.Fatalf("purchase quote reused for change: %v", err)
	}
	status, expiredQuote := apiRequest(t, h, "POST", "/v1/quotes", map[string]any{"customer_id": "change-customer", "plan_id": "pro", "seat_quantity": 5}, nil)
	if status != http.StatusCreated {
		t.Fatalf("new quote: %d %+v", status, expiredQuote)
	}
	*now = now.Add(16 * time.Minute)
	if _, err := l.BindChangeQuote(context.Background(), expiredQuote["quote_id"].(string), subID, "next_period", revision); err != lab.ErrConflict {
		t.Fatalf("expired quote bound for change: %v", err)
	}
}

func TestV1ChangeQuotePinsRevisionAndTargetPrice(t *testing.T) {
	t.Run("scheduled price", func(t *testing.T) {
		l, _, h := openAPI(t)
		subID, revision := activeBasicForChange(t, l, h)
		status, q := apiRequest(t, h, "POST", "/v1/quotes", map[string]any{"customer_id": "change-customer", "plan_id": "pro", "seat_quantity": 5, "change_subscription_id": subID, "change_mode": "next_period", "expected_revision": revision}, nil)
		if status != 201 || q["change_subscription_id"] != subID {
			t.Fatalf("change quote: %d %+v", status, q)
		}
		body := map[string]any{"quote_id": q["quote_id"], "context_fingerprint": q["context_fingerprint"]}
		status, rejected := apiRequest(t, h, "POST", "/v1/subscriptions", body, map[string]string{"Idempotency-Key": "wrong-operation"})
		if status != 409 || rejected["error"] != "QUOTE_BOUND_TO_CHANGE" {
			t.Fatalf("change quote used for purchase: %d %+v", status, rejected)
		}
		body["expected_revision"] = revision
		body["change_mode"] = "next_period"
		status, result := apiRequest(t, h, "POST", "/v1/subscriptions/"+subID+"/changes", body, map[string]string{"Idempotency-Key": "change-next"})
		if status != 200 || result["requested_price_version_id"] != "pro-v1" || result["status"] != "scheduled" {
			t.Fatalf("schedule: %d %+v", status, result)
		}
		status, replay := apiRequest(t, h, "POST", "/v1/subscriptions/"+subID+"/changes", body, map[string]string{"Idempotency-Key": "change-next"})
		if status != 200 || replay["change_id"] != result["change_id"] {
			t.Fatalf("schedule replay: %d %+v", status, replay)
		}
	})
	t.Run("future selection changed", func(t *testing.T) {
		l, _, h := openAPI(t)
		subID, revision := activeBasicForChange(t, l, h)
		_, q := apiRequest(t, h, "POST", "/v1/quotes", map[string]any{"customer_id": "change-customer", "plan_id": "pro", "seat_quantity": 5, "change_subscription_id": subID, "change_mode": "next_period", "expected_revision": revision}, nil)
		boundary := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
		if _, err := l.PublishProPrice(context.Background(), lab.ProPriceSpec{ID: "pro-v2", Version: 2, FixedMinor: 6000, SeatMinor: 1200, IncludedTasks: 20000, UsageRateNum: 1, UsageRateDen: 10, EffectiveFrom: boundary}); err != nil {
			t.Fatal(err)
		}
		if err := l.SelectCatalogPrice(context.Background(), "pro", "default", boundary, "pro-v2"); err != nil {
			t.Fatal(err)
		}
		body := map[string]any{"quote_id": q["quote_id"], "context_fingerprint": q["context_fingerprint"], "expected_revision": revision, "change_mode": "next_period"}
		status, _ := apiRequest(t, h, "POST", "/v1/subscriptions/"+subID+"/changes", body, map[string]string{"Idempotency-Key": "changed-selection"})
		if status != 409 {
			t.Fatalf("stale quoted price scheduled: %d", status)
		}
		state, err := l.State(context.Background())
		if err != nil || len(state.Schedules) != 0 || state.Subscriptions[0].Revision != revision {
			t.Fatalf("failed schedule changed source: %+v %v", state.Schedules, err)
		}
	})
}

func TestV1GraceAndCorrectionReads(t *testing.T) {
	l, clock, h := openAPI(t)
	_, q := apiRequest(t, h, "POST", "/v1/quotes", map[string]any{"customer_id": "grace-customer", "plan_id": "basic"}, nil)
	_, receipt := apiRequest(t, h, "POST", "/v1/subscriptions", map[string]any{"quote_id": q["quote_id"], "context_fingerprint": q["context_fingerprint"]}, map[string]string{"Idempotency-Key": "grace-buy"})
	ctx := context.Background()
	if _, err := l.DispatchNext(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if err := l.RebuildEntitlements(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := l.PostReduction(ctx, receipt["invoice_id"].(string), 400, "service correction", "api-correction"); err != nil {
		t.Fatal(err)
	}
	_, invoice := apiRequest(t, h, "GET", "/v1/invoices/"+receipt["invoice_id"].(string), nil, nil)
	if invoice["original_minor"] != float64(2000) || invoice["obligation_minor"] != float64(1600) || len(invoice["corrections"].([]any)) != 1 {
		t.Fatalf("corrected invoice: %+v", invoice)
	}
	*clock = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if _, err := l.RunRenewals(ctx); err != nil {
		t.Fatal(err)
	}
	if err := l.RefreshEntitlements(ctx); err != nil {
		t.Fatal(err)
	}
	_, sub := apiRequest(t, h, "GET", "/v1/subscriptions/"+receipt["subscription_id"].(string), nil, nil)
	if sub["service_status"] != "grace" {
		t.Fatalf("renewal grace not visible: %+v", sub)
	}
}

func TestV1InternalAccountAdapterFollowsReadCutover(t *testing.T) {
	l, _, h := openAPI(t)
	ctx := context.Background()
	_, q := apiRequest(t, h, "POST", "/v1/quotes", map[string]any{"customer_id": "mapped-customer", "plan_id": "basic"}, nil)
	_, receipt := apiRequest(t, h, "POST", "/v1/subscriptions", map[string]any{"quote_id": q["quote_id"], "context_fingerprint": q["context_fingerprint"]}, map[string]string{"Idempotency-Key": "mapped-buy"})
	if _, err := l.DispatchNext(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if err := l.RebuildEntitlements(ctx); err != nil {
		t.Fatal(err)
	}
	subID := receipt["subscription_id"].(string)
	if _, err := l.LinkLegacyAccount(ctx, lab.AccountLink{LegacyAccountID: "old-account", CustomerID: "mapped-customer", BeneficiaryID: "mapped-customer", Cohort: "default", HasHistory: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.ShadowQuote(ctx, "old-account", "basic", 0, 2000, "USD"); err != nil {
		t.Fatal(err)
	}
	if _, err := l.ShadowEntitlement(ctx, "old-account", subID, "active"); err != nil {
		t.Fatal(err)
	}
	backfill, err := l.BackfillLegacyProvenance(ctx, lab.LegacyProvenance{LegacyInvoiceID: "old-invoice", LegacySubscriptionID: "old-sub", LegacyAccountID: "old-account", CommerceSubscriptionID: subID, CommerceInvoiceID: receipt["invoice_id"].(string), PriceVersionID: "basic-v1"})
	if err != nil || backfill.Status != "complete" {
		t.Fatalf("backfill: %+v %v", backfill, err)
	}
	if _, err := l.RunReconciliation(ctx, l.ClockTime()); err != nil {
		t.Fatal(err)
	}
	path := "/v1/internal/accounts/old-account/entitlements/" + subID
	status, _ := apiRequest(t, h, "GET", path, nil, nil)
	if status != 403 {
		t.Fatalf("adapter leaked without credential: %d", status)
	}
	status, before := apiRequest(t, h, "GET", path, nil, map[string]string{"X-Lab-Internal-Token": "internal-secret"})
	if status != 200 || before["read_owner"] != "legacy" || before["status"] != "active" {
		t.Fatalf("legacy adapter: %d %+v", status, before)
	}
	if _, err := l.SwitchAccountRead(ctx, "old-account", lab.MigrationThresholds{MaxQuoteP95Millis: 5000, MaxUnknownPayments: 0, MaxOpenDiscrepancies: 0}); err != nil {
		t.Fatal(err)
	}
	status, after := apiRequest(t, h, "GET", path, nil, map[string]string{"X-Lab-Internal-Token": "internal-secret"})
	if status != 200 || after["read_owner"] != "commerce" || after["status"] != "active" {
		t.Fatalf("commerce adapter: %d %+v", status, after)
	}
}
