package portal

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gorbital.dev/cli/internal/pgmeta"
)

func migration(version int64, name string, applied bool, sql string) pgmeta.Migration {
	return pgmeta.Migration{Version: version, Name: name, Path: "db/migrations/" + name + ".sql", SQL: sql, Applied: applied}
}

func TestComputeSchemaStatus(t *testing.T) {
	now := time.Date(2026, 9, 17, 6, 0, 0, 0, time.FixedZone("x", 3600))
	migrations := []pgmeta.Migration{
		migration(10, "10_init", true, "create table a"),
		migration(20, "20_invoices", true, "create table invoices (edited)"),
		migration(15, "15_late", false, "create table late"),
		migration(30, "30_new", false, "create table new"),
		{Version: 5, Applied: true}, // a version without a file
	}
	record := map[string]string{"10_init.sql": HashSQL("create table a"), "20_invoices.sql": HashSQL("create table invoices")}

	st := ComputeSchemaStatus(SchemaInput{Migrations: migrations, Record: record, Source: SchemaSourceCode, Now: now})
	if !st.Database || st.Source != SchemaSourceCode || !st.CheckedAt.Equal(now) || st.CheckedAt.Location() != time.UTC {
		t.Errorf("status = %+v", st)
	}
	wantPending := []PendingMigration{{File: "15_late.sql", Version: "15", Reason: PendingOutOfOrder}, {File: "30_new.sql", Version: "30", Reason: PendingNew}}
	if len(st.Pending) != 2 || st.Pending[0] != wantPending[0] || st.Pending[1] != wantPending[1] {
		t.Errorf("pending = %+v, want %+v", st.Pending, wantPending)
	}
	if len(st.Edited) != 1 || st.Edited[0] != (EditedMigration{File: "20_invoices.sql", Version: "20"}) {
		t.Errorf("edited = %+v", st.Edited)
	}
	if !st.NeedsRestart || st.Applied == nil || len(st.Applied) != 0 || st.Problem != "" {
		t.Errorf("needs_restart = %v, applied = %#v, problem = %q", st.NeedsRestart, st.Applied, st.Problem)
	}

	// With reload on and the app running, orb dev applies pending files on
	// its own; a failed migrate means it won't.
	if st := ComputeSchemaStatus(SchemaInput{Migrations: migrations, AutoApply: true}); st.NeedsRestart {
		t.Error("needs_restart with auto-apply")
	}
	if st := ComputeSchemaStatus(SchemaInput{Migrations: migrations, AutoApply: true, Problem: "migrations failed"}); !st.NeedsRestart || st.Problem != "migrations failed" {
		t.Errorf("after a failed migrate = %+v", st)
	}
	// Nothing pending never needs a restart, whatever the mode.
	if st := ComputeSchemaStatus(SchemaInput{Migrations: migrations[:2], Record: record, Applied: []string{"20_invoices.sql"}, Source: SchemaSourceMigrate}); st.NeedsRestart || len(st.Pending) != 0 || len(st.Applied) != 1 || len(st.Edited) != 1 {
		t.Errorf("all applied = %+v", st)
	}
	// An applied file the record doesn't know isn't reported as edited.
	if st := ComputeSchemaStatus(SchemaInput{Migrations: migrations[:2]}); len(st.Edited) != 0 {
		t.Errorf("without a record, edited = %+v", st.Edited)
	}
	if st := ComputeSchemaStatus(SchemaInput{}); !st.Database || st.CheckedAt.IsZero() || st.Pending == nil || st.Edited == nil {
		t.Errorf("empty input = %+v", st)
	}
}

func TestSchemaStatusJSON(t *testing.T) {
	data, _ := json.Marshal(noDatabaseSchema())
	if want := `{"database":false,"applied":[],"pending":[],"edited":[],"needs_restart":false,"problem":""}`; string(data) != want {
		t.Errorf("no database = %s, want %s", data, want)
	}
	now := time.Date(2026, 9, 17, 6, 0, 0, 0, time.UTC)
	st := ComputeSchemaStatus(SchemaInput{Migrations: []pgmeta.Migration{migration(20, "20_x", false, "")}, Source: SchemaSourceCode, Applied: []string{"10_invoices.sql"}, Now: now})
	data, _ = json.Marshal(st)
	want := `{"database":true,"source":"code","checked_at":"2026-09-17T06:00:00Z","applied":["10_invoices.sql"],"pending":[{"file":"20_x.sql","version":"20","reason":"new"}],"edited":[],"needs_restart":true,"problem":""}`
	if string(data) != want {
		t.Errorf("status = %s, want %s", data, want)
	}
}

