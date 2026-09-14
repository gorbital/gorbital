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
	code := Main(context.Background(), args, &stdout, &stderr)
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
		{"future preset", []string{"new", "api", "--preset", "full"}, 2, "arrives in v0.2"},
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
	t.Chdir(t.TempDir())
	code, out, errOut := runAps(t, "new", "shop-api", "--module", "example.com/shop-api", "--skip-tidy", "--no-git", "--json")
	if code != 0 {
		t.Fatalf("aps new = %d, stderr %q", code, errOut)
	}

	var res newResult
	if err := json.Unmarshal([]byte(out), &res); err != nil || res.Name != "shop-api" || res.Files < 15 {
		t.Errorf("aps new --json = %q (%v), want result with files", out, err)
	}

	goMod, _ := os.ReadFile(filepath.Join("shop-api", "go.mod"))
	if !strings.HasPrefix(string(goMod), "module example.com/shop-api\n") {
		t.Errorf("go.mod = %q, want module example.com/shop-api", goMod)
	}

	var lock lockFile
	lockBytes, _ := os.ReadFile(filepath.Join("shop-api", "apistock.lock"))
	if err := json.Unmarshal(lockBytes, &lock); err != nil || lock.APIVersion != LockAPIVersion || len(lock.Recipes) != 1 || len(lock.Recipes[0].Operations) != res.Files {
		t.Errorf("apistock.lock = %s (%v), want %d createFile operations", lockBytes, err, res.Files)
	}

	_ = filepath.WalkDir("shop-api", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, _ := os.ReadFile(p)
		if bytes.Contains(b, []byte("acme-api")) || bytes.Contains(b, []byte("⟦")) {
			t.Errorf("%s still contains placeholder or template syntax", p)
		}
		return nil
	})
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
	before, _ := snapshot(dir)

	write(".aps/api", "binary")
	write("notes.txt", "ignored")
	if after, _ := snapshot(dir); after != before {
		t.Error("snapshot changed after writing ignored files")
	}

	later := time.Now().Add(time.Second)
	write("main.go", "package main // edited")
	_ = os.Chtimes(filepath.Join(dir, "main.go"), later, later)
	if after, _ := snapshot(dir); after == before {
		t.Error("snapshot unchanged after editing main.go")
	}
}

// TestNewAppBuildsAndPassesItsTests creates an app against this checkout,
// then builds it and runs its own test suite. Set APS_E2E=1 to run it.
func TestNewAppBuildsAndPassesItsTests(t *testing.T) {
	if os.Getenv("APS_E2E") == "" {
		t.Skip("set APS_E2E=1 to run the end-to-end test")
	}
	repo, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())

	if code, _, errOut := runAps(t, "new", "e2e-api", "--local", repo, "--no-git"); code != 0 {
		t.Fatalf("aps new --local = %d: %s", code, errOut)
	}
	for _, args := range [][]string{{"vet", "./..."}, {"test", "./..."}} {
		cmd := exec.Command("go", args...)
		cmd.Dir = "e2e-api"
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go %s in generated app failed: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
}
