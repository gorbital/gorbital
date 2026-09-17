package gorbital

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"gorbital.dev/actor"
	lifecycle "gorbital.dev/app"
	"gorbital.dev/buildinfo"
	"gorbital.dev/health"
	"gorbital.dev/httpx"
	"gorbital.dev/mail"
	"gorbital.dev/modules/auditpg"
	"gorbital.dev/modules/auth"
	"gorbital.dev/modules/devconsole"
	"gorbital.dev/modules/flags"
	"gorbital.dev/modules/idempotency"
	"gorbital.dev/modules/jobs"
	"gorbital.dev/modules/mail/smtp"
	"gorbital.dev/modules/mail/suppressionpg"
	"gorbital.dev/modules/observability"
	"gorbital.dev/modules/postgres"
	"gorbital.dev/modules/ratelimitpg"
	"gorbital.dev/modules/releases"
	"gorbital.dev/modules/settings"
	"gorbital.dev/modules/storage"
	"gorbital.dev/modules/storage/local"
	"gorbital.dev/modules/storage/logarchive"
	"gorbital.dev/modules/telemetry"
	"gorbital.dev/ratelimit"
	"gorbital.dev/requestid"

	"gorbital.dev/gorbital/internal/builtinjobs"
)

// storageLocalPath is where the local storage driver's signed URLs are
// served.
const storageLocalPath = "/storage"

// An App is a built application: its HTTP handler, background workers and
// the resources they hold. [New] builds it; [App.Run] serves it until a
// shutdown signal; [App.Close] releases it without running.
type App struct {
	cfg     Config
	o       options
	modules []Module
	logger  *slog.Logger
	cleanup *lifecycle.Cleanup
	health  *health.Checker
	started time.Time

	tel        *telemetry.Telemetry
	metrics    *httpx.Server // nil unless METRICS_ADDR is set
	devLogs    *devconsole.Logs
	console    *devconsole.Console // nil unless the dev console is on
	logArchive *logarchive.Archive

	deps        Deps
	settings    builtinSettings
	catalog     *auth.Catalog
	migrations  fs.FS
	jobsManager *jobs.Manager
	releases    *releases.Tracker
	collector   *observability.Collector
	storageURLs http.Handler // local storage's signed URLs; nil for other drivers

	// What built-in modules read through the Platform (platform.go).
	platform     *Platform
	rateLimiters []RateLimiter // built-in and declared by modules
	retention    []Retention
	reg          *registry                       // what Mount registered, such as guard limiters
	authStep     func(http.Handler) http.Handler // the authenticator and the dev operator
	onShutdown   []func()

	api     huma.API
	handler http.Handler
}

// New builds the app from cfg in dependency order (ADR-0081): telemetry,
// the database pool, the audit log, the settings and flags registries with
// every module's declarations, their stores, email delivery, rate limits,
// idempotency keys, file storage, request metrics, the job definitions and
// client, the mailer modules send through, release tracking, the dev
// console, then the modules' routes and the middleware stack. Every step is
// a public constructor of a library module, which an app can also call
// itself.
//
// New connects to PostgreSQL and loads the stored settings, flags and job
// overrides, but never migrates (ADR-0017): run [Migrate] first. Pending
// migrations are logged as a warning.
//
// It returns an error, after closing whatever it had opened, for a missing
// DATABASE_URL, an unreachable database, conflicting migrations, a module
// declaration that fails (a duplicate module, route, operation ID,
// permission, setting, flag or job, naming both modules), an S3-compatible
// STORAGE_DRIVER without [WithStorage], or MAIL_DELIVERY=provider without
// [WithMailer].
func New(ctx context.Context, cfg Config, opts ...Option) (*App, error) {
	o := newOptions(opts)
	modules := o.allModules()
	if err := validateModules(modules); err != nil {
		return nil, err
	}
	if cfg.DatabaseURL.IsZero() {
		return nil, fmt.Errorf("%w: DATABASE_URL is required", errInvalidConfig)
	}
	migrations, err := mergeMigrations(modules, o.migrations)
	if err != nil {
		return nil, err
	}
	a, err := newBase(ctx, cfg, o)
	if err != nil {
		return nil, err
	}
	a.modules, a.migrations = modules, migrations
	if err := a.build(ctx); err != nil {
		return nil, errors.Join(err, a.cleanup.Close(ctx))
	}
	return a, nil
}