func TestMigrationRecord(t *testing.T) {
	dir := t.TempDir()
	r, err := LoadMigrationRecord(dir)
	if err != nil || len(r.Applied) != 0 {
		t.Fatalf("missing record = %+v, %v", r, err)
	}
	migrations := []pgmeta.Migration{
		migration(10, "10_init", true, "a"),
		migration(20, "20_invoices", true, "b2"),
		migration(30, "30_new", false, "c"),
		{Version: 5, Applied: true},
	}
	r.Applied["20_invoices.sql"] = HashSQL("b1")
	r.Applied["40_gone.sql"] = HashSQL("gone")
	r = r.Update(migrations, nil)
	want := map[string]string{"10_init.sql": HashSQL("a"), "20_invoices.sql": HashSQL("b1")}
	if len(r.Applied) != len(want) || r.Applied["10_init.sql"] != want["10_init.sql"] || r.Applied["20_invoices.sql"] != want["20_invoices.sql"] {
		t.Errorf("Update() = %v, want %v (new files baselined, known kept, unapplied dropped)", r.Applied, want)
	}
	// A refreshed file (applied again by a redo) takes its current content.
	if r = r.Update(migrations, []string{"20_invoices.sql"}); r.Applied["20_invoices.sql"] != HashSQL("b2") {
		t.Errorf("refreshed hash = %s, want the current content's", r.Applied["20_invoices.sql"])
	}
	if err := r.Save(dir); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".orb", "portal", "migrations.json"))
	if err != nil || !strings.Contains(string(data), `"10_init.sql": "`+HashSQL("a")+`"`) {
		t.Errorf("record file = %s, %v", data, err)
	}
	if again, err := LoadMigrationRecord(dir); err != nil || len(again.Applied) != 2 || again.Applied["20_invoices.sql"] != HashSQL("b2") {
		t.Errorf("reloaded = %+v, %v", again, err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".orb", "portal", "migrations.json"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if r, err := LoadMigrationRecord(dir); err == nil || r.Applied == nil {
		t.Errorf("corrupt record = %+v, %v; want an error and an empty record", r, err)
	}
}

func TestHubSchema(t *testing.T) {
	hub := NewHubSize(2)
	if _, ok := hub.Schema(); ok {
		t.Error("a new hub has a schema status")
	}
	sub := hub.Subscribe()
	defer hub.Unsubscribe(sub)
	st := ComputeSchemaStatus(SchemaInput{Source: SchemaSourceStartup})
	hub.SetSchema(st)
	select {
	case e := <-sub.C:
		if e.Type != "schema" || e.Schema == nil || e.Schema.Source != SchemaSourceStartup || e.State != nil || e.Output != nil {
			t.Errorf("event = %+v", e)
		}
	default:
		t.Fatal("no schema event")
	}
	if got, ok := hub.Schema(); !ok || got.Source != SchemaSourceStartup {
		t.Errorf("Schema() = %+v, %v", got, ok)
	}
	// Output and state still work as before.
	hub.AddLine("app", "x")
	hub.SetState(AppStatus{State: StateRunning})
	if e := <-sub.C; e.Type != "output" {
		t.Errorf("output event = %+v", e)
	}
	if e := <-sub.C; e.Type != "state" || e.Schema != nil {
		t.Errorf("state event = %+v", e)
	}
}

// nextEvent reads one Server-Sent Event.
func nextEvent(t *testing.T, reader *bufio.Reader) (string, string) {
	t.Helper()
	var event, data string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		line = strings.TrimRight(line, "\n")
		switch {
		case strings.HasPrefix(line, "event: "):
			event = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			data = strings.TrimPrefix(line, "data: ")
		case line == "" && event != "":
			return event, data
		}
	}
}

func TestEventsStreamCarriesSchema(t *testing.T) {
	s, ts, _ := newTestServer(t, nil)
	s.cfg.Hub.SetSchema(ComputeSchemaStatus(SchemaInput{Source: SchemaSourceStartup}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+APIPrefix+"events", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: testToken})
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	reader := bufio.NewReader(res.Body)
	if event, _ := nextEvent(t, reader); event != "state" {
		t.Fatalf("first event = %s, want state", event)
	}
	if event, data := nextEvent(t, reader); event != "schema" || !strings.Contains(data, `"type":"schema"`) || !strings.Contains(data, `"source":"startup"`) {
		t.Errorf("second event = %s %s, want the latest schema status", event, data)
	}
	s.cfg.Hub.SetSchema(ComputeSchemaStatus(SchemaInput{Source: SchemaSourceCode, Migrations: []pgmeta.Migration{migration(20, "20_x", false, "")}}))
	if event, data := nextEvent(t, reader); event != "schema" || !strings.Contains(data, `"source":"code"`) || !strings.Contains(data, `"needs_restart":true`) {
		t.Errorf("schema event = %s %s", event, data)
	}
}

