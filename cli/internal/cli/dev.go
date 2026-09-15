package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"hash/fnv"
	"io"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

const devUsage = `Usage: aps dev [flags]

Builds and runs the app, rebuilding when files change. In an app with a
database (Full preset) it first creates .env from .env.example when missing,
starts PostgreSQL and Mailpit from compose.yaml with Docker, applies
migrations and runs seed data; changed migrations are applied before the
restart. Services keep running after aps dev stops: docker compose down
stops them, docker compose down -v also deletes their data.
`

// watchIgnored are directories never watched for changes.
var watchIgnored = map[string]bool{".git": true, ".aps": true, "bin": true, "node_modules": true, "vendor": true, "tmp": true}

// migrationsDir holds a Full app's migrations.
var migrationsDir = filepath.Join("db", "migrations")

func runDev(ctx context.Context, args []string, stderr io.Writer) error {
	flags := flag.NewFlagSet("aps dev", flag.ContinueOnError)
	flags.SetOutput(stderr)
	noReload := flags.Bool("no-reload", false, "build and run once, without watching for changes")
	interval := flags.Duration("interval", 500*time.Millisecond, "how often to check for changes")
	observability := flags.Bool("observability", false, "also start Grafana and send the app's traces, metrics and logs to it")
	noServices := flags.Bool("no-services", false, "don't start Docker services; use the PostgreSQL and SMTP addresses in .env as they are")
	flags.Usage = func() {
		fmt.Fprint(stderr, devUsage+"\nFlags:\n")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return usageError("aps dev takes no arguments")
	}
	manifest, err := os.ReadFile("apistock.yaml")
	if err != nil {
		return usageError("no apistock.yaml in this directory; run aps dev inside an app created with aps new")
	}

	d := newDevRunner(stderr)
	d.database = slices.Contains(manifestFeatures(manifest), "postgres")
	d.services = !*noServices
	d.observability = *observability
	if err := d.prepare(ctx); err != nil {
		return err
	}
	return d.loop(ctx, !*noReload, *interval)
}

type devRunner struct {
	out  io.Writer
	bin  string
	cmd  *exec.Cmd
	done chan error

	database      bool     // the app has PostgreSQL: migrate and seed
	services      bool     // start the database's Docker Compose services
	observability bool     // start Grafana and send telemetry to it
	extraEnv      []string // set for the app over the environment and .env

	// run runs a command with env, streaming its output; output runs one and
	// returns its standard output; lookPath finds a program. Tests replace
	// them.
	run      func(ctx context.Context, env []string, name string, args ...string) error
	output   func(ctx context.Context, env []string, name string, args ...string) ([]byte, error)
	lookPath func(file string) (string, error)
}

func newDevRunner(out io.Writer) *devRunner {
	return &devRunner{
		out: out,
		bin: filepath.Join(".aps", "api"),
		run: func(ctx context.Context, env []string, name string, args ...string) error {
			cmd := exec.CommandContext(ctx, name, args...)
			cmd.Env, cmd.Stdout, cmd.Stderr = env, out, out
			return cmd.Run()
		},
		output: func(ctx context.Context, env []string, name string, args ...string) ([]byte, error) {
			cmd := exec.CommandContext(ctx, name, args...)
			cmd.Env = env
			return cmd.Output()
		},
		lookPath: exec.LookPath,
	}
}

// composeService is a service in the app's compose.yaml and the .env
// variables holding its host ports.
type composeService struct {
	name  string
	ports []servicePort
}

type servicePort struct {
	env, def string
}

var (
	databaseServices = []composeService{
		{"postgres", []servicePort{{"POSTGRES_PORT", "5432"}}},
		{"mailpit", []servicePort{{"MAILPIT_SMTP_PORT", "1025"}, {"MAILPIT_WEB_PORT", "8025"}}},
	}
	grafanaService = composeService{"grafana", []servicePort{{"GRAFANA_PORT", "3000"}, {"OTLP_HTTP_PORT", "4318"}}}
)

