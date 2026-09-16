package pgmeta

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// testClient connects to GORBITAL_TEST_DATABASE_URL and creates a schema
// for the test, dropped when it ends. Without the variable the test is
// skipped, unless GORBITAL_REQUIRE_DB is set (CI), where it fails.
func testClient(t *testing.T) (*Client, string) {
	t.Helper()
	url := os.Getenv("GORBITAL_TEST_DATABASE_URL")
	if url == "" {
		if os.Getenv("GORBITAL_REQUIRE_DB") != "" {
			t.Fatal("GORBITAL_TEST_DATABASE_URL is unset")
		}
		t.Skip("set GORBITAL_TEST_DATABASE_URL to run database tests (docker compose up -d --wait)")
	}
	ctx := context.Background()
	c, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	schema := fmt.Sprintf("pgmeta_%d", time.Now().UnixNano()%1_000_000_000)
	exec(t, c.pool, "CREATE SCHEMA "+ident(schema))
	t.Cleanup(func() { exec(t, c.pool, "DROP SCHEMA "+ident(schema)+" CASCADE") })
	return c, schema
}

func exec(t *testing.T, pool *pgxpool.Pool, sql string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), sql); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

func str(s string) *string { return &s }

func setup(t *testing.T, c *Client, schema string) {
	t.Helper()
	s := ident(schema)
	exec(t, c.pool, "CREATE TYPE "+s+".status AS ENUM ('active', 'archived')")
	exec(t, c.pool, "CREATE TABLE "+s+`.owners (id text PRIMARY KEY, email text NOT NULL UNIQUE)`)
	exec(t, c.pool, "CREATE TABLE "+s+`.projects (
		id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
		owner_id text NOT NULL REFERENCES `+s+`.owners (id) ON DELETE CASCADE,
		name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
		status `+s+`.status NOT NULL DEFAULT 'active',
		tags text[] NOT NULL DEFAULT '{}',
		meta jsonb,
		created_at timestamptz NOT NULL DEFAULT now())`)
	exec(t, c.pool, "COMMENT ON TABLE "+s+".projects IS 'What people work on'")
	exec(t, c.pool, "COMMENT ON COLUMN "+s+".projects.name IS 'Shown in lists'")
	exec(t, c.pool, "CREATE INDEX projects_owner_idx ON "+s+".projects (owner_id, created_at)")
	exec(t, c.pool, "CREATE VIEW "+s+".active_projects AS SELECT id, name FROM "+s+".projects WHERE status = 'active'")
	exec(t, c.pool, "CREATE TABLE "+s+".river_job (id int)")
	exec(t, c.pool, "CREATE TABLE "+s+".auth_users (id text PRIMARY KEY)")
	exec(t, c.pool, "CREATE TABLE "+s+".stuff (a int NOT NULL, b int NOT NULL)")
	exec(t, c.pool, "CREATE TYPE "+s+".unused AS ENUM ('a', 'b')")
	exec(t, c.pool, "CREATE FUNCTION "+s+".touch() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$")
	exec(t, c.pool, "INSERT INTO "+s+".owners VALUES ('usr_1', 'a@example.com'), ('usr_2', 'b@example.com')")
	exec(t, c.pool, "INSERT INTO "+s+".projects (owner_id, name, tags, meta) VALUES ('usr_1', 'Website', '{web,design}', '{\"a\": 1}'), ('usr_1', 'Mobile app', '{}', NULL), ('usr_2', 'Legacy', '{}', NULL)")
	exec(t, c.pool, "UPDATE "+s+".projects SET status = 'archived' WHERE name = 'Legacy'")
	exec(t, c.pool, "ANALYZE "+s+".projects")
}

