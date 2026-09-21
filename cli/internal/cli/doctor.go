package cli

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"gorbital.dev/cli/internal/recipes"
)

// Check statuses of orb doctor.
const (
	doctorOK   = "ok"
	doctorWarn = "warn"
	doctorFail = "fail"
)

type doctorCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
	Fix    string `json:"fix,omitempty"`
}

type doctorResult struct {
	App     string `json:"app"`
	Preset  string `json:"preset,omitempty"`
	Tenancy string `json:"tenancy,omitempty"`
	// Layout is "main" for an app on gorbital.Main and "v0.1" for an app
	// wired by internal/app.
	Layout   string        `json:"layout,omitempty"`
	Checks   []doctorCheck `json:"checks"`
	Failures int           `json:"failures"`
	Warnings int           `json:"warnings"`
}

// errDoctorFailed reports checks that failed; the report lists them.
var errDoctorFailed = errors.New("some checks failed; see the fixes above")

// doctorCommandTimeout bounds each program orb doctor runs, including a cold
// go run.
const doctorCommandTimeout = 3 * time.Minute

// doctorCommand runs a program in dir for orb doctor. Tests replace it.
var doctorCommand = func(ctx context.Context, dir string, env []string, name string, args ...string) (stdout, stderr string, err error) {
	ctx, cancel := context.WithTimeout(ctx, doctorCommandTimeout)
	defer cancel()
	var out, errOut bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir, cmd.Env, cmd.Stdout, cmd.Stderr = dir, env, &out, &errOut
	err = cmd.Run()
	return out.String(), errOut.String(), err
}

const doctorUsage = `Usage: orb doctor [flags]

Checks the app in the current directory and prints what to fix: the Go
toolchain, git, Docker and the Go orb was built with; gorbital.lock and the
library version; the lines generators insert at (v0.1 apps) or the module
list, modules copied with orb eject, the middleware stack and
APP_REQUEST_TIMEOUT (apps on gorbital.Main);
.env; whether api/ matches the code; and the database's migrations and
row-level security. It changes nothing (ADR-0051).

Exit codes: 0 when no check failed (warnings allowed), 1 when one did, 2 for
invalid usage.
`

func runDoctor(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("orb doctor", flag.ContinueOnError)
	flags.SetOutput(stderr)
	asJSON := flags.Bool("json", false, "print the result as JSON")
	fast := flags.Bool("fast", false, "skip the checks that build the app: api/ files and database")
	flags.Usage = func() {
		fmt.Fprint(stderr, doctorUsage+"\nFlags:\n")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return usageError(fmt.Sprintf("unexpected arguments: %s", strings.Join(flags.Args(), " ")))
	}
	app, err := findApp()
	if err != nil {
		return err
	}

	d := &doctor{dir: app.dir, res: doctorResult{App: filepath.Base(app.dir), Layout: appLayout(app.dir), Checks: []doctorCheck{}}}
	main := d.res.Layout == layoutMain
	d.toolchain(ctx)
	d.project(ctx)
	switch {
	case main:
		d.modules(app)
		d.profile()
		d.resourceScopes(app)
		d.ejected(ctx)
		d.stack()
	case d.res.Preset == "full":
		d.generatorAnchors()
	}
	if d.res.Preset == "full" || main {
		d.environment(ctx)
	}
	env, envErr := devEnv(filepath.Join(app.dir, envPath))
	if main && envErr == nil {
		d.requestTimeout(env)
	}
	if !*fast {
		if envErr != nil {
			d.add(doctorFail, ".env", envErr.Error(), "fix the line in .env")
		} else {
			d.apiFiles(ctx, env)
			if d.res.Preset == "full" || main {
				d.database(ctx, env)
			}
		}
	}

	if *asJSON {
		if err := writeJSON(stdout, d.res); err != nil {
			return err
		}
	} else {
		d.report(stdout)
	}
	if d.res.Failures > 0 {
		return errDoctorFailed
	}
	return nil
}

type doctor struct {
	dir string
	res doctorResult
}

func (d *doctor) add(status, name, detail, fix string) {
	d.res.Checks = append(d.res.Checks, doctorCheck{Name: name, Status: status, Detail: detail, Fix: fix})
	switch status {
	case doctorFail:
		d.res.Failures++
	case doctorWarn:
		d.res.Warnings++
	}
}

func (d *doctor) path(rel string) string { return filepath.Join(d.dir, filepath.FromSlash(rel)) }

