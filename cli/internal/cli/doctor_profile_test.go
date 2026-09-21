package cli

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"gorbital.dev/cli/internal/recipes"
)

// TestDemoMigrationIsAfterTheBuiltins: the demonstration module's fixed
// migration version is the first one after the built-in modules', which
// run in the same history. A new built-in migration moves it.
func TestDemoMigrationIsAfterTheBuiltins(t *testing.T) {
	if want := strconv.FormatInt(latestBuiltinMigration+1, 10); recipes.DemoMigrationVersion != want {
		t.Errorf("recipes.DemoMigrationVersion = %s, want %s: a migration before the built-in ones is refused on a database that ran them",
			recipes.DemoMigrationVersion, want)
	}
}

// TestDoctorChecksTheProfile: orb doctor reports a file the app's profile
// says it should have and hasn't, and wiring that doesn't match what
// gorbital.yaml says the app is (ADR-0090 §8).
func TestDoctorChecksTheProfile(t *testing.T) {
	dir := newApp(t, "--auth", "full", "--scope", "organisation")

	out := doctorOutput(t, dir)
	for _, want := range []string{"profile files", "profile wiring"} {
		if !strings.Contains(out, want) {
			t.Errorf("orb doctor doesn't check the %s:\n%s", want, out)
		}
	}
	if strings.Contains(out, "warn  profile") {
		t.Errorf("orb doctor warns about a freshly created app's profile:\n%s", out)
	}

	// A file the profile asks for, gone.
	if err := os.Remove(filepath.Join(dir, "AUTH_PROVIDERS.md")); err != nil {
		t.Fatal(err)
	}
	if out := doctorOutput(t, dir); !strings.Contains(out, "AUTH_PROVIDERS.md") {
		t.Errorf("orb doctor doesn't report the missing file:\n%s", out)
	}

	// main.go that doesn't build what gorbital.yaml describes.
	main := filepath.Join(dir, "cmd", "api", "main.go")
	source, err := os.ReadFile(main)
	if err != nil {
		t.Fatal(err)
	}
	without := strings.Replace(string(source), "gorbital.WithModules(orgshttp.Module(auth)),", "", 1)
	if without == string(source) {
		t.Fatal("main.go doesn't mount orgshttp")
	}
	if err := os.WriteFile(main, []byte(without), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := doctorOutput(t, dir); !strings.Contains(out, "doesn't mount orgshttp.Module") {
		t.Errorf("orb doctor doesn't report the missing tenancy:\n%s", out)
	}
}

// doctorOutput runs orb doctor --fast in dir and returns what it printed.
func doctorOutput(t *testing.T, dir string) string {
	t.Helper()
	t.Chdir(dir)
	_, out, errOut := runOrb(t, "doctor", "--fast")
	return out + errOut
}

// newApp creates an app with the flags and returns its directory.
func newApp(t *testing.T, flags ...string) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	args := append([]string{"new", "shop-api", "--module", "example.com/shop-api", "--local", repoAbs(t), "--skip-tidy", "--no-git"}, flags...)
	if code, _, errOut := runOrb(t, args...); code != 0 {
		t.Fatalf("orb new %v = %d: %s", flags, code, errOut)
	}
	return filepath.Join(dir, "shop-api")
}
