package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	orgsdomain "example.com/plateful/internal/modules/orgs/domain"
)

const insertInvitationSQL = `
	INSERT INTO org_invitations (id, org_id, email, normalized_email, role, token_hash, invited_by, created_at, sent_at, expires_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8, $9)
	RETURNING ` + invitationColumns

// InsertInvitation creates an invitation, or returns ErrAlreadyInvited when
// an open one exists for the address.
func (s *Store) InsertInvitation(ctx context.Context, inv orgsdomain.Invitation) (orgsdomain.Invitation, error) {
	rows, err := s.db.Query(ctx, insertInvitationSQL,
		inv.ID, inv.OrgID, inv.Email, inv.NormalizedEmail, inv.Role, inv.TokenHash, inv.InvitedBy, inv.CreatedAt, inv.ExpiresAt)
	if err != nil {
		return orgsdomain.Invitation{}, constraintError(err)
	}
	created, err := pgx.CollectExactlyOneRow(rows, scanInvitation)
	return created, constraintError(err)
}
