package cli

import (
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"gorbital.dev/cli/internal/recipes"
)

// newMainApp copies examples/shelfie without the shelves and clubbooks
// modules, which orb gen module generates, and makes the copy the working
// directory. With
// buildable, the copy's replace directives point at this repository, so it
// builds.
func newMainApp(t *testing.T, buildable bool) string {
	t.Helper()
	shelfie := filepath.Join(repoRoot(t), "examples", "shelfie")
	dir := filepath.Join(t.TempDir(), "shelfie")
	err := filepath.WalkDir(shelfie, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(shelfie, path)
		switch {
		case d.IsDir() && (rel == filepath.Join("internal", "modules", "shelves") || rel == filepath.Join("internal", "modules", "clubbooks") ||
			d.Name() == ".orb" || d.Name() == "bin"):
			return filepath.SkipDir
		case d.IsDir():
			return os.MkdirAll(filepath.Join(dir, rel), 0o755)
		case strings.HasSuffix(rel, "_shelves.sql") || strings.HasSuffix(rel, "_club_books.sql") || d.Name() == ".env":
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, rel), data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	app := appInfo{dir: dir, module: "example.com/shelfie"}
	modules, err := findModules(app)
	if err != nil {
		t.Fatal(err)
	}
	list, err := renderModules(modules)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, filepath.FromSlash(modulesGenPath)), string(list))
	if buildable {
		// absoluteReplaces resolves each directive against the application's
		// own directory, so this doesn't care how deep examples/shelfie is.
		goMod := readFile(t, filepath.Join(dir, "go.mod"))
		writeFile(t, filepath.Join(dir, "go.mod"), absoluteReplaces(t, goMod, shelfie))
	}
	t.Chdir(dir)
	return dir
}

