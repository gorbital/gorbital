package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestEjectedModulesPass ejects every built-in module from copies of the
// example apps that use them, and proves each app still builds, passes vet,
// gofmt and golangci-lint, exports the same OpenAPI document, satisfies orb
// doctor, refuses a second ejection and, with a test database, passes its
// whole test suite, the ejected modules' tests included (ADR-0083).
func TestEjectedModulesPass(t *testing.T) {
	if testing.Short() {
		t.Skip("ejects modules into copies of the example apps, then builds and tests them")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go is not installed")
	}

	t.Run("shelfie: flags, ops, orgs, auth", func(t *testing.T) {
		dir := copyExampleApp(t, "shelfie")
		// orgs before auth: the library's orgshttp takes sign-in's
		// authenticator.
		for _, module := range []string{"flags", "ops", "orgs", "auth"} {
			ejectAndCheck(t, dir, module)
		}
		checkLintAndTests(t, dir)
	})

	t.Run("invoicing: orgs", func(t *testing.T) {
		dir := copyExampleApp(t, "invoicing")
		ejectAndCheck(t, dir, "orgs")
		checkLintAndTests(t, dir)
	})

	t.Run("admin tool with email events: mailevents", func(t *testing.T) {
		dir := copyExampleApp(t, "admin-tool")
		main := readFile(t, filepath.Join(dir, "cmd", "api", "main.go"))
		main = strings.Replace(main, "\t\"gorbital.dev/gorbital/opshttp\"\n", "\t\"gorbital.dev/gorbital/mailevents\"\n\t\"gorbital.dev/gorbital/opshttp\"\n", 1)
		main = strings.Replace(main, "opshttp.Module(), flagshttp.Module())", "opshttp.Module(), flagshttp.Module(), mailevents.Module())", 1)
		if !strings.Contains(main, "mailevents.Module()") {
			t.Fatal("admin-tool's main.go changed; add mailevents.Module() to it another way")
		}
		writeFile(t, filepath.Join(dir, "cmd", "api", "main.go"), main)
		runInApp(t, dir, "go", "mod", "tidy")
		runInApp(t, dir, "go", "run", "./cmd/api", "openapi", "--dir", "api")
		commitAll(t, "Receive email events")
		ejectAndCheck(t, dir, "mailevents")
		checkLintAndTests(t, dir)
	})
}

// ejectAndCheck ejects module from the committed app in dir and checks what
// needs no database, then commits.
func ejectAndCheck(t *testing.T, dir, module string) {
	t.Helper()
	res, _ := eject(t, 0, module, "--json")
	if !res.Tidied || len(res.Files) == 0 || !strings.Contains(strings.Join(res.Modified, " "), "cmd/api/main.go") {
		t.Fatalf("orb eject %s = %+v", module, res)
	}
	if unformatted := runInApp(t, dir, "gofmt", "-l", "."); unformatted != "" {
		t.Errorf("after ejecting %s, gofmt -l:\n%s", module, unformatted)
	}
	runInApp(t, dir, "go", "build", "./...")
	runInApp(t, dir, "go", "vet", "./...")

	// The API is the library module's.
	exported := t.TempDir()
	runInApp(t, dir, "go", "run", "./cmd/api", "openapi", "--dir", exported)
	for _, f := range []string{"openapi.json", "postman_collection.json", "llms.txt"} {
		if readFile(t, filepath.Join(exported, f)) != readFile(t, filepath.Join(dir, "api", f)) {
			t.Errorf("after ejecting %s, api/%s changed", module, f)
		}
	}

	// orb doctor sees the ejected module and a correct module list.
	code, out, errOut := runOrb(t, "doctor", "--fast", "--json")
	var doctor doctorResult
	if err := json.Unmarshal([]byte(out), &doctor); err != nil || code != 0 {
		t.Fatalf("orb doctor after ejecting %s = %d %s %s", module, code, out, errOut)
	}
	ejected := 0
	for _, c := range doctor.Checks {
		switch c.Name {
		case "ejected":
			ejected++
			if c.Status != doctorOK {
				t.Errorf("after ejecting %s, doctor: %+v", module, c)
			}
		case "modules", "gorbital.lock", "stack":
			if c.Status != doctorOK {
				t.Errorf("after ejecting %s, doctor: %+v", module, c)
			}
		}
	}
	if lock, err := readLock(dir); err != nil || ejected != len(lock.Ejected) {
		t.Errorf("doctor reported %d ejected modules; gorbital.lock %+v, %v", ejected, lock.Ejected, err)
	}

	commitAll(t, "Eject "+module)
	if _, out := eject(t, 1, module, "--dry-run"); !strings.Contains(out, module+" is already ejected") {
		t.Errorf("a second orb eject %s = %q", module, out)
	}
}

// checkLintAndTests runs golangci-lint, when installed, and the app's tests,
// when a test database is configured.
func checkLintAndTests(t *testing.T, dir string) {
	t.Helper()
	if lint, err := exec.LookPath("golangci-lint"); err == nil {
		runInApp(t, dir, lint, "run", "--config", filepath.Join(repoRoot(t), ".golangci.yml"), "./...")
	} else {
		t.Log("golangci-lint isn't installed: ejected code not linted")
	}
	if os.Getenv("GORBITAL_TEST_DATABASE_URL") == "" {
		t.Log("set GORBITAL_TEST_DATABASE_URL to run the app's tests")
		return
	}
	runInApp(t, dir, "go", "test", "-count=1", "./...")
}

func runInApp(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
	return string(out)
}
