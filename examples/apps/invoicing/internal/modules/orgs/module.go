package orgshttp

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gorbital.dev/actor"
	"gorbital.dev/gorbital"
	"gorbital.dev/httpx"
	"gorbital.dev/modules/jobs"
	orgslib "gorbital.dev/modules/orgs"
	"gorbital.dev/modules/settings"

	"example.com/invoicing/internal/modules/orgs/delivery/jobs/orgspurge"
	orgsdomain "example.com/invoicing/internal/modules/orgs/domain"
	"example.com/invoicing/internal/modules/orgs/repository/migrations"
	"example.com/invoicing/internal/modules/orgs/usecase"
)

// purgeJob is the name of the job that purges deleted organisations.
const purgeJob = orgspurge.Name

// errorMappings are the error codes of a v0.1 app's module_orgs.go: the
// organisations module's, and those every org-scoped module shares. Error
// codes are public API: add new ones, never change existing ones.
func errorMappings() []httpx.Mapping {
	return []httpx.Mapping{
		{Err: actor.ErrUnauthenticated, Status: http.StatusUnauthorized, Code: "unauthenticated", Detail: "authentication is required"},
		{Err: actor.ErrForbidden, Status: http.StatusForbidden, Code: "forbidden", Detail: "your role in this organisation doesn't allow this"},
		{Err: actor.ErrStepUpRequired, Status: http.StatusForbidden, Code: "mfa_required", Detail: "sign in with two-factor authentication to do this in this organisation"},
		{Err: orgslib.ErrOrgNotFound, Status: http.StatusNotFound, Code: "org_not_found", Detail: "you aren't a member of an organisation with this ID"},
		{Err: orgsdomain.ErrUnauthenticated, Status: http.StatusUnauthorized, Code: "unauthenticated", Detail: "authentication is required"},
		{Err: orgsdomain.ErrForbidden, Status: http.StatusForbidden, Code: "forbidden", Detail: "missing permission for this operation"},
		{Err: orgsdomain.ErrSessionRequired, Status: http.StatusForbidden, Code: "session_required", Detail: "sign in to do this: an API key can't join or leave organisations"},
		{Err: orgsdomain.ErrInvalidName, Status: http.StatusUnprocessableEntity, Code: "invalid_org_name", Detail: "an organisation name must be 1 to 100 characters on one line"},
		{Err: orgsdomain.ErrOrgVersionConflict, Status: http.StatusConflict, Code: "org_version_conflict", Detail: "the organisation changed since you read it; get it again and retry"},
		{Err: orgsdomain.ErrPersonalWorkspace, Status: http.StatusConflict, Code: "personal_workspace", Detail: "a personal workspace can't be left, deleted or shared; create an organisation instead"},
		{Err: orgsdomain.ErrMemberNotFound, Status: http.StatusNotFound, Code: "member_not_found", Detail: "the organisation has no member with this ID"},
		{Err: orgsdomain.ErrUnknownRole, Status: http.StatusUnprocessableEntity, Code: "unknown_role", Detail: "the role isn't one of the organisation roles"},
		{Err: orgsdomain.ErrRoleNotAllowed, Status: http.StatusForbidden, Code: "role_not_allowed", Detail: "you can't give, change or remove a role above your own, and only owners manage owners"},
		{Err: orgsdomain.ErrLastOwner, Status: http.StatusConflict, Code: "last_owner", Detail: "an organisation needs at least one owner; make another member an owner first"},
		{Err: orgsdomain.ErrSoleOwner, Status: http.StatusConflict, Code: "sole_owner", Detail: "you are the only owner of organisations with other members; make another member an owner, or delete them, first"},
		{Err: orgsdomain.ErrAlreadyMember, Status: http.StatusConflict, Code: "already_member", Detail: "this person is already a member"},
		{Err: orgsdomain.ErrAlreadyInvited, Status: http.StatusConflict, Code: "already_invited", Detail: "this address already has an open invitation; resend it instead"},
		{Err: orgsdomain.ErrInvitationNotFound, Status: http.StatusNotFound, Code: "invitation_not_found", Detail: "the invitation doesn't exist, was used or revoked, or expired"},
		{Err: orgsdomain.ErrInvitationEmail, Status: http.StatusForbidden, Code: "invitation_for_another_email", Detail: "the invitation was sent to another address; sign in with the invited, verified address"},
		{Err: orgsdomain.ErrTooManyInvitations, Status: http.StatusTooManyRequests, Code: "too_many_invitations", Detail: "too many invitations were sent from this organisation, or by you, in the last hour; try again later"},
		{Err: orgsdomain.ErrEmailNotVerified, Status: http.StatusForbidden, Code: "email_not_verified", Detail: "verify your email address to create organisations or send invitations"},
		{Err: orgsdomain.ErrTooManyOrgs, Status: http.StatusConflict, Code: "too_many_orgs", Detail: "you own as many organisations as allowed; delete one, or hand one over, first"},
		{Err: settings.ErrNotOrgOverridable, Status: http.StatusNotFound, Code: "setting_not_found", Detail: "organisations can't set a setting with this key"},
	}
}

