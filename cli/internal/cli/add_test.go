package cli

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"gorbital.dev/cli/internal/recipes"
)

// goldenApp returns the absolute path of examples/full-single.
func goldenApp(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "examples", "full-single")
}

// newMailApp copies the files orb add mail reads from examples/full-single
// into a temporary directory and makes it the working directory.
func newMailApp(t *testing.T) string {
	t.Helper()
	golden, dir := goldenApp(t), t.TempDir()
	for _, f := range []string{"go.mod", "gorbital.yaml", ".env.example", "internal/app/mail.go", recipes.InfraMailPath, recipes.InfraMailTestPath} {
		writeFile(t, filepath.Join(dir, f), readFile(t, filepath.Join(golden, f)))
	}
	t.Chdir(dir)
	return dir
}

func addMail(t *testing.T, args ...string) addMailResult {
	t.Helper()
	code, out, errOut := runOrb(t, append([]string{"add", "mail", "--json", "--skip-tidy"}, args...)...)
	if code != 0 {
		t.Fatalf("orb add mail %v = %d, stderr %q", args, code, errOut)
	}
	var res addMailResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("orb add mail --json output %q: %v", out, err)
	}
	return res
}

func TestAddMailSMTPWithFlags(t *testing.T) {
	newMailApp(t)
	res := addMail(t, "--smtp-host", "smtp.postmarkapp.com", "--smtp-username", "server-token")
	if res.Provider != "smtp" || res.AlreadyConfigured || len(res.Modules) != 0 ||
		!slices.Equal(res.EnvVariables, []string{"SMTP_HOST", "SMTP_PORT", "SMTP_TLS", "SMTP_USERNAME"}) {
		t.Errorf("result = %+v", res)
	}
	if want := []string{recipes.InfraMailPath, recipes.InfraMailTestPath, ".env.example", ".env", "gorbital.yaml"}; !slices.Equal(res.Files, want) {
		t.Errorf("files = %v, want %v", res.Files, want)
	}

	smtp, err := recipes.RenderMail(recipes.MailSMTP, "example.com/acme-api")
	if err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, recipes.InfraMailPath); got != string(smtp.InfraMail) {
		t.Errorf("infra_mail.go is not the SMTP recipe:\n%s", got)
	}
	if got := readFile(t, recipes.InfraMailTestPath); got != string(smtp.InfraMailTest) {
		t.Errorf("infra_mail_test.go is not the SMTP recipe:\n%s", got)
	}
	if example := readFile(t, ".env.example"); !strings.Contains(example, "\nSMTP_HOST=\n") || strings.Contains(example, "RESEND_API_KEY") || !strings.Contains(example, "\nMAIL_DELIVERY=\n") {
		t.Errorf(".env.example doesn't hold the SMTP block alone:\n%s", example)
	}
	env := readFile(t, ".env")
	for _, want := range []string{"\nSMTP_HOST=smtp.postmarkapp.com\n", "\nSMTP_PORT=587\n", "\nSMTP_TLS=starttls\n", "\nSMTP_USERNAME=server-token\n", "\nSMTP_PASSWORD=\n", "\nMAIL_DELIVERY=\n"} {
		if !strings.Contains(env, want) {
			t.Errorf(".env lacks %q:\n%s", want, env)
		}
	}
	if info, err := os.Stat(".env"); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf(".env mode = %v, %v; want 0600", info.Mode().Perm(), err)
	}
	if manifest := readFile(t, "gorbital.yaml"); !strings.HasSuffix(manifest, "\nmail: smtp\n") {
		t.Errorf("gorbital.yaml = %s", manifest)
	}
}

// TestAddMailRecordsTheProviderInTheLock checks that orb add mail keeps
// gorbital.lock true to the files, so upgrades rebuild SMTP files for an
// SMTP app (ADR-0050).
func TestAddMailRecordsTheProviderInTheLock(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if code, _, errOut := runOrb(t, "new", "shop-api", "--preset", "full", "--skip-tidy", "--no-git", "--json"); code != 0 {
		t.Fatalf("orb new = %d, stderr %q", code, errOut)
	}
	t.Chdir(filepath.Join(dir, "shop-api"))
	before, err := readLock(".")
	if err != nil {
		t.Fatal(err)
	}

	res := addMail(t, "--provider", "smtp")
	if !slices.Contains(res.Files, lockPath) {
		t.Errorf("files = %v, want %s among them", res.Files, lockPath)
	}
	after, err := readLock(".")
	if err != nil {
		t.Fatal(err)
	}
	if after.Inputs.Mail != recipes.MailSMTP || after.Orb != before.Orb || len(after.Files) != len(before.Files) || after.tracks(".env") {
		t.Errorf("lock after orb add mail = %+v, want mail smtp and the same release and files", after)
	}
	for _, f := range after.Files {
		if sha256Hex([]byte(readFile(t, f.Path))) != f.SHA256 {
			t.Errorf("gorbital.lock hash of %s is stale after orb add mail", f.Path)
		}
	}
	assertLockRebuilds(t, ".")

	// Choosing the provider the app already has changes nothing.
	if res := addMail(t, "--provider", "smtp"); !res.AlreadyConfigured {
		t.Errorf("second orb add mail = %+v, want already configured", res)
	}
}

