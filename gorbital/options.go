package gorbital

import (
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"regexp"
	"runtime/debug"
	"strings"

	"gorbital.dev/mail"
	"gorbital.dev/modules/storage"
)

// An Option configures [New], [Main] and [Migrate]. Every option is one line
// in main.go that names what the app contains (ADR-0081).
type Option interface{ apply(*options) }

type optionFunc func(*options)

func (f optionFunc) apply(o *options) { f(o) }

// An Authenticator resolves who makes each request. Its middleware runs at
// the Auth step of the middleware stack ([Stack]) and sets the actor
// (actor.With, or auth.WithPrincipal) for authenticated requests; requests
// it can't authenticate pass through without one, and every route that
// isn't guard.Public() then answers 401.
//
// A value that also has a method Module() Module contributes that module
// too: its routes, permissions, settings, jobs and migrations. One with a
// method Setup(ctx, AuthSetup) error receives the app's configuration,
// dependencies and permission catalog before it serves, and one with a
// method CheckConfig(Config) error checks the configuration first
// ([AuthSetup]). gorbital.dev/gorbital/authhttp has all of them.
type Authenticator interface {
	Middleware(logger *slog.Logger) func(http.Handler) http.Handler
}

type options struct {
	name       string
	modules    []Module
	auth       Authenticator
	storage    func(Config) (storage.Store, error)
	mailer     func(Config) (mail.Sender, error)
	middleware []func(Deps) func(http.Handler) http.Handler
	stack      func(Stack) []func(http.Handler) http.Handler
	logger     *slog.Logger
	migrations fs.FS
}

func newOptions(opts []Option) options {
	var o options
	for _, opt := range opts {
		if opt != nil {
			opt.apply(&o)
		}
	}
	if o.name == "" {
		o.name = defaultName()
	}
	return o
}

// allModules returns the authenticator's module, when it has one, then the
// app's modules.
func (o options) allModules() []Module {
	var all []Module
	if m, ok := o.auth.(interface{ Module() Module }); ok {
		all = append(all, m.Module())
	}
	return append(all, o.modules...)
}

// WithName sets the app's name, used as the service name in logs, traces
// and metrics, the OpenAPI document's title and the database connections'
// application name. Without it, the name is the last element of the main
// module's path, such as shelfie for example.com/shelfie.
func WithName(name string) Option {
	return optionFunc(func(o *options) { o.name = name })
}

// WithModules adds modules to the app, after those added before. Order
// matters only for reading: each module's routes, settings and permissions
// are its own, and a duplicate fails New naming both modules.
func WithModules(modules ...Module) Option {
	return optionFunc(func(o *options) { o.modules = append(o.modules, modules...) })
}

// WithAuth sets the app's authenticator. Without one, requests have no
// actor, so only guard.Public() routes succeed, and New logs how many routes
// can't be reached.
func WithAuth(a Authenticator) Option {
	return optionFunc(func(o *options) { o.auth = a })
}

// WithStorage sets the app's file storage, passed to modules as
// Deps.Storage. Without it, New opens the local driver for
// STORAGE_DRIVER=local (the default in development) and refuses the
// S3-compatible drivers, whose client gorbital doesn't import: pass one,
// built from Config.Storage with [WithStorageFunc].
func WithStorage(s storage.Store) Option {
	return optionFunc(func(o *options) {
		o.storage = func(Config) (storage.Store, error) { return s, nil }
	})
}

// WithStorageFunc sets the app's file storage like [WithStorage], built
// from the loaded configuration, as main.go needs for a store whose
// settings come from STORAGE_* variables. An error from open fails New as a
// configuration error.
func WithStorageFunc(open func(cfg Config) (storage.Store, error)) Option {
	return optionFunc(func(o *options) { o.storage = open })
}

// WithMailer sets the provider email is delivered through when
// MAIL_DELIVERY is provider, the only mode production allows. Modules don't
// send through it directly: Deps.Mailer queues each message as a job, and
// the mail worker delivers it through this sender, skipping suppressed
// addresses. In development, devmail and mailpit deliver over SMTP without
// it.
func WithMailer(s mail.Sender) Option {
	return optionFunc(func(o *options) {
		o.mailer = func(Config) (mail.Sender, error) { return s, nil }
	})
}

// WithMailerFunc sets the email provider like [WithMailer], built from the
// loaded configuration, such as an API key in Config.Mail. An error from
// open fails New as a configuration error.
func WithMailerFunc(open func(cfg Config) (mail.Sender, error)) Option {
	return optionFunc(func(o *options) { o.mailer = open })
}

// WithMiddleware adds middleware that runs on every request after the
// built-in stack: after authentication, rate limits and idempotency keys,
// so it can read the actor. Middleware added by WithMiddleware and
// [WithMiddlewareFunc] runs in the order the options are given.
func WithMiddleware(middleware ...func(http.Handler) http.Handler) Option {
	return optionFunc(func(o *options) {
		for _, mw := range middleware {
			o.middleware = append(o.middleware, func(Deps) func(http.Handler) http.Handler { return mw })
		}
	})
}

// WithMiddlewareFunc adds middleware like [WithMiddleware], built with the
// app's dependencies once they exist, such as a middleware that reads a
// runtime setting or writes to the database.
func WithMiddlewareFunc(build func(d Deps) func(http.Handler) http.Handler) Option {
	return optionFunc(func(o *options) { o.middleware = append(o.middleware, build) })
}

// WithStack replaces the order of the built-in middleware stack: build
// receives every built-in step and returns the steps to run, outermost
// first. Leaving out Recover or Auth is allowed, and logged as a warning
// when New builds the handler. [WithMiddleware] still runs after the
// returned steps.
func WithStack(build func(s Stack) []func(http.Handler) http.Handler) Option {
	return optionFunc(func(o *options) { o.stack = build })
}

// WithLogger sets the logger of the app and its modules. Without it, New
// builds one from APP_LOG_LEVEL and APP_LOG_FORMAT through the telemetry
// module, which also feeds the dev console and the hourly log archive.
func WithLogger(logger *slog.Logger) Option {
	return optionFunc(func(o *options) { o.logger = logger })
}

// WithMigrations sets the app's own goose migrations, usually the embedded
// files of its db/migrations package. [Migrate] merges them with the
// library's and the modules' migrations by version; New reports pending
// ones; the dev console lists them.
func WithMigrations(fsys fs.FS) Option {
	return optionFunc(func(o *options) { o.migrations = fsys })
}

var nameChars = regexp.MustCompile(`[^a-z0-9_-]+`)

// defaultName is the last element of the main module's path, or "app".
func defaultName() string {
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Path != "" {
		if name := nameChars.ReplaceAllString(strings.ToLower(path.Base(bi.Main.Path)), "-"); name != "" && name != "-" {
			return name
		}
	}
	return "app"
}