// permissions are a v0.1 app's organisation permissions: the platform ones
// every user holds (the user role), and the organisation ones owners,
// admins and members hold through their role in each organisation. Names
// are public API.
func permissions() []gorbital.Permission {
	owner := []string{orgslib.RoleOwner}
	admins := []string{orgslib.RoleOwner, orgslib.RoleAdmin}
	all := []string{orgslib.RoleOwner, orgslib.RoleAdmin, orgslib.RoleMember}
	return []gorbital.Permission{
		{Name: usecase.PermOrgCreate, Description: "Create organisations", Roles: []string{"user"}},
		{Name: usecase.PermOrgList, Description: "See the organisations you belong to", Roles: []string{"user"}},

		{Name: usecase.PermOrgRead, Description: "See the organisation", OrgRoles: all},
		{Name: usecase.PermOrgUpdate, Description: "Rename the organisation", OrgRoles: admins},
		{Name: usecase.PermOrgDelete, Description: "Delete and restore the organisation", OrgRoles: owner},
		{Name: usecase.PermMembersRead, Description: "See the members", OrgRoles: all},
		{Name: usecase.PermMembersManage, Description: "Invite people, change roles and remove members, up to your own role", OrgRoles: admins},
		{Name: usecase.PermSettingsRead, Description: "See the organisation's settings and their history", OrgRoles: all},
		{Name: usecase.PermSettingsWrite, Description: "Change the organisation's settings", OrgRoles: admins},
		{Name: usecase.PermServiceAccountsManage, Description: "Create and manage service accounts and their API keys, up to your own role", OrgRoles: admins},
	}
}

// moduleMigrations are the organisations module's migrations with the
// versions v0.1 multi-tenant apps carry them under in db/migrations, byte for
// byte (ADR-0083). Released versions never change; add new migrations with
// later versions.
func moduleMigrations() []gorbital.Migration {
	return []gorbital.Migration{
		{Version: 20260916000001, Name: "orgs", FS: migrations.FS, File: "00001_orgs.sql"},
		{Version: 20260918000002, Name: "settings_org_purge", FS: migrations.FS, File: "00002_settings_org_purge.sql"},
	}
}

// defineJobs keeps the app's dependencies for Platform and defines
// orgs_purge with a v0.1 app's defaults. Operators can override them in
// /ops/jobs/definitions/orgs_purge.
func (m *module) defineJobs(defs *jobs.Definitions, d gorbital.Deps) {
	m.mu.Lock()
	m.deps = d
	m.mu.Unlock()
	jobs.Define(defs, jobs.Definition[orgspurge.Args]{
		Name:        orgspurge.Name,
		Description: "Removes organisations deleted longer ago than orgs.deleted_org_retention, with their members, invitations and data.",
		Worker:      orgspurge.NewWorker(m.purge, d.Logger),
		NewArgs:     func() orgspurge.Args { return orgspurge.Args{} },
		Enabled:     true,
		Schedule:    "45 3 * * *",
		Timeout:     10 * time.Minute,
		MaxAttempts: 3,
		Queue:       "default",
		Priority:    2,
	})
}

