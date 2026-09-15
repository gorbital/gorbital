package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fakeDoctorCommands answers the programs aps doctor runs without Docker, a
// build or a database: go env and git run for real, docker fails, the API
// export writes the api/ files as they are when the fake is installed (what
// the code generates), and migrate --status answers status.
func fakeDoctorCommands(t *testing.T, status string) *[]string {
	t.Helper()
	generated := map[string][]byte{}
	for _, f := range []string{"openapi.json", "postman_collection.json", "llms.txt"} {
		generated[f], _ = os.ReadFile(filepath.Join("api", f))
	}
	var calls []string
	saved := doctorCommand
	doctorCommand = func(ctx context.Context, dir string, env []string, name string, args ...string) (string, string, error) {
		call := name + " " + strings.Join(args, " ")
		calls = append(calls, call)
		switch {
		case name == "docker":
			return "", "", errors.New("docker isn't running")
		case strings.HasPrefix(call, "go run ./cmd/api openapi --dir "):
			for f, data := range generated {
				if err := os.WriteFile(filepath.Join(args[len(args)-1], f), data, 0o644); err != nil {
					return "", "", err
				}
			}
			return "", "", nil
		case strings.HasPrefix(call, "go run ./cmd/migrate --status"):
			return status, "", nil
		}
		return saved(ctx, dir, env, name, args...)
	}
	t.Cleanup(func() { doctorCommand = saved })
	return &calls
}

func doctorRun(t *testing.T, wantCode int, args ...string) doctorResult {
	t.Helper()
	code, out, errOut := runAps(t, append([]string{"doctor", "--json"}, args...)...)
	if code != wantCode {
		t.Fatalf("aps doctor %v = %d, want %d; stdout %s stderr %s", args, code, wantCode, out, errOut)
	}
	var res doctorResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("aps doctor --json output %q: %v", out, err)
	}
	return res
}

func check(res doctorResult, name string) doctorCheck {
	for _, c := range res.Checks {
		if c.Name == name && c.Status != doctorOK {
			return c
		}
	}
	for _, c := range res.Checks {
		if c.Name == name {
			return c
		}
	}
	return doctorCheck{}
}

func TestDoctorOnANewApp(t *testing.T) {
	newGitApp(t, "--preset", "full")
	writeFile(t, ".env", readFile(t, ".env.example"))
	calls := fakeDoctorCommands(t, `{"current":9,"latest":9,"pending":0}`)

	res := doctorRun(t, 0)
	if res.Preset != "full" || res.Failures != 0 {
		t.Fatalf("result = %+v, want no failures", res)
	}
	for name, status := range map[string]string{
		"go": doctorOK, "git": doctorOK, "apistock.yaml": doctorOK, "docker": doctorWarn, "apistock.lock": doctorOK,
		"anchors": doctorOK, ".env": doctorOK, "api files": doctorOK, "database": doctorOK,
	} {
		if c := check(res, name); c.Status != status {
			t.Errorf("%s = %+v, want %s", name, c, status)
		}
	}
	if c := check(res, "apistock.lock"); !strings.Contains(c.Detail, "0 of") {
		t.Errorf("lock detail = %q, want no edited files", c.Detail)
	}

	*calls = nil
	fast := doctorRun(t, 0, "--fast")
	if slices.ContainsFunc(*calls, func(c string) bool { return strings.HasPrefix(c, "go run") }) || check(fast, "database").Name != "" {
		t.Errorf("--fast ran %v and reported %+v; want no builds", *calls, fast.Checks)
	}
}

func TestDoctorFindsProblems(t *testing.T) {
	newGitApp(t, "--preset", "full")
	fakeDoctorCommands(t, `{"current":9,"latest":11,"pending":2}`)
	// Three tracked files edited: .gitignore, modules.go and llms.txt.
	writeFile(t, ".gitignore", "")
	writeFile(t, ".env", "RESEND_API_KEY=re_live_secret\n")
	modules := readFile(t, "internal/app/modules.go")
	writeFile(t, "internal/app/modules.go", strings.Replace(modules, "//aps:anchor modules", "", 1))
	writeFile(t, "api/llms.txt", "stale\n")

	code, out, _ := runAps(t, "doctor")
	if code != 1 || !strings.Contains(out, "fix:") {
		t.Errorf("aps doctor with problems = %d:\n%s", code, out)
	}
	if strings.Contains(out, "re_live_secret") {
		t.Error("aps doctor printed a secret from .env")
	}

	res := doctorRun(t, 1)
	for name, want := range map[string]string{
		"anchor":        "internal/app/modules.go",
		".env":          "holds secrets",
		"api files":     "api/llms.txt",
		"database":      "2 migrations pending",
		"apistock.lock": "3 of",
	} {
		c := check(res, name)
		if !strings.Contains(c.Detail, want) {
			t.Errorf("%s = %+v, want detail containing %q", name, c, want)
		}
	}
	if c := check(res, "anchor"); c.Status != doctorFail || c.Fix == "" {
		t.Errorf("anchor check = %+v, want a failure with a fix", c)
	}

	fakeDoctorCommands(t, `{"config_error":"invalid configuration: APP_ENV must be development, test or production"}`)
	if c := check(doctorRun(t, 1), "configuration"); c.Status != doctorFail || !strings.Contains(c.Detail, "APP_ENV") {
		t.Errorf("configuration check = %+v", c)
	}
	fakeDoctorCommands(t, `{"current":12,"latest":11}`)
	if c := check(doctorRun(t, 1), "database"); c.Status != doctorFail || !strings.Contains(c.Detail, "newest file") {
		t.Errorf("database ahead of the code = %+v", c)
	}
}

func TestDoctorOnAMinimalApp(t *testing.T) {
	newGitApp(t, "--preset", "minimal")
	fakeDoctorCommands(t, "")
	res := doctorRun(t, 0)
	if res.Preset != "minimal" || check(res, "docker").Name != "" || check(res, "database").Name != "" || check(res, "api files").Status != doctorOK {
		t.Errorf("minimal app checks = %+v, want no Docker or database checks", res.Checks)
	}
}

func TestVersionAtLeast(t *testing.T) {
	for _, tt := range []struct {
		have, want string
		ok         bool
	}{
		{"1.26.0", "1.26.0", true}, {"1.26.1", "1.26.0", true}, {"1.27", "1.26.0", true},
		{"1.25.9", "1.26.0", false}, {"1.26rc1", "1.26.0", false}, {"1.26rc1", "1.25.0", true}, {"1.26.0", "1.26", true},
	} {
		if got := versionAtLeast(tt.have, tt.want); got != tt.ok {
			t.Errorf("versionAtLeast(%s, %s) = %v, want %v", tt.have, tt.want, got, tt.ok)
		}
	}
}