// newBase sets up logging, telemetry and health checks, which need no
// infrastructure.
func newBase(ctx context.Context, cfg Config, o options) (*App, error) {
	a := &App{cfg: cfg, o: o, cleanup: &lifecycle.Cleanup{}, started: time.Now()}

	var err error
	if cfg.devConsoleOn() && o.logger == nil {
		if a.devLogs, err = devconsole.NewLogs(devconsole.DefaultMaxLogs); err != nil {
			return nil, err
		}
	}
	host, _ := os.Hostname() // empty on failure: archive keys then carry no instance
	a.logArchive, err = logarchive.New(cfg.LogArchiveDir, o.name,
		logarchive.WithLevel(cfg.LogLevel), logarchive.WithInstance(host))
	if err != nil {
		return nil, err
	}

	format := telemetry.LogFormat(cfg.LogFormat)
	if format == "" {
		format = telemetry.LogFormatText
		if cfg.Production() {
			format = telemetry.LogFormatJSON
		}
	}
	telOpts := []telemetry.Option{
		telemetry.WithOTLPExport(cfg.OTLPEndpoint != ""),
		telemetry.WithLogFormat(format),
		telemetry.WithLogLevel(cfg.LogLevel),
		telemetry.WithTraceContextFrom(cfg.TrustedCallers),
		telemetry.WithPrometheus(cfg.MetricsAddr != ""),
		telemetry.WithRuntimeMetrics(),
		telemetry.WithLogTee(a.logArchive.Handler()),
	}
	if a.devLogs != nil {
		telOpts = append(telOpts, telemetry.WithLogTee(a.devLogs.Handler()))
	}
	a.tel, err = telemetry.Setup(ctx, o.name, buildinfo.Read().Version, telOpts...)
	if err != nil {
		return nil, err
	}
	a.cleanup.Add("telemetry", a.tel.Shutdown)       // registered first, closed last
	a.cleanup.Add("log archive", a.logArchive.Close) // stores the partial hour while the logger still works

	a.logger = a.tel.Logger()
	if o.logger != nil {
		a.logger = o.logger
	}
	a.health = health.New(a.logger)
	if cfg.MetricsAddr != "" {
		mux := http.NewServeMux()
		mux.Handle("GET /metrics", a.tel.MetricsHandler())
		a.metrics = httpx.NewServer(cfg.MetricsAddr, mux, httpx.WithErrorLogger(a.logger))
	}
	return a, nil
}

