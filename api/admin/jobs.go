package admin

import (
	"database/sql"
	"errors"
	"net/http"
)

func (s *Server) getJob(w http.ResponseWriter, r *http.Request) {
	job, err := s.lab.AdminJob(r.Context(), r.PathValue("id"))
	if errors.Is(err, sql.ErrNoRows) {
		apiError(w, http.StatusNotFound, "NOT_FOUND", "Job not found")
		return
	}
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not load job")
		return
	}
	writeJSON(w, http.StatusOK, job)
}
