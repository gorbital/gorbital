package cli

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gorbital.dev/cli/internal/recipes"
	"gorbital.dev/cli/internal/recipes/generate"
)

// bashBlock returns the lines of the fenced bash block of markdown that
// holds want, with each line's trailing # comment removed.
func bashBlock(t *testing.T, markdown, want string) []string {
	t.Helper()
	for _, block := range strings.Split(markdown, "```bash\n")[1:] {
		block, _, _ = strings.Cut(block, "```")
		if !strings.Contains(block, want) {
			continue
		}
		var lines []string
		for _, line := range strings.Split(block, "\n") {
			command, _, _ := strings.Cut(line, "#")
			if command = strings.TrimSpace(command); command != "" {
				lines = append(lines, command)
			}
		}
		return lines
	}
	t.Fatalf("no bash block holding %q in:\n%s", want, markdown)
	return nil
}

// TestNewPrintsTheStepsTheREADMEDoes: the "without orb" steps orb new prints
// are the app's own steps, so a Full app started without the CLI reads .env
// into the environment and has an encryption key before seed runs (CLI-12d).
func TestNewPrintsTheStepsTheREADMEDoes(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	code, out, errOut := runOrb(t, "new", "shop-api", "--skip-tidy", "--no-git", "--preset", "full")
	if code != 0 {
		t.Fatalf("orb new --preset full = %d: %s", code, errOut)
	}
	t.Chdir(filepath.Join(dir, "shop-api"))

	readme := readFile(t, "README.md")
	steps := bashBlock(t, readme, "cp .env.example .env")
	if len(steps) < 6 {
		t.Fatalf("README.md lists only %d steps without orb: %v", len(steps), steps)
	}
	for _, step := range steps {
		if !strings.Contains(out, step) {
			t.Errorf("orb new didn't print the README's step %q; it printed:\n%s", step, out)
		}
	}
	if !strings.Contains(out, encryptionKeysVar) {
		t.Errorf("orb new didn't mention %s; it printed:\n%s", encryptionKeysVar, out)
	}
}

// TestNewAppHoldsSignIn: a new Full app holds its sign-in, and a
// multi-tenant one its organisations, as its own code (v0.2.1): the
// modules in internal/modules, their migrations in db/migrations, main.go
// importing the copies and gorbital.lock recording them with the library's
// hash, so orb doctor can say when the library changes. --no-eject keeps
// them in the library.
func TestNewAppHoldsSignIn(t *testing.T) {
	repo, err := filepath.Abs(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		tenancy    string
		noEject    bool
		modules    []string
		migrations []string
	}{
		{"single", false, []string{"auth"}, []string{"20260915000001_auth.sql", "20260918000070_auth_bans.sql"}},
		{"multi", false, []string{"orgs", "auth"}, []string{"20260915000001_auth.sql", "20260916000001_orgs.sql", "20260918000002_settings_org_purge.sql"}},
		{"multi", true, nil, nil},
	} {
		t.Run(fmt.Sprintf("%s no-eject=%v", tt.tenancy, tt.noEject), func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			args := []string{"new", "shop-api", "--module", "example.com/shop-api", "--preset", "full", "--tenancy", tt.tenancy, "--local", repo, "--skip-tidy", "--no-git", "--json"}
			if tt.noEject {
				args = append(args, "--no-eject")
			}
			code, out, errOut := runOrb(t, args...)
			if code != 0 {
				t.Fatalf("orb %v = %d: %s", args, code, errOut)
			}
			var res newResult
			if err := json.Unmarshal([]byte(out), &res); err != nil {
				t.Fatal(err)
			}
			app := filepath.Join(dir, "shop-api")
			main := readFile(t, filepath.Join(app, "cmd", "api", "main.go"))
			lock, err := readLock(app)
			if err != nil {
				t.Fatal(err)
			}
			if len(res.Ejected) != len(tt.modules) || len(lock.Ejected) != len(tt.modules) {
				t.Fatalf("ejected %+v, gorbital.lock %+v; want %v", res.Ejected, lock.Ejected, tt.modules)
			}
			for i, name := range tt.modules {
				m, _ := lookupEjectable(name)
				if res.Ejected[i].Module != name || res.Ejected[i].Files == 0 || res.Ejected[i].Version != recipes.LibraryVersion {
					t.Errorf("ejected[%d] = %+v, want %s first", i, res.Ejected[i], name)
				}
				if _, err := os.Stat(filepath.Join(app, filepath.FromSlash(m.dir()), m.pkg+".go")); err != nil {
					t.Errorf("%s has no root package: %v", m.dir(), err)
				}
				want, err := hashLibraryPackage(filepath.Join(repo, "gorbital", m.pkg))
				if e, ok := lock.ejected(name); !ok || err != nil || e.SHA256 != want || e.Package != m.importPath() {
					t.Errorf("gorbital.lock records %s as %+v (%v), want the hash of %s", name, e, err, m.importPath())
				}
				copyImport := fmt.Sprintf("%s %q", m.pkg, "example.com/shop-api/"+m.dir())
				if !strings.Contains(main, copyImport) || strings.Contains(main, `"`+m.importPath()+`"`) {
					t.Errorf("main.go doesn't import %s instead of the library:\n%s", copyImport, main)
				}
			}
			for _, name := range tt.migrations {
				if _, err := os.Stat(filepath.Join(app, "db", "migrations", name)); err != nil {
					t.Errorf("db/migrations lacks %s: %v", name, err)
				}
			}
			if tt.noEject {
				for _, m := range []string{"auth", "orgs"} {
					if _, err := os.Stat(filepath.Join(app, "internal", "modules", m)); err == nil {
						t.Errorf("--no-eject wrote internal/modules/%s", m)
					}
				}
				for _, imp := range []string{`"gorbital.dev/gorbital/authhttp"`, `"gorbital.dev/gorbital/orgshttp"`} {
					if !strings.Contains(main, imp) {
						t.Errorf("with --no-eject main.go doesn't import %s:\n%s", imp, main)
					}
				}
				if migrations, _ := filepath.Glob(filepath.Join(app, "db", "migrations", "*_auth*.sql")); len(migrations) > 0 {
					t.Errorf("--no-eject copied sign-in's migrations %v", migrations)
				}
			}
		})
	}
}