// build connects the infrastructure, then everything that depends on it.
func (a *App) build(ctx context.Context) error {
	cfg, o := a.cfg, a.o
	pool, err := postgres.Open(ctx, cfg.DatabaseURL,
		postgres.WithMaxConns(cfg.DBMaxConns),
		postgres.WithApplicationName(o.name),
		postgres.WithTracerProvider(a.tel.TracerProvider()),
		postgres.WithMeterProvider(a.tel.MeterProvider()),
		postgres.WithLogger(a.logger), // records row-level security bypasses (ADR-0061)
	)
	if err != nil {
		return err
	}
	a.cleanup.Add("postgres", func(context.Context) error { pool.Close(); return nil })
	a.health.Add(postgres.HealthCheck(pool))
	a.warnDatabase(ctx, pool)

	recorder, err := auditpg.NewStore(pool)
	if err != nil {
		return err
	}

	// Declarations first, then the stores that freeze them (ADR-0083).
	reg := settings.NewRegistry()
	flagReg := flags.NewRegistry()
	a.catalog = auth.NewCatalog()
	if a.rateLimiters, err = collectRateLimiters(a.modules); err != nil {
		return err
	}
	if err := catchPanic("gorbital", "settings", func() { a.settings = declareSettings(reg, o.name) }); err != nil {
		return err
	}
	if err := Declare(Declarations{Permissions: a.catalog, Settings: reg, Flags: flagReg}, a.modules...); err != nil {
		return err
	}
	if err := declareRoles(a.catalog, a.modules); err != nil {
		return err
	}
	settingsStore, err := settings.NewStore(ctx, pool, reg, recorder, settings.WithLogger(a.logger))
	if err != nil {
		return err
	}
	flagsStore, err := flags.NewStore(ctx, pool, flagReg, recorder, flags.WithLogger(a.logger))
	if err != nil {
		return err
	}

	// The mail worker delivers queued email, skipping suppressed addresses
	// (ADR-0062).
	sender, err := deliverySender(cfg, o)
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

	// Limits every instance shares (ADR-0052).
	rateLimits, err := ratelimitpg.NewStore(pool, ratelimitpg.WithLogger(a.logger))
	if err != nil {
		return err
	}
	ipLimiter, err := rateLimits.Limiter("auth_ip", func(ctx context.Context) ratelimit.Limit {
		return ratelimit.Per(a.settings.authIPRequestsPerMinute.Get(ctx), time.Minute)
	})
	if err != nil {
		return err
	}
	idempotencyStore, err := idempotency.NewStore(pool,
		idempotency.WithRetention(a.settings.idempotencyRetention.Get), idempotency.WithLogger(a.logger))
	if err != nil {
		return err
	}

	store, err := a.openStorage()
	if err != nil {
		return err
	}
	a.logArchive.Bind(store, a.settings.logsArchiveEnabled, a.logger)

	observabilityStore, err := observability.NewStore(pool)
	if err != nil {
		return err
	}

	// Modules send email through mailer, which queues it for the mail
	// worker; it exists once the job client does, just below, and before any
	// job or request runs.
	var mailer mail.Sender
	a.deps = Deps{
		DB:         pool,
		Audit:      recorder,
		Mailer:     mail.SenderFunc(func(ctx context.Context, m mail.Message) error { return mailer.Send(ctx, m) }),
		Settings:   settingsStore,
		Flags:      flagsStore,
		Storage:    store,
		RateLimits: rateLimits,
		Logger:     a.logger,
	}

	// How long data is kept: what New builds, then the modules' (ADR-0051).
	a.retention, err = collectRetention([]Retention{
		{Data: "audit_events", Setting: a.settings.auditRetention, Delete: recorder.DeleteBefore, Oldest: recorder.Oldest},
		{Data: "settings_history", Setting: a.settings.historyRetention, Delete: settingsStore.DeleteHistoryBefore, Oldest: settingsStore.OldestHistory},
		{Data: "flags_history", Setting: a.settings.historyRetention, Delete: flagsStore.DeleteHistoryBefore, Oldest: flagsStore.OldestHistory},
		{
			Data: "job_definition_history", Setting: a.settings.historyRetention,
			Delete: func(ctx context.Context, before time.Time, limit int) (int64, error) {
				return a.jobsManager.DeleteHistoryBefore(ctx, before, limit)
			},
			Oldest: func(ctx context.Context) (time.Time, bool, error) { return a.jobsManager.OldestHistory(ctx) },
		},
		{Data: "observability_minutes", Setting: a.settings.observabilityRetention, Job: builtinjobs.ObservabilityCleanup, Oldest: observabilityStore.Oldest},
		{Data: "idempotency_keys", Setting: a.settings.idempotencyRetention, Job: builtinjobs.IdempotencyCleanup, Oldest: idempotencyStore.Oldest},
		{Data: "release_instances", Setting: a.settings.releasesInstanceRetention, EnforcedBy: "each instance, when it starts"},
	}, a.modules, a.deps)
	if err != nil {
		return err
	}

	defs := jobs.NewDefinitions()
	builtinjobs.Define(defs, builtinjobs.Deps{
		Logger:                 a.logger,
		Recorder:               recorder,
		RateLimitCleanup:       rateLimits.DeleteExpired,
		IdempotencyCleanup:     idempotencyStore.DeleteExpired,
		ObservabilityCleanup:   observabilityStore.DeleteBefore,
		ObservabilityRetention: a.settings.observabilityRetention.Get,
		DetectIncidents: func(ctx context.Context) (observability.DetectionResult, error) {
			return observabilityStore.DetectIncident(ctx, observability.Detection{
				Window:      a.settings.incidentsDetectionWindow.Get(ctx),
				Threshold:   a.settings.incidentsErrorRateThreshold.Get(ctx) / 100,
				MinRequests: int64(a.settings.incidentsMinRequests.Get(ctx)),
				Actor:       actor.System(builtinjobs.IncidentsDetect),
			})
		},
		RetentionTargets: retentionTargets(a.retention),
	})
	if err := defineModuleJobs(defs, a.modules, a.deps); err != nil {
		return err
	}
	client, err := jobs.New(pool, workers,
		jobs.WithQueues(map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: cfg.JobWorkers}}),
		jobs.WithDefinitions(defs),
		jobs.WithLogger(a.logger.With("source", "jobs")),
		jobs.WithTracerProvider(a.tel.TracerProvider()),
	)
	if err != nil {
		return err
	}
	a.jobsManager, err = jobs.NewManager(ctx, pool, client, recorder, jobs.WithManagerLogger(a.logger.With("source", "jobs")))
	if err != nil {
		return err
	}
	if err := checkRetentionJobs(ctx, a.jobsManager, a.retention); err != nil {
		return err
	}
	mailer = mail.WithDefaults(jobs.AsyncSender(client), a.settings.mailDefaults())
	a.deps.Jobs = client
	warnDefaultSender(ctx, a.logger, cfg, a.settings)

	// This instance records its build and a heartbeat while it runs, and
	// counts its requests under the same instance ID (ADR-0040, ADR-0064).
	a.releases, err = releases.NewTracker(pool, buildinfo.Read(), releases.WithLogger(a.logger),
		releases.WithRetentionFunc(a.settings.releasesInstanceRetention.Get))
	if err != nil {
		return err
	}
	a.collector, err = observability.NewCollector(
		observability.WithInstance(a.releases.InstanceID()),
		observability.WithSink(observabilityStore),
		observability.WithLogger(a.logger),
	)
	if err != nil {
		return err
	}
	if err := a.buildDevConsole(); err != nil {
		return err
	}
	if err := a.buildPlatform(); err != nil {
		return err
	}

	api, mux, mounted, err := buildAPI(cfg, o, a.modules, a.deps)
	if err != nil {
		return err
	}
	if err := checkGuardLimiters(a.rateLimiters, mounted); err != nil {
		return err
	}
	a.api, a.reg = api, mounted
	a.warnUnreachable()
	mux.Handle("GET /livez", a.health.Liveness())
	mux.Handle("GET /readyz", a.health.Readiness())
	if a.storageURLs != nil {
		mux.Handle(storageLocalPath+"/", a.storageURLs)
	}

	stack, err := a.stack(ipLimiter, idempotencyStore)
	if err != nil {
		return err
	}
	a.handler = a.chain(mux, stack)
	return nil
}

