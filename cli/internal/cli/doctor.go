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
	"slices"
	"strconv"
	"strings"
	"time"

	"apistock.dev/cli/internal/recipes"
)

// Check statuses of aps doctor.
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
	App      string        `json:"app"`
	Preset   string        `json:"preset,omitempty"`
	Tenancy  string        `json:"tenancy,omitempty"`
	Checks   []doctorCheck `json:"checks"`
	Failures int           `json:"failures"`
	Warnings int           `json:"warnings"`
}

// errDoctorFailed reports checks that failed; the report lists them.
var errDoctorFailed = errors.New("some checks failed; see the fixes above")

// doctorCommandTimeout bounds each program aps doctor runs, including a cold
// go run.
const doctorCommandTimeout = 3 * time.Minute

// doctorCommand runs a program in dir for aps doctor. Tests replace it.
var doctorCommand = func(ctx context.Context, dir string, env []string, name string, args ...string) (stdout, stderr string, err error) {
	ctx, cancel := context.WithTimeout(ctx, doctorCommandTimeout)
	defer cancel()
	var out, errOut bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir, cmd.Env, cmd.Stdout, cmd.Stderr = dir, env, &out, &errOut
	err = cmd.Run()
	return out.String(), errOut.String(), err
}

const doctorUsage = `Usage: aps doctor [flags]

Checks the app in the current directory and prints what to fix: the Go
toolchain, git and Docker; apistock.lock and the library version; the lines
generators insert at; .env; whether api/ matches the code; and the
database's migrations. It changes nothing (ADR-0051).
`

