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

func usagePeriodPath(w http.ResponseWriter, r *http.Request) (string, int, bool) {
	id := r.PathValue("id")
	index, err := strconv.Atoi(r.PathValue("index"))
	if id == "" || err != nil || index < 0 {
		apiError(w, http.StatusBadRequest, "INVALID_PERIOD", "Invalid usage period")
		return "", 0, false
	}
	return id, index, true
}

func (s *Server) usagePeriodDetail(w http.ResponseWriter, r *http.Request) {
	if !validateQueryKeys(w, r, "INVALID_FILTER") {
		return
	}
	id, index, ok := usagePeriodPath(w, r)
	if !ok {
		return
	}
	detail, err := s.lab.AdminUsagePeriodDetail(r.Context(), id, index)
	if errors.Is(err, sql.ErrNoRows) {
		apiError(w, http.StatusNotFound, "NOT_FOUND", "Usage period not found")
		return
	}
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not load usage period")
		return
	}
	safe, err := safeValue(detail)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not encode usage period")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"period": safe, "observed_at": time.Now().UTC().Format(time.RFC3339Nano)})
}

func (s *Server) usagePeriodRatings(w http.ResponseWriter, r *http.Request) {
	limit, ok := historyLimit(w, r)
	if !ok {
		return
	}
	id, index, ok := usagePeriodPath(w, r)
	if !ok {
		return
	}
	var after *lab.AdminUsageRatingCursor
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		if len(raw) > 2048 {
			apiError(w, http.StatusBadRequest, "INVALID_CURSOR", "Invalid usage rating cursor")
			return
		}
		decoded, err := base64.RawURLEncoding.DecodeString(raw)
		if err != nil {
			apiError(w, http.StatusBadRequest, "INVALID_CURSOR", "Invalid usage rating cursor")
			return
		}
		var cursor lab.AdminUsageRatingCursor
		if err := json.Unmarshal(decoded, &cursor); err != nil || cursor.SubscriptionID != id || cursor.PeriodIndex != index || cursor.Revision < 1 {
			apiError(w, http.StatusBadRequest, "INVALID_CURSOR", "Invalid usage rating cursor")
			return
		}
		after = &cursor
	}
	page, err := s.lab.AdminUsagePeriodRatings(r.Context(), id, index, after, limit)
	if errors.Is(err, sql.ErrNoRows) {
		apiError(w, http.StatusNotFound, "NOT_FOUND", "Usage period not found")
		return
	}
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not load usage ratings")
		return
	}
	safe, err := safeValue(page.Items)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not encode usage ratings")
		return
	}
	next := ""
	if page.Next != nil {
		encoded, err := json.Marshal(page.Next)
		if err != nil {
			apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not encode usage rating cursor")
			return
		}
		next = base64.RawURLEncoding.EncodeToString(encoded)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": safe, "next_cursor": next, "observed_at": time.Now().UTC().Format(time.RFC3339Nano)})
}
