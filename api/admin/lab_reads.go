package admin

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"billforge/lab"
)

func (s *Server) labClock(w http.ResponseWriter, r *http.Request) {
	state, err := s.lab.AdminClock(r.Context())
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not load lab clock")
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) labFaults(w http.ResponseWriter, r *http.Request) {
	limit, ok := historyLimit(w, r)
	if !ok {
		return
	}
	var after *lab.AdminFaultTicketCursor
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(raw)
		if err != nil {
			apiError(w, http.StatusBadRequest, "INVALID_CURSOR", "Invalid fault ticket cursor")
			return
		}
		var token struct {
			Pending *bool `json:"pending"`
			RowID   int64 `json:"row_id"`
		}
		decoder := json.NewDecoder(bytes.NewReader(decoded))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&token); err != nil || decoder.Decode(new(any)) != io.EOF || token.Pending == nil || token.RowID < 1 {
			apiError(w, http.StatusBadRequest, "INVALID_CURSOR", "Invalid fault ticket cursor")
			return
		}
		after = &lab.AdminFaultTicketCursor{Pending: *token.Pending, RowID: token.RowID}
	}
	items, next, err := s.lab.AdminFaultTicketsPage(r.Context(), after, limit)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not load fault tickets")
		return
	}
	nextCursor := ""
	if next != nil {
		encoded, err := json.Marshal(map[string]any{"pending": next.Pending, "row_id": next.RowID})
		if err != nil {
			apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not encode fault ticket cursor")
			return
		}
		nextCursor = base64.RawURLEncoding.EncodeToString(encoded)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": nextCursor, "observed_at": time.Now().UTC().Format(time.RFC3339Nano)})
}
