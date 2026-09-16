package portal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"gorbital.dev/cli/internal/genplan"
	"gorbital.dev/cli/internal/pgmeta"
)

// SQLRunner runs scripts: the SQL editor's side of the database
// (ADR-0068). *pgmeta.Client implements it.
type SQLRunner interface {
	Run(ctx context.Context, req pgmeta.RunRequest) (pgmeta.RunResult, error)
	Explain(ctx context.Context, sql string, analyze bool) (json.RawMessage, error)
}

// ExplainRequest is the body of sql/explain.
type ExplainRequest struct {
	SQL     string `json:"sql"`
	Analyze bool   `json:"analyze,omitempty"`
}

// ExplainResponse answers sql/explain with the plan as PostgreSQL's JSON.
type ExplainResponse struct {
	Plan json.RawMessage `json:"plan"`
}

// CheckRequest is the body of sql/check.
type CheckRequest struct {
	SQL string `json:"sql"`
}

// CheckResponse answers sql/check.
type CheckResponse struct {
	Warnings []pgmeta.Warning `json:"warnings"`
}

// SnippetRequest is the body of a snippet PUT.
type SnippetRequest struct {
	SQL      string `json:"sql"`
	Favorite bool   `json:"favorite,omitempty"`
}

// SQLMigrationRequest is the body of sql/migration: a script saved as a
// migration's Up section.
type SQLMigrationRequest struct {
	Name string `json:"name"`
	SQL  string `json:"sql"`
	// Apply also runs the migration through the supervisor.
	Apply      bool `json:"apply,omitempty"`
	AllowDirty bool `json:"allow_dirty,omitempty"`
}