// nextMigrationAfterNewest is the version orb gives a migration generated in
// dir: the one after the app's newest, since Shelfie's are dated later than
// the clock this test runs on. It is computed rather than written down,
// because every chapter that adds a migration moves it.
func nextMigrationAfterNewest(t *testing.T, dir string) string {
	t.Helper()
	v, err := nextMigrationVersion(dir, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// shelvesArgs is the command that generated Shelfie's shelves module.
var shelvesArgs = []string{"gen", "module", "Shelf", "name:string:unique", "description:text", "visibility:enum(private,shared)", "--plural", "Shelves"}

// clubBooksArgs is the command that generated Shelfie's clubbooks module,
// owned by organisations (chapter 8).
var clubBooksArgs = []string{"gen", "module", "ClubBook", "title:string:unique", "author:string?", "status:enum(proposed,reading,finished)", "note:text", "--org"}

func TestGenModule(t *testing.T) {
	dir := newMainApp(t, false)
	shelfie := filepath.Join(repoRoot(t), "examples", "shelfie")

	code, out, errOut := runOrb(t, append(shelvesArgs, "--dry-run", "--json")...)
	var res genModuleResult
	if code != 0 || json.Unmarshal([]byte(out), &res) != nil || !res.DryRun || res.Module != "shelves" || res.Route != "/v1/shelves" ||
		!slices.Equal(res.Permissions, []string{"shelves.shelf.read", "shelves.shelf.write"}) || len(res.Files) != 28 {
		t.Fatalf("orb gen module --dry-run --json = %d %s %s", code, out, errOut)
	}
	if _, err := os.Stat(filepath.Join(dir, "internal", "modules", "shelves")); err == nil {
		t.Fatal("--dry-run wrote the module")
	}

	wantVersion := nextMigrationAfterNewest(t, dir)
	code, out, errOut = runOrb(t, append(shelvesArgs, "--allow-dirty")...)
	if code != 0 || !strings.Contains(out, "✓ Created module shelves") || !strings.Contains(out, "modify internal/modules/modules.gen.go") {
		t.Fatalf("orb gen module = %d %s %s", code, out, errOut)
	}
	// The files are Shelfie's (TestModuleMatchesShelfie keeps the templates
	// and Shelfie equal), and the module list names the module. The
	// migration follows the app's newest one, 20260920000004_phone_sign_in.sql;
	// Shelfie's shelves migration was generated before that one existed.
	if !strings.Contains(readFile(t, filepath.Join(dir, filepath.FromSlash(modulesGenPath))), "shelves.Module(),") {
		t.Error("modules.gen.go doesn't list shelves")
	}
	for _, f := range res.Files {
		if f == modulesGenPath || f == manifestPath {
			continue // Shelfie's lists clubbooks too, and records their scopes
		}
		golden := f
		if strings.HasSuffix(f, "_shelves.sql") {
			if f != "db/migrations/"+wantVersion+"_shelves.sql" {
				t.Errorf("migration = %s, want the one after the app's newest (%s)", f, wantVersion)
			}
			golden = "db/migrations/20260920000002_shelves.sql"
		}
		if got, want := readFile(t, filepath.Join(dir, filepath.FromSlash(f))), readFile(t, filepath.Join(shelfie, filepath.FromSlash(golden))); got != want {
			t.Errorf("%s differs from Shelfie's", f)
		}
	}

	// Never overwritten: a second run is refused, and the dry run says so.
	for _, args := range [][]string{append(shelvesArgs, "--allow-dirty"), append(shelvesArgs, "--dry-run")} {
		if code, _, errOut := runOrb(t, args...); code != 1 || !strings.Contains(errOut, "internal/modules/shelves already exists") {
			t.Errorf("orb gen module again = %d %q", code, errOut)
		}
	}
}

func TestGenModuleOrg(t *testing.T) {
	dir := newMainApp(t, false)
	shelfie := filepath.Join(repoRoot(t), "examples", "shelfie")

	code, out, errOut := runOrb(t, append(clubBooksArgs, "--dry-run", "--json")...)
	var res genModuleResult
	if code != 0 || json.Unmarshal([]byte(out), &res) != nil || res.Scope != recipes.ScopeTenant || res.Route != "/v1/orgs/{orgId}/club-books" || res.RowLevelSecurity ||
		!slices.Equal(res.Permissions, []string{"clubbooks.club_book.read", "clubbooks.club_book.write"}) || len(res.Files) != 28 {
		t.Fatalf("orb gen module --org --dry-run --json = %d %s %s", code, out, errOut)
	}

	// Shelfie's main.go adds orgshttp, so the next steps don't ask for it.
	code, out, errOut = runOrb(t, append(clubBooksArgs, "--allow-dirty")...)
	if code != 0 || !strings.Contains(out, "for an organisation's club books (guard.OrgMember)") || !strings.Contains(out, "GET /v1/orgs") ||
		!strings.Contains(out, "Every organisation role (owner, admin and member)") || strings.Contains(out, "orgshttp.Module(auth)") {
		t.Fatalf("orb gen module --org = %d %s %s", code, out, errOut)
	}
	// The files are Shelfie's clubbooks module, which TestModuleMatchesShelfie
	// keeps equal to the templates.
	for _, f := range res.Files {
		if f == modulesGenPath || f == manifestPath {
			continue
		}
		golden := f
		if strings.HasSuffix(f, "_club_books.sql") {
			golden = "db/migrations/20260920000005_club_books.sql" // Shelfie's, generated when its history was shorter
		}
		if got, want := readFile(t, filepath.Join(dir, filepath.FromSlash(f))), readFile(t, filepath.Join(shelfie, filepath.FromSlash(golden))); got != want {
			t.Errorf("%s differs from Shelfie's", f)
		}
	}
}

// TestGenModuleOrgWiring checks what orb gen module --org says and writes
// around the module: the organisations module as a next step when main.go
// lacks it, never an edit to main.go, and the row-level security policy in
// apps that have row-level security.
func TestGenModuleOrgWiring(t *testing.T) {
	dir := newMainApp(t, false)
	mainGo := filepath.Join(dir, "cmd", "api", "main.go")
	var kept []string
	for line := range strings.Lines(readFile(t, mainGo)) {
		if !strings.Contains(line, "orgshttp") {
			kept = append(kept, line)
		}
	}
	withoutOrgs := strings.Join(kept, "")
	writeFile(t, mainGo, withoutOrgs)

	projectsVersion := nextMigrationAfterNewest(t, dir)
	code, out, errOut := runOrb(t, "gen", "module", "Project", "name:string", "--org", "--allow-dirty")
	if code != 0 || !strings.Contains(out, "1. Add the organisations module") || !strings.Contains(out, "gorbital.WithModules(orgshttp.Module(auth))") ||
		!strings.Contains(out, "passed to gorbital.WithAuth") {
		t.Fatalf("orb gen module --org without orgshttp in main.go = %d %s %s", code, out, errOut)
	}
	if readFile(t, mainGo) != withoutOrgs {
		t.Error("orb gen module --org changed main.go")
	}
	migration := readFile(t, filepath.Join(dir, "db", "migrations", projectsVersion+"_projects.sql"))
	if !strings.Contains(migration, "org_id     text        NOT NULL,") || !strings.Contains(migration, "REFERENCES orgs (id) ON DELETE CASCADE") ||
		strings.Contains(migration, "ROW LEVEL SECURITY") {
		t.Errorf("migration without row-level security:\n%s", migration)
	}

	// With the app's row-level security migration, or rls: true in
	// gorbital.yaml, the migration carries the policy.
	writeFile(t, filepath.Join(dir, "db", "migrations", "20260920000006_row_level_security.sql"), "-- +goose Up\nSELECT 1;\n")
	code, out, errOut = runOrb(t, "gen", "module", "Invoice", "number:string:unique", "--org", "--allow-dirty", "--json")
	var res genModuleResult
	if code != 0 || json.Unmarshal([]byte(out), &res) != nil || !res.RowLevelSecurity {
		t.Fatalf("orb gen module --org with row-level security = %d %s %s", code, out, errOut)
	}
	if got := readFile(t, filepath.Join(dir, filepath.FromSlash(res.Migration))); !strings.Contains(got, "ALTER TABLE invoices FORCE ROW LEVEL SECURITY;") ||
		!strings.Contains(got, "CREATE POLICY org_isolation ON invoices") {
		t.Errorf("migration with row-level security:\n%s", got)
	}
	if err := os.Remove(filepath.Join(dir, "db", "migrations", "20260920000006_row_level_security.sql")); err != nil {
		t.Fatal(err)
	}
	if code, out, _ := runOrb(t, "gen", "module", "Invoice", "number:string", "--org", "--dry-run", "--json"); code != 1 {
		t.Errorf("a second invoices module = %d %s, want refused", code, out)
	}
	writeFile(t, filepath.Join(dir, "gorbital.yaml"), readFile(t, filepath.Join(dir, "gorbital.yaml"))+"rls: true\n")
	code, out, errOut = runOrb(t, "gen", "module", "Receipt", "number:string", "--org", "--dry-run", "--json")
	res = genModuleResult{}
	if code != 0 || json.Unmarshal([]byte(out), &res) != nil || !res.RowLevelSecurity {
		t.Errorf("orb gen module --org with rls: true in gorbital.yaml = %d %s %s", code, out, errOut)
	}
	// Row-level security is for organisations' rows only.
	code, out, errOut = runOrb(t, "gen", "module", "Receipt", "number:string", "--dry-run", "--json")
	res = genModuleResult{}
	if code != 0 || json.Unmarshal([]byte(out), &res) != nil || res.RowLevelSecurity || res.Scope != "user" {
		t.Errorf("orb gen module (user) with rls: true = %d %s %s", code, out, errOut)
	}
}

func TestGenModuleArchitectureTestAndOptional(t *testing.T) {
	dir := newMainApp(t, false)
	if err := os.Remove(filepath.Join(dir, "internal", "modules", "architecture_test.go")); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := runOrb(t, "gen", "module", "Friend", "name:string", "nickname:string?", "--allow-dirty", "--json")
	var res genModuleResult
	if code != 0 || json.Unmarshal([]byte(out), &res) != nil || !slices.Contains(res.Files, "internal/modules/architecture_test.go") {
		t.Fatalf("orb gen module in an app without the architecture test = %d %s %s", code, out, errOut)
	}
	if got := readFile(t, filepath.Join(dir, "db", "migrations", filepath.Base(res.Migration))); !strings.Contains(got, "nickname   text        NOT NULL DEFAULT '' CHECK (char_length(nickname) <= 100)") {
		t.Errorf("optional string column in:\n%s", got)
	}
}

func TestGenModuleErrors(t *testing.T) {
	newMainApp(t, false)
	for _, tt := range []struct {
		args []string
		code int
		want string
	}{
		{[]string{"Shelf"}, 2, "missing fields"},
		{[]string{}, 2, "missing record name"},
		{[]string{"Shelf", "name:strin"}, 2, "type must be string, text or enum"},
		{[]string{"Shelf", "nickname:string?"}, 2, "required string field"},
		{[]string{"Page", "name:string"}, 2, "would clash"},
		{[]string{"Book", "title:string"}, 1, "internal/modules/books already exists"},
	} {
		code, _, errOut := runOrb(t, append([]string{"gen", "module", "--no-input", "--allow-dirty"}, tt.args...)...)
		if code != tt.code || !strings.Contains(errOut, tt.want) {
			t.Errorf("orb gen module %q = %d %q, want %d containing %q", tt.args, code, errOut, tt.code, tt.want)
		}
	}
	// orb gen resource --scope org is orb gen module --scope tenant: org is
	// the old name of the scope, and reported under the new one.
	code, out, errOut := runOrb(t, "gen", "resource", "Shelf", "name:string", "--scope", "org", "--dry-run", "--json")
	var res genModuleResult
	if code != 0 || json.Unmarshal([]byte(out), &res) != nil || res.Scope != recipes.ScopeTenant || res.Route != "/v1/orgs/{orgId}/shelfs" {
		t.Errorf("orb gen resource --scope org = %d %s %s", code, out, errOut)
	}

	// A v0.1 app gets orb gen resource.
	newResourceApp(t)
	if code, _, errOut := runOrb(t, "gen", "module", "Shelf", "name:string"); code != 1 || !strings.Contains(errOut, "use orb gen resource") {
		t.Errorf("orb gen module in a v0.1 app = %d %q", code, errOut)
	}
	// An app that is neither.
	t.Chdir(t.TempDir())
	writeFile(t, "go.mod", "module example.com/plain\n")
	if code, _, errOut := runOrb(t, "gen", "module", "Shelf", "name:string"); code != 1 || !strings.Contains(errOut, "isn't an app on gorbital.Main") {
		t.Errorf("orb gen module in a plain module = %d %q", code, errOut)
	}
}

func TestGenResourceIsGenModuleOnMain(t *testing.T) {
	dir := newMainApp(t, false)
	code, out, errOut := runOrb(t, "gen", "resource", "Note", "title:string", "body:text", "--allow-dirty", "--json")
	var res genModuleResult
	if code != 0 || json.Unmarshal([]byte(out), &res) != nil || res.Module != "notes" || len(res.Permissions) != 2 ||
		!strings.Contains(errOut, "runs orb gen module") {
		t.Fatalf("orb gen resource on gorbital.Main = %d %s %s", code, out, errOut)
	}
	if _, err := os.Stat(filepath.Join(dir, "internal", "modules", "notes", "delivery", "routes.go")); err != nil {
		t.Errorf("no layered module: %v", err)
	}
	if !strings.Contains(readFile(t, filepath.Join(dir, filepath.FromSlash(modulesGenPath))), "notes.Module(),") {
		t.Error("modules.gen.go doesn't list notes")
	}
}

// TestGeneratedCodePasses generates modules of every shape, middleware and a
// guard into a copy of Shelfie, then vets and lints the app and runs every
// test, the generated ones included, on PostgreSQL.
func TestGeneratedCodePasses(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and tests a copy of Shelfie")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go is not installed")
	}
	dir := newMainApp(t, true)
	for _, args := range [][]string{
		shelvesArgs,
		clubBooksArgs,
		{"gen", "module", "Customer", "email:string:unique", "full_name:string", "nickname:string?", "account_code:string:unique", "notes:text", "tier:enum(free,pro,enterprise)", "region:enum(eu,us)"},
		{"gen", "module", "Note", "title:string", "body:text"},
		// The scopes without an ownership column (ADR-0091). The custom
		// module's own tests are red until its policy is written, so this
		// one gets a policy before the suite runs.
		{"gen", "module", "Catalogue", "name:string:unique", "blurb:text", "state:enum(draft,live)", "--scope", "public"},
		{"gen", "module", "Ticket", "subject:string:unique", "body:text", "state:enum(open,closed)", "--scope", "custom"},
		{"policy"},
		{"gen", "middleware", "RequireClientVersion", "--module", "customers"},
		{"gen", "middleware", "ActiveSubscription", "--module", "customers", "--guard"},
		{"gen", "middleware", "TenantHeader", "--global"},
		{"gen", "module", "Project", "name:string", "code:string:unique", "summary:string?", "stage:enum(draft,live)", "--org"},
		// Row-level security from here on: the app's migration covers the
		// organisation tables so far, and the next module's migration carries
		// the policy.
		{"rls"},
		{"gen", "module", "Invoice", "number:string:unique", "notes:text", "--org"},
	} {
		switch args[0] {
		case "rls":
			addRowLevelSecurity(t, dir)
			continue
		case "policy":
			writeTicketPolicy(t, dir)
			continue
		}
		if code, out, errOut := runOrb(t, append(args, "--allow-dirty", "--no-input")...); code != 0 {
			t.Fatalf("orb %s = %d\n%s%s", strings.Join(args, " "), code, out, errOut)
		}
	}
	if invoices, _ := filepath.Glob(filepath.Join(dir, "db", "migrations", "*_invoices.sql")); len(invoices) != 1 ||
		!strings.Contains(readFile(t, invoices[0]), "CREATE POLICY org_isolation ON invoices") {
		t.Errorf("the invoices migration %v has no row-level security policy", invoices)
	}
	run := func(name string, args ...string) string {
		t.Helper()
		cmd := exec.Command(name, args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
		}
		return string(out)
	}
	if unformatted := run("gofmt", "-l", "."); unformatted != "" {
		t.Errorf("gofmt -l:\n%s", unformatted)
	}
	run("go", "vet", "./...")
	run("go", "run", "./cmd/api", "openapi", "--dir", "api")
	if lint, err := exec.LookPath("golangci-lint"); err == nil {
		run(lint, "run", "--config", filepath.Join(repoRoot(t), ".golangci.yml"), "./...")
	} else {
		t.Log("golangci-lint isn't installed: generated code not linted")
	}
	if os.Getenv("GORBITAL_TEST_DATABASE_URL") == "" {
		t.Skip("vetted the generated code; set GORBITAL_TEST_DATABASE_URL to run its tests")
	}
	run("go", "test", "-count=1", "./...")
}

