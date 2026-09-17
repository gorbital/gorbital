package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
	"time"
)

func runOrb(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	// A non-terminal stdin: commands never prompt in tests.
	code := Main(context.Background(), args, strings.NewReader(""), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestMainCommands(t *testing.T) {
	if code, _, _ := runOrb(t); code != 2 {
		t.Errorf("orb (no args) exit = %d, want 2", code)
	}
	if code, out, _ := runOrb(t, "version"); code != 0 || !strings.HasPrefix(out, "orb ") {
		t.Errorf("orb version = %d %q, want 0 and version line", code, out)
	}
	if code, out, _ := runOrb(t, "help"); code != 0 || !strings.Contains(out, "orb new") {
		t.Errorf("orb help = %d %q, want usage", code, out)
	}
	if code, _, errOut := runOrb(t, "deploy"); code != 2 || !strings.Contains(errOut, "unknown command") {
		t.Errorf("orb deploy = %d %q, want 2 unknown command", code, errOut)
	}
}

func TestNewValidation(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.Mkdir("taken", 0o755); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		args     []string
		wantCode int
		wantErr  string
	}{
		{"missing name", []string{"new"}, 2, "missing app name"},
		{"uppercase", []string{"new", "My-API"}, 2, "invalid app name"},
		{"path traversal", []string{"new", "../escape"}, 2, "invalid app name"},
		{"template injection", []string{"new", "x{{.Module}}"}, 2, "invalid app name"},
		{"double hyphen", []string{"new", "my--api"}, 2, "invalid app name"},
		{"bad module", []string{"new", "api", "--module", "example.com/../x"}, 2, "invalid module path"},
		{"custom preset", []string{"new", "api", "--preset", "custom"}, 2, "isn't available yet"},
		{"unknown preset", []string{"new", "api", "--preset", "huge"}, 2, "unknown preset"},
		{"unknown tenancy", []string{"new", "api", "--preset", "full", "--tenancy", "many"}, 2, "unknown tenancy"},
		{"multi-tenant minimal", []string{"new", "api", "--tenancy", "multi"}, 2, "needs the Full preset"},
		{"bad local", []string{"new", "api", "--local", "."}, 2, "not an gorbital checkout"},
		{"existing directory", []string{"new", "taken", "--skip-tidy", "--no-git"}, 1, "already exists"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, _, errOut := runOrb(t, tt.args...)
			if code != tt.wantCode || !strings.Contains(errOut, tt.wantErr) {
				t.Errorf("orb %s = %d %q, want %d containing %q", strings.Join(tt.args, " "), code, errOut, tt.wantCode, tt.wantErr)
			}
		})
	}
	entries, _ := os.ReadDir(".")
	if len(entries) != 1 {
		t.Errorf("failed commands left %d entries in the directory, want only 'taken'", len(entries))
	}
}

