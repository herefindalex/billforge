package admin

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"time"
)

func (s *Server) listJobs(w http.ResponseWriter, r *http.Request) {
	limit, ok := historyLimit(w, r)
	if !ok {
		return
	}
	var before int64
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed < 1 || strconv.FormatInt(parsed, 10) != raw {
			apiError(w, http.StatusBadRequest, "INVALID_CURSOR", "Invalid job cursor")
			return
		}
		before = parsed
	}
	jobs, next, err := s.lab.AdminJobsPage(r.Context(), before, limit)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not load jobs")
		return
	}
	safe, err := safeValue(jobs)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not encode jobs")
		return
	}
	nextCursor := ""
	if next > 0 {
		nextCursor = strconv.FormatInt(next, 10)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": safe, "next_cursor": nextCursor, "observed_at": time.Now().UTC().Format(time.RFC3339Nano)})
}

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