func TestCatalog(t *testing.T) {
	c, schema := testClient(t)
	setup(t, c, schema)
	ctx := context.Background()

	if v, err := c.ServerVersion(ctx); err != nil || v < 160000 {
		t.Fatalf("ServerVersion() = %d, %v", v, err)
	}
	schemas, err := c.Schemas(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var found, system bool
	for _, s := range schemas {
		if s.Name == schema && !s.System {
			found = true
		}
		if s.Name == "pg_catalog" && s.System {
			system = true
		}
	}
	if !found || !system {
		t.Errorf("Schemas() = %+v", schemas)
	}

	tables, err := c.Tables(ctx, []string{schema})
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]Table{}
	for _, tb := range tables {
		byName[tb.Name] = tb
	}
	if p := byName["projects"]; p.Kind != "table" || p.RowEstimate != 3 || p.Bytes == 0 || p.Comment == nil || *p.Comment != "What people work on" || p.Ownership != OwnershipUser {
		t.Errorf("projects = %+v", p)
	}
	if v := byName["active_projects"]; v.Kind != "view" {
		t.Errorf("view = %+v", v)
	}
	if byName["river_job"].Ownership != OwnershipSystem || byName["auth_users"].Ownership != OwnershipManaged {
		t.Errorf("ownership: river_job %s, auth_users %s", byName["river_job"].Ownership, byName["auth_users"].Ownership)
	}

	detail, err := c.Detail(ctx, schema, "projects")
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Columns) != 7 || strings.Join(detail.PrimaryKey, ",") != "id" {
		t.Fatalf("detail = %+v", detail)
	}
	cols := map[string]Column{}
	for _, col := range detail.Columns {
		cols[col.Name] = col
	}
	if id := cols["id"]; id.DataType != "bigint" || id.Identity != "a" || !id.IsPrimaryKey || id.IsNullable {
		t.Errorf("id = %+v", id)
	}
	if st := cols["status"]; st.DataType != schema+".status" && st.TypeName != "status" || strings.Join(st.EnumValues, ",") != "active,archived" || st.DefaultExpr == nil {
		t.Errorf("status = %+v", st)
	}
	if tags := cols["tags"]; !tags.IsArray || tags.DataType != "text[]" {
		t.Errorf("tags = %+v", tags)
	}
	if name := cols["name"]; name.Comment == nil || *name.Comment != "Shown in lists" || name.IsNullable {
		t.Errorf("name = %+v", name)
	}
	if owner := cols["owner_id"]; len(owner.FKTargets) != 1 || owner.FKTargets[0] != schema+".owners" {
		t.Errorf("owner_id = %+v", owner)
	}
	if meta := cols["meta"]; !meta.IsNullable || meta.DataType != "jsonb" {
		t.Errorf("meta = %+v", meta)
	}
	types := map[string]Constraint{}
	for _, con := range detail.Constraints {
		types[con.Type] = con
	}
	if fk := types["f"]; fk.RefTable == nil || *fk.RefTable != "owners" || fk.OnDelete == nil || *fk.OnDelete != "CASCADE" || strings.Join(fk.Columns, ",") != "owner_id" || strings.Join(fk.RefColumns, ",") != "id" {
		t.Errorf("fk = %+v", fk)
	}
	if ck := types["c"]; !strings.Contains(ck.Definition, "char_length") {
		t.Errorf("check = %+v", ck)
	}
	if len(detail.Indexes) != 2 || !detail.Indexes[0].IsPrimary || detail.Indexes[1].Name != "projects_owner_idx" || strings.Join(detail.Indexes[1].Columns, ",") != "owner_id,created_at" {
		t.Errorf("indexes = %+v", detail.Indexes)
	}

	enums, err := c.Enums(ctx, []string{schema})
	if err != nil || len(enums) != 2 || enums[0].Name != "status" || strings.Join(enums[0].Values, ",") != "active,archived" {
		t.Errorf("Enums() = %+v, %v", enums, err)
	}
	fks, err := c.ForeignKeys(ctx, []string{schema})
	if err != nil || len(fks) != 1 || fks[0].Table != "projects" || fks[0].RefTable != "owners" {
		t.Errorf("ForeignKeys() = %+v, %v", fks, err)
	}
	views, err := c.Views(ctx, []string{schema})
	if err != nil || len(views) != 1 || !strings.Contains(views[0].Definition, "status") {
		t.Errorf("Views() = %+v, %v", views, err)
	}
	if ext, err := c.Extensions(ctx); err != nil || len(ext) == 0 {
		t.Errorf("Extensions() = %d, %v", len(ext), err)
	}
	if fns, err := c.Functions(ctx, []string{schema}); err != nil || len(fns) != 1 || fns[0].Name != "touch" || fns[0].Language != "plpgsql" || fns[0].Definition == nil {
		t.Errorf("Functions() = %+v, %v", fns, err)
	}
	if opts, err := c.Types(ctx, []string{schema}); err != nil || len(opts) != len(pickerTypes)+2 || opts[len(opts)-2].Name != schema+".status" {
		t.Errorf("Types() = %d, %v", len(opts), err)
	}
	if _, err := c.Detail(ctx, schema, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Detail(nope) = %v", err)
	}
}

