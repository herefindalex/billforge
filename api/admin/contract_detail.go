package admin

import (
	"database/sql"
	"errors"
	"net/http"
	"time"
)

func (s *Server) contractDetail(w http.ResponseWriter, r *http.Request) {
	detail, err := s.lab.AdminContractVersionDetail(r.Context(), r.PathValue("id"))
	if errors.Is(err, sql.ErrNoRows) {
		apiError(w, http.StatusNotFound, "NOT_FOUND", "Contract version not found")
		return
	}
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not load contract version")
		return
	}
	safe, err := safeValue(detail)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not encode contract version")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"contract": safe, "observed_at": time.Now().UTC().Format(time.RFC3339Nano)})
}