// TestNewAppIsTheGoldenApp: orb new with the golden apps' name and module
// writes the golden apps in examples/, sign-in and organisations included,
// file for file. api/surface.json is left out: --skip-tidy doesn't record
// it (go generate does, into both). When it fails after a change to a
// library module or to a golden app, run go generate ./internal/recipes/.
func TestNewAppIsTheGoldenApp(t *testing.T) {
	repo, err := filepath.Abs(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, tenancy := range []string{"single", "multi"} {
		t.Run(tenancy, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			if code, _, errOut := runOrb(t, "new", generate.PlaceholderName, "--module", generate.PlaceholderModule, "--preset", "full", "--tenancy", tenancy, "--local", repo, "--skip-tidy", "--no-git"); code != 0 {
				t.Fatalf("orb new = %d: %s", code, errOut)
			}
			app := filepath.Join(dir, generate.PlaceholderName)
			golden := filepath.Join(repo, "examples", "full-"+tenancy)
			tracked, ok, err := generate.GitFiles(t.Context(), golden)
			if err != nil || !ok {
				t.Skipf("examples/full-%s isn't in a git work tree (%v)", tenancy, err)
			}
			skip := map[string]bool{"go.mod": true, "go.sum": true, "gorbital.lock": true, surfacePath: true}
			written := map[string]bool{}
			_ = filepath.WalkDir(app, func(p string, d fs.DirEntry, err error) error {
				if err == nil && !d.IsDir() {
					rel, _ := filepath.Rel(app, p)
					written[filepath.ToSlash(rel)] = true
				}
				return err
			})
			for rel := range tracked {
				if skip[rel] {
					continue
				}
				want := readFile(t, filepath.Join(golden, filepath.FromSlash(rel)))
				if got, err := os.ReadFile(filepath.Join(app, filepath.FromSlash(rel))); err != nil || string(got) != want {
					t.Errorf("orb new wrote %s differently from examples/full-%s (%v); run go generate ./internal/recipes/", rel, tenancy, err)
				}
				delete(written, rel)
			}
			for rel := range written {
				if !skip[rel] {
					t.Errorf("orb new wrote %s, which examples/full-%s lacks; run go generate ./internal/recipes/", rel, tenancy)
				}
			}
		})
	}
}
