package admin

import (
	"database/sql"
	"errors"
	"net/http"
	"time"
)

func (s *Server) priceDetail(w http.ResponseWriter, r *http.Request) {
	detail, err := s.lab.AdminPriceVersionDetail(r.Context(), r.PathValue("id"))
	if errors.Is(err, sql.ErrNoRows) {
		apiError(w, http.StatusNotFound, "NOT_FOUND", "Price version not found")
		return
	}
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not load price version")
		return
	}
	safe, err := safeValue(detail)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not encode price version")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"price": safe, "observed_at": time.Now().UTC().Format(time.RFC3339Nano)})
}
