package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"

	"gorbital.dev/cli/internal/devmail"
	"gorbital.dev/cli/internal/genplan"
	"gorbital.dev/cli/internal/pgmeta"
	"gorbital.dev/cli/internal/portal"
	"gorbital.dev/cli/internal/portal/ui"
)

// The Dev Portal (ADR-0066): orb dev serves the portal's UI and API on a
// loopback port next to the app. The portal's token is new on every run and
// travels only in the link orb dev prints and opens; the app's dev console
// token (ADR-0065) stays inside orb dev, which adds it to proxied /_dev/
// requests.

const (
	// devPortalTokenVar, when set in orb dev's own environment, is used as
	// the portal token instead of a random one, for tools that need the same
	// token across runs. It is never written to .env.
	devPortalTokenVar = "DEV_PORTAL_TOKEN" //nolint:gosec // the variable's name, not a credential
	// devPortalPortVar in .env (or the environment) moves the portal's port.
	devPortalPortVar = "DEV_PORTAL_PORT"
	// defaultPortalPort is where the portal listens unless told otherwise.
	defaultPortalPort = "3100"
)

// preparePortal chooses the portal's port and token before the banner, and
// checks the port is free, so a conflict stops orb dev with a clear message
// like the other ports.
func (d *devRunner) preparePortal(env []string) error {
	port := d.portalPortFlag
	if port == "" {
		port = envValue(env, devPortalPortVar, defaultPortalPort)
	}
	if n, err := strconv.ParseUint(port, 10, 16); err != nil || n == 0 {
		return usageError(fmt.Sprintf("the Dev Portal port must be a port number, got %q (--portal-port or %s in .env)", port, devPortalPortVar))
	}
	if err := checkServicePort("the Dev Portal", devPortalPortVar, port); err != nil {
		return fmt.Errorf("%w\n  or pass --portal-port, or --no-portal", err)
	}
	d.portalPort = port
	if t := os.Getenv(devPortalTokenVar); t != "" {
		if err := portal.CheckToken(t); err != nil {
			return fmt.Errorf("%s in your environment: %w", devPortalTokenVar, err)
		}
		d.portalToken, d.portalTokenFromEnv = t, true
	} else {
		d.portalToken = newDevConsoleToken() // the same shape: 256 random bits
	}
	return nil
}

// portalURL is where the portal listens.
func (d *devRunner) portalURL() string {
	return "http://" + net.JoinHostPort("127.0.0.1", d.portalPort)
}

// portalLink is the link that signs the browser in: the portal's address
// with this run's token.
func (d *devRunner) portalLink() string {
	return d.portalURL() + portal.AuthPath + "?t=" + d.portalToken
}

// servePortal starts the portal server and, unless told not to, opens its
// link in the browser. It returns a function that stops the server.
func (d *devRunner) servePortal(ctx context.Context) (func(), error) {
	if !d.portal {
		return func() {}, nil
	}
	d.system = portal.NewSystemSampler(d.dir, func() int { return d.Status().PID })
	if err := d.startMailCatcher(); err != nil {
		return nil, err
	}
	logs, err := portal.NewLogStore(d.dir)
	if err != nil {
		return nil, fmt.Errorf("the Dev Portal's log store: %w", err)
	}
	d.logs = logs
	server, err := portal.New(d.portalConfig())
	if err != nil {
		_ = logs.Close()
		return nil, err
	}
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", d.portalPort))
	if err != nil {
		_ = logs.Close()
		return nil, fmt.Errorf("the Dev Portal can't listen on port %s: %w", d.portalPort, err)
	}
	// Every output line (the app's, orb's) goes to the store; with
	// services, the PostgreSQL container's log too (ADR-0072).
	stopLogs := make(chan struct{})
	go logs.Follow(d.hub, stopLogs)
	logsCtx, cancelLogs := context.WithCancel(ctx)
	go d.system.Run(logsCtx)
	if d.database && d.services {
		go d.followPostgresLogs(logsCtx, logs)
	}
	httpServer := &http.Server{
		Handler:           server.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	go func() {
		if err := httpServer.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintf(d.out, "orb: the Dev Portal stopped: %v\n", err)
		}
	}()
	d.server = server
	if d.openBrowser && shouldOpenBrowser(d.rawOut) {
		if err := d.open(d.portalLink()); err != nil {
			fmt.Fprintf(d.out, "orb: couldn't open a browser (%v); open the Dev Portal link above yourself\n", err)
		}
	}
	return func() {
		server.Close()
		d.closeDatabase()
		shutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdown)
		cancelLogs()
		close(stopLogs)
		_ = logs.Close()
		if d.mailServer != nil {
			_ = d.mailServer.Close()
		}
	}, nil
}

