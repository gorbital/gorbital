// Package orgshttp is organisations as a gorbital module, for multi-tenant
// apps (ADR-0023, ADR-0048): organisations with members holding one role
// each (owner, admin, member, and the roles the app's modules grant),
// invitations by email, a personal workspace per account, deletion with a
// restore period and the orgs_purge job, each organisation's own runtime
// settings (ADR-0056) and client feature flags (ADR-0057), and
// organisations' service accounts with their API keys (ADR-0058).
//
// It is the orgs module v0.1 multi-tenant apps generate into
// internal/modules/orgs, moved into the library: the same paths under
// /v1/orgs and /v1/invitations, operation IDs, schemas, error codes, audit
// actions, permissions and roles, runtime settings (orgs.*), job and
// migrations. Add it in main.go with the app's sign-in, which it needs for
// accounts, personal workspaces and service accounts:
//
//	auth := authhttp.New()
//	gorbital.Main(
//		gorbital.WithAuth(auth),
//		gorbital.WithModules(opshttp.Module(), orgshttp.Module(auth)),
//		gorbital.WithModules(modules.All()...),
//	)
//
// Organisations are one tenancy an app can have (ADR-0088): the module
// makes [DefaultScope] the app's scope, so app modules scope their routes
// to an organisation with guard.Scope, which asks this module whether the
// caller is a member whose role grants a permission, and declare their
// permissions with gorbital.Permission's ScopeRoles:
//
//	invoices := r.Group("/v1/orgs/{orgId}/invoices")
//	gorbital.Get(invoices, "", h.list, guard.Scope("invoices.invoice.read"))
//
// [ScopeName] mounts the same module under the app's own words, and
// [Module] takes an [Identity] rather than a particular sign-in.
//
// Stability: experimental until v0.2.0 (ADR-0015, ADR-0081).
package orgshttp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/authhttp"
	"gorbital.dev/httpx"
	"gorbital.dev/mail"
	authlib "gorbital.dev/modules/auth"
	orgslib "gorbital.dev/modules/orgs"
	"gorbital.dev/ratelimit"

	"gorbital.dev/gorbital/orgshttp/internal/delivery"
	orgsdomain "gorbital.dev/gorbital/orgshttp/internal/domain"
	"gorbital.dev/gorbital/orgshttp/internal/repository"
	"gorbital.dev/gorbital/orgshttp/internal/usecase"
)

// An Option configures [Module].
type Option func(*options)

type options struct {
	brand             *mail.Brand
	names             names
	noServiceAccounts bool
}

// Brand sets what invitation emails have in common with the app's other
// emails: its name, link, logo, support address and footer (mail.Brand).
// Without it, invitations carry the app's name (gorbital.WithName) linking
// to APP_PUBLIC_URL, as sign-in's emails do without authhttp.Brand; pass the
// same value to both.
func Brand(b mail.Brand) Option {
	return func(o *options) { o.brand = &b }
}

// WithoutServiceAccounts leaves out the operations on organisations'
// service accounts and their API keys
// (/v1/orgs/{orgId}/service-accounts, ADR-0058) and the
// orgs.service_accounts.manage permission that guards them.
//
// They are sign-in's operations, registered through the [Identity]'s
// OrgServiceAccountRoutes: an identity that can't issue API keys doesn't
// have that method, and an app that mounts organisations under its own
// words ([ScopeName]) can't have them under /v1/orgs. Without this option
// gorbital.New fails in both cases, naming it.
func WithoutServiceAccounts() Option {
	return func(o *options) { o.noServiceAccounts = true }
}

// An Identity is what organisations need from the app's sign-in:
// organisation members are its accounts (org_members references
// auth_users), so it tells organisations about accounts as they are
// created and deleted, and asks them before deleting one.
// *authhttp.Authenticator is one, and nothing in the library is any other:
// the interface is the seam that keeps organisations from requiring
// gorbital's own sign-in (ADR-0088, ADR-0092).
//
// An identity that issues API keys also has
//
//	OrgServiceAccountRoutes(r *gorbital.Router)
//
// which registers the operations on organisations' service accounts;
// [WithoutServiceAccounts] leaves them out for an identity that doesn't.
type Identity interface {
	// UseOrganisations connects the identity to o: a new account gets a
	// personal workspace, deleting an account is refused while it is an
	// organisation's only owner and otherwise leaves its organisations,
	// and o authorizes the organisations whose service accounts the
	// identity manages. It is called once, while the app is built.
	UseOrganisations(o authhttp.Organisations) error
}

// serviceAccounts is the part of an [Identity] that issues API keys.
type serviceAccounts interface {
	OrgServiceAccountRoutes(r *gorbital.Router)
}

