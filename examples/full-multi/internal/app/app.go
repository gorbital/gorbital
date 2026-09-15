// Package app is the composition root of acme-api. It is the only package
// that reads configuration, and it builds, wires, runs and shuts down every
// component in a defined order.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/riverqueue/river"

	lifecycle "apistock.dev/app"
	"apistock.dev/buildinfo"
	"apistock.dev/config"
	"apistock.dev/health"
	"apistock.dev/httpx"
	"apistock.dev/mail"
	"apistock.dev/modules/auditpg"
	authlib "apistock.dev/modules/auth"
	"apistock.dev/modules/jobs"
	"apistock.dev/modules/openapi"
	orgslib "apistock.dev/modules/orgs"
	"apistock.dev/modules/postgres"
	"apistock.dev/modules/releases"
	"apistock.dev/modules/settings"
	"apistock.dev/modules/telemetry"

	authmodule "example.com/acme-api/internal/modules/auth"
	authdomain "example.com/acme-api/internal/modules/auth/domain"
	authusecase "example.com/acme-api/internal/modules/auth/usecase"
	opsusecase "example.com/acme-api/internal/modules/ops/usecase"
	orgsmodule "example.com/acme-api/internal/modules/orgs"
	orgsusecase "example.com/acme-api/internal/modules/orgs/usecase"
)

// ServiceName identifies the service in logs, traces and docs.
const ServiceName = "acme-api"

// App is the wired application.
type App struct {
	cfg         Config
	logger      *slog.Logger
	tel         *telemetry.Telemetry
	health      *health.Checker
	cleanup     *lifecycle.Cleanup
	settings    *settings.Store
	jobs        *jobs.Client
	jobsManager *jobs.Manager
	auth        *authmodule.Module
	orgs        *orgsmodule.Module
	releases    *releases.Tracker
	api         huma.API
	handler     http.Handler
}

// New builds the application. Components are constructed in dependency
// order; each registers its resources on the cleanup stack. Run migrations
// first (cmd/migrate).
func New(ctx context.Context, cfg Config) (*App, error) {
	if cfg.DatabaseURL.IsZero() {
		return nil, errors.New("DATABASE_URL is required")
	}
	a, err := newBase(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := a.build(ctx); err != nil {
		return nil, errors.Join(err, a.cleanup.Close(ctx))
	}
	return a, nil
}

// newBase sets up telemetry and health, which need no infrastructure.
func newBase(ctx context.Context, cfg Config) (*App, error) {
	cleanup := &lifecycle.Cleanup{}

	format := telemetry.LogFormatText
	if cfg.Production() {
		format = telemetry.LogFormatJSON
	}
	tel, err := telemetry.Setup(ctx, ServiceName, buildinfo.Read().Version,
		telemetry.WithOTLPExport(cfg.OTLPEndpoint != ""),
		telemetry.WithLogFormat(format),
		telemetry.WithLogLevel(cfg.LogLevel),
	)
	if err != nil {
		return nil, err
	}
	cleanup.Add("telemetry", tel.Shutdown) // registered first, closed last

	return &App{
		cfg:     cfg,
		logger:  tel.Logger(),
		tel:     tel,
		health:  health.New(tel.Logger()),
		cleanup: cleanup,
	}, nil
}

// build connects infrastructure, then the modules that depend on it.
func (a *App) build(ctx context.Context) error {
	pool, err := postgres.Open(ctx, a.cfg.DatabaseURL,
		postgres.WithMaxConns(a.cfg.DBMaxConns),
		postgres.WithApplicationName(ServiceName),
		postgres.WithTracerProvider(a.tel.TracerProvider()),
	)
	if err != nil {
		return err
	}
	a.cleanup.Add("postgres", func(context.Context) error { pool.Close(); return nil })
	a.health.Add(postgres.HealthCheck(pool))

	// Audit events from every module go to the audit_events table, listed by
	// /ops/audit (ADR-0036).
	recorder, err := auditpg.NewStore(pool)
	if err != nil {
		return err
	}

	reg := settings.NewRegistry()
	appSettings := declareSettings(reg)
	a.settings, err = settings.NewStore(ctx, pool, reg, recorder, settings.WithLogger(a.logger))
	if err != nil {
		return err
	}

	// The mail worker delivers queued email through Mailpit in development,
	// or the provider in infra_mail.go.
	sender, err := newMailSender(a.cfg)
	if err != nil {
		return err
	}
	workers := river.NewWorkers()
	if err := jobs.AddMailWorker(workers, sender); err != nil {
		return err
	}

	defs := jobs.NewDefinitions()
	defineJobs(defs, jobDeps{
		logger: a.logger,
		// a.auth is built below, before any job runs.
		authCleanup: func(ctx context.Context) (authdomain.CleanupResult, error) { return a.auth.Service().Cleanup(ctx) },
		orgsPurge:   func(ctx context.Context) (int, error) { return a.orgs.Service().Purge(ctx) },
	})
	a.jobs, err = jobs.New(pool, workers,
		jobs.WithQueues(map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: a.cfg.JobWorkers}}),
		jobs.WithDefinitions(defs),
		jobs.WithLogger(a.logger),
		jobs.WithTracerProvider(a.tel.TracerProvider()),
	)
	if err != nil {
		return err
	}
	a.jobsManager, err = jobs.NewManager(ctx, pool, a.jobs, recorder, jobs.WithManagerLogger(a.logger))
	if err != nil {
		return err
	}

	// Modules send email through mailer: it fills the sender from the mail.*
	// runtime settings and queues the message for the mail worker.
	mailer := mail.WithDefaults(jobs.AsyncSender(a.jobs), appSettings.mailDefaults())
	warnDefaultSender(ctx, a.logger, a.cfg, appSettings)

	// Organisations: members, org roles (permissions.go), invitations and
	// personal workspaces (ADR-0048). Built before authentication, whose
	// account hooks use it.
	a.orgs, err = orgsmodule.New(pool, orgsusecase.Config{
		Catalog:             declareOrgPermissions(),
		Recorder:            recorder,
		Emails:              orgslib.NewMailEmails(mailer, ServiceName),
		InvitationURL:       appSettings.orgsInvitationURL,
		InvitationTTL:       appSettings.orgsInvitationTTL,
		DeletedOrgRetention: appSettings.orgsDeletedOrgRetention,
		Logger:              a.logger,
	})
	if err != nil {
		return err
	}

	// Authentication: users, sessions and the platform roles that grant ops
	// permissions (permissions.go).
	google, apple := a.cfg.Social.providers(a.cfg.ProviderEndpoints)
	a.auth, err = authmodule.New(pool, authusecase.Config{
		Google:                  google,
		Apple:                   apple,
		PublicURL:               a.cfg.Social.PublicURL,
		ReturnOrigins:           a.cfg.returnOrigins(),
		DefaultReturnTo:         a.cfg.Social.PublicURL + "/docs",
		Catalog:                 declarePermissions(),
		Recorder:                recorder,
		Emails:                  authlib.NewMailEmails(mailer, ServiceName),
		Keyring:                 a.cfg.keyring(),
		Issuer:                  ServiceName,
		Passkeys:                a.cfg.WebAuthn.passkeys(),
		Logger:                  a.logger,
		SessionIdleTTL:          appSettings.authSessionIdleTTL,
		SessionAbsoluteTTL:      appSettings.authSessionAbsoluteTTL,
		VerificationCodeTTL:     appSettings.authVerificationCodeTTL,
		ResetCodeTTL:            appSettings.authResetCodeTTL,
		DeletedAccountRetention: appSettings.authDeletedAccountRetention,
		Hooks:                   orgsHooks{orgs: a.orgs.Service},
	})
	if err != nil {
		return err
	}

	// Release tracking: this instance records its build and a heartbeat while
	// it runs, listed by /ops/releases (ADR-0040).
	a.releases, err = releases.NewTracker(pool, buildinfo.Read(), releases.WithLogger(a.logger))
	if err != nil {
		return err
	}
	releaseLog, err := releases.NewStore(pool)
	if err != nil {
		return err
	}

	// Business modules such as projects build themselves from these in their
	// module_<name>.go files.
	return a.buildHTTP(services{
		db:          pool,
		recorder:    recorder,
		logger:      a.logger,
		pingMessage: appSettings.pingMessage,
		auth:        a.auth,
		orgs:        a.orgs,
		ops: opsusecase.Deps{
			Settings: a.settings,
			Jobs:     a.jobsManager,
			Audit:    recorder,
			Releases: releaseLog,
			Mailer:   mailer,
			Mail:     mailInfo(a.cfg, appSettings),
			// What /ops/auth/providers reports (ADR-0045).
			SignInMethods: a.cfg.signInMethods,
		},
	})
}

