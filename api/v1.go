package api

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"billforge/lab"
)

type Server struct {
	lab           *lab.Lab
	internalToken string
}

func New(l *lab.Lab, internalToken string) http.Handler {
	s := &Server{lab: l, internalToken: internalToken}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/quotes", s.createQuote)
	mux.HandleFunc("POST /v1/subscriptions", s.acceptQuote)
	mux.HandleFunc("GET /v1/subscriptions/{id}", s.getSubscription)
	mux.HandleFunc("POST /v1/subscriptions/{id}/changes", s.changeSubscription)
	mux.HandleFunc("POST /v1/usage-events", s.recordUsage)
	mux.HandleFunc("GET /v1/entitlements/{beneficiary}", s.getEntitlements)
	mux.HandleFunc("GET /v1/invoices/{id}", s.getInvoice)
	mux.HandleFunc("POST /v1/contracts", s.publishContract)
	mux.HandleFunc("POST /v1/migrations", s.createMigration)
	mux.HandleFunc("POST /v1/repairs", s.repair)
	mux.HandleFunc("GET /v1/internal/accounts/{account}/entitlements/{subscription}", s.accountAdapterEntitlement)
	return mux
}

func write(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func fail(w http.ResponseWriter, status int, code, message string) {
	write(w, status, map[string]string{"error": code, "message": message})
}

func domainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		fail(w, http.StatusNotFound, "NOT_FOUND", "resource not found")
	case errors.Is(err, lab.ErrConflict):
		fail(w, http.StatusConflict, "CONFLICT", "source state or request conflicts")
	case errors.Is(err, lab.ErrPaymentUnknown):
		fail(w, http.StatusAccepted, "PAYMENT_PENDING_VERIFICATION", "query the original payment operation")
	default:
		fail(w, http.StatusInternalServerError, "INTERNAL_ERROR", "operation failed")
	}
}

func decode(r *http.Request, dst any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON content")
	}
	return nil
}

func (s *Server) requireInternal(w http.ResponseWriter, r *http.Request) bool {
	if !s.internalAllowed(r) {
		fail(w, http.StatusForbidden, "INTERNAL_ACCESS_REQUIRED", "internal credential required")
		return false
	}
	return true
}

func (s *Server) internalAllowed(r *http.Request) bool {
	provided := r.Header.Get("X-Lab-Internal-Token")
	return s.internalToken != "" && subtle.ConstantTimeCompare([]byte(provided), []byte(s.internalToken)) == 1
}

type QuoteComponent struct {
	Kind         string `json:"kind"`
	DisplayLabel string `json:"display_label"`
	AmountMinor  int64  `json:"amount_minor,omitempty"`
	Quantity     int64  `json:"quantity,omitempty"`
	RateNum      int64  `json:"rate_num,omitempty"`
	RateDen      int64  `json:"rate_den,omitempty"`
	MeterID      string `json:"meter_id,omitempty"`
}

type QuoteResponse struct {
	QuoteID                    string           `json:"quote_id"`
	CustomerID                 string           `json:"customer_id"`
	PriceVersionID             string           `json:"price_version_id"`
	PriceChecksum              string           `json:"price_checksum"`
	ContractVersionID          string           `json:"contract_version_id,omitempty"`
	ContractChecksum           string           `json:"contract_checksum,omitempty"`
	Currency                   string           `json:"currency"`
	DueNowMinor                int64            `json:"due_now_minor"`
	DueNowEstimated            bool             `json:"due_now_estimated"`
	RecurringCommittedMinor    int64            `json:"recurring_committed_minor"`
	SeatQuantity               int64            `json:"seat_quantity"`
	ExpiresAt                  time.Time        `json:"expires_at"`
	ContextFingerprint         string           `json:"context_fingerprint"`
	Components                 []QuoteComponent `json:"components"`
	RequiredClientCapabilities []string         `json:"required_client_capabilities"`
	RequiresClientCapability   bool             `json:"requires_client_capability"`
	TaxStatus                  string           `json:"tax_status"`
	ChangeSubscriptionID       string           `json:"change_subscription_id,omitempty"`
	ChangeMode                 string           `json:"change_mode,omitempty"`
	ExpectedRevision           int64            `json:"expected_revision,omitempty"`
}

