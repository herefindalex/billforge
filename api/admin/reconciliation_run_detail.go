package admin

import (
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

func (s *Server) reconciliationRunDetail(w http.ResponseWriter, r *http.Request) {
	limit, ok := historyLimit(w, r)
	if !ok {
		return
	}
	var after string
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(raw)
		if err != nil || !utf8.Valid(decoded) || !strings.HasPrefix(string(decoded), "disc:") {
			apiError(w, http.StatusBadRequest, "INVALID_CURSOR", "Invalid run cursor")
			return
		}
		after = string(decoded)
	}
	page, err := s.lab.AdminReconciliationRunPage(r.Context(), r.PathValue("id"), after, limit)
	if errors.Is(err, sql.ErrNoRows) {
		apiError(w, http.StatusNotFound, "NOT_FOUND", "Reconciliation run not found")
		return
	}
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not load reconciliation run")
		return
	}
	safe, err := safeValue(page)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not encode reconciliation run")
		return
	}
	next := ""
	if page.NextID != "" {
		next = base64.RawURLEncoding.EncodeToString([]byte(page.NextID))
	}
	writeJSON(w, http.StatusOK, map[string]any{"page": safe, "next_cursor": next, "observed_at": time.Now().UTC().Format(time.RFC3339Nano)})
}