func TestAddMailSwitchesBackToResend(t *testing.T) {
	newMailApp(t)
	writeFile(t, ".env", "OPS_TOKEN=abc\nRESEND_API_KEY=re_saved_key\n")
	addMail(t, "--provider", "smtp")
	// Drop the Resend module, as go mod tidy does once nothing imports it.
	cmd := exec.Command("go", "mod", "edit", "-droprequire=gorbital.dev/modules/mail/resend", "-dropreplace=gorbital.dev/modules/mail/resend")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod edit: %v\n%s", err, out)
	}

	res := addMail(t, "--provider", "resend")
	if !slices.Equal(res.Modules, []string{"gorbital.dev/modules/mail/resend"}) || !slices.Contains(res.Files, "go.mod") {
		t.Errorf("result = %+v, want the Resend module added", res)
	}
	goMod := readFile(t, "go.mod")
	for _, want := range []string{"gorbital.dev/modules/mail/resend v0.1.0", "gorbital.dev/modules/mail/resend => ../../modules/mail/resend"} {
		if !strings.Contains(goMod, want) {
			t.Errorf("go.mod lacks %q:\n%s", want, goMod)
		}
	}
	env := readFile(t, ".env")
	block, err := recipes.Block([]byte(env), recipes.MailBlock)
	if strings.Count(env, "RESEND_API_KEY=") != 1 || err != nil || !strings.Contains(string(block), "RESEND_API_KEY=re_saved_key\n") || strings.Contains(env, "SMTP_HOST") {
		t.Errorf(".env should keep the saved key once, inside the Resend block:\n%s", env)
	}
	resend, _ := recipes.RenderMail(recipes.MailResend, "example.com/acme-api")
	if readFile(t, recipes.InfraMailPath) != string(resend.InfraMail) || readFile(t, recipes.InfraMailTestPath) != string(resend.InfraMailTest) {
		t.Error("infra_mail.go and infra_mail_test.go are not the Resend recipe")
	}
}

// TestAddMailMakesAnExistingEnvPrivate: a .env made with cp .env.example
// .env is readable by everyone; once orb add mail saves provider settings in
// it, only the owner can read it, and orb says it changed the mode.
func TestAddMailMakesAnExistingEnvPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes are Unix permissions")
	}
	newMailApp(t)
	writeFile(t, ".env", readFile(t, ".env.example"))
	if err := os.Chmod(".env", 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := runOrb(t, "add", "mail", "--json", "--skip-tidy", "--smtp-host", "smtp.example.com", "--smtp-username", "server-token")
	if code != 0 {
		t.Fatalf("orb add mail = %d, stderr %q", code, errOut)
	}
	if info, err := os.Stat(".env"); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf(".env mode = %v, %v; want 0600", info.Mode().Perm(), err)
	}
	if !strings.Contains(errOut, ".env was readable by other users (mode 0644)") {
		t.Errorf("stderr = %q, want a warning about the old mode", errOut)
	}
	if !strings.Contains(readFile(t, ".env"), "SMTP_USERNAME=server-token") {
		t.Error(".env lacks the saved settings")
	}
}

func TestAddMailAlreadyConfigured(t *testing.T) {
	newMailApp(t)
	files := []string{recipes.InfraMailPath, recipes.InfraMailTestPath, ".env.example", "gorbital.yaml", "go.mod"}
	snapshot := func() string {
		var b strings.Builder
		for _, f := range files {
			b.WriteString(readFile(t, f))
		}
		return b.String()
	}
	before := snapshot()
	code, out, errOut := runOrb(t, "add", "mail", "--yes")
	if code != 0 || !strings.Contains(out, "already sends email with Resend") || !strings.Contains(out, "RESEND_API_KEY=re_") || !strings.Contains(out, "POST /ops/mail/test") {
		t.Errorf("orb add mail on a Resend app = %d %q %q", code, out, errOut)
	}
	if snapshot() != before {
		t.Error("files changed although email was already set up")
	}
	if _, err := os.Stat(".env"); err == nil {
		t.Error(".env was created without values to save")
	}
}

