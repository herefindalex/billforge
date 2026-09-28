package admin

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"
)

func (s *Server) labStatus(w http.ResponseWriter, r *http.Request) {
	if !validateQueryKeys(w, r, "INVALID_FILTER") {
		return
	}
	clock, err := s.lab.AdminClock(r.Context())
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not load lab clock")
		return
	}
	pendingFaults, err := s.lab.AdminPendingFaultCount(r.Context())
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not load fault count")
		return
	}
	provider, providerObservedAt, err := s.lab.AdminProviderCounts(r.Context())
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not load provider status")
		return
	}
	safe, err := safeValue(map[string]any{"clock": clock, "pending_fault_tickets": pendingFaults, "provider": provider})
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not encode lab status")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": safe, "observed_at": time.Now().UTC().Format(time.RFC3339Nano), "provider_observed_at": providerObservedAt.Format(time.RFC3339Nano)})
}

func (s *Server) providerCaptures(w http.ResponseWriter, r *http.Request) {
	s.providerOperations(w, r, "captures")
}

func (s *Server) providerRefunds(w http.ResponseWriter, r *http.Request) {
	s.providerOperations(w, r, "refunds")
}

func (s *Server) providerOperations(w http.ResponseWriter, r *http.Request, kind string) {
	if !validateQueryKeys(w, r, "INVALID_FILTER", "limit", "cursor", "status") {
		return
	}
	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			apiError(w, http.StatusBadRequest, "INVALID_LIMIT", "Limit must be 1–100")
			return
		}
		limit = parsed
	}
	status := r.URL.Query().Get("status")
	if status != "" && status != "succeeded" && status != "definitively_failed" {
		apiError(w, http.StatusBadRequest, "INVALID_FILTER", "Invalid provider status")
		return
	}
	scope := resourceFilterScope("provider-"+kind, map[string]string{"status": status})
	var before int64
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(raw)
		if err != nil {
			apiError(w, http.StatusBadRequest, "INVALID_CURSOR", "Invalid provider cursor")
			return
		}
		var cursor resourcePageCursor
		decoder := json.NewDecoder(bytes.NewReader(decoded))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&cursor) != nil || decoder.Decode(new(any)) != io.EOF || cursor.After < 1 || cursor.Scope != scope {
			apiError(w, http.StatusBadRequest, "INVALID_CURSOR", "Invalid provider cursor")
			return
		}
		before = cursor.After
	}
	items, next, observedAt, err := s.lab.AdminProviderOperationsPage(r.Context(), kind, status, before, limit)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not load provider operations")
		return
	}
	safe, err := safeValue(items)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not encode provider operations")
		return
	}
	nextCursor := ""
	if next > 0 {
		encoded, err := json.Marshal(resourcePageCursor{After: next, Scope: scope})
		if err != nil {
			apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not encode provider cursor")
			return
		}
		nextCursor = base64.RawURLEncoding.EncodeToString(encoded)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": safe, "next_cursor": nextCursor, "observed_at": observedAt.Format(time.RFC3339Nano)})
}