func runDoctor(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("aps doctor", flag.ContinueOnError)
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

	d := &doctor{dir: app.dir, res: doctorResult{App: filepath.Base(app.dir), Checks: []doctorCheck{}}}
	d.toolchain(ctx)
	d.project(ctx)
	if d.res.Preset == "full" {
		d.generatorAnchors()
		d.environment(ctx)
	}
	if !*fast {
		env, err := devEnv(filepath.Join(app.dir, envPath))
		if err != nil {
			d.add(doctorFail, ".env", err.Error(), "fix the line in .env")
		} else {
			d.apiFiles(ctx, env)
			if d.res.Preset == "full" {
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

// toolchain checks Go against go.mod, git and, for Full apps, Docker.
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
		d.add(doctorWarn, "git", "git isn't installed", "install git: aps upgrade, aps add and the generators work on a clean git tree")
	} else {
		d.add(doctorOK, "git", "installed", "")
	}
}

// project checks apistock.yaml, apistock.lock and the library in go.mod.
func (d *doctor) project(ctx context.Context) {
	inputs, err := readManifest(d.dir)
	if err != nil {
		d.add(doctorFail, "apistock.yaml", firstLine(err.Error()), "apps created with aps new have one; aps commands need it")
		return
	}
	d.res.Preset, d.res.Tenancy = inputs.Preset, inputs.Tenancy
	d.add(doctorOK, "apistock.yaml", fmt.Sprintf("%s preset, %s tenancy", inputs.Preset, inputs.Tenancy), "")

	if inputs.Preset == "full" {
		if _, _, err := doctorCommand(ctx, d.dir, nil, "docker", "info", "--format", "{{.ServerVersion}}"); err != nil {
			d.add(doctorWarn, "docker", "Docker isn't running or isn't installed", "start Docker Desktop (or Docker Engine with Compose v2): aps dev runs PostgreSQL and Mailpit in it")
		} else {
			d.add(doctorOK, "docker", "running", "")
		}
	}

	lock, err := readLock(d.dir)
	switch {
	case errors.Is(err, errNoLock):
		d.add(doctorWarn, "apistock.lock", "missing, so aps upgrade can't rebuild what apistock wrote", "restore it from git history")
	case err != nil:
		d.add(doctorFail, "apistock.lock", firstLine(err.Error()), "restore it from git history, or use the aps that wrote it")
	case lock.APIVersion == lockAPIVersionV1:
		d.add(doctorWarn, "apistock.lock", "written before v0.5, without the release that created the app", "aps upgrade --from <that release>, such as --from v0.4.0")
	default:
		edited := 0
		for _, f := range lock.Files {
			if content, err := os.ReadFile(d.path(f.Path)); err != nil || sha256Hex(content) != f.SHA256 {
				edited++
			}
		}
		detail := fmt.Sprintf("from aps %s; %d of %d files apistock wrote are edited or removed", cmpOr(lock.Aps.Version, "(unknown)"), edited, len(lock.Files))
		if lock.Aps.Version != Version {
			d.add(doctorWarn, "apistock.lock", detail+"; this is aps "+Version, "aps upgrade merges this release's templates on a branch")
		} else {
			d.add(doctorOK, "apistock.lock", detail, "")
		}
	}

	info, err := readGoMod(ctx, d.dir)
	if err != nil {
		return // reported by the go check
	}
	version, local := info.apistock()
	switch {
	case local != "":
		dir := local
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(d.dir, dir)
		}
		if _, err := resolveLocal(dir); err != nil {
			d.add(doctorFail, "library", fmt.Sprintf("go.mod replaces apistock.dev with %s, which isn't an apistock checkout", local), "point the replace directives in go.mod at your apistock checkout")
		} else {
			d.add(doctorOK, "library", "from the checkout at "+local, "")
		}
	case !slices.ContainsFunc(info.Require, func(r goModRequire) bool { return r.Path == "apistock.dev" }):
		d.add(doctorWarn, "library", "go.mod doesn't require apistock.dev", "run go mod tidy")
	default:
		d.add(doctorOK, "library", "apistock.dev "+version, "")
	}
}

// generatorAnchors checks the lines generators insert after.
func (d *doctor) generatorAnchors() {
	anchors := []struct{ file, anchor, generator string }{
		{"internal/app/modules.go", recipes.ModulesAnchor, "aps gen resource"},
		{"internal/app/jobs.go", recipes.JobAnchor, "aps gen job"},
	}
	if d.res.Tenancy == recipes.TenancyMulti {
		anchors = append(anchors, struct{ file, anchor, generator string }{"internal/app/permissions.go", recipes.OrgPermissionsAnchor, "aps gen resource --scope org"})
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
		d.add(doctorFail, "anchor", ".env.example has no # aps:begin mail … # aps:end mail block", "put the block back: aps add mail replaces the provider's variables inside it")
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
		d.add(doctorWarn, ".env", "missing", "aps dev creates it from .env.example, or run: cp .env.example .env")
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
	d.add(doctorOK, ".env", "has every variable .env.example has", "")
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
	tmp, err := os.MkdirTemp("", "aps-doctor-")
	if err != nil {
		d.add(doctorWarn, "api files", err.Error(), "")
		return
	}
	defer os.RemoveAll(tmp)
	if _, errOut, err := doctorCommand(ctx, d.dir, env, "go", "run", "./cmd/api", "openapi", "--dir", tmp); err != nil {
		d.add(doctorWarn, "api files", "couldn't export them: "+firstLine(cmpOr(errOut, err.Error())), "check that the app builds (go build ./...); apps from before v0.5 get --dir with aps upgrade")
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
// migrate command, so aps needs no database driver.
func (d *doctor) database(ctx context.Context, env []string) {
	if _, err := os.Stat(d.path("cmd/migrate")); err != nil {
		return
	}
	out, errOut, err := doctorCommand(ctx, d.dir, env, "go", "run", "./cmd/migrate", "--status", "--json")
	var s struct {
		ConfigError   string `json:"config_error"`
		DatabaseError string `json:"database_error"`
		Current       int64  `json:"current"`
		Latest        int64  `json:"latest"`
		Pending       int    `json:"pending"`
	}
	if jsonErr := json.Unmarshal([]byte(out), &s); err != nil || jsonErr != nil {
		d.add(doctorWarn, "database", "couldn't read the migration status: "+firstLine(cmp.Or(errOut, errString(err), errString(jsonErr))), "check that the app builds; apps from before v0.5 get migrate --status with aps upgrade")
		return
	}
	switch {
	case s.ConfigError != "":
		d.add(doctorFail, "configuration", firstLine(s.ConfigError), "set the variables in .env or the environment; .env.example documents each")
	case s.DatabaseError != "":
		d.add(doctorWarn, "database", "unreachable: "+firstLine(s.DatabaseError), "start it with aps dev (or docker compose up -d --wait) and check DATABASE_URL")
	case s.Current > s.Latest:
		d.add(doctorFail, "database", fmt.Sprintf("at migration %d, but the newest file in db/migrations is %d", s.Current, s.Latest), "restore the missing migration files: the database ran migrations this code doesn't have")
	case s.Pending > 0:
		d.add(doctorWarn, "database", strconv.Itoa(s.Pending)+" migrations pending", "go run ./cmd/migrate (aps dev runs them)")
	default:
		d.add(doctorOK, "database", fmt.Sprintf("at migration %d, none pending", s.Current), "")
	}
}

func (d *doctor) report(w io.Writer) {
	s := newStyles(w)
	about := d.res.App
	if d.res.Preset != "" {
		about += fmt.Sprintf(" (%s, %s tenancy)", d.res.Preset, d.res.Tenancy)
	}
	fmt.Fprintf(w, "%s\n\n", s.strong.Render("aps doctor · "+about))
	for _, c := range d.res.Checks {
		status := c.Status
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
