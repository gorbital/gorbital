package usecase

import (
	"context"
	"encoding/json"
	"time"

	orgslib "gorbital.dev/modules/orgs"
	"gorbital.dev/modules/settings"

	orgsdomain "example.com/invoicing/internal/modules/orgs/domain"
)

// The use cases own this port; orgshttp/internal/repository implements
// it with SQL.

// Store reads and writes organisations, members and invitations. Unless a
// method says otherwise, deleted organisations aren't found.
type Store interface {
	orgslib.Memberships

	// ServiceAccountRole returns the role of an enabled service account of a
	// live organisation, or orgs.ErrNotMember (ADR-0058).
	ServiceAccountRole(ctx context.Context, orgID orgslib.ID, serviceAccountID string) (string, error)

	// InsertOrg creates an organisation.
	InsertOrg(ctx context.Context, o orgsdomain.Org) (orgsdomain.Org, error)
	// SelectOrg returns a live organisation, or orgs.ErrOrgNotFound. lock
	// locks the row until the transaction ends; membership changes lock it
	// first, so owner counts stay consistent.
	SelectOrg(ctx context.Context, id orgslib.ID, lock bool) (orgsdomain.Org, error)
	// SelectDeletedOrg returns a deleted organisation that isn't purged yet,
	// or orgs.ErrOrgNotFound, and locks it.
	SelectDeletedOrg(ctx context.Context, id orgslib.ID) (orgsdomain.Org, error)
	// SelectPersonalOrg returns userID's personal workspace, deleted or not,
	// and whether there is one.
	SelectPersonalOrg(ctx context.Context, userID string) (orgsdomain.Org, bool, error)
	// SelectMemberships returns userID's live organisations: the personal
	// workspace first, then by name.
	SelectMemberships(ctx context.Context, userID string) ([]orgsdomain.Membership, error)
	// UpdateOrgName renames an organisation at version and increments the
	// version, or returns ErrOrgVersionConflict.
	UpdateOrgName(ctx context.Context, id orgslib.ID, name string, version int64, now time.Time) (orgsdomain.Org, error)
	// LockUserOrgs takes a lock on userID's owned organisations until the
	// transaction ends, so checks of how many they own are serialized.
	LockUserOrgs(ctx context.Context, userID string) error
	// CountOwnedOrgs returns how many live organisations userID owns,
	// personal workspaces aside.
	CountOwnedOrgs(ctx context.Context, userID string) (int, error)
	// MarkOrgDeleted soft deletes an organisation until purgeAfter.
	MarkOrgDeleted(ctx context.Context, id orgslib.ID, now, purgeAfter time.Time) error
	// RestoreOrg undoes MarkOrgDeleted.
	RestoreOrg(ctx context.Context, id orgslib.ID, now time.Time) error
	// SelectOrgsToPurge returns up to limit organisations whose purge time
	// has passed.
	SelectOrgsToPurge(ctx context.Context, now time.Time, limit int) ([]orgslib.ID, error)
	// DeleteOrg removes an organisation whose purge time has passed and,
	// through foreign keys, every org-scoped row. It reports false when the
	// organisation was restored, or deleted again with a later purge time,
	// since it was selected.
	DeleteOrg(ctx context.Context, id orgslib.ID, now time.Time) (bool, error)

	// InsertMember adds a member, or returns ErrAlreadyMember.
	InsertMember(ctx context.Context, m orgsdomain.Member) error
	// SelectMember returns a member of an organisation, deleted or not, or
	// ErrMemberNotFound.
	SelectMember(ctx context.Context, orgID orgslib.ID, userID string) (orgsdomain.Member, error)
	// SelectMembers returns an organisation's members, owners first.
	SelectMembers(ctx context.Context, orgID orgslib.ID) ([]orgsdomain.Member, error)
	// IsMemberEmail reports whether a user with this normalized, verified
	// email address is a member.
	IsMemberEmail(ctx context.Context, orgID orgslib.ID, normalizedEmail string) (bool, error)
	// UpdateMemberRole changes a member's role, or returns ErrMemberNotFound.
	UpdateMemberRole(ctx context.Context, orgID orgslib.ID, userID, role string) error
	// DeleteMember removes a member, or returns ErrMemberNotFound.
	DeleteMember(ctx context.Context, orgID orgslib.ID, userID string) error
	// CountOwners returns how many owners other than excludeUserID an
	// organisation has, counting only live accounts.
	CountOwners(ctx context.Context, orgID orgslib.ID, excludeUserID string) (int, error)
	// SelectUserOrgs returns every organisation userID belongs to, deleted or
	// not, with the user's role, the number of owners and of members with
	// live accounts.
	SelectUserOrgs(ctx context.Context, userID string) ([]UserOrg, error)
	// SelectEarliestMember returns the member with a live account other than
	// excludeUserID who joined first, among those with role (any role when
	// empty), or ErrMemberNotFound.
	SelectEarliestMember(ctx context.Context, orgID orgslib.ID, role, excludeUserID string) (orgsdomain.Member, error)

	// SelectUserEmail returns an account's email address and whether it is
	// verified, or ErrMemberNotFound when there is no live account. It reads
	// the auth module's accounts table.
	SelectUserEmail(ctx context.Context, userID string) (email, normalized string, verified bool, err error)

	// InsertInvitation creates an invitation, or returns ErrAlreadyInvited
	// when an open one exists for the address.
	InsertInvitation(ctx context.Context, inv orgsdomain.Invitation) (orgsdomain.Invitation, error)
	// SelectInvitation returns an organisation's invitation by ID, or
	// ErrInvitationNotFound, and locks it.
	SelectInvitation(ctx context.Context, orgID orgslib.ID, id string) (orgsdomain.Invitation, error)
	// SelectInvitationByTokenHash returns an invitation by its token hash,
	// or ErrInvitationNotFound. lock locks it until the transaction ends.
	SelectInvitationByTokenHash(ctx context.Context, tokenHash []byte, lock bool) (orgsdomain.Invitation, error)
	// SelectOpenInvitations returns an organisation's invitations that
	// weren't accepted or revoked, newest first.
	SelectOpenInvitations(ctx context.Context, orgID orgslib.ID) ([]orgsdomain.Invitation, error)
	// CountInvitationsSince counts an organisation's invitations sent or
	// resent after since.
	CountInvitationsSince(ctx context.Context, orgID orgslib.ID, since time.Time) (int, error)
	// ReplaceInvitationToken gives an invitation a new token, inviter and
	// expiry.
	ReplaceInvitationToken(ctx context.Context, id string, tokenHash []byte, invitedBy string, now, expiresAt time.Time) error
	// RevokeInvitation marks an invitation revoked.
	RevokeInvitation(ctx context.Context, id string, now time.Time) error
	// RevokeExpiredInvitation revokes the open, expired invitation for an
	// address, so a new one can be sent.
	RevokeExpiredInvitation(ctx context.Context, orgID orgslib.ID, normalizedEmail string, now time.Time) error
	// AcceptInvitation marks an invitation accepted.
	AcceptInvitation(ctx context.Context, id string, now time.Time) error

	// InTx runs fn in one transaction: it commits when fn returns nil and
	// rolls back otherwise.
	InTx(ctx context.Context, fn func(tx Store) error) error
}

// UserOrg is one of a user's organisations, for account deletion.
type UserOrg struct {
	OrgID    orgslib.ID
	Personal bool
	Deleted  bool
	Role     string
	Owners   int
	Members  int
}

// SettingsStore stores organisations' own values of runtime settings declared
// settings.OrgOverridable (ADR-0056). *settings.Store implements it.
type SettingsStore interface {
	ListForOrg(orgID string) ([]settings.View, error)
	GetForOrg(orgID, key string) (settings.View, error)
	SetForOrg(ctx context.Context, orgID, key string, value json.RawMessage, change settings.Change) (settings.View, error)
	ResetForOrg(ctx context.Context, orgID, key string, change settings.Change) (settings.View, error)
	HistoryForOrg(ctx context.Context, orgID, key string, before int64, limit int) ([]settings.HistoryEntry, error)
}

var _ SettingsStore = (*settings.Store)(nil)