// toolchain checks Go against go.mod, git, and the Go orb was built with.
func (d *doctor) toolchain(ctx context.Context) {
	have, _, err := doctorCommand(ctx, d.dir, nil, "go", "env", "GOVERSION")
	have = strings.TrimSpace(have)
	info, modErr := readGoMod(ctx, d.dir)
	switch {
	case err != nil:
		d.add(doctorFail, "go", "the go command isn't available", "install Go from https://go.dev/dl")
	case modErr != nil:
		d.add(doctorFail, "go", "go.mod can't be read: "+firstLine(modErr.Error()), "fix go.mod")
	case info.Go != "" && !versionAtLeast(strings.TrimPrefix(have, "go"), info.Go):
		d.add(doctorWarn, "go", fmt.Sprintf("%s is older than go.mod's go %s", have, info.Go), "install Go "+info.Go+" or newer, or leave GOTOOLCHAIN=auto so go downloads it")
	default:
		d.add(doctorOK, "go", fmt.Sprintf("%s; go.mod needs %s", have, cmpOr(info.Go, "any")), "")
	}

	if _, err := exec.LookPath("git"); err != nil {
		d.add(doctorWarn, "git", "git isn't installed", "install git: orb upgrade, orb add and the generators work on a clean git tree")
	} else {
		d.add(doctorOK, "git", "installed", "")
	}

	if warning := toolchainWarning(runtime.Version()); warning != "" {
		d.add(doctorWarn, "orb", warning, "install Go "+minimumGo+" or newer, then run go install for orb again")
	} else {
		d.add(doctorOK, "orb", Version+" built with "+runtime.Version(), "")
	}
}

// project checks gorbital.yaml, gorbital.lock and the library in go.mod.
func (d *doctor) project(ctx context.Context) {
	inputs, err := readManifest(d.dir)
	if err != nil {
		d.add(doctorFail, "gorbital.yaml", firstLine(err.Error()), "apps created with orb new have one; orb commands need it")
		return
	}
	d.res.Preset, d.res.Tenancy = inputs.Preset, inputs.Tenancy
	d.add(doctorOK, "gorbital.yaml", fmt.Sprintf("%s preset, %s tenancy", inputs.Preset, inputs.Tenancy), "")

	if inputs.Preset == "full" {
		if _, _, err := doctorCommand(ctx, d.dir, nil, "docker", "info", "--format", "{{.ServerVersion}}"); err != nil {
			d.add(doctorWarn, "docker", "Docker isn't running or isn't installed", "start Docker Desktop (or Docker Engine with Compose v2): orb dev runs PostgreSQL and Mailpit in it")
		} else {
			d.add(doctorOK, "docker", "running", "")
		}
	}

	lock, err := readLock(d.dir)
	switch {
	case errors.Is(err, errNoLock):
		d.add(doctorWarn, "gorbital.lock", "missing, so orb upgrade can't rebuild what gorbital wrote", "restore it from git history")
	case err != nil:
		d.add(doctorFail, "gorbital.lock", firstLine(err.Error()), "restore it from git history, or use the orb that wrote it")
	case lock.APIVersion == lockAPIVersionV1:
		d.add(doctorWarn, "gorbital.lock", "written by an early development build of orb, without the release that created the app", "orb upgrade --from <commit that created the app>")
	case !lock.rendered():
		d.add(doctorOK, "gorbital.lock", fmt.Sprintf("records %d modules orb eject copied", len(lock.Ejected)), "")
	default:
		edited := 0
		for _, f := range lock.Files {
			if content, err := os.ReadFile(d.path(f.Path)); err != nil || sha256Hex(content) != f.SHA256 {
				edited++
			}
		}
		detail := fmt.Sprintf("from orb %s; %d of %d files gorbital wrote are edited or removed", cmpOr(lock.Orb.Version, "(unknown)"), edited, len(lock.Files))
		if lock.Orb.Version != Version {
			d.add(doctorWarn, "gorbital.lock", detail+"; this is orb "+Version, "orb upgrade merges this release's templates on a branch")
		} else {
			d.add(doctorOK, "gorbital.lock", detail, "")
		}
	}

	info, err := readGoMod(ctx, d.dir)
	if err != nil {
		return // reported by the go check
	}
	version, local := info.gorbital()
	switch {
	case local != "":
		dir := local
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(d.dir, dir)
		}
		if _, err := resolveLocal(dir); err != nil {
			d.add(doctorFail, "library", fmt.Sprintf("go.mod replaces gorbital.dev with %s, which isn't a gorbital checkout", local), "point the replace directives in go.mod at your gorbital checkout")
		} else {
			d.add(doctorOK, "library", "from the checkout at "+local, "")
		}
	case !slices.ContainsFunc(info.Require, func(r goModRequire) bool { return r.Path == "gorbital.dev" }):
		d.add(doctorWarn, "library", "go.mod doesn't require gorbital.dev", "run go mod tidy")
	default:
		d.add(doctorOK, "library", "gorbital.dev "+version, "")
	}
}

