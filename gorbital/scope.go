package gorbital

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"gorbital.dev/actor"
	"gorbital.dev/gorbital/internal/route"
	"gorbital.dev/httpx"
	"gorbital.dev/modules/auth"
)

// A Scope is the app's tenancy: what a tenant is called, how its IDs look,
// which roles it has, and how a request's tenant reaches the database
// (ADR-0088). The framework never learns the app's table names and never
// queries them; membership lives behind the [ScopeAuthorizer].
//
// gorbital.dev/gorbital/orgshttp supplies one for organisations. An app
// with its own membership tables supplies its own:
//
//	gorbital.WithScope(gorbital.Scope{
//		Name:         "merchant",
//		PathParam:    "merchantId",
//		NotFoundCode: "merchant_not_found",
//		ValidID:      merchants.ValidID,
//		Roles:        merchants.Roles,
//		Session:      postgres.WithScope,
//	}, merchants.NewAuthorizer(db))
type Scope struct {
	// Name is the concept in lowercase singular, such as "organisation" or
	// "merchant". It names guards in logs and metrics and appears in the
	// registration errors the guards return.
	Name string

	// PathParam is the path parameter scope routes carry the ID in, such
	// as "orgId". Registration fails for a scope route whose path lacks
	// it. Empty uses [DefaultScopePathParam].
	PathParam string

	// NotFoundCode is the problem code that refuses a request for a scope
	// the actor is not a member of, such as "org_not_found". Unknown,
	// deleted, forbidden and malformed IDs are all refused with it and
	// 404, so scope IDs can't be probed. Empty uses
	// [DefaultScopeNotFoundCode].
	NotFoundCode string

	// ValidID reports whether s is a well-formed scope ID. A malformed ID
	// is refused exactly as an unknown one is, before the authorizer is
	// asked, so it never reaches the app's queries. Nil accepts any
	// non-empty string, which is correct for an app whose authorizer
	// validates the ID itself.
	ValidID func(s string) bool

	// Roles are the scope's roles, most privileged first, with what each
	// one grants. [Permission.ScopeRoles] names them. Roles a permission
	// names and this list doesn't are declared with a generic description.
	Roles []ScopeRole

	// Session carries the scope into the request's database connections,
	// for row-level security (postgres.WithScope, ADR-0061). Nil leaves
	// connections unscoped, which is correct only for an app that filters
	// by scope in every query.
	Session func(ctx context.Context, scopeID string) context.Context
}

// A ScopeRole is one role of a [Scope], such as an organisation's owner.
type ScopeRole struct {
	Name        string
	Description string
}

// Defaults for the fields of a [Scope] left empty. They are the vocabulary
// of v0.1 and v0.2 organisation apps, whose HTTP contract they keep.
const (
	DefaultScopeName         = "organisation"
	DefaultScopePathParam    = route.OrgIDParam
	DefaultScopeNotFoundCode = "org_not_found"
)

// ErrScopeNotFound is what a [ScopeAuthorizer] returns for a scope the
// actor is not a member of: one that doesn't exist, is deleted, or exists
// and doesn't have them. The guard answers all three, and a malformed ID,
// with 404 and the scope's NotFoundCode, so scope IDs can't be probed.
//
// orgs.ErrOrgNotFound is treated the same way, so an organisations
// authorizer needs no change.
var ErrScopeNotFound = errors.New("gorbital: scope not found")

// A ScopeAuthorizer decides whether the actor of a request may act in a
// scope, for guard.Scope. An app sets one with [WithScope]; a module sets
// one with [Platform.SetScope].
//
// AuthorizeScope checks that the actor in ctx is a member of scopeID whose
// role grants permission, and returns a context whose actor acts in the
// scope: actor.Actor.OrgID set to scopeID, and Permissions those of the
// role, limited by an API key's scopes. It returns
//
//   - [ErrScopeNotFound] when scopeID isn't a scope the actor is a member
//     of, deleted and nonexistent ones included;
//   - actor.ErrUnauthenticated without a member actor;
//   - actor.ErrStepUpRequired when the role grants permission only to
//     sessions verified with a second factor;
//   - actor.ErrForbidden when the role doesn't grant it;
//
// and any other error for a failure, which the guard answers with 500.
// orgs.RequireMember has exactly these semantics.
type ScopeAuthorizer interface {
	AuthorizeScope(ctx context.Context, scopeID, permission string) (context.Context, error)
}

// withDefaults returns the scope with its empty fields filled in.
func (s Scope) withDefaults() Scope {
	if s.Name == "" {
		s.Name = DefaultScopeName
	}
	if s.PathParam == "" {
		s.PathParam = DefaultScopePathParam
	}
	if s.NotFoundCode == "" {
		s.NotFoundCode = DefaultScopeNotFoundCode
	}
	return s
}