func (s *Server) quoteResponse(ctx context.Context, q lab.Quote) (QuoteResponse, error) {
	terms, err := s.lab.PriceTerms(ctx, q.PriceVersionID)
	if err != nil {
		return QuoteResponse{}, err
	}
	fixed, seat := terms.FixedMinor, terms.SeatMinor
	contractChecksum := ""
	if q.ContractVersionID != "" {
		state, err := s.lab.State(ctx)
		if err != nil {
			return QuoteResponse{}, err
		}
		found := false
		for _, c := range state.Contracts {
			if c.ID == q.ContractVersionID {
				fixed, seat, contractChecksum = c.FixedMinor, c.SeatMinor, c.Checksum
				found = true
				break
			}
		}
		if !found {
			return QuoteResponse{}, sql.ErrNoRows
		}
	}
	components := []QuoteComponent{{Kind: "fixed", DisplayLabel: "每期固定費", AmountMinor: fixed}}
	if seat > 0 {
		components = append(components, QuoteComponent{Kind: "per_seat", DisplayLabel: "每期每席費", AmountMinor: seat, Quantity: q.SeatQuantity})
	}
	var required []string
	if terms.MeterID != "" {
		components = append(components,
			QuoteComponent{Kind: "included_quantity", DisplayLabel: "每期內含 " + terms.MeterID, Quantity: terms.IncludedQuantity, MeterID: terms.MeterID},
			QuoteComponent{Kind: "usage_overage", DisplayLabel: "超額 " + terms.MeterID, RateNum: terms.UsageRateNum, RateDen: terms.UsageRateDen, MeterID: terms.MeterID})
		if terms.MeterID != "tasks" {
			required = append(required, "meter:"+terms.MeterID)
		}
	}
	if q.ContractVersionID != "" {
		required = append(required, "contract:net30")
	}
	dueNow := q.AmountMinor
	if q.ContractVersionID != "" {
		dueNow = 0
	}
	result := QuoteResponse{
		QuoteID: q.ID, CustomerID: q.CustomerID, PriceVersionID: q.PriceVersionID,
		PriceChecksum: terms.Checksum, ContractVersionID: q.ContractVersionID, ContractChecksum: contractChecksum,
		Currency: q.Currency, DueNowMinor: dueNow, RecurringCommittedMinor: q.AmountMinor,
		SeatQuantity: q.SeatQuantity, ExpiresAt: q.ExpiresAt, ContextFingerprint: q.Fingerprint,
		Components: components, RequiredClientCapabilities: required, RequiresClientCapability: len(required) > 0,
		TaxStatus: "unsupported",
	}
	binding, err := s.lab.ChangeQuoteBinding(ctx, q.ID)
	if err == nil {
		result.ContextFingerprint = binding.Fingerprint
		result.ChangeSubscriptionID = binding.SubscriptionID
		result.ChangeMode = binding.Mode
		result.ExpectedRevision = binding.ExpectedRevision
		if binding.Mode == "next_period" {
			result.DueNowMinor = 0
		} else {
			result.DueNowMinor, err = s.lab.EstimateImmediateProUpgrade(ctx, binding.SubscriptionID, q.PriceVersionID, q.SeatQuantity, binding.ExpectedRevision)
			if err != nil {
				return QuoteResponse{}, err
			}
			result.DueNowEstimated = true
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return QuoteResponse{}, err
	}
	return result, nil
}

func (s *Server) createQuote(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CustomerID           string `json:"customer_id"`
		PlanID               string `json:"plan_id"`
		Cohort               string `json:"cohort"`
		SeatQuantity         int64  `json:"seat_quantity"`
		ContractVersionID    string `json:"contract_version_id"`
		ChangeSubscriptionID string `json:"change_subscription_id"`
		ChangeMode           string `json:"change_mode"`
		ExpectedRevision     int64  `json:"expected_revision"`
	}
	if err := decode(r, &body); err != nil || body.CustomerID == "" || body.SeatQuantity < 0 {
		fail(w, http.StatusBadRequest, "INVALID_REQUEST", "valid customer and seat quantity required")
		return
	}
	var q lab.Quote
	var err error
	if body.ContractVersionID != "" {
		if !s.requireInternal(w, r) {
			return
		}
		q, err = s.lab.CreateContractQuote(r.Context(), body.CustomerID, body.ContractVersionID, body.SeatQuantity)
	} else {
		if body.PlanID == "" {
			fail(w, 400, "INVALID_REQUEST", "plan_id required")
			return
		}
		if body.Cohort == "" {
			body.Cohort = "default"
		}
		q, err = s.lab.CreateQuoteForCohort(r.Context(), body.CustomerID, body.PlanID, body.Cohort, body.SeatQuantity)
	}
	if err != nil {
		domainError(w, err)
		return
	}
	if body.ChangeSubscriptionID != "" {
		if body.ContractVersionID != "" {
			fail(w, 400, "INVALID_REQUEST", "contract change quote unsupported")
			return
		}
		if _, err := s.lab.BindChangeQuote(r.Context(), q.ID, body.ChangeSubscriptionID, body.ChangeMode, body.ExpectedRevision); err != nil {
			domainError(w, err)
			return
		}
	}
	response, err := s.quoteResponse(r.Context(), q)
	if err != nil {
		domainError(w, err)
		return
	}
	write(w, http.StatusCreated, response)
}

