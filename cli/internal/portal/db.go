package portal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"gorbital.dev/cli/internal/genplan"
	"gorbital.dev/cli/internal/pgmeta"
)

// Database is what the Table Editor and Schema pages need from the app's
// database (ADR-0067). *pgmeta.Client implements it.
type Database interface {
	Schemas(ctx context.Context) ([]pgmeta.Schema, error)
	Tables(ctx context.Context, schemas []string) ([]pgmeta.Table, error)
	Detail(ctx context.Context, schema, table string) (pgmeta.TableDetail, error)
	ForeignKeys(ctx context.Context, schemas []string) ([]pgmeta.ForeignKey, error)
	Enums(ctx context.Context, schemas []string) ([]pgmeta.Enum, error)
	Functions(ctx context.Context, schemas []string) ([]pgmeta.Function, error)
	Views(ctx context.Context, schemas []string) ([]pgmeta.View, error)
	Extensions(ctx context.Context) ([]pgmeta.Extension, error)
	Types(ctx context.Context, schemas []string) ([]pgmeta.TypeOption, error)
	Query(ctx context.Context, q pgmeta.RowQuery) (pgmeta.RowPage, error)
	Insert(ctx context.Context, e pgmeta.RowEdit) ([]pgmeta.Cell, error)
	InsertMany(ctx context.Context, schema, table string, rows []map[string]pgmeta.Cell) (int64, error)
	Update(ctx context.Context, e pgmeta.RowEdit) ([]pgmeta.Cell, error)
	Delete(ctx context.Context, e pgmeta.RowEdit) (int64, error)
	Plan(ctx context.Context, ch pgmeta.Change) (pgmeta.Plan, error)
	Migrations(ctx context.Context, dir string) ([]pgmeta.Migration, error)
	// Observability (ADR-0073).
	Stats(ctx context.Context) (pgmeta.DatabaseStats, error)
	Statements(ctx context.Context, sort string, limit int) (pgmeta.Statements, error)
	ResetStatements(ctx context.Context) error
	Advise(ctx context.Context) (pgmeta.Advice, error)
}

// DatabaseConfig connects the portal to the app's database.
type DatabaseConfig struct {
	// Open returns the database, connecting on first use; nil when the app
	// has none. Errors answer 503 database_unavailable.
	Open func(ctx context.Context) (Database, error)
	// NextMigrationVersion returns the version of the next migration file,
	// after every existing one.
	NextMigrationVersion func() (string, error)
	// Apply writes a migration file, after the clean-git check unless
	// allowDirty, then asks the supervisor to apply it.
	Apply func(ctx context.Context, plan genplan.Plan, allowDirty bool) error
	// SchemaStatus computes the live schema status now (ADR-0080): the
	// files against the database and the record of applied content, with
	// the source and applied files of the last published status. Nil in an
	// app without a database.
	SchemaStatus func(ctx context.Context) (SchemaStatus, error)
}

// DDLRequest is the body of a ddl plan or apply request.
type DDLRequest struct {
	Change pgmeta.Change `json:"change"`
	// Name names the migration file; empty derives it from the summary.
	Name       string `json:"name,omitempty"`
	AllowDirty bool   `json:"allow_dirty,omitempty"`
}

// DDLResponse answers a ddl plan or apply request.
type DDLResponse struct {
	Plan pgmeta.Plan `json:"plan"`
	// File is the migration file the plan becomes.
	File genplan.Change `json:"file"`
	// Applied reports the file was written and the migration queued.
	Applied bool `json:"applied"`
}

// RowsResponse answers an insert or update: the row as stored.
type RowsResponse struct {
	Row []pgmeta.Cell `json:"row"`
}

// DeleteResponse answers a delete.
type DeleteResponse struct {
	Deleted int64 `json:"deleted"`
}

// ImportRequest is the body of rows/import: rows to insert in one
// transaction, at most pgmeta.MaxLimit at a time.
type ImportRequest struct {
	Schema string                   `json:"schema"`
	Table  string                   `json:"table"`
	Rows   []map[string]pgmeta.Cell `json:"rows"`
}

// ImportResponse answers an import.
type ImportResponse struct {
	Inserted int64 `json:"inserted"`
}

