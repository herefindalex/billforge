package admin

import (
	"database/sql"
	"errors"
	"net/http"
	"time"
)

func (s *Server) creditDetail(w http.ResponseWriter, r *http.Request) {
	detail, err := s.lab.AdminCreditDetail(r.Context(), r.PathValue("id"))
	if errors.Is(err, sql.ErrNoRows) {
		apiError(w, http.StatusNotFound, "NOT_FOUND", "Credit not found")
		return
	}
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not load credit")
		return
	}
	safe, err := safeValue(detail)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not encode credit")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"credit": safe, "observed_at": time.Now().UTC().Format(time.RFC3339Nano)})
}