// stack builds the built-in middleware steps from the configuration.
func (a *App) stack(ipLimiter ratelimit.Taker, idempotencyStore *idempotency.Store) (Stack, error) {
	cfg := a.cfg
	cors, err := httpx.CORS(httpx.CORSOptions{
		AllowedOrigins: cfg.CORSOrigins,
		AllowedHeaders: []string{"Authorization", "Content-Type", requestid.Header, idempotency.Header},
		ExposedHeaders: []string{requestid.Header, "Retry-After", idempotency.ReplayedHeader},
	})
	if err != nil {
		return Stack{}, err
	}
	crossOrigin, err := httpx.CrossOrigin(cfg.CORSOrigins...)
	if err != nil {
		return Stack{}, err
	}
	var hsts time.Duration
	if cfg.Production() {
		hsts = 365 * 24 * time.Hour
	}
	authenticate := func(next http.Handler) http.Handler { return next }
	if a.o.auth != nil {
		authenticate = a.o.auth.Middleware(a.logger)
	}
	// In development, the dev console's token acts as the platform
	// administrator on /ops/ for the Dev Portal (ADR-0066); without the
	// console it adds nothing.
	operator := a.console.Operator(opsPrefix, devOperator(a.catalog), a.logger)
	a.authStep = func(next http.Handler) http.Handler { return authenticate(operator(next)) }
	return Stack{
		Recover:        httpx.Recover(a.logger),
		TrustedProxies: httpx.TrustedProxies(cfg.TrustedProxies),
		RequestID:      httpx.RequestIDFrom(cfg.TrustedCallers),
		Telemetry:      a.tel.HTTPMiddleware(),
		Observability:  a.collector.Middleware(),
		AccessLog:      httpx.AccessLog(a.logger),
		Timeout:        httpx.Timeout(cfg.RequestTimeout),
		SecureHeaders:  httpx.SecureHeaders(httpx.SecureHeadersOptions{HSTSMaxAge: hsts}),
		CORS:           cors,
		CrossOrigin:    exceptCrossSitePosts(crossOrigin),
		BodyLimit:      httpx.BodyLimit(cfg.MaxBodyBytes),
		Maintenance: httpx.Maintenance(httpx.MaintenanceOptions{
			Enabled:    a.settings.maintenanceEnabled,
			Message:    a.settings.maintenanceMessage,
			RetryAfter: a.settings.maintenanceRetryAfter,
			Open:       maintenanceOpen,
		}),
		Auth:      a.authStep,
		RateLimit: ratelimit.Middleware(ipLimiter, signInLimitKey(ratelimit.ByRemoteIP), nil),
		Idempotency: idempotency.Middleware(idempotencyStore, idempotency.WithSkip(func(r *http.Request) bool {
			return strings.HasPrefix(r.URL.Path, signInPrefix)
		})),
	}, nil
}

