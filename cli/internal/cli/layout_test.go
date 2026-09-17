package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gorbital.dev/cli/internal/merge"
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

// TestV02AddOrgs: orb add orgs moves a v0.2 app from the single-tenant tree
// to the multi-tenant one: main.go adds orgshttp.Module(auth), the example
// module belongs to organisations, and only the data conversion is a new
// migration, since the organisations tables are the library's.
func TestV02AddOrgs(t *testing.T) {
	newGitApp(t, "--preset", "full")
	writeFile(t, "internal/modules/customers/module.go", "package customers\n")
	commitAll(t, "Add customers")

	res, _ := addOrgs(t, 0, "--json", "--skip-build")
	if res.Layout != recipes.LayoutV02 || len(res.Conflicts) != 0 || !slices.Equal(res.UserScoped, []string{"customers"}) {
		t.Fatalf("result = %+v", res)
	}
	tree := mainTree(t, recipes.TenancyMulti, recipes.MailResend)
	for p, content := range tree {
		if isMigrationPath(p) || slices.Contains(slices.Concat(untrackedPaths, derivedPaths), p) {
			continue
		}
		if got, err := os.ReadFile(p); err != nil || string(got) != string(content) {
			t.Errorf("%s isn't the multi-tenant file (%v)", p, err)
		}
	}
	if !strings.Contains(readFile(t, "cmd/api/main.go"), "gorbital.WithModules(orgshttp.Module(auth)),") {
		t.Errorf("main.go doesn't add the organisations module:\n%s", readFile(t, "cmd/api/main.go"))
	}
	if orgs, _ := filepath.Glob("db/migrations/*_orgs.sql"); len(orgs) != 0 {
		t.Errorf("orb add orgs copied %v; the library's organisations module has its migrations", orgs)
	}
	converts, _ := filepath.Glob("db/migrations/*_orgs_convert.sql")
	if len(converts) != 1 || readFile(t, converts[0]) != string(recipes.OrgsConversion()) {
		t.Errorf("conversion migrations = %v", converts)
	}
	if _, err := os.Stat("db/migrations/20260916000002_projects.sql"); err == nil {
		t.Error("the multi-tenant projects migration was copied; the conversion changes the existing table")
	}
	for _, c := range res.Changes {
		if c.Path == "cmd/api/main.go" && c.Action != merge.Update {
			t.Errorf("main.go: %s", c.Action)
		}
	}
	lock, err := readLock(".")
	if err != nil || lock.Inputs.Tenancy != recipes.TenancyMulti || lock.Inputs.Layout != recipes.LayoutV02 {
		t.Errorf("lock inputs = %+v, %v", lock.Inputs, err)
	}
	assertLockRebuilds(t, ".")
}

// TestV02AddRLS: orb add rls works in an app on gorbital.Main, which Phase 7
// couldn't do without a lock, and orb gen module --org then writes the
// policy.
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
