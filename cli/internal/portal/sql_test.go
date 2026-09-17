package portal

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gorbital.dev/cli/internal/pgmeta"
)

// runnerDB is a fakeDB that also runs scripts.
type runnerDB struct {
	fakeDB
	runs []pgmeta.RunRequest
}

func (r *runnerDB) Run(_ context.Context, req pgmeta.RunRequest) (pgmeta.RunResult, error) {
	r.runs = append(r.runs, req)
	if strings.Contains(req.SQL, "boom") {
		return pgmeta.RunResult{Mode: "rollback", Statements: []pgmeta.StatementResult{}, RolledBack: true,
			Error: &pgmeta.RunError{Message: "syntax error", Code: "42601", Position: 1, Line: 1}}, nil
	}
	if req.Mode == "bad" {
		return pgmeta.RunResult{}, pgmeta.ErrInvalidInput
	}
	return pgmeta.RunResult{Mode: "rollback", RolledBack: true, DurationMS: 1.5, Statements: []pgmeta.StatementResult{
		{Command: "SELECT 2", Columns: []string{"n"}, Rows: [][]pgmeta.Cell{{s("1")}, {s("2")}}},
	}}, nil
}

func (r *runnerDB) Explain(_ context.Context, sql string, analyze bool) (json.RawMessage, error) {
	if strings.Contains(sql, "boom") {
		return nil, pgmeta.ErrInvalidInput
	}
	return json.RawMessage(`[{"Plan":{"Node Type":"Seq Scan","Analyze":` + map[bool]string{true: "true", false: "false"}[analyze] + `}}]`), nil
}

func newSQLServer(t *testing.T) (*Server, *httptest.Server, *runnerDB) {
	t.Helper()
	db := &runnerDB{}
	srv, ts, _ := newDBServer(t, db, nil)
	srv.cfg.SQL = NewSQLStore(srv.cfg.Project.Dir)
	return srv, ts, db
}

func TestSQLRunExplainCheckTemplates(t *testing.T) {
	srv, ts, db := newSQLServer(t)

	res := call(t, ts, http.MethodGet, APIPrefix+"db/sql/templates", "", nil)
	if out := decode[struct{ Templates []pgmeta.Template }](t, res); res.StatusCode != 200 || len(out.Templates) < 5 || out.Templates[0].SQL == "" {
		t.Errorf("templates = %d %d", res.StatusCode, len(out.Templates))
	}
	res = call(t, ts, http.MethodPost, APIPrefix+"db/sql/check", `{"sql":"DELETE FROM users;\nSELECT 1"}`, nil)
	if out := decode[CheckResponse](t, res); res.StatusCode != 200 || len(out.Warnings) != 1 || out.Warnings[0].Kind != "delete_without_where" {
		t.Errorf("check = %d %+v", res.StatusCode, out)
	}
	res = call(t, ts, http.MethodPost, APIPrefix+"db/sql/check", `{"sql":"SELECT 1"}`, nil)
	if body := decodeRaw(t, res); !strings.Contains(body, `"warnings":[]`) {
		t.Errorf("check without warnings = %s", body)
	}

	res = call(t, ts, http.MethodPost, APIPrefix+"db/sql/run", `{"sql":"SELECT generate_series(1,2) AS n","row_limit":10}`, nil)
	out := decode[pgmeta.RunResult](t, res)
	if res.StatusCode != 200 || len(out.Statements) != 1 || len(out.Statements[0].Rows) != 2 || db.runs[0].RowLimit != 10 {
		t.Errorf("run = %d %+v", res.StatusCode, out)
	}
	res = call(t, ts, http.MethodPost, APIPrefix+"db/sql/run", `{"sql":"boom"}`, nil)
	if out := decode[pgmeta.RunResult](t, res); res.StatusCode != 200 || out.Error == nil || out.Error.Code != "42601" {
		t.Errorf("run with a server error = %d %+v (the error is data, not a problem)", res.StatusCode, out)
	}
	res = call(t, ts, http.MethodPost, APIPrefix+"db/sql/run", `{"sql":"SELECT 1","mode":"bad"}`, nil)
	if p := decode[problem](t, res); res.StatusCode != 422 || p.Code != "invalid_input" {
		t.Errorf("bad mode = %d %+v", res.StatusCode, p)
	}

	// Runs are in the history, newest first, errors included.
	res = call(t, ts, http.MethodGet, APIPrefix+"db/sql/history", "", nil)
	hist := decode[struct {
		History []HistoryEntry
		Max     int
	}](t, res)
	if res.StatusCode != 200 || len(hist.History) != 2 || hist.History[0].SQL != "boom" || hist.History[0].Error == "" || hist.History[1].Rows != 2 || hist.Max != MaxHistory {
		t.Errorf("history = %d %+v", res.StatusCode, hist)
	}
	if _, err := os.Stat(filepath.Join(srv.cfg.Project.Dir, ".orb", "portal", "sql-history.jsonl")); err != nil {
		t.Errorf("history file: %v", err)
	}
	res = call(t, ts, http.MethodDelete, APIPrefix+"db/sql/history", "", nil)
	if res.StatusCode != 200 {
		t.Errorf("clear history = %d", res.StatusCode)
	}
	res = call(t, ts, http.MethodGet, APIPrefix+"db/sql/history", "", nil)
	if hist := decode[struct{ History []HistoryEntry }](t, res); len(hist.History) != 0 {
		t.Errorf("history after clear = %+v", hist)
	}

	res = call(t, ts, http.MethodPost, APIPrefix+"db/sql/explain", `{"sql":"SELECT 1","analyze":true}`, nil)
	if out := decode[ExplainResponse](t, res); res.StatusCode != 200 || !strings.Contains(string(out.Plan), `"Analyze":true`) {
		t.Errorf("explain = %d %s", res.StatusCode, out.Plan)
	}
	res = call(t, ts, http.MethodPost, APIPrefix+"db/sql/explain", `{"sql":"boom"}`, nil)
	if res.StatusCode != 422 {
		t.Errorf("explain error = %d", res.StatusCode)
	}
}

