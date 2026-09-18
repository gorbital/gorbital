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
// App modules scope their routes to an organisation with guard.OrgMember,
// which asks this module whether the caller is a member whose role grants a
// permission, and declare their permissions with gorbital.Permission's
// OrgRoles:
//
//	invoices := r.Group("/v1/orgs/{orgId}/invoices")
//	gorbital.Get(invoices, "", h.list, guard.OrgMember("invoices.invoice.read"))
//
// Stability: experimental until v0.2.0 (ADR-0015, ADR-0081).
package orgshttp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	authhttp "example.com/plateful/internal/modules/auth"
	"gorbital.dev/gorbital"
	"gorbital.dev/httpx"
	"gorbital.dev/mail"
	authlib "gorbital.dev/modules/auth"
	orgslib "gorbital.dev/modules/orgs"
	"gorbital.dev/ratelimit"

	"example.com/plateful/internal/modules/orgs/delivery"
	orgsdomain "example.com/plateful/internal/modules/orgs/domain"
	"example.com/plateful/internal/modules/orgs/repository"
	"example.com/plateful/internal/modules/orgs/usecase"
)

// An Option configures [Module].
type Option func(*options)

type options struct {
	brand *mail.Brand
}

// Brand sets what invitation emails have in common with the app's other
// emails: its name, link, logo, support address and footer (mail.Brand).
// Without it, invitations carry the app's name (gorbital.WithName) linking
// to APP_PUBLIC_URL, as sign-in's emails do without authhttp.Brand; pass the
// same value to both.
func Brand(b mail.Brand) Option {
	return func(o *options) { o.brand = &b }
}

// Module returns organisations as a module named "orgs". auth is the app's
// sign-in, the value passed to gorbital.WithAuth: organisation members are
// its accounts (org_members references auth_users), a new account gets a
// personal workspace, deleting an account leaves or deletes its
// organisations, and organisations' service accounts are sign-in's.
//
// gorbital.New fails with a configuration error when auth is nil or isn't
// the app's authenticator. Mounted by hand with gorbital.Mount, the module
// registers its routes for the OpenAPI document only.
func Module(auth *authhttp.Authenticator, opts ...Option) gorbital.Module {
	return newModule(auth, opts...).gorbitalModule()
}

// newModule applies opts.
func newModule(auth *authhttp.Authenticator, opts ...Option) *module {
	var o options
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	return &module{auth: auth, opts: o, settings: &orgSettings{}}
}

// gorbitalModule is the module's declaration.
func (m *module) gorbitalModule() gorbital.Module {
	return gorbital.Module{
		Name:         "orgs",
		Errors:       errorMappings(),
		Permissions:  permissions(),
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
			delivery.Register(r, m.service())
			if m.auth != nil {
				m.auth.OrgServiceAccountRoutes(r)
			}
		},
	}
}

// invitationsLimiter limits invitations per user across organisations.
const invitationsLimiter = "orgs_invitations"

// module is what one Module value builds.
type module struct {
	auth     *authhttp.Authenticator
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
var errNoAuthenticator = errors.New("orgshttp.Module needs the app's sign-in: pass the *authhttp.Authenticator given to gorbital.WithAuth")

// platform builds the use cases from what the app built, connects them to
// sign-in and makes them the app's organisation authorizer.
func (m *module) platform(p *gorbital.Platform) error {
	if m.auth == nil {
		return errNoAuthenticator
	}
	if p.Authenticator != gorbital.Authenticator(m.auth) {
		return fmt.Errorf("orgshttp.Module was given another Authenticator than gorbital.WithAuth: %w", errNoAuthenticator)
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
	return p.SetOrgAuthorizer(orgs)
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
// and to guard.OrgMember (gorbital.OrgAuthorizer), as a v0.1 app's
// orgs_hooks.go and orgs_service_accounts.go did.
type organisations struct{ svc *usecase.Service }

var (
	_ authhttp.Organisations = organisations{}
	_ gorbital.OrgAuthorizer = organisations{}
)

// AuthorizeOrg checks membership with orgs.RequireMember on members and the
// organisation's enabled service accounts.
func (o organisations) AuthorizeOrg(ctx context.Context, orgID, permission string) (context.Context, error) {
	ctx, _, err := orgslib.RequireMember(ctx, o.svc.Memberships(), o.svc.Catalog(), orgslib.ID(orgID), permission)
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
