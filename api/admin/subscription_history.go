package admin

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"billforge/lab"
)

func historyLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	if !validateQueryKeys(w, r, "INVALID_FILTER", "limit", "cursor") {
		return 0, false
	}
	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			apiError(w, http.StatusBadRequest, "INVALID_LIMIT", "Limit must be 1–100")
			return 0, false
		}
		limit = parsed
	}
	return limit, true
}

func (s *Server) subscriptionPeriods(w http.ResponseWriter, r *http.Request) {
	limit, ok := historyLimit(w, r)
	if !ok {
		return
	}
	var before *int
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(raw)
		if err != nil {
			apiError(w, http.StatusBadRequest, "INVALID_CURSOR", "Invalid period cursor")
			return
		}
		index, err := strconv.Atoi(string(decoded))
		if err != nil || index < 0 || strconv.Itoa(index) != string(decoded) {
			apiError(w, http.StatusBadRequest, "INVALID_CURSOR", "Invalid period cursor")
			return
		}
		before = &index
	}
	page, err := s.lab.AdminSubscriptionPeriods(r.Context(), r.PathValue("id"), before, limit)
	if errors.Is(err, sql.ErrNoRows) {
		apiError(w, http.StatusNotFound, "NOT_FOUND", "Subscription not found")
		return
	}
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not load billing periods")
		return
	}
	safe, err := safeValue(page.Items)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not encode billing periods")
		return
	}
	next := ""
	if page.NextIndex != nil {
		next = base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(*page.NextIndex)))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": safe, "next_cursor": next, "observed_at": time.Now().UTC().Format(time.RFC3339Nano)})
}

func (s *Server) subscriptionTimeline(w http.ResponseWriter, r *http.Request) {
	limit, ok := historyLimit(w, r)
	if !ok {
		return
	}
	var after *lab.AdminTimelineCursor
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(raw)
		if err != nil || len(decoded) == 0 {
			apiError(w, http.StatusBadRequest, "INVALID_CURSOR", "Invalid timeline cursor")
			return
		}
		var cursor lab.AdminTimelineCursor
		if err := json.Unmarshal(decoded, &cursor); err != nil || cursor.AtNano == 0 || cursor.Kind == "" {
			apiError(w, http.StatusBadRequest, "INVALID_CURSOR", "Invalid timeline cursor")
			return
		}
		after = &cursor
	}
	page, err := s.lab.AdminSubscriptionTimeline(r.Context(), r.PathValue("id"), after, limit)
	if errors.Is(err, sql.ErrNoRows) {
		apiError(w, http.StatusNotFound, "NOT_FOUND", "Subscription not found")
		return
	}
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not load subscription timeline")
		return
	}
	safe, err := safeValue(page.Items)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not encode subscription timeline")
		return
	}
	next := ""
	if page.Next != nil {
		encoded, err := json.Marshal(page.Next)
		if err != nil {
			apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not encode timeline cursor")
			return
		}
		next = base64.RawURLEncoding.EncodeToString(encoded)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": safe, "next_cursor": next, "observed_at": time.Now().UTC().Format(time.RFC3339Nano)})
}

func (s *Server) subscriptionEntitlement(w http.ResponseWriter, r *http.Request) {
	item, err := s.lab.AdminSubscriptionEntitlement(r.Context(), r.PathValue("id"))
	if errors.Is(err, sql.ErrNoRows) {
		apiError(w, http.StatusNotFound, "NOT_FOUND", "Subscription not found")
		return
	}
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not load entitlement")
		return
	}
	safe, err := safeValue(item)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not encode entitlement")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entitlement": safe, "observed_at": time.Now().UTC().Format(time.RFC3339Nano)})
}