func TestNewCreatesApp(t *testing.T) {
	for _, tt := range []struct {
		preset   string
		tenancy  string
		recipe   string
		minFiles int
		// contains maps a created file to text it must contain.
		contains map[string]string
	}{
		{"minimal", "single", "base-minimal", 15, map[string]string{
			"go.mod":        "module example.com/shop-api\n",
			"gorbital.yaml": "preset: minimal",
		}},
		{"full", "multi", "base-full-multi", 150, map[string]string{
			"gorbital.yaml":                             "tenancy: multi",
			"internal/app/app.go":                       `const ServiceName = "shop-api"`,
			"internal/modules/orgs/module.go":           "package orgs",
			"db/migrations/20260916000002_projects.sql": "org_id      text        NOT NULL REFERENCES orgs (id)",
		}},
		{"full", "single", "base-full", 100, map[string]string{
			"go.mod":                              "module example.com/shop-api\n",
			"gorbital.yaml":                       "preset: full",
			"compose.yaml":                        "POSTGRES_DB: shop-api",
			".env.example":                        "DATABASE_URL=postgres://shop-api:shop-api@127.0.0.1:5432/shop-api",
			"internal/app/app.go":                 `const ServiceName = "shop-api"`,
			"internal/modules/projects/module.go": "package projects",
			"db/migrations/migrations.go":         "package migrations",
		}},
	} {
		t.Run(tt.recipe, func(t *testing.T) {
			t.Chdir(t.TempDir())
			code, out, errOut := runOrb(t, "new", "shop-api", "--module", "example.com/shop-api", "--preset", tt.preset, "--tenancy", tt.tenancy, "--skip-tidy", "--no-git", "--json")
			if code != 0 {
				t.Fatalf("orb new --preset %s --tenancy %s = %d, stderr %q", tt.preset, tt.tenancy, code, errOut)
			}
			var res newResult
			if err := json.Unmarshal([]byte(out), &res); err != nil || res.Name != "shop-api" || res.Preset != tt.preset || res.Tenancy != tt.tenancy || res.Files < tt.minFiles {
				t.Errorf("orb new --json = %q (%v), want the %s preset, %s tenancy, with at least %d files", out, err, tt.preset, tt.tenancy, tt.minFiles)
			}
			for path, want := range tt.contains {
				if got, _ := os.ReadFile(filepath.Join("shop-api", filepath.FromSlash(path))); !strings.Contains(string(got), want) {
					t.Errorf("%s lacks %q:\n%s", path, want, got)
				}
			}

			wantInputs := lockInputs{Name: "shop-api", Module: "example.com/shop-api", Preset: tt.preset, Tenancy: tt.tenancy}
			if tt.preset == "full" {
				wantInputs.Mail = "resend"
			}
			lock, err := readLock("shop-api")
			// Every rendered file is tracked except go.mod.
			if err != nil || lock.APIVersion != LockAPIVersion || lock.Orb.Version != Version || lock.Inputs != wantInputs ||
				len(lock.Files) != res.Files-1 || lock.tracks("go.mod") {
				t.Errorf("gorbital.lock = %+v (%v), want %s with inputs %+v and %d files", lock, err, LockAPIVersion, wantInputs, res.Files-1)
			}
			for _, f := range lock.Files {
				if got, _ := os.ReadFile(filepath.Join("shop-api", filepath.FromSlash(f.Path))); sha256Hex(got) != f.SHA256 {
					t.Errorf("gorbital.lock hash of %s doesn't match the file", f.Path)
				}
			}
			assertLockRebuilds(t, "shop-api")

			_ = filepath.WalkDir("shop-api", func(p string, d fs.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return err
				}
				b, _ := os.ReadFile(p)
				for _, leak := range []string{"acme-api", "⟦", "../../"} {
					if bytes.Contains(b, []byte(leak)) {
						t.Errorf("%s contains %q from the golden app or its templates", p, leak)
					}
				}
				return nil
			})
		})
	}
}

func TestNewFullPrintsNextSteps(t *testing.T) {
	t.Chdir(t.TempDir())
	code, out, errOut := runOrb(t, "new", "shop-api", "--preset", "full", "--skip-tidy", "--no-git")
	if code != 0 {
		t.Fatalf("orb new --preset full = %d, stderr %q", code, errOut)
	}
	for _, want := range []string{
		"creating shop-api in ./shop-api\n", "preset full · tenancy single · library", "✓ wrote ", "\ncreated shop-api\n",
		"docker compose up -d --wait", "go run ./cmd/migrate", "go run ./cmd/seed", "http://127.0.0.1:8025", "admin@example.com",
		"AUTH_PROVIDERS.md", "POSTGRES_PORT", "orb add mail", "next: cd shop-api\n        orb dev\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("orb new --preset full output lacks %q:\n%s", want, out)
		}
	}
	// --skip-tidy and --no-git skip their steps; output that isn't a terminal has no colour.
	for _, unwanted := range []string{"go mod tidy", "initialised git", "\x1b[", "orgs.invitation_url"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("orb new --skip-tidy --no-git output has %q:\n%s", unwanted, out)
		}
	}

	code, out, errOut = runOrb(t, "new", "team-api", "--preset", "full", "--tenancy", "multi", "--skip-tidy", "--no-git")
	if code != 0 {
		t.Fatalf("orb new --preset full --tenancy multi = %d, stderr %q", code, errOut)
	}
	for _, want := range []string{"preset full · tenancy multi · library", "personal workspace", "orgs.invitation_url", "next: cd team-api"} {
		if !strings.Contains(out, want) {
			t.Errorf("orb new --tenancy multi output lacks %q:\n%s", want, out)
		}
	}
}

