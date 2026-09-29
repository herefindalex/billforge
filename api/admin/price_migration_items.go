package admin

import (
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"
	"unicode/utf8"
)

type priceMigrationItemsCursor struct {
	After string `json:"after"`
	Scope string `json:"scope"`
}

func (s *Server) migrationItems(w http.ResponseWriter, r *http.Request) {
	if !validateQueryKeys(w, r, "INVALID_FILTER", "limit", "status", "cursor") {
		return
	}
	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			apiError(w, http.StatusBadRequest, "INVALID_LIMIT", "Limit must be 1–100")
			return
		}
		limit = parsed
	}
	status := r.URL.Query().Get("status")
	switch status {
	case "", "pending", "applied", "conflicted", "skipped":
	default:
		apiError(w, http.StatusBadRequest, "INVALID_FILTER", "Invalid migration item status")
		return
	}
	id := r.PathValue("id")
	scope := fmt.Sprintf("%x", sha256.Sum256([]byte(id+"\x00"+status)))
	after := ""
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		if len(raw) > 8192 {
			apiError(w, http.StatusBadRequest, "INVALID_CURSOR", "Invalid migration item cursor")
			return
		}
		decoded, err := base64.RawURLEncoding.DecodeString(raw)
		if err != nil || len(decoded) > 4096 {
			apiError(w, http.StatusBadRequest, "INVALID_CURSOR", "Invalid migration item cursor")
			return
		}
		var cursor priceMigrationItemsCursor
		if err := json.Unmarshal(decoded, &cursor); err != nil || cursor.After == "" || !utf8.ValidString(cursor.After) || cursor.Scope != scope {
			apiError(w, http.StatusBadRequest, "INVALID_CURSOR", "Invalid migration item cursor")
			return
		}
		after = cursor.After
	}
	page, err := s.lab.PriceMigrationItems(r.Context(), id, status, after, limit)
	if errors.Is(err, sql.ErrNoRows) {
		apiError(w, http.StatusNotFound, "NOT_FOUND", "Migration not found")
		return
	}
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not load migration items")
		return
	}
	safe, err := safeValue(page.Items)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not encode migration items")
		return
	}
	next := ""
	if page.Next != "" {
		encoded, err := json.Marshal(priceMigrationItemsCursor{After: page.Next, Scope: scope})
		if err != nil {
			apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not encode migration item cursor")
			return
		}
		next = base64.RawURLEncoding.EncodeToString(encoded)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": safe, "next_cursor": next, "observed_at": time.Now().UTC().Format(time.RFC3339Nano)})
}
