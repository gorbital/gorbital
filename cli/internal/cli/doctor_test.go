package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"gorbital.dev/cli/internal/recipes"
)

// fakeDoctorCommands answers the programs orb doctor runs without Docker, a
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
		case strings.HasPrefix(call, "go run ./cmd/migrate --status"), strings.HasPrefix(call, "go run ./cmd/api migrate --status"):
			return status, "", nil
		}
		return saved(ctx, dir, env, name, args...)
	}
	t.Cleanup(func() { doctorCommand = saved })
	return &calls
}

// copyEnvExample writes .env from .env.example with an encryption key in
// it, the way orb dev leaves a Full app: the key is the one value the app
// can't start without that .env.example leaves for orb to fill.
func copyEnvExample(t *testing.T, extra string) {
	t.Helper()
	t.Setenv(encryptionKeysVar, "") // never inherit a key from the environment
	example := readFile(t, envExamplePath)
	env := strings.Replace(example, encryptionKeysVar+"=\n", encryptionKeysVar+"=k1:"+strings.Repeat("A", 42)+"==\n", 1)
	if env == example {
		t.Fatalf("%s doesn't declare %s:\n%s", envExamplePath, encryptionKeysVar, example)
	}
	writeFile(t, envPath, env+extra)
}

func doctorRun(t *testing.T, wantCode int, args ...string) doctorResult {
	t.Helper()
	code, out, errOut := runOrb(t, append([]string{"doctor", "--json"}, args...)...)
	if code != wantCode {
		t.Fatalf("orb doctor %v = %d, want %d; stdout %s stderr %s", args, code, wantCode, out, errOut)
	}
	var res doctorResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("orb doctor --json output %q: %v", out, err)
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
	newV01GitApp(t, recipes.TenancySingle)
	copyEnvExample(t, "")
	calls := fakeDoctorCommands(t, `{"current":9,"latest":9,"pending":0}`)

	res := doctorRun(t, 0)
	if res.Preset != "full" || res.Failures != 0 {
		t.Fatalf("result = %+v, want no failures", res)
	}
	for name, status := range map[string]string{
		"go": doctorOK, "git": doctorOK, "gorbital.yaml": doctorOK, "docker": doctorWarn, "gorbital.lock": doctorOK,
		"anchors": doctorOK, ".env": doctorOK, "api files": doctorOK, "database": doctorOK,
	} {
		if c := check(res, name); c.Status != status {
			t.Errorf("%s = %+v, want %s", name, c, status)
		}
	}
	wantOrb := doctorOK
	if toolchainWarning(runtime.Version()) != "" {
		wantOrb = doctorWarn
	}
	if c := check(res, "orb"); c.Status != wantOrb {
		t.Errorf("orb = %+v, want a warning only when orb was built with a Go older than %s", c, minimumGo)
	}
	if c := check(res, "gorbital.lock"); !strings.Contains(c.Detail, "0 of") {
		t.Errorf("lock detail = %q, want no edited files", c.Detail)
	}

	*calls = nil
	fast := doctorRun(t, 0, "--fast")
	if slices.ContainsFunc(*calls, func(c string) bool { return strings.HasPrefix(c, "go run") }) || check(fast, "database").Name != "" {
		t.Errorf("--fast ran %v and reported %+v; want no builds", *calls, fast.Checks)
	}
}

