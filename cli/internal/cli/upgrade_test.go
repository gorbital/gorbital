package cli

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"gorbital.dev/cli/internal/merge"
	"gorbital.dev/cli/internal/recipes"
)

const (
	oldFooter    = "\nOld release footer.\n"
	oldNotes     = "OLD_NOTES.md"
	newMigration = "db/migrations/20260915000006_auth_social.sql"
)

// olderRelease returns this orb's Full templates as an earlier release
// that differs in three ways: README.md ends with a footer this release
// removed, OLD_NOTES.md exists, and the social sign-in migration doesn't.
func olderRelease(t *testing.T) recipes.Release {
	t.Helper()
	src := os.DirFS(filepath.Join(repoRoot(t), recipesDir))
	fsys := fstest.MapFS{}
	for _, dir := range []string{"full", "mail"} {
		err := fs.WalkDir(src, dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			data, err := fs.ReadFile(src, p)
			fsys[p] = &fstest.MapFile{Data: data}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	fsys["full/README.md.tmpl"].Data = append(fsys["full/README.md.tmpl"].Data, oldFooter...)
	fsys["full/"+oldNotes+".tmpl"] = &fstest.MapFile{Data: []byte("Notes from the old release.\n")}
	delete(fsys, "full/"+newMigration+".tmpl")
	return recipes.ReleaseFS(fsys)
}

// useRelease makes orb upgrade read release, and records the ref it asked for.
func useRelease(t *testing.T, release recipes.Release) *string {
	t.Helper()
	var ref string
	saved := openRelease
	openRelease = func(_ context.Context, _, r, _ string) (recipes.Release, func(), error) {
		ref = r
		return release, func() {}, nil
	}
	t.Cleanup(func() { openRelease = saved })
	return &ref
}

// isolateGit gives git a fixed identity and no user or system config.
func isolateGit(t *testing.T) {
	t.Helper()
	empty := filepath.Join(t.TempDir(), "gitconfig")
	writeFile(t, empty, "")
	for k, v := range map[string]string{
		"GIT_CONFIG_GLOBAL": empty, "GIT_CONFIG_NOSYSTEM": "1",
		"GIT_AUTHOR_NAME": "orb test", "GIT_AUTHOR_EMAIL": "test@example.com",
		"GIT_COMMITTER_NAME": "orb test", "GIT_COMMITTER_EMAIL": "test@example.com",
	} {
		t.Setenv(k, v)
	}
}

func git(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// appFromRelease writes a single-tenant Full app as release renders it, with
// a v2 lock naming version, commits it and makes it the working directory.
func appFromRelease(t *testing.T, release recipes.Release, version string) {
	t.Helper()
	isolateGit(t)
	dir := filepath.Join(t.TempDir(), "shop-api")
	in := lockInputs{Name: "shop-api", Module: "example.com/shop-api", Preset: "full", Tenancy: recipes.TenancySingle, Mail: recipes.MailResend}
	tree, err := release.Tree(in.Preset, in.Tenancy, in.Mail, recipes.Data{Name: in.Name, Module: in.Module, LibraryVersion: recipes.LibraryVersion})
	if err != nil {
		t.Fatal(err)
	}
	for p, content := range tree {
		writeFile(t, filepath.Join(dir, filepath.FromSlash(p)), string(content))
	}
	l := lockFromTree(in, tree)
	l.Orb = lockOrb{Version: version}
	b, err := l.encode()
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, lockPath), string(b))
	t.Chdir(dir)
	commitAll(t, "Create app")
}

func commitAll(t *testing.T, message string) {
	t.Helper()
	if _, err := os.Stat(".git"); err != nil {
		git(t, "init", "--quiet", "--initial-branch=main")
	}
	git(t, "add", "-A")
	git(t, "commit", "--quiet", "-m", message)
}

func upgrade(t *testing.T, wantCode int, args ...string) upgradeResult {
	t.Helper()
	code, out, errOut := runOrb(t, append([]string{"upgrade", "--json", "--skip-tidy"}, args...)...)
	if code != wantCode {
		t.Fatalf("orb upgrade %v = %d, want %d; stdout %s stderr %s", args, code, wantCode, out, errOut)
	}
	var res upgradeResult
	if wantCode == 0 || wantCode == 1 && out != "" {
		if err := json.Unmarshal([]byte(out), &res); err != nil {
			t.Fatalf("orb upgrade --json output %q: %v", out, err)
		}
	}
	return res
}

func actions(res upgradeResult) map[string]merge.Action {
	m := map[string]merge.Action{}
	for _, c := range res.Changes {
		m[c.Path] = c.Action
	}
	return m
}

func TestUpgradeMergesTemplateChanges(t *testing.T) {
	old := olderRelease(t)
	appFromRelease(t, old, "v0.0.9")
	ref := useRelease(t, old)

	// The developer's edits: a changed title in a file the release changes,
	// a line in a file it doesn't, and a module of their own.
	readme := readFile(t, "README.md")
	writeFile(t, "README.md", strings.Replace(readme, "# shop-api\n", "# Shop API for Acme\n", 1))
	appGo := readFile(t, "internal/app/app.go") + "\n// Acme customisation\n"
	writeFile(t, "internal/app/app.go", appGo)
	writeFile(t, "internal/modules/customers/module.go", "package customers\n")
	commitAll(t, "Edit app")

	// A dirty tree is refused; a dry run writes nothing.
	writeFile(t, "scratch.txt", "x")
	if code, _, errOut := runOrb(t, "upgrade", "--skip-tidy", "--skip-build"); code != 1 || !strings.Contains(errOut, "uncommitted changes") || strings.Contains(errOut, "allow-dirty") {
		t.Errorf("upgrade with a dirty tree = %d, %q", code, errOut)
	}
	os.Remove("scratch.txt")
	dry := upgrade(t, 0, "--dry-run")
	if !dry.DryRun || dry.Branch != "" || git(t, "status", "--porcelain") != "" || git(t, "branch", "--show-current") != "main" {
		t.Errorf("dry run = %+v, and it changed the repository", dry)
	}

	res := upgrade(t, 0, "--skip-build")
	if *ref != "v0.0.9" {
		t.Errorf("rebuilt the base from %q, want the lock's v0.0.9", *ref)
	}
	want := map[string]merge.Action{"README.md": merge.Merged, oldNotes: merge.Delete, newMigration: merge.Create}
	for p, a := range want {
		if actions(res)[p] != a {
			t.Errorf("%s: %s, want %s (changes %v)", p, actions(res)[p], a, actions(res))
		}
	}
	if len(res.Changes) != len(want) || len(res.Conflicts) != 0 || res.Committed || res.Unproven != 0 {
		t.Errorf("result = %+v", res)
	}

	if got := git(t, "branch", "--show-current"); got != upgradeBranchPrefix+Version || res.Branch != got {
		t.Errorf("branch = %s, result branch %s", got, res.Branch)
	}
	if got := readFile(t, "README.md"); !strings.HasPrefix(got, "# Shop API for Acme\n") || strings.Contains(got, "Old release footer") {
		t.Errorf("README.md didn't keep the title edit and take the footer removal:\n%s", got)
	}
	if readFile(t, "internal/app/app.go") != appGo || readFile(t, "internal/modules/customers/module.go") != "package customers\n" {
		t.Error("the developer's other edits changed")
	}
	if _, err := os.Stat(oldNotes); err == nil {
		t.Errorf("%s wasn't deleted", oldNotes)
	}
	if _, err := os.Stat(newMigration); err != nil {
		t.Errorf("%s wasn't created: %v", newMigration, err)
	}
	lock, err := readLock(".")
	if err != nil || lock.Orb.Version != Version {
		t.Errorf("lock = %+v, %v; want this release", lock.Orb, err)
	}
	assertLockRebuilds(t, ".")
}

func TestUpgradeConflictKeepsBothSides(t *testing.T) {
	old := olderRelease(t)
	appFromRelease(t, old, "v0.0.9")
	useRelease(t, old)
	readme := readFile(t, "README.md")
	writeFile(t, "README.md", strings.Replace(readme, "Old release footer.", "Our own footer.", 1))
	commitAll(t, "Edit footer")

	res := upgrade(t, 1, "--skip-build")
	if !slices.Equal(res.Conflicts, []string{"README.md"}) || res.Committed {
		t.Errorf("result = %+v, want a conflict in README.md and no commit", res)
	}
	got := readFile(t, "README.md")
	for _, s := range []string{"<<<<<<< yours", "Our own footer.", ">>>>>>> gorbital " + Version} {
		if !strings.Contains(got, s) {
			t.Errorf("README.md lacks %q:\n%s", s, got)
		}
	}
	if git(t, "branch", "--show-current") != upgradeBranchPrefix+Version || git(t, "log", "-1", "--format=%s") != "Edit footer" {
		t.Error("the conflicted upgrade should be on its branch, uncommitted")
	}
}

func TestUpgradeUpToDate(t *testing.T) {
	appFromRelease(t, recipes.Embedded(), Version)
	useRelease(t, recipes.Embedded())
	// The lock was written by a development build without a commit.
	if code, _, errOut := runOrb(t, "upgrade"); code != 2 || !strings.Contains(errOut, "--from") {
		t.Errorf("upgrade without a recorded release = %d, %q", code, errOut)
	}

	res := upgrade(t, 0, "--from", "v0.1.0")
	if !res.UpToDate || res.Branch != "" || git(t, "branch", "--show-current") != "main" {
		t.Errorf("result = %+v, want up to date without a branch", res)
	}
}

// TestUpgradeFromV1Lock: an app created by an early development build names
// the commit that created it with --from, which must reproduce every
// recorded hash.
func TestUpgradeFromV1Lock(t *testing.T) {
	appFromRelease(t, recipes.Embedded(), "unused")
	l, _ := readLock(".")
	var ops []map[string]string
	for _, f := range l.Files {
		ops = append(ops, map[string]string{"op": "createFile", "path": f.Path, "sha256": f.SHA256})
	}
	v1, _ := json.Marshal(map[string]any{"apiVersion": lockAPIVersionV1, "generator": "orb v0.1.0-dev",
		"recipes": []any{map[string]any{"name": "base-full", "version": "v0.1.0", "operations": ops}}})
	writeFile(t, lockPath, string(v1))
	commitAll(t, "Lock from an early development build")

	if code, _, errOut := runOrb(t, "upgrade"); code != 2 || !strings.Contains(errOut, "--from <commit>") {
		t.Errorf("upgrade of a v1 lock without --from = %d, %q", code, errOut)
	}

	useRelease(t, olderRelease(t))
	if code, _, errOut := runOrb(t, "upgrade", "--from", "v0.0.8", "--skip-tidy"); code != 1 || !strings.Contains(errOut, "don't match gorbital.lock") {
		t.Errorf("upgrade --from the wrong release = %d, %q", code, errOut)
	}

	useRelease(t, recipes.Embedded())
	res := upgrade(t, 0, "--from", "v0.0.9", "--skip-build")
	if len(res.Changes) != 0 || res.UpToDate || res.Branch == "" {
		t.Errorf("result = %+v, want only the lock rewritten on a branch", res)
	}
	if lock, err := readLock("."); err != nil || lock.APIVersion != LockAPIVersion || lock.Inputs.Preset != "full" {
		t.Errorf("lock after upgrade = %+v, %v", lock, err)
	}
	assertLockRebuilds(t, ".")
}