// Run serves HTTP and runs background work until a shutdown signal, then
// shuts down gracefully.
func (a *App) Run(ctx context.Context) error {
	server := httpx.NewServer(a.cfg.Addr, a.handler, httpx.WithErrorLogger(a.logger))

	opts := []lifecycle.Option{
		lifecycle.WithCleanup(a.cleanup),
		lifecycle.WithLogger(a.logger),
		lifecycle.OnShutdown(a.health.SetShuttingDown),
	}
	if !a.cfg.Production() {
		opts = append(opts, lifecycle.WithDrainDelay(0)) // no load balancer to drain locally
	}

	a.reportSignInMethods(ctx)
	a.logger.InfoContext(ctx, "starting", "addr", "http://"+a.cfg.Addr, "docs_enabled", a.cfg.DocsEnabled, "mail_delivery", a.cfg.MailDelivery)
	return lifecycle.Run(ctx, append([]lifecycle.Runner{server}, a.Workers()...), opts...)
}

// Workers returns the background runners: the settings and job definition
// listeners, the job client and the release tracker. Run starts them; tests
// start them directly.
func (a *App) Workers() []lifecycle.Runner {
	return []lifecycle.Runner{a.settings, a.jobs, a.jobsManager, a.releases}
}

// Auth returns the authentication service, for tests and commands.
func (a *App) Auth() *authusecase.Service { return a.auth.Service() }

// Orgs returns the organisations service, for tests and commands.
func (a *App) Orgs() *orgsusecase.Service { return a.orgs.Service() }

// Handler returns the HTTP handler, for tests.
func (a *App) Handler() http.Handler { return a.handler }

// Close releases resources without running the server, for tests and
// one-off commands.
func (a *App) Close(ctx context.Context) error { return a.cleanup.Close(ctx) }

// WriteOpenAPI writes the API's OpenAPI document to w. It needs no database.
func WriteOpenAPI(ctx context.Context, cfg Config, w io.Writer) (err error) {
	a, err := newBase(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, a.Close(ctx)) }()
	if err := a.buildHTTP(services{pingMessage: config.Static(defaultPingMessage)}); err != nil {
		return err
	}
	if err := openapi.WriteSpec(w, a.api); err != nil {
		return fmt.Errorf("export openapi: %w", err)
	}
	return nil
}