func TestNewMinimalLog(t *testing.T) {
	t.Chdir(t.TempDir())
	code, out, errOut := runOrb(t, "new", "shop-api", "--skip-tidy", "--no-git")
	if code != 0 {
		t.Fatalf("orb new = %d, stderr %q", code, errOut)
	}
	for _, want := range []string{"creating shop-api in ./shop-api\n", "preset minimal · library", "✓ wrote ", "api docs     http://127.0.0.1:8080/docs", "next: cd shop-api"} {
		if !strings.Contains(out, want) {
			t.Errorf("orb new output lacks %q:\n%s", want, out)
		}
	}
	if code, out, _ := runOrb(t, "new", "other-api", "--skip-tidy", "--no-git", "--json"); code != 0 || strings.Contains(out, "creating") {
		t.Errorf("orb new --json = %d, printed the log:\n%s", code, out)
	}
}

func TestParseDotEnv(t *testing.T) {
	src := `# comment
APP_ENV=development
export APP_ADDR=127.0.0.1:9090
QUOTED="hello # not a comment"
SINGLE='x=y'
TRAILING=value # comment
EMPTY=
`
	got, err := parseDotEnv(strings.NewReader(src))
	if err != nil {
		t.Fatalf("parseDotEnv() error = %v", err)
	}
	want := map[string]string{
		"APP_ENV": "development", "APP_ADDR": "127.0.0.1:9090", "QUOTED": "hello # not a comment",
		"SINGLE": "x=y", "TRAILING": "value", "EMPTY": "",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("parseDotEnv()[%s] = %q, want %q", k, got[k], v)
		}
	}
	if _, err := parseDotEnv(strings.NewReader("not a pair\n")); err == nil {
		t.Error("parseDotEnv(invalid line) error = nil, want error")
	}
}

func TestSnapshotDetectsChanges(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) {
		p := filepath.Join(dir, name)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("main.go", "package main")
	before, _ := snapshot(dir, watched)

	write(".orb/api", "binary")
	write("notes.txt", "ignored")
	if after, _ := snapshot(dir, watched); after != before {
		t.Error("snapshot changed after writing ignored files")
	}

	later := time.Now().Add(time.Second)
	write("main.go", "package main // edited")
	_ = os.Chtimes(filepath.Join(dir, "main.go"), later, later)
	if after, _ := snapshot(dir, watched); after == before {
		t.Error("snapshot unchanged after editing main.go")
	}
}

