// Package usecase holds the orgs module's application logic: organisations,
// members, invitations, personal workspaces, deletion and purging
// (ADR-0048). Every operation on an organisation starts with
// orgs.RequireMember, which checks membership and the role's permission.
// orgshttp wires it (ADR-0083).
package usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"gorbital.dev/actor"
	"gorbital.dev/audit"
	"gorbital.dev/config"
	authlib "gorbital.dev/modules/auth"
	orgslib "gorbital.dev/modules/orgs"
	"gorbital.dev/ratelimit"

	orgsdomain "example.com/plateful/internal/modules/orgs/domain"
)

// Permissions the orgs module checks. The org catalog in
// orgshttp's module.go grants them to roles. They are public API.
const (
	PermOrgRead       = "orgs.org.read"
	PermOrgUpdate     = "orgs.org.update"
	PermOrgDelete     = "orgs.org.delete"
	PermMembersRead   = "orgs.members.read"
	PermMembersManage = "orgs.members.manage"
	// PermServiceAccountsManage lets a member manage the organisation's
	// service accounts and their API keys (ADR-0058).
	PermServiceAccountsManage = "orgs.service_accounts.manage"
)

// Platform permissions the orgs module checks outside an organisation. The
// user role, through orgshttp's module.go, grants them to every user, so
// only an API key scoped without them is refused (ADR-0058). They are public
// API.
const (
	PermOrgCreate = "orgs.org.create"
	PermOrgList   = "orgs.org.list"
)

// Audit actions. They are public API (ADR-0015): add new ones, never rename.
const (
	ActionOrgCreated         = "orgs.org.created"
	ActionOrgRenamed         = "orgs.org.renamed"
	ActionOrgDeleted         = "orgs.org.deleted"
	ActionOrgRestored        = "orgs.org.restored"
	ActionOrgPurged          = "orgs.org.purged"
	ActionMemberAdded        = "orgs.member.added"
	ActionMemberRoleChanged  = "orgs.member.role_changed"
	ActionMemberRemoved      = "orgs.member.removed"
	ActionMemberLeft         = "orgs.member.left"
	ActionInvitationCreated  = "orgs.invitation.created"
	ActionInvitationResent   = "orgs.invitation.resent"
	ActionInvitationRevoked  = "orgs.invitation.revoked"
	ActionInvitationAccepted = "orgs.invitation.accepted"
)

// Limits.
const (
	// InvitationsPerHour bounds invitations an organisation sends, resends
	// included.
	InvitationsPerHour = 20
	// DefaultInvitationTTL, DefaultDeletedOrgRetention, DefaultMaxOwnedOrgs
	// and DefaultUserInvitationsPerHour are the defaults of their runtime
	// settings.
	DefaultInvitationTTL          = 7 * 24 * time.Hour
	DefaultDeletedOrgRetention    = 30 * 24 * time.Hour
	DefaultMaxOwnedOrgs           = 20
	DefaultUserInvitationsPerHour = 50
	// purgeBatch bounds organisations listed per purge query, and
	// purgeBudget how long one Purge keeps starting new batches, well within
	// the orgs_purge job's timeout. What is left waits for the next run.
	purgeBatch  = 100
	purgeBudget = 5 * time.Minute
)

// Config holds the Service's dependencies and tunables.
type Config struct {
	// Required.
	Store    Store
	Catalog  *authlib.Catalog // the org catalog, not the platform one
	Recorder audit.Recorder
	Emails   orgslib.Emails
	// InvitationURL is the frontend page invitation links open, such as
	// https://app.example.com/invitations. The token goes in the fragment.
	InvitationURL config.Value[string]

	// Optional.
	Logger *slog.Logger
	// InvitationTTL, DeletedOrgRetention and MaxOwnedOrgs are usually
	// runtime settings. MaxOwnedOrgs bounds the live organisations, personal
	// workspaces aside, a user may own through creating or restoring one.
	InvitationTTL       config.Value[time.Duration]
	DeletedOrgRetention config.Value[time.Duration]
	MaxOwnedOrgs        config.Value[int]
	// InvitationLimiter limits invitations sent or resent per user, across
	// all their organisations. The app passes a limiter shared across
	// instances (ADR-0052). Without one, an in-memory limiter allows
	// DefaultUserInvitationsPerHour per instance.
	InvitationLimiter ratelimit.Taker
	// Settings holds organisations' own values of runtime settings
	// (ADR-0056). Without it, organisations have no settings of their own.
	Settings SettingsStore
	// Flags evaluates the feature flags clients may read (ADR-0057).
	// Without it, organisations list no flags.
	Flags FlagsStore
	// Now is the clock, for tests.
	Now func() time.Time
}