// chain wraps mux in the stack, the app's middleware and the dev console.
func (a *App) chain(mux *http.ServeMux, s Stack) http.Handler {
	var recovers, auths atomic.Bool
	s.Recover, s.Auth = used(s.Recover, &recovers), used(s.Auth, &auths)
	steps := s.Default()
	if a.o.stack != nil {
		steps = a.o.stack(s)
	}
	for _, build := range a.o.middleware {
		steps = append(steps, build(a.deps))
	}
	// RecordRoute gives spans, metrics and request counts the matched route
	// pattern, which middleware copying the request would hide from them.
	h := chain(telemetry.RecordRoute(observability.RecordRoute(mux)), steps)
	if !recovers.Load() {
		a.logger.Warn("the middleware stack has no Recover step: a panic in a handler ends the connection instead of answering 500 (gorbital.WithStack)")
	}
	if !auths.Load() {
		a.logger.Warn("the middleware stack has no Auth step: no request is authenticated, so only public routes succeed (gorbital.WithStack)")
	}
	// /_dev/ in development with DEV_CONSOLE_TOKEN, before every middleware.
	return a.console.Mount(h, a.logger)
}

// warnUnreachable logs how many routes need an actor when the app has no
// authenticator to set one.
func (a *App) warnUnreachable() {
	if a.o.auth != nil {
		return
	}
	n := 0
	for _, item := range a.api.OpenAPI().Paths {
		for _, op := range []*huma.Operation{item.Get, item.Post, item.Put, item.Patch, item.Delete} {
			if op != nil && len(op.Security) > 0 {
				n++
			}
		}
	}
	if n > 0 {
		a.logger.Warn("no authenticator (gorbital.WithAuth): routes that require sign-in answer 401 to every request", "unreachable_routes", n)
	}
}

