package admin

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"billforge/lab"
)

func (s *Server) submitCommand(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("Idempotency-Key")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var input struct {
		ActionID  string          `json:"action_id"`
		TargetID  string          `json:"target_id"`
		PreviewID string          `json:"preview_id"`
		Payload   json.RawMessage `json:"payload"`
	}
	if err := decoder.Decode(&input); err != nil {
		apiError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid command request")
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		apiError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid command request")
		return
	}
	if !s.authorizeActionIntent(w, r, input.ActionID, input.TargetID, input.Payload) {
		return
	}
	command, replay, err := s.lab.AdminSubmitCommand(r.Context(), "local-admin", key, input.ActionID, input.TargetID, input.Payload, input.PreviewID)
	if err != nil {
		switch {
		case errors.Is(err, lab.ErrAdminIdempotencyConflict):
			apiError(w, http.StatusConflict, "IDEMPOTENCY_CONFLICT", "This key belongs to a different command")
		case errors.Is(err, lab.ErrAdminPreviewStale):
			apiError(w, http.StatusConflict, "PREVIEW_STALE", "Preview has expired, changed, or was already used")
		case errors.Is(err, lab.ErrAdminUnsupportedAction):
			apiError(w, http.StatusUnprocessableEntity, "ACTION_UNAVAILABLE", "This command is not available yet")
		case errors.Is(err, lab.ErrAdminInvalidCommand):
			apiError(w, http.StatusUnprocessableEntity, "INVALID_COMMAND", "Command fields are invalid")
		default:
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"error": map[string]any{
					"code":      "COMMAND_ADMISSION_UNKNOWN",
					"message":   "Command acceptance is unconfirmed; retry with the same request key",
					"retryable": true,
				},
			})
		}
		return
	}
	if command.Status == "accepted" {
		commandID := command.ID
		command, err = s.lab.AdminExecuteCommand(r.Context(), commandID)
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{
				"error":      map[string]any{"code": "COMMAND_PENDING_RETRY", "message": "Command is recorded and will be retried", "retryable": true},
				"command_id": commandID,
			})
			return
		}
	}
	// A replay returns its original command; a newly rejected preview lets the UI request a fresh comparison.
	if !replay && command.Status == "failed" && command.ErrorCode == "PREVIEW_STALE" {
		apiError(w, http.StatusConflict, "PREVIEW_STALE", "Preview source changed; review the new preview before confirming")
		return
	}
	code := http.StatusAccepted
	if replay {
		code = http.StatusOK
	}
	writeJSON(w, code, command)
}

func (s *Server) getCommand(w http.ResponseWriter, r *http.Request) {
	command, err := s.lab.AdminCommand(r.Context(), r.PathValue("id"))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			apiError(w, http.StatusNotFound, "NOT_FOUND", "Command not found")
			return
		}
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not load command")
		return
	}
	command.IdempotencyKey = ""
	writeJSON(w, http.StatusOK, command)
}

func (s *Server) resumeCommand(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	command, err := s.lab.AdminCommand(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		apiError(w, http.StatusNotFound, "NOT_FOUND", "Command not found")
		return
	}
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not load command")
		return
	}
	item, ok := s.lookupSession(r)
	if !ok {
		apiError(w, http.StatusUnauthorized, "SESSION_REQUIRED", "Sign in to continue")
		return
	}
	actionAllowed := false
	for _, capability := range item.capabilities {
		if capability == actionCapability(command.ActionID) {
			actionAllowed = true
			break
		}
	}
	if !actionAllowed && ((command.Status == "accepted" && command.ErrorCode == "PERMISSION_REVOKED_REVIEW") || command.Status == "waiting_verification") {
		command, err = s.lab.AdminVerifyExistingObligation(r.Context(), id)
		if errors.Is(err, lab.ErrConflict) {
			apiError(w, http.StatusForbidden, "PERMISSION_DENIED", "This command cannot be verified with read-only access")
			return
		}
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{
				"error":      map[string]any{"code": "COMMAND_PENDING_RETRY", "message": "Command verification remains pending", "retryable": true},
				"command_id": id,
			})
			return
		}
		writeJSON(w, http.StatusOK, command)
		return
	}
	storedPayload, err := s.lab.AdminCommandPayload(r.Context(), command.ID)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not load command")
		return
	}
	if !s.authorizeActionIntent(w, r, command.ActionID, command.TargetID, storedPayload) {
		return
	}
	if command.Status == "accepted" || command.Status == "running" || command.Status == "waiting_verification" {
		command, err = s.lab.AdminExecuteCommand(r.Context(), id)
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{
				"error":      map[string]any{"code": "COMMAND_PENDING_RETRY", "message": "Command remains recorded and can be retried", "retryable": true},
				"command_id": id,
			})
			return
		}
	}
	writeJSON(w, http.StatusOK, command)
}

func (s *Server) listCommands(w http.ResponseWriter, r *http.Request) {
	if !validateQueryKeys(w, r, "INVALID_FILTER", "limit", "cursor") {
		return
	}
	limit := 50
	if input := r.URL.Query().Get("limit"); input != "" {
		parsed, err := strconv.Atoi(input)
		if err != nil || parsed < 1 || parsed > 100 {
			apiError(w, http.StatusBadRequest, "INVALID_LIMIT", "Limit must be 1–100")
			return
		}
		limit = parsed
	}
	after := int64(0)
	if input := r.URL.Query().Get("cursor"); input != "" {
		parsed, err := strconv.ParseInt(input, 10, 64)
		if err != nil || parsed < 1 {
			apiError(w, http.StatusBadRequest, "INVALID_CURSOR", "Cursor is invalid")
			return
		}
		after = parsed
	}
	commands, next, err := s.lab.AdminCommandsPage(r.Context(), after, limit)
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not load commands")
		return
	}
	for i := range commands {
		commands[i].IdempotencyKey = ""
	}
	nextCursor := ""
	if next > 0 {
		nextCursor = strconv.FormatInt(next, 10)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": commands, "next_cursor": nextCursor})
}
