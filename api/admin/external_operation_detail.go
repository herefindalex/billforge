package admin

import (
	"database/sql"
	"errors"
	"net/http"
	"time"
)

func (s *Server) paymentDetail(w http.ResponseWriter, r *http.Request) {
	s.externalOperationDetail(w, r, "C09", "Payment")
}

func (s *Server) refundDetail(w http.ResponseWriter, r *http.Request) {
	s.externalOperationDetail(w, r, "C16", "Refund")
}

func (s *Server) externalOperationDetail(w http.ResponseWriter, r *http.Request, actionID, label string) {
	operation, err := s.lab.AdminExternalOperationState(r.Context(), actionID, r.PathValue("id"))
	if errors.Is(err, sql.ErrNoRows) {
		apiError(w, http.StatusNotFound, "NOT_FOUND", label+" not found")
		return
	}
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not load "+label)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"operation":   operation,
		"observed_at": time.Now().UTC().Format(time.RFC3339Nano),
	})
}
