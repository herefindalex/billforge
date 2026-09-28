package admin

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"billforge/lab"
)

func (s *Server) invoiceHistory(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	switch kind {
	case "corrections", "applications", "grants", "refunds":
	default:
		apiError(w, http.StatusNotFound, "NOT_FOUND", "Invoice history kind not found")
		return
	}
	limit, ok := historyLimit(w, r)
	if !ok {
		return
	}
	invoiceID := r.PathValue("id")
	var after *lab.AdminInvoiceHistoryCursor
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		if len(raw) > 2048 {
			apiError(w, http.StatusBadRequest, "INVALID_CURSOR", "Invalid invoice history cursor")
			return
		}
		decoded, err := base64.RawURLEncoding.DecodeString(raw)
		if err != nil {
			apiError(w, http.StatusBadRequest, "INVALID_CURSOR", "Invalid invoice history cursor")
			return
		}
		var cursor lab.AdminInvoiceHistoryCursor
		if err := json.Unmarshal(decoded, &cursor); err != nil || cursor.InvoiceID != invoiceID || cursor.Kind != kind || cursor.AtNano <= 0 || cursor.ID == "" {
			apiError(w, http.StatusBadRequest, "INVALID_CURSOR", "Invalid invoice history cursor")
			return
		}
		after = &cursor
	}
	page, err := s.lab.AdminInvoiceHistory(r.Context(), invoiceID, kind, after, limit)
	if errors.Is(err, sql.ErrNoRows) {
		apiError(w, http.StatusNotFound, "NOT_FOUND", "Invoice not found")
		return
	}
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not load invoice history")
		return
	}
	safe, err := safeValue(page.Items)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not encode invoice history")
		return
	}
	next := ""
	if page.Next != nil {
		encoded, err := json.Marshal(page.Next)
		if err != nil {
			apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not encode invoice cursor")
			return
		}
		next = base64.RawURLEncoding.EncodeToString(encoded)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": safe, "next_cursor": next, "currency": page.Currency, "observed_at": time.Now().UTC().Format(time.RFC3339Nano)})
}
