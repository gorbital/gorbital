package portal

import (
	"context"
	"net/http"
	"strconv"
	"time"
)

// Observability endpoints (ADR-0073), under /_portal/api:
//
//	GET  health            every service's health: app, database, mail, services
//	GET  system            the machine and the app process (sampled)
//	GET  db/stats          connections, cache, sizes, locks, long-running statements
//	GET  db/statements     pg_stat_statements (sort, limit)
//	POST db/statements/reset
//	GET  db/advice         index and vacuum suggestions

// ServiceHealth is one service's state as orb dev sees it.
type ServiceHealth struct {
	// Name: app, postgres, mail, or a Docker Compose service.
	Name string `json:"name"`
	// Status is ok, degraded, down or unknown.
	Status string `json:"status"`
	// Detail says why, in a sentence; Version what answered.
	Detail  string `json:"detail,omitempty"`
	Version string `json:"version,omitempty"`
	// URL is where to look, when there is a page.
	URL       string    `json:"url,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
	LatencyMS float64   `json:"latency_ms"`
}

// Health statuses.
const (
	HealthOK       = "ok"
	HealthDegraded = "degraded"
	HealthDown     = "down"
	HealthUnknown  = "unknown"
)

func (s *Server) serveHealth(w http.ResponseWriter, r *http.Request) {
	services := []ServiceHealth{}
	if s.cfg.Health != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		services = s.cfg.Health(ctx)
		if services == nil {
			services = []ServiceHealth{}
		}
	}
	_ = writeJSON(w, http.StatusOK, map[string]any{"services": services})
}

func (s *Server) serveSystem(w http.ResponseWriter, _ *http.Request) {
	if s.cfg.System == nil {
		writeProblem(w, http.StatusNotFound, "no_system_sampler", "this orb dev doesn't sample the system")
		return
	}
	_ = writeJSON(w, http.StatusOK, s.cfg.System.Last())
}

func (s *Server) serveDBStats(w http.ResponseWriter, r *http.Request) {
	s.withDB(func(ctx context.Context, db Database, _ *http.Request) (any, error) { return db.Stats(ctx) })(w, r)
}

func (s *Server) serveDBStatements(w http.ResponseWriter, r *http.Request) {
	s.withDB(func(ctx context.Context, db Database, r *http.Request) (any, error) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		return db.Statements(ctx, r.URL.Query().Get("sort"), limit)
	})(w, r)
}

func (s *Server) serveDBStatementsReset(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Database.Open == nil {
		writeProblem(w, http.StatusNotFound, "no_database", "this app has no database (the Minimal preset)")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	db, err := s.cfg.Database.Open(ctx)
	if err != nil {
		writeProblem(w, http.StatusServiceUnavailable, "database_unavailable", "the database isn't reachable: "+err.Error())
		return
	}
	if err := db.ResetStatements(ctx); err != nil {
		writeProblem(w, http.StatusConflict, "statements_unavailable", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) serveDBAdvice(w http.ResponseWriter, r *http.Request) {
	s.withDB(func(ctx context.Context, db Database, _ *http.Request) (any, error) { return db.Advise(ctx) })(w, r)
}