func TestSQLSnippets(t *testing.T) {
	srv, ts, _ := newSQLServer(t)
	res := call(t, ts, http.MethodGet, APIPrefix+"db/sql/snippets", "", nil)
	if body := decodeRaw(t, res); res.StatusCode != 200 || !strings.Contains(body, `"snippets":[]`) {
		t.Errorf("empty snippets = %d %s", res.StatusCode, body)
	}
	res = call(t, ts, http.MethodPut, APIPrefix+"db/sql/snippets/slow-queries", `{"sql":"SELECT 1;","favorite":true}`, nil)
	if sn := decode[Snippet](t, res); res.StatusCode != 200 || sn.Name != "slow-queries" || !sn.Favorite || sn.Path != "db/queries/slow-queries.sql" {
		t.Errorf("save = %d %+v", res.StatusCode, sn)
	}
	call(t, ts, http.MethodPut, APIPrefix+"db/sql/snippets/aaa", `{"sql":"SELECT 2;"}`, nil)
	data, err := os.ReadFile(filepath.Join(srv.cfg.Project.Dir, "db", "queries", "slow-queries.sql"))
	if err != nil || string(data) != "SELECT 1;" {
		t.Errorf("snippet file = %q, %v", data, err)
	}
	res = call(t, ts, http.MethodGet, APIPrefix+"db/sql/snippets", "", nil)
	list := decode[struct{ Snippets []Snippet }](t, res)
	if len(list.Snippets) != 2 || list.Snippets[0].Name != "slow-queries" || list.Snippets[1].Name != "aaa" {
		t.Errorf("snippets = %+v (favourites first)", list.Snippets)
	}
	res = call(t, ts, http.MethodPut, APIPrefix+"db/sql/snippets/..%2Fevil", `{"sql":"x"}`, nil)
	if res.StatusCode != 422 && res.StatusCode != 404 {
		t.Errorf("bad name = %d", res.StatusCode)
	}
	res = call(t, ts, http.MethodPut, APIPrefix+"db/sql/snippets/has%20space", `{"sql":"x"}`, nil)
	if p := decode[problem](t, res); res.StatusCode != 422 || p.Code != "invalid_input" {
		t.Errorf("name with a space = %d %+v", res.StatusCode, p)
	}
	res = call(t, ts, http.MethodDelete, APIPrefix+"db/sql/snippets/slow-queries", "", nil)
	if res.StatusCode != 200 {
		t.Errorf("delete = %d", res.StatusCode)
	}
	res = call(t, ts, http.MethodGet, APIPrefix+"db/sql/snippets", "", nil)
	if list := decode[struct{ Snippets []Snippet }](t, res); len(list.Snippets) != 1 || list.Snippets[0].Favorite {
		t.Errorf("after delete = %+v", list.Snippets)
	}
	// The favourite flag alone can change.
	res = call(t, ts, http.MethodPut, APIPrefix+"db/sql/snippets/aaa", `{"sql":"SELECT 2;","favorite":true}`, nil)
	if sn := decode[Snippet](t, res); !sn.Favorite {
		t.Errorf("favourite = %+v", sn)
	}
}