// startMailCatcher starts the SMTP catcher when the app sends its email
// to devmail (ADR-0074): a Full app in development whose .env doesn't
// choose mailpit or the provider.
func (d *devRunner) startMailCatcher() error {
	if !d.database {
		return nil
	}
	env, err := devEnv(".env")
	if err != nil {
		return err
	}
	if mailDelivery(env) != "devmail" {
		return nil
	}
	store, err := devmail.Open(d.dir)
	if err != nil {
		return fmt.Errorf("the mail catcher's store: %w", err)
	}
	server := devmail.NewServer(store)
	addr := devMailAddr(env)
	if err := server.Listen(addr); err != nil {
		return fmt.Errorf("the mail catcher can't listen on %s (DEV_MAIL_SMTP_ADDR): %w", addr, err)
	}
	d.mailStore, d.mailServer, d.mailAddr = store, server, addr
	return nil
}

// followPostgresLogs streams the postgres service's log into the store
// while ctx lasts (docker compose logs -f). Docker being absent or the
// service stopped ends it quietly; the store then has no postgres source.
func (d *devRunner) followPostgresLogs(ctx context.Context, logs *portal.LogStore) {
	env, err := devEnv(".env")
	if err != nil {
		return
	}
	cmd := exec.CommandContext(ctx, "docker", "compose", "logs", "-f", "--no-log-prefix", "--no-color", "--since", "1s", "postgres")
	cmd.Env = withAppEnv(env)
	cmd.Dir = d.dir
	out, err := cmd.StdoutPipe()
	if err != nil {
		return
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return
	}
	_ = logs.IngestReader(portal.SourcePostgres, out)
	_ = cmd.Wait()
}

// portalConfig builds the portal's configuration from the app and this run.
func (d *devRunner) portalConfig() portal.Config {
	env, _ := devEnv(".env")
	env = withAppEnv(env)
	api := "http://" + reachableAddr(appAddr(env))
	links := map[string]string{"api": api, "docs": api + "/docs"}
	if d.database && d.services && mailDelivery(env) == "mailpit" {
		links["mail"] = "http://127.0.0.1:" + envValue(env, "MAILPIT_WEB_PORT", "8025")
	}
	if d.consoleToken != "" {
		links["console"] = api + "/_dev/"
	}
	if d.observability {
		links["grafana"] = "http://127.0.0.1:" + envValue(env, "GRAFANA_PORT", "3000")
	}
	return portal.Config{
		Token:        d.portalToken,
		Version:      Version,
		Project:      d.project(),
		Supervisor:   d,
		Hub:          d.hub,
		ConsoleToken: d.consoleToken,
		Links:        links,
		Generators:   d.generators(),
		Jobs:         func() ([]portal.JobSource, error) { return jobSources(d.dir) },
		Logs:         d.logs,
		System:       d.system,
		Mail:         portal.MailConfig{Store: d.mailStore, SMTPAddr: d.mailAddr},
		Env:          portal.NewEnvEditor(d.dir),
		Git:          portal.NewGit(d.dir),
		OpenInEditor: func(path string, line int) error { return openInEditor(d.dir, path, line) },
		Health:       d.health,
		Database:     d.databaseConfig(),
		SQL:          d.sqlStore(),
		UI:           ui.FS(),
		Logf:         func(format string, args ...any) { fmt.Fprintf(d.out, format+"\n", args...) },
	}
}

