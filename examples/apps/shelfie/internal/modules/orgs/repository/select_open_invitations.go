package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	orgslib "gorbital.dev/modules/orgs"

	orgsdomain "example.com/shelfie/internal/modules/orgs/domain"
)

const selectOpenInvitationsSQL = `
	SELECT ` + invitationColumns + ` FROM org_invitations
	WHERE org_id = $1 AND accepted_at IS NULL AND revoked_at IS NULL
	ORDER BY created_at DESC, id DESC`

// SelectOpenInvitations returns an organisation's invitations that weren't
// accepted or revoked, newest first.
func (s *Store) SelectOpenInvitations(ctx context.Context, orgID orgslib.ID) ([]orgsdomain.Invitation, error) {
	rows, err := s.db.Query(ctx, selectOpenInvitationsSQL, orgID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanInvitation)
}
