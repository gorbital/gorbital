package cli

import (
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// schemaTool is a small program the end-to-end test writes into apps it
// creates: it creates databases, runs the app's goose migrations, runs SQL
// and prints the schema in an order-independent form.
const schemaTool = `package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"gorbital.dev/config"
	"gorbital.dev/modules/postgres"
	"github.com/jackc/pgx/v5"

	"MODULE/db/migrations"
)

func main() {
	if err := run(context.Background(), os.Args[1], os.Args[2], os.Args[3:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cmd, url string, args []string) error {
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	switch cmd {
	case "createdb":
		_, err = conn.Exec(ctx, "DROP DATABASE IF EXISTS "+pgx.Identifier{args[0]}.Sanitize()+" WITH (FORCE)")
		if err == nil {
			_, err = conn.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{args[0]}.Sanitize())
		}
		return err
	case "migrate":
		pool, err := postgres.Open(ctx, config.NewSecret(url))
		if err != nil {
			return err
		}
		defer pool.Close()
		_, err = postgres.Migrate(ctx, pool, migrations.FS)
		return err
	case "exec":
		_, err = conn.Exec(ctx, args[0])
		return err
	case "query":
		var out string
		if err := conn.QueryRow(ctx, args[0]).Scan(&out); err != nil {
			return err
		}
		fmt.Println(out)
		return nil
	case "schema":
		var lines []string
		for _, q := range []string{
			"SELECT 'column ' || table_name || '.' || column_name || ' ' || data_type || ' null=' || is_nullable || ' default=' || coalesce(column_default, '') FROM information_schema.columns WHERE table_schema = 'public'",
			"SELECT 'constraint ' || conrelid::regclass || ' ' || conname || ' ' || pg_get_constraintdef(oid) FROM pg_constraint WHERE connamespace = 'public'::regnamespace",
			"SELECT 'index ' || indexname || ' ' || indexdef FROM pg_indexes WHERE schemaname = 'public'",
		} {
			rows, err := conn.Query(ctx, q)
			if err != nil {
				return err
			}
			got, err := pgx.CollectRows(rows, pgx.RowTo[string])
			if err != nil {
				return err
			}
			lines = append(lines, got...)
		}
		sort.Strings(lines)
		fmt.Println(strings.Join(lines, "\n"))
		return nil
	}
	return fmt.Errorf("unknown command %s", cmd)
}
`

// TestAddOrgsConvertsADatabase proves the conversion on real data: a
// single-tenant app's database with accounts and projects, after orb add
// orgs and its migrations, has the schema of a new multi-tenant app (column
// order aside), every project in its owner's personal workspace, IDs the
// app accepts, and the converted app passes its own test suite. Set ORB_E2E=1
// and GORBITAL_TEST_DATABASE_URL to run it.
func TestAddOrgsConvertsADatabase(t *testing.T) {
	adminURL := os.Getenv("GORBITAL_TEST_DATABASE_URL")
	if os.Getenv("ORB_E2E") == "" || adminURL == "" {
		t.Skip("set ORB_E2E=1 and GORBITAL_TEST_DATABASE_URL to run the end-to-end test")
	}
	repo, err := filepath.Abs(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	isolateGit(t)
	work := t.TempDir()

	// v0.1-layout apps: in the v0.2 layout the organisations module's
	// migrations are older than a single-tenant database's (ADR-0083, Phase 9
	// known gaps), so orb add orgs converts new databases only there.
	newApp := func(name string, args ...string) string {
		t.Helper()
		t.Chdir(work)
		tenancy := "single"
		if len(args) == 2 {
			tenancy = args[1]
		}
		dir := writeV01App(t, work, name, tenancy, repo)
		writeFile(t, filepath.Join(dir, "internal", "schematool", "main.go"), strings.Replace(schemaTool, "MODULE", name, 1))
		return dir
	}
	tool := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("go", append([]string{"run", "./internal/schematool"}, args...)...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("schematool %s: %v\n%s", args[0], err, out)
		}
		return strings.TrimSpace(string(out))
	}
	dbURL := func(name string) string {
		u, err := url.Parse(adminURL)
		if err != nil {
			t.Fatal(err)
		}
		u.Path = "/" + name
		return u.String()
	}

	// A new multi-tenant app's schema is the target.
	multi := newApp("multi-api", "--tenancy", "multi")
	tool(multi, "createdb", adminURL, "orb_e2e_multi")
	tool(multi, "migrate", dbURL("orb_e2e_multi"))
	want := tool(multi, "schema", dbURL("orb_e2e_multi"))

	// A single-tenant app with data: two accounts with projects, one of them
	// deleted, and one without.
	single := newApp("shop-api")
	singleDB := dbURL("orb_e2e_single")
	tool(single, "createdb", adminURL, "orb_e2e_single")
	tool(single, "migrate", singleDB)
	tool(single, "exec", singleDB, `
		INSERT INTO auth_users (id, email, email_normalized, created_at, updated_at, deleted_at) VALUES
			('usr_ann', 'ann@example.com', 'ann@example.com', now(), now(), NULL),
			('usr_bob', 'bob@example.com', 'bob@example.com', now(), now(), now()),
			('usr_cat', 'cat@example.com', 'cat@example.com', now(), now(), NULL);
		INSERT INTO projects (id, owner_id, name, created_at, updated_at) VALUES
			('prj_1', 'usr_ann', 'Apollo', now(), now()),
			('prj_2', 'usr_ann', 'Gemini', now(), now()),
			('prj_3', 'usr_bob', 'Apollo', now(), now());`)

	t.Chdir(single)
	commitAll(t, "Create app")
	if code, out, errOut := runOrb(t, "add", "orgs"); code != 0 {
		t.Fatalf("orb add orgs = %d\n%s\n%s", code, out, errOut)
	}
	if git(t, "log", "-1", "--format=%s") != "Add organisations" {
		t.Error("orb add orgs didn't commit")
	}

	tool(single, "migrate", singleDB)
	if got := tool(single, "schema", singleDB); got != want {
		t.Errorf("converted schema differs from a new multi-tenant app's:\n%s", lineDiff(want, got))
	}
	for query, wantValue := range map[string]string{
		"SELECT count(*)::text FROM orgs WHERE personal": "3",
		"SELECT count(*)::text FROM org_members m JOIN orgs o ON o.id = m.org_id WHERE o.personal AND m.user_id = o.created_by AND m.role = 'owner'": "3",
		"SELECT count(*)::text FROM projects p JOIN orgs o ON o.id = p.org_id WHERE o.personal AND o.created_by = p.created_by":                      "3",
		"SELECT (deleted_at IS NOT NULL AND purge_after > deleted_at)::text FROM orgs WHERE created_by = 'usr_bob'":                                  "true",
		"SELECT count(*)::text FROM orgs WHERE deleted_at IS NOT NULL":                                                                               "1",
		"SELECT count(DISTINCT id)::text FROM orgs WHERE id ~ '^org_[a-z2-7]{26}$' AND right(id, 1) ~ '[aeimquy4]'":                                  "3",
	} {
		if got := tool(single, "query", singleDB, query); got != wantValue {
			t.Errorf("%s = %s, want %s", query, got, wantValue)
		}
	}

	// The converted app's own tests pass, with the database.
	test := exec.Command("go", "test", "./...")
	test.Dir = single
	test.Env = append(os.Environ(), "GORBITAL_REQUIRE_DB=1")
	if out, err := test.CombinedOutput(); err != nil {
		t.Errorf("converted app's tests: %v\n%s", err, out)
	}
}

// lineDiff lists lines only in want (-) or only in got (+).
func lineDiff(want, got string) string {
	in := func(s string) map[string]bool {
		m := map[string]bool{}
		for _, l := range strings.Split(s, "\n") {
			m[l] = true
		}
		return m
	}
	w, g := in(want), in(got)
	var b strings.Builder
	for _, l := range strings.Split(want, "\n") {
		if !g[l] {
			b.WriteString("- " + l + "\n")
		}
	}
	for _, l := range strings.Split(got, "\n") {
		if !w[l] {
			b.WriteString("+ " + l + "\n")
		}
	}
	return b.String()
}
