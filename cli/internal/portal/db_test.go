package portal

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gorbital.dev/cli/internal/genplan"
	"gorbital.dev/cli/internal/pgmeta"
)

// fakeDB answers the Table Editor from memory.
type fakeDB struct {
	rows    [][]pgmeta.Cell
	planned []pgmeta.Change
}

func s(v string) *string { return &v }

func (f *fakeDB) Schemas(context.Context) ([]pgmeta.Schema, error) {
	return []pgmeta.Schema{{Name: "public"}, {Name: "pg_catalog", System: true}}, nil
}

func (f *fakeDB) Tables(_ context.Context, schemas []string) ([]pgmeta.Table, error) {
	if strings.Join(schemas, ",") != "public" {
		return nil, errors.New("unexpected schemas " + strings.Join(schemas, ","))
	}
	return []pgmeta.Table{{Schema: "public", Name: "projects", Kind: "table", Ownership: pgmeta.OwnershipUser}}, nil
}

func (f *fakeDB) Detail(_ context.Context, schema, table string) (pgmeta.TableDetail, error) {
	if table != "projects" {
		return pgmeta.TableDetail{}, pgmeta.ErrNotFound
	}
	return pgmeta.TableDetail{Table: pgmeta.Table{Schema: schema, Name: table}, Columns: []pgmeta.Column{{Name: "id"}, {Name: "name"}}, PrimaryKey: []string{"id"}}, nil
}

func (f *fakeDB) ForeignKeys(context.Context, []string) ([]pgmeta.ForeignKey, error) { return nil, nil }
func (f *fakeDB) Enums(context.Context, []string) ([]pgmeta.Enum, error)             { return nil, nil }
func (f *fakeDB) Functions(context.Context, []string) ([]pgmeta.Function, error)     { return nil, nil }
func (f *fakeDB) Views(context.Context, []string) ([]pgmeta.View, error)             { return nil, nil }
func (f *fakeDB) Extensions(context.Context) ([]pgmeta.Extension, error)             { return nil, nil }
func (f *fakeDB) Types(context.Context, []string) ([]pgmeta.TypeOption, error) {
	return []pgmeta.TypeOption{{Name: "text"}}, nil
}

func (f *fakeDB) Query(_ context.Context, q pgmeta.RowQuery) (pgmeta.RowPage, error) {
	for _, fl := range q.Filters {
		if fl.Column == "nope" {
			return pgmeta.RowPage{}, pgmeta.ErrUnknownColumn
		}
	}
	return pgmeta.RowPage{Columns: []pgmeta.Column{{Name: "id"}, {Name: "name"}}, PrimaryKey: []string{"id"}, Rows: f.rows, Count: int64(len(f.rows)), Limit: q.Limit}, nil
}

func (f *fakeDB) Insert(_ context.Context, e pgmeta.RowEdit) ([]pgmeta.Cell, error) {
	if e.Table == "river_job" {
		return nil, pgmeta.ErrSystemTable
	}
	row := []pgmeta.Cell{s("9"), e.Values["name"]}
	f.rows = append(f.rows, row)
	return row, nil
}

func (f *fakeDB) InsertMany(_ context.Context, _, _ string, rows []map[string]pgmeta.Cell) (int64, error) {
	return int64(len(rows)), nil
}

func (f *fakeDB) Update(_ context.Context, e pgmeta.RowEdit) ([]pgmeta.Cell, error) {
	if *e.Keys[0]["id"] == "404" {
		return nil, pgmeta.ErrRowCount
	}
	return []pgmeta.Cell{e.Keys[0]["id"], e.Values["name"]}, nil
}

func (f *fakeDB) Delete(_ context.Context, e pgmeta.RowEdit) (int64, error) {
	if e.Table == "nokey" {
		return 0, pgmeta.ErrNoPrimaryKey
	}
	return int64(len(e.Keys)), nil
}

func (f *fakeDB) Plan(_ context.Context, ch pgmeta.Change) (pgmeta.Plan, error) {
	if ch.Kind == "add_column" && ch.Column != nil && ch.Column.Type == "money" {
		return pgmeta.Plan{}, pgmeta.ErrInvalidInput
	}
	f.planned = append(f.planned, ch)
	return pgmeta.Plan{Summary: "Add phone to projects", Up: []string{`ALTER TABLE "public"."projects" ADD COLUMN "phone" text;`}, Down: []string{`ALTER TABLE "public"."projects" DROP COLUMN "phone";`}}, nil
}

