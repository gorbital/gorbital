package cli

import (
	"bytes"
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
	"sync"
	"time"

	"golang.org/x/term"

	"gorbital.dev/cli/internal/devmail"
	"gorbital.dev/cli/internal/genplan"
	"gorbital.dev/cli/internal/pgmeta"
	"gorbital.dev/cli/internal/portal"
	"gorbital.dev/cli/internal/tunnel"
)

const devUsage = `Usage: orb dev [flags]

Builds and runs the app, rebuilding when files change. In an app with a
database (Full preset) it first creates .env from .env.example when missing,
starts PostgreSQL and Mailpit from compose.yaml with Docker, applies
migrations and runs seed data; changed migrations are applied before the
restart. Services keep running after orb dev stops: docker compose down
stops them, docker compose down -v also deletes their data.

It also serves the Dev Portal at http://127.0.0.1:3100 (DEV_PORTAL_PORT or
--portal-port to move it) and opens it in your browser: the app's state and
output, its routes, jobs, logs and email, and the generators, in one place
(docs/guides/dev-portal.md). Its link carries a token that is new on every
run; the portal answers only this machine.

--tunnel quick or --tunnel named also exposes the app, and only the app, on
a public HTTPS address with your own cloudflared (docs/dev-portal/tunnel.md):
quick gets a random trycloudflare.com URL that changes on every run (for
webhooks and phones); named uses the tunnel you created in the Cloudflare
dashboard, its token from CLOUDFLARE_TUNNEL_TOKEN in .env and its hostname
from --tunnel-hostname or ORB_TUNNEL_HOSTNAME (for Google, Apple and GitHub
sign-in and passkeys). The Dev Portal's Tunnel screen starts and stops it
too. Exit status 2 for a wrong --tunnel value, 1 when the tunnel can't start
(cloudflared missing, APP_ENV not development, a missing token or hostname).
`

// watchIgnored are directories never watched for changes.
var watchIgnored = map[string]bool{".git": true, ".orb": true, "bin": true, "node_modules": true, "vendor": true, "tmp": true}

// migrationsDir holds a Full app's migrations.
var migrationsDir = filepath.Join("db", "migrations")

