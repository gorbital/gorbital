package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"gorbital.dev/cli/internal/merge"
	"gorbital.dev/cli/internal/recipes"
)

// newGitApp creates an app with orb new, commits it and makes it the
// working directory.
func newGitApp(t *testing.T, args ...string) {
	t.Helper()
	isolateGit(t)
	dir := t.TempDir()
	t.Chdir(dir)
	if code, _, errOut := runOrb(t, append([]string{"new", "shop-api", "--skip-tidy", "--no-git", "--json"}, args...)...); code != 0 {
		t.Fatalf("orb new %v = %d: %s", args, code, errOut)
	}
	t.Chdir(filepath.Join(dir, "shop-api"))
	commitAll(t, "Create app")
}

// writeV01App writes a Full app named name of tenancy in the v0.1 layout
// into parent/name, as orb v0.1 created them, with its lock; local is a
// gorbital checkout for replace directives, or "". With local it runs go
// mod tidy, as orb new does.
func writeV01App(t *testing.T, parent, name, tenancy, local string) string {
	t.Helper()
	dir := filepath.Join(parent, name)
	in := lockInputs{Name: name, Module: name, Preset: "full", Tenancy: tenancy, Mail: recipes.MailResend}
	tree, err := inputsTree(recipes.Embedded(), in, in.Mail, recipes.Data{Name: in.Name, Module: in.Module, LibraryVersion: recipes.LibraryVersion, Local: local})
	if err != nil {
		t.Fatal(err)
	}
	for p, content := range tree {
		writeFile(t, filepath.Join(dir, filepath.FromSlash(p)), string(content))
	}
	b, err := lockFromTree(in, tree).encode()
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, lockPath), string(b))
	if local != "" {
		goIn(t, dir, "mod", "tidy")
	}
	return dir
}

// newV01GitApp writes a Full app of tenancy in the v0.1 layout, as orb v0.1
// created them and this orb still upgrades them, with its lock, commits it
// and makes it the working directory.
func newV01GitApp(t *testing.T, tenancy string) {
	t.Helper()
	isolateGit(t)
	dir := writeV01App(t, t.TempDir(), "shop-api", tenancy, "")
	t.Chdir(dir)
	commitAll(t, "Create app")
}

func addOrgs(t *testing.T, wantCode int, args ...string) (upgradeResult, string) {
	t.Helper()
	code, out, errOut := runOrb(t, append([]string{"add", "orgs", "--skip-tidy"}, args...)...)
	if code != wantCode {
		t.Fatalf("orb add orgs %v = %d, want %d; stdout %s stderr %s", args, code, wantCode, out, errOut)
	}
	var res upgradeResult
	if slices.Contains(args, "--json") && out != "" {
		if err := json.Unmarshal([]byte(out), &res); err != nil {
			t.Fatalf("orb add orgs --json output %q: %v", out, err)
		}
	}
	return res, errOut
}

func TestAddOrgs(t *testing.T) {
	newV01GitApp(t, recipes.TenancySingle)
	writeFile(t, "internal/modules/customers/module.go", "package customers\n")
	commitAll(t, "Add customers")

	dry, _ := addOrgs(t, 0, "--json", "--dry-run")
	if !dry.DryRun || git(t, "status", "--porcelain") != "" || git(t, "branch", "--show-current") != "main" {
		t.Errorf("dry run = %+v and changed the repository", dry)
	}

	res, _ := addOrgs(t, 0, "--json", "--skip-build")
	if res.Branch != addOrgsBranch || git(t, "branch", "--show-current") != addOrgsBranch || len(res.Conflicts) != 0 {
		t.Errorf("result = %+v", res)
	}
	if !slices.Equal(res.UserScoped, []string{"customers"}) {
		t.Errorf("user-scoped modules = %v, want [customers]", res.UserScoped)
	}

	// The app is now the multi-tenant tree, apart from its migrations.
	tree, err := recipes.Embedded().Tree("full", recipes.TenancyMulti, recipes.LayoutV01, recipes.MailResend, recipes.Data{Name: "shop-api", Module: "shop-api", LibraryVersion: recipes.LibraryVersion})
	if err != nil {
		t.Fatal(err)
	}
	for p, content := range tree {
		if isMigrationPath(p) || slices.Contains(slices.Concat(untrackedPaths, derivedPaths), p) {
			continue
		}
		if got, err := os.ReadFile(p); err != nil || string(got) != string(content) {
			t.Errorf("%s isn't the multi-tenant file (%v)", p, err)
		}
	}
	for _, p := range []string{"db/migrations/20260915000002_projects.sql", "internal/modules/customers/module.go"} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s was removed: %v", p, err)
		}
	}
	for _, p := range []string{recipes.OrgsMigrationPath, "db/migrations/20260916000002_projects.sql"} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("%s was copied; an existing database needs the conversion migrations instead", p)
		}
	}
	orgsMigrations, _ := filepath.Glob("db/migrations/*_orgs.sql")
	converts, _ := filepath.Glob("db/migrations/*_orgs_convert.sql")
	if len(orgsMigrations) != 1 || len(converts) != 1 || orgsMigrations[0] >= converts[0] || orgsMigrations[0] <= "db/migrations/20260915000006" {
		t.Fatalf("migrations = %v and %v, want one of each, the conversion after", orgsMigrations, converts)
	}
	if readFile(t, orgsMigrations[0]) != string(tree[recipes.OrgsMigrationPath]) || readFile(t, converts[0]) != string(recipes.OrgsConversion()) {
		t.Error("the new migrations aren't the organisations migration and the conversion")
	}
	// Later organisation migrations follow the conversion (ADR-0056).
	settingsOrgs, _ := filepath.Glob("db/migrations/*_settings_org_purge.sql")
	if len(settingsOrgs) != 1 || settingsOrgs[0] <= converts[0] || readFile(t, settingsOrgs[0]) != string(tree["db/migrations/20260918000002_settings_org_purge.sql"]) {
		t.Errorf("settings organisation migrations = %v, want one after %s", settingsOrgs, converts[0])
	}
	for _, c := range res.Changes {
		if c.Path == orgsMigrations[0] && c.Action != merge.Create {
			t.Errorf("%s: %s", c.Path, c.Action)
		}
	}

	lock, err := readLock(".")
	if err != nil || lock.Inputs.Tenancy != recipes.TenancyMulti || !strings.HasSuffix(readFile(t, "gorbital.yaml"), "mail: resend\n") || !strings.Contains(readFile(t, "gorbital.yaml"), "tenancy: multi\n") {
		t.Errorf("lock inputs = %+v, %v; gorbital.yaml:\n%s", lock.Inputs, err, readFile(t, "gorbital.yaml"))
	}
	assertLockRebuilds(t, ".")

	// Running it again, once merged, changes nothing.
	commitAll(t, "Add organisations")
	if code, out, errOut := runOrb(t, "add", "orgs"); code != 0 || !strings.Contains(out, "already has organisations") {
		t.Errorf("second orb add orgs = %d, %q %q", code, out, errOut)
	}
}

