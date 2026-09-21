package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gorbital.dev/cli/internal/recipes"
)

// Tests of the v0.2 layout: apps orb new writes on gorbital.Main (Phase 9,
// ADR-0083), and v0.1 apps kept on theirs.

// mainTree renders the Full preset's v0.2 tree of tenancy for shop-api.
func mainTree(t *testing.T, tenancy, mail string) map[string][]byte {
	t.Helper()
	tree, err := recipes.Embedded().Tree("full", tenancy, recipes.LayoutV02, mail, recipes.Data{Name: "shop-api", Module: "shop-api", LibraryVersion: recipes.LibraryVersion})
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

func TestNewFullAppIsOnGorbitalMain(t *testing.T) {
	newGitApp(t, "--preset", "full")
	lock, err := readLock(".")
	if err != nil || lock.Inputs.Layout != recipes.LayoutV02 {
		t.Fatalf("lock = %+v, %v; want the v0.2 layout", lock.Inputs, err)
	}
	for _, gone := range []string{"internal/app", "cmd/migrate", "cmd/seed"} {
		if _, err := os.Stat(gone); err == nil {
			t.Errorf("a new Full app has %s", gone)
		}
	}
	if appLayout(".") != layoutMain {
		t.Errorf("appLayout = %q, want %q", appLayout("."), layoutMain)
	}
	if got := migrateCommand("."); got != "go run ./cmd/api migrate" {
		t.Errorf("migrateCommand = %q", got)
	}
	if got := seedCommand("."); !slices.Equal(got, []string{"run", "./cmd/api", "seed"}) {
		t.Errorf("seedCommand = %v, want sign-in's seed command", got)
	}
	assertLockRebuilds(t, ".")

	// Minimal apps keep composing core packages (D17) and record no layout.
	newGitApp(t, "--preset", "minimal")
	if lock, err := readLock("."); err != nil || lock.Inputs.Layout != "" {
		t.Errorf("minimal lock = %+v, %v", lock.Inputs, err)
	}
	if seedCommand(".") != nil {
		t.Error("a Minimal app has a seed command")
	}
}

func TestV01AppKeepsItsLayout(t *testing.T) {
	newV01GitApp(t, recipes.TenancySingle)
	if got := seedCommand("."); !slices.Equal(got, []string{"run", "./cmd/seed"}) {
		t.Errorf("seedCommand = %v", got)
	}
	if got := migrateCommand("."); got != "go run ./cmd/migrate" {
		t.Errorf("migrateCommand = %q", got)
	}
	useRelease(t, recipes.Embedded())
	res := upgrade(t, 0, "--from", "v0.1.0")
	if !res.UpToDate || res.Layout != recipes.LayoutV01 {
		t.Errorf("upgrade = %+v, want a v0.1 app up to date with the v0.1 templates", res)
	}
	if _, err := os.Stat("cmd/api/mail.go"); err == nil {
		t.Error("the upgrade wrote a v0.2 file into a v0.1 app")
	}
	code, out, _ := runOrb(t, "upgrade", "--from", "v0.1.0", "--dry-run")
	if code != 0 || !strings.Contains(out, "up to date") {
		t.Errorf("upgrade --dry-run = %d, %q", code, out)
	}

	// A v0.1 release's templates rebuild a v0.1 app, whatever this orb's
	// default layout.
	older := olderRelease(t)
	appFromRelease(t, older, "v0.0.9")
	useRelease(t, recipes.Embedded())
	_, out, _ = runOrb(t, "upgrade", "--dry-run", "--skip-tidy")
	if !strings.Contains(out, "keeps the v0.1 layout") || !strings.Contains(out, "orb upgrade --layout v0.2") {
		t.Errorf("upgrade of a v0.1 app doesn't point at the layout move:\n%s", out)
	}
}

func TestV02UpgradeUpToDate(t *testing.T) {
	newGitApp(t, "--preset", "full", "--tenancy", "multi")
	useRelease(t, recipes.Embedded())
	res := upgrade(t, 0, "--from", "v0.2.0")
	if !res.UpToDate || res.Layout != recipes.LayoutV02 {
		t.Errorf("upgrade = %+v, want up to date in the v0.2 layout", res)
	}
}

// TestV02AddOrgsWithOwnSignIn: in an app that holds its sign-in, as orb new
// writes every Full app since v0.2.1 and as the only shape since v0.2.2
// removed --no-eject, orb add orgs copies the organisations module
// too, as orb eject orgs does, since the library's orgshttp takes the
// library's sign-in: main.go and the tests import the app's copies, and
// gorbital.lock records both.
func TestV02AddOrgsWithOwnSignIn(t *testing.T) {
	newGitApp(t, "--preset", "full")
	res, _ := addOrgs(t, 0, "--json", "--skip-build")
	if len(res.Conflicts) != 0 || len(res.UserScoped) != 0 {
		t.Fatalf("result = %+v", res)
	}
	main := readFile(t, "cmd/api/main.go")
	for _, want := range []string{`orgshttp "shop-api/internal/modules/orgs"`, `authhttp "shop-api/internal/modules/auth"`, "gorbital.WithModules(orgshttp.Module(auth)),"} {
		if !strings.Contains(main, want) {
			t.Errorf("main.go lacks %s:\n%s", want, main)
		}
	}
	if strings.Contains(main, `"gorbital.dev/gorbital/orgshttp"`) || strings.Contains(readFile(t, "internal/modules/projects/projects_test.go"), `"gorbital.dev/gorbital/orgshttp"`) {
		t.Error("a merged file still imports the library's orgshttp")
	}
	if _, err := os.Stat("internal/modules/orgs/orgshttp.go"); err != nil {
		t.Errorf("the organisations module wasn't copied: %v", err)
	}
	if !strings.Contains(readFile(t, "internal/modules/orgs/orgshttp.go"), `"shop-api/internal/modules/auth"`) {
		t.Error("the copied organisations module doesn't import the app's sign-in")
	}
	lock, err := readLock(".")
	if _, ok := lock.ejected("orgs"); err != nil || !ok {
		t.Errorf("gorbital.lock = %+v (%v), want orgs ejected", lock.Ejected, err)
	}
	if _, ok := lock.ejected("auth"); !ok {
		t.Error("gorbital.lock lost the ejected sign-in")
	}
}

func TestV02AddRLS(t *testing.T) {
	newGitApp(t, "--preset", "full", "--tenancy", "multi")
	_, out, _ := addRLS(t, 0)
	if !strings.Contains(out, "go run ./cmd/api migrate") {
		t.Errorf("next steps = %q, want gorbital.Main's migrate", out)
	}
	migrations, _ := filepath.Glob("db/migrations/*_row_level_security.sql")
	if len(migrations) != 1 || readFile(t, migrations[0]) != readFile(t, recipes.RowLevelSecurityPath) {
		t.Fatalf("migrations = %v", migrations)
	}
	assertLockRebuilds(t, ".")
	commitAll(t, "Add row-level security")

	code, out, errOut := runOrb(t, "gen", "module", "Customer", "name:string", "--org", "--json")
	var mod genModuleResult
	if code != 0 || json.Unmarshal([]byte(out), &mod) != nil || !mod.RowLevelSecurity {
		t.Errorf("orb gen module --org = %d %s %s; want the policy", code, out, errOut)
	}
}

// TestV02GenResource: in a multi-tenant app on gorbital.Main, orb gen
// resource writes an organisation module unless --scope user says otherwise,
// as it does in v0.1.
func TestV02GenResource(t *testing.T) {
	newGitApp(t, "--preset", "full", "--tenancy", "multi")
	for _, tt := range []struct {
		args  []string
		scope string
	}{
		{[]string{"gen", "resource", "Customer", "name:string", "--allow-dirty", "--json"}, recipes.ScopeTenant},
		{[]string{"gen", "resource", "Note", "title:string", "--scope", "user", "--allow-dirty", "--json"}, recipes.ScopeUser},
	} {
		code, out, errOut := runOrb(t, tt.args...)
		var res genModuleResult
		if code != 0 || json.Unmarshal([]byte(out), &res) != nil || res.Scope != tt.scope || !strings.Contains(errOut, "runs orb gen module") {
			t.Errorf("orb %v = %d %s %s; want a module of scope %s", tt.args, code, out, errOut, tt.scope)
		}
	}
}

func TestV02AddMail(t *testing.T) {
	newGitApp(t, "--preset", "full")
	res := addMail(t, "--provider", "smtp", "--smtp-host", "smtp.example.com")
	// The SMTP module is already in go.mod, required indirectly by gorbital.
	if want := []string{recipes.MainMailPath, ".env.example", ".env", "gorbital.yaml", lockPath}; !slices.Equal(res.Files, want) || len(res.Modules) != 0 {
		t.Errorf("files = %v, modules = %v; want %v", res.Files, res.Modules, want)
	}
	smtp := mainTree(t, recipes.TenancySingle, recipes.MailSMTP)
	for _, p := range []string{recipes.MainMailPath, ".env.example", "gorbital.yaml"} {
		if readFile(t, p) != string(smtp[p]) {
			t.Errorf("%s isn't the SMTP tree's", p)
		}
	}
	if _, err := os.Stat(recipes.InfraMailPath); err == nil {
		t.Errorf("orb add mail wrote %s into an app on gorbital.Main", recipes.InfraMailPath)
	}
	if lock, err := readLock("."); err != nil || lock.Inputs.Mail != recipes.MailSMTP {
		t.Errorf("lock = %+v, %v", lock.Inputs, err)
	}
	assertLockRebuilds(t, ".")
}

func TestV02AddStorage(t *testing.T) {
	newGitApp(t, "--preset", "full")
	code, out, errOut := runOrb(t, "add", "storage", "--driver", "minio", "--json")
	var res addStorageResult
	if code != 0 || json.Unmarshal([]byte(out), &res) != nil || res.Driver != "minio" || !slices.Contains(res.Files, "compose.yaml") {
		t.Errorf("orb add storage = %d %s %s", code, out, errOut)
	}
}
