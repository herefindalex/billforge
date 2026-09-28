package admin

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"time"

	"billforge/lab"
)

func (s *Server) accountMigrationDetail(w http.ResponseWriter, r *http.Request) {
	detail, err := s.lab.AdminAccountMigrationDetail(r.Context(), r.PathValue("id"))
	if errors.Is(err, sql.ErrNoRows) {
		apiError(w, http.StatusNotFound, "NOT_FOUND", "Account migration not found")
		return
	}
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not load account migration")
		return
	}
	safe, err := safeValue(detail)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not encode account migration")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"migration": safe, "observed_at": time.Now().UTC().Format(time.RFC3339Nano)})
}

func (s *Server) accountMigrationReadiness(w http.ResponseWriter, r *http.Request) {
	if !validateQueryKeys(w, r, "INVALID_THRESHOLDS", "max_quote_p95_millis", "max_unknown_payments", "max_open_discrepancies") {
		return
	}
	query := r.URL.Query()
	p95, p95Err := strconv.ParseInt(query.Get("max_quote_p95_millis"), 10, 64)
	unknown, unknownErr := strconv.ParseInt(query.Get("max_unknown_payments"), 10, 64)
	open, openErr := strconv.ParseInt(query.Get("max_open_discrepancies"), 10, 64)
	if p95Err != nil || unknownErr != nil || openErr != nil || p95 <= 0 || unknown < 0 || open < 0 {
		apiError(w, http.StatusBadRequest, "INVALID_THRESHOLDS", "Readiness thresholds are required")
		return
	}
	readiness, err := s.lab.AdminMigrationReadiness(r.Context(), r.PathValue("id"), lab.MigrationThresholds{MaxQuoteP95Millis: p95, MaxUnknownPayments: unknown, MaxOpenDiscrepancies: open})
	if errors.Is(err, sql.ErrNoRows) {
		apiError(w, http.StatusNotFound, "NOT_FOUND", "Account migration not found")
		return
	}
	if errors.Is(err, lab.ErrConflict) {
		apiError(w, http.StatusBadRequest, "INVALID_THRESHOLDS", "Readiness thresholds are invalid")
		return
	}
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not calculate readiness")
		return
	}
	safe, err := safeValue(readiness)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not encode readiness")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"readiness": safe, "observed_at": time.Now().UTC().Format(time.RFC3339Nano)})
}

func (s *Server) accountMigrationEntitlement(w http.ResponseWriter, r *http.Request) {
	read, err := s.lab.ReadAccountEntitlement(r.Context(), r.PathValue("id"), r.PathValue("subscriptionId"))
	if errors.Is(err, sql.ErrNoRows) {
		apiError(w, http.StatusNotFound, "NOT_FOUND", "Account or entitlement source not found")
		return
	}
	if errors.Is(err, lab.ErrConflict) {
		apiError(w, http.StatusConflict, "ACCOUNT_MISMATCH", "Subscription does not belong to this account")
		return
	}
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not read account entitlement")
		return
	}
	safe, err := safeValue(read)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not encode account entitlement")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entitlement": safe, "observed_at": time.Now().UTC().Format(time.RFC3339Nano)})
}