// validate reports what is wrong with the scope, if anything.
func (s Scope) validate() error {
	switch {
	case !scopeName.MatchString(s.Name):
		return fmt.Errorf("scope name %q must be lowercase letters, such as merchant", s.Name)
	case !scopePathParam.MatchString(s.PathParam):
		return fmt.Errorf("scope path parameter %q must be a letter followed by letters and digits, such as merchantId", s.PathParam)
	case !problemCode.MatchString(s.NotFoundCode):
		return fmt.Errorf("scope refusal code %q must be lowercase snake_case, such as merchant_not_found", s.NotFoundCode)
	}
	for _, r := range s.Roles {
		if !scopeRoleName.MatchString(r.Name) {
			return fmt.Errorf("scope role %q must be lowercase snake_case, such as owner", r.Name)
		}
	}
	return nil
}

// valid reports whether id is shaped like one of the scope's IDs.
func (s Scope) valid(id string) bool {
	if s.ValidID == nil {
		return id != ""
	}
	return s.ValidID(id)
}

// setScope gives the app its scope. It refuses a second one: an app has
// one source of truth for membership.
func (a *App) setScope(s Scope, auth ScopeAuthorizer, from string) error {
	if auth == nil {
		return errors.New("the scope authorizer is nil")
	}
	if a.scopeAuth != nil {
		return fmt.Errorf("the app already has a scope (%s): add one scope, not two", a.scopeFrom)
	}
	s = s.withDefaults()
	if err := s.validate(); err != nil {
		return err
	}
	a.scope, a.scopeAuth, a.scopeFrom = s, auth, from
	return nil
}

// SetScope makes s the app's scope and a its authorizer, which every route
// with guard.Scope asks. It is for a module that owns membership, such as
// gorbital.dev/gorbital/orgshttp, called from Module.Platform. An app that
// authorizes scopes itself uses [WithScope] instead.
//
// It returns an error when a is nil, the scope is invalid, or the app
// already has one.
func (p *Platform) SetScope(s Scope, a ScopeAuthorizer) error {
	return p.app.setScope(s, a, "set by a module")
}

// The refusals of guard.Scope. forbidden and mfa_required keep the codes
// and details of v0.1 multi-tenant apps (module_orgs.go); the not-found
// code comes from the scope.
var (
	errScopeForbidden   = httpx.NewProblem(http.StatusForbidden, "forbidden", "your role in this organisation doesn't allow this")
	errScopeMFARequired = httpx.NewProblem(http.StatusForbidden, "mfa_required", "sign in with two-factor authentication to do this in this organisation")
)

// guardName is how a scope guard calls itself in registration errors: the
// name the app wrote, so a v0.2 app's errors are unchanged.
func guardName(g route.Guard) string {
	if strings.HasPrefix(g.Name, "org_member:") {
		return "guard.OrgMember"
	}
	return "guard.Scope"
}

// pathSegment is the plural the registration error suggests a scope route
// is mounted under: the "orgs" of /v1/orgs/{orgId} for organisations, and
// the scope's name with an s otherwise.
func (s Scope) pathSegment() string {
	if s.Name == DefaultScopeName {
		return "orgs"
	}
	if strings.HasSuffix(s.Name, "s") {
		return s.Name
	}
	return s.Name + "s"
}

// notFound is the scope's 404, built once per app.
func (s Scope) notFound() error {
	detail := "you aren't a member of an organisation with this ID"
	if s.Name != DefaultScopeName {
		detail = "you aren't a member of a " + s.Name + " with this ID"
	}
	return httpx.NewProblem(http.StatusNotFound, s.NotFoundCode, detail)
}

// forbidden and mfaRequired keep v0.1's wording for organisations and name
// the app's own concept otherwise.
func (s Scope) forbidden() error {
	if s.Name == DefaultScopeName {
		return errScopeForbidden
	}
	return httpx.NewProblem(http.StatusForbidden, "forbidden", "your role in this "+s.Name+" doesn't allow this")
}

func (s Scope) mfaRequired() error {
	if s.Name == DefaultScopeName {
		return errScopeMFARequired
	}
	return httpx.NewProblem(http.StatusForbidden, "mfa_required", "sign in with two-factor authentication to do this in this "+s.Name)
}

// noAuthorizer is the registration error for a scope route in an app with
// no scope.
func (g *registry) noAuthorizer() error {
	return errors.New("gorbital: guard.Scope: the app has no scope authorizer (gorbital.WithScope, or a module such as orgshttp.Module)")
}