// writeTicketPolicy writes the access rule a --scope custom module is
// generated without: the developer's job, done here so the generated
// module's own tests can run. Every ticket is its creator's, which is a
// rule gorbital could have guessed and therefore didn't.
func writeTicketPolicy(t *testing.T, dir string) {
	t.Helper()
	path := filepath.Join(dir, "internal", "modules", "tickets", "policy.go")
	src := readFile(t, path)
	src = strings.ReplaceAll(src, `"gorbital.dev/gorbital"`, `"gorbital.dev/actor"`)
	for _, method := range []string{"CanRead", "CanWrite"} {
		decl := "func (p Policy) " + method + "(ctx context.Context, ticket domain.Ticket) error {\n"
		src = strings.Replace(src, decl+"\treturn gorbital.ErrNotImplemented", decl+ownerCheck, 1)
	}
	filter := "func (p Policy) Filter(ctx context.Context, q *repository.Query) error {\n"
	src = strings.Replace(src, filter+"\treturn gorbital.ErrNotImplemented", filter+ownerFilter, 1)
	if strings.Contains(src, "return gorbital.ErrNotImplemented") {
		t.Fatalf("the generated policy.go didn't have the stubs this test replaces:\n%s", src)
	}
	writeFile(t, path, src)
}

// The bodies writeTicketPolicy puts in place of the stubs.
const (
	ownerCheck = "\tif a, ok := actor.From(ctx); !ok || a.ID != ticket.CreatedBy {\n" +
		"\t\treturn domain.ErrTicketNotFound\n\t}\n\treturn nil"
	ownerFilter = "\ta, ok := actor.From(ctx)\n\tif !ok {\n\t\treturn domain.ErrUnauthenticated\n\t}\n" +
		"\tq.And(\"created_by = ?\", a.ID)\n\treturn nil"
)

// addRowLevelSecurity adds the multi-tenant apps' row-level security
// migration (orb add rls's file) to the app in dir, as its next migration.
func addRowLevelSecurity(t *testing.T, dir string) {
	t.Helper()
	version, err := nextMigrationVersion(dir, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	policy := readFile(t, filepath.Join(repoRoot(t), "examples", "v0.1", "full-multi", "db", "row_level_security.sql"))
	writeFile(t, filepath.Join(dir, "db", "migrations", version+"_row_level_security.sql"), policy)
}