// databaseConfig connects the portal's Table Editor and Schema pages to
// the app's database (ADR-0067): DATABASE_URL from .env, opened on first
// use and kept for the run. Apps without a database get nothing.
func (d *devRunner) databaseConfig() portal.DatabaseConfig {
	if !d.database {
		return portal.DatabaseConfig{}
	}
	return portal.DatabaseConfig{
		Open: func(ctx context.Context) (portal.Database, error) {
			d.dbMu.Lock()
			defer d.dbMu.Unlock()
			if d.db != nil {
				return d.db, nil
			}
			env, err := devEnv(".env")
			if err != nil {
				return nil, err
			}
			url := envValue(env, "DATABASE_URL", "")
			if url == "" {
				return nil, errors.New("DATABASE_URL isn't set in .env")
			}
			client, err := pgmeta.Open(ctx, url)
			if err != nil {
				return nil, err
			}
			if err := client.Ping(ctx); err != nil {
				client.Close()
				return nil, err
			}
			d.db = client
			return client, nil
		},
		NextMigrationVersion: d.nextMigrationVersion,
		Apply: func(ctx context.Context, plan genplan.Plan, allowDirty bool) error {
			if !allowDirty {
				if err := requireCleanGit(ctx, d.dir); err != nil {
					return err
				}
			}
			if err := genplan.Apply(d.dir, plan); err != nil {
				return err
			}
			return d.Migrate()
		},
	}
}

// sqlStore returns the SQL editor's store, or nil without a database.
func (d *devRunner) sqlStore() *portal.SQLStore {
	if !d.database {
		return nil
	}
	return portal.NewSQLStore(d.dir)
}

// closeDatabase releases the portal's database connection.
func (d *devRunner) closeDatabase() {
	d.dbMu.Lock()
	defer d.dbMu.Unlock()
	if d.db != nil {
		d.db.Close()
		d.db = nil
	}
}

// reachableAddr returns addr with a wildcard host replaced by 127.0.0.1.
func reachableAddr(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		return net.JoinHostPort("127.0.0.1", port)
	}
	return addr
}

// project describes the app from gorbital.yaml and go.mod, tolerating a
// manifest with fewer keys than orb new writes.
func (d *devRunner) project() portal.Project {
	p := portal.Project{Dir: d.dir, Database: d.database, Features: []string{}}
	if manifest, err := os.ReadFile(filepath.Join(d.dir, manifestPath)); err == nil {
		if features := manifestFeatures(manifest); features != nil {
			p.Features = features
		}
		for line := range strings.Lines(string(manifest)) {
			key, value, ok := strings.Cut(line, ":")
			if !ok || strings.HasPrefix(line, " ") || strings.HasPrefix(line, "#") {
				continue
			}
			value = strings.TrimSpace(value)
			switch key {
			case "name":
				p.Name = value
			case "module":
				p.Module = value
			case "preset":
				p.Preset = value
			case "tenancy":
				p.Tenancy = value
			case "mail":
				p.Mail = value
			}
		}
	}
	if p.Module == "" {
		if data, err := os.ReadFile(filepath.Join(d.dir, "go.mod")); err == nil {
			p.Module = modulePath(data)
		}
	}
	if p.Name == "" {
		p.Name = filepath.Base(d.dir)
	}
	return p
}

// Status implements portal.Supervisor.
func (d *devRunner) Status() portal.AppStatus {
	d.mu.Lock()
	defer d.mu.Unlock()
	addr := d.addr
	if addr == "" {
		env, _ := devEnv(".env")
		addr = appAddr(env)
	}
	s := portal.AppStatus{
		State: d.state, Addr: addr, URL: "http://" + reachableAddr(addr),
		Restarts: d.restarts, Problem: d.problem, Console: d.consoleToken != "",
	}
	if d.pid != 0 {
		started := d.startedAt
		s.PID, s.StartedAt = d.pid, &started
	}
	return s
}

