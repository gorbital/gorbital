package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// TestUpgradeFromV040 is ADR-0050's definition of done: a Full app created
// by aps at v0.4.0 and edited with the commands developers run (a generated
// resource, a generated migration, aps add mail smtp, an edit to a tracked
// file) upgrades with this aps: every edit is kept, the lock becomes v2,
// the app builds, api/openapi.json is regenerated, the upgrade is committed
// on its branch, and no test that passed before fails after. Set APS_E2E=1
// to run it; it needs the v0.4.0 tag and the Go module cache or network.
func TestUpgradeFromV040(t *testing.T) {
	if os.Getenv("APS_E2E") == "" {
		t.Skip("set APS_E2E=1 to run the end-to-end test")
	}
	repo, err := filepath.Abs(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gitOutput(t.Context(), repo, "rev-parse", "--verify", "--quiet", "v0.4.0^{commit}"); err != nil {
		t.Skip("tag v0.4.0 isn't in this checkout (git fetch --tags)")
	}

	// Build aps as released at v0.4.0.
	work := t.TempDir()
	old := filepath.Join(work, "v0.4.0")
	if out, err := exec.Command("git", "-C", repo, "worktree", "add", "--detach", "--quiet", old, "v0.4.0").CombinedOutput(); err != nil {
		t.Fatalf("git worktree add: %v\n%s", err, out)
	}
	t.Cleanup(func() {
		if out, err := exec.Command("git", "-C", repo, "worktree", "remove", "--force", old).CombinedOutput(); err != nil {
			t.Logf("git worktree remove: %v\n%s", err, out)
		}
	})
	oldAps := filepath.Join(work, "aps-v0.4.0")
	build := exec.Command("go", "build", "-o", oldAps, "./cmd/aps")
	build.Dir = filepath.Join(old, "cli")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build aps v0.4.0: %v\n%s", err, out)
	}
	runOld := func(args ...string) {
		t.Helper()
		cmd := exec.Command(oldAps, args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("aps v0.4.0 %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}

	isolateGit(t)
	t.Chdir(work)
	runOld("new", "shop-api", "--preset", "full", "--local", repo, "--json")
	t.Chdir(filepath.Join(work, "shop-api"))
	commitAll(t, "Create app")
	runOld("gen", "resource", "Customer", "name:string", "notes:text", "--json")
	commitAll(t, "Add customers")
	runOld("gen", "migration", "AddCustomerPhone", "--json")
	commitAll(t, "Add migration")
	runOld("add", "mail", "--provider", "smtp", "--smtp-host", "smtp.example.com", "--json")
	commitAll(t, "Switch to SMTP")
	routes := readFile(t, "internal/app/routes.go") + "\n// Acme: an edit to a tracked file\n"
	writeFile(t, "internal/app/routes.go", routes)
	commitAll(t, "Edit routes")
	before := failingTests(t)

	if code, out, errOut := runAps(t, "upgrade", "--from", "v0.4.0"); code != 0 {
		t.Fatalf("aps upgrade --from v0.4.0 = %d\n%s\n%s", code, out, errOut)
	}

	if got := git(t, "branch", "--show-current"); got != upgradeBranchPrefix+Version {
		t.Errorf("branch = %s", got)
	}
	if got := git(t, "log", "-1", "--format=%s"); got != "Upgrade apistock to "+Version || git(t, "status", "--porcelain") != "" {
		t.Errorf("last commit = %q with a dirty tree; want the upgrade committed", got)
	}
	// The routes.go template changed since v0.4.0 (maintenance mode), so the
	// upgrade merges: the developer's line and the template's change both
	// arrive.
	if got := readFile(t, "internal/app/routes.go"); !strings.Contains(got, "// Acme: an edit to a tracked file") || !strings.Contains(got, "a.maintenance()") {
		t.Errorf("routes.go after the upgrade lacks the developer's edit or the template's change:\n%s", got)
	}
	for _, p := range []string{"internal/modules/customers/module.go", "internal/app/module_customers.go"} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("generated %s was lost: %v", p, err)
		}
	}
	if migrations, _ := filepath.Glob("db/migrations/*_add_customer_phone.sql"); len(migrations) != 1 {
		t.Errorf("generated migration lost: %v", migrations)
	}
	if !strings.Contains(readFile(t, ".env.example"), "SMTP_HOST=") {
		t.Error("the SMTP setup was lost")
	}
	lock, err := readLock(".")
	if err != nil || lock.APIVersion != LockAPIVersion || lock.Inputs.Mail != "smtp" {
		t.Errorf("lock = %+v, %v; want v2 recording SMTP", lock, err)
	}
	assertLockRebuilds(t, ".")

	goIn(t, ".", "vet", "./...")
	for _, name := range failingTests(t) {
		if !slices.Contains(before, name) {
			t.Errorf("%s passed before the upgrade and fails after", name)
		}
	}
}

var failLine = regexp.MustCompile(`(?m)^\s*--- FAIL: (\S+)`)

// failingTests runs the app's tests and returns the names of those failing.
func failingTests(t *testing.T) []string {
	t.Helper()
	out, _ := exec.Command("go", "test", "./...").CombinedOutput()
	var names []string
	for _, m := range failLine.FindAllStringSubmatch(string(out), -1) {
		names = append(names, m[1])
	}
	if len(names) == 0 && strings.Contains(string(out), "FAIL") && !strings.Contains(string(out), "--- FAIL") {
		t.Fatalf("the app's tests don't compile or run:\n%s", out)
	}
	return names
}