// warnDatabase logs pending migrations and what keeps row-level security
// from protecting organisation data, without stopping the start.
func (a *App) warnDatabase(ctx context.Context, pool *pgxpool.Pool) {
	if state, err := postgres.Migrations(ctx, pool, a.migrations); err != nil {
		a.logger.WarnContext(ctx, "couldn't read the migration state", slog.Any("error", err))
	} else if state.Pending > 0 {
		a.logger.WarnContext(ctx, "the database has pending migrations: run the migrate command", "pending", state.Pending, "current", state.Current, "latest", state.Latest)
	}
	warnings, err := rowLevelSecurityWarnings(ctx, pool)
	if err != nil {
		a.logger.WarnContext(ctx, "couldn't check row-level security", slog.Any("error", err))
	}
	for _, w := range warnings {
		a.logger.WarnContext(ctx, w)
	}
}

// deliverySender returns the sender the mail worker delivers through.
func deliverySender(cfg Config, o options) (mail.Sender, error) {
	switch cfg.MailDelivery {
	case MailDevMail:
		return smtp.New(cfg.DevMailAddr, smtp.WithTLS(smtp.TLSNone))
	case MailMailpit:
		return smtp.New(cfg.MailpitAddr, smtp.WithTLS(smtp.TLSNone))
	}
	if o.mailer == nil {
		return nil, fmt.Errorf("%w: MAIL_DELIVERY=provider needs an email provider: pass gorbital.WithMailer or gorbital.WithMailerFunc in main.go (in development, leave MAIL_DELIVERY empty to use orb dev's mail catcher)", errInvalidConfig)
	}
	s, err := o.mailer(cfg)
	if err != nil {
		return nil, fmt.Errorf("%w: email provider: %w", errInvalidConfig, err)
	}
	if s == nil {
		return nil, fmt.Errorf("%w: the email provider is nil", errInvalidConfig)
	}
	return s, nil
}

// openStorage returns the app's file storage: the one passed with
// WithStorage, or the local driver.
func (a *App) openStorage() (storage.Store, error) {
	cfg := a.cfg
	if a.o.storage != nil {
		s, err := a.o.storage(cfg)
		if err != nil {
			return nil, fmt.Errorf("%w: file storage: %w", errInvalidConfig, err)
		}
		if s == nil {
			return nil, fmt.Errorf("%w: the file storage is nil", errInvalidConfig)
		}
		return s, nil
	}
	if cfg.Storage.Driver != StorageLocal {
		return nil, fmt.Errorf("%w: STORAGE_DRIVER=%s needs its client: pass gorbital.WithStorageFunc in main.go, building the store from cfg.Storage (such as with gorbital.dev/modules/storage/s3)", errInvalidConfig, cfg.Storage.Driver)
	}
	key := []byte(cfg.Storage.SigningKey.Reveal())
	if len(key) == 0 {
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
	}
	base := cfg.Auth.PublicURL
	if base == devPublicURL { // the sign-in default, not where the app listens
		base = ""
	}
	if base == "" {
		host, port, err := net.SplitHostPort(cfg.Addr)
		if err != nil || host == "" || host == "0.0.0.0" || host == "::" {
			host = "127.0.0.1"
		}
		base = "http://" + net.JoinHostPort(host, port)
	}
	store, err := local.New(cfg.Storage.LocalDir, local.WithSigner(key, strings.TrimRight(base, "/")+storageLocalPath))
	if err != nil {
		return nil, err
	}
	a.storageURLs = http.StripPrefix(storageLocalPath, store.Handler())
	return store, nil
}