// errNotBuilt reports a job run in an app whose module wasn't built.
var errNotBuilt = errors.New("orgshttp: the organisations module isn't built (gorbital.New)")

// purge runs the use case once the module is built.
func (m *module) purge(ctx context.Context) (int, error) {
	svc := m.service()
	if svc == nil {
		return 0, errNotBuilt
	}
	return svc.Purge(ctx)
}

// orgSettings are the organisations module's runtime settings, with the
// keys, defaults and rules of a v0.1 app's settings.go, so values operators
// changed stay in effect. Setting keys are public API.
type orgSettings struct {
	invitationURL       *settings.Setting[string]
	invitationTTL       *settings.Setting[time.Duration]
	deletedOrgRetention *settings.Setting[time.Duration]
	maxOwned            *settings.Setting[int]
	invitationsPerHour  *settings.Setting[int]
}

// defaultInvitationURL is where invitation links go until the frontend's
// page is set in orgs.invitation_url.
const defaultInvitationURL = "http://localhost:3000/invitations"

func (s *orgSettings) declared() bool { return s.invitationURL != nil }

func (s *orgSettings) declare(reg *settings.Registry) {
	s.invitationURL = settings.String(reg, "orgs.invitation_url", defaultInvitationURL,
		settings.Describe("The frontend page invitation emails link to. The link adds #token=… for the page to POST to /v1/invitations/accept."),
		settings.Group("orgs"),
		settings.MaxLen(500),
		settings.Validate(absoluteHTTPURL),
		settings.ReasonRequired(), // where invitation links, with their tokens, go
	)
	s.invitationTTL = settings.Duration(reg, "orgs.invitation_ttl", usecase.DefaultInvitationTTL,
		settings.Describe("How long an invitation link stays valid. Owners and admins may set their organisation's own value, within the same range, with PUT /v1/orgs/{orgId}/settings/orgs.invitation_ttl."),
		settings.Group("orgs"),
		settings.Range(24*time.Hour, 30*24*time.Hour),
		settings.ReasonRequired(),
		settings.OrgOverridable(), // an example organisation setting (ADR-0056)
	)
	s.deletedOrgRetention = settings.Duration(reg, "orgs.deleted_org_retention", usecase.DefaultDeletedOrgRetention,
		settings.Describe("How long a deleted organisation can be restored before the orgs_purge job removes it with its data."),
		settings.Group("orgs"),
		settings.Range(24*time.Hour, 365*24*time.Hour),
		settings.ReasonRequired(),
	)
	s.maxOwned = settings.Int(reg, "orgs.max_owned", usecase.DefaultMaxOwnedOrgs,
		settings.Describe("How many organisations, personal workspaces aside, one user may own. Checked when they create or restore one."),
		settings.Group("orgs"),
		settings.Range(1, 10_000),
		settings.ReasonRequired(),
	)
	s.invitationsPerHour = settings.Int(reg, "orgs.user_invitations_per_hour", usecase.DefaultUserInvitationsPerHour,
		settings.Describe("Invitations one user may send or resend per hour across all their organisations, on top of each organisation's 20 an hour."),
		settings.Group("rate_limits"),
		settings.Range(1, 10_000),
		settings.ReasonRequired(),
	)
}

// absoluteHTTPURL accepts http and https URLs with a host and no fragment.
func absoluteHTTPURL(s string) error {
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Fragment != "" || strings.ContainsAny(s, " \r\n") {
		return errors.New("must be an http or https URL without a #fragment, such as https://app.example.com/invitations")
	}
	return nil
}
