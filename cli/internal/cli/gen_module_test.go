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
)

// newMainApp copies examples/apps/shelfie without the shelves module, which
// orb gen module generates, and makes the copy the working directory. With
// buildable, the copy's replace directives point at this repository, so it
// builds.
func newMainApp(t *testing.T, buildable bool) string {
	t.Helper()
	shelfie := filepath.Join(repoRoot(t), "examples", "apps", "shelfie")
	dir := filepath.Join(t.TempDir(), "shelfie")
	err := filepath.WalkDir(shelfie, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(shelfie, path)
		switch {
		case d.IsDir() && (rel == filepath.Join("internal", "modules", "shelves") || d.Name() == ".orb" || d.Name() == "bin"):
			return filepath.SkipDir
		case d.IsDir():
			return os.MkdirAll(filepath.Join(dir, rel), 0o755)
		case strings.HasSuffix(rel, "_shelves.sql") || d.Name() == ".env":
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
		goMod := readFile(t, filepath.Join(dir, "go.mod"))
		writeFile(t, filepath.Join(dir, "go.mod"), strings.ReplaceAll(goMod, " => ../../..", " => "+repoRoot(t)))
	}
	t.Chdir(dir)
	return dir
}

// shelvesArgs is the command that generated Shelfie's shelves module.
var shelvesArgs = []string{"gen", "module", "Shelf", "name:string:unique", "description:text", "visibility:enum(private,shared)", "--plural", "Shelves"}

func TestGenModule(t *testing.T) {
	dir := newMainApp(t, false)
	shelfie := filepath.Join(repoRoot(t), "examples", "apps", "shelfie")

	code, out, errOut := runOrb(t, append(shelvesArgs, "--dry-run", "--json")...)
	var res genModuleResult
	if code != 0 || json.Unmarshal([]byte(out), &res) != nil || !res.DryRun || res.Module != "shelves" || res.Route != "/v1/shelves" ||
		!slices.Equal(res.Permissions, []string{"shelves.shelf.read", "shelves.shelf.write"}) || len(res.Files) != 27 {
		t.Fatalf("orb gen module --dry-run --json = %d %s %s", code, out, errOut)
	}
	if _, err := os.Stat(filepath.Join(dir, "internal", "modules", "shelves")); err == nil {
		t.Fatal("--dry-run wrote the module")
	}

	code, out, errOut = runOrb(t, append(shelvesArgs, "--allow-dirty")...)
	if code != 0 || !strings.Contains(out, "✓ Created module shelves") || !strings.Contains(out, "modify internal/modules/modules.gen.go") {
		t.Fatalf("orb gen module = %d %s %s", code, out, errOut)
	}
	// The files are Shelfie's (TestModuleMatchesShelfie keeps the templates
	// and Shelfie equal), and the module list names the module.
	for _, f := range res.Files {
		if got, want := readFile(t, filepath.Join(dir, filepath.FromSlash(f))), readFile(t, filepath.Join(shelfie, filepath.FromSlash(f))); got != want {
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
		{[]string{"Shelf", "name:string", "--org"}, 2, "Phase 7"},
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
	// orb gen resource --scope org is refused the same way.
	if code, _, errOut := runOrb(t, "gen", "resource", "Shelf", "name:string", "--scope", "org", "--allow-dirty"); code != 2 || !strings.Contains(errOut, "Phase 7") {
		t.Errorf("orb gen resource --scope org = %d %q", code, errOut)
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
		{"gen", "module", "Customer", "email:string:unique", "full_name:string", "nickname:string?", "account_code:string:unique", "notes:text", "tier:enum(free,pro,enterprise)", "region:enum(eu,us)"},
		{"gen", "module", "Note", "title:string", "body:text"},
		{"gen", "middleware", "RequireClientVersion", "--module", "customers"},
		{"gen", "middleware", "ActiveSubscription", "--module", "customers", "--guard"},
		{"gen", "middleware", "TenantHeader", "--global"},
	} {
		if code, out, errOut := runOrb(t, append(args, "--allow-dirty", "--no-input")...); code != 0 {
			t.Fatalf("orb %s = %d\n%s%s", strings.Join(args, " "), code, out, errOut)
		}
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