func newDBServer(t *testing.T, db Database, openErr error) (*Server, *httptest.Server, *fakeSupervisor) {
	t.Helper()
	dir := t.TempDir()
	var sup *fakeSupervisor
	srv, ts, fake := newTestServer(t, func(c *Config) {
		c.Project.Dir = dir
		c.Database = DatabaseConfig{
			Open:                 func(context.Context) (Database, error) { return db, openErr },
			NextMigrationVersion: func() (string, error) { return "20260916120000", nil },
			Apply: func(_ context.Context, plan genplan.Plan, allowDirty bool) error {
				if !allowDirty {
					return errors.New("uncommitted changes")
				}
				if err := genplan.Apply(dir, plan); err != nil {
					return err
				}
				return sup.Migrate()
			},
		}
	})
	sup = fake
	return srv, ts, sup
}

func TestDatabaseCatalogAndRows(t *testing.T) {
	db := &fakeDB{rows: [][]pgmeta.Cell{{s("1"), s("Website")}}}
	_, ts, _ := newDBServer(t, db, nil)

	var schemas struct{ Schemas []pgmeta.Schema }
	res := call(t, ts, http.MethodGet, APIPrefix+"db/schemas", "", nil)
	if schemas = decode[struct{ Schemas []pgmeta.Schema }](t, res); res.StatusCode != 200 || len(schemas.Schemas) != 2 {
		t.Errorf("schemas = %d %+v", res.StatusCode, schemas)
	}
	// Without ?schema= the non-system schemas are used.
	res = call(t, ts, http.MethodGet, APIPrefix+"db/tables", "", nil)
	if tables := decode[struct{ Tables []pgmeta.Table }](t, res); res.StatusCode != 200 || len(tables.Tables) != 1 {
		t.Errorf("tables = %d %+v", res.StatusCode, tables)
	}
	res = call(t, ts, http.MethodGet, APIPrefix+"db/tables/public/projects", "", nil)
	if d := decode[pgmeta.TableDetail](t, res); res.StatusCode != 200 || len(d.Columns) != 2 {
		t.Errorf("detail = %d %+v", res.StatusCode, d)
	}
	res = call(t, ts, http.MethodGet, APIPrefix+"db/tables/public/nope", "", nil)
	if p := decode[problem](t, res); res.StatusCode != 404 || p.Code != "not_found" {
		t.Errorf("missing table = %d %+v", res.StatusCode, p)
	}
	res = call(t, ts, http.MethodGet, APIPrefix+"db/types", "", nil)
	if res.StatusCode != 200 {
		t.Errorf("types = %d", res.StatusCode)
	}

	res = call(t, ts, http.MethodPost, APIPrefix+"db/rows/query", `{"schema":"public","table":"projects","limit":50}`, nil)
	if page := decode[pgmeta.RowPage](t, res); res.StatusCode != 200 || page.Count != 1 || *page.Rows[0][1] != "Website" || page.Limit != 50 {
		t.Errorf("query = %d %+v", res.StatusCode, page)
	}
	res = call(t, ts, http.MethodPost, APIPrefix+"db/rows/query", `{"schema":"public","table":"projects","filters":[{"column":"nope","operator":"="}]}`, nil)
	if p := decode[problem](t, res); res.StatusCode != 422 || p.Code != "invalid_input" {
		t.Errorf("unknown column = %d %+v", res.StatusCode, p)
	}
	res = call(t, ts, http.MethodPost, APIPrefix+"db/rows/query", `{"schema":"public","table":"projects","colour":1}`, nil)
	if p := decode[problem](t, res); res.StatusCode != 400 || p.Code != "invalid_json" {
		t.Errorf("unknown field = %d %+v", res.StatusCode, p)
	}
	res = call(t, ts, http.MethodPost, APIPrefix+"db/rows/insert", `{"schema":"public","table":"projects","values":{"name":"New"}}`, nil)
	if r := decode[RowsResponse](t, res); res.StatusCode != 200 || *r.Row[1] != "New" {
		t.Errorf("insert = %d %+v", res.StatusCode, r)
	}
	res = call(t, ts, http.MethodPost, APIPrefix+"db/rows/insert", `{"schema":"public","table":"river_job","values":{"id":"1"}}`, nil)
	if p := decode[problem](t, res); res.StatusCode != 403 || p.Code != "system_table" {
		t.Errorf("system table = %d %+v", res.StatusCode, p)
	}
	res = call(t, ts, http.MethodPost, APIPrefix+"db/rows/import", `{"schema":"public","table":"projects","rows":[{"name":"a"},{"name":"b"}]}`, nil)
	if r := decode[ImportResponse](t, res); res.StatusCode != 200 || r.Inserted != 2 {
		t.Errorf("import = %d %+v", res.StatusCode, r)
	}
	res = call(t, ts, http.MethodPost, APIPrefix+"db/rows/update", `{"schema":"public","table":"projects","keys":[{"id":"1"}],"values":{"name":null}}`, nil)
	if r := decode[RowsResponse](t, res); res.StatusCode != 200 || r.Row[1] != nil {
		t.Errorf("update to null = %d %+v", res.StatusCode, r)
	}
	res = call(t, ts, http.MethodPost, APIPrefix+"db/rows/update", `{"schema":"public","table":"projects","keys":[{"id":"404"}],"values":{"name":"x"}}`, nil)
	if p := decode[problem](t, res); res.StatusCode != 409 || p.Code != "row_count" {
		t.Errorf("missing row = %d %+v", res.StatusCode, p)
	}
	res = call(t, ts, http.MethodPost, APIPrefix+"db/rows/delete", `{"schema":"public","table":"projects","keys":[{"id":"1"},{"id":"2"}]}`, nil)
	if r := decode[DeleteResponse](t, res); res.StatusCode != 200 || r.Deleted != 2 {
		t.Errorf("delete = %d %+v", res.StatusCode, r)
	}
	res = call(t, ts, http.MethodPost, APIPrefix+"db/rows/delete", `{"schema":"public","table":"nokey","keys":[{"a":"1"}]}`, nil)
	if p := decode[problem](t, res); res.StatusCode != 409 || p.Code != "no_primary_key" {
		t.Errorf("no key = %d %+v", res.StatusCode, p)
	}
}

