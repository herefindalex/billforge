package admin

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"time"
	"unicode/utf8"

	"billforge/lab"
)

func (s *Server) accountMigrationShadows(w http.ResponseWriter, r *http.Request) {
	limit, ok := historyLimit(w, r)
	if !ok {
		return
	}
	var after *lab.AdminShadowCursor
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(raw)
		if err != nil {
			apiError(w, http.StatusBadRequest, "INVALID_CURSOR", "Invalid shadow cursor")
			return
		}
		var cursor lab.AdminShadowCursor
		if err := json.Unmarshal(decoded, &cursor); err != nil || cursor.AtNano == 0 || cursor.ID == "" {
			apiError(w, http.StatusBadRequest, "INVALID_CURSOR", "Invalid shadow cursor")
			return
		}
		after = &cursor
	}
	page, err := s.lab.AdminAccountMigrationShadows(r.Context(), r.PathValue("id"), after, limit)
	if errors.Is(err, sql.ErrNoRows) {
		apiError(w, http.StatusNotFound, "NOT_FOUND", "Account migration not found")
		return
	}
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not load shadow comparisons")
		return
	}
	safe, err := safeValue(page.Items)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not encode shadow comparisons")
		return
	}
	next := ""
	if page.Next != nil {
		encoded, err := json.Marshal(page.Next)
		if err != nil {
			apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not encode shadow cursor")
			return
		}
		next = base64.RawURLEncoding.EncodeToString(encoded)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": safe, "next_cursor": next, "observed_at": time.Now().UTC().Format(time.RFC3339Nano)})
}

func (s *Server) accountMigrationProvenance(w http.ResponseWriter, r *http.Request) {
	limit, ok := historyLimit(w, r)
	if !ok {
		return
	}
	var afterID string
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(raw)
		if err != nil || len(decoded) == 0 || !utf8.Valid(decoded) {
			apiError(w, http.StatusBadRequest, "INVALID_CURSOR", "Invalid provenance cursor")
			return
		}
		afterID = string(decoded)
	}
	page, err := s.lab.AdminAccountMigrationProvenance(r.Context(), r.PathValue("id"), afterID, limit)
	if errors.Is(err, sql.ErrNoRows) {
		apiError(w, http.StatusNotFound, "NOT_FOUND", "Account migration not found")
		return
	}
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not load provenance")
		return
	}
	safe, err := safeValue(page.Items)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not encode provenance")
		return
	}
	next := ""
	if page.NextID != "" {
		next = base64.RawURLEncoding.EncodeToString([]byte(page.NextID))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": safe, "next_cursor": next, "observed_at": time.Now().UTC().Format(time.RFC3339Nano)})
}
