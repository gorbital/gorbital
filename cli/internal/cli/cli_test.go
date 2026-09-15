package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func runAps(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	// A non-terminal stdin: commands never prompt in tests.
	code := Main(context.Background(), args, strings.NewReader(""), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestMainCommands(t *testing.T) {
	if code, _, _ := runAps(t); code != 2 {
		t.Errorf("aps (no args) exit = %d, want 2", code)
	}
	if code, out, _ := runAps(t, "version"); code != 0 || !strings.HasPrefix(out, "aps ") {
		t.Errorf("aps version = %d %q, want 0 and version line", code, out)
	}
	if code, out, _ := runAps(t, "help"); code != 0 || !strings.Contains(out, "aps new") {
		t.Errorf("aps help = %d %q, want usage", code, out)
	}
	if code, _, errOut := runAps(t, "deploy"); code != 2 || !strings.Contains(errOut, "unknown command") {
		t.Errorf("aps deploy = %d %q, want 2 unknown command", code, errOut)
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
		{"bad local", []string{"new", "api", "--local", "."}, 2, "not an apistock checkout"},
		{"existing directory", []string{"new", "taken", "--skip-tidy", "--no-git"}, 1, "already exists"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, _, errOut := runAps(t, tt.args...)
			if code != tt.wantCode || !strings.Contains(errOut, tt.wantErr) {
				t.Errorf("aps %s = %d %q, want %d containing %q", strings.Join(tt.args, " "), code, errOut, tt.wantCode, tt.wantErr)
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
		recipe   string
		minFiles int
		// contains maps a created file to text it must contain.
		contains map[string]string
	}{
		{"minimal", "base-minimal", 15, map[string]string{
			"go.mod":        "module example.com/shop-api\n",
			"apistock.yaml": "preset: minimal",
		}},
		{"full", "base-full", 100, map[string]string{
			"go.mod":                              "module example.com/shop-api\n",
			"apistock.yaml":                       "preset: full",
			"compose.yaml":                        "POSTGRES_DB: shop-api",
			".env.example":                        "DATABASE_URL=postgres://shop-api:shop-api@127.0.0.1:5432/shop-api",
			"internal/app/app.go":                 `const ServiceName = "shop-api"`,
			"internal/modules/projects/module.go": "package projects",
			"db/migrations/migrations.go":         "package migrations",
		}},
	} {
		t.Run(tt.preset, func(t *testing.T) {
			t.Chdir(t.TempDir())
			code, out, errOut := runAps(t, "new", "shop-api", "--module", "example.com/shop-api", "--preset", tt.preset, "--skip-tidy", "--no-git", "--json")
			if code != 0 {
				t.Fatalf("aps new --preset %s = %d, stderr %q", tt.preset, code, errOut)
			}
			var res newResult
			if err := json.Unmarshal([]byte(out), &res); err != nil || res.Name != "shop-api" || res.Preset != tt.preset || res.Files < tt.minFiles {
				t.Errorf("aps new --json = %q (%v), want the %s preset with at least %d files", out, err, tt.preset, tt.minFiles)
			}
			for path, want := range tt.contains {
				if got, _ := os.ReadFile(filepath.Join("shop-api", filepath.FromSlash(path))); !strings.Contains(string(got), want) {
					t.Errorf("%s lacks %q:\n%s", path, want, got)
				}
			}

			var lock lockFile
			lockBytes, _ := os.ReadFile(filepath.Join("shop-api", "apistock.lock"))
			if err := json.Unmarshal(lockBytes, &lock); err != nil || lock.APIVersion != LockAPIVersion || len(lock.Recipes) != 1 ||
				lock.Recipes[0].Name != tt.recipe || len(lock.Recipes[0].Operations) != res.Files {
				t.Errorf("apistock.lock = %s (%v), want recipe %s with %d createFile operations", lockBytes, err, tt.recipe, res.Files)
			}

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
	code, out, errOut := runAps(t, "new", "shop-api", "--preset", "full", "--skip-tidy", "--no-git")
	if code != 0 {
		t.Fatalf("aps new --preset full = %d, stderr %q", code, errOut)
	}
	for _, want := range []string{"full preset", "aps dev", "docker compose up -d --wait", "go run ./cmd/migrate", "go run ./cmd/seed", "http://127.0.0.1:8025", "admin@example.com", "POSTGRES_PORT", "aps add mail"} {
		if !strings.Contains(out, want) {
			t.Errorf("aps new --preset full output lacks %q:\n%s", want, out)
		}
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

	write(".aps/api", "binary")
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
// database tests run when APISTOCK_TEST_DATABASE_URL is set; afterwards the
// generators users run next must leave it building and passing its tests,
// including a generated migration that changes a generated resource's table.
// Set APS_E2E=1 to run it.
func TestNewAppBuildsAndPassesItsTests(t *testing.T) {
	if os.Getenv("APS_E2E") == "" {
		t.Skip("set APS_E2E=1 to run the end-to-end test")
	}
	repo, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, preset := range []string{"minimal", "full"} {
		t.Run(preset, func(t *testing.T) {
			t.Chdir(t.TempDir())
			name := "e2e-" + preset
			if code, _, errOut := runAps(t, "new", name, "--preset", preset, "--local", repo, "--no-git"); code != 0 {
				t.Fatalf("aps new --preset %s --local = %d: %s", preset, code, errOut)
			}
			goIn(t, name, "vet", "./...")
			goIn(t, name, "test", "./...")
			if preset != "full" {
				return
			}

			t.Chdir(name)
			for _, args := range [][]string{
				{"gen", "resource", "Customer", "email:string:unique", "notes:text", "tier:enum(free,pro)", "--yes"},
				{"gen", "job", "SendDigest", "--every", "1h", "--yes"},
				{"gen", "migration", "add_customer_phone", "--yes"},
			} {
				if code, _, errOut := runAps(t, args...); code != 0 {
					t.Fatalf("aps %s in a new Full app = %d: %s", strings.Join(args, " "), code, errOut)
				}
			}
			// The migration runs after the resource's, so it can change its table.
			added, _ := filepath.Glob(filepath.Join("db", "migrations", "*_add_customer_phone.sql"))
			if len(added) != 1 {
				t.Fatalf("aps gen migration wrote %v, want one add_customer_phone migration", added)
			}
			sql := readFile(t, added[0]) + "ALTER TABLE customers ADD COLUMN phone text NOT NULL DEFAULT '';\n"
			if err := os.WriteFile(added[0], []byte(sql), 0o644); err != nil {
				t.Fatal(err)
			}
			// As aps gen resource's next steps say: the new endpoints change the spec.
			spec, err := exec.Command("go", "run", "./cmd/api", "openapi").Output()
			if err != nil {
				t.Fatalf("go run ./cmd/api openapi: %v", err)
			}
			if err := os.WriteFile(filepath.Join("api", "openapi.json"), spec, 0o644); err != nil {
				t.Fatal(err)
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
