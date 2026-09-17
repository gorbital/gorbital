package portal

import (
	"net/http"

	"gorbital.dev/cli/internal/routes"
)

// serveRoutes is GET /_portal/api/routes: every route of the app with its
// guards, public flag, middleware, handler and source position, as orb routes
// --json lists them (ADR-0082, v0.2 Phase 8). It can build the app, so it
// may take a few seconds.
func (s *Server) serveRoutes(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Routes == nil {
		writeProblem(w, http.StatusNotFound, "not_found", "this orb dev lists no routes")
		return
	}
	list, err := s.cfg.Routes(r.Context())
	if err != nil {
		writeProblem(w, http.StatusUnprocessableEntity, "routes_failed", err.Error())
		return
	}
	_ = writeJSON(w, http.StatusOK, list)
}

// RouteList is GET /_portal/api/routes.
type RouteList = routes.List
