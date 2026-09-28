package api

import (
	"net/http"
	"time"

	"billforge/lab"
)

func (s *Server) publishContract(w http.ResponseWriter, r *http.Request) {
	if !s.requireInternal(w, r) {
		return
	}
	var body struct {
		ID                         string    `json:"id"`
		CustomerID                 string    `json:"customer_id"`
		Version                    int64     `json:"version"`
		BasePriceVersionID         string    `json:"base_price_version_id"`
		FixedMinor                 int64     `json:"fixed_minor"`
		SeatMinor                  int64     `json:"seat_minor"`
		EffectiveFrom              time.Time `json:"effective_from"`
		EffectiveTo                time.Time `json:"effective_to"`
		PostContractPriceVersionID string    `json:"post_contract_price_version_id"`
	}
	if err := decode(r, &body); err != nil {
		fail(w, 400, "INVALID_REQUEST", "invalid contract payload")
		return
	}
	result, err := s.lab.PublishContract(r.Context(), lab.ContractSpec{
		ID: body.ID, CustomerID: body.CustomerID, Version: body.Version, BasePriceVersionID: body.BasePriceVersionID,
		FixedMinor: body.FixedMinor, SeatMinor: body.SeatMinor, EffectiveFrom: body.EffectiveFrom, EffectiveTo: body.EffectiveTo,
		PostContractPriceVersionID: body.PostContractPriceVersionID,
	})
	if err != nil {
		domainError(w, err)
		return
	}
	write(w, 201, result)
}

func (s *Server) createMigration(w http.ResponseWriter, r *http.Request) {
	if !s.requireInternal(w, r) {
		return
	}
	var body struct {
		ID                   string   `json:"id"`
		Cohort               string   `json:"cohort"`
		TargetPriceVersionID string   `json:"target_price_version_id"`
		SubscriptionIDs      []string `json:"subscription_ids"`
		PreviewOnly          bool     `json:"preview_only"`
	}
	if err := decode(r, &body); err != nil || body.TargetPriceVersionID == "" || len(body.SubscriptionIDs) == 0 {
		fail(w, 400, "INVALID_REQUEST", "target price and subscription IDs required")
		return
	}
	if body.PreviewOnly {
		items, err := s.lab.PreviewPriceMigration(r.Context(), body.TargetPriceVersionID, body.SubscriptionIDs)
		if err != nil {
			domainError(w, err)
			return
		}
		write(w, 200, map[string]any{"items": items, "preview_only": true})
		return
	}
	if body.ID == "" || body.Cohort == "" {
		fail(w, 400, "INVALID_REQUEST", "id and cohort required")
		return
	}
	result, err := s.lab.PlanPriceMigration(r.Context(), body.ID, body.Cohort, body.TargetPriceVersionID, body.SubscriptionIDs)
	if err != nil {
		domainError(w, err)
		return
	}
	write(w, 201, result)
}

func (s *Server) repair(w http.ResponseWriter, r *http.Request) {
	if !s.requireInternal(w, r) {
		return
	}
	var body struct {
		DiscrepancyID string `json:"discrepancy_id"`
	}
	key := r.Header.Get("Idempotency-Key")
	if err := decode(r, &body); err != nil || body.DiscrepancyID == "" || key == "" {
		fail(w, 400, "INVALID_REQUEST", "discrepancy ID and Idempotency-Key required")
		return
	}
	op, err := s.lab.RepairDiscrepancy(r.Context(), body.DiscrepancyID, key)
	if err != nil {
		domainError(w, err)
		return
	}
	write(w, 200, op)
}

func (s *Server) accountAdapterEntitlement(w http.ResponseWriter, r *http.Request) {
	if !s.requireInternal(w, r) {
		return
	}
	result, err := s.lab.ReadAccountEntitlement(r.Context(), r.PathValue("account"), r.PathValue("subscription"))
	if err != nil {
		domainError(w, err)
		return
	}
	write(w, 200, map[string]any{"read_owner": result.Owner, "status": result.Status, "source_revision": result.SourceRevision, "as_of": result.AsOf})
}