func TestRows(t *testing.T) {
	c, schema := testClient(t)
	setup(t, c, schema)
	ctx := context.Background()

	page, err := c.Query(ctx, RowQuery{Schema: schema, Table: "projects", Sorts: []Sort{{Column: "name"}}})
	if err != nil {
		t.Fatal(err)
	}
	if page.Count != 3 || page.Estimated || len(page.Rows) != 3 || page.Limit != DefaultLimit || strings.Join(page.PrimaryKey, ",") != "id" {
		t.Fatalf("page = %+v", page)
	}
	nameAt := 2 // id, owner_id, name, ...
	if *page.Rows[0][nameAt] != "Legacy" || *page.Rows[2][nameAt] != "Website" {
		t.Errorf("sorted names = %v %v", *page.Rows[0][nameAt], *page.Rows[2][nameAt])
	}
	if tags := *page.Rows[2][4]; tags != "{web,design}" {
		t.Errorf("tags cell = %q", tags)
	}
	if page.Rows[0][5] != nil || *page.Rows[2][5] != `{"a": 1}` {
		t.Errorf("meta cells = %v %v", page.Rows[0][5], page.Rows[2][5])
	}

	// Filters with every operator, values as text the server casts.
	for _, tt := range []struct {
		filter Filter
		want   int64
	}{
		{Filter{Column: "status", Operator: "=", Value: "active"}, 2},
		{Filter{Column: "name", Operator: "~~*", Value: "%app%"}, 1},
		{Filter{Column: "id", Operator: ">", Value: "1"}, 2},
		{Filter{Column: "name", Operator: "in", Values: []string{"Legacy", "Website"}}, 2},
		{Filter{Column: "meta", Operator: "is", Value: "null"}, 2},
		{Filter{Column: "meta", Operator: "is", Value: "not null"}, 1},
		{Filter{Column: "name", Operator: "in"}, 0},
	} {
		page, err := c.Query(ctx, RowQuery{Schema: schema, Table: "projects", Filters: []Filter{tt.filter}})
		if err != nil || page.Count != tt.want || int64(len(page.Rows)) != tt.want {
			t.Errorf("filter %+v: count %d rows %d, %v; want %d", tt.filter, page.Count, len(page.Rows), err, tt.want)
		}
	}
	if _, err := c.Query(ctx, RowQuery{Schema: schema, Table: "projects", Filters: []Filter{{Column: "nope", Operator: "="}}}); !errors.Is(err, ErrUnknownColumn) {
		t.Errorf("unknown column = %v", err)
	}
	if _, err := c.Query(ctx, RowQuery{Schema: schema, Table: "projects", Filters: []Filter{{Column: "name", Operator: "; DROP", Value: "x"}}}); !errors.Is(err, ErrUnknownOperator) {
		t.Errorf("bad operator = %v", err)
	}
	if _, err := c.Query(ctx, RowQuery{Schema: schema, Table: "projects", Filters: []Filter{{Column: "id", Operator: "=", Value: "not a number"}}}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("bad value = %v", err)
	}
	if page, err := c.Query(ctx, RowQuery{Schema: schema, Table: "projects", Limit: 2, Offset: 2}); err != nil || len(page.Rows) != 1 || page.Count != 3 {
		t.Errorf("paging = %d rows, count %d, %v", len(page.Rows), page.Count, err)
	}

	// Insert, update, delete by primary key.
	row, err := c.Insert(ctx, RowEdit{Schema: schema, Table: "projects", Values: map[string]Cell{"owner_id": str("usr_2"), "name": str("New"), "tags": str(`{"x y",z}`)}})
	if err != nil || *row[2] != "New" || *row[4] != `{"x y",z}` || *row[3] != "active" {
		t.Fatalf("Insert() = %v, %v", cells(row), err)
	}
	id := *row[0]
	row, err = c.Update(ctx, RowEdit{Schema: schema, Table: "projects", Keys: []map[string]Cell{{"id": str(id)}}, Values: map[string]Cell{"name": str("Renamed"), "meta": nil, "status": str("archived")}})
	if err != nil || *row[2] != "Renamed" || row[5] != nil || *row[3] != "archived" {
		t.Fatalf("Update() = %v, %v", cells(row), err)
	}
	if _, err := c.Update(ctx, RowEdit{Schema: schema, Table: "projects", Keys: []map[string]Cell{{"id": str("999999")}}, Values: map[string]Cell{"name": str("x")}}); !errors.Is(err, ErrRowCount) {
		t.Errorf("update of a missing row = %v", err)
	}
	if _, err := c.Update(ctx, RowEdit{Schema: schema, Table: "projects", Keys: []map[string]Cell{{"id": str(id)}}, Values: map[string]Cell{"name": str("")}}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("update violating a check = %v", err)
	}
	if _, err := c.Insert(ctx, RowEdit{Schema: schema, Table: "projects", Values: map[string]Cell{"owner_id": str("usr_2"), "name": str("x"), "colour": str("red")}}); !errors.Is(err, ErrUnknownColumn) {
		t.Errorf("insert with an unknown column = %v", err)
	}
	n, err := c.Delete(ctx, RowEdit{Schema: schema, Table: "projects", Keys: []map[string]Cell{{"id": str(id)}}})
	if err != nil || n != 1 {
		t.Errorf("Delete() = %d, %v", n, err)
	}
	if _, err := c.Delete(ctx, RowEdit{Schema: schema, Table: "projects", Keys: []map[string]Cell{{"id": str(id)}, {"id": str("1")}}}); !errors.Is(err, ErrRowCount) {
		t.Errorf("delete with a missing key = %v", err)
	}
	if page, _ := c.Query(ctx, RowQuery{Schema: schema, Table: "projects"}); page.Count != 3 {
		t.Errorf("a refused delete removed rows: %d left", page.Count)
	}

	// Imports are all or nothing.
	n, err = c.InsertMany(ctx, schema, "projects", []map[string]Cell{
		{"owner_id": str("usr_1"), "name": str("Import 1")},
		{"owner_id": str("usr_1"), "name": str("Import 2"), "meta": nil},
	})
	if err != nil || n != 2 {
		t.Errorf("InsertMany() = %d, %v", n, err)
	}
	if _, err := c.InsertMany(ctx, schema, "projects", []map[string]Cell{{"owner_id": str("usr_1"), "name": str("ok")}, {"owner_id": str("nobody"), "name": str("bad fk")}}); !errors.Is(err, ErrInvalidInput) || !strings.Contains(err.Error(), "row 2") {
		t.Errorf("InsertMany() with a bad row = %v", err)
	}
	if page, _ := c.Query(ctx, RowQuery{Schema: schema, Table: "projects"}); page.Count != 5 {
		t.Errorf("a refused import added rows: %d", page.Count)
	}

	// Tables without a primary key are read-only; system tables too.
	exec(t, c.pool, "CREATE TABLE "+ident(schema)+".nokey (a int)")
	if _, err := c.Delete(ctx, RowEdit{Schema: schema, Table: "nokey", Keys: []map[string]Cell{{"a": str("1")}}}); !errors.Is(err, ErrNoPrimaryKey) {
		t.Errorf("delete without a key = %v", err)
	}
	if _, err := c.Insert(ctx, RowEdit{Schema: schema, Table: "river_job", Values: map[string]Cell{"id": str("1")}}); !errors.Is(err, ErrSystemTable) {
		t.Errorf("insert into a system table = %v", err)
	}
}