// prepare gets everything the app needs before its first start (ADR-0028):
// .env, Docker Compose services, migrations and seed data.
func (d *devRunner) prepare(ctx context.Context) error {
	if d.database {
		created, err := copyIfMissing(".env.example", ".env")
		if err != nil {
			return err
		}
		if created {
			fmt.Fprintln(d.out, "aps: created .env from .env.example")
		}
	}
	env, err := devEnv(".env")
	if err != nil {
		return err
	}

	var services []composeService
	if d.database && d.services {
		services = append(services, databaseServices...)
	}
	if d.observability {
		services = append(services, grafanaService)
		d.extraEnv = append(d.extraEnv, "OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:"+envValue(env, "OTLP_HTTP_PORT", "4318"))
	}
	if len(services) > 0 {
		if err := d.startServices(ctx, env, services); err != nil {
			return err
		}
	}
	if d.database {
		if err := d.migrate(ctx, env); err != nil {
			return err
		}
		if err := d.seed(ctx, env); err != nil {
			return err
		}
	}
	d.banner(env)
	return nil
}

// startServices checks Docker and the services' host ports, then starts the
// services and waits until they are healthy.
func (d *devRunner) startServices(ctx context.Context, env []string, services []composeService) error {
	if _, err := os.Stat("compose.yaml"); err != nil {
		return errors.New("no compose.yaml in this directory: aps dev starts the services defined there")
	}
	if _, err := d.lookPath("docker"); err != nil {
		return errors.New(d.dockerHelp("Docker isn't installed"))
	}
	if _, err := d.output(ctx, env, "docker", "compose", "version"); err != nil {
		return errors.New(d.dockerHelp("Docker Compose v2 isn't available (docker compose version failed)"))
	}
	compose := []string{"compose"}
	if d.observability {
		compose = append(compose, "--profile", "observability")
	}
	running, err := d.output(ctx, env, "docker", slices.Concat(compose, []string{"ps", "--services", "--status", "running"})...)
	if err != nil {
		return errors.New(d.dockerHelp("Docker isn't running, or compose.yaml is invalid (docker compose ps failed)"))
	}
	// Ports held by this app's own running services aren't conflicts.
	up := strings.Fields(string(running))
	for _, s := range services {
		if slices.Contains(up, s.name) {
			continue
		}
		for _, p := range s.ports {
			if err := checkServicePort(s.name, p.env, envValue(env, p.env, p.def)); err != nil {
				return err
			}
		}
	}

	args := slices.Concat(compose, []string{"up", "-d", "--wait"})
	fmt.Fprintf(d.out, "aps: starting services (docker %s)\n", strings.Join(args, " "))
	if err := d.run(ctx, env, "docker", args...); err != nil {
		return fmt.Errorf("starting services failed: %w", err)
	}
	return nil
}

func (d *devRunner) dockerHelp(problem string) string {
	msg := problem + "\n  aps dev starts the services in compose.yaml with Docker: install Docker Desktop (or Docker Engine with Compose v2) and start it"
	if d.database && !d.observability {
		msg += ",\n  or set DATABASE_URL in .env to an existing PostgreSQL and run aps dev --no-services"
	}
	return msg
}

// checkServicePort reports a clear error when another program listens on a
// service's host port, naming the .env variable that moves it.
func checkServicePort(service, envVar, port string) error {
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", port))
	if err == nil {
		return ln.Close()
	}
	suggestion := "another port"
	if n, convErr := strconv.Atoi(port); convErr == nil {
		suggestion = strconv.Itoa(n + 10)
	}
	msg := fmt.Sprintf("port %s for %s is already in use by another program\n"+
		"  use another port by adding this line to .env:\n"+
		"    %s=%s", port, service, envVar, suggestion)
	switch envVar {
	case "POSTGRES_PORT":
		msg += "\n  and the same port in DATABASE_URL"
	case "MAILPIT_SMTP_PORT":
		msg += "\n  and the same port in MAILPIT_SMTP_ADDR"
	}
	return errors.New(msg)
}

