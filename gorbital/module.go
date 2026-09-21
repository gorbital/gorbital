package gorbital

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"

	"github.com/jackc/pgx/v5/pgxpool"

	"gorbital.dev/audit"
	"gorbital.dev/httpx"
	"gorbital.dev/mail"
	"gorbital.dev/modules/flags"
	"gorbital.dev/modules/jobs"
	"gorbital.dev/modules/ratelimitpg"
	"gorbital.dev/modules/settings"
	"gorbital.dev/modules/storage"
)

// A Module is one feature of an app. Its package returns it from a function
// named Module, so declarations such as settings are created in that
// function's closure and used by Routes without a lookup by name:
//
//	func Module() gorbital.Module {
//		var pageSize *settings.Setting[int]
//		return gorbital.Module{
//			Name:     "books",
//			Settings: func(r *settings.Registry) { pageSize = settings.Int(r, "books.page_size", 20) },
//			Routes:   func(r *gorbital.Router, d gorbital.Deps) { /* use pageSize */ },
//		}
//	}
type Module struct {
	// Name identifies the module in operation IDs, errors and logs. It is
	// lowercase snake_case, such as "books", and unique in an app.
	Name string

	// Routes registers the module's operations. [Mount] calls it once.
	Routes func(r *Router, d Deps)

	// Errors map the module's errors to problem responses. Error codes are
	// public API: add new ones, never change existing ones (ADR-0015).
	Errors []httpx.Mapping

	// Permissions are the permissions the module checks and the roles that
	// hold them. Permission names are public API.
	Permissions []Permission

	// Settings declares the module's runtime settings. [Declare] calls it
	// once, before the settings store is built.
	Settings func(r *settings.Registry)

	// Flags declares the module's feature flags. [Declare] calls it once,
	// before the flags store is built.
	Flags func(r *flags.Registry)

	// Middleware runs on every route of the module, before group and route
	// middleware (see [Use]).
	Middleware []func(http.Handler) http.Handler

	// Jobs defines the module's background jobs with jobs.Define. [New]
	// calls it once, before the job client exists, because the client is
	// built from the definitions. So in the Deps it receives, Jobs is nil,
	// and Mailer queues through the job client [New] builds next: a worker
	// keeps d and uses it when a job runs, never inside Jobs itself. A
	// worker that enqueues other jobs gets the client from its context with
	// river.ClientFromContext.
	Jobs func(defs *jobs.Definitions, d Deps)

	// Migrations are the module's migrations, merged by version with the
	// library's and the app's (ADR-0083). App modules keep theirs in the
	// app's db/migrations ([WithMigrations]) so tables of different modules
	// can reference each other; built-in modules declare theirs here.
	Migrations []Migration

	// RateLimiters describe the named limiters the module creates on
	// Deps.RateLimits, for /ops/auth/rate-limits. A name declared twice
	// fails New naming both modules.
	RateLimiters []RateLimiter

	// Retention says how long the module's data is kept and what deletes
	// it, for /ops/retention; the built-in retention job deletes what has
	// a Delete function. [New] calls it once, before defining the jobs,
	// with the Deps Jobs receives.
	Retention func(d Deps) []Retention

	// Platform receives what New built for the whole app, after every
	// store and before any module's Routes. It is for gorbital's built-in
	// modules (see [Platform]). An error from it, such as an invalid
	// secret in the configuration, fails New as a configuration error. It
	// isn't called when the OpenAPI document is exported without a
	// database.
	Platform func(p *Platform) error
}

// A Permission is a permission a module checks, such as "books.book.write".
type Permission struct {
	Name        string
	Description string
	// Roles are the platform roles that hold the permission, such as
	// "user". A role the app doesn't declare grants nothing.
	Roles []string
	// ScopeRoles are the scope roles that hold the permission, such as
	// "owner", "admin" and "member" for organisations, or whatever the
	// app's [Scope] declares: a member holds it only while acting in a
	// scope, through guard.Scope. A permission with ScopeRoles is a scope
	// permission: it is declared in the scope catalog
	// ([Platform.OrgPermissions]), not the platform's, and can't have
	// Roles too.
	ScopeRoles []string

	// OrgRoles is the v0.2 name of ScopeRoles, still honoured: prefer
	// ScopeRoles in new code, and set one or the other, never both. It
	// carries no Deprecated marker on purpose — v0.3.0 is a patch, and a
	// marker would make staticcheck fail the build of every app that
	// already uses the name, including the copies of sign-in and
	// organisations orb new wrote for them.
	//
	// OrgRoles are the organisation roles that hold the permission, such
	// as "owner", "admin" and "member" (ADR-0023, ADR-0048): a member holds
	// it only while acting in an organisation, through guard.OrgMember. A
	// permission with OrgRoles is an organisation permission: it is
	// declared in the organisation catalog ([Platform.OrgPermissions]), not
	// the platform's, and can't have Roles too.
	OrgRoles []string
}

// Deps are the app's shared dependencies, passed to each module's Routes.
//
// Every field may be nil: all of them are while the OpenAPI document is
// exported without a database, and Storage is nil unless the app configures
// file storage. A module registers the same routes either way and uses its
// dependencies only when handling requests.
type Deps struct {
	DB       *pgxpool.Pool
	Audit    audit.Recorder
	Mailer   mail.Sender
	Jobs     *jobs.Client
	Settings *settings.Store
	Flags    *flags.Store
	Storage  storage.Store
	// RateLimits shares guard.RateLimit budgets across instances; without
	// it, each instance counts on its own.
	RateLimits *ratelimitpg.Store
	// Logger is tagged with the module's name by [Mount].
	Logger *slog.Logger
}

// scopeRoles returns the scope roles the permission is held by, from
// either field. Declare rejects a permission that sets both.
func (p Permission) scopeRoles() []string {
	if len(p.ScopeRoles) > 0 {
		return p.ScopeRoles
	}
	return p.OrgRoles
}

var moduleName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// validateModules checks the names every step relies on.
func validateModules(modules []Module) error {
	seen := make(map[string]bool, len(modules))
	for i, m := range modules {
		switch {
		case m.Name == "":
			return fmt.Errorf("gorbital: module %d has no name", i)
		case !moduleName.MatchString(m.Name):
			return fmt.Errorf("gorbital: module name %q must be lowercase snake_case, like books", m.Name)
		case seen[m.Name]:
			return fmt.Errorf("gorbital: two modules are named %q", m.Name)
		}
		seen[m.Name] = true
	}
	return nil
}

// errNilHandler is returned for a route registered with a nil handler.
var errNilHandler = errors.New("handler is nil")

// catchPanic runs fn and returns a panic from it as an error. The
// registries gorbital declares into (settings, flags, the permission
// catalog) and Huma's registration panic on invalid declarations; the
// composition reports them as errors naming the module instead.
func catchPanic(module, what string, fn func()) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("gorbital: module %q: %s: %v", module, what, r)
		}
	}()
	fn()
	return nil
}