// Restart implements portal.Supervisor: loop rebuilds and restarts the app.
func (d *devRunner) Restart() error { return d.request(commandRestart) }

// Stop implements portal.Supervisor.
func (d *devRunner) Stop() error { return d.request(commandStop) }

// Start implements portal.Supervisor.
func (d *devRunner) Start() error { return d.request(commandStart) }

// Migrate implements portal.Supervisor: loop applies pending migrations.
func (d *devRunner) Migrate() error { return d.requestMigration(commandMigrate) }

// MigrateDown implements portal.Supervisor: loop rolls back one migration.
func (d *devRunner) MigrateDown() error { return d.requestMigration(commandDown) }

// MigrateRedo implements portal.Supervisor: loop rolls back one migration
// and applies it again.
func (d *devRunner) MigrateRedo() error { return d.requestMigration(commandRedo) }

func (d *devRunner) requestMigration(c devCommand) error {
	if !d.database {
		return errors.New("this app has no database: migrations apply to apps created with the Full preset")
	}
	return d.request(c)
}

// request queues a command for loop, refusing a second one while the
// first waits.
func (d *devRunner) request(c devCommand) error {
	select {
	case d.commands <- c:
		return nil
	default:
		return errors.New("orb dev is still handling the previous request; try again in a moment")
	}
}

// generators wires the portal's generators to the same plans orb gen uses.
func (d *devRunner) generators() map[string]portal.Generator {
	app := func() (appInfo, error) { return findAppIn(d.dir) }
	apply := func(ctx context.Context, allowDirty bool, plan genplan.Plan) error {
		if !allowDirty {
			if err := requireCleanGit(ctx, d.dir); err != nil {
				return err
			}
		}
		return genplan.Apply(d.dir, plan)
	}
	return map[string]portal.Generator{
		"job": {
			Plan: func(_ context.Context, input json.RawMessage) (genplan.Plan, error) {
				a, in, err := portalJobInput(app, input)
				if err != nil {
					return genplan.Plan{}, err
				}
				return planJob(a, in)
			},
			Apply: func(ctx context.Context, input json.RawMessage, allowDirty bool) (genplan.Plan, error) {
				a, in, err := portalJobInput(app, input)
				if err != nil {
					return genplan.Plan{}, err
				}
				plan, err := planJob(a, in)
				if err != nil {
					return genplan.Plan{}, err
				}
				return plan, apply(ctx, allowDirty, plan)
			},
		},
		"migration": {
			Plan: func(_ context.Context, input json.RawMessage) (genplan.Plan, error) {
				a, in, err := portalMigrationInput(app, input)
				if err != nil {
					return genplan.Plan{}, err
				}
				return planMigration(a, in.Name, time.Now())
			},
			Apply: func(ctx context.Context, input json.RawMessage, allowDirty bool) (genplan.Plan, error) {
				a, in, err := portalMigrationInput(app, input)
				if err != nil {
					return genplan.Plan{}, err
				}
				plan, err := planMigration(a, in.Name, time.Now())
				if err != nil {
					return genplan.Plan{}, err
				}
				return plan, apply(ctx, allowDirty, plan)
			},
		},
		"resource": {
			Plan: func(_ context.Context, input json.RawMessage) (genplan.Plan, error) {
				a, in, err := portalResourceInput(app, input)
				if err != nil {
					return genplan.Plan{}, err
				}
				plan, _, err := planResource(a, in, time.Now())
				return plan, err
			},
			Apply: func(ctx context.Context, input json.RawMessage, allowDirty bool) (genplan.Plan, error) {
				a, in, err := portalResourceInput(app, input)
				if err != nil {
					return genplan.Plan{}, err
				}
				plan, _, err := planResource(a, in, time.Now())
				if err != nil {
					return genplan.Plan{}, err
				}
				return plan, apply(ctx, allowDirty, plan)
			},
		},
	}
}