func cells(row []Cell) []string {
	out := make([]string, len(row))
	for i, c := range row {
		if c == nil {
			out[i] = "NULL"
		} else {
			out[i] = *c
		}
	}
	return out
}

func TestPlanAgainstTheDatabase(t *testing.T) {
	c, schema := testClient(t)
	setup(t, c, schema)
	ctx := context.Background()

	p, err := c.Plan(ctx, Change{Kind: "add_column", Schema: schema, Table: "projects", Column: &ColumnSpec{Name: "due_on", Type: "date", Nullable: new(bool), Default: str("CURRENT_DATE"), DefaultIsExpr: true, Comment: "Deadline"}})
	if err != nil {
		t.Fatal(err)
	}
	wantUp := "ALTER TABLE " + ident(schema, "projects") + ` ADD COLUMN "due_on" date NOT NULL DEFAULT CURRENT_DATE;`
	if len(p.Up) != 2 || p.Up[0] != wantUp || !strings.HasPrefix(p.Up[1], "COMMENT ON COLUMN") || len(p.Down) != 1 || !strings.HasSuffix(p.Down[0], `DROP COLUMN "due_on";`) {
		t.Errorf("add_column plan = %+v", p)
	}
	// The plan runs: apply Up, then Down, and the table is as before.
	for _, sql := range p.Up {
		exec(t, c.pool, sql)
	}
	if detail, _ := c.Detail(ctx, schema, "projects"); len(detail.Columns) != 8 {
		t.Errorf("after Up: %d columns", len(detail.Columns))
	}
	for _, sql := range p.Down {
		exec(t, c.pool, sql)
	}
	if detail, _ := c.Detail(ctx, schema, "projects"); len(detail.Columns) != 7 {
		t.Errorf("after Down: %d columns", len(detail.Columns))
	}

	// Every kind renders SQL the server accepts, Up then Down.
	changes := []Change{
		{Kind: "create_table", Schema: schema, Table: "tasks", Columns: []ColumnSpec{
			{Name: "id", Type: "int8", Identity: "always", PrimaryKey: true},
			{Name: "project_id", Type: "int8", Nullable: new(bool), References: &FKSpec{RefSchema: schema, RefTable: "projects", RefColumns: []string{"id"}, OnDelete: "cascade"}},
			{Name: "title", Type: "varchar(120)", Nullable: new(bool), Default: str(""), Check: "char_length(title) > 0"},
			{Name: "status", Type: schema + ".status", Nullable: new(bool), Default: str("active")},
			{Name: "labels", Type: "text", Array: true},
		}, Uniques: [][]string{{"project_id", "title"}}},
		{Kind: "rename_table", Schema: schema, Table: "owners", NewName: "people"},
		{Kind: "rename_column", Schema: schema, Table: "projects", Column: &ColumnSpec{Name: "name"}, NewName: "title"},
		{Kind: "alter_column", Schema: schema, Table: "projects", Column: &ColumnSpec{Name: "tags", Nullable: ptr(true), Type: "varchar(50)", Array: true, Default: str("{}"), Comment: "Free-form"}},
		{Kind: "add_unique", Schema: schema, Table: "projects", Unique: []string{"owner_id", "name"}},
		{Kind: "add_check", Schema: schema, Table: "projects", Check: "char_length(name) < 200", Name: "projects_name_short"},
		{Kind: "drop_constraint", Schema: schema, Table: "projects", ConstraintName: "projects_owner_id_fkey"},
		{Kind: "create_index", Schema: schema, Table: "projects", Index: &IndexSpec{Columns: []string{"status", "(lower(name))"}, Where: "status = 'active'"}},
		{Kind: "drop_index", Schema: schema, Table: "projects", IndexName: "projects_owner_idx"},
		{Kind: "create_enum", Schema: schema, Name: "priority", Values: []string{"low", "high"}},
		{Kind: "comment", Schema: schema, Table: "projects", Comment: str("Changed")},
		{Kind: "comment", Schema: schema, Table: "projects", Column: &ColumnSpec{Name: "name"}, Comment: str("")},
		{Kind: "rls", Schema: schema, Table: "projects", Enabled: ptr(true)},
		{Kind: "create_extension", Schema: schema, Name: "pgcrypto"},
		{Kind: "create_function", Schema: schema, Name: "add_one", Signature: "add_one(integer)", Definition: "CREATE FUNCTION " + ident(schema) + ".add_one(n integer) RETURNS integer LANGUAGE sql IMMUTABLE AS $$ SELECT n + 1 $$"},
		{Kind: "create_trigger", Schema: schema, Table: "projects", Name: "projects_touch", Definition: "CREATE TRIGGER projects_touch BEFORE UPDATE ON " + ident(schema, "projects") + " FOR EACH ROW EXECUTE FUNCTION " + ident(schema) + ".touch()"},
		{Kind: "create_view", Schema: schema, Name: "archived_projects", Definition: "SELECT id, name FROM " + ident(schema, "projects") + " WHERE status = 'archived'"},
		{Kind: "create_view", Schema: schema, Name: "project_counts", Materialized: true, Definition: "SELECT owner_id, count(*) AS n FROM " + ident(schema, "projects") + " GROUP BY owner_id"},
		{Kind: "drop_view", Schema: schema, Name: "active_projects", Definition: "SELECT id, name FROM " + ident(schema, "projects") + " WHERE status = 'active'"},
		{Kind: "drop_enum", Schema: schema, Name: "unused"},
		{Kind: "set_primary_key", Schema: schema, Table: "stuff", PrimaryKey: []string{"a", "b"}},
		{Kind: "set_primary_key", Schema: schema, Table: "projects", PrimaryKey: []string{"id"}},
	}
	for _, ch := range changes {
		p, err := c.Plan(ctx, ch)
		if err != nil {
			t.Errorf("%s: %v", ch.Kind, err)
			continue
		}
		for _, sql := range p.Up {
			if _, err := c.pool.Exec(ctx, sql); err != nil {
				t.Errorf("%s Up %q: %v", ch.Kind, sql, err)
			}
		}
		for _, sql := range p.Down {
			if _, err := c.pool.Exec(ctx, sql); err != nil {
				t.Errorf("%s Down %q: %v", ch.Kind, sql, err)
			}
		}
	}
	// Irreversible plans say so and still run.
	p, err = c.Plan(ctx, Change{Kind: "drop_column", Schema: schema, Table: "projects", Column: &ColumnSpec{Name: "tags"}})
	if err != nil || !p.Irreversible || len(p.Notes) == 0 || !strings.HasPrefix(p.Down[0], "-- ") {
		t.Errorf("drop_column plan = %+v, %v", p, err)
	}
	// System and managed tables refuse schema changes; unknown things are
	// refused before any SQL.
	if _, err := c.Plan(ctx, Change{Kind: "add_column", Schema: schema, Table: "auth_users", Column: &ColumnSpec{Name: "x", Type: "text"}}); !errors.Is(err, ErrSystemTable) {
		t.Errorf("managed table = %v", err)
	}
	if _, err := c.Plan(ctx, Change{Kind: "add_column", Schema: schema, Table: "projects", Column: &ColumnSpec{Name: "x", Type: "money"}}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("unknown type = %v", err)
	}
	if _, err := c.Plan(ctx, Change{Kind: "add_column", Schema: schema, Table: "projects", Column: &ColumnSpec{Name: "x; drop", Type: "text"}}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("bad name = %v", err)
	}
	if _, err := c.Plan(ctx, Change{Kind: "drop_index", Schema: schema, Table: "projects", IndexName: "projects_pkey"}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("drop the primary key's index = %v", err)
	}
}

