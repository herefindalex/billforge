package admin

import (
	"database/sql"
	"errors"
	"net/http"
	"time"
)

func (s *Server) invoiceDetail(w http.ResponseWriter, r *http.Request) {
	detail, err := s.lab.AdminInvoiceDetail(r.Context(), r.PathValue("id"))
	if errors.Is(err, sql.ErrNoRows) {
		apiError(w, http.StatusNotFound, "NOT_FOUND", "Invoice not found")
		return
	}
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not load invoice")
		return
	}
	safe, err := safeValue(detail)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not encode invoice")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"invoice": safe, "observed_at": time.Now().UTC().Format(time.RFC3339Nano)})
}
