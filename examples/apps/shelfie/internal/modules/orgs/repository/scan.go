package repository

import (
	"time"

	"github.com/jackc/pgx/v5"

	orgsdomain "example.com/shelfie/internal/modules/orgs/domain"
)

const orgColumns = `o.id, o.name, o.personal, o.created_by, o.version, o.created_at, o.updated_at, o.deleted_at, o.purge_after`

func scanOrgFields(row pgx.CollectableRow, extra ...any) (orgsdomain.Org, error) {
	var o orgsdomain.Org
	dest := append([]any{&o.ID, &o.Name, &o.Personal, &o.CreatedBy, &o.Version, &o.CreatedAt, &o.UpdatedAt, &o.DeletedAt, &o.PurgeAfter}, extra...)
	err := row.Scan(dest...)
	o.CreatedAt, o.UpdatedAt = o.CreatedAt.UTC(), o.UpdatedAt.UTC()
	o.DeletedAt, o.PurgeAfter = utc(o.DeletedAt), utc(o.PurgeAfter)
	return o, err
}

func scanOrg(row pgx.CollectableRow) (orgsdomain.Org, error) { return scanOrgFields(row) }

func scanMembership(row pgx.CollectableRow) (orgsdomain.Membership, error) {
	var m orgsdomain.Membership
	o, err := scanOrgFields(row, &m.Role)
	m.Org = o
	return m, err
}

const memberColumns = `m.org_id, m.user_id, u.email, m.role, m.joined_at, m.added_by`

func scanMember(row pgx.CollectableRow) (orgsdomain.Member, error) {
	var m orgsdomain.Member
	err := row.Scan(&m.OrgID, &m.UserID, &m.Email, &m.Role, &m.JoinedAt, &m.AddedBy)
	m.JoinedAt = m.JoinedAt.UTC()
	return m, err
}

const invitationColumns = `id, org_id, email, normalized_email, role, token_hash, invited_by, created_at, expires_at, accepted_at, revoked_at`

func scanInvitation(row pgx.CollectableRow) (orgsdomain.Invitation, error) {
	var i orgsdomain.Invitation
	err := row.Scan(&i.ID, &i.OrgID, &i.Email, &i.NormalizedEmail, &i.Role, &i.TokenHash, &i.InvitedBy,
		&i.CreatedAt, &i.ExpiresAt, &i.AcceptedAt, &i.RevokedAt)
	i.CreatedAt, i.ExpiresAt = i.CreatedAt.UTC(), i.ExpiresAt.UTC()
	i.AcceptedAt, i.RevokedAt = utc(i.AcceptedAt), utc(i.RevokedAt)
	return i, err
}

func utc(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}