func TestSchemaStatusEndpoint(t *testing.T) {
	// Without a database: 200 and {"database": false}, not 404.
	_, ts, _ := newTestServer(t, func(c *Config) { c.Project.Database = false })
	res := call(t, ts, http.MethodGet, APIPrefix+"db/schema-status", "", nil)
	if st := decode[SchemaStatus](t, res); res.StatusCode != http.StatusOK || st.Database || st.Applied == nil || st.Pending == nil {
		t.Errorf("without a database = %d %+v", res.StatusCode, st)
	}

	var fail error
	_, ts, _ = newTestServer(t, func(c *Config) {
		c.Database = DatabaseConfig{
			Open: func(context.Context) (Database, error) { return &fakeDB{}, nil },
			SchemaStatus: func(context.Context) (SchemaStatus, error) {
				if fail != nil {
					return SchemaStatus{}, fail
				}
				return ComputeSchemaStatus(SchemaInput{Source: SchemaSourceMigrate, Applied: []string{"10_init.sql"}, Migrations: []pgmeta.Migration{migration(20, "20_x", false, "")}}), nil
			},
		}
	})
	res = call(t, ts, http.MethodGet, APIPrefix+"db/schema-status", "", nil)
	if st := decode[SchemaStatus](t, res); res.StatusCode != http.StatusOK || !st.Database || st.Source != SchemaSourceMigrate || len(st.Applied) != 1 || len(st.Pending) != 1 || !st.NeedsRestart {
		t.Errorf("schema-status = %d %+v", res.StatusCode, st)
	}
	fail = errors.New("connection refused")
	res = call(t, ts, http.MethodGet, APIPrefix+"db/schema-status", "", nil)
	if p := decode[problem](t, res); res.StatusCode != http.StatusServiceUnavailable || p.Code != "database_unavailable" || !strings.Contains(p.Detail, "connection refused") {
		t.Errorf("unreachable = %d %+v", res.StatusCode, p)
	}
}

// ddlDB runs scripts, committing in commit mode.
type ddlDB struct{ fakeDB }

func (d *ddlDB) Run(_ context.Context, req pgmeta.RunRequest) (pgmeta.RunResult, error) {
	res := pgmeta.RunResult{Mode: req.Mode, Statements: []pgmeta.StatementResult{{Command: "CREATE TABLE"}}}
	if req.Mode == pgmeta.ModeCommit {
		res.Committed = true
	} else {
		res.RolledBack = true
	}
	return res, nil
}

func (d *ddlDB) Explain(context.Context, string, bool) (json.RawMessage, error) {
	return json.RawMessage(`[]`), nil
}

func TestSQLRunPublishesSchemaAfterDDL(t *testing.T) {
	computed := 0
	s, ts, _ := newTestServer(t, func(c *Config) {
		c.Database = DatabaseConfig{
			Open: func(context.Context) (Database, error) { return &ddlDB{}, nil },
			SchemaStatus: func(context.Context) (SchemaStatus, error) {
				computed++
				return ComputeSchemaStatus(SchemaInput{Source: SchemaSourceStartup, Applied: []string{"10_init.sql"}}), nil
			},
		}
	})
	run := func(sql, mode string) {
		t.Helper()
		body, _ := json.Marshal(pgmeta.RunRequest{SQL: sql, Mode: mode})
		if res := call(t, ts, http.MethodPost, APIPrefix+"db/sql/run", string(body), nil); res.StatusCode != http.StatusOK {
			t.Fatalf("run %q = %d", sql, res.StatusCode)
		}
	}
	// A rolled-back DDL and a committed SELECT change nothing.
	run("CREATE TABLE t (id int)", pgmeta.ModeRollback)
	run("SELECT 1", pgmeta.ModeCommit)
	if _, ok := s.cfg.Hub.Schema(); ok || computed != 0 {
		t.Errorf("schema published without a committed DDL (computed %d)", computed)
	}
	run("SELECT 1;\nalter table t add column x int", pgmeta.ModeCommit)
	st, ok := s.cfg.Hub.Schema()
	if !ok || computed != 1 || st.Source != SchemaSourceSQL || len(st.Applied) != 0 || !st.Database {
		t.Errorf("after a committed DDL: schema = %+v, %v (computed %d)", st, ok, computed)
	}
}