// TestNewAppBuildsAndPassesItsTests creates an app of each preset against
// this checkout, then vets it and runs its own test suite. A Full app's
// database tests run when GORBITAL_TEST_DATABASE_URL is set; afterwards the
// generators users run next must leave it building and passing its tests,
// including a generated migration that changes a generated resource's table.
// Set ORB_E2E=1 to run it.
func TestNewAppBuildsAndPassesItsTests(t *testing.T) {
	if os.Getenv("ORB_E2E") == "" {
		t.Skip("set ORB_E2E=1 to run the end-to-end test")
	}
	repo, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, app := range []struct{ preset, tenancy string }{{"minimal", "single"}, {"full", "single"}, {"full", "multi"}} {
		t.Run(app.preset+"-"+app.tenancy, func(t *testing.T) {
			t.Chdir(t.TempDir())
			name := "e2e-" + app.preset + "-" + app.tenancy
			if code, _, errOut := runOrb(t, "new", name, "--preset", app.preset, "--tenancy", app.tenancy, "--local", repo, "--no-git"); code != 0 {
				t.Fatalf("orb new --preset %s --tenancy %s --local = %d: %s", app.preset, app.tenancy, code, errOut)
			}
			goIn(t, name, "vet", "./...")
			goIn(t, name, "test", "./...")
			if app.preset != "full" {
				return
			}
			t.Chdir(name)
			// In a multi-tenant app the resource belongs to organisations (--scope org by default).
			generators := [][]string{
				{"gen", "resource", "Customer", "email:string:unique", "notes:text", "tier:enum(free,pro)", "--yes"},
				{"gen", "job", "SendDigest", "--every", "1h", "--yes"},
				{"gen", "migration", "add_phone", "--yes"},
			}
			table := "customers"
			for _, args := range generators {
				if code, _, errOut := runOrb(t, args...); code != 0 {
					t.Fatalf("orb %s in a new Full app = %d: %s", strings.Join(args, " "), code, errOut)
				}
			}
			// The migration runs after the table's, so it can change it.
			added, _ := filepath.Glob(filepath.Join("db", "migrations", "*_add_phone.sql"))
			if len(added) != 1 {
				t.Fatalf("orb gen migration wrote %v, want one add_phone migration", added)
			}
			sql := readFile(t, added[0]) + "ALTER TABLE " + table + " ADD COLUMN phone text NOT NULL DEFAULT '';\n"
			if err := os.WriteFile(added[0], []byte(sql), 0o644); err != nil {
				t.Fatal(err)
			}
			// As orb gen resource's next steps say: the new endpoints change the
			// spec, and the new error codes, audit actions, permissions and job
			// are recorded in api/surface.json.
			if out, err := exec.Command("go", "run", "./cmd/api", "openapi", "--dir", "api").CombinedOutput(); err != nil {
				t.Fatalf("go run ./cmd/api openapi --dir api: %v\n%s", err, out)
			}
			if out, err := exec.Command("go", "test", "./internal/app", "-run", "TestPublicSurface", "-update").CombinedOutput(); err != nil {
				t.Fatalf("go test ./internal/app -run TestPublicSurface -update: %v\n%s", err, out)
			}
			goIn(t, ".", "vet", "./...")
			goIn(t, ".", "test", "./...")
		})
	}
}

func goIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go %s in %s failed: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
}

func TestModuleVersion(t *testing.T) {
	for _, tt := range []struct {
		name string
		info *debug.BuildInfo
		want string
	}{
		{"go install at a release", &debug.BuildInfo{Main: debug.Module{Path: "gorbital.dev/cli", Version: "v0.1.0"}}, "v0.1.0"},
		{"go install at a pre-release", &debug.BuildInfo{Main: debug.Module{Path: "gorbital.dev/cli", Version: "v0.2.0-rc.1"}}, "v0.2.0-rc.1"},
		{"build from a checkout", &debug.BuildInfo{Main: debug.Module{Path: "gorbital.dev/cli", Version: "(devel)"}}, "dev"},
		{"pseudo-version", &debug.BuildInfo{Main: debug.Module{Path: "gorbital.dev/cli", Version: "v0.1.1-0.20260917120000-0123456789ab"}}, "dev"},
		{"modified tree", &debug.BuildInfo{Main: debug.Module{Path: "gorbital.dev/cli", Version: "v0.1.1-0.20260917120000-0123456789ab+dirty"}}, "dev"},
		{"another main module", &debug.BuildInfo{Main: debug.Module{Path: "example.com/tool", Version: "v1.0.0"}}, "dev"},
		{"no build info", nil, "dev"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := moduleVersion("dev", func() (*debug.BuildInfo, bool) { return tt.info, tt.info != nil })
			if got != tt.want {
				t.Errorf("moduleVersion() = %q, want %q", got, tt.want)
			}
		})
	}
}
