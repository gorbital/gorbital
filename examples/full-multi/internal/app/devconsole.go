package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net"
	"slices"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"

	"gorbital.dev/buildinfo"
	"gorbital.dev/config"
	"gorbital.dev/modules/devconsole"
	"gorbital.dev/modules/jobs"
	"gorbital.dev/modules/observability"
	"gorbital.dev/modules/postgres"

	"example.com/acme-api/db/migrations"
)

// The development console's APIs under /_dev/ (ADR-0065): what the app
// wired, its routes, the environment variables it read, recent requests and
// logs, email in Mailpit, migrations and job runs. They exist only when
// APP_ENV is development and DEV_CONSOLE_TOKEN is set, which orb dev does
// with a new random token on every run; every request needs a localhost
// Host header, a local connection and that token. See
// docs/guides/dev-console.md.

// devConsoleJobRuns is how many recent job runs GET /_dev/jobs lists.
const devConsoleJobRuns = 50

// devConsoleConfig is the dev console's configuration.
type devConsoleConfig struct {
	// Token turns the console on in development (DEV_CONSOLE_TOKEN).
	Token config.Secret
	// MailpitWebPort is the port of Mailpit's web interface on
	// MAILPIT_SMTP_ADDR's host (MAILPIT_WEB_PORT, as in compose.yaml).
	MailpitWebPort string
}

// loadDevConsoleConfig reads the dev console's variables: the token is
// refused in production and checked for length in development.
func loadDevConsoleConfig(get func(key string) string, secret func(key string) config.Secret, production bool) (devConsoleConfig, []error) {
	var errs []error
	c := devConsoleConfig{Token: secret("DEV_CONSOLE_TOKEN"), MailpitWebPort: "8025"}
	switch {
	case c.Token.IsZero():
	case production:
		errs = append(errs, errors.New("DEV_CONSOLE_TOKEN is for local development only: unset it in production"))
	case devconsole.CheckToken(c.Token.Reveal()) != nil:
		errs = append(errs, fmt.Errorf("DEV_CONSOLE_TOKEN must be %d to %d visible ASCII characters; orb dev generates one",
			devconsole.MinTokenLength, devconsole.MaxTokenLength))
	}
	if v := get("MAILPIT_WEB_PORT"); v != "" {
		if n, err := strconv.ParseUint(v, 10, 16); err != nil || n == 0 {
			errs = append(errs, fmt.Errorf("MAILPIT_WEB_PORT must be a port number, got %q", v))
		}
		c.MailpitWebPort = v
	}
	return c, errs
}

// devConsoleOn reports whether the app serves the dev console.
func (c Config) devConsoleOn() bool { return c.Env == "development" && !c.DevConsole.Token.IsZero() }

// newDevConsoleLogs returns the buffer of recent log records the console
// serves, or nil when the console is off.
func newDevConsoleLogs(cfg Config) (*devconsole.Logs, error) {
	if !cfg.devConsoleOn() {
		return nil, nil // no console, no buffer
	}
	return devconsole.NewLogs(devconsole.DefaultMaxLogs)
}

// buildDevConsole creates the console when it is on, fed by the request
// collector; a.console stays nil otherwise, which serves nothing.
func (a *App) buildDevConsole(pool *pgxpool.Pool) error {
	if !a.cfg.devConsoleOn() {
		return nil
	}
	sources := devconsole.Sources{
		App:    a.devApp,
		Routes: a.devRoutes,
		Config: func(context.Context) ([]devconsole.EnvKey, error) { return a.cfg.EnvKeys, nil },
		Migrations: func(ctx context.Context) (devconsole.Migrations, error) {
			state, err := postgres.Migrations(ctx, pool, migrations.FS)
			return devconsole.Migrations{Current: state.Current, Latest: state.Latest, Pending: state.Pending}, err
		},
		Jobs: a.devJobRuns,
	}
	if a.cfg.MailDelivery == mailDeliveryMailpit {
		host, _, err := net.SplitHostPort(a.cfg.MailpitAddr)
		if err != nil {
			return err
		}
		if sources.Mail, err = devconsole.MailpitSource("http://"+net.JoinHostPort(host, a.cfg.DevConsole.MailpitWebPort), nil); err != nil {
			return err
		}
	}
	console, err := devconsole.New(a.cfg.DevConsole.Token.Reveal(),
		devconsole.WithAddr(a.cfg.Addr),
		devconsole.WithLogs(a.devLogs),
		devconsole.WithSources(sources),
	)
	if err != nil {
		return err
	}
	// Every request the collector counts, with its path and IDs.
	unsubscribe := a.collector.Subscribe(func(r observability.Request) {
		console.RecordRequest(devconsole.Request{
			Time: r.Time, Method: r.Method, Route: r.Route, Path: r.Path, Status: r.Status,
			DurationMS: float64(r.Duration.Microseconds()) / 1000, RequestID: r.RequestID, TraceID: r.TraceID,
		})
	})
	a.cleanup.Add("dev console", func(context.Context) error { unsubscribe(); return nil })
	a.console = console
	return nil
}