// generatorAnchors checks the lines generators insert after.
func (d *doctor) generatorAnchors() {
	anchors := []struct{ file, anchor, generator string }{
		{"internal/app/modules.go", recipes.ModulesAnchor, "orb gen resource"},
		{"internal/app/jobs.go", recipes.JobAnchor, "orb gen job"},
		{"internal/app/permissions.go", recipes.UserPermissionsAnchor, "orb gen resource --scope user"},
	}
	if d.res.Tenancy == recipes.TenancyMulti {
		anchors = append(anchors, struct{ file, anchor, generator string }{"internal/app/permissions.go", recipes.OrgPermissionsAnchor, "orb gen resource --scope org"})
	}
	missing := 0
	for _, a := range anchors {
		src, err := os.ReadFile(d.path(a.file))
		if err != nil || !bytes.Contains(src, []byte(a.anchor)) {
			missing++
			d.add(doctorFail, "anchor", fmt.Sprintf("%s has no %q line", a.file, a.anchor), fmt.Sprintf("put the line back where %s inserts code, as in a new app", a.generator))
		}
	}
	example, err := os.ReadFile(d.path(envExamplePath))
	if err == nil {
		_, err = recipes.Block(example, recipes.MailBlock)
	}
	if err != nil {
		missing++
		d.add(doctorFail, "anchor", ".env.example has no # orb:begin mail … # orb:end mail block", "put the block back: orb add mail replaces the provider's variables inside it")
	}
	if missing == 0 {
		d.add(doctorOK, "anchors", "every line generators insert at is in place", "")
	}
}

// secretKeyWords mark .env variables that usually hold secrets.
var secretKeyWords = []string{"KEY", "SECRET", "PASSWORD", "TOKEN", "DATABASE_URL"}

// environment checks .env against .env.example and git. Values are never
// printed.
func (d *doctor) environment(ctx context.Context) {
	env, err := readDotEnvFile(d.path(envPath))
	if errors.Is(err, os.ErrNotExist) {
		d.add(doctorWarn, ".env", "missing", "orb dev creates it from .env.example, or run: cp .env.example .env")
		return
	} else if err != nil {
		d.add(doctorFail, ".env", firstLine(err.Error()), "fix the line in .env")
		return
	}
	example, err := readDotEnvFile(d.path(envExamplePath))
	if err != nil {
		example = map[string]string{}
	}
	var missing []string
	for key := range example {
		if _, ok := env[key]; !ok {
			missing = append(missing, key)
		}
	}
	slices.Sort(missing)

	secrets := false
	for key, value := range env {
		if value != "" && slices.ContainsFunc(secretKeyWords, func(w string) bool { return strings.Contains(strings.ToUpper(key), w) }) {
			secrets = true
		}
	}
	if insideGitRepo(ctx, d.dir) {
		if _, _, err := doctorCommand(ctx, d.dir, nil, "git", "check-ignore", "--quiet", envPath); err != nil {
			if secrets {
				d.add(doctorFail, ".env", "holds secrets, but git doesn't ignore it", "add .env to .gitignore before committing")
			} else {
				d.add(doctorWarn, ".env", "git doesn't ignore it", "add .env to .gitignore before putting secrets in it")
			}
			return
		}
	}
	if len(missing) > 0 {
		d.add(doctorWarn, ".env", "lacks variables .env.example has: "+strings.Join(missing, ", "), "copy them from .env.example; the app uses its defaults until then")
		return
	}
	if status, detail, fix := unusableEnvValue(env, example); detail != "" {
		d.add(status, ".env", detail, fix)
		return
	}
	d.add(doctorOK, ".env", "has every variable .env.example has", "")
}

// envPlaceholders are values nobody means: a variable still carrying one is
// unset in effect. Kept to values that can't be anyone's real setting, and
// matched whole, so a real value that begins like an example (a bucket
// named your-company-uploads) is never mistaken for one.
var envPlaceholders = []string{"changeme", "change-me", "change_me", "replaceme", "replace-me", "replace-this", "todo", "tbd"}