// dbHandler routes the database API; every request passed the guard.
func (s *Server) dbHandler() http.Handler {
	mux := http.NewServeMux()
	get := func(pattern string, fn func(ctx context.Context, db Database, r *http.Request) (any, error)) {
		mux.HandleFunc("GET "+APIPrefix+"db/"+pattern, s.withDB(fn))
	}
	post := func(pattern string, fn func(ctx context.Context, db Database, r *http.Request) (any, error)) {
		mux.HandleFunc("POST "+APIPrefix+"db/"+pattern, s.withDB(fn))
	}
	get("schemas", func(ctx context.Context, db Database, _ *http.Request) (any, error) {
		v, err := db.Schemas(ctx)
		return map[string]any{"schemas": v}, err
	})
	get("tables", func(ctx context.Context, db Database, r *http.Request) (any, error) {
		schemas, err := schemasParam(ctx, db, r)
		if err != nil {
			return nil, err
		}
		v, err := db.Tables(ctx, schemas)
		return map[string]any{"tables": v}, err
	})
	get("tables/{schema}/{table}", func(ctx context.Context, db Database, r *http.Request) (any, error) {
		return db.Detail(ctx, r.PathValue("schema"), r.PathValue("table"))
	})
	get("foreign-keys", func(ctx context.Context, db Database, r *http.Request) (any, error) {
		schemas, err := schemasParam(ctx, db, r)
		if err != nil {
			return nil, err
		}
		v, err := db.ForeignKeys(ctx, schemas)
		return map[string]any{"foreign_keys": v}, err
	})
	get("enums", func(ctx context.Context, db Database, r *http.Request) (any, error) {
		schemas, err := schemasParam(ctx, db, r)
		if err != nil {
			return nil, err
		}
		v, err := db.Enums(ctx, schemas)
		return map[string]any{"enums": v}, err
	})
	get("functions", func(ctx context.Context, db Database, r *http.Request) (any, error) {
		schemas, err := schemasParam(ctx, db, r)
		if err != nil {
			return nil, err
		}
		v, err := db.Functions(ctx, schemas)
		return map[string]any{"functions": v}, err
	})
	get("views", func(ctx context.Context, db Database, r *http.Request) (any, error) {
		schemas, err := schemasParam(ctx, db, r)
		if err != nil {
			return nil, err
		}
		v, err := db.Views(ctx, schemas)
		return map[string]any{"views": v}, err
	})
	get("extensions", func(ctx context.Context, db Database, _ *http.Request) (any, error) {
		v, err := db.Extensions(ctx)
		return map[string]any{"extensions": v}, err
	})
	get("migrations", func(ctx context.Context, db Database, _ *http.Request) (any, error) {
		v, err := db.Migrations(ctx, s.cfg.Project.Dir)
		return map[string]any{"migrations": v}, err
	})
	get("types", func(ctx context.Context, db Database, r *http.Request) (any, error) {
		schemas, err := schemasParam(ctx, db, r)
		if err != nil {
			return nil, err
		}
		v, err := db.Types(ctx, schemas)
		return map[string]any{"types": v}, err
	})
	post("rows/query", func(ctx context.Context, db Database, r *http.Request) (any, error) {
		var q pgmeta.RowQuery
		if err := readBody(r, &q); err != nil {
			return nil, err
		}
		return db.Query(ctx, q)
	})
	post("rows/insert", func(ctx context.Context, db Database, r *http.Request) (any, error) {
		var e pgmeta.RowEdit
		if err := readBody(r, &e); err != nil {
			return nil, err
		}
		row, err := db.Insert(ctx, e)
		return RowsResponse{Row: row}, err
	})
	post("rows/import", func(ctx context.Context, db Database, r *http.Request) (any, error) {
		var req ImportRequest
		if err := readBody(r, &req); err != nil {
			return nil, err
		}
		n, err := db.InsertMany(ctx, req.Schema, req.Table, req.Rows)
		return ImportResponse{Inserted: n}, err
	})
	post("rows/update", func(ctx context.Context, db Database, r *http.Request) (any, error) {
		var e pgmeta.RowEdit
		if err := readBody(r, &e); err != nil {
			return nil, err
		}
		row, err := db.Update(ctx, e)
		return RowsResponse{Row: row}, err
	})
	post("rows/delete", func(ctx context.Context, db Database, r *http.Request) (any, error) {
		var e pgmeta.RowEdit
		if err := readBody(r, &e); err != nil {
			return nil, err
		}
		n, err := db.Delete(ctx, e)
		return DeleteResponse{Deleted: n}, err
	})
	post("ddl/plan", func(ctx context.Context, db Database, r *http.Request) (any, error) {
		return s.ddl(ctx, db, r, false)
	})
	post("ddl/apply", func(ctx context.Context, db Database, r *http.Request) (any, error) {
		return s.ddl(ctx, db, r, true)
	})
	// The live schema status (ADR-0080, schema.go): not through withDB, so
	// an app without a database answers {"database": false}.
	mux.HandleFunc("GET "+APIPrefix+"db/schema-status", s.serveSchemaStatus)
	// Observability (ADR-0073, observe.go).
	mux.HandleFunc("GET "+APIPrefix+"db/stats", s.serveDBStats)
	mux.HandleFunc("GET "+APIPrefix+"db/statements", s.serveDBStatements)
	mux.HandleFunc("POST "+APIPrefix+"db/statements/reset", s.serveDBStatementsReset)
	mux.HandleFunc("GET "+APIPrefix+"db/advice", s.serveDBAdvice)
	mux.HandleFunc(APIPrefix+"db/", func(w http.ResponseWriter, r *http.Request) {
		writeProblem(w, http.StatusNotFound, "not_found", "no portal endpoint "+r.Method+" "+r.URL.Path)
	})
	return mux
}