func TestDatabaseDDL(t *testing.T) {
	db := &fakeDB{}
	srv, ts, sup := newDBServer(t, db, nil)
	body := `{"change":{"kind":"add_column","schema":"public","table":"projects","column":{"name":"phone","type":"text"}}}`

	res := call(t, ts, http.MethodPost, APIPrefix+"db/ddl/plan", body, nil)
	out := decode[DDLResponse](t, res)
	if res.StatusCode != 200 || out.Applied || out.File.Path != "db/migrations/20260916120000_add_phone_to_projects.sql" || !strings.Contains(string(out.File.Content), "-- +goose Up\nALTER TABLE") {
		t.Errorf("plan = %d %+v", res.StatusCode, out)
	}
	if _, err := os.Stat(filepath.Join(srv.cfg.Project.Dir, "db")); err == nil {
		t.Error("plan wrote a file")
	}
	res = call(t, ts, http.MethodPost, APIPrefix+"db/ddl/apply", body, nil)
	if res.StatusCode != http.StatusInternalServerError {
		bodyBytes, _ := io.ReadAll(res.Body)
		t.Errorf("apply with a dirty tree = %d %s", res.StatusCode, bodyBytes)
	}
	res = call(t, ts, http.MethodPost, APIPrefix+"db/ddl/apply", strings.TrimSuffix(body, "}")+`,"name":"Add phone","allow_dirty":true}`, nil)
	out = decode[DDLResponse](t, res)
	if res.StatusCode != 200 || !out.Applied || out.File.Path != "db/migrations/20260916120000_add_phone.sql" {
		t.Fatalf("apply = %d %+v", res.StatusCode, out)
	}
	written, err := os.ReadFile(filepath.Join(srv.cfg.Project.Dir, filepath.FromSlash(out.File.Path)))
	if err != nil || string(written) != string(out.File.Content) {
		t.Errorf("file = %q, %v", written, err)
	}
	if strings.Join(sup.actions, ",") != "migrate" {
		t.Errorf("supervisor actions = %q, want migrate", sup.actions)
	}
	res = call(t, ts, http.MethodPost, APIPrefix+"db/ddl/plan", `{"change":{"kind":"add_column","schema":"public","table":"projects","column":{"name":"x","type":"money"}}}`, nil)
	if p := decode[problem](t, res); res.StatusCode != 422 || p.Code != "invalid_input" {
		t.Errorf("bad type = %d %+v", res.StatusCode, p)
	}
}

func TestDatabaseUnavailableOrAbsent(t *testing.T) {
	_, ts, _ := newDBServer(t, nil, errors.New("connection refused"))
	res := call(t, ts, http.MethodGet, APIPrefix+"db/schemas", "", nil)
	if p := decode[problem](t, res); res.StatusCode != 503 || p.Code != "database_unavailable" {
		t.Errorf("unreachable = %d %+v", res.StatusCode, p)
	}
	_, ts2, _ := newTestServer(t, nil)
	res = call(t, ts2, http.MethodGet, APIPrefix+"db/schemas", "", nil)
	if p := decode[problem](t, res); res.StatusCode != 404 || p.Code != "no_database" {
		t.Errorf("no database = %d %+v", res.StatusCode, p)
	}
	status := decode[Status](t, call(t, ts2, http.MethodGet, APIPrefix+"status", "", nil))
	if status.Portal.Database {
		t.Error("status reports a database for an app without one")
	}
}