// unusableEnvValue reports the first .env value the app can't work with, how
// to fix it, and how bad it is, or "". Two things count as unusable, both
// decided by the value alone, never by what the variable means:
//
//   - a placeholder, whole or in <angle brackets>: nobody means one, so the
//     variable is unset however it looks (fail).
//   - AUTH_ENCRYPTION_KEYS empty, when .env.example declares it and the
//     environment doesn't set it. It is the one variable orb itself knows
//     an app needs a value for: orb dev writes one (ensureEncryptionKey),
//     and without one seed refuses to create the administrator and nobody
//     can set up two-factor authentication. A warning, not a failure: a
//     Full app still starts and serves in development without it, and it is
//     the state orb new leaves behind until the first orb dev.
//
// Everything else is left alone on purpose. A variable .env.example leaves
// empty is an optional one (Google, Apple, GitHub, WebAuthn, storage,
// Resend) and stays green. Blanking a variable .env.example gives a value
// isn't reported either: the app either falls back to its default or
// refuses to start, and then the configuration check reports it in the
// app's own words. Nor is a value that is wrong rather than unset — an
// expired key, a database that isn't there — which only the app can judge.
// Values are never printed, only names.
func unusableEnvValue(env, example map[string]string) (status, detail, fix string) {
	var placeholders []string
	for key, value := range env {
		if isEnvPlaceholder(value) {
			placeholders = append(placeholders, key)
		}
	}
	if len(placeholders) > 0 {
		slices.Sort(placeholders)
		return doctorFail, "still holds an example value for " + strings.Join(placeholders, ", "),
			"set what each needs; .env.example documents them"
	}
	if _, declared := example[encryptionKeysVar]; declared &&
		strings.TrimSpace(env[encryptionKeysVar]) == "" && os.Getenv(encryptionKeysVar) == "" {
		return doctorWarn, encryptionKeysVar + " has no value, so seed and two-factor authentication refuse to run",
			`orb dev writes a development key into .env, or set one: echo "k1:$(openssl rand -base64 32)"`
	}
	return "", "", ""
}

// isEnvPlaceholder reports whether value was never filled in.
func isEnvPlaceholder(value string) bool {
	v := strings.ToLower(strings.TrimSpace(value))
	if strings.HasPrefix(v, "<") && strings.HasSuffix(v, ">") && len(v) > 2 {
		return true
	}
	return slices.Contains(envPlaceholders, v)
}

// apiFiles checks that api/ matches the code, by exporting to a temporary
// directory.
func (d *doctor) apiFiles(ctx context.Context, env []string) {
	if _, err := os.Stat(d.path("api/openapi.json")); err != nil {
		return
	}
	if _, err := os.Stat(d.path("cmd/api")); err != nil {
		return
	}
	tmp, err := os.MkdirTemp("", "orb-doctor-")
	if err != nil {
		d.add(doctorWarn, "api files", err.Error(), "")
		return
	}
	defer os.RemoveAll(tmp)
	if _, errOut, err := doctorCommand(ctx, d.dir, env, "go", "run", "./cmd/api", "openapi", "--dir", tmp); err != nil {
		d.add(doctorWarn, "api files", "couldn't export them: "+firstLine(cmpOr(errOut, err.Error())), "check that the app builds (go build ./...); apps from early development builds get --dir with orb upgrade")
		return
	}
	var stale []string
	for _, name := range []string{"openapi.json", "postman_collection.json", "llms.txt"} {
		want, _ := os.ReadFile(filepath.Join(tmp, name))
		got, err := os.ReadFile(d.path("api/" + name))
		if err != nil || !bytes.Equal(got, want) {
			stale = append(stale, "api/"+name)
		}
	}
	if len(stale) > 0 {
		d.add(doctorWarn, "api files", strings.Join(stale, ", ")+" don't match the code", "go run ./cmd/api openapi --dir api")
		return
	}
	d.add(doctorOK, "api files", "openapi.json, postman_collection.json and llms.txt match the code", "")
}