// TestAddOrgsWarnsAboutTheOrganisationsMigrations checks the warning an app
// on gorbital.Main gets: the organisations module's migrations keep v0.1's
// versions, older than the built-in ones its database already ran, so an
// existing database refuses them (ADR-0083). A v0.1 app copies them under
// new versions instead and needs no warning.
func TestAddOrgsWarnsAboutTheOrganisationsMigrations(t *testing.T) {
	const banner = "an existing database will refuse these migrations"
	t.Run("an app on gorbital.Main", func(t *testing.T) {
		newGitApp(t, "--preset", "full")
		res, _ := addOrgs(t, 0, "--json", "--dry-run")
		if res.Layout != recipes.LayoutV02 {
			t.Fatalf("layout = %q, want %s", res.Layout, recipes.LayoutV02)
		}
		w := res.MigrationOrder
		if w == nil {
			t.Fatal("--json has no migration_order_warning")
		}
		if !slices.Equal(w.Versions, []string{"20260916000001", "20260918000002"}) {
			t.Errorf("versions = %v, want the organisations module's", w.Versions)
		}
		if w.Newest != strconv.FormatInt(latestBuiltinMigration, 10) {
			t.Errorf("newest = %q, want %d", w.Newest, latestBuiltinMigration)
		}
		if !strings.Contains(w.Error, "out-of-order") || !strings.Contains(w.Summary, "goose refuses") {
			t.Errorf("warning = %+v", w)
		}
		var commands, docs []string
		for _, o := range w.Options {
			commands = append(commands, o.Commands...)
			if o.Doc != "" {
				docs = append(docs, o.Doc)
			}
		}
		if !slices.Contains(commands, "docker compose down -v && docker compose up -d --wait") ||
			!slices.Contains(commands, "go run ./cmd/api migrate") || len(docs) == 0 {
			t.Errorf("options = %+v", w.Options)
		}

		// The report says it too, with the versions and a command.
		code, out, errOut := runOrb(t, "add", "orgs", "--skip-tidy", "--dry-run")
		if code != 0 {
			t.Fatalf("orb add orgs --dry-run = %d: %s", code, errOut)
		}
		for _, want := range []string{banner, "20260916000001", "20260918000002", "docker compose down -v", "goose_db_version", "docs/start/organisations.md"} {
			if !strings.Contains(out, want) {
				t.Errorf("the report lacks %q:\n%s", want, out)
			}
		}
	})
	t.Run("a v0.1 app", func(t *testing.T) {
		newV01GitApp(t, recipes.TenancySingle)
		res, _ := addOrgs(t, 0, "--json", "--dry-run")
		if res.MigrationOrder != nil {
			t.Errorf("a v0.1 app got %+v; it copies the migrations under new versions", res.MigrationOrder)
		}
		code, out, errOut := runOrb(t, "add", "orgs", "--skip-tidy", "--dry-run")
		if code != 0 {
			t.Fatalf("orb add orgs --dry-run = %d: %s", code, errOut)
		}
		if strings.Contains(out, banner) {
			t.Errorf("a v0.1 app was warned:\n%s", out)
		}
	})
}

func TestAddOrgsRefuses(t *testing.T) {
	t.Run("minimal preset", func(t *testing.T) {
		newGitApp(t, "--preset", "minimal")
		if _, errOut := addOrgs(t, 1); !strings.Contains(errOut, "Full preset") {
			t.Errorf("stderr = %q", errOut)
		}
	})
	t.Run("older release", func(t *testing.T) {
		newV01GitApp(t, recipes.TenancySingle)
		l, _ := readLock(".")
		l.Orb = lockOrb{Version: "v0.0.9"}
		b, _ := l.encode()
		writeFile(t, lockPath, string(b))
		commitAll(t, "Older lock")
		if _, errOut := addOrgs(t, 1); !strings.Contains(errOut, "run orb upgrade first") {
			t.Errorf("stderr = %q", errOut)
		}
	})
	t.Run("dirty tree", func(t *testing.T) {
		newV01GitApp(t, recipes.TenancySingle)
		writeFile(t, "scratch.txt", "x")
		if _, errOut := addOrgs(t, 1); !strings.Contains(errOut, "uncommitted changes") {
			t.Errorf("stderr = %q", errOut)
		}
	})
}