func TestDoctorFindsProblems(t *testing.T) {
	newV01GitApp(t, recipes.TenancySingle)
	fakeDoctorCommands(t, `{"current":9,"latest":11,"pending":2}`)
	// Three tracked files edited: .gitignore, modules.go and llms.txt.
	writeFile(t, ".gitignore", "")
	writeFile(t, ".env", "RESEND_API_KEY=re_live_secret\n")
	modules := readFile(t, "internal/app/modules.go")
	writeFile(t, "internal/app/modules.go", strings.Replace(modules, "//orb:anchor modules", "", 1))
	writeFile(t, "api/llms.txt", "stale\n")

	code, out, _ := runOrb(t, "doctor")
	if code != 1 || !strings.Contains(out, "fix:") {
		t.Errorf("orb doctor with problems = %d:\n%s", code, out)
	}
	if strings.Contains(out, "re_live_secret") {
		t.Error("orb doctor printed a secret from .env")
	}

	res := doctorRun(t, 1)
	for name, want := range map[string]string{
		"anchor":        "internal/app/modules.go",
		".env":          "holds secrets",
		"api files":     "api/llms.txt",
		"database":      "2 migrations pending",
		"gorbital.lock": "3 of",
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

// TestDoctorReportsEveryConfigurationProblem: an app's LoadConfig joins its
// problems under an "invalid configuration:" line that says nothing itself,
// so the report has to keep the lines under it, not just the first one
// (CLI-12d).
func TestDoctorReportsEveryConfigurationProblem(t *testing.T) {
	newV01GitApp(t, recipes.TenancySingle)
	copyEnvExample(t, "")
	fakeDoctorCommands(t, `{"config_error":"invalid configuration:\nAPP_ADDR \"8080\" is not host:port\nAPP_DB_MAX_CONNS must be between 1 and 1000, got \"nope\"\nMAIL_DELIVERY must be devmail, mailpit or provider, got \"post\""}`)

	c := check(doctorRun(t, 1), "configuration")
	if c.Status != doctorFail {
		t.Fatalf("configuration check = %+v, want a failure", c)
	}
	for _, want := range []string{"APP_ADDR", "APP_DB_MAX_CONNS", "MAIL_DELIVERY"} {
		if !strings.Contains(c.Detail, want) {
			t.Errorf("configuration detail = %q, want it to name %s", c.Detail, want)
		}
	}
	if strings.Contains(c.Detail, "\n") {
		t.Errorf("configuration detail = %q, want one line", c.Detail)
	}

	// The prefix carries nothing, and apps repeat it; neither reaches the
	// report.
	for _, in := range []string{
		"invalid configuration:\nAPP_ENV is required",
		"invalid configuration: invalid configuration:\nAPP_ENV is required",
		"invalid configuration: APP_ENV is required",
	} {
		if got := configProblems(in); got != "APP_ENV is required" {
			t.Errorf("configProblems(%q) = %q", in, got)
		}
	}
}

// TestDoctorLooksAtEnvValues: .env can hold every variable .env.example has
// and still be unusable, so the key sets matching isn't enough (CLI-12d).
func TestDoctorLooksAtEnvValues(t *testing.T) {
	newV01GitApp(t, recipes.TenancySingle)
	fakeDoctorCommands(t, `{"current":9,"latest":9,"pending":0}`)

	// A plain cp .env.example .env: every variable is there, and seed
	// refuses because the encryption key has no value.
	t.Setenv(encryptionKeysVar, "")
	writeFile(t, envPath, readFile(t, envExamplePath))
	c := check(doctorRun(t, 0), ".env")
	if c.Status != doctorWarn || !strings.Contains(c.Detail, encryptionKeysVar) || c.Fix == "" {
		t.Errorf(".env with an empty %s = %+v, want a warning naming it with a fix", encryptionKeysVar, c)
	}

	// An example value left in place is no value at all.
	for _, value := range []string{"changeme", "CHANGE-ME", "<your-resend-key>", "TODO"} {
		copyEnvExample(t, "RESEND_API_KEY="+value+"\n")
		c := check(doctorRun(t, 1), ".env")
		if c.Status != doctorFail || !strings.Contains(c.Detail, "RESEND_API_KEY") {
			t.Errorf(".env with RESEND_API_KEY=%s = %+v, want a failure naming it", value, c)
		}
		if strings.Contains(c.Detail, value) || strings.Contains(c.Fix, value) {
			t.Errorf(".env check printed the value: %+v", c)
		}
	}

	// Optional variables .env.example leaves empty stay green, and a real
	// value that reads like an example isn't one.
	copyEnvExample(t, "STORAGE_BUCKET=your-company-uploads\nGOOGLE_CLIENT_SECRET=\n")
	if c := check(doctorRun(t, 0), ".env"); c.Status != doctorOK {
		t.Errorf(".env with empty optional variables = %+v, want ok", c)
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

// TestToolchainWarning: orb built with a Go release that lacks the os.Root
// fixes warns (CLI-7); newer releases and development toolchains don't.
func TestToolchainWarning(t *testing.T) {
	for version, warns := range map[string]bool{
		"go1.26.0": true, "go1.26.4": true, "go1.26rc1": true, "go1.26.5": false, "go1.26.8": false,
		"go1.27.0": false, "go1.27rc2": false, "go1.26.5 X:boringcrypto": false, "devel go1.27-abcdef": false,
	} {
		if got := toolchainWarning(version); (got != "") != warns || warns && !strings.Contains(got, version) {
			t.Errorf("toolchainWarning(%q) = %q, want a warning: %v", version, got, warns)
		}
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