func ptr[T any](v T) *T { return &v }

func TestRenderAndHelpers(t *testing.T) {
	p := Plan{Summary: "Add phone to users", Up: []string{`ALTER TABLE "public"."users" ADD COLUMN "phone" text;`}, Down: []string{`ALTER TABLE "public"."users" DROP COLUMN "phone";`}}
	got := string(Render(p))
	for _, want := range []string{"-- Add phone to users.\n", "-- +goose Up\nALTER TABLE", "-- +goose Down\nALTER TABLE \"public\".\"users\" DROP COLUMN"} {
		if !strings.Contains(got, want) {
			t.Errorf("Render() lacks %q:\n%s", want, got)
		}
	}
	irreversible := string(Render(Plan{Summary: "Drop x", Up: []string{"DROP TABLE x;"}, Irreversible: true, Notes: []string{"data is lost"}, NoTransaction: true}))
	if !strings.Contains(irreversible, "-- +goose NO TRANSACTION\n-- +goose Up") || !strings.Contains(irreversible, "-- data is lost\n-- irreversible") {
		t.Errorf("Render(irreversible) = %s", irreversible)
	}
	if literal("it's") != "'it''s'" || literal(`a\b`) != `E'a\\b'` || ident("a\"b") != `"a""b"` {
		t.Error("literal or ident")
	}
	if _, err := typeSQL(ColumnSpec{Type: "numeric(10,2)"}, nil); err != nil {
		t.Error(err)
	}
	if s, _ := typeSQL(ColumnSpec{Type: "varchar(100)", Array: true}, nil); s != "character varying(100)[]" {
		t.Errorf("typeSQL = %q", s)
	}
	if _, err := plan(Change{Kind: "sing"}, snapshot{}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("unknown kind = %v", err)
	}
}

