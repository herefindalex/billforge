package admin

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"billforge/lab"
)

func (s *Server) createPreview(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var input struct {
		ActionID         string          `json:"action_id"`
		TargetID         string          `json:"target_id"`
		Payload          json.RawMessage `json:"payload"`
		ExpectedRevision string          `json:"expected_revision"`
	}
	if err := decoder.Decode(&input); err != nil {
		apiError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid preview request")
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		apiError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid preview request")
		return
	}
	if !s.authorizeActionIntent(w, r, input.ActionID, input.TargetID, input.Payload) {
		return
	}
	if input.ExpectedRevision != "" {
		apiError(w, http.StatusUnprocessableEntity, "INVALID_REVISION", "This action does not use a revision")
		return
	}
	preview, err := s.lab.AdminCreatePreview(r.Context(), "local-admin", input.ActionID, input.TargetID, input.Payload)
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			apiError(w, http.StatusNotFound, "NOT_FOUND", "Source record not found")
		case errors.Is(err, lab.ErrAccountMigrationStopped):
			apiError(w, http.StatusConflict, "ACCOUNT_MIGRATION_STOPPED", "Account migration is stopped; new commerce writes are disabled")
		case errors.Is(err, lab.ErrExpired):
			apiError(w, http.StatusConflict, "QUOTE_EXPIRED", "Quote expired; create a new quote")
		case errors.Is(err, lab.ErrChangeQuoteBindingMismatch):
			apiError(w, http.StatusConflict, "CHANGE_QUOTE_BINDING_MISMATCH", "Quote ID and change binding do not match this subscription")
		case errors.Is(err, lab.ErrChangeQuoteRevisionChanged):
			apiError(w, http.StatusConflict, "CHANGE_QUOTE_REVISION_CHANGED", "Subscription revision changed; create a new change quote")
		case errors.Is(err, lab.ErrChangeQuotePriceSuperseded):
			apiError(w, http.StatusConflict, "CHANGE_QUOTE_PRICE_SUPERSEDED", "Quote price is no longer selected; create a new change quote")
		case errors.Is(err, lab.ErrConflict):
			apiError(w, http.StatusConflict, "PREVIEW_UNAVAILABLE", "Source changed or has no eligible items")
		case errors.Is(err, lab.ErrAdminUnsupportedAction):
			apiError(w, http.StatusUnprocessableEntity, "ACTION_UNAVAILABLE", "This preview is not available yet")
		case errors.Is(err, lab.ErrAdminInvalidCommand):
			apiError(w, http.StatusUnprocessableEntity, "INVALID_PREVIEW", "Preview fields are invalid")
		default:
			apiError(w, http.StatusInternalServerError, "PREVIEW_FAILED", "Could not create preview")
		}
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

func (s *Server) getPreview(w http.ResponseWriter, r *http.Request) {
	preview, err := s.lab.AdminGetPreview(r.Context(), "local-admin", r.PathValue("id"))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			apiError(w, http.StatusNotFound, "NOT_FOUND", "Preview not found")
			return
		}
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not load preview")
		return
	}
	writeJSON(w, http.StatusOK, preview)
}