// Service runs the orgs use cases. It is safe for concurrent use.
type Service struct {
	store         Store
	catalog       *authlib.Catalog
	recorder      audit.Recorder
	emails        orgslib.Emails
	invitationURL config.Value[string]
	invitationTTL config.Value[time.Duration]
	retention     config.Value[time.Duration]
	maxOwned      config.Value[int]
	invitations   ratelimit.Taker
	settings      SettingsStore
	flags         FlagsStore
	logger        *slog.Logger
	now           func() time.Time
}

// NewService returns a Service. It freezes the catalog.
func NewService(c Config) (*Service, error) {
	if c.Store == nil || c.Catalog == nil || c.Recorder == nil || c.Emails == nil || c.InvitationURL == nil {
		return nil, errors.New("orgs: invalid service: store, catalog, audit recorder, emails and invitation URL are required")
	}
	for _, role := range []string{orgslib.RoleOwner, orgslib.RoleAdmin, orgslib.RoleMember} {
		if !c.Catalog.HasRole(role) {
			return nil, fmt.Errorf("orgs: invalid service: the org catalog must declare the %s role", role)
		}
	}
	s := &Service{
		store: c.Store, catalog: c.Catalog, recorder: c.Recorder, emails: c.Emails,
		invitationURL: c.InvitationURL, invitationTTL: c.InvitationTTL, retention: c.DeletedOrgRetention,
		maxOwned: c.MaxOwnedOrgs, invitations: c.InvitationLimiter, settings: c.Settings, flags: c.Flags, logger: c.Logger, now: c.Now,
	}
	if s.invitationTTL == nil {
		s.invitationTTL = config.Static(DefaultInvitationTTL)
	}
	if s.retention == nil {
		s.retention = config.Static(DefaultDeletedOrgRetention)
	}
	if s.maxOwned == nil {
		s.maxOwned = config.Static(DefaultMaxOwnedOrgs)
	}
	if s.logger == nil {
		s.logger = slog.New(slog.DiscardHandler)
	}
	if s.now == nil {
		s.now = time.Now
	}
	if s.invitations == nil {
		s.invitations = ratelimit.New(float64(DefaultUserInvitationsPerHour)/time.Hour.Seconds(), DefaultUserInvitationsPerHour, ratelimit.WithClock(s.now))
	}
	c.Catalog.Freeze()
	return s, nil
}

// Catalog returns the org catalog.
func (s *Service) Catalog() *authlib.Catalog { return s.catalog }

// Memberships returns what other org-scoped modules pass to
// orgs.RequireMember: members, and the organisation's enabled service
// accounts with their role (ADR-0058). The orgs module's own operations
// check members only, so a service account can't manage the organisation,
// its members or invitations.
func (s *Service) Memberships() orgslib.Memberships { return resourceMemberships{s.store} }

// resourceMemberships answers for members and service accounts.
type resourceMemberships struct{ store Store }

func (m resourceMemberships) MemberRole(ctx context.Context, orgID orgslib.ID, id string) (string, error) {
	if strings.HasPrefix(id, "svc_") {
		return m.store.ServiceAccountRole(ctx, orgID, id)
	}
	return m.store.MemberRole(ctx, orgID, id)
}

// AuthorizeServiceAccounts checks that the signed-in user is a member of
// orgID whose role may manage its service accounts, and returns a context
// acting in the organisation and the member's role. Service accounts
// themselves can't.
func (s *Service) AuthorizeServiceAccounts(ctx context.Context, orgID string) (context.Context, string, error) {
	ctx, me, err := orgslib.RequireMember(ctx, s.store, s.catalog, orgslib.ID(orgID), PermServiceAccountsManage)
	if err != nil {
		return ctx, "", storeError("authorize", err)
	}
	return ctx, me.Role, nil
}

// CanAssignServiceAccountRole reports whether a member with callerRole may
// give a service account role, or manage one that has it: as for members,
// but never the owner role.
func (s *Service) CanAssignServiceAccountRole(callerRole, role string) bool {
	return role != orgslib.RoleOwner && s.canAssign(callerRole, role)
}

// clock returns the time at PostgreSQL's microsecond precision.
func (s *Service) clock() time.Time { return s.now().UTC().Truncate(time.Microsecond) }

// userID returns the signed-in user's ID, or ErrUnauthenticated.
func userID(ctx context.Context) (string, error) {
	a, ok := actor.From(ctx)
	if !ok || a.Kind != actor.KindUser || a.ID == "" {
		return "", orgsdomain.ErrUnauthenticated
	}
	return a.ID, nil
}