func runDev(ctx context.Context, args []string, stderr io.Writer) error {
	flags := flag.NewFlagSet("orb dev", flag.ContinueOnError)
	flags.SetOutput(stderr)
	noReload := flags.Bool("no-reload", false, "build and run once, without watching for changes")
	interval := flags.Duration("interval", 500*time.Millisecond, "how often to check for changes")
	observability := flags.Bool("observability", false, "also start Grafana and send the app's traces, metrics and logs to it")
	noServices := flags.Bool("no-services", false, "don't start Docker services; use the PostgreSQL and SMTP addresses in .env as they are")
	portalPort := flags.String("portal-port", "", "port the Dev Portal listens on (default DEV_PORTAL_PORT in .env, or "+defaultPortalPort+")")
	noPortal := flags.Bool("no-portal", false, "don't serve the Dev Portal")
	noOpen := flags.Bool("no-open", false, "don't open the Dev Portal in a browser")
	tunnelMode := flags.String("tunnel", "", "expose the app on a public HTTPS address with cloudflared: quick or named")
	tunnelHostname := flags.String("tunnel-hostname", "", "a named tunnel's public hostname (default ORB_TUNNEL_HOSTNAME, or the one saved from the Dev Portal)")
	flags.Usage = func() {
		fmt.Fprint(stderr, devUsage+"\nFlags:\n")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() > 0 {
		return usageError("orb dev takes no arguments")
	}
	if *tunnelMode != "" {
		if _, err := tunnel.ParseMode(*tunnelMode); err != nil {
			return usageError("--tunnel: " + err.Error())
		}
	}
	if *tunnelHostname != "" && *tunnelMode != string(tunnel.ModeNamed) {
		return usageError("--tunnel-hostname is for --tunnel named")
	}
	manifest, err := os.ReadFile("gorbital.yaml")
	if err != nil {
		return usageError("no gorbital.yaml in this directory; run orb dev inside an app created with orb new")
	}

	d := newDevRunner(stderr)
	d.database = slices.Contains(manifestFeatures(manifest), "postgres")
	d.services = !*noServices
	d.observability = *observability
	d.reload = !*noReload
	d.portal = !*noPortal
	d.portalPortFlag = *portalPort
	d.openBrowser = !*noOpen
	d.tunnelMode = tunnel.Mode(*tunnelMode)
	if err := d.prepare(ctx); err != nil {
		return err
	}
	stopPortal, err := d.servePortal(ctx)
	if err != nil {
		return err
	}
	defer stopPortal()
	// The tunnel stops before the portal, on every way out of orb dev.
	defer d.tunnel.Close()
	if d.tunnelMode != "" {
		if err := d.tunnel.Start(ctx, tunnel.StartOptions{Mode: d.tunnelMode, Hostname: *tunnelHostname}); err != nil {
			return fmt.Errorf("the tunnel can't start: %w", err)
		}
	}
	return d.loop(ctx, !*noReload, *interval)
}

type devRunner struct {
	out    io.Writer // orb's own messages, also kept for the portal
	rawOut io.Writer // the same messages, without the portal copy
	bin    string
	cmd    *exec.Cmd
	done   chan error
	dir    string // the app directory

	database      bool     // the app has PostgreSQL: migrate and seed
	services      bool     // start the database's Docker Compose services
	observability bool     // start Grafana and send telemetry to it
	reload        bool     // watch the app's files and rebuild on change
	extraEnv      []string // set for the app over the environment and .env

	// migrations lists db/migrations with each file's state for the schema
	// status (ADR-0080); nil uses the portal's database connection. Tests
	// replace it.
	migrations func(ctx context.Context) ([]pgmeta.Migration, error)

	// consoleToken is the dev console token given to the app, or "";
	// consoleTokenFromEnv reports one taken from orb dev's environment.
	consoleToken        string
	consoleTokenFromEnv bool

	// The Dev Portal (ADR-0066): whether to serve it, its port (the flag,
	// else DEV_PORTAL_PORT, else the default), its per-run token, and
	// whether to open it in a browser. hub carries the app's output and
	// state to the portal; server is the portal while it runs.
	portal             bool
	portalPortFlag     string
	portalPort         string
	portalToken        string
	portalTokenFromEnv bool
	openBrowser        bool
	hub                *portal.Hub
	logs               *portal.LogStore      // the local log store while the portal runs (ADR-0072)
	system             *portal.SystemSampler // the machine and the app process (ADR-0073)
	mailStore          *devmail.Store        // the mail catcher's inbox, when it runs (ADR-0074)
	mailServer         *devmail.Server
	mailAddr           string
	// tunnel runs cloudflared (ADR-0086); tunnelMode is --tunnel's.
	tunnel     *tunnel.Manager
	tunnelMode tunnel.Mode
	server     *portal.Server
	open       func(url string) error // opens a URL in the browser; tests replace it
	// db is the portal's connection to the app's database, opened on the
	// first request that needs it (ADR-0067).
	dbMu sync.Mutex
	db   *pgmeta.Client

	// The app's state as the portal reports it (ADR-0066), guarded by mu:
	// loop changes it, portal requests read it.
	mu        sync.Mutex
	state     portal.State
	pid       int
	startedAt time.Time
	restarts  int
	problem   string
	addr      string // APP_ADDR as last read
	// lastMigrateError is the last migrate run's error, the schema
	// status's problem, until the next success.
	lastMigrateError string

	// commands carries restart, stop and start requests from the portal to
	// loop, which runs them between change checks.
	commands chan devCommand

	// run runs a command with env, streaming its output; output runs one and
	// returns its standard output; lookPath finds a program. Tests replace
	// them.
	run      func(ctx context.Context, env []string, name string, args ...string) error
	output   func(ctx context.Context, env []string, name string, args ...string) ([]byte, error)
	lookPath func(file string) (string, error)
}

// devCommand is a request from the portal to loop.
type devCommand string

const (
	commandRestart devCommand = "restart"
	commandStop    devCommand = "stop"
	commandStart   devCommand = "start"
	commandMigrate devCommand = "migrate"
	commandDown    devCommand = "migrate-down"
	commandRedo    devCommand = "migrate-redo"
	commandReset   devCommand = "reset-database"
)

func newDevRunner(out io.Writer) *devRunner {
	hub := portal.NewHub()
	dir, _ := os.Getwd()
	d := &devRunner{
		out:      io.MultiWriter(out, hub.Writer("orb")),
		rawOut:   out,
		bin:      filepath.Join(".orb", "api"),
		dir:      dir,
		hub:      hub,
		open:     openInBrowser,
		state:    portal.StatePreparing,
		commands: make(chan devCommand, 1),
		run: func(ctx context.Context, env []string, name string, args ...string) error {
			cmd := exec.CommandContext(ctx, name, args...)
			// The command's output (migrations, seeds, docker) goes to the
			// terminal and to the portal, so a failed migration's error is
			// readable there.
			cmd.Env, cmd.Stdout, cmd.Stderr = env, io.MultiWriter(out, hub.Writer("orb")), io.MultiWriter(out, hub.Writer("orb"))
			return cmd.Run()
		},
		output: func(ctx context.Context, env []string, name string, args ...string) ([]byte, error) {
			cmd := exec.CommandContext(ctx, name, args...)
			cmd.Env = env
			return cmd.Output()
		},
		lookPath: exec.LookPath,
	}
	d.tunnel = tunnel.New(tunnel.Config{
		Dir: dir,
		Env: func() []string {
			env, _ := devEnv(".env")
			return withAppEnv(env)
		},
		Target:   func() (string, error) { return d.Status().URL, nil },
		OnChange: hub.SetTunnel,
		Logf:     func(format string, args ...any) { fmt.Fprintf(d.out, format+"\n", args...) },
	})
	return d
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
		{"minio", []servicePort{{"MINIO_PORT", "9000"}, {"MINIO_CONSOLE_PORT", "9001"}}},
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
			fmt.Fprintln(d.out, "orb: created .env from .env.example")
		}
		wrote, err := ensureEncryptionKey(".env", ".env.example")
		if err != nil {
			return err
		}
		if wrote {
			fmt.Fprintln(d.out, "orb: wrote a random development AUTH_ENCRYPTION_KEYS to .env")
		}
	}
	env, err := devEnv(".env")
	if err != nil {
		return err
	}
	env = withAppEnv(env)

	// The dev console's token: in the app's environment only, printed once
	// below, never written to .env or any other file (ADR-0065).
	d.consoleToken, d.consoleTokenFromEnv, err = devConsoleToken(env, ".env.example")
	if err != nil {
		return err
	}
	if d.consoleToken != "" {
		d.extraEnv = append(d.extraEnv, devConsoleTokenVar+"="+d.consoleToken)
	}
	if d.portal {
		if err := d.preparePortal(env); err != nil {
			return err
		}
	}

	var services []composeService
	if d.database && d.services {
		services = append(services, composeServicesIn("compose.yaml", databaseServices)...)
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
		// The startup schema status is published here (ADR-0080): the hub
		// keeps it for the portal, which starts right after.
		if err := d.runMigrate(ctx, env, portal.SchemaSourceStartup, commandMigrate); err != nil {
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
		return errors.New("no compose.yaml in this directory: orb dev starts the services defined there")
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
	fmt.Fprintf(d.out, "orb: starting services (docker %s)\n", strings.Join(args, " "))
	if err := d.run(ctx, env, "docker", args...); err != nil {
		return fmt.Errorf("starting services failed: %w", err)
	}
	return nil
}

func (d *devRunner) dockerHelp(problem string) string {
	msg := problem + "\n  orb dev starts the services in compose.yaml with Docker: install Docker Desktop (or Docker Engine with Compose v2) and start it"
	if d.database && !d.observability {
		msg += ",\n  or set DATABASE_URL in .env to an existing PostgreSQL and run orb dev --no-services"
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

// migrateWith runs the app's migrate command with args, such as --down or
// --redo (development only, ADR-0069). runMigrate wraps it with the schema
// status (ADR-0080).
func (d *devRunner) migrateWith(ctx context.Context, env []string, args ...string) error {
	for _, all := range migrateCommands(".", args) {
		fmt.Fprintf(d.out, "orb: applying migrations (go %s)\n", strings.Join(all, " "))
		if err := d.run(ctx, env, "go", all...); err != nil {
			return fmt.Errorf("migrations failed: %w", err)
		}
	}
	return nil
}

// migrateCommands returns the go commands that run the app's migrations
// with cmd/migrate's flags: cmd/migrate itself in v0.1 apps, and the
// migrate and migrate-down commands of gorbital.Main in apps without it.
func migrateCommands(dir string, args []string) [][]string {
	if _, err := os.Stat(filepath.Join(dir, "cmd", "migrate")); err == nil || !isGorbitalApp(dir) {
		return [][]string{append([]string{"run", "./cmd/migrate"}, args...)}
	}
	switch {
	case slices.Contains(args, "--down"):
		return [][]string{{"run", "./cmd/api", "migrate-down"}}
	case slices.Contains(args, "--redo"):
		return [][]string{{"run", "./cmd/api", "migrate-down"}, {"run", "./cmd/api", "migrate"}}
	}
	return [][]string{append([]string{"run", "./cmd/api", "migrate"}, args...)}
}

// generateModules rewrites internal/modules/modules.gen.go before a build
// when the app has one and a module was added or removed (orb gen modules).
func (d *devRunner) generateModules() {
	if _, err := os.Stat(filepath.FromSlash(modulesGenPath)); err != nil {
		return
	}
	app, err := findAppIn(".")
	if err == nil {
		var plan genplan.Plan
		if plan, err = planModules(app); err == nil && len(plan.Changes) > 0 {
			if err = genplan.Apply(app.dir, plan); err == nil {
				fmt.Fprintf(d.out, "orb: updated %s (%s)\n", modulesGenPath, moduleCount(plan.Result.(genModulesResult).Modules))
			}
		}
	}
	if err != nil {
		fmt.Fprintf(d.out, "orb: couldn't update %s: %v\n", modulesGenPath, err)
	}
}

// seed runs the app's seed data command, which does nothing when its data
// is already there (ADR-0042). Apps without cmd/seed skip it.
func (d *devRunner) seed(ctx context.Context, env []string) error {
	if _, err := os.Stat(filepath.Join("cmd", "seed")); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	fmt.Fprintln(d.out, "orb: seed data (go run ./cmd/seed)")
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
	switch {
	case d.database && mailDelivery(env) == "devmail" && d.portal:
		fmt.Fprintf(d.out, "  ✓ Emails     %s/mail (caught at %s)\n", d.portalURL(), devMailAddr(env))
	case d.database && mailDelivery(env) == "devmail":
		fmt.Fprintf(d.out, "  ✓ Emails     caught at %s; the Dev Portal's Mail screen shows them (orb dev without --no-portal)\n", devMailAddr(env))
	case d.database && d.services && mailDelivery(env) == "mailpit":
		fmt.Fprintf(d.out, "  ✓ Emails     http://127.0.0.1:%s\n", envValue(env, "MAILPIT_WEB_PORT", "8025"))
	}
	if d.consoleToken != "" {
		fmt.Fprintf(d.out, "  ✓ Dev APIs   %s/_dev/ (docs/guides/dev-console.md)\n", api)
		if d.consoleTokenFromEnv {
			fmt.Fprintf(d.out, "    Token      %s from your environment\n", devConsoleTokenVar)
		} else {
			fmt.Fprintf(d.out, "    Token      %s (Authorization: Bearer; new on every orb dev run)\n", d.consoleToken)
		}
	}
	if d.portal {
		fmt.Fprintf(d.out, "  ✓ Dev Portal %s (docs/guides/dev-portal.md)\n", d.portalLink())
		if d.portalTokenFromEnv {
			fmt.Fprintf(d.out, "    Token      %s from your environment\n", devPortalTokenVar)
		} else if !d.openBrowser {
			fmt.Fprintln(d.out, "    Open the link above; it holds this run's token")
		}
	}
	if d.tunnelMode != "" {
		fmt.Fprintf(d.out, "  ✓ Tunnel     a %s tunnel to the app starts next; its URL follows (docs/dev-portal/tunnel.md)\n", d.tunnelMode)
	}
	if d.observability {
		fmt.Fprintf(d.out, "  ✓ Grafana    http://127.0.0.1:%s (traces, metrics and logs)\n", envValue(env, "GRAFANA_PORT", "3000"))
	} else if _, err := os.Stat("compose.yaml"); err == nil {
		fmt.Fprintln(d.out, "  Tip: orb dev --observability to see traces and logs")
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

	// Two snapshots of db/migrations: lastSQL is the one last migrated
	// (rebuild migrates when it differs), seenSQL the one last examined for
	// the schema status. Without reload only db/migrations is watched, and
	// a change is reported rather than applied (ADR-0080); the portal's
	// commands still run.
	var last uint64
	if reload {
		last, _ = snapshot(".", watched)
	}
	lastSQL, _ := snapshot(migrationsDir, isSQL)
	seenSQL := lastSQL
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			fmt.Fprintln(d.out, "orb: stopping")
			return nil
		case err := <-d.done:
			if !reload {
				d.exited(exitError(err))
				return exitError(err)
			}
			fmt.Fprintf(d.out, "orb: app exited (%v); waiting for changes\n", exitError(err))
			d.exited(exitError(err))
		case c := <-d.commands:
			migrated := lastSQL
			d.runCommand(ctx, c, &lastSQL)
			if lastSQL != migrated {
				seenSQL = lastSQL
			}
		case <-ticker.C:
			if reload {
				cur, err := snapshot(".", watched)
				if err == nil && cur != last {
					last = cur
					fmt.Fprintln(d.out, "orb: change detected, rebuilding")
					d.rebuild(ctx, &lastSQL)
					seenSQL, _ = snapshot(migrationsDir, isSQL)
					continue
				}
			}
			if d.database && migrationsChanged(&seenSQL) && seenSQL != lastSQL {
				d.schemaChangedInCode(ctx)
			}
		}
	}
}

// runCommand runs a request from the portal.
func (d *devRunner) runCommand(ctx context.Context, c devCommand, lastSQL *uint64) {
	switch c {
	case commandRestart:
		fmt.Fprintln(d.out, "orb: restart requested from the Dev Portal, rebuilding")
		d.rebuild(ctx, lastSQL)
	case commandStop:
		if d.cmd == nil {
			return
		}
		fmt.Fprintln(d.out, "orb: stop requested from the Dev Portal")
		d.stop()
		d.setState(portal.StateStopped, "")
	case commandStart:
		if d.cmd != nil {
			return
		}
		fmt.Fprintln(d.out, "orb: start requested from the Dev Portal")
		if err := d.start(); err != nil {
			fmt.Fprintf(d.out, "orb: start failed: %v\n", err)
			d.setState(portal.StateStopped, err.Error())
		}
	case commandMigrate, commandDown, commandRedo, commandReset:
		fmt.Fprintf(d.out, "orb: %s requested from the Dev Portal\n", c)
		env, err := devEnv(".env")
		if err == nil {
			err = d.runMigrate(ctx, withAppEnv(env), portal.SchemaSourcePortal, c)
		}
		if err == nil && c == commandReset {
			err = d.seed(ctx, withAppEnv(env))
		}
		if err != nil {
			d.setStateAfterFailure("migrations failed: " + err.Error())
			return
		}
		if sql, err := snapshot(migrationsDir, isSQL); err == nil {
			*lastSQL = sql
		}
		d.setStateAfterFailure("")
	}
}

// rebuild builds the app, applies changed migrations and swaps the running
// process for the new build. A failed build or migration leaves the previous
// version running and reports the problem.
func (d *devRunner) rebuild(ctx context.Context, lastSQL *uint64) {
	d.setState(portal.StateBuilding, "")
	if err := d.build(ctx); err != nil {
		d.keepRunning("build failed")
		d.setStateAfterFailure("build failed: " + err.Error())
		// Migrations that changed with the code wait for the next
		// successful build: say so (ADR-0080).
		if sql, _ := snapshot(migrationsDir, isSQL); d.database && sql != *lastSQL {
			d.schemaChangedInCode(ctx)
		}
		return
	}
	if sql, _ := snapshot(migrationsDir, isSQL); d.database && sql != *lastSQL {
		env, err := devEnv(".env")
		if err == nil {
			err = d.runMigrate(ctx, withAppEnv(env), portal.SchemaSourceMigrate, commandMigrate)
		}
		if err != nil {
			d.keepRunning("migrations failed")
			d.setStateAfterFailure("migrations failed: " + err.Error())
			return
		}
		*lastSQL = sql
	}
	d.stop()
	if err := d.start(); err != nil {
		fmt.Fprintf(d.out, "orb: start failed: %v\n", err)
		d.setState(portal.StateStopped, err.Error())
	}
}

// keepRunning reports a failed rebuild step.
func (d *devRunner) keepRunning(what string) {
	if d.cmd != nil {
		fmt.Fprintf(d.out, "orb: %s; the previous version keeps running\n", what)
	} else {
		fmt.Fprintf(d.out, "orb: %s; fix the errors and save again\n", what)
	}
}

func (d *devRunner) build(ctx context.Context) error {
	d.generateModules()
	cmd := exec.CommandContext(ctx, "go", "build", "-o", d.bin, "./cmd/api")
	cmd.Stdout, cmd.Stderr = d.out, d.out
	return cmd.Run()
}

// setState records the app's state and problem and tells the portal.
func (d *devRunner) setState(state portal.State, problem string) {
	d.mu.Lock()
	d.state, d.problem = state, problem
	server := d.server
	d.mu.Unlock()
	d.hub.SetState(d.Status())
	if server != nil {
		server.CloseIdleConnections() // the app's port may have changed hands
	}
}

// setStateAfterFailure records a failed rebuild: the previous version keeps
// running if it was, else the app stays stopped.
func (d *devRunner) setStateAfterFailure(problem string) {
	state := portal.StateStopped
	if d.cmd != nil {
		state = portal.StateRunning
	}
	d.setState(state, problem)
}

// exited records that the app process ended on its own.
func (d *devRunner) exited(err error) {
	d.cmd, d.done = nil, nil
	problem := ""
	if err != nil {
		problem = "the app exited: " + err.Error()
	}
	d.setState(portal.StateStopped, problem)
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
	cmd.Env = d.appEnv(env)
	// The app logs JSON (appEnv). The log store gets the JSON as it is;
	// the terminal and the portal's console get it rendered for people
	// (portal.RenderPretty), with colours only on a terminal.
	console := &textRenderer{w: d.hub.Writer("app"), console: true}
	stdout := io.Writer(newTextRenderer(os.Stdout, isTerminal(os.Stdout)))
	stderr := io.Writer(newTextRenderer(d.rawOut, isTerminal(d.rawOut)))
	if d.logs != nil {
		stdout = io.MultiWriter(stdout, d.logs.Writer("app"))
		stderr = io.MultiWriter(stderr, d.logs.Writer("app"))
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, io.MultiWriter(stdout, console), io.MultiWriter(stderr, console)
	configureProcess(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start app: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	d.cmd, d.done = cmd, done
	d.mu.Lock()
	if !d.startedAt.IsZero() {
		d.restarts++
	}
	d.pid, d.startedAt, d.addr = cmd.Process.Pid, time.Now(), appAddr(env)
	d.mu.Unlock()
	d.setState(portal.StateRunning, "")
	// A quick tunnel follows the app to a new port.
	d.tunnel.TargetChanged(context.Background())
	return nil
}

// appEnv returns the app process's environment: env with APP_ENV and,
// unless .env chose one, APP_LOG_FORMAT=json so the portal's log store
// reads structured records (ADR-0072; the terminal still shows text),
// then extraEnv, whose later values win.
func (d *devRunner) appEnv(env []string) []string {
	env = withAppEnv(env)
	if envValue(env, "APP_LOG_FORMAT", "") == "" {
		env = append(env, "APP_LOG_FORMAT=json")
	}
	// The tunnel's token is cloudflared's, not the app's (ADR-0086).
	env = slices.DeleteFunc(env, func(kv string) bool {
		return strings.HasPrefix(kv, tunnel.TokenVar+"=") || strings.HasPrefix(kv, tunnel.TokenFileVar+"=")
	})
	return append(env, d.extraEnv...)
}

// textRenderer writes JSON log lines as readable text (see
// portal.RenderPretty), with colours when color; other lines pass
// unchanged. Partial lines wait for their newline.
type textRenderer struct {
	w     io.Writer
	buf   []byte
	color bool
	// console renders without the time: the portal's console shows its own.
	console bool
}

func newTextRenderer(w io.Writer, color bool) *textRenderer { return &textRenderer{w: w, color: color} }

// isTerminal reports whether w is a terminal, for colours.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && term.IsTerminal(int(f.Fd())) //nolint:gosec // a file descriptor fits
}

func (t *textRenderer) Write(p []byte) (int, error) {
	t.buf = append(t.buf, p...)
	for {
		i := bytes.IndexByte(t.buf, '\n')
		if i < 0 {
			break
		}
		line := string(t.buf[:i])
		t.buf = t.buf[i+1:]
		rendered := portal.RenderPretty(line, t.color)
		if t.console {
			rendered = portal.RenderPrettyLine(line)
		}
		if _, err := io.WriteString(t.w, rendered+"\n"); err != nil {
			return len(p), err
		}
	}
	if len(t.buf) > 64<<10 { // a line without end: pass it through
		if _, err := t.w.Write(t.buf); err != nil {
			return len(p), err
		}
		t.buf = t.buf[:0]
	}
	return len(p), nil
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
	d.mu.Lock()
	d.pid = 0
	d.mu.Unlock()
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

// withAppEnv adds APP_ENV=development when neither the environment nor .env
// sets it: apps refuse to start without APP_ENV, and orb dev runs them for
// development.
func withAppEnv(env []string) []string {
	if envValue(env, "APP_ENV", "") == "" {
		return append(env, "APP_ENV=development")
	}
	return env
}

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

// manifestFeatures returns the features listed in gorbital.yaml, written as
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

// composeServicesIn keeps PostgreSQL and the optional services compose.yaml
// defines: Mailpit (apps made before ADR-0074) and MinIO (orb add storage
// --driver minio, ADR-0075).
func composeServicesIn(path string, services []composeService) []composeService {
	data, err := os.ReadFile(path)
	if err != nil {
		return services
	}
	var out []composeService
	for _, s := range services {
		if s.name != "postgres" && !strings.Contains(string(data), "\n  "+s.name+":") {
			continue // optional services: Mailpit (older apps), MinIO (orb add storage)
		}
		out = append(out, s)
	}
	return out
}

// mailDelivery is MAIL_DELIVERY as the app resolves it: devmail in
// development unless set.
func mailDelivery(env []string) string {
	if v := envValue(env, "MAIL_DELIVERY", ""); v != "" {
		return v
	}
	if envValue(env, "APP_ENV", "development") == "production" {
		return "provider"
	}
	return "devmail"
}

// devMailAddr is where the mail catcher listens (DEV_MAIL_SMTP_ADDR).
func devMailAddr(env []string) string { return envValue(env, "DEV_MAIL_SMTP_ADDR", "127.0.0.1:1025") }