func TestAddMailDryRunWritesNothing(t *testing.T) {
	newMailApp(t)
	code, out, errOut := runOrb(t, "add", "mail", "--provider", "smtp", "--smtp-host", "smtp.example.com", "--smtp-port", "465", "--dry-run")
	if code != 0 || !strings.Contains(out, "Would set up email with SMTP") || !strings.Contains(out, "smtp.example.com:465 (tls)") ||
		!strings.Contains(out, ".env (created from .env.example, saves SMTP_HOST, SMTP_PORT, SMTP_TLS)") {
		t.Errorf("orb add mail --dry-run = %d %q %q", code, out, errOut)
	}
	if _, err := os.Stat(".env"); !os.IsNotExist(err) {
		t.Error("dry run created .env")
	}
	if strings.Contains(readFile(t, recipes.InfraMailPath), `"smtp"`) {
		t.Error("dry run replaced infra_mail.go")
	}
}

func TestAddMailValidation(t *testing.T) {
	newMailApp(t)
	tests := []struct {
		name string
		args []string
		code int
		want string
	}{
		{"unknown provider", []string{"--provider", "sendgrid"}, 2, "resend or smtp"},
		{"SMTP flag with Resend", []string{"--provider", "resend", "--smtp-host", "mail.example.com"}, 2, "only to --provider smtp"},
		{"host with a scheme", []string{"--smtp-host", "smtp://mail.example.com"}, 2, "without a scheme"},
		{"bad port", []string{"--smtp-host", "mail.example.com", "--smtp-port", "99999"}, 2, "port number"},
		{"bad encryption", []string{"--smtp-host", "mail.example.com", "--smtp-tls", "ssl"}, 2, "starttls, tls or none"},
		{"positional argument", []string{"resend"}, 2, "unexpected arguments"},
	}
	for _, tt := range tests {
		code, _, errOut := runOrb(t, append([]string{"add", "mail", "--skip-tidy"}, tt.args...)...)
		if code != tt.code || !strings.Contains(errOut, tt.want) {
			t.Errorf("%s: exit %d %q, want %d containing %q", tt.name, code, errOut, tt.code, tt.want)
		}
	}
	if _, err := os.Stat(".env"); err == nil {
		t.Error("a failed command wrote .env")
	}

	writeFile(t, ".env.example", "OPS_TOKEN=\n")
	if code, _, errOut := runOrb(t, "add", "mail", "--yes"); code != 1 || !strings.Contains(errOut, "# orb:begin mail") {
		t.Errorf("without the email block = %d %q, want the lines to add", code, errOut)
	}

	minimal := t.TempDir()
	writeFile(t, filepath.Join(minimal, "go.mod"), "module example.com/minimal\n")
	t.Chdir(minimal)
	if code, _, errOut := runOrb(t, "add", "mail", "--yes"); code != 1 || !strings.Contains(errOut, "Full preset") {
		t.Errorf("in a Minimal app = %d %q, want Full preset guidance", code, errOut)
	}
	if code, _, errOut := runOrb(t, "add", "cache"); code != 2 || !strings.Contains(errOut, "want mail") {
		t.Errorf("orb add cache = %d %q", code, errOut)
	}
}

func TestUpdateDotEnv(t *testing.T) {
	env := []byte("OPS_TOKEN=abc\nSMTP_HOST=old.example.com\n# orb:begin mail\nRESEND_API_KEY=re_1\n# orb:end mail\nAPP_ENV=development\n")
	block := []byte("# orb:begin mail\nSMTP_HOST=\nSMTP_PASSWORD=\n# orb:end mail\n")
	got := string(updateDotEnv(env, block, map[string]string{"SMTP_PASSWORD": "p@ss word#1"}))
	want := "OPS_TOKEN=abc\n# orb:begin mail\nSMTP_HOST=old.example.com\nSMTP_PASSWORD=\"p@ss word#1\"\n# orb:end mail\nAPP_ENV=development\n"
	if got != want {
		t.Errorf("updateDotEnv() =\n%s\nwant\n%s", got, want)
	}
	parsed, err := parseDotEnv(strings.NewReader(got))
	if err != nil || parsed["SMTP_PASSWORD"] != "p@ss word#1" || parsed["SMTP_HOST"] != "old.example.com" {
		t.Errorf("parsed .env = %v, %v", parsed, err)
	}

	if got := string(updateDotEnv([]byte("A=1"), block, nil)); got != "A=1\n\n"+string(block) {
		t.Errorf("updateDotEnv(without block) = %q, want the block appended", got)
	}
}

func TestSetManifestKey(t *testing.T) {
	if got := string(recipes.SetManifestKey([]byte("name: x\nmail: resend\npreset: full\n"), "mail", "smtp")); got != "name: x\nmail: smtp\npreset: full\n" {
		t.Errorf("recipes.SetManifestKey(replace) = %q", got)
	}
	if got := string(recipes.SetManifestKey([]byte("name: x"), "mail", "smtp")); got != "name: x\nmail: smtp\n" {
		t.Errorf("recipes.SetManifestKey(append) = %q", got)
	}
}

