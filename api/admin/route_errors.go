package admin

import (
	"net/http"
	"strings"
)

type routeStatusProbe struct {
	header http.Header
	status int
}

func (p *routeStatusProbe) Header() http.Header { return p.header }

func (p *routeStatusProbe) WriteHeader(status int) { p.status = status }

func (p *routeStatusProbe) Write(data []byte) (int, error) {
	if p.status == 0 {
		p.status = http.StatusOK
	}
	return len(data), nil
}

func (s *Server) serveAdminRoute(mux *http.ServeMux, w http.ResponseWriter, r *http.Request) {
	handler, pattern := mux.Handler(r)
	if pattern != "" || (r.URL.Path != "/admin/api" && !strings.HasPrefix(r.URL.Path, "/admin/api/")) {
		mux.ServeHTTP(w, r)
		return
	}

	// ServeMux writes text/plain for unmatched methods and paths. Keep API
	// failures under the normal session guard and JSON error contract.
	probe := &routeStatusProbe{header: make(http.Header)}
	handler.ServeHTTP(probe, r)
	s.protected(func(w http.ResponseWriter, r *http.Request) {
		if probe.status == http.StatusMethodNotAllowed {
			w.Header().Set("Allow", probe.header.Get("Allow"))
			apiError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "This method is not available for the API route")
			return
		}
		apiError(w, http.StatusNotFound, "NOT_FOUND", "API route not found")
	})(w, r)
}
