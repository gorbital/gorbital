package cli

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// copyExampleApp copies examples/apps/<name> with its replace directives
// pointing at this repository, so it builds, commits it and makes the copy
// the working directory.
func copyExampleApp(t *testing.T, name string) string {
	t.Helper()
	return copyAppAt(t, filepath.Join("examples", "apps", name), name)
}

// copyAppAt copies the app at rel in the repository into a temporary
// directory, with its replace directives pointing at the checkout, and
// commits it.
func copyAppAt(t *testing.T, rel, name string) string {
	t.Helper()
	isolateGit(t)
	src := filepath.Join(repoRoot(t), rel)
	dir := filepath.Join(t.TempDir(), name)
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		switch {
		case d.IsDir() && (d.Name() == ".orb" || d.Name() == "bin"):
			return filepath.SkipDir
		case d.IsDir():
			return os.MkdirAll(filepath.Join(dir, rel), 0o755)
		case d.Name() == ".env":
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
	writeFile(t, filepath.Join(dir, "go.mod"), absoluteReplaces(t, readFile(t, filepath.Join(dir, "go.mod")), src))
	t.Chdir(dir)
	commitAll(t, "Create app")
	return dir
}

// absoluteReplaces rewrites the relative replace directives of an app in
// the repository to absolute paths, so a copy elsewhere builds.
func absoluteReplaces(t *testing.T, goMod, appDir string) string {
	t.Helper()
	var out []string
	for line := range strings.Lines(goMod) {
		if from, path, ok := strings.Cut(strings.TrimRight(line, "\r\n"), " => "); ok && strings.HasPrefix(path, "../") {
			absolute, err := filepath.Abs(filepath.Join(appDir, filepath.FromSlash(path)))
			if err != nil {
				t.Fatal(err)
			}
			line = from + " => " + absolute + "\n"
		}
		out = append(out, line)
	}
	return strings.Join(out, "")
}

func eject(t *testing.T, wantCode int, args ...string) (ejectResult, string) {
	t.Helper()
	code, out, errOut := runOrb(t, append([]string{"eject"}, args...)...)
	if code != wantCode {
		t.Fatalf("orb eject %v = %d, want %d; stdout %s stderr %s", args, code, wantCode, out, errOut)
	}
	var res ejectResult
	if slices.Contains(args, "--json") && code == 0 {
		if err := json.Unmarshal([]byte(out), &res); err != nil {
			t.Fatalf("orb eject --json output %q: %v", out, err)
		}
	}
	return res, out + errOut
}

func TestRewriteGoImports(t *testing.T) {
	rewrite := ejectImportRewriter("example.com/shop", []ejectableModule{{name: "auth", pkg: "authhttp"}, {name: "mailevents", pkg: "mailevents"}})
	src := `package delivery

import (
	"context"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/authhttp"
	"gorbital.dev/gorbital/opshttp"

	authdomain "gorbital.dev/gorbital/authhttp/internal/domain"
	"gorbital.dev/gorbital/authhttp/internal/delivery/jobs/authcleanup"
	"gorbital.dev/gorbital/mailevents"
)

// Uses "gorbital.dev/gorbital/authhttp" in a comment.
var _ = context.Background
`
	want := `package delivery

import (
	"context"

	authhttp "example.com/shop/internal/modules/auth"
	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/opshttp"

	"example.com/shop/internal/modules/auth/delivery/jobs/authcleanup"
	authdomain "example.com/shop/internal/modules/auth/domain"
	"example.com/shop/internal/modules/mailevents"
)

// Uses "gorbital.dev/gorbital/authhttp" in a comment.
var _ = context.Background
`
	got, changed, err := rewriteGoImports("x.go", []byte(src), rewrite)
	if err != nil || !changed || string(got) != want {
		t.Errorf("rewriteGoImports() = %v, %v\n%s\nwant:\n%s", changed, err, got, want)
	}
	if got, changed, err := rewriteGoImports("x.go", []byte("package x\n\nimport \"fmt\"\n"), rewrite); err != nil || changed || string(got) != "package x\n\nimport \"fmt\"\n" {
		t.Errorf("rewriteGoImports() without library imports = %q, %v, %v", got, changed, err)
	}
	if imp := libraryInternalImport([]byte("package x\n\nimport \"gorbital.dev/gorbital/internal/route\"\n")); imp != "gorbital.dev/gorbital/internal/route" {
		t.Errorf("libraryInternalImport() = %q", imp)
	}
}

func TestNoEjectReason(t *testing.T) {
	for src, want := range map[string]string{
		"//orb:noeject reads the repository\n\npackage x_test\n":     "reads the repository",
		"//orb:noeject\npackage x_test\n":                            "a test of the library itself",
		"package x_test\n\n//orb:noeject after the package clause\n": "",
		"//orb:noejectable\npackage x_test\n":                        "",
	} {
		if got, _ := noEjectReason([]byte(src)); got != want {
			t.Errorf("noEjectReason(%q) = %q, want %q", src, got, want)
		}
	}
}

func TestChangelogMentions(t *testing.T) {
	changelog := `# Changelog

## Unreleased (v0.3.0)

### Fixed

- authhttp: sessions end when a password changes. Details follow.
- opshttp: a fix elsewhere.

## v0.2.1 (2026-10-01)

- Sign-in (` + "`authhttp`" + `) refuses expired passkeys.
  - A nested authhttp entry

## v0.2.0 (2026-09-20)

- authhttp: the release that was ejected.
`
	got := changelogMentions(changelog, "authhttp", "v0.2.0")
	want := []string{"authhttp: sessions end when a password changes", "Sign-in (`authhttp`) refuses expired passkeys.", "A nested authhttp entry"}
	if !slices.Equal(got, want) {
		t.Errorf("changelogMentions() = %q, want %q", got, want)
	}
	if got := changelogMentions(changelog, "authhttp", "v0.3.0"); len(got) != 0 {
		t.Errorf("changelogMentions() at the newest version = %q", got)
	}
	long := "## v0.2.1\n" + strings.Repeat("- authhttp fix\n", maxChangelogEntries+2)
	if got := changelogMentions(long, "authhttp", "v0.2.0"); len(got) != maxChangelogEntries+1 || got[maxChangelogEntries] != "and 2 more" {
		t.Errorf("changelogMentions() of many entries = %q", got)
	}
}

func TestDoctorEjectedModules(t *testing.T) {
	copyExampleApp(t, "shelfie")
	writeFile(t, ".env", readFile(t, ".env.example"))
	eject(t, 0, "flags", "--skip-tidy", "--allow-dirty")
	fakeDoctorCommands(t, `{"current":1,"latest":1,"pending":0}`)
	check := func(res doctorResult, status, detail string) {
		t.Helper()
		for _, c := range res.Checks {
			if c.Name == "ejected" {
				if c.Status != status || !strings.Contains(c.Detail, detail) {
					t.Errorf("ejected check = %+v, want %s containing %q", c, status, detail)
				}
				return
			}
		}
		t.Errorf("no ejected check in %+v", res.Checks)
	}
	res := doctorRun(t, 0, "--fast")
	check(res, doctorOK, "internal/modules/flags is the app's code, ejected from gorbital.dev/gorbital/flagshttp v0.2.0")
	for _, c := range res.Checks {
		if (c.Name == "modules" || c.Name == "gorbital.lock") && c.Status != doctorOK {
			t.Errorf("%s check = %+v, want ok with an ejected module", c.Name, c)
		}
	}

	// The library's package changed since: a warning with what to do.
	lock, err := readLock(".")
	if err != nil {
		t.Fatal(err)
	}
	lock.Ejected[0].SHA256 = strings.Repeat("0", 64)
	data, _ := lock.encode()
	writeFile(t, lockPath, string(data))
	res = doctorRun(t, 0, "--fast")
	check(res, doctorWarn, "the library's flagshttp has changed since")

	// The copy is gone: the app doesn't build.
	if err := os.RemoveAll(filepath.Join("internal", "modules", "flags")); err != nil {
		t.Fatal(err)
	}
	res = doctorRun(t, 1, "--fast")
	check(res, doctorFail, "internal/modules/flags is missing")
}

func TestReadLockEjected(t *testing.T) {
	t.Chdir(t.TempDir())
	for body, want := range map[string]string{
		`{"apiVersion":"gorbital.dev/v2","orb":{},"inputs":{"name":"","module":"","preset":"","tenancy":""},"files":[],"ejected":[{"module":"auth","package":"gorbital.dev/gorbital/authhttp","version":"v0.2.0","date":"2026-09-17","sha256":"x"}]}`:      "",
		`{"apiVersion":"gorbital.dev/v2","orb":{},"inputs":{"name":"","module":"","preset":"","tenancy":""},"files":[],"ejected":[{"module":"billing","package":"gorbital.dev/gorbital/billing"}]}`:                                                        `unknown ejected module "billing"`,
		`{"apiVersion":"gorbital.dev/v2","orb":{},"inputs":{"name":"","module":"","preset":"","tenancy":""},"files":[],"ejected":[{"module":"ops","package":"gorbital.dev/gorbital/opshttp"},{"module":"ops","package":"gorbital.dev/gorbital/opshttp"}]}`: "twice",
		`{"apiVersion":"gorbital.dev/v2","orb":{},"inputs":{"name":"","module":"","preset":"","tenancy":""},"files":[],"ejected":[{"module":"ops","package":"example.com/ops"}]}`:                                                                          "want gorbital.dev/gorbital/opshttp",
	} {
		writeFile(t, lockPath, body)
		_, err := readLock(".")
		if (want == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), want)) {
			t.Errorf("readLock(%s) error = %v, want %q", body, err, want)
		}
	}
}