// withDB opens the database and answers fn's result or error.
func (s *Server) withDB(fn func(ctx context.Context, db Database, r *http.Request) (any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
		out, err := fn(ctx, db, r)
		if err != nil {
			writeDBError(w, err)
			return
		}
		_ = writeJSON(w, http.StatusOK, out)
	}
}

// writeDBError maps pgmeta's errors to problems.
func writeDBError(w http.ResponseWriter, err error) {
	var be *bodyError
	switch {
	case errors.As(err, &be):
		writeProblem(w, http.StatusBadRequest, "invalid_json", err.Error())
	case errors.Is(err, pgmeta.ErrNotFound):
		writeProblem(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, pgmeta.ErrUnknownColumn), errors.Is(err, pgmeta.ErrUnknownOperator), errors.Is(err, pgmeta.ErrInvalidInput):
		writeProblem(w, http.StatusUnprocessableEntity, "invalid_input", err.Error())
	case errors.Is(err, pgmeta.ErrNoPrimaryKey):
		writeProblem(w, http.StatusConflict, "no_primary_key", err.Error())
	case errors.Is(err, pgmeta.ErrRowCount):
		writeProblem(w, http.StatusConflict, "row_count", err.Error())
	case errors.Is(err, pgmeta.ErrSystemTable):
		writeProblem(w, http.StatusForbidden, "system_table", err.Error())
	case errors.Is(err, genplan.ErrExists), errors.Is(err, genplan.ErrStale):
		writeProblem(w, http.StatusConflict, "plan_conflict", err.Error())
	case errors.Is(err, context.DeadlineExceeded):
		writeProblem(w, http.StatusGatewayTimeout, "database_timeout", "the query took too long")
	default:
		writeProblem(w, http.StatusInternalServerError, "database_error", err.Error())
	}
}

type bodyError struct{ err error }

func (e *bodyError) Error() string { return "the body must be JSON: " + e.err.Error() }
func (e *bodyError) Unwrap() error { return e.err }

// readBody decodes a JSON body into v, refusing unknown fields.
func readBody(r *http.Request, v any) error {
	body, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, maxRequestBody))
	if err != nil {
		return &bodyError{err}
	}
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return &bodyError{err}
	}
	return nil
}

// schemasParam returns the ?schema= values, or every non-system schema.
func schemasParam(ctx context.Context, db Database, r *http.Request) ([]string, error) {
	if q := r.URL.Query()["schema"]; len(q) > 0 {
		return q, nil
	}
	all, err := db.Schemas(ctx)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, s := range all {
		if !s.System {
			names = append(names, s.Name)
		}
	}
	return names, nil
}

var slugPattern = regexp.MustCompile(`[^a-z0-9]+`)

// ddl plans a change and, on apply, writes it as a migration and queues
// the migrate command.
func (s *Server) ddl(ctx context.Context, db Database, r *http.Request, apply bool) (any, error) {
	var req DDLRequest
	if err := readBody(r, &req); err != nil {
		return nil, err
	}
	p, err := db.Plan(ctx, req.Change)
	if err != nil {
		return nil, err
	}
	if s.cfg.Database.NextMigrationVersion == nil {
		return nil, fmt.Errorf("%w: this orb can't write migrations", pgmeta.ErrInvalidInput)
	}
	version, err := s.cfg.Database.NextMigrationVersion()
	if err != nil {
		return nil, err
	}
	name := strings.Trim(slugPattern.ReplaceAllString(strings.ToLower(req.Name), "_"), "_")
	if name == "" {
		name = strings.Trim(slugPattern.ReplaceAllString(strings.ToLower(p.Summary), "_"), "_")
	}
	if len(name) > 60 {
		name = name[:60]
	}
	file := genplan.Change{Path: "db/migrations/" + version + "_" + name + ".sql", Kind: genplan.Create, Content: pgmeta.Render(p)}
	out := DDLResponse{Plan: p, File: file}
	if !apply {
		return out, nil
	}
	if s.cfg.Database.Apply == nil {
		return nil, fmt.Errorf("%w: this orb can't apply migrations", pgmeta.ErrInvalidInput)
	}
	plan := genplan.Plan{Generator: "ddl", Name: name, Summary: p.Summary, Changes: []genplan.Change{file}}
	if err := s.cfg.Database.Apply(ctx, plan, req.AllowDirty); err != nil {
		return nil, err
	}
	out.Applied = true
	return out, nil
}
