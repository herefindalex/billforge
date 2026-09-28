package admin

import (
	"encoding/json"
	"net/http"
)

func actionCapability(actionID string) string {
	switch actionID {
	case "C01", "C02", "C03", "C04", "C05", "C06":
		return "subscription.manage"
	case "C07", "C08", "C09", "C10", "C11", "C12", "C13", "C14", "C15", "C16", "C17", "C30", "C32":
		return "finance.adjust"
	case "C18", "C19", "C20", "C21":
		return "catalog.publish"
	case "C22", "C23", "C24", "C25", "C36", "C37", "C38", "C39", "C40", "C41", "C42", "C43":
		return "migration.manage"
	case "C26", "C27", "C28", "C29":
		return "usage.manage"
	case "C31":
		return "contract.manage"
	case "C33", "C34", "C35":
		return "reconciliation.repair"
	case "C44", "C45":
		return "operations.run"
	case "C46", "C47", "C48", "C49":
		return "lab.control"
	}
	return ""
}

func (s *Server) authorizeAction(w http.ResponseWriter, r *http.Request, actionID string) bool {
	required := actionCapability(actionID)
	if required == "" {
		apiError(w, http.StatusUnprocessableEntity, "ACTION_UNAVAILABLE", "Unknown action")
		return false
	}
	item, ok := s.lookupSession(r)
	if !ok {
		apiError(w, http.StatusUnauthorized, "SESSION_REQUIRED", "Sign in to continue")
		return false
	}
	hasCapability := func(want string) bool {
		for _, capability := range item.capabilities {
			if capability == want {
				return true
			}
		}
		return false
	}
	if hasCapability(required) {
		return true
	}
	apiError(w, http.StatusForbidden, "PERMISSION_DENIED", "This action is not permitted")
	return false
}

func (s *Server) authorizeActionIntent(w http.ResponseWriter, r *http.Request, actionID, targetID string, payload json.RawMessage) bool {
	if !s.authorizeAction(w, r, actionID) {
		return false
	}
	var contract bool
	switch actionID {
	case "C01":
		var input struct {
			ContractVersionID string `json:"contract_version_id"`
		}
		if json.Unmarshal(payload, &input) == nil {
			contract = input.ContractVersionID != ""
		}
	case "C02":
		var err error
		contract, err = s.lab.IsContractQuote(r.Context(), targetID)
		if err != nil {
			apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not check quote permissions")
			return false
		}
	}
	if !contract {
		return true
	}
	item, _ := s.lookupSession(r)
	for _, capability := range item.capabilities {
		if capability == "contract.manage" {
			return true
		}
	}
	apiError(w, http.StatusForbidden, "PERMISSION_DENIED", "This action is not permitted")
	return false
}