func capabilitySet(header string) map[string]bool {
	set := map[string]bool{}
	for _, raw := range strings.Split(header, ",") {
		if x := strings.TrimSpace(raw); x != "" {
			set[x] = true
		}
	}
	return set
}

func (s *Server) acceptQuote(w http.ResponseWriter, r *http.Request) {
	var body struct {
		QuoteID            string `json:"quote_id"`
		ContextFingerprint string `json:"context_fingerprint"`
	}
	key := r.Header.Get("Idempotency-Key")
	if err := decode(r, &body); err != nil || body.QuoteID == "" || body.ContextFingerprint == "" || key == "" {
		fail(w, 400, "INVALID_REQUEST", "quote_id, context_fingerprint and Idempotency-Key required")
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
	if _, err := s.lab.ChangeQuoteBinding(r.Context(), q.ID); err == nil {
		fail(w, 409, "QUOTE_BOUND_TO_CHANGE", "use the subscription change endpoint")
		return
	} else if !errors.Is(err, sql.ErrNoRows) {
		domainError(w, err)
		return
	}
	if q.Fingerprint != body.ContextFingerprint {
		fail(w, 409, "QUOTE_CONTEXT_CONFLICT", "quote fingerprint changed")
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
			fail(w, 409, "CLIENT_CAPABILITY_REQUIRED", fmt.Sprintf("client must display and accept %s", required))
			return
		}
	}
	var receipt lab.Receipt
	if q.ContractVersionID != "" {
		if !s.requireInternal(w, r) {
			return
		}
		receipt, err = s.lab.AcceptContractQuote(r.Context(), q.ID, q.Fingerprint, key)
	} else {
		receipt, err = s.lab.AcceptQuote(r.Context(), q.ID, q.Fingerprint, key)
	}
	if err != nil {
		domainError(w, err)
		return
	}
	current, err := s.lab.State(r.Context())
	if err != nil {
		domainError(w, err)
		return
	}
	status, serviceStatus := "pending", "pending"
	for _, sub := range current.Subscriptions {
		if sub.ID == receipt.SubscriptionID {
			status, serviceStatus = sub.Status, effectiveService(sub)
			break
		}
	}
	write(w, http.StatusCreated, map[string]any{
		"subscription_id": receipt.SubscriptionID, "invoice_id": receipt.InvoiceID,
		"payment_operation_id": receipt.OperationID, "amount_minor": receipt.AmountMinor,
		"currency": receipt.Currency, "status": status, "service_status": serviceStatus,
	})
}
