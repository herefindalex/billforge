package admin

import (
	"database/sql"
	"errors"
	"net/http"
	"time"
)

func (s *Server) discrepancyDetail(w http.ResponseWriter, r *http.Request) {
	detail, err := s.lab.AdminDiscrepancyDetail(r.Context(), r.PathValue("id"))
	if errors.Is(err, sql.ErrNoRows) {
		apiError(w, http.StatusNotFound, "NOT_FOUND", "Discrepancy not found")
		return
	}
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not load discrepancy")
		return
	}
	safe, err := safeValue(detail)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not encode discrepancy")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"discrepancy": safe, "observed_at": time.Now().UTC().Format(time.RFC3339Nano)})
}
