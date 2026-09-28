package admin

import (
	"encoding/json"
	"net/http"
	"time"
)

func (s *Server) overview(w http.ResponseWriter, r *http.Request) {
	counts, err := s.lab.AdminOverviewCounts(r.Context())
	if err != nil {
		apiError(w, http.StatusInternalServerError, "QUERY_FAILED", "Could not load commerce state")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"observed_at": time.Now().UTC().Format(time.RFC3339Nano),
		"counts":      counts,
	})
}

func stringNumbers(value any) any {
	switch x := value.(type) {
	case json.Number:
		return x.String()
	case []any:
		for i := range x {
			x[i] = stringNumbers(x[i])
		}
		return x
	case map[string]any:
		for key, item := range x {
			x[key] = stringNumbers(item)
		}
		return x
	default:
		return x
	}
}