// scopeMiddleware returns the operation middleware of a guard.Scope guard:
// it asks the app's authorizer, refuses with the scope's problems, and
// passes on a context acting in the scope, whose database connections
// carry it for row-level security.
func (g *registry) scopeMiddleware(module string, op *huma.Operation, guard route.Guard, public bool) (func(huma.Context, func(huma.Context)), error) {
	s := g.scope
	param := "{" + s.PathParam + "}"
	switch {
	case public:
		return nil, fmt.Errorf("%s can't be used on a public route", guardName(guard))
	case !strings.Contains(op.Path, param):
		return nil, fmt.Errorf("%s needs the %s ID in the path as %s, such as /v1/%s/%s/invoices",
			guardName(guard), s.Name, param, s.pathSegment(), param)
	}
	permission := guard.Scope.Permission
	g.scopeRoutes = append(g.scopeRoutes, op.Method+" "+op.Path)
	routeAttr := routeAttribute(op.Path)
	return func(hctx huma.Context, next func(huma.Context)) {
		scopeID := hctx.Param(s.PathParam)
		ctx, err := g.authorizeScope(hctx.Context(), scopeID, permission)
		if err != nil {
			g.refuse(hctx, module, guard.Name, routeAttr, err)
			return
		}
		if s.Session != nil {
			ctx = s.Session(ctx, scopeID)
		}
		next(huma.WithContext(hctx, ctx))
	}, nil
}

// authorizeScope asks the authorizer and turns its errors into problems.
func (g *registry) authorizeScope(ctx context.Context, scopeID, permission string) (context.Context, error) {
	if g.scopeAuth == nil {
		return ctx, g.noAuthorizer()
	}
	s := g.scope
	if !s.valid(scopeID) {
		if _, ok := actor.From(ctx); !ok {
			return ctx, errUnauthenticated
		}
		return ctx, s.notFound() // malformed IDs look like unknown ones
	}
	scoped, err := g.scopeAuth.AuthorizeScope(ctx, scopeID, permission)
	switch {
	case err == nil:
		if a, ok := actor.From(scoped); !ok || a.OrgID != scopeID {
			return ctx, fmt.Errorf("gorbital: guard.Scope: the %s authorizer didn't return a context acting in %s", s.Name, scopeID)
		}
		return scoped, nil
	case errors.Is(err, ErrScopeNotFound):
		return ctx, s.notFound()
	case errors.Is(err, actor.ErrUnauthenticated):
		return ctx, errUnauthenticated
	case errors.Is(err, actor.ErrStepUpRequired):
		return ctx, s.mfaRequired()
	case errors.Is(err, actor.ErrForbidden):
		return ctx, s.forbidden()
	default:
		return ctx, err
	}
}

// checkScopeRoutes fails when routes use guard.Scope and nothing
// authorizes scopes.
func checkScopeRoutes(reg *registry) error {
	if len(reg.scopeRoutes) == 0 || reg.scopeAuth != nil {
		return nil
	}
	return fmt.Errorf("gorbital: guard.Scope on %s needs a scope: add gorbital.WithScope, or a module such as orgshttp.Module", strings.Join(reg.scopeRoutes, ", "))
}

// declareScopeRoles declares every scope role the modules' permissions
// name, with the permissions the modules grant it: the scope's own roles
// first, in its order, then any others by name.
func declareScopeRoles(catalog *auth.Catalog, s Scope, modules []Module) error {
	var named []string
	descriptions := make(map[string]string, len(s.Roles))
	for _, r := range s.Roles {
		named = append(named, r.Name)
		descriptions[r.Name] = r.Description
	}
	var roles []string
	for _, m := range modules {
		for _, p := range m.Permissions {
			roles = append(roles, p.scopeRoles()...)
		}
	}
	slices.SortFunc(roles, func(a, b string) int {
		ia, ib := slices.Index(named, a), slices.Index(named, b)
		switch {
		case ia >= 0 && ib >= 0:
			return ia - ib
		case ia >= 0:
			return -1
		case ib >= 0:
			return 1
		}
		return strings.Compare(a, b)
	})
	for _, role := range slices.Compact(roles) {
		description, ok := descriptions[role]
		if !ok {
			description = "Granted by the app's modules"
		}
		if err := catchPanic("gorbital", "scope role "+role, func() {
			catalog.Role(role, description, ScopeGrants(role, modules...)...)
		}); err != nil {
			return err
		}
	}
	return nil
}

// The shapes a scope's names must have.
var (
	scopeName      = regexp.MustCompile(`^[a-z][a-z]*$`)
	scopePathParam = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9]*$`)
	scopeRoleName  = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	problemCode    = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
)