// devApp describes the app for GET /_dev/app.
func (a *App) devApp(ctx context.Context) (devconsole.App, error) {
	routes, err := a.devRoutes(ctx)
	if err != nil {
		return devconsole.App{}, err
	}
	definitions, err := a.jobsManager.Definitions(ctx)
	if err != nil {
		return devconsole.App{}, err
	}
	info := buildinfo.Read()
	out := devconsole.App{
		Name: ServiceName, Version: info.Version, Commit: info.Commit, GoVersion: info.GoVersion, Env: a.cfg.Env,
		Libraries:   devconsole.Libraries(),
		Modules:     routeTags(routes),
		Jobs:        []devconsole.Job{},
		Settings:    []devconsole.Setting{},
		Flags:       []devconsole.Flag{},
		Permissions: []devconsole.PermissionCatalog{},
	}
	for _, d := range definitions {
		job := devconsole.Job{Name: d.Name, Description: d.Description, Enabled: d.Config.Enabled, Schedule: d.Config.Schedule, Modified: d.Modified}
		if !d.NextRunAt.IsZero() {
			job.NextRunAt = &d.NextRunAt
		}
		out.Jobs = append(out.Jobs, job)
	}
	for _, s := range a.settings.List() {
		out.Settings = append(out.Settings, devconsole.Setting{
			Key: s.Key, Group: s.Group, Description: s.Description, Kind: string(s.Kind),
			Value: s.Value, Default: s.Default, Modified: s.Modified, OrgOverridable: s.OrgOverridable,
		})
	}
	for _, f := range a.flags.List() {
		out.Flags = append(out.Flags, devconsole.Flag{
			Key: f.Key, Group: f.Group, Description: f.Description, Client: f.Client,
			Enabled: f.State.Enabled, Default: f.State.Default, Percentage: f.State.Percentage, Modified: f.Modified,
			Targets: len(f.State.Orgs.Allow) + len(f.State.Orgs.Deny) + len(f.State.Users.Allow) + len(f.State.Users.Deny),
		})
	}
	catalogs := permissionCatalogs()
	for _, name := range slices.Sorted(maps.Keys(catalogs)) {
		catalog := devconsole.PermissionCatalog{Name: name, Permissions: []devconsole.Permission{}, Roles: []devconsole.Role{}}
		for _, p := range catalogs[name].AllPermissions() {
			catalog.Permissions = append(catalog.Permissions, devconsole.Permission{Name: p.Name, Description: p.Description})
		}
		for _, r := range catalogs[name].Roles() {
			catalog.Roles = append(catalog.Roles, devconsole.Role{Name: r.Name, Description: r.Description, Permissions: r.Permissions})
		}
		out.Permissions = append(out.Permissions, catalog)
	}
	return out, nil
}

// devJobRuns lists the most recent job runs, without their arguments, for
// GET /_dev/jobs.
func (a *App) devJobRuns(ctx context.Context) ([]devconsole.JobRun, error) {
	page, err := a.jobsManager.Jobs(ctx, jobs.JobFilter{Limit: devConsoleJobRuns})
	if err != nil {
		return nil, err
	}
	runs := make([]devconsole.JobRun, 0, len(page.Jobs))
	for _, j := range page.Jobs {
		run := devconsole.JobRun{
			ID: j.ID, Kind: j.Kind, Queue: j.Queue, State: string(j.State), Attempt: j.Attempt, MaxAttempts: j.MaxAttempts,
			CreatedAt: j.CreatedAt, ScheduledAt: j.ScheduledAt, AttemptedAt: j.AttemptedAt, FinalizedAt: j.FinalizedAt,
			Errors: []string{}, RequestID: j.RequestID,
		}
		for _, e := range j.Errors {
			run.Errors = append(run.Errors, e.Message)
		}
		runs = append(runs, run)
	}
	return runs, nil
}

// devRoutes lists the OpenAPI operations and the plain handlers buildHTTP
// mounts (routes.go), for GET /_dev/routes.
func (a *App) devRoutes(context.Context) ([]devconsole.Route, error) {
	doc, err := json.Marshal(a.api.OpenAPI())
	if err != nil {
		return nil, err
	}
	routes, err := devconsole.RoutesFromOpenAPI(doc)
	if err != nil {
		return nil, err
	}
	routes = append(routes, a.handlerRoutes()...)
	devconsole.SortRoutes(routes)
	return routes, nil
}

// handlerRoutes are the routes buildHTTP mounts outside the OpenAPI
// document. Keep them in step with routes.go and passkeys.go;
// TestDevConsoleRoutes checks each is served.
func (a *App) handlerRoutes() []devconsole.Route {
	paths := []string{"/livez", "/readyz"}
	if a.cfg.DocsEnabled {
		paths = append(paths, "/docs", "/openapi.json", "/openapi.yaml")
	}
	if len(a.cfg.WebAuthn.AppleAppIDs) > 0 {
		paths = append(paths, "/.well-known/apple-app-site-association")
	}
	if len(a.cfg.WebAuthn.AndroidApps) > 0 {
		paths = append(paths, "/.well-known/assetlinks.json")
	}
	routes := make([]devconsole.Route, 0, len(paths))
	for _, p := range paths {
		routes = append(routes, devconsole.Route{Method: "GET", Path: p, Tags: []string{}, Source: devconsole.RouteHandler})
	}
	return routes
}

// routeTags returns the tags of routes, sorted and without duplicates.
func routeTags(routes []devconsole.Route) []string {
	tags := []string{}
	for _, r := range routes {
		tags = append(tags, r.Tags...)
	}
	slices.Sort(tags)
	return slices.Compact(tags)
}