// database checks the configuration and migrations through the app's
// migrate command, so orb needs no database driver.
func (d *doctor) database(ctx context.Context, env []string) {
	commands := migrateCommands(d.dir, []string{"--status", "--json"})
	if _, err := os.Stat(d.path("cmd/migrate")); err != nil && !isGorbitalApp(d.dir) {
		return
	}
	out, errOut, err := doctorCommand(ctx, d.dir, env, "go", commands[0]...)
	var s struct {
		ConfigError   string `json:"config_error"`
		DatabaseError string `json:"database_error"`
		Current       int64  `json:"current"`
		Latest        int64  `json:"latest"`
		Pending       int    `json:"pending"`
		// Problems with row-level security, from apps since ADR-0061.
		RowLevelSecurity []string `json:"row_level_security"`
	}
	if jsonErr := json.Unmarshal([]byte(out), &s); err != nil || jsonErr != nil {
		d.add(doctorWarn, "database", "couldn't read the migration status: "+firstLine(cmp.Or(errOut, errString(err), errString(jsonErr))), "check that the app builds; apps from early development builds get migrate --status with orb upgrade")
		return
	}
	switch {
	case s.ConfigError != "":
		d.add(doctorFail, "configuration", configProblems(s.ConfigError), "set the variables in .env or the environment; .env.example documents each")
	case s.DatabaseError != "":
		d.add(doctorWarn, "database", "unreachable: "+firstLine(s.DatabaseError), "start it with orb dev (or docker compose up -d --wait) and check DATABASE_URL")
	case s.Current > s.Latest && isGorbitalApp(d.dir):
		d.add(doctorFail, "database", fmt.Sprintf("at migration %d, but the newest migration of the app and the library is %d", s.Current, s.Latest), "restore the missing migration files, or update gorbital.dev/gorbital: the database ran migrations this code doesn't have")
	case s.Current > s.Latest:
		d.add(doctorFail, "database", fmt.Sprintf("at migration %d, but the newest file in db/migrations is %d", s.Current, s.Latest), "restore the missing migration files: the database ran migrations this code doesn't have")
	case s.Pending > 0:
		d.add(doctorWarn, "database", strconv.Itoa(s.Pending)+" migrations pending", "go "+strings.Join(migrateCommands(d.dir, nil)[0], " ")+" (orb dev runs them)")
	default:
		d.add(doctorOK, "database", fmt.Sprintf("at migration %d, none pending", s.Current), "")
	}
	for _, problem := range s.RowLevelSecurity {
		d.add(doctorWarn, "row-level security", firstLine(problem), "see docs/guides/row-level-security.md")
	}
}

func (d *doctor) report(w io.Writer) {
	s := newStyles(w)
	about := d.res.App
	if d.res.Preset != "" {
		about += fmt.Sprintf(" (%s, %s tenancy)", d.res.Preset, d.res.Tenancy)
	}
	fmt.Fprintf(w, "%s\n\n", s.strong.Render("orb doctor · "+about))
	for _, c := range d.res.Checks {
		var status string
		switch c.Status {
		case doctorOK:
			status = s.muted.Render(fmt.Sprintf("%-4s", c.Status))
		case doctorFail:
			status = s.accent.Render(fmt.Sprintf("%-4s", c.Status))
		default:
			status = fmt.Sprintf("%-4s", c.Status)
		}
		fmt.Fprintf(w, "  %s  %-14s %s\n", status, c.Name, c.Detail)
		if c.Fix != "" {
			fmt.Fprintf(w, "  %s  %-14s %s\n", "    ", "", s.dim.Render("fix: "+c.Fix))
		}
	}
	fmt.Fprintf(w, "\n  %d failed, %d warnings\n", d.res.Failures, d.res.Warnings)
}

func readDotEnvFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return parseDotEnv(f)
}

// versionAtLeast compares dotted Go versions such as 1.26.0 and 1.26. A
// pre-release (1.26rc1) is older than its release.
func versionAtLeast(have, want string) bool {
	h, w := strings.Split(have, "."), strings.Split(want, ".")
	prerelease := false
	for i := range max(len(h), len(w)) {
		var a, b int
		if i < len(h) {
			digits := strings.IndexFunc(h[i], func(r rune) bool { return r < '0' || r > '9' })
			if digits >= 0 {
				prerelease, h[i] = true, h[i][:digits]
			}
			a, _ = strconv.Atoi(h[i])
		}
		if i < len(w) {
			b, _ = strconv.Atoi(w[i])
		}
		if a != b {
			return a > b
		}
	}
	return !prerelease
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}

// configErrorPrefix is what an app's LoadConfig puts in front of the
// per-variable problems, on a line of its own.
const configErrorPrefix = "invalid configuration:"

// configProblems turns an app's configuration error into one line naming
// every variable. LoadConfig joins the problems under an "invalid
// configuration:" line that carries nothing itself, so firstLine would throw
// the detail away; older apps (and migrate --status without --json) repeat
// the prefix. Whatever is left of the message is kept, so a shape this
// doesn't know still reaches the report.
func configProblems(s string) string {
	rest := strings.TrimSpace(s)
	for {
		trimmed := strings.TrimSpace(strings.TrimPrefix(rest, configErrorPrefix))
		if trimmed == rest {
			break
		}
		rest = trimmed
	}
	var problems []string
	for _, line := range strings.Split(rest, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			problems = append(problems, line)
		}
	}
	if len(problems) == 0 {
		return firstLine(s) // nothing but the prefix: keep the message as it is
	}
	return strings.Join(problems, "; ")
}
