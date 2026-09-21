package cli

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestSecurityPrintsNoSecret checks what roadmap item 93 asks of the
// command: it may say where a secret is and what it is called, never what
// it is. A value planted in every place a rule looks must not reach the
// report.
func TestSecurityPrintsNoSecret(t *testing.T) {
	const secret = "hunter2-zzqq-do-not-print"
	dir := t.TempDir()
	write := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(path)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, path), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("db/migrations/20260101000001_x.sql", "-- +goose Up\nCREATE TABLE t (\n    id text PRIMARY KEY,\n    token text NOT NULL DEFAULT '"+secret+"'\n);\n")
	write("internal/modules/x/store.go", `package x

import "log/slog"

const insertSQL = "INSERT INTO other (id, secret) VALUES ('`+secret+`', $1)"

const defaultToken = "`+secret+`"

func Log(log *slog.Logger, apiKey string) {
	log.Info("`+secret+`", "key", apiKey)
	_ = insertSQL
	_ = defaultToken
}
`)
	var out strings.Builder
	res := securityResult{App: "x", Findings: scanApp(dir).rules(), NotChecked: securityNotChecked}
	if len(res.Findings) == 0 {
		t.Fatal("the fixture planted no findings")
	}
	res.report(&out)
	if strings.Contains(out.String(), secret) {
		t.Errorf("the report printed a value it found:\n%s", out.String())
	}
	encoded, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), secret) {
		t.Errorf("--json printed a value it found:\n%s", encoded)
	}
}

// securityFixtureApp copies the flawed fixture into a temporary directory
// as an application: a go.mod, no gorbital.lock, and one of every planted
// flaw. It needs no library and no network, so the shape of
// orb doctor --security --json is recorded from it.
func securityFixtureApp(t *testing.T) string {
	t.Helper()
	src := securityFixture(t, "flawed")
	dir := filepath.Join(t.TempDir(), "fixture-api")
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dir, rel), 0o755)
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
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/fixture-api\n\ngo 1.26.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	return dir
}

// TestCopiedPackageFilesMatchTheCopy keeps the reader that
// orb doctor --security compares versions with in step with the copy
// planner itself: the two walk the same package and must agree on which
// files an app ends up holding, or the diff would be against the wrong
// set.
func TestCopiedPackageFilesMatchTheCopy(t *testing.T) {
	copyExampleApp(t, "shelfie")
	app, err := findApp()
	if err != nil {
		t.Fatal(err)
	}
	lib, err := resolveLibrary(t.Context(), app.dir)
	if err != nil {
		t.Skipf("no library source: %v", err)
	}
	m, _ := lookupEjectable("flags")
	plan, err := planEjectFrom(app, m, lib, lockFile{}, false, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for _, f := range plan.Result.(ejectResult).Files {
		want = append(want, strings.TrimPrefix(f, m.dir()+"/"))
	}
	files, err := copiedPackageFiles(filepath.Join(lib.Dir, m.pkg))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for f := range files {
		got = append(got, f)
	}
	slices.Sort(want)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("copiedPackageFiles = %q, the copy writes %q", got, want)
	}
}

func TestChangelogSections(t *testing.T) {
	changelog := `# Changelog

## v0.2.2 (2026-09-21)

### Fixed

- authhttp: sessions end when a password changes.
- opshttp: something else.

## v0.2.1 (2026-10-01)

- Sign-in (` + "`authhttp`" + `) refuses expired passkeys.

## v0.2.0 (2026-09-20)

- authhttp: the release the copy was made from.
`
	got := changelogSections(changelog, "authhttp", "v0.2.0")
	if len(got) != 2 {
		t.Fatalf("changelogSections() = %+v, want two releases", got)
	}
	if got[0].Version != "v0.2.2" || got[0].Date != "2026-09-21" || len(got[0].Entries) != 1 {
		t.Errorf("newest release = %+v", got[0])
	}
	if got[1].Version != "v0.2.1" || len(got[1].Entries) != 1 {
		t.Errorf("older release = %+v", got[1])
	}
	if got := changelogSections(changelog, "authhttp", "v0.2.2"); len(got) != 0 {
		t.Errorf("changelogSections() at the newest version = %+v", got)
	}
}

// TestDoctorSecurityRuns is the command end to end in an app orb wrote,
// with the go commands answered: the report says what it checked and what
// it did not, whatever it found.
func TestDoctorSecurityRuns(t *testing.T) {
	copyExampleApp(t, "shelfie")
	fakeDoctorCommands(t, `{"current":1,"latest":1,"pending":0}`)
	code, out, errOut := runOrb(t, "doctor", "--security")
	if code != 0 && code != 1 {
		t.Fatalf("orb doctor --security = %d; stderr %s", code, errOut)
	}
	for _, want := range []string{"What this checked", "What this did not check", "no advisory feed", "credential-stored-unhashed"} {
		if !strings.Contains(out, want) {
			t.Errorf("the report never says %q:\n%s", want, out)
		}
	}

	code, out, errOut = runOrb(t, "doctor", "--security", "--json")
	if code != 0 && code != 1 {
		t.Fatalf("orb doctor --security --json = %d; stderr %s", code, errOut)
	}
	var res securityResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("--json output %q: %v", out, err)
	}
	if len(res.Rules) != len(securityRules) || len(res.NotChecked) == 0 || len(res.Checked) == 0 {
		t.Errorf("--json result = %+v", res)
	}
	if len(res.Copies) != 2 {
		t.Errorf("--json reports %d copied modules, want auth and orgs", len(res.Copies))
	}
	for _, c := range res.Copies {
		if len(c.Undetermined) == 0 {
			t.Errorf("%s says nothing about what it couldn't determine: %+v", c.Module, c)
		}
	}
}

// TestSecurityWithoutALock is an app that owns no copied module: the
// review still runs, and still ends by saying what it did not check.
func TestSecurityWithoutALock(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/app\n\ngo 1.26.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	var out strings.Builder
	app, err := findApp()
	if err != nil {
		t.Fatal(err)
	}
	if err := runDoctorSecurity(context.Background(), app, false, &out); err != nil {
		t.Fatalf("orb doctor --security in an empty app: %v", err)
	}
	if !strings.Contains(out.String(), "no copied modules") || !strings.Contains(out.String(), "What this did not check") {
		t.Errorf("report:\n%s", out.String())
	}
}