func (d *devRunner) migrate(ctx context.Context, env []string) error {
	fmt.Fprintln(d.out, "aps: applying migrations (go run ./cmd/migrate)")
	if err := d.run(ctx, env, "go", "run", "./cmd/migrate"); err != nil {
		return fmt.Errorf("migrations failed: %w", err)
	}
	return nil
}

// seed runs the app's seed data command, which does nothing when its data
// is already there (ADR-0042). Apps without cmd/seed skip it.
func (d *devRunner) seed(ctx context.Context, env []string) error {
	if _, err := os.Stat(filepath.Join("cmd", "seed")); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	fmt.Fprintln(d.out, "aps: seed data (go run ./cmd/seed)")
	if err := d.run(ctx, env, "go", "run", "./cmd/seed"); err != nil {
		return fmt.Errorf("seed data failed: %w", err)
	}
	return nil
}

// banner prints where to find the app and its services.
func (d *devRunner) banner(env []string) {
	api := "http://" + appAddr(env)
	if host, port, err := net.SplitHostPort(appAddr(env)); err == nil && (host == "" || host == "0.0.0.0" || host == "::") {
		api = "http://" + net.JoinHostPort("127.0.0.1", port)
	}
	fmt.Fprintf(d.out, "\n  ✓ API        %s\n  ✓ API docs   %s/docs\n", api, api)
	if d.database && d.services {
		fmt.Fprintf(d.out, "  ✓ Emails     http://127.0.0.1:%s\n", envValue(env, "MAILPIT_WEB_PORT", "8025"))
	}
	if d.observability {
		fmt.Fprintf(d.out, "  ✓ Grafana    http://127.0.0.1:%s (traces, metrics and logs)\n", envValue(env, "GRAFANA_PORT", "3000"))
	} else if _, err := os.Stat("compose.yaml"); err == nil {
		fmt.Fprintln(d.out, "  Tip: aps dev --observability to see traces and logs")
	}
	fmt.Fprintln(d.out)
}

func (d *devRunner) loop(ctx context.Context, reload bool, interval time.Duration) error {
	if err := d.build(ctx); err != nil {
		return fmt.Errorf("build failed: %w", err)
	}
	if err := d.start(); err != nil {
		return err
	}
	defer d.stop()

	if !reload {
		select {
		case <-ctx.Done():
			return nil
		case err := <-d.done:
			d.cmd = nil
			return exitError(err)
		}
	}

	last, _ := snapshot(".", watched)
	lastSQL, _ := snapshot(migrationsDir, isSQL)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			fmt.Fprintln(d.out, "aps: stopping")
			return nil
		case err := <-d.done:
			fmt.Fprintf(d.out, "aps: app exited (%v); waiting for changes\n", exitError(err))
			d.cmd, d.done = nil, nil
		case <-ticker.C:
			cur, err := snapshot(".", watched)
			if err != nil || cur == last {
				continue
			}
			last = cur
			fmt.Fprintln(d.out, "aps: change detected, rebuilding")
			if err := d.build(ctx); err != nil {
				d.keepRunning("build failed")
				continue
			}
			if sql, _ := snapshot(migrationsDir, isSQL); d.database && sql != lastSQL {
				env, err := devEnv(".env")
				if err == nil {
					err = d.migrate(ctx, env)
				}
				if err != nil {
					d.keepRunning("migrations failed")
					continue
				}
				lastSQL = sql
			}
			d.stop()
			if err := d.start(); err != nil {
				fmt.Fprintf(d.out, "aps: start failed: %v\n", err)
			}
		}
	}
}

// keepRunning reports a failed rebuild step.
func (d *devRunner) keepRunning(what string) {
	if d.cmd != nil {
		fmt.Fprintf(d.out, "aps: %s; the previous version keeps running\n", what)
	} else {
		fmt.Fprintf(d.out, "aps: %s; fix the errors and save again\n", what)
	}
}

