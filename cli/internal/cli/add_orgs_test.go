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
	newGitApp(t, "--preset", "full")
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
	tree, err := recipes.Embedded().Tree("full", recipes.TenancyMulti, recipes.MailResend, recipes.Data{Name: "shop-api", Module: "shop-api", LibraryVersion: recipes.LibraryVersion})
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

func TestAddOrgsRefuses(t *testing.T) {
	t.Run("minimal preset", func(t *testing.T) {
		newGitApp(t, "--preset", "minimal")
		if _, errOut := addOrgs(t, 1); !strings.Contains(errOut, "Full preset") {
			t.Errorf("stderr = %q", errOut)
		}
	})
	t.Run("older release", func(t *testing.T) {
		newGitApp(t, "--preset", "full")
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
		newGitApp(t, "--preset", "full")
		writeFile(t, "scratch.txt", "x")
		if _, errOut := addOrgs(t, 1); !strings.Contains(errOut, "uncommitted changes") {
			t.Errorf("stderr = %q", errOut)
		}
	})
}