// buildPlatform gives the modules that ask for it what New built for the
// whole app (Module.Platform).
func (a *App) buildPlatform() error {
	a.platform = &Platform{
		Config: a.cfg, Name: a.o.name, StartedAt: a.started, InstanceID: a.releases.InstanceID(),
		Health: a.health, Jobs: a.jobsManager, MailSender: a.settings.mailDefaults(), Migrations: a.migrations,
		app: a,
	}
	for _, m := range a.modules {
		if m.Platform == nil {
			continue
		}
		var err error
		if panicErr := catchPanic(m.Name, "platform", func() { err = m.Platform(a.platform) }); panicErr != nil {
			return panicErr
		}
		if err != nil {
			return fmt.Errorf("%w: module %q: %w", errInvalidConfig, m.Name, err)
		}
	}
	return nil
}

// declareRoles declares every role the modules' permissions name, with the
// permissions the modules grant it.
func declareRoles(catalog *auth.Catalog, modules []Module) error {
	var roles []string
	for _, m := range modules {
		for _, p := range m.Permissions {
			roles = append(roles, p.Roles...)
		}
	}
	slices.Sort(roles)
	for _, role := range slices.Compact(roles) {
		if err := catchPanic("gorbital", "role "+role, func() {
			catalog.Role(role, "Granted by the app's modules", Grants(role, modules...)...)
		}); err != nil {
			return err
		}
	}
	return nil
}

var definedTwice = regexp.MustCompile(`^jobs: definition "([^"]+)": defined twice$`)

// defineModuleJobs calls each module's Jobs. A job defined twice fails
// naming both modules.
func defineModuleJobs(defs *jobs.Definitions, modules []Module, deps Deps) error {
	for i, m := range modules {
		if m.Jobs == nil {
			continue
		}
		d := moduleDeps(deps, m.Name)
		var panicked any
		func() {
			defer func() { panicked = recover() }()
			m.Jobs(defs, d)
		}()
		if panicked == nil {
			continue
		}
		msg := fmt.Sprint(panicked)
		if match := definedTwice.FindStringSubmatch(msg); match != nil {
			if other := jobOwner(match[1], m, modules[:i], deps); other != "" {
				return fmt.Errorf("gorbital: job %q is defined by modules %q and %q", match[1], other, m.Name)
			}
		}
		return fmt.Errorf("gorbital: module %q: %s", m.Name, msg)
	}
	return nil
}

// jobOwner finds which of earlier, or gorbital itself, also defines the job
// name that failing defined, by defining each of them alone before failing's
// jobs. It runs only on the error path.
func jobOwner(name string, failing Module, earlier []Module, deps Deps) string {
	defines := func(define func(*jobs.Definitions)) (found bool) {
		defs := jobs.NewDefinitions()
		define(defs)
		defer func() {
			if r := recover(); r != nil {
				found = fmt.Sprint(r) == `jobs: definition "`+name+`": defined twice`
			}
		}()
		failing.Jobs(defs, moduleDeps(deps, failing.Name))
		return false
	}
	if defines(func(defs *jobs.Definitions) { builtinjobs.Define(defs, builtinjobs.Deps{}) }) {
		return "gorbital"
	}
	for _, m := range earlier {
		if m.Jobs != nil && defines(func(defs *jobs.Definitions) { m.Jobs(defs, moduleDeps(deps, m.Name)) }) {
			return m.Name
		}
	}
	return ""
}

// moduleDeps returns deps with the logger tagged with the module's name.
func moduleDeps(deps Deps, module string) Deps {
	deps.Logger = moduleLogger(deps.Logger, module)
	return deps
}