func (d *devRunner) build(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "go", "build", "-o", d.bin, "./cmd/api")
	cmd.Stdout, cmd.Stderr = d.out, d.out
	return cmd.Run()
}

func (d *devRunner) start() error {
	env, err := devEnv(".env")
	if err != nil {
		return err
	}
	if err := checkPortFree(appAddr(env)); err != nil {
		return err
	}
	cmd := exec.Command(d.bin)
	cmd.Env = append(env, d.extraEnv...) // later values win
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, d.out
	configureProcess(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start app: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	d.cmd, d.done = cmd, done
	return nil
}

// stop asks the app to shut down gracefully and kills it after 10 seconds.
func (d *devRunner) stop() {
	if d.cmd == nil {
		return
	}
	_ = interrupt(d.cmd)
	select {
	case <-d.done:
	case <-time.After(10 * time.Second):
		_ = d.cmd.Process.Kill()
		<-d.done
	}
	d.cmd, d.done = nil, nil
}

func exitError(err error) error {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return fmt.Errorf("exit status %d", ee.ExitCode())
	}
	return err
}

// snapshot fingerprints the files under dir whose names match: their paths,
// modification times and sizes.
func snapshot(dir string, match func(name string) bool) (uint64, error) {
	h := fnv.New64a()
	err := filepath.WalkDir(dir, func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if p != dir && watchIgnored[entry.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !match(entry.Name()) {
			return nil
		}
		info, err := entry.Info()
		if errors.Is(err, fs.ErrNotExist) {
			return nil // file vanished during the walk
		}
		if err != nil {
			return err
		}
		fmt.Fprintf(h, "%s|%d|%d\n", p, info.ModTime().UnixNano(), info.Size())
		return nil
	})
	return h.Sum64(), err
}

// watched reports whether a change to the file triggers a rebuild: Go
// sources, module files, .env and embedded assets.
func watched(name string) bool {
	switch {
	case strings.HasSuffix(name, ".go"), name == "go.mod", name == "go.sum", name == ".env":
		return true
	case strings.HasSuffix(name, ".html"), strings.HasSuffix(name, ".json"), isSQL(name):
		return true
	}
	return false
}

func isSQL(name string) bool { return strings.HasSuffix(name, ".sql") }

// envValue returns the last non-empty value of key in env, or def.
func envValue(env []string, key, def string) string {
	v := def
	for _, kv := range env {
		if s, ok := strings.CutPrefix(kv, key+"="); ok && s != "" {
			v = s
		}
	}
	return v
}

// copyIfMissing copies src to dst with mode 0600 when dst doesn't exist and
// src does, and reports whether it did.
func copyIfMissing(src, dst string) (bool, error) {
	if _, err := os.Stat(dst); err == nil {
		return false, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	data, err := os.ReadFile(src)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return true, os.WriteFile(dst, data, 0o600) //nolint:gosec // dst is a fixed file name (.env) in the app directory
}

// manifestFeatures returns the features listed in apistock.yaml, written as
// "features: [a, b]" or as a block list.
func manifestFeatures(manifest []byte) []string {
	var features []string
	inList := false
	for line := range strings.Lines(string(manifest)) {
		trimmed := strings.TrimSpace(line)
		if inList {
			if item, ok := strings.CutPrefix(trimmed, "- "); ok {
				features = append(features, strings.TrimSpace(item))
				continue
			}
			inList = false
		}
		rest, ok := strings.CutPrefix(line, "features:")
		if !ok {
			continue
		}
		rest = strings.TrimSpace(rest)
		if rest == "" {
			inList = true
			continue
		}
		rest = strings.TrimSuffix(strings.TrimPrefix(rest, "["), "]")
		for f := range strings.SplitSeq(rest, ",") {
			if f = strings.TrimSpace(f); f != "" {
				features = append(features, f)
			}
		}
	}
	return features
}