// Module returns organisations as a module named "orgs". id is the app's
// sign-in, the value passed to gorbital.WithAuth when that is
// gorbital.dev/gorbital/authhttp: organisation members are its accounts
// (org_members references auth_users), a new account gets a personal
// workspace, deleting an account leaves or deletes its organisations, and
// organisations' service accounts are sign-in's.
//
// gorbital.New fails with a configuration error when id is nil, or is the
// app's authenticator's type and not the app's authenticator. Mounted by
// hand with gorbital.Mount, the module registers its routes for the
// OpenAPI document only.
func Module(id Identity, opts ...Option) gorbital.Module {
	return newModule(id, opts...).gorbitalModule()
}

// newModule applies opts.
func newModule(id Identity, opts ...Option) *module {
	var o options
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	return &module{auth: id, opts: o, settings: &orgSettings{}}
}

// gorbitalModule is the module's declaration.
func (m *module) gorbitalModule() gorbital.Module {
	return gorbital.Module{
		Name:         "orgs",
		Errors:       errorMappings(m.opts.scope()),
		Permissions:  permissions(m.opts.serviceAccounts()),
		Settings:     m.settings.declare,
		Jobs:         m.defineJobs,
		Migrations:   moduleMigrations(),
		RateLimiters: []gorbital.RateLimiter{{Name: invitationsLimiter, Keys: "user ID", Description: "Invitations each user sends or resends per hour across their organisations (orgs.user_invitations_per_hour)"}},
		Retention: func(gorbital.Deps) []gorbital.Retention {
			return []gorbital.Retention{{Data: "deleted_organisations", Setting: m.settings.deletedOrgRetention, Job: purgeJob}}
		},
		Platform: m.platform,
		Routes: func(r *gorbital.Router, _ gorbital.Deps) {
			// svc is nil while the OpenAPI document is exported: the
			// operations are registered, but no use case runs.
			delivery.Register(r, m.service(), m.vocabulary())
			if sa, ok := m.serviceAccounts(); ok {
				sa.OrgServiceAccountRoutes(r)
			}
		},
	}
}

// invitationsLimiter limits invitations per user across organisations.
const invitationsLimiter = "orgs_invitations"

// module is what one Module value builds.
type module struct {
	auth     Identity
	opts     options
	settings *orgSettings

	mu   sync.Mutex
	deps gorbital.Deps // captured by Jobs, which gorbital.New calls before Platform
	svc  *usecase.Service
}

// service returns the use cases, or nil before Platform.
func (m *module) service() *usecase.Service {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.svc
}

// errNoAuthenticator reports orgshttp.Module without the app's sign-in.
var errNoAuthenticator = errors.New("orgshttp.Module needs the app's sign-in: pass the *authhttp.Authenticator given to gorbital.WithAuth, or another orgshttp.Identity")

// errServiceAccounts reports service account operations that can't be
// registered under the app's words or by its identity.
var errServiceAccounts = errors.New("add orgshttp.WithoutServiceAccounts()")

// vocabulary is the words the operations are mounted under.
func (m *module) vocabulary() delivery.Vocabulary {
	v := delivery.Organisations()
	if n := m.opts.names; n.named() {
		v = delivery.Vocabulary{Plural: n.plural, Param: n.param, Tag: strings.ToUpper(n.plural[:1]) + n.plural[1:]}
	}
	return v
}

// serviceAccounts returns the identity that registers the organisation
// service account operations, and false when the app left them out or the
// identity can't issue API keys.
func (m *module) serviceAccounts() (serviceAccounts, bool) {
	if !m.opts.serviceAccounts() {
		return nil, false
	}
	sa, ok := m.auth.(serviceAccounts)
	return sa, ok
}

// serviceAccounts reports whether the app kept the service account
// operations.
func (o options) serviceAccounts() bool { return !o.noServiceAccounts && !o.names.named() }