func TestSQLMigration(t *testing.T) {
	srv, ts, _ := newSQLServer(t)
	res := call(t, ts, http.MethodPost, APIPrefix+"db/sql/migration", `{"name":"Add phone","sql":"ALTER TABLE users ADD COLUMN phone text;"}`, nil)
	out := decode[DDLResponse](t, res)
	if res.StatusCode != 200 || out.Applied || out.File.Path != "db/migrations/20260916120000_add_phone.sql" || !strings.Contains(string(out.File.Content), "-- +goose Up\nALTER TABLE users ADD COLUMN phone text;\n") {
		t.Errorf("migration preview = %d %+v", res.StatusCode, out)
	}
	if _, err := os.Stat(filepath.Join(srv.cfg.Project.Dir, "db")); err == nil {
		t.Error("preview wrote a file")
	}
	res = call(t, ts, http.MethodPost, APIPrefix+"db/sql/migration", `{"name":"Add phone","sql":"ALTER TABLE users ADD COLUMN phone text;","apply":true,"allow_dirty":true}`, nil)
	if out := decode[DDLResponse](t, res); res.StatusCode != 200 || !out.Applied {
		t.Errorf("migration apply = %d %+v", res.StatusCode, out)
	}
	if _, err := os.Stat(filepath.Join(srv.cfg.Project.Dir, "db", "migrations", "20260916120000_add_phone.sql")); err != nil {
		t.Errorf("migration file: %v", err)
	}
	res = call(t, ts, http.MethodPost, APIPrefix+"db/sql/migration", `{"name":"","sql":"SELECT 1"}`, nil)
	if res.StatusCode != 422 {
		t.Errorf("no name = %d", res.StatusCode)
	}
}

func TestSQLWithoutDatabase(t *testing.T) {
	_, ts, _ := newTestServer(t, nil)
	for _, path := range []string{"db/sql/snippets", "db/sql/history"} {
		res := call(t, ts, http.MethodGet, APIPrefix+path, "", nil)
		if p := decode[problem](t, res); res.StatusCode != 404 || p.Code != "no_database" {
			t.Errorf("%s without a database = %d %+v", path, res.StatusCode, p)
		}
	}
	res := call(t, ts, http.MethodPost, APIPrefix+"db/sql/run", `{"sql":"SELECT 1"}`, nil)
	if res.StatusCode != 404 {
		t.Errorf("run without a database = %d", res.StatusCode)
	}
}

func TestSQLStoreHistoryCap(t *testing.T) {
	store := NewSQLStore(t.TempDir())
	for i := range MaxHistory + 150 {
		if err := store.Record(HistoryEntry{SQL: "SELECT " + strings.Repeat("x", i%3), Mode: "rollback"}); err != nil {
			t.Fatal(err)
		}
	}
	hist, err := store.History()
	if err != nil || len(hist) != MaxHistory {
		t.Errorf("History() = %d entries, %v; want %d", len(hist), err, MaxHistory)
	}
	if err := store.ClearHistory(); err != nil {
		t.Fatal(err)
	}
	if err := store.ClearHistory(); err != nil {
		t.Errorf("clearing twice: %v", err)
	}
	if _, err := store.Save("../x", "SELECT 1", false); !errors.Is(err, ErrSnippetName) {
		t.Errorf("Save(../x) = %v", err)
	}
}

func decodeRaw(t *testing.T, res *http.Response) string {
	t.Helper()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