func TestMigrationsList(t *testing.T) {
	c, _ := testClient(t)
	ctx := context.Background()
	dir := t.TempDir()
	// A table of goose's shape, in a schema of this test, isn't what the
	// client reads (it reads the search path's goose_db_version), so the list
	// covers the file side and the no-table case.
	if err := os.MkdirAll(filepath.Join(dir, "db", "migrations"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "db", "migrations", "20260101000001_init.sql"), []byte("-- +goose Up\nCREATE TABLE t (id int);\n-- +goose Down\nDROP TABLE t;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "db", "migrations", "20260101000002_more.sql"), []byte("-- +goose Up\nSELECT 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "db", "migrations", "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	list, err := c.Migrations(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) < 2 || list[0].Name != "init" || !list[0].HasDown || list[1].HasDown || list[0].Path != "db/migrations/20260101000001_init.sql" {
		t.Errorf("Migrations() = %+v", list)
	}
	for _, m := range list {
		if m.Path != "" && m.Applied {
			t.Errorf("file %s reported as applied in a fresh database", m.Path)
		}
	}
	if list, err := c.Migrations(ctx, t.TempDir()); err != nil || len(list) != 0 && list[0].Path != "" {
		t.Errorf("Migrations() without files = %+v, %v", list, err)
	}
}