// platform builds the use cases from what the app built, connects them to
// sign-in and makes them the app's scope.
func (m *module) platform(p *gorbital.Platform) error {
	if m.auth == nil || nilAuthenticator(m.auth) {
		return errNoAuthenticator
	}
	if a, ok := m.auth.(gorbital.Authenticator); ok && p.Authenticator != a {
		return fmt.Errorf("orgshttp.Module was given another Authenticator than gorbital.WithAuth: %w", errNoAuthenticator)
	}
	if err := m.opts.names.validate(); err != nil {
		return err
	}
	if !m.opts.noServiceAccounts {
		if _, ok := m.auth.(serviceAccounts); !ok {
			return fmt.Errorf("orgshttp: the identity can't register organisations' service accounts (it has no OrgServiceAccountRoutes): %w", errServiceAccounts)
		}
		if m.opts.names.named() {
			return fmt.Errorf("orgshttp: organisations' service accounts stay under /v1/orgs/{orgId}/service-accounts, which orgshttp.ScopeName can't rename: %w", errServiceAccounts)
		}
	}
	m.mu.Lock()
	d := m.deps
	m.mu.Unlock()
	if d.DB == nil || d.Audit == nil || d.Mailer == nil || d.RateLimits == nil || d.Settings == nil || d.Flags == nil {
		return errors.New("orgshttp: the app's Deps need DB, Audit, Mailer, RateLimits, Settings and Flags")
	}
	if !m.settings.declared() {
		return errors.New("orgshttp: the orgs.* settings aren't declared: add the module with gorbital.WithModules")
	}
	limiter, err := d.RateLimits.Limiter(invitationsLimiter, func(ctx context.Context) ratelimit.Limit {
		return ratelimit.Per(m.settings.invitationsPerHour.Get(ctx), time.Hour)
	})
	if err != nil {
		return err
	}
	logger := d.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	svc, err := usecase.NewService(usecase.Config{
		Store:               repository.NewStore(d.DB),
		Catalog:             p.OrgPermissions,
		Recorder:            d.Audit,
		Emails:              orgslib.NewBrandedEmails(d.Mailer, m.brand(p)),
		InvitationURL:       m.settings.invitationURL,
		InvitationTTL:       m.settings.invitationTTL,
		DeletedOrgRetention: m.settings.deletedOrgRetention,
		MaxOwnedOrgs:        m.settings.maxOwned,
		InvitationLimiter:   limiter,
		Settings:            d.Settings, // settings declared OrgOverridable
		Flags:               d.Flags,    // organisation members' client flags
		Logger:              logger,
	})
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.svc = svc
	m.mu.Unlock()

	orgs := organisations{svc: svc}
	if err := m.auth.UseOrganisations(orgs); err != nil {
		return err
	}
	return p.SetScope(m.opts.scope(), orgs)
}

// nilAuthenticator reports whether id is a nil *authhttp.Authenticator,
// which a non-nil Identity can hold.
func nilAuthenticator(id Identity) bool {
	a, ok := id.(*authhttp.Authenticator)
	return ok && a == nil
}

// brand is the Brand option, with the app's name and public URL as its
// defaults.
func (m *module) brand(p *gorbital.Platform) mail.Brand {
	b := mail.Brand{}
	if m.opts.brand != nil {
		b = *m.opts.brand
	}
	if b.Name == "" {
		b.Name = p.Name
	}
	if b.URL == "" {
		b.URL = p.Config.Auth.PublicURL
	}
	return b
}

// organisations connects the use cases to sign-in (authhttp.Organisations)
// and to guard.Scope (gorbital.ScopeAuthorizer), as a v0.1 app's
// orgs_hooks.go and orgs_service_accounts.go did.
type organisations struct{ svc *usecase.Service }

var (
	_ authhttp.Organisations   = organisations{}
	_ gorbital.ScopeAuthorizer = organisations{}
)

// AuthorizeScope checks membership with orgs.RequireMember on members and
// the organisation's enabled service accounts. A non-member is refused
// with gorbital.ErrScopeNotFound, so the guard answers 404 with the
// scope's code without reading the message.
func (o organisations) AuthorizeScope(ctx context.Context, scopeID, permission string) (context.Context, error) {
	ctx, _, err := orgslib.RequireMember(ctx, o.svc.Memberships(), o.svc.Catalog(), orgslib.ID(scopeID), permission)
	if err != nil && errors.Is(err, orgslib.ErrOrgNotFound) {
		return ctx, fmt.Errorf("%w: %w", gorbital.ErrScopeNotFound, err)
	}
	return ctx, err
}

// AccountCreated gives a new account its personal workspace.
func (o organisations) AccountCreated(ctx context.Context, userID string) error {
	_, err := o.svc.EnsurePersonalWorkspace(ctx, userID)
	return err
}

// CheckAccountDeletion answers 409 sole_owner and lists the organisations to
// hand over or delete first.
func (o organisations) CheckAccountDeletion(ctx context.Context, userID string) error {
	err := o.svc.CheckAccountDeletion(ctx, userID)
	var sole *orgsdomain.SoleOwnerError
	if !errors.As(err, &sole) {
		return err
	}
	p := httpx.NewProblem(http.StatusConflict, "sole_owner",
		"you are the only owner of organisations with other members; make another member an owner, or delete them, first")
	for _, org := range sole.Orgs {
		p.Errors = append(p.Errors, httpx.FieldError{Location: "orgs", Message: org.OrgID})
	}
	return p
}

// AccountDeleted removes a deleted account from its organisations.
func (o organisations) AccountDeleted(ctx context.Context, userID string) error {
	return o.svc.RemoveAccount(ctx, userID)
}

func (o organisations) AuthorizeServiceAccounts(ctx context.Context, orgID string) (context.Context, string, error) {
	return o.svc.AuthorizeServiceAccounts(ctx, orgID)
}

func (o organisations) CanAssignServiceAccountRole(callerRole, role string) bool {
	return o.svc.CanAssignServiceAccountRole(callerRole, role)
}

func (o organisations) Catalog() *authlib.Catalog { return o.svc.Catalog() }
