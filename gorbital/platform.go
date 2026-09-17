package gorbital

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"

	"gorbital.dev/actor"
	"gorbital.dev/health"
	"gorbital.dev/mail"
	"gorbital.dev/modules/jobs"
	"gorbital.dev/modules/settings"

	"gorbital.dev/gorbital/internal/builtinjobs"
)

// A Platform is what [New] builds for the app as a whole, beyond [Deps]:
// the job manager, the instance's identity and health, the configuration,
// and what every module declared about its data and rate limits. The
// operations API (gorbital.dev/gorbital/opshttp) reports on them and the
// mail events module (gorbital.dev/gorbital/mailevents) reads its webhook
// secret from the configuration; they receive it through
// [Module.Platform].
//
// Platform is for gorbital's built-in modules. App modules use Deps: a
// Platform's fields follow what the built-in modules need, and grow with
// them (ADR-0083).
type Platform struct {
	// Config is the app's configuration.
	Config Config
	// Name is the app's name ([WithName]).
	Name string
	// StartedAt is when New started building the app.
	StartedAt time.Time
	// InstanceID identifies this process in /ops/releases and in request
	// metrics.
	InstanceID string
	// Health runs the readiness checks of /readyz.
	Health *health.Checker
	// Jobs manages job definitions, runs and queues.
	Jobs *jobs.Manager
	// MailSender are the runtime settings that fill every email's sender.
	MailSender mail.Defaults
	// Migrations are every migration Migrate applies: the library's, the
	// modules' and the app's, merged.
	Migrations fs.FS

	app *App
}

// A RateLimiter describes a named rate limiter a module creates on
// Deps.RateLimits, for GET /ops/auth/rate-limits, where operators reset a
// key's budget. Limiters of guard.RateLimit are listed without one.
type RateLimiter struct {
	// Name is the limiter's name, as passed to ratelimitpg.Store.Limiter,
	// such as "auth_login". Names are unique in an app.
	Name string
	// Keys says what a key is, such as "client IP address" or "actor ID",
	// so operators know what to reset.
	Keys string
	// Description says what the limiter protects.
	Description string
}

// A Retention is how long one kind of a module's data is kept and what
// deletes it, listed by GET /ops/retention. Exactly one of Delete, Job and
// EnforcedBy says what deletes the data.
type Retention struct {
	// Data names the data, such as "audit_events", in /ops/retention, logs
	// and retention.purged audit events. Names are unique in an app.
	Data string
	// Setting is the runtime setting that holds how long the data is kept.
	Setting *settings.Setting[time.Duration]
	// Delete removes up to limit rows older than before and returns how
	// many it removed. The built-in retention job calls it every day, with
	// the time Setting's value ago.
	Delete func(ctx context.Context, before time.Time, limit int) (int64, error)
	// Job is the name of the job that deletes the data, when the module
	// deletes it with a job of its own, such as "auth_cleanup".
	Job string
	// EnforcedBy says what deletes the data when no job does, such as
	// "each instance, when it starts".
	EnforcedBy string
	// Oldest returns when the oldest stored row was written, and false when
	// there is none. Leave it nil when that can't be read cheaply.
	Oldest func(ctx context.Context) (time.Time, bool, error)
}

// A SignInMethod is a way to sign in and whether the app has it configured,
// as GET /ops/auth/providers lists it and the auth-providers command prints
// it (ADR-0045). It never holds configuration values.
type SignInMethod struct {
	// Key identifies the method, such as "passkeys".
	Key string
	// Name is how people call it, such as "Passkeys in browsers".
	Name    string
	Enabled bool
	// Detail describes an enabled method, such as its relying party ID;
	// never a secret.
	Detail string
	// Missing are the environment variables that turn a disabled method on.
	Missing []string
	// Guide is the documentation section that explains the method, such as
	// "AUTH_PROVIDERS.md#passkeys".
	Guide string
}

// signInReporter is the optional method of an authenticator that reports
// its sign-in methods.
type signInReporter interface {
	SignInMethods(cfg Config) []SignInMethod
}

// SignInMethods returns the sign-in methods the app's authenticator
// ([WithAuth]) reports for the configuration, through its optional method
//
//	SignInMethods(cfg gorbital.Config) []gorbital.SignInMethod
//
// It returns an empty list without an authenticator or when the
// authenticator doesn't report its methods.
// gorbital.dev/gorbital/authhttp reports every method a v0.1 app listed.
func (p *Platform) SignInMethods() []SignInMethod {
	if r, ok := p.app.o.auth.(signInReporter); ok {
		if methods := r.SignInMethods(p.Config); methods != nil {
			return methods
		}
	}
	return []SignInMethod{}
}

// RateLimiters returns every named rate limiter of the app: the built-in
// one of the RateLimit step (auth_ip), those modules declare in
// Module.RateLimiters, in module order, then those guard.RateLimit
// creates, by name.
func (p *Platform) RateLimiters() []RateLimiter {
	out := slices.Clone(p.app.rateLimiters)
	if reg := p.app.reg; reg != nil {
		for _, name := range slices.Sorted(maps.Keys(reg.limiters)) {
			l := reg.limiters[name]
			out = append(out, RateLimiter{Name: name, Keys: l.keys, Description: "guard.RateLimit on " + strings.Join(l.routes, ", ")})
		}
	}
	return out
}

// Retention returns how long each kind of data is kept: what gorbital
// builds, then each module's Module.Retention, in module order.
func (p *Platform) Retention() []Retention { return slices.Clone(p.app.retention) }