// requireUser returns the signed-in user's ID when their platform
// permissions include permission, and ErrForbidden otherwise: every user
// holds the orgs module's platform permissions, an API key only within its
// scopes (ADR-0058).
func requireUser(ctx context.Context, permission string) (string, error) {
	uid, err := userID(ctx)
	if err != nil {
		return "", err
	}
	if err := actor.Require(ctx, permission); err != nil {
		return "", orgsdomain.ErrForbidden
	}
	return uid, nil
}

// requireSession returns the signed-in user's ID, and ErrSessionRequired
// for a request made with an API key. Joining and leaving organisations
// change what the account's keys reach and who may act for it, so only the
// person can, like account management (ADR-0058).
func requireSession(ctx context.Context) (string, error) {
	uid, err := userID(ctx)
	if err != nil {
		return "", err
	}
	if p, ok := authlib.PrincipalFrom(ctx); ok && p.APIKey() {
		return "", orgsdomain.ErrSessionRequired
	}
	return uid, nil
}

// lockOrg locks a live organisation, then checks again, under the lock,
// that the signed-in user is a member whose role grants permission. Use
// cases call orgs.RequireMember first to fail fast, but a request that
// passed it just before a concurrent removal or demotion committed must not
// act on the old role (security review ORG-4). Membership changes lock the
// organisation row first, so what this reads is current until the
// transaction ends.
func (s *Service) lockOrg(ctx context.Context, tx Store, orgID orgslib.ID, permission string) (orgsdomain.Org, orgslib.Member, error) {
	o, err := tx.SelectOrg(ctx, orgID, true)
	if err != nil {
		return orgsdomain.Org{}, orgslib.Member{}, err
	}
	_, me, err := orgslib.RequireMember(ctx, tx, s.catalog, orgID, permission)
	if err != nil {
		return orgsdomain.Org{}, orgslib.Member{}, err
	}
	return o, me, nil
}

// requireVerified returns ErrEmailNotVerified unless userID's email address
// is verified, and the address otherwise. Creating organisations and
// sending invitations need one, so a throwaway account can't send email
// from the app's domain (security review ORG-3).
func (s *Service) requireVerified(ctx context.Context, userID string) (string, error) {
	email, _, verified, err := s.store.SelectUserEmail(ctx, userID)
	if errors.Is(err, orgsdomain.ErrMemberNotFound) {
		return "", orgsdomain.ErrUnauthenticated
	}
	if err != nil {
		return "", err
	}
	if !verified {
		return "", orgsdomain.ErrEmailNotVerified
	}
	return email, nil
}

// by names the actor in ctx for added_by and invited_by columns.
func by(ctx context.Context) string {
	a := actor.FromOrAnonymous(ctx)
	return string(a.Kind) + ":" + a.ID
}

// audit records an event after the change it describes; a failed write is
// logged, not returned. Inside RequireMember's context the recorder adds the
// organisation; orgID is set for events outside one.
func (s *Service) audit(ctx context.Context, action string, orgID orgslib.ID, resourceType, resourceID string, metadata map[string]any) {
	e := audit.Event{
		Action: action, OrgID: string(orgID), ResourceType: resourceType, ResourceID: resourceID,
		Outcome: audit.OutcomeSuccess, Metadata: metadata,
	}
	if err := s.recorder.Record(context.WithoutCancel(ctx), e); err != nil {
		s.logger.ErrorContext(ctx, "record orgs audit event", "action", action, "err", err)
	}
}

// storeError returns known errors as they are and hides the rest, such as
// driver errors, which aren't API (ADR-0018).
func storeError(op string, err error) error {
	known := []error{
		orgslib.ErrOrgNotFound, actor.ErrUnauthenticated, actor.ErrForbidden, actor.ErrStepUpRequired,
		orgsdomain.ErrUnauthenticated, orgsdomain.ErrForbidden, orgsdomain.ErrSessionRequired, orgsdomain.ErrInvalidName, orgsdomain.ErrOrgVersionConflict,
		orgsdomain.ErrPersonalWorkspace, orgsdomain.ErrMemberNotFound, orgsdomain.ErrUnknownRole,
		orgsdomain.ErrRoleNotAllowed, orgsdomain.ErrLastOwner, orgsdomain.ErrSoleOwner,
		orgsdomain.ErrAlreadyMember, orgsdomain.ErrAlreadyInvited, orgsdomain.ErrInvitationNotFound, orgsdomain.ErrInvitationEmail,
		orgsdomain.ErrTooManyInvitations, orgsdomain.ErrEmailNotVerified, orgsdomain.ErrTooManyOrgs,
	}
	for _, k := range known {
		if errors.Is(err, k) {
			return err
		}
	}
	return fmt.Errorf("orgs: %s: %v", op, err) //nolint:errorlint // driver errors aren't API (ADR-0018)
}
