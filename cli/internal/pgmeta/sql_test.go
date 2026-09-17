package pgmeta

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRunModes(t *testing.T) {
	c, schema := testClient(t)
	setup(t, c, schema)
	ctx := context.Background()
	table := ident(schema, "projects")

	// Rollback mode sees its own writes and leaves nothing behind.
	res, err := c.Run(ctx, RunRequest{SQL: "UPDATE " + table + " SET name = 'x';\nSELECT count(*) AS n FROM " + table + " WHERE name = 'x';"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Mode != ModeRollback || !res.RolledBack || res.Committed || res.Error != nil || len(res.Statements) != 2 {
		t.Fatalf("rollback run = %+v", res)
	}
	if s := res.Statements[0]; s.Command != "UPDATE 3" || s.RowsAffected != 3 || s.Columns != nil {
		t.Errorf("update result = %+v", s)
	}
	if s := res.Statements[1]; strings.Join(s.Columns, ",") != "n" || len(s.Rows) != 1 || *s.Rows[0][0] != "3" {
		t.Errorf("select result = %+v", s)
	}
	if len(res.Warnings) != 1 || res.Warnings[0].Kind != "update_without_where" {
		t.Errorf("warnings = %+v", res.Warnings)
	}
	if page, _ := c.Query(ctx, RowQuery{Schema: schema, Table: "projects", Filters: []Filter{{Column: "name", Operator: "=", Value: "x"}}}); page.Count != 0 {
		t.Errorf("rollback mode wrote %d rows", page.Count)
	}

	// Commit mode keeps them.
	res, err = c.Run(ctx, RunRequest{SQL: "UPDATE " + table + " SET name = 'kept' WHERE name = 'Legacy'", Mode: ModeCommit})
	if err != nil || !res.Committed || res.RolledBack {
		t.Fatalf("commit run = %+v, %v", res, err)
	}
	if page, _ := c.Query(ctx, RowQuery{Schema: schema, Table: "projects", Filters: []Filter{{Column: "name", Operator: "=", Value: "kept"}}}); page.Count != 1 {
		t.Errorf("commit mode kept %d rows", page.Count)
	}

	// Read-only mode refuses writes, with the error located.
	res, err = c.Run(ctx, RunRequest{SQL: "SELECT 1;\n\nDELETE FROM " + table + ";", Mode: ModeReadOnly})
	if err != nil || res.Error == nil || res.Error.Code != "25006" || !res.RolledBack || len(res.Statements) != 1 {
		t.Fatalf("readonly run = %+v, %v", res, err)
	}

	// A syntax error names the line; the server parses the whole script
	// first, so nothing ran. A runtime error keeps the earlier results.
	res, err = c.Run(ctx, RunRequest{SQL: "SELECT 1 AS a;\nSELEC 2;"})
	if err != nil || res.Error == nil || res.Error.Line != 2 || res.Error.Position != 16 || res.Error.Code != "42601" || len(res.Statements) != 0 {
		t.Fatalf("syntax error run = %+v %+v, %v", res, res.Error, err)
	}
	res, err = c.Run(ctx, RunRequest{SQL: "SELECT 1 AS a;\nSELECT 1/0;"})
	if err != nil || res.Error == nil || res.Error.Code != "22012" || len(res.Statements) != 1 || !res.RolledBack {
		t.Fatalf("runtime error run = %+v %+v, %v", res, res.Error, err)
	}

	// Rows beyond the limit are dropped and reported; NULL is nil.
	res, err = c.Run(ctx, RunRequest{SQL: "SELECT g, NULL::text AS n FROM generate_series(1, 10) g", RowLimit: 3})
	if err != nil || len(res.Statements) != 1 || len(res.Statements[0].Rows) != 3 || !res.Statements[0].Truncated || res.Statements[0].Rows[0][1] != nil {
		t.Fatalf("limited run = %+v, %v", res, err)
	}

	// Transaction control is refused outside commit mode; empty scripts too.
	if _, err := c.Run(ctx, RunRequest{SQL: "BEGIN; SELECT 1; COMMIT;"}); !errors.Is(err, ErrScriptControlsTransaction) {
		t.Errorf("BEGIN in rollback mode = %v", err)
	}
	if _, err := c.Run(ctx, RunRequest{SQL: "  \n"}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("empty script = %v", err)
	}
	if _, err := c.Run(ctx, RunRequest{SQL: "SELECT 1", Mode: "maybe"}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("bad mode = %v", err)
	}

	// A timeout comes back as an error, not a hang.
	res, err = c.Run(ctx, RunRequest{SQL: "SELECT pg_sleep(3)", TimeoutSeconds: 1})
	if err != nil || res.Error == nil || res.Error.Code != "57014" || !strings.Contains(res.Error.Message, "too long") {
		t.Fatalf("timed out run = %+v, %v", res, err)
	}

	// Every template runs (those needing an extension only when it is there).
	installed := map[string]bool{}
	exts, _ := c.Extensions(ctx)
	for _, e := range exts {
		installed[e.Name] = e.Installed
	}
	for _, tpl := range Templates() {
		if tpl.Needs != "" && !installed[tpl.Needs] {
			continue
		}
		res, err := c.Run(ctx, RunRequest{SQL: tpl.SQL, Mode: ModeReadOnly})
		if err != nil || (res.Error != nil && !strings.Contains(res.Error.Message, "does not exist")) {
			t.Errorf("template %s: %+v, %v", tpl.Name, res.Error, err)
		}
	}
}

func TestExplain(t *testing.T) {
	c, schema := testClient(t)
	setup(t, c, schema)
	ctx := context.Background()
	plan, err := c.Explain(ctx, "SELECT * FROM "+ident(schema, "projects")+" WHERE id = 1;", false)
	if err != nil || !strings.Contains(string(plan), `"Plan"`) || strings.Contains(string(plan), "Actual Rows") {
		t.Errorf("Explain() = %s, %v", plan, err)
	}
	plan, err = c.Explain(ctx, "UPDATE "+ident(schema, "projects")+" SET name = 'x'", true)
	if err != nil || !strings.Contains(string(plan), "Actual Rows") {
		t.Errorf("Explain(analyze) = %s, %v", plan, err)
	}
	if page, _ := c.Query(ctx, RowQuery{Schema: schema, Table: "projects", Filters: []Filter{{Column: "name", Operator: "=", Value: "x"}}}); page.Count != 0 {
		t.Errorf("EXPLAIN ANALYZE wrote %d rows", page.Count)
	}
	if _, err := c.Explain(ctx, "SELEC 1", false); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("bad statement = %v", err)
	}
	if _, err := c.Explain(ctx, "COMMIT", false); !errors.Is(err, ErrScriptControlsTransaction) {
		t.Errorf("transaction control = %v", err)
	}
}

func TestCheck(t *testing.T) {
	script := `-- a comment with DROP TABLE inside
SELECT 'DELETE FROM users' AS s;
DELETE FROM users;
DELETE FROM users WHERE id = 1;
UPDATE users SET a = 1;

UPDATE users SET a = 1 WHERE id = 2;
DROP TABLE users;
TRUNCATE users;
ALTER TABLE users DROP COLUMN phone;
ALTER TABLE users ALTER COLUMN age TYPE bigint;
alter table users add column x int;`
	got := Check(script)
	want := []Warning{
		{Kind: "delete_without_where", Line: 3},
		{Kind: "update_without_where", Line: 5},
		{Kind: "drop", Line: 8},
		{Kind: "truncate", Line: 9},
		{Kind: "drop_column", Line: 10},
		{Kind: "alter_type", Line: 11},
	}
	if len(got) != len(want) {
		t.Fatalf("Check() = %+v, want %d warnings", got, len(want))
	}
	for i, w := range want {
		if got[i].Kind != w.Kind || got[i].Line != w.Line || got[i].Message == "" {
			t.Errorf("warning %d = %+v, want %+v", i, got[i], w)
		}
	}
	if warnings := Check("SELECT 1; INSERT INTO t VALUES (1)"); len(warnings) != 0 {
		t.Errorf("harmless script = %+v", warnings)
	}
}
