package orgshttp

import (
	"fmt"
	"regexp"

	"gorbital.dev/gorbital"
	orgslib "gorbital.dev/modules/orgs"
	"gorbital.dev/modules/postgres"
)

// DefaultScope is the tenancy organisations give an app (ADR-0088): the
// concept named "organisation", its ID in {orgId} and shaped like
// orgs.ParseID accepts, refused with 404 org_not_found, the roles owner,
// admin and member, and the organisation carried into the request's
// connections for row-level security. It is the vocabulary of v0.1 and
// v0.2 multi-tenant apps, so their HTTP contract doesn't change.
//
// [Module] sets it with gorbital.Platform.SetScope, so an app that mounts
// organisations needs nothing else for guard.Scope. An app that writes its
// own membership rules starts from its own gorbital.Scope instead;
// [ScopeName] renames this one.
//
// Unlike gorbital.DefaultOrgScope it has a ValidID: only this package may
// import gorbital.dev/modules/orgs, so only this package knows how an
// organisation ID is shaped.
func DefaultScope() gorbital.Scope {
	return gorbital.Scope{
		Name:         "organisation",
		PathParam:    "orgId",
		NotFoundCode: "org_not_found",
		ValidID:      validOrgID,
		Roles: []gorbital.ScopeRole{
			{Name: orgslib.RoleOwner, Description: "Everything, including deleting the organisation and managing owners"},
			{Name: orgslib.RoleAdmin, Description: "Manages the organisation and its members, except owners"},
			{Name: orgslib.RoleMember, Description: "Works in the organisation"},
		},
		Session: postgres.WithScope,
	}
}

// validOrgID reports whether id is shaped like an organisation ID. A
// malformed ID is refused exactly as an unknown one is, before the
// authorizer is asked, so it never reaches a query.
func validOrgID(id string) bool {
	_, err := orgslib.ParseID(id)
	return err == nil
}

// ScopeName mounts the same organisations under the app's own words:
// singular names the concept ("merchant"), plural is the path segment the
// routes are under ("merchants"), and param is the path parameter the ID
// is read from ("merchantId").
//
// It changes four things, and only these four:
//
//   - the route paths, /v1/orgs/… becoming /v1/<plural>/…;
//   - the path parameter, {orgId} becoming {param}, in the paths, in the
//     OpenAPI document and for guard.Scope;
//   - the refusal code of a request from someone who isn't a member,
//     org_not_found becoming <singular>_not_found, with the 403 forbidden
//     and mfa_required details worded for the concept;
//   - the OpenAPI tag, Organisations becoming the plural capitalised.
//
// Everything else is the app's data or its declared API, and renaming it
// is out of scope for v0.3.0: the database tables and columns, the
// migrations, the permission names (orgs.*), the role names (owner, admin,
// member), the operation IDs (orgs-*), the schema names, the audit actions
// (orgs.*), the runtime setting keys (orgs.*), the job name (orgs_purge),
// the other error codes and the wording of the messages that carry them.
// The ID format is unchanged too: a merchant's ID is still org_….
//
// The organisation service accounts under
// /v1/orgs/{orgId}/service-accounts are sign-in's operations, registered
// by gorbital.dev/gorbital/authhttp under its own paths, so they can't
// follow the new words: gorbital.New fails unless
// [WithoutServiceAccounts] leaves them out.
//
// singular and plural are lowercase letters; param is a letter followed by
// letters and digits. gorbital.New fails, naming the value, when one
// isn't.
func ScopeName(singular, plural, param string) Option {
	return func(o *options) {
		o.names = names{singular: singular, plural: plural, param: param}
	}
}

// names are the words [ScopeName] sets; the zero value is organisations'.
type names struct{ singular, plural, param string }

// named reports whether the app chose its own words.
func (n names) named() bool { return n != names{} }

// The shapes the words must have, as gorbital.Scope requires of a name and
// a path parameter, and joinPath of a path segment.
var (
	scopeWord  = regexp.MustCompile(`^[a-z]+$`)
	scopeParam = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9]*$`)
)

// validate reports what is wrong with the words, if anything.
func (n names) validate() error {
	switch {
	case !n.named():
		return nil
	case !scopeWord.MatchString(n.singular):
		return fmt.Errorf("orgshttp.ScopeName: the singular %q must be lowercase letters, such as merchant", n.singular)
	case !scopeWord.MatchString(n.plural):
		return fmt.Errorf("orgshttp.ScopeName: the plural %q must be lowercase letters, such as merchants", n.plural)
	case !scopeParam.MatchString(n.param):
		return fmt.Errorf("orgshttp.ScopeName: the path parameter %q must be a letter followed by letters and digits, such as merchantId", n.param)
	}
	return nil
}

// scope is the scope the module sets: organisations' under the app's
// words.
func (o options) scope() gorbital.Scope {
	s := DefaultScope()
	if o.names.named() {
		s.Name = o.names.singular
		s.PathParam = o.names.param
		s.NotFoundCode = o.names.singular + "_not_found"
	}
	return s
}
