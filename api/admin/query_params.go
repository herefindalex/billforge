package admin

import (
	"net/http"
	"net/url"
)

// validateQueryKeys prevents a misspelled or repeated query parameter from
// silently changing the meaning of a management read.
func validateQueryKeys(w http.ResponseWriter, r *http.Request, code string, allowed ...string) bool {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		apiError(w, http.StatusBadRequest, code, "Malformed query parameter")
		return false
	}
	keys := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		keys[key] = struct{}{}
	}
	for key, values := range query {
		if _, ok := keys[key]; !ok || len(values) != 1 {
			apiError(w, http.StatusBadRequest, code, "Unknown or repeated query parameter")
			return false
		}
	}
	return true
}
