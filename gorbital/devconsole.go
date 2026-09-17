package gorbital

import (
	"context"
	"encoding/json"
	"net"
	"slices"
	"strings"

	"gorbital.dev/actor"
	"gorbital.dev/buildinfo"
	"gorbital.dev/mail"
	"gorbital.dev/modules/auth"
	"gorbital.dev/modules/devconsole"
	"gorbital.dev/modules/jobs"
	"gorbital.dev/modules/observability"
	"gorbital.dev/modules/postgres"
)

// devConsoleJobRuns is how many recent job runs GET /_dev/jobs lists.
const devConsoleJobRuns = 50

// opsPrefix is where the dev console's token acts as the development
// operator (ADR-0066): the operations APIs, and nothing else.
const opsPrefix = "/ops/"

// platformAdmin is the role whose permissions the development operator
// holds: every /ops permission, as the operations API declares it.
const platformAdmin = "platform_admin"

// devOperator is the actor a request carrying the dev console token becomes
// on /ops/ in development: a system actor named after the console, with the
// platform administrator's permissions, so the Dev Portal can operate the
// app without a signed-in administrator. Audit events record it as
// system/dev-console. It exists only while the console does, which
// production refuses.
func devOperator(catalog *auth.Catalog) actor.Actor {
	return actor.Actor{Kind: actor.KindSystem, ID: "dev-console", Label: "dev console (orb dev)", Permissions: catalog.Permissions(platformAdmin)}
}

// buildDevConsole creates the development console's APIs under /_dev/
// (ADR-0065) when APP_ENV is development and DEV_CONSOLE_TOKEN is set, as
// orb dev does; otherwise a.console stays nil, which serves nothing.
func (a *App) buildDevConsole() error {
	if !a.cfg.devConsoleOn() {
		return nil
	}
	sources := devconsole.Sources{
		App:    a.devApp,
		Routes: a.devRoutes,
		Config: func(context.Context) ([]devconsole.EnvKey, error) { return a.cfg.EnvKeys, nil },
		Migrations: func(ctx context.Context) (devconsole.Migrations, error) {
			state, err := postgres.Migrations(ctx, a.deps.DB, a.migrations)
			return devconsole.Migrations{Current: state.Current, Latest: state.Latest, Pending: state.Pending}, err
		},
		Jobs:         a.devJobRuns,
		MailPreviews: a.devMailPreviews(),
	}
	if a.cfg.MailDelivery == MailMailpit {
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
		Name: a.o.name, Version: info.Version, Commit: info.Commit, GoVersion: info.GoVersion, Env: a.cfg.Env,
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
	for _, s := range a.deps.Settings.List() {
		out.Settings = append(out.Settings, devconsole.Setting{
			Key: s.Key, Group: s.Group, Description: s.Description, Kind: string(s.Kind),
			Value: s.Value, Default: s.Default, Modified: s.Modified, OrgOverridable: s.OrgOverridable,
		})
	}
	for _, f := range a.deps.Flags.List() {
		out.Flags = append(out.Flags, devconsole.Flag{
			Key: f.Key, Group: f.Group, Description: f.Description, Client: f.Client,
			Enabled: f.State.Enabled, Default: f.State.Default, Percentage: f.State.Percentage, Modified: f.Modified,
			Targets: len(f.State.Orgs.Allow) + len(f.State.Orgs.Deny) + len(f.State.Users.Allow) + len(f.State.Users.Deny),
		})
	}
	catalog := devconsole.PermissionCatalog{Name: "platform", Permissions: []devconsole.Permission{}, Roles: []devconsole.Role{}}
	for _, p := range a.catalog.AllPermissions() {
		catalog.Permissions = append(catalog.Permissions, devconsole.Permission{Name: p.Name, Description: p.Description})
	}
	for _, r := range a.catalog.Roles() {
		catalog.Roles = append(catalog.Roles, devconsole.Role{Name: r.Name, Description: r.Description, Permissions: r.Permissions})
	}
	out.Permissions = append(out.Permissions, catalog)
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

// devRoutes lists the OpenAPI operations and the plain handlers New mounts,
// for GET /_dev/routes.
func (a *App) devRoutes(context.Context) ([]devconsole.Route, error) {
	doc, err := json.Marshal(a.api.OpenAPI())
	if err != nil {
		return nil, err
	}
	routes, err := devconsole.RoutesFromOpenAPI(doc)
	if err != nil {
		return nil, err
	}
	paths := []string{"/livez", "/readyz"}
	if a.cfg.DocsEnabled {
		paths = append(paths, "/docs", "/openapi.json", "/openapi.yaml")
	}
	for _, p := range paths {
		routes = append(routes, devconsole.Route{Method: "GET", Path: p, Tags: []string{}, Source: devconsole.RouteHandler})
	}
	for _, h := range a.handlers {
		method, path, _ := strings.Cut(h.pattern, " ")
		routes = append(routes, devconsole.Route{Method: method, Path: path, Tags: []string{}, Source: devconsole.RouteHandler})
	}
	devconsole.SortRoutes(routes)
	return routes, nil
}

// devMailPreviews renders the app's emails with sample data for the Dev
// Portal's template preview (ADR-0074): the authenticator's messages and a
// plain test message. Sending goes through the app's mailer, so a preview
// lands in the inbox the way a real message does.
func (a *App) devMailPreviews() *devconsole.MailPreviewer {
	brand := mail.Brand{Name: a.o.name, URL: a.cfg.Auth.PublicURL}
	kinds := map[string]func(ctx context.Context, to string) (mail.Message, error){}
	var previews []devconsole.MailPreview
	for _, p := range a.mailPreviews {
		kinds[p.Name] = p.Build
		previews = append(previews, devconsole.MailPreview{Name: p.Name, Description: p.Description, Category: p.Category})
	}
	previews = append(previews, devconsole.MailPreview{Name: "test", Description: "A plain message, to check delivery", Category: "test"})
	kinds["test"] = func(_ context.Context, to string) (mail.Message, error) {
		return brand.Message(to, "Test email from "+a.o.name, "test", mail.Email{
			Preheader:  "If you can read it, email delivery works",
			Title:      "Email delivery works",
			Paragraphs: []string{"This is a test message from " + a.o.name + ". If you can read it, email delivery works."},
		}), nil
	}
	return &devconsole.MailPreviewer{
		Previews: previews,
		Build: func(ctx context.Context, name, to string) (mail.Message, error) {
			build, ok := kinds[name]
			if !ok {
				return mail.Message{}, devconsole.ErrUnknownPreview
			}
			return build(ctx, to)
		},
		Send: func(ctx context.Context, m mail.Message) error { return a.deps.Mailer.Send(ctx, m) },
	}
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
