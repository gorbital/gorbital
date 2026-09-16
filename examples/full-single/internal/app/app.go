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
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/riverqueue/river"

	lifecycle "gorbital.dev/app"
	"gorbital.dev/buildinfo"
	"gorbital.dev/config"
	"gorbital.dev/health"
	"gorbital.dev/httpx"
	"gorbital.dev/mail"
	"gorbital.dev/modules/auditpg"
	authlib "gorbital.dev/modules/auth"
	"gorbital.dev/modules/jobs"
	"gorbital.dev/modules/mail/suppressionpg"
	"gorbital.dev/modules/openapi"
	"gorbital.dev/modules/postgres"
	"gorbital.dev/modules/releases"
	"gorbital.dev/modules/settings"
	"gorbital.dev/modules/telemetry"

	"example.com/acme-api/internal/jobs/authcleanup"
	"example.com/acme-api/internal/jobs/retention"
	authmodule "example.com/acme-api/internal/modules/auth"
	authdomain "example.com/acme-api/internal/modules/auth/domain"
	authusecase "example.com/acme-api/internal/modules/auth/usecase"
	maileventsusecase "example.com/acme-api/internal/modules/mailevents/usecase"
	opsusecase "example.com/acme-api/internal/modules/ops/usecase"
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
	releases    *releases.Tracker
	api         huma.API
	handler     http.Handler
	started     time.Time
	appSettings appSettings
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
		telemetry.WithTraceContextFrom(cfg.TrustedCallers), // other clients start a new trace
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
		started: time.Now(),
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
	a.appSettings = appSettings
	a.settings, err = settings.NewStore(ctx, pool, reg, recorder, settings.WithLogger(a.logger))
	if err != nil {
		return err
	}

	// The mail worker delivers queued email through Mailpit in development,
	// or the provider in infra_mail.go, skipping addresses on the suppression
	// list: permanent bounces and complaints (ADR-0062).
	sender, err := newMailSender(a.cfg)
	if err != nil {
		return err
	}
	suppressions, err := suppressionpg.NewStore(pool)
	if err != nil {
		return err
	}
	workers := river.NewWorkers()
	if err := jobs.AddMailWorker(workers, mail.WithSuppressionList(sender, suppressions)); err != nil {
		return err
	}

	// Rate limits every instance shares (rate_limits.go, ADR-0052).
	limits, err := newRateLimits(pool, appSettings, a.logger)
	if err != nil {
		return err
	}

	defs := jobs.NewDefinitions()
	defineJobs(defs, jobDeps{
		logger:           a.logger,
		recorder:         recorder,
		rateLimitCleanup: limits.store.DeleteExpired,
		// a.auth and a.jobsManager are built below, before any job runs.
		authCleanup: func(ctx context.Context) (authdomain.CleanupResult, error) { return a.auth.Service().Cleanup(ctx) },
		authRevokeTokens: func(ctx context.Context) (authdomain.RevocationResult, error) {
			return a.auth.Service().RevokeProviderTokens(ctx)
		},
		// What the retention job deletes (ADR-0051).
		retentionTargets: []retention.Target{
			{Name: "audit_events", Retention: appSettings.auditRetention.Get, Delete: recorder.DeleteBefore},
			{Name: "settings_history", Retention: appSettings.historyRetention.Get, Delete: a.settings.DeleteHistoryBefore},
			{Name: "job_definition_history", Retention: appSettings.historyRetention.Get, Delete: func(ctx context.Context, before time.Time, limit int) (int64, error) {
				return a.jobsManager.DeleteHistoryBefore(ctx, before, limit)
			}},
		},
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

	// Authentication: users, sessions and the platform roles that grant ops
	// permissions (permissions.go).
	google, apple := a.cfg.Social.providers(a.cfg.ProviderEndpoints)
	a.auth, err = authmodule.New(pool, authusecase.Config{
		LoginLimiter:            limits.login,
		LoginAddressLimiter:     limits.loginAddress,
		MFALimiter:              limits.mfa,
		ReauthLimiter:           limits.reauth,
		CodeLimiter:             limits.code,
		NoticeLimiter:           limits.notice,
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
		UnverifiedAccountTTL:    appSettings.authUnverifiedAccountTTL,
	})
	if err != nil {
		return err
	}

	// Release tracking: this instance records its build and a heartbeat while
	// it runs, listed by /ops/releases (ADR-0040).
	a.releases, err = releases.NewTracker(pool, buildinfo.Read(), releases.WithLogger(a.logger),
		releases.WithRetentionFunc(appSettings.releasesInstanceRetention.Get))
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
		ipLimiter:   limits.ip,
		ops: opsusecase.Deps{
			Settings: a.settings,
			Jobs:     a.jobsManager,
			Audit:    recorder,
			Releases: releaseLog,
			Mailer:   mailer,
			Mail:     mailInfo(a.cfg, appSettings),
			// What /ops/auth/providers reports (ADR-0045).
			SignInMethods: a.cfg.signInMethods,
			// Test emails each operator may send (rate_limits.go).
			TestEmailLimiter: limits.testEmail,
			// Suppressed addresses for /ops/mail/suppressions (ADR-0062).
			Suppressions: suppressions,
			// What /ops/system reports (ADR-0051).
			System: systemReporter{pool: pool, health: a.health, tracker: a.releases, started: a.started, workers: a.cfg.JobWorkers},
			// What /ops/retention reports (ADR-0051).
			Retention: retentionReporter{jobs: a.jobsManager, policies: []retentionPolicy{
				{data: "audit_events", setting: appSettings.auditRetention.Key(), retention: appSettings.auditRetention.Get, job: retention.Name, oldest: recorder.Oldest},
				{data: "settings_history", setting: appSettings.historyRetention.Key(), retention: appSettings.historyRetention.Get, job: retention.Name, oldest: a.settings.OldestHistory},
				{data: "job_definition_history", setting: appSettings.historyRetention.Key(), retention: appSettings.historyRetention.Get, job: retention.Name, oldest: a.jobsManager.OldestHistory},
				{data: "release_instances", setting: appSettings.releasesInstanceRetention.Key(), retention: appSettings.releasesInstanceRetention.Get, enforcedBy: "each instance, when it starts"},
				{data: "deleted_accounts", setting: appSettings.authDeletedAccountRetention.Key(), retention: appSettings.authDeletedAccountRetention.Get, job: authcleanup.Name},
				{data: "unverified_accounts", setting: appSettings.authUnverifiedAccountTTL.Key(), retention: appSettings.authUnverifiedAccountTTL.Get, job: authcleanup.Name},
			}},
		},
		// The provider's bounce and complaint webhook (infra_mail.go).
		mailEvents: maileventsusecase.Deps{Reader: a.cfg.Mail.webhookReader(), Suppressions: suppressions, Recorder: recorder, Logger: a.logger},
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
