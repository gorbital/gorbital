package authhttp

import (
	"context"
	"errors"

	"gorbital.dev/gorbital"
	authlib "gorbital.dev/modules/auth"

	"example.com/shelfie/internal/modules/auth/delivery"
	authdomain "example.com/shelfie/internal/modules/auth/domain"
	"example.com/shelfie/internal/modules/auth/usecase"
)

// Organisations is what sign-in needs from an app's organisations
// (ADR-0048, ADR-0058): taking part in creating and deleting accounts, and
// deciding who manages an organisation's service accounts, which sign-in
// stores and authenticates. gorbital.dev/gorbital/orgshttp implements it
// and connects it with [Authenticator.UseOrganisations]; apps don't call
// either.
type Organisations interface {
	// AccountCreated runs after an account is created: registration, a
	// first Google, Apple or GitHub sign-in, or an operator's CreateUser.
	// Its error is logged, not returned: the account exists either way.
	AccountCreated(ctx context.Context, userID string) error
	// CheckAccountDeletion runs before the signed-in user's account is
	// deleted. An error stops the deletion and is returned as it is.
	CheckAccountDeletion(ctx context.Context, userID string) error
	// AccountDeleted runs after an account is deleted. Its error is logged.
	AccountDeleted(ctx context.Context, userID string) error
	// AuthorizeServiceAccounts checks that the signed-in user is a member of
	// orgID whose role may manage its service accounts, and returns a
	// context acting in the organisation and the user's role there.
	AuthorizeServiceAccounts(ctx context.Context, orgID string) (context.Context, string, error)
	// CanAssignServiceAccountRole reports whether a member with callerRole
	// may give a service account role, or manage one that has it. The owner
	// role is never allowed.
	CanAssignServiceAccountRole(callerRole, role string) bool
	// Catalog returns the organisation catalog.
	Catalog() *authlib.Catalog
}

// UseOrganisations connects sign-in to the app's organisations: new
// accounts and deleted ones reach o, and the operations of
// [Authenticator.OrgServiceAccountRoutes] manage the service accounts of
// organisations o authorizes. Until it is called, accounts are created and
// deleted without organisations, and organisations have no service accounts
// (404 service_account_not_found). A later call replaces o.
func (a *Authenticator) UseOrganisations(o Organisations) error {
	if o == nil {
		return errors.New("authhttp: UseOrganisations: the organisations are nil")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.orgs = o
	return nil
}

// OrgServiceAccountRoutes registers the operations on organisations'
// service accounts and their API keys under
// /v1/orgs/{orgId}/service-accounts on r, with v0.1's operation IDs,
// schemas and error codes (ADR-0058). The organisations module registers
// them; they work once [Authenticator.UseOrganisations] is called, and
// before [Authenticator.Setup] they register for the OpenAPI document only.
func (a *Authenticator) OrgServiceAccountRoutes(r *gorbital.Router) {
	delivery.RegisterOrgServiceAccounts(r, a.service())
}

// organisations returns what UseOrganisations connected, or nil.
func (a *Authenticator) organisations() Organisations {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.orgs
}

// orgHooks are the account hooks of the use cases, which ask the
// organisations connected when an account changes.
type orgHooks struct{ a *Authenticator }

var _ usecase.AccountHooks = orgHooks{}

func (h orgHooks) AccountCreated(ctx context.Context, userID string) error {
	if o := h.a.organisations(); o != nil {
		return o.AccountCreated(ctx, userID)
	}
	return nil
}

func (h orgHooks) CheckAccountDeletion(ctx context.Context, userID string) error {
	if o := h.a.organisations(); o != nil {
		return o.CheckAccountDeletion(ctx, userID)
	}
	return nil
}

func (h orgHooks) AccountDeleted(ctx context.Context, userID string) error {
	if o := h.a.organisations(); o != nil {
		return o.AccountDeleted(ctx, userID)
	}
	return nil
}

// orgAccess is the use cases' OrgAccess, asking the organisations connected
// on every request.
type orgAccess struct{ a *Authenticator }

var _ usecase.OrgAccess = orgAccess{}

// noOrgRoles is the catalog without organisations: no role grants anything.
var noOrgRoles = func() *authlib.Catalog {
	c := authlib.NewCatalog()
	c.Freeze()
	return c
}()

func (o orgAccess) AuthorizeServiceAccounts(ctx context.Context, orgID string) (context.Context, string, error) {
	if orgs := o.a.organisations(); orgs != nil {
		return orgs.AuthorizeServiceAccounts(ctx, orgID)
	}
	return ctx, "", authdomain.ErrServiceAccountNotFound
}

func (o orgAccess) CanAssign(callerRole, role string) bool {
	if orgs := o.a.organisations(); orgs != nil {
		return orgs.CanAssignServiceAccountRole(callerRole, role)
	}
	return false
}

func (o orgAccess) Catalog() *authlib.Catalog {
	if orgs := o.a.organisations(); orgs != nil {
		return orgs.Catalog()
	}
	return noOrgRoles
}