// Run serves HTTP and runs the background workers until ctx is done or a
// shutdown signal (SIGINT, SIGTERM) arrives, then shuts down in the order of
// ADR-0017: readiness answers 503, the drain delay passes (5 seconds in
// production, none in development), the server stops taking requests and
// the workers stop, then resources close in the reverse order New opened
// them, telemetry last. It returns nil after a clean shutdown, or every
// failure joined.
//
// The workers are the settings, flags and job definition listeners, the
// job client, the release heartbeat and the request collector, plus the
// metrics listener when METRICS_ADDR is set. Run releases the app's
// resources when it returns, so a Close after it does nothing.
func (a *App) Run(ctx context.Context) error {
	server := httpx.NewServer(a.cfg.Addr, a.handler, httpx.WithErrorLogger(a.logger))
	a.logger.InfoContext(ctx, "starting", "addr", "http://"+a.cfg.Addr, "docs_enabled", a.cfg.DocsEnabled,
		"mail_delivery", a.cfg.MailDelivery, "metrics_addr", a.cfg.MetricsAddr, "dev_console", a.console != nil)
	return lifecycle.Run(ctx, a.runners(server), a.runOptions()...)
}

// runners are what Run starts: the server first, then the workers.
func (a *App) runners(server lifecycle.Runner) []lifecycle.Runner {
	runners := []lifecycle.Runner{server, a.deps.Settings, a.deps.Flags, a.deps.Jobs, a.jobsManager, a.releases, a.collector}
	if a.metrics != nil {
		runners = append(runners, a.metrics)
	}
	return runners
}

func (a *App) runOptions() []lifecycle.Option {
	opts := []lifecycle.Option{
		lifecycle.WithCleanup(a.cleanup),
		lifecycle.WithLogger(a.logger),
		lifecycle.OnShutdown(a.health.SetShuttingDown),
		lifecycle.OnShutdown(a.console.Close), // the dev console's streams would hold the server open
	}
	for _, fn := range a.onShutdown { // such as the operations API's streams
		opts = append(opts, lifecycle.OnShutdown(fn))
	}
	if !a.cfg.Production() {
		opts = append(opts, lifecycle.WithDrainDelay(0)) // no load balancer to drain locally
	}
	return opts
}

// Handler returns the app's HTTP handler: every route behind the middleware
// stack, as Run serves it. Use it in tests, or to serve the app from
// another server; background workers run only with Run.
func (a *App) Handler() http.Handler { return a.handler }

// Deps returns the dependencies the app passes to its modules, for
// commands, seed data and tests that use the same stores.
func (a *App) Deps() Deps { return a.deps }

// Close releases the app's resources without running it, in the reverse
// order New opened them. Call it when Run is never called, such as in
// tests.
func (a *App) Close(ctx context.Context) error { return a.cleanup.Close(ctx) }

// rlsLeftOut are the organisation tables a multi-tenant app leaves without
// row-level security by design (ADR-0061).
var rlsLeftOut = []string{"org_invitations", "org_members"}

// rowLevelSecurityWarnings returns one line per problem with row-level
// security, or none when it is off or sound.
func rowLevelSecurityWarnings(ctx context.Context, db postgres.DBTX) ([]string, error) {
	r, err := postgres.CheckRowLevelSecurity(ctx, db)
	if err != nil || !r.On() {
		return nil, err
	}
	var warnings []string
	if r.Bypasses {
		warnings = append(warnings, fmt.Sprintf("row-level security is on, but the database role %s is a superuser or has BYPASSRLS, so no policy applies to it; connect as a role without either (docs/guides/row-level-security.md)", r.Role))
	}
	if len(r.NotForced) > 0 {
		warnings = append(warnings, "row-level security isn't forced on "+strings.Join(r.NotForced, ", ")+", so its policies don't limit the tables' owner; run ALTER TABLE … FORCE ROW LEVEL SECURITY")
	}
	var unprotected []string
	for _, table := range r.Unprotected {
		if !slices.Contains(rlsLeftOut, table) {
			unprotected = append(unprotected, table)
		}
	}
	if len(unprotected) > 0 {
		warnings = append(warnings, "organisation tables without row-level security: "+strings.Join(unprotected, ", ")+"; add the org_isolation policy in a migration (docs/guides/row-level-security.md)")
	}
	return warnings, nil
}
