package opshttp

import (
	"context"
	"errors"
	"net/http"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"gorbital.dev/actor"
	"gorbital.dev/audit"
	"gorbital.dev/buildinfo"
	"gorbital.dev/config"
	"gorbital.dev/gorbital"
	"gorbital.dev/modules/auditpg"
	"gorbital.dev/modules/jobs"
	"gorbital.dev/modules/mail/suppressionpg"
	"gorbital.dev/modules/observability"
	"gorbital.dev/modules/postgres"
	"gorbital.dev/modules/ratelimitpg"
	"gorbital.dev/modules/releases"
	"gorbital.dev/ratelimit"

	"gorbital.dev/gorbital/internal/builtinjobs"
	opsdomain "gorbital.dev/gorbital/opshttp/internal/domain"
	opsusecase "gorbital.dev/gorbital/opshttp/internal/usecase"
)

// testEmailLimiter limits POST /ops/mail/test per operator.
const testEmailLimiter = "ops_test_email"

// serviceDeps builds the use cases' dependencies from the app's, as a v0.1
// app's app.go wires the ops module.
func serviceDeps(d gorbital.Deps, p *gorbital.Platform, o options) (opsusecase.Deps, error) {
	if d.DB == nil {
		return opsusecase.Deps{}, errNoDatabase
	}
	auditStore, err := auditpg.NewStore(d.DB)
	if err != nil {
		return opsusecase.Deps{}, err
	}
	releaseLog, err := releases.NewStore(d.DB)
	if err != nil {
		return opsusecase.Deps{}, err
	}
	suppressions, err := suppressionpg.NewStore(d.DB)
	if err != nil {
		return opsusecase.Deps{}, err
	}
	observabilityStore, err := observability.NewStore(d.DB)
	if err != nil {
		return opsusecase.Deps{}, err
	}
	streams, err := observability.NewStreams(opsusecase.MaxStreams, opsusecase.MaxStreamsPerUser, opsusecase.MaxStreamDuration)
	if err != nil {
		return opsusecase.Deps{}, err
	}
	p.OnShutdown(streams.Close) // live streams would hold the server open

	deps := opsusecase.Deps{
		Settings: d.Settings,
		Flags:    d.Flags,
		Jobs:     p.Jobs,
		Audit:    auditLog{Store: auditStore, recorder: d.Audit},
		Releases: releaseLog,
		Mailer:   d.Mailer,
		Mail:     mailInfo(p, o.mailProvider),
		// What /ops/auth/providers reports (ADR-0045).
		SignInMethods: signInMethods(o.signInMethods),
		// The limiters and their resets for /ops/auth/rate-limits (ADR-0070).
		RateLimits: rateLimitAdmin{platform: p, store: d.RateLimits},
		// Suppressed addresses for /ops/mail/suppressions (ADR-0062).
		Suppressions: suppressions,
		// File storage for /ops/storage (ADR-0075).
		Storage: d.Storage,
		// Live observability and incidents (ADR-0064).
		Observability:  observabilityStore,
		Incidents:      observabilityStore,
		Streams:        streams,
		Reauthenticate: reauthenticate(p),
		// What /ops/system reports (ADR-0051).
		System: systemReporter{pool: d.DB, platform: p},
		// What /ops/retention reports (ADR-0051).
		Retention: retentionReporter{platform: p},
	}
	// Test emails each operator may send.
	if d.RateLimits != nil {
		limiter, err := d.RateLimits.Limiter(testEmailLimiter, func(context.Context) ratelimit.Limit {
			return ratelimit.Per(opsusecase.TestEmailsPerHour, time.Hour)
		})
		if err != nil {
			return opsusecase.Deps{}, err
		}
		deps.TestEmailLimiter = limiter
	}
	return deps, nil
}

// auditLog lists audit events from the audit store and records the
// module's own events through the app's recorder, like every other module.
type auditLog struct {
	*auditpg.Store
	recorder audit.Recorder
}

func (l auditLog) Record(ctx context.Context, e audit.Event) error { return l.recorder.Record(ctx, e) }

// mailInfo describes how the app sends email, without secrets.
func mailInfo(p *gorbital.Platform, provider string) opsusecase.MailInfo {
	info := opsusecase.MailInfo{AppName: p.Name, Provider: provider, Delivery: p.Config.MailDelivery, Sender: p.MailSender}
	if provider == ProviderResend {
		configured := func(s config.Secret) string {
			if s.IsZero() {
				return "missing"
			}
			return "configured"
		}
		info.Details = map[string]string{
			"api_key":        configured(p.Config.Mail.ResendAPIKey),
			"webhook_secret": configured(p.Config.Mail.ResendWebhookSecret),
		}
	}
	return info
}

// signInMethods adapts the SignInMethods option to the use cases.
func signInMethods(list func() []SignInMethod) func() []opsdomain.SignInMethod {
	if list == nil {
		return func() []opsdomain.SignInMethod { return []opsdomain.SignInMethod{} }
	}
	return func() []opsdomain.SignInMethod {
		methods := list()
		out := make([]opsdomain.SignInMethod, len(methods))
		for i, m := range methods {
			out[i] = opsdomain.SignInMethod(m)
		}
		return out
	}
}

// reauthenticate checks a stream's credentials again while the stream
// runs: a signed-out, expired or revoked session ends it.
func reauthenticate(p *gorbital.Platform) func(ctx context.Context, r *http.Request) (context.Context, error) {
	return func(ctx context.Context, r *http.Request) (context.Context, error) {
		a, ok := p.Authenticate(ctx, r)
		if !ok {
			return nil, opsdomain.ErrUnauthenticated
		}
		return actor.With(ctx, a), nil
	}
}

