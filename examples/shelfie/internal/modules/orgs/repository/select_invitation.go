package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	orgslib "gorbital.dev/modules/orgs"
	"gorbital.dev/modules/postgres"

	orgsdomain "example.com/shelfie/internal/modules/orgs/domain"
)

const selectInvitationSQL = `SELECT ` + invitationColumns + ` FROM org_invitations WHERE org_id = $1 AND id = $2 FOR UPDATE`

// SelectInvitation returns an organisation's invitation by ID, or
// ErrInvitationNotFound, and locks it.
func (s *Store) SelectInvitation(ctx context.Context, orgID orgslib.ID, id string) (orgsdomain.Invitation, error) {
	rows, err := s.db.Query(ctx, selectInvitationSQL, orgID, id)
	if err != nil {
		return orgsdomain.Invitation{}, err
	}
	inv, err := pgx.CollectExactlyOneRow(rows, scanInvitation)
	if postgres.IsNoRows(err) {
		return orgsdomain.Invitation{}, orgsdomain.ErrInvitationNotFound
	}
	return inv, err
}
