package api

import (
	"net/http"

	"billforge/lab"
)

func effectiveService(sub lab.SubscriptionState) string {
	if sub.Status != "active" {
		return "pending"
	}
	if sub.EntitlementSourceRevision != sub.Revision {
		return "pending"
	}
	if sub.EntitlementStatus == "" {
		return "pending"
	}
	return sub.EntitlementStatus
}

func (s *Server) getSubscription(w http.ResponseWriter, r *http.Request) {
	state, err := s.lab.State(r.Context())
	if err != nil {
		domainError(w, err)
		return
	}
	id := r.PathValue("id")
	for _, sub := range state.Subscriptions {
		if sub.ID != id {
			continue
		}
		var schedules []lab.ScheduleResult
		for _, x := range state.Schedules {
			if x.SubscriptionID == id {
				schedules = append(schedules, x)
			}
		}
		var assignments []lab.PricingAssignmentState
		for _, x := range state.Assignments {
			if x.SubscriptionID == id {
				assignments = append(assignments, x)
			}
		}
		var invoices []lab.InvoiceState
		var payments []map[string]any
		for _, x := range state.Invoices {
			if x.SubscriptionID == id {
				invoices = append(invoices, x)
				for _, p := range state.Payments {
					if p.InvoiceID == x.ID {
						item := map[string]any{"payment_operation_id": p.ID, "invoice_id": p.InvoiceID, "amount_minor": p.AmountMinor, "currency": p.Currency, "status": p.Status}
						if s.internalAllowed(r) {
							item["provider_key"] = p.ProviderKey
							item["request_key"] = p.RequestKey
						}
						payments = append(payments, item)
					}
				}
			}
		}
		write(w, 200, map[string]any{
			"subscription_id": sub.ID, "customer_id": sub.CustomerID, "price_version_id": sub.PriceVersionID,
			"seat_quantity": sub.SeatQuantity, "revision": sub.Revision, "source_status": sub.Status,
			"service_status": effectiveService(sub), "entitlement_reason": sub.EntitlementReason,
			"entitlement_source_revision": sub.EntitlementSourceRevision, "as_of": s.lab.ClockTime(),
			"scheduled_changes": schedules, "assignments": assignments, "invoices": invoices, "payments": payments,
		})
		return
	}
	fail(w, 404, "NOT_FOUND", "subscription not found")
}

func (s *Server) getEntitlements(w http.ResponseWriter, r *http.Request) {
	state, err := s.lab.State(r.Context())
	if err != nil {
		domainError(w, err)
		return
	}
	beneficiary := r.PathValue("beneficiary")
	var items []map[string]any
	for _, sub := range state.Subscriptions {
		if sub.CustomerID != beneficiary {
			continue
		}
		items = append(items, map[string]any{
			"subscription_id": sub.ID, "feature": "service", "status": effectiveService(sub),
			"reason": sub.EntitlementReason, "price_version_id": sub.PriceVersionID,
			"source_revision": sub.EntitlementSourceRevision, "subscription_revision": sub.Revision,
		})
	}
	write(w, 200, map[string]any{"beneficiary": beneficiary, "entitlements": items, "as_of": s.lab.ClockTime()})
}

func (s *Server) getInvoice(w http.ResponseWriter, r *http.Request) {
	state, err := s.lab.State(r.Context())
	if err != nil {
		domainError(w, err)
		return
	}
	id := r.PathValue("id")
	for _, invoice := range state.Invoices {
		if invoice.ID != id {
			continue
		}
		lines, err := s.lab.InvoiceLines(r.Context(), id)
		if err != nil {
			domainError(w, err)
			return
		}
		var corrections []lab.CorrectionState
		for _, c := range state.Corrections {
			if c.InvoiceID == id {
				corrections = append(corrections, c)
			}
		}
		write(w, 200, map[string]any{
			"invoice_id": id, "subscription_id": invoice.SubscriptionID, "period_index": invoice.PeriodIndex,
			"currency": invoice.Balance.Currency, "original_minor": invoice.Balance.OriginalMinor,
			"obligation_minor": invoice.Balance.ObligationMinor, "net_applied_minor": invoice.Balance.NetAppliedMinor,
			"outstanding_minor": invoice.Balance.OutstandingMinor, "lines": lines, "corrections": corrections,
			"as_of": s.lab.ClockTime(),
		})
		return
	}
	fail(w, 404, "NOT_FOUND", "invoice not found")
}