// sqlHandler routes the SQL editor's API under /_portal/api/db/sql/.
func (s *Server) sqlHandler() http.Handler {
	mux := http.NewServeMux()
	prefix := APIPrefix + "db/sql/"
	mux.HandleFunc("GET "+prefix+"templates", func(w http.ResponseWriter, _ *http.Request) {
		_ = writeJSON(w, http.StatusOK, map[string]any{"templates": pgmeta.Templates()})
	})
	mux.HandleFunc("POST "+prefix+"check", func(w http.ResponseWriter, r *http.Request) {
		var req CheckRequest
		if err := readBody(r, &req); err != nil {
			writeDBError(w, err)
			return
		}
		warnings := pgmeta.Check(req.SQL)
		if warnings == nil {
			warnings = []pgmeta.Warning{}
		}
		_ = writeJSON(w, http.StatusOK, CheckResponse{Warnings: warnings})
	})
	mux.HandleFunc("POST "+prefix+"run", s.withDB(func(ctx context.Context, db Database, r *http.Request) (any, error) {
		var req pgmeta.RunRequest
		if err := readBody(r, &req); err != nil {
			return nil, err
		}
		runner, ok := db.(SQLRunner)
		if !ok {
			return nil, fmt.Errorf("%w: this database can't run scripts", pgmeta.ErrInvalidInput)
		}
		res, err := runner.Run(ctx, req)
		if err != nil {
			return nil, err
		}
		if s.cfg.SQL != nil {
			entry := HistoryEntry{Time: time.Now().UTC(), SQL: req.SQL, Mode: res.Mode, DurationMS: res.DurationMS}
			if n := len(res.Statements); n > 0 {
				entry.Rows = len(res.Statements[n-1].Rows)
			}
			if res.Error != nil {
				entry.Error = res.Error.Message
			}
			if err := s.cfg.SQL.Record(entry); err != nil {
				s.cfg.Logf("orb: dev portal couldn't record the query history: %v", err)
			}
		}
		return res, nil
	}))
	mux.HandleFunc("POST "+prefix+"explain", s.withDB(func(ctx context.Context, db Database, r *http.Request) (any, error) {
		var req ExplainRequest
		if err := readBody(r, &req); err != nil {
			return nil, err
		}
		runner, ok := db.(SQLRunner)
		if !ok {
			return nil, fmt.Errorf("%w: this database can't explain", pgmeta.ErrInvalidInput)
		}
		plan, err := runner.Explain(ctx, req.SQL, req.Analyze)
		if err != nil {
			return nil, err
		}
		return ExplainResponse{Plan: plan}, nil
	}))
	mux.HandleFunc("GET "+prefix+"snippets", s.withStore(func(_ *http.Request) (any, error) {
		v, err := s.cfg.SQL.Snippets()
		return map[string]any{"snippets": v}, err
	}))
	mux.HandleFunc("PUT "+prefix+"snippets/{name}", s.withStore(func(r *http.Request) (any, error) {
		var req SnippetRequest
		if err := readBody(r, &req); err != nil {
			return nil, err
		}
		return s.cfg.SQL.Save(r.PathValue("name"), req.SQL, req.Favorite)
	}))
	mux.HandleFunc("DELETE "+prefix+"snippets/{name}", s.withStore(func(r *http.Request) (any, error) {
		return map[string]bool{"deleted": true}, s.cfg.SQL.Delete(r.PathValue("name"))
	}))
	mux.HandleFunc("GET "+prefix+"history", s.withStore(func(_ *http.Request) (any, error) {
		v, err := s.cfg.SQL.History()
		return map[string]any{"history": v, "max": MaxHistory}, err
	}))
	mux.HandleFunc("DELETE "+prefix+"history", s.withStore(func(_ *http.Request) (any, error) {
		return map[string]bool{"cleared": true}, s.cfg.SQL.ClearHistory()
	}))
	mux.HandleFunc("POST "+prefix+"migration", func(w http.ResponseWriter, r *http.Request) {
		var req SQLMigrationRequest
		if err := readBody(r, &req); err != nil {
			writeDBError(w, err)
			return
		}
		if strings.TrimSpace(req.SQL) == "" {
			writeProblem(w, http.StatusUnprocessableEntity, "invalid_input", "the migration is empty")
			return
		}
		if s.cfg.Database.NextMigrationVersion == nil || s.cfg.Database.Apply == nil {
			writeProblem(w, http.StatusNotFound, "no_database", "this app has no database (the Minimal preset)")
			return
		}
		version, err := s.cfg.Database.NextMigrationVersion()
		if err != nil {
			writeDBError(w, err)
			return
		}
		name := strings.Trim(slugPattern.ReplaceAllString(strings.ToLower(req.Name), "_"), "_")
		if name == "" {
			writeProblem(w, http.StatusUnprocessableEntity, "invalid_input", "the migration needs a name")
			return
		}
		if len(name) > 60 {
			name = name[:60]
		}
		content := "-- " + strings.ReplaceAll(strings.TrimSpace(req.Name), "\n", " ") + ".\n--\n-- Written from the Dev Portal's SQL editor. Change this migration freely\n-- until it is released; afterwards, add a new one.\n\n-- +goose Up\n" + strings.TrimRight(req.SQL, "\n") + "\n"
		file := genplan.Change{Path: "db/migrations/" + version + "_" + name + ".sql", Kind: genplan.Create, Content: []byte(content)}
		out := DDLResponse{Plan: pgmeta.Plan{Summary: req.Name, Up: []string{strings.TrimSpace(req.SQL)}}, File: file}
		if req.Apply {
			plan := genplan.Plan{Generator: "sql", Name: name, Summary: req.Name, Changes: []genplan.Change{file}}
			if err := s.cfg.Database.Apply(r.Context(), plan, req.AllowDirty); err != nil {
				writeDBError(w, err)
				return
			}
			out.Applied = true
		}
		_ = writeJSON(w, http.StatusOK, out)
	})
	mux.HandleFunc(prefix, func(w http.ResponseWriter, r *http.Request) {
		writeProblem(w, http.StatusNotFound, "not_found", "no portal endpoint "+r.Method+" "+r.URL.Path)
	})
	return mux
}

// withStore answers fn when the SQL store exists.
func (s *Server) withStore(fn func(r *http.Request) (any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.SQL == nil {
			writeProblem(w, http.StatusNotFound, "no_database", "this app has no database (the Minimal preset)")
			return
		}
		out, err := fn(r)
		switch {
		case errors.Is(err, ErrSnippetName):
			writeProblem(w, http.StatusUnprocessableEntity, "invalid_input", err.Error())
		case err != nil:
			writeDBError(w, err)
		default:
			_ = writeJSON(w, http.StatusOK, out)
		}
	}
}
