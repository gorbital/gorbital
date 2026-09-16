package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// TestUpgradeFromRelease is ADR-0050's definition of done: a Full app
// created by orb at an earlier release and edited with the commands
// developers run (a generated resource, a generated migration, orb add mail
// smtp, an edit to a tracked file) upgrades with this orb: every edit is
// kept, the lock is v2, the app builds, api/openapi.json is regenerated, the
// upgrade is committed on its branch, and no test that passed before fails
// after. Set ORB_E2E=1 to run it; it needs the release tag and the Go module
// cache or network.
//
// The release is ORB_UPGRADE_FROM, or else the newest vX.Y.Z tag made after
// the rename to gorbital. Earlier tags can't be used: their CLI is in cmd/aps
// and their module is apistock.dev, so the orb built there writes apps this
// repository's library doesn't serve. The test skips until such a tag exists.
func TestUpgradeFromRelease(t *testing.T) {
	if os.Getenv("ORB_E2E") == "" {
		t.Skip("set ORB_E2E=1 to run the end-to-end test")
	}
	repo, err := filepath.Abs(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	from := os.Getenv("ORB_UPGRADE_FROM")
	if from == "" {
		if from = latestRenamedRelease(t, repo); from == "" {
			t.Skip("no vX.Y.Z tag after the rename to gorbital (earlier ones build apistock.dev apps with cmd/aps); tag a release, git fetch --tags, or set ORB_UPGRADE_FROM")
		}
	} else if !renamedRelease(t, repo, from) {
		t.Fatalf("ORB_UPGRADE_FROM=%s isn't a tag or commit here whose go.mod is module gorbital.dev with cli/cmd/orb (git fetch --tags?)", from)
	}

	// Build orb as released at from.
	work := t.TempDir()
	old := filepath.Join(work, "release")
	if out, err := exec.Command("git", "-C", repo, "worktree", "add", "--detach", "--quiet", old, from).CombinedOutput(); err != nil {
		t.Fatalf("git worktree add: %v\n%s", err, out)
	}
	t.Cleanup(func() {
		if out, err := exec.Command("git", "-C", repo, "worktree", "remove", "--force", old).CombinedOutput(); err != nil {
			t.Logf("git worktree remove: %v\n%s", err, out)
		}
	})
	oldOrb := filepath.Join(work, "orb-"+filepath.Base(from))
	build := exec.Command("go", "build", "-o", oldOrb, "./cmd/orb")
	build.Dir = filepath.Join(old, "cli")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build orb %s: %v\n%s", from, err, out)
	}
	runOld := func(args ...string) {
		t.Helper()
		cmd := exec.Command(oldOrb, args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("orb %s %s: %v\n%s", from, strings.Join(args, " "), err, out)
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

	code, out, errOut := runOrb(t, "upgrade", "--from", from, "--json")
	if code != 0 {
		t.Fatalf("orb upgrade --from %s = %d\n%s\n%s", from, code, out, errOut)
	}
	var res upgradeResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("orb upgrade --json output %q: %v", out, err)
	}
	if res.UpToDate {
		t.Skipf("orb at %s writes the same files as this orb, so there is nothing to upgrade; set ORB_UPGRADE_FROM to an older release", from)
	}

	if got := git(t, "branch", "--show-current"); got != upgradeBranchPrefix+Version {
		t.Errorf("branch = %s", got)
	}
	if got := git(t, "log", "-1", "--format=%s"); got != "Upgrade gorbital to "+Version || git(t, "status", "--porcelain") != "" {
		t.Errorf("last commit = %q with a dirty tree; want the upgrade committed", got)
	}
	// The developer's line survives whether or not the template changed.
	if got := readFile(t, "internal/app/routes.go"); !strings.Contains(got, "// Acme: an edit to a tracked file") || strings.Contains(got, "<<<<<<<") {
		t.Errorf("routes.go after the upgrade lacks the developer's edit or has conflict markers:\n%s", got)
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

// releaseTag matches release tags of the root module, without pre-releases.
var releaseTag = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)

// latestRenamedRelease returns the newest release tag in repo made after the
// rename to gorbital, or "" when there is none.
func latestRenamedRelease(t *testing.T, repo string) string {
	t.Helper()
	tags, err := gitOutput(t.Context(), repo, "tag", "--list", "v*", "--sort=-v:refname")
	if err != nil {
		t.Fatalf("git tag: %v", err)
	}
	for tag := range strings.Lines(tags) {
		if tag = strings.TrimSpace(tag); releaseTag.MatchString(tag) && renamedRelease(t, repo, tag) {
			return tag
		}
	}
	return ""
}

// renamedRelease reports whether ref in repo has the gorbital.dev module and
// orb's command in cli/cmd/orb.
func renamedRelease(t *testing.T, repo, ref string) bool {
	t.Helper()
	if !refPattern.MatchString(ref) {
		return false
	}
	goMod, err := gitOutput(t.Context(), repo, "cat-file", "blob", ref+":go.mod")
	if err != nil || modulePath([]byte(goMod)) != "gorbital.dev" {
		return false
	}
	_, err = gitOutput(t.Context(), repo, "cat-file", "-e", ref+":cli/cmd/orb/main.go")
	return err == nil
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
