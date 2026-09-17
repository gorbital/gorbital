package gorbital

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"gorbital.dev/actor"
	"gorbital.dev/gorbital/internal/route"
	"gorbital.dev/httpx"
	"gorbital.dev/modules/auth"
	"gorbital.dev/modules/orgs"
	"gorbital.dev/modules/postgres"
)

// An OrgAuthorizer decides whether the actor of a request may act in an
// organisation, for guard.OrgMember. gorbital.dev/gorbital/orgshttp is one;
// it gives it to the app with [Platform.SetOrgAuthorizer].
//
// AuthorizeOrg checks that the actor in ctx is a member of orgID (a user,
// or a service account of that organisation authenticated by its API key)
// whose role grants permission, and returns a context whose actor acts in
// the organisation: actor.Actor.OrgID set, and Permissions those of the
// role, limited by an API key's scopes. It returns
//
//   - orgs.ErrOrgNotFound when orgID isn't an organisation the actor is a
//     member of, deleted and nonexistent ones included, so organisation IDs
//     can't be probed;
//   - actor.ErrUnauthenticated without a member actor;
//   - actor.ErrStepUpRequired when the role grants permission only to
//     sessions verified with a second factor;
//   - actor.ErrForbidden when the role doesn't grant it;
//
// and any other error for a failure, which the guard answers with 500.
// orgs.RequireMember has exactly these semantics.
type OrgAuthorizer interface {
	AuthorizeOrg(ctx context.Context, orgID, permission string) (context.Context, error)
}

// SetOrgAuthorizer makes a the app's [OrgAuthorizer], which every route
// with guard.OrgMember asks. It is for the organisations module
// (gorbital.dev/gorbital/orgshttp), which calls it from Module.Platform. It
// returns an error when a is nil or the app already has one: an app has one
// source of truth for memberships.
func (p *Platform) SetOrgAuthorizer(a OrgAuthorizer) error {
	switch {
	case a == nil:
		return errors.New("the organisation authorizer is nil")
	case p.app.orgs != nil:
		return errors.New("the app already has an organisation authorizer: add one organisations module")
	}
	p.app.orgs = a
	return nil
}

// orgRoleOrder and orgRoleDescriptions are the organisation roles of v0.1
// multi-tenant apps, whose names are public API, in the order and with the
// descriptions of their permissions.go.
var (
	orgRoleOrder        = []string{orgs.RoleOwner, orgs.RoleAdmin, orgs.RoleMember}
	orgRoleDescriptions = map[string]string{
		orgs.RoleOwner:  "Everything, including deleting the organisation and managing owners",
		orgs.RoleAdmin:  "Manages the organisation and its members, except owners",
		orgs.RoleMember: "Works in the organisation",
	}
)

// declareOrgRoles declares every organisation role the modules' permissions
// name in catalog, with the permissions the modules grant it: owner, admin
// and member first, then the others by name.
func declareOrgRoles(catalog *auth.Catalog, modules []Module) error {
	var roles []string
	for _, m := range modules {
		for _, p := range m.Permissions {
			roles = append(roles, p.OrgRoles...)
		}
	}
	slices.SortFunc(roles, func(a, b string) int {
		ia, ib := slices.Index(orgRoleOrder, a), slices.Index(orgRoleOrder, b)
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
		description, ok := orgRoleDescriptions[role]
		if !ok {
			description = "Granted by the app's modules"
		}
		if err := catchPanic("gorbital", "organisation role "+role, func() {
			catalog.Role(role, description, OrgGrants(role, modules...)...)
		}); err != nil {
			return err
		}
	}
	return nil
}

// The refusals of guard.OrgMember, with the codes and details of v0.1
// multi-tenant apps (module_orgs.go).
var (
	errOrgNotFound       = httpx.NewProblem(http.StatusNotFound, "org_not_found", "you aren't a member of an organisation with this ID")
	errOrgForbidden      = httpx.NewProblem(http.StatusForbidden, "forbidden", "your role in this organisation doesn't allow this")
	errOrgMFARequired    = httpx.NewProblem(http.StatusForbidden, "mfa_required", "sign in with two-factor authentication to do this in this organisation")
	errNoOrgAuthorizer   = errors.New("gorbital: guard.OrgMember: the app has no organisation authorizer (orgshttp.Module)")
	errOrgPathParameter  = errors.New("guard.OrgMember needs the organisation ID in the path as {" + route.OrgIDParam + "}, such as /v1/orgs/{orgId}/invoices")
	errOrgMemberOnPublic = errors.New("guard.OrgMember can't be used on a public route")
)

// orgMiddleware returns the operation middleware of a guard.OrgMember guard:
// it asks the app's authorizer, refuses with v0.1's problems, and passes on
// a context acting in the organisation, whose database connections carry it
// for row-level security (postgres.WithOrg).
func (g *registry) orgMiddleware(module string, op *huma.Operation, guard route.Guard, public bool) (func(huma.Context, func(huma.Context)), error) {
	switch {
	case public:
		return nil, errOrgMemberOnPublic
	case !strings.Contains(op.Path, "{"+route.OrgIDParam+"}"):
		return nil, errOrgPathParameter
	}
	permission := guard.Org.Permission
	g.orgRoutes = append(g.orgRoutes, op.Method+" "+op.Path)
	routeAttr := routeAttribute(op.Path)
	return func(hctx huma.Context, next func(huma.Context)) {
		orgID := hctx.Param(route.OrgIDParam)
		ctx, err := g.authorizeOrg(hctx.Context(), orgID, permission)
		if err != nil {
			g.refuse(hctx, module, guard.Name, routeAttr, err)
			return
		}
		next(huma.WithContext(hctx, postgres.WithOrg(ctx, orgID)))
	}, nil
}

// authorizeOrg asks the authorizer and turns its errors into problems.
func (g *registry) authorizeOrg(ctx context.Context, orgID, permission string) (context.Context, error) {
	if g.orgs == nil {
		return ctx, errNoOrgAuthorizer
	}
	if _, err := orgs.ParseID(orgID); err != nil {
		if _, ok := actor.From(ctx); !ok {
			return ctx, errUnauthenticated
		}
		return ctx, errOrgNotFound // malformed IDs look like unknown ones
	}
	orgCtx, err := g.orgs.AuthorizeOrg(ctx, orgID, permission)
	switch {
	case err == nil:
		if a, ok := actor.From(orgCtx); !ok || a.OrgID != orgID {
			return ctx, fmt.Errorf("gorbital: guard.OrgMember: the organisation authorizer didn't return a context acting in %s", orgID)
		}
		return orgCtx, nil
	case errors.Is(err, orgs.ErrOrgNotFound):
		return ctx, errOrgNotFound
	case errors.Is(err, actor.ErrUnauthenticated):
		return ctx, errUnauthenticated
	case errors.Is(err, actor.ErrStepUpRequired):
		return ctx, errOrgMFARequired
	case errors.Is(err, actor.ErrForbidden):
		return ctx, errOrgForbidden
	default:
		return ctx, err
	}
}

// checkOrgRoutes fails when routes use guard.OrgMember and nothing
// authorizes organisations.
func checkOrgRoutes(reg *registry) error {
	if len(reg.orgRoutes) == 0 || reg.orgs != nil {
		return nil
	}
	return fmt.Errorf("gorbital: guard.OrgMember on %s needs organisations: add orgshttp.Module to the app", strings.Join(reg.orgRoutes, ", "))
}