func TestPromptMailResend(t *testing.T) {
	stdin := answers(
		"1",           // provider: Resend
		"re_test_123", // API key
	)
	var in mailInput
	var out bytes.Buffer
	if err := promptMail(&in, map[string]bool{}, promptFlags{plain: true}, stdin, &out); err != nil {
		t.Fatalf("promptMail() error = %v\noutput:\n%s", err, out.String())
	}
	if in.provider != recipes.MailResend || in.apiKey != "re_test_123" {
		t.Errorf("answers = %+v\noutput:\n%s", in, out.String())
	}
	for _, text := range []string{"How should the app send email?", "never a flag", "/ops/settings", "Resend API key"} {
		if !strings.Contains(out.String(), text) {
			t.Errorf("output lacks %q:\n%s", text, out.String())
		}
	}
	if strings.Contains(out.String(), "SMTP server") {
		t.Errorf("SMTP questions asked for Resend:\n%s", out.String())
	}
}

func TestPromptMailSMTP(t *testing.T) {
	stdin := answers(
		"2",                    // provider: SMTP
		"smtp.postmarkapp.com", // server
		"2",                    // 465 with TLS
		"server-token",         // username
		"s3cret",               // password
	)
	var in mailInput
	var out bytes.Buffer
	if err := promptMail(&in, map[string]bool{}, promptFlags{plain: true}, stdin, &out); err != nil {
		t.Fatalf("promptMail() error = %v\noutput:\n%s", err, out.String())
	}
	want := mailInput{provider: recipes.MailSMTP, smtpHost: "smtp.postmarkapp.com", smtpPort: "465", smtpTLS: "tls", smtpUsername: "server-token", smtpPassword: "s3cret"}
	if in != want {
		t.Errorf("answers = %+v\nwant      %+v\noutput:\n%s", in, want, out.String())
	}
	if strings.Contains(out.String(), "Resend API key") {
		t.Errorf("Resend question asked for SMTP:\n%s", out.String())
	}
}

func TestPromptMailSkipsQuestionsAnsweredByFlags(t *testing.T) {
	stdin := answers("") // only the password remains; leave it for .env
	in := mailInput{provider: recipes.MailSMTP, smtpHost: "smtp.example.com", smtpPort: "587", smtpUsername: "user"}
	set := map[string]bool{"smtp-host": true, "smtp-port": true, "smtp-username": true}
	var out bytes.Buffer
	if err := promptMail(&in, set, promptFlags{plain: true}, stdin, &out); err != nil {
		t.Fatalf("promptMail() error = %v\noutput:\n%s", err, out.String())
	}
	for _, skipped := range []string{"How should the app send email?", "SMTP server (optional)", "Port and encryption", "SMTP username"} {
		if strings.Contains(out.String(), skipped) {
			t.Errorf("question %q asked although its flag was given:\n%s", skipped, out.String())
		}
	}
	if !strings.Contains(out.String(), "SMTP password") || in.smtpPassword != "" {
		t.Errorf("password question = %+v\noutput:\n%s", in, out.String())
	}
}

// TestAddMailAppBuilds switches a copy of examples/full-single to SMTP and
// back to Resend, building, vetting and testing it each time; tests that need
// PostgreSQL skip without it. Set ORB_E2E=1 to run it.
func TestAddMailAppBuilds(t *testing.T) {
	if os.Getenv("ORB_E2E") == "" {
		t.Skip("set ORB_E2E=1 to run the end-to-end test")
	}
	golden := goldenApp(t)
	repo, err := filepath.Abs(filepath.Join(golden, "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	err = filepath.WalkDir(golden, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(golden, p)
		if d.IsDir() {
			if rel == "bin" || rel == ".orb" {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(dir, rel), 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if rel == "go.mod" {
			data = []byte(strings.ReplaceAll(string(data), "=> ../..", "=> "+filepath.ToSlash(repo)))
		}
		return os.WriteFile(filepath.Join(dir, rel), data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	for _, provider := range []string{recipes.MailSMTP, recipes.MailResend} {
		if code, _, errOut := runOrb(t, "add", "mail", "--provider", provider, "--allow-dirty"); code != 0 {
			t.Fatalf("orb add mail --provider %s = %d: %s", provider, code, errOut)
		}
		for _, args := range [][]string{{"build", "./..."}, {"vet", "./..."}, {"test", "./internal/app/"}} {
			cmd := exec.Command("go", args...)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("go %s with %s failed: %v\n%s", strings.Join(args, " "), provider, err, out)
			}
		}
	}
}