// rateLimitAdmin lists the app's named rate limiters and resets a key's
// budget in the shared store (ADR-0070).
type rateLimitAdmin struct {
	platform *gorbital.Platform
	store    *ratelimitpg.Store
}

func (r rateLimitAdmin) Limiters() []opsdomain.RateLimiter {
	limiters := r.platform.RateLimiters()
	out := make([]opsdomain.RateLimiter, len(limiters))
	for i, l := range limiters {
		out[i] = opsdomain.RateLimiter(l)
	}
	return out
}

func (r rateLimitAdmin) Reset(ctx context.Context, name, key string) (bool, error) {
	if r.store == nil {
		return false, nil
	}
	return r.store.Reset(ctx, name, key)
}

// systemTimeout bounds each database call the system report makes.
const systemTimeout = 2 * time.Second

// systemReporter builds GET /ops/system's report from this instance
// (ADR-0051). Errors are reported as fixed descriptions: driver messages can
// name hosts.
type systemReporter struct {
	pool     *pgxpool.Pool
	platform *gorbital.Platform
}

func (r systemReporter) SystemReport(ctx context.Context) opsusecase.SystemReport {
	info := buildinfo.Read()
	p := r.platform
	report := opsusecase.SystemReport{
		Instance: opsusecase.SystemInstance{
			ID: p.InstanceID, Version: info.Version, Commit: info.Commit, BuildTime: info.BuildTime,
			Modified: info.Modified, StartedAt: p.StartedAt.UTC(), Uptime: time.Since(p.StartedAt),
		},
		Database: r.database(ctx),
		Runtime:  goRuntime(),
		Jobs:     opsusecase.SystemJobs{Workers: p.Config.JobWorkers},
	}
	if p.Config.JobWorkers > 0 {
		report.Jobs.Queues = []string{river.QueueDefault}
	}

	status, _ := p.Health.Check(ctx)
	for name, check := range status.Checks {
		report.Checks = append(report.Checks, opsusecase.SystemCheck{Name: name, Status: check.Status, Duration: time.Duration(check.DurationMS) * time.Millisecond})
	}
	slices.SortFunc(report.Checks, func(a, b opsusecase.SystemCheck) int { return strings.Compare(a.Name, b.Name) })
	return report
}

func (r systemReporter) database(ctx context.Context) opsusecase.SystemDatabase {
	db := opsusecase.SystemDatabase{Status: "ok"}
	stat := r.pool.Stat()
	db.Pool = opsusecase.PoolStats{
		Total: stat.TotalConns(), Idle: stat.IdleConns(), InUse: stat.AcquiredConns(), Max: stat.MaxConns(),
		Acquires: stat.AcquireCount(), EmptyAcquires: stat.EmptyAcquireCount(), CanceledAcquires: stat.CanceledAcquireCount(),
	}
	if n := stat.AcquireCount(); n > 0 {
		db.Pool.AverageAcquire = stat.AcquireDuration() / time.Duration(n)
	}

	pingCtx, cancel := context.WithTimeout(ctx, systemTimeout)
	defer cancel()
	start := time.Now()
	if err := r.pool.Ping(pingCtx); err != nil {
		db.Status, db.Error = "error", "the database refused the connection or didn't answer within 2 seconds"
		return db
	}
	db.PingDuration = time.Since(start)

	migrateCtx, cancelMigrations := context.WithTimeout(ctx, systemTimeout)
	defer cancelMigrations()
	state, err := postgres.Migrations(migrateCtx, r.pool, r.platform.Migrations)
	if err != nil {
		db.Status, db.Error = "error", "couldn't read the migration state"
		return db
	}
	db.Migrations = opsusecase.MigrationStatus{Current: state.Current, Latest: state.Latest, Pending: state.Pending}
	return db
}

func goRuntime() opsusecase.SystemRuntime {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	rt := opsusecase.SystemRuntime{
		GoVersion: runtime.Version(), GOMAXPROCS: runtime.GOMAXPROCS(0), Goroutines: runtime.NumGoroutine(),
		HeapInUseBytes: m.HeapInuse, GCs: m.NumGC,
	}
	if m.NumGC > 0 {
		rt.LastGCPause = time.Duration(m.PauseNs[(m.NumGC+255)%256]) //nolint:gosec // a GC pause is far below MaxInt64 nanoseconds (292 years)
	}
	return rt
}

// retentionReporter builds GET /ops/retention's report from the retention
// every module declared (gorbital.Retention).
type retentionReporter struct {
	platform *gorbital.Platform
}

func (r retentionReporter) RetentionReport(ctx context.Context) ([]opsusecase.RetentionPolicy, error) {
	policies := r.platform.Retention()
	report := make([]opsusecase.RetentionPolicy, len(policies))
	for i, p := range policies {
		job := p.Job
		if p.Delete != nil {
			job = builtinjobs.Retention
		}
		rp := opsusecase.RetentionPolicy{Data: p.Data, Setting: p.Setting.Key(), Retention: p.Setting.Get(ctx), Job: job, EnforcedBy: p.EnforcedBy}
		if p.Oldest != nil {
			oldest, ok, err := p.Oldest(ctx)
			if err != nil {
				return nil, err
			}
			if ok {
				rp.OldestAt = &oldest
			}
		}
		if job != "" {
			def, err := r.platform.Jobs.Definition(ctx, job)
			if errors.Is(err, jobs.ErrUnknownDefinition) {
				return nil, errors.New("retention policy " + p.Data + " names an undefined job " + job)
			} else if err != nil {
				return nil, err
			}
			rp.LastRun, rp.NextRunAt = def.LastRun, def.NextRunAt
		}
		report[i] = rp
	}
	return report, nil
}
