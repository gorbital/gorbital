package cli

import (
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestAddRLS proves orb add rls on a real database: a multi-tenant app's
// database with projects in two organisations, after orb add rls and its
// migration, forces row-level security on every organisation table but the
// two left out; a role without bypass sees only the organisation its
// connection carries; orb doctor reports a superuser connection; an
// organisation resource generated afterwards carries the policy; and the
// app's own tests, which connect as a role without bypass, pass with every
// policy in place. Set ORB_E2E=1 and GORBITAL_TEST_DATABASE_URL to run it.
func TestAddRLS(t *testing.T) {
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
	t.Chdir(work)
	if code, _, errOut := runOrb(t, "new", "rls-api", "--local", repo, "--no-git", "--json", "--preset", "full", "--tenancy", "multi"); code != 0 {
		t.Fatalf("orb new = %d: %s", code, errOut)
	}
	dir := filepath.Join(work, "rls-api")
	writeFile(t, filepath.Join(dir, "internal", "schematool", "main.go"), strings.Replace(schemaTool, "MODULE", "rls-api", 1))
	tool := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("go", append([]string{"run", "./internal/schematool"}, args...)...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("schematool %s: %v\n%s", args[0], err, out)
		}
		return strings.TrimSpace(string(out))
	}
	u, err := url.Parse(adminURL)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/orb_e2e_rls"
	dbURL := u.String()
	// asRole connects as a role without bypass, carrying org in
	// gorbital.org_id from the connection's startup options.
	asRole := func(org string) string {
		q := u.Query()
		q.Set("options", "-c role=orb_e2e_rls_app -c gorbital.org_id="+org)
		v := *u
		v.RawQuery = strings.ReplaceAll(q.Encode(), "+", "%20")
		return v.String()
	}

	// An existing database with data in two organisations.
	tool("createdb", adminURL, "orb_e2e_rls")
	tool("migrate", dbURL)
	tool("exec", dbURL, `
		INSERT INTO orgs (id, name, created_by, created_at, updated_at) VALUES
			('org_a', 'A', 'usr_a', now(), now()), ('org_b', 'B', 'usr_b', now(), now());
		INSERT INTO projects (id, org_id, created_by, name, created_at, updated_at) VALUES
			('prj_1', 'org_a', 'usr_a', 'Apollo', now(), now()),
			('prj_2', 'org_a', 'usr_a', 'Gemini', now(), now()),
			('prj_3', 'org_b', 'usr_b', 'Apollo', now(), now());
		DO $$ BEGIN
			IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'orb_e2e_rls_app') THEN
				CREATE ROLE orb_e2e_rls_app NOLOGIN NOSUPERUSER NOBYPASSRLS;
			END IF;
		END $$;
		GRANT USAGE ON SCHEMA public TO orb_e2e_rls_app;
		GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO orb_e2e_rls_app;`)

	t.Chdir(dir)
	commitAll(t, "Create app")
	if code, out, errOut := runOrb(t, "add", "rls"); code != 0 {
		t.Fatalf("orb add rls = %d\n%s\n%s", code, out, errOut)
	}
	commitAll(t, "Add row-level security")
	tool("migrate", dbURL)

	for query, want := range map[string]string{
		"SELECT string_agg(relname, ',' ORDER BY relname) FROM pg_class WHERE relrowsecurity AND relforcerowsecurity AND relnamespace = 'public'::regnamespace": "projects",
		"SELECT count(*)::text FROM pg_policies WHERE policyname = 'org_isolation'":                                                                             "1",
	} {
		if got := tool("query", dbURL, query); got != want {
			t.Errorf("%s = %s, want %s", query, got, want)
		}
	}
	for org, want := range map[string]string{"org_a": "2", "org_b": "1", "": "0"} {
		if got := tool("query", asRole(org), "SELECT count(*)::text FROM projects"); got != want {
			t.Errorf("projects without an org_id filter as the app's role in %q = %s, want %s", org, got, want)
		}
	}

	// orb doctor reads the status through the app and warns about the
	// superuser in DATABASE_URL.
	var env []string
	for line := range strings.Lines(readFile(t, ".env.example")) {
		if !strings.HasPrefix(line, "DATABASE_URL=") {
			env = append(env, line)
		}
	}
	writeFile(t, ".env", strings.Join(env, "")+"\nDATABASE_URL="+dbURL+"\n")
	code, out, errOut := runOrb(t, "doctor", "--json")
	var report doctorResult
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("orb doctor = %d: %v\n%s\n%s", code, err, out, errOut)
	}
	if c := check(report, "row-level security"); c.Status != doctorWarn || !strings.Contains(c.Detail, "BYPASSRLS") {
		t.Errorf("orb doctor row-level security = %+v; checks %+v", c, report.Checks)
	}
	if err := os.Remove(".env"); err != nil {
		t.Fatal(err)
	}

	// An organisation resource generated now carries the policy.
	if code, _, errOut := runOrb(t, "gen", "resource", "Customer", "email:string:unique", "notes:text", "tier:enum(free,pro)"); code != 0 {
		t.Fatalf("orb gen resource = %d: %s", code, errOut)
	}
	tool("migrate", dbURL)
	if got := tool("query", dbURL, "SELECT string_agg(tablename, ',' ORDER BY tablename) FROM pg_policies WHERE policyname = 'org_isolation'"); got != "customers,projects" {
		t.Errorf("tables with the policy = %s, want customers,projects", got)
	}
	regen := exec.Command("go", "run", "./cmd/api", "openapi", "--dir", "api")
	regen.Dir = dir
	if out, err := regen.CombinedOutput(); err != nil {
		t.Fatalf("regenerate api files: %v\n%s", err, out)
	}
	record := exec.Command("go", "test", "./internal/app", "-run", "^TestPublicSurface$", "-update")
	record.Dir = dir
	if out, err := record.CombinedOutput(); err != nil {
		t.Fatalf("record api/surface.json: %v\n%s", err, out)
	}

	// The app's tests, generated resource included, pass with every policy
	// in place: their databases run the row-level security migration and
	// the app connects as a role without bypass.
	test := exec.Command("go", "test", "./...")
	test.Dir = dir
	test.Env = append(os.Environ(), "GORBITAL_REQUIRE_DB=1")
	if out, err := test.CombinedOutput(); err != nil {
		t.Errorf("the app's tests with row-level security: %v\n%s", err, out)
	}
}