// Authenticate runs the app's authentication step again for r, such as a
// long-running stream checking that its session hasn't ended: the
// authenticator ([WithAuth]) and, in development, the dev console's
// operator on /ops/. It sees r's headers and client address, but none of the
// values in r's context, so an actor set before doesn't carry over. It
// returns the actor the step resolved, and false when the request isn't
// authenticated any more.
func (p *Platform) Authenticate(ctx context.Context, r *http.Request) (actor.Actor, bool) {
	var resolved actor.Actor
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		resolved = actor.FromOrAnonymous(r.Context())
	})
	p.app.authStep(next).ServeHTTP(discardWriter{header: http.Header{}}, r.Clone(withoutValues{ctx}))
	return resolved, resolved.Kind != "" && resolved.Kind != actor.KindAnonymous
}

// OnShutdown adds fn to what runs when [App.Run] starts shutting down,
// before the server stops taking requests, such as closing streams that
// would hold it open.
func (p *Platform) OnShutdown(fn func()) {
	if fn != nil {
		p.app.onShutdown = append(p.app.onShutdown, fn)
	}
}

// withoutValues is a context with ctx's deadline and cancellation, and no
// values.
type withoutValues struct{ context.Context }

func (withoutValues) Value(any) any { return nil }

// discardWriter is a response writer that keeps nothing.
type discardWriter struct{ header http.Header }

func (w discardWriter) Header() http.Header       { return w.header }
func (discardWriter) Write(b []byte) (int, error) { return len(b), nil }
func (discardWriter) WriteHeader(int)             {}

// builtinRateLimiters are the limiters New creates itself.
var builtinRateLimiters = []RateLimiter{
	{Name: "auth_ip", Keys: "client IP address", Description: "Requests to /v1/auth per address (auth.ip_requests_per_minute)"},
}

// collectRateLimiters returns the built-in limiters and the modules'. A
// name declared twice fails naming both modules.
func collectRateLimiters(modules []Module) ([]RateLimiter, error) {
	out := slices.Clone(builtinRateLimiters)
	owner := map[string]string{}
	for _, l := range builtinRateLimiters {
		owner[l.Name] = "gorbital"
	}
	for _, m := range modules {
		for _, l := range m.RateLimiters {
			if l.Name == "" {
				return nil, fmt.Errorf("gorbital: module %q declares a rate limiter without a name", m.Name)
			}
			if prev, ok := owner[l.Name]; ok {
				return nil, fmt.Errorf("gorbital: rate limiter %q is declared by modules %q and %q", l.Name, prev, m.Name)
			}
			owner[l.Name] = m.Name
			out = append(out, l)
		}
	}
	return out, nil
}

// checkGuardLimiters refuses a guard.RateLimit limiter named like a declared
// one: their budgets would mix.
func checkGuardLimiters(declared []RateLimiter, reg *registry) error {
	for _, l := range declared {
		if g, ok := reg.limiters[l.Name]; ok {
			return fmt.Errorf("gorbital: rate limiter %q is declared by a module and used by guard.RateLimit on %s; name the guard's limiter differently (guard.Named)", l.Name, strings.Join(g.routes, ", "))
		}
	}
	return nil
}

// collectRetention returns builtin followed by each module's retention, and
// checks them. A Data name used twice fails naming both modules.
func collectRetention(builtin []Retention, modules []Module, deps Deps) ([]Retention, error) {
	out := slices.Clone(builtin)
	owner := map[string]string{}
	for _, r := range builtin {
		owner[r.Data] = "gorbital"
	}
	for _, m := range modules {
		if m.Retention == nil {
			continue
		}
		var declared []Retention
		if err := catchPanic(m.Name, "retention", func() { declared = m.Retention(moduleDeps(deps, m.Name)) }); err != nil {
			return nil, err
		}
		for _, r := range declared {
			if err := checkRetention(r); err != nil {
				return nil, fmt.Errorf("gorbital: module %q: retention %q: %w", m.Name, r.Data, err)
			}
			if prev, ok := owner[r.Data]; ok {
				return nil, fmt.Errorf("gorbital: retention of %q is declared by modules %q and %q", r.Data, prev, m.Name)
			}
			owner[r.Data] = m.Name
			out = append(out, r)
		}
	}
	return out, nil
}

// checkRetention checks what New and /ops/retention rely on.
func checkRetention(r Retention) error {
	deleters := 0
	for _, set := range []bool{r.Delete != nil, r.Job != "", r.EnforcedBy != ""} {
		if set {
			deleters++
		}
	}
	switch {
	case r.Data == "":
		return errors.New("the data has no name")
	case r.Setting == nil:
		return errors.New("the setting is nil")
	case deleters != 1:
		return errors.New("set exactly one of Delete, Job and EnforcedBy to say what deletes the data")
	}
	return nil
}

// retentionTargets are the retentions the built-in retention job deletes.
func retentionTargets(retention []Retention) []builtinjobs.Target {
	var targets []builtinjobs.Target
	for _, r := range retention {
		if r.Delete != nil {
			targets = append(targets, builtinjobs.Target{Name: r.Data, Retention: r.Setting.Get, Delete: r.Delete})
		}
	}
	return targets
}

// checkRetentionJobs fails when a retention names a job the app doesn't
// define.
func checkRetentionJobs(ctx context.Context, manager *jobs.Manager, retention []Retention) error {
	for _, r := range retention {
		if r.Job == "" {
			continue
		}
		if _, err := manager.Definition(ctx, r.Job); errors.Is(err, jobs.ErrUnknownDefinition) {
			return fmt.Errorf("gorbital: retention of %q names the job %q, which no module defines", r.Data, r.Job)
		} else if err != nil {
			return err
		}
	}
	return nil
}