// jobInputJSON is orb gen job's answers as the portal sends them: the
// flags' names with underscores, and the flags' defaults when absent.
type jobInputJSON struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// Trigger is schedule, interval or manual; empty means schedule when
	// Schedule is set, interval when Every is set, else a daily schedule.
	Trigger     string `json:"trigger"`
	Schedule    string `json:"schedule"`
	Every       string `json:"every"`
	Timeout     string `json:"timeout"`
	MaxAttempts int    `json:"max_attempts"`
	Queue       string `json:"queue"`
	Priority    int    `json:"priority"`
	Disabled    bool   `json:"disabled"`
	// The kind and its fields (ADR-0071); kind defaults to custom.
	Kind     string `json:"kind"`
	Method   string `json:"method"`
	URL      string `json:"url"`
	Body     string `json:"body"`
	SQL      string `json:"sql"`
	To       string `json:"to"`
	Subject  string `json:"subject"`
	Text     string `json:"text"`
	Dispatch string `json:"dispatch"`
}

func decodeInput[T any](input json.RawMessage, v *T) error {
	dec := json.NewDecoder(strings.NewReader(string(input)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return usageError("invalid input: " + err.Error())
	}
	return nil
}

func portalJobInput(app func() (appInfo, error), input json.RawMessage) (appInfo, jobInput, error) {
	a, err := app()
	if err != nil {
		return appInfo{}, jobInput{}, err
	}
	in := jobInputJSON{Timeout: "1m", MaxAttempts: 5, Queue: "default", Priority: 1}
	if err := decodeInput(input, &in); err != nil {
		return appInfo{}, jobInput{}, err
	}
	if strings.TrimSpace(in.Name) == "" {
		return appInfo{}, jobInput{}, usageError("missing job name")
	}
	trigger := in.Trigger
	switch {
	case trigger != "" && trigger != triggerSchedule && trigger != triggerInterval && trigger != triggerManual:
		return appInfo{}, jobInput{}, usageError(fmt.Sprintf("unknown trigger %q (want schedule, interval or manual)", trigger))
	case trigger == "" && in.Every != "":
		trigger = triggerInterval
	case trigger == "":
		trigger = triggerSchedule
		if in.Schedule == "" {
			in.Schedule = "0 3 * * *"
		}
	}
	return a, jobInput{
		name: in.Name, description: in.Description, trigger: trigger, schedule: in.Schedule, every: in.Every,
		timeout: in.Timeout, maxAttempts: in.MaxAttempts, queue: in.Queue, priority: in.Priority, enabled: !in.Disabled,
		kind: in.Kind, httpMethod: in.Method, httpURL: in.URL, httpBody: in.Body, sql: in.SQL,
		emailTo: in.To, emailSubject: in.Subject, emailText: in.Text, dispatchTarget: in.Dispatch,
	}, nil
}

// migrationInputJSON is orb gen migration's answer.
type migrationInputJSON struct {
	Name string `json:"name"`
}

func portalMigrationInput(app func() (appInfo, error), input json.RawMessage) (appInfo, migrationInputJSON, error) {
	a, err := app()
	if err != nil {
		return appInfo{}, migrationInputJSON{}, err
	}
	var in migrationInputJSON
	if err := decodeInput(input, &in); err != nil {
		return appInfo{}, migrationInputJSON{}, err
	}
	if strings.TrimSpace(in.Name) == "" {
		return appInfo{}, migrationInputJSON{}, usageError("missing migration name")
	}
	return a, in, nil
}

// resourceInputJSON is orb gen resource's answers.
type resourceInputJSON struct {
	Name string `json:"name"`
	// Fields are specs as on the command line: name:string:unique.
	Fields   []string `json:"fields"`
	Plural   string   `json:"plural"`
	IDPrefix string   `json:"id_prefix"`
	Scope    string   `json:"scope"`
}

func portalResourceInput(app func() (appInfo, error), input json.RawMessage) (appInfo, resourceInput, error) {
	a, err := app()
	if err != nil {
		return appInfo{}, resourceInput{}, err
	}
	var in resourceInputJSON
	if err := decodeInput(input, &in); err != nil {
		return appInfo{}, resourceInput{}, err
	}
	return a, resourceInput{name: in.Name, specs: in.Fields, plural: in.Plural, idPrefix: in.IDPrefix, scope: in.Scope}, nil
}

// shouldOpenBrowser reports whether orb dev may open a browser: only when
// its output is a terminal and it isn't running in CI.
func shouldOpenBrowser(out any) bool {
	if os.Getenv("CI") != "" {
		return false
	}
	f, ok := out.(*os.File)
	return ok && term.IsTerminal(int(f.Fd())) //nolint:gosec // file descriptors fit in int
}

// openInBrowser opens url with the platform's opener, without waiting.
func openInBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

// nextMigrationVersion is the next migration file's version: after every
// file's and, when the database answers, after every version goose has
// applied, so a version whose file was deleted (a rolled-back experiment,
// another checkout) isn't reused and silently skipped.
func (d *devRunner) nextMigrationVersion() (string, error) {
	version, err := nextMigrationVersion(d.dir, time.Now())
	if err != nil {
		return "", err
	}
	open := d.databaseConfig().Open
	if open == nil {
		return version, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	db, err := open(ctx)
	if err != nil {
		return version, nil //nolint:nilerr // without the database, the files decide
	}
	migrations, err := db.Migrations(ctx, d.dir)
	if err != nil {
		return version, nil //nolint:nilerr // same
	}
	applied := map[string]bool{}
	for _, m := range migrations {
		if m.Applied {
			applied[strconv.FormatInt(m.Version, 10)] = true
		}
	}
	for applied[version] {
		n, err := strconv.ParseInt(version, 10, 64)
		if err != nil {
			return version, nil //nolint:nilerr // not a numeric version: nothing to skip
		}
		version = strconv.FormatInt(n+1, 10)
	}
	return version, nil
}

// health checks what the app depends on for the portal's Observability
// screen (ADR-0073): the app's readiness, the database, the local mail
// catcher and the Docker Compose services.
func (d *devRunner) health(ctx context.Context) []portal.ServiceHealth {
	var out []portal.ServiceHealth
	now := func() time.Time { return time.Now().UTC() }
	timed := func(name string, fn func() (string, string, string)) {
		start := time.Now()
		status, detail, version := fn()
		out = append(out, portal.ServiceHealth{Name: name, Status: status, Detail: detail, Version: version, CheckedAt: now(), LatencyMS: float64(time.Since(start)) / float64(time.Millisecond)})
	}
	client := &http.Client{Timeout: 3 * time.Second}
	timed("app", func() (string, string, string) {
		st := d.Status()
		if st.State != portal.StateRunning {
			return portal.HealthDown, "the app is " + string(st.State), ""
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, st.URL+"/readyz", nil)
		res, err := client.Do(req)
		if err != nil {
			return portal.HealthDown, "readiness check failed: " + err.Error(), ""
		}
		defer res.Body.Close()
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
		if res.StatusCode != http.StatusOK {
			return portal.HealthDegraded, fmt.Sprintf("/readyz answered %d", res.StatusCode), ""
		}
		return portal.HealthOK, "ready", ""
	})
	if open := d.databaseConfig().Open; open != nil {
		timed("postgres", func() (string, string, string) {
			db, err := open(ctx)
			if err != nil {
				return portal.HealthDown, err.Error(), ""
			}
			st, err := db.Stats(ctx)
			if err != nil {
				return portal.HealthDown, err.Error(), ""
			}
			detail := fmt.Sprintf("%d of %d connections", st.Connections, st.MaxConnections)
			if len(st.Locks) > 0 {
				return portal.HealthDegraded, fmt.Sprintf("%s; %d sessions wait on locks", detail, len(st.Locks)), st.Version
			}
			return portal.HealthOK, detail, st.Version
		})
	}
	env, _ := devEnv(".env")
	if d.mailStore != nil {
		timed("mail", func() (string, string, string) {
			return portal.HealthOK, fmt.Sprintf("orb dev's catcher on %s, %d messages", d.mailAddr, d.mailStore.Count()), ""
		})
	}
	if d.database && d.services && mailDelivery(env) == "mailpit" {
		timed("mail", func() (string, string, string) {
			url := "http://127.0.0.1:" + envValue(env, "MAILPIT_WEB_PORT", "8025") + "/api/v1/info"
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			res, err := client.Do(req)
			if err != nil {
				return portal.HealthDown, "Mailpit isn't answering: " + err.Error(), ""
			}
			defer res.Body.Close()
			var info struct {
				Version  string `json:"Version"`
				Messages int    `json:"Messages"`
			}
			if err := json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&info); err != nil || res.StatusCode != http.StatusOK {
				return portal.HealthDegraded, fmt.Sprintf("Mailpit answered %d", res.StatusCode), ""
			}
			return portal.HealthOK, fmt.Sprintf("%d messages in the inbox", info.Messages), info.Version
		})
	}
	if d.database && d.services {
		out = append(out, d.composeServices(ctx, env)...)
	}
	return out
}

// composeServices reads docker compose ps for every service's state.
func (d *devRunner) composeServices(ctx context.Context, env []string) []portal.ServiceHealth {
	raw, err := d.output(ctx, withAppEnv(env), "docker", "compose", "ps", "--all", "--format", "json")
	if err != nil {
		return nil
	}
	var out []portal.ServiceHealth
	// Docker prints one JSON object per line (or an array, in older versions).
	dec := json.NewDecoder(bytes.NewReader(raw))
	for {
		var v any
		if err := dec.Decode(&v); err != nil {
			break
		}
		items, ok := v.([]any)
		if !ok {
			items = []any{v}
		}
		for _, it := range items {
			m, _ := it.(map[string]any)
			name, _ := m["Service"].(string)
			state, _ := m["State"].(string)
			health, _ := m["Health"].(string)
			image, _ := m["Image"].(string)
			if name == "" || name == "postgres" || name == "mailpit" {
				continue // reported above with more detail
			}
			status := portal.HealthUnknown
			switch {
			case state == "running" && (health == "" || health == "healthy"):
				status = portal.HealthOK
			case state == "running":
				status = portal.HealthDegraded
			case state != "":
				status = portal.HealthDown
			}
			detail := state
			if health != "" {
				detail += ", " + health
			}
			out = append(out, portal.ServiceHealth{Name: name, Status: status, Detail: detail, Version: image, CheckedAt: time.Now().UTC()})
		}
	}
	return out
}

// openInEditor opens a file of the app in the developer's editor: the
// command in ORB_EDITOR or VISUAL (with a :line suffix for editors that
// take one), else VS Code's `code --goto` when installed, else the
// system's opener (open, xdg-open).
func openInEditor(dir, path string, line int) error {
	abs := filepath.Join(dir, filepath.FromSlash(path))
	target := abs
	if line > 0 {
		target = fmt.Sprintf("%s:%d", abs, line)
	}
	if editor := firstEnv("ORB_EDITOR", "VISUAL"); editor != "" {
		parts := strings.Fields(editor)
		return exec.Command(parts[0], append(parts[1:], target)...).Start() //nolint:gosec // the developer's own editor
	}
	if code, err := exec.LookPath("code"); err == nil {
		return exec.Command(code, "--goto", target).Start()
	}
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", abs).Start()
	case "windows":
		return exec.Command("cmd", "/c", "start", "", abs).Start()
	default:
		return exec.Command("xdg-open", abs).Start()
	}
}

func firstEnv(keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}
