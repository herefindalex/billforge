package api

import (
	"database/sql"
	"errors"
	"net/http"

	"billforge/lab"
)

func (s *Server) changeSubscription(w http.ResponseWriter, r *http.Request) {
	var body struct {
		QuoteID            string `json:"quote_id"`
		ContextFingerprint string `json:"context_fingerprint"`
		ExpectedRevision   int64  `json:"expected_revision"`
		ChangeMode         string `json:"change_mode"`
	}
	key := r.Header.Get("Idempotency-Key")
	if err := decode(r, &body); err != nil || key == "" || body.QuoteID == "" || body.ContextFingerprint == "" || body.ExpectedRevision <= 0 || (body.ChangeMode != "next_period" && body.ChangeMode != "immediate") {
		fail(w, 400, "INVALID_REQUEST", "quote, fingerprint, revision, mode and Idempotency-Key required")
		return
	}
	binding, err := s.lab.ChangeQuoteBinding(r.Context(), body.QuoteID)
	if err != nil {
		domainError(w, err)
		return
	}
	if binding.SubscriptionID != r.PathValue("id") || binding.Mode != body.ChangeMode || binding.ExpectedRevision != body.ExpectedRevision || binding.Fingerprint != body.ContextFingerprint {
		fail(w, 409, "QUOTE_CONTEXT_CONFLICT", "change quote context differs")
		return
	}
	state, err := s.lab.State(r.Context())
	if err != nil {
		domainError(w, err)
		return
	}
	var q lab.Quote
	found := false
	for _, item := range state.Quotes {
		if item.ID == body.QuoteID {
			q = item.Quote
			found = true
			break
		}
	}
	if !found {
		fail(w, 404, "NOT_FOUND", "quote not found")
		return
	}
	response, err := s.quoteResponse(r.Context(), q)
	if err != nil {
		domainError(w, err)
		return
	}
	capabilities := capabilitySet(r.Header.Get("X-Client-Capabilities"))
	for _, required := range response.RequiredClientCapabilities {
		if !capabilities[required] {
			fail(w, 409, "CLIENT_CAPABILITY_REQUIRED", "client must display and accept "+required)
			return
		}
	}
	planID := ""
	for _, price := range state.Prices {
		if price.ID == q.PriceVersionID {
			planID = price.PlanID
			break
		}
	}
	if planID == "" {
		domainError(w, sql.ErrNoRows)
		return
	}
	if body.ChangeMode == "next_period" {
		result, err := s.lab.ScheduleNextPlanAtPrice(r.Context(), binding.SubscriptionID, planID, q.SeatQuantity, binding.ExpectedRevision, key, q.ID, binding.Fingerprint)
		if err != nil {
			domainError(w, err)
			return
		}
		write(w, 200, map[string]any{"change_id": result.ID, "subscription_id": binding.SubscriptionID, "requested_price_version_id": result.TargetPriceVersionID, "effective_at": result.EffectiveAt, "status": result.Status, "revision": result.Revision})
		return
	}
	if planID != "pro" {
		fail(w, 409, "UNSUPPORTED_CHANGE", "immediate change supports the Pro upgrade policy")
		return
	}
	result, err := s.lab.RequestImmediateProUpgradeAtPrice(r.Context(), binding.SubscriptionID, q.SeatQuantity, binding.ExpectedRevision, key, q.ID, binding.Fingerprint)
	if err != nil {
		if errors.Is(err, lab.ErrPaymentUnknown) {
			fail(w, 202, "PAYMENT_PENDING_VERIFICATION", "payment outcome must be checked")
			return
		}
		domainError(w, err)
		return
	}
	write(w, 200, map[string]any{"change_id": result.ID, "subscription_id": binding.SubscriptionID, "requested_price_version_id": result.TargetPriceVersionID, "pending_amount_minor": result.QuotedAmountMinor, "status": result.Status})
}
