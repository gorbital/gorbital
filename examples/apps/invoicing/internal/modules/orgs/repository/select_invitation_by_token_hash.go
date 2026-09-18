package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/modules/postgres"

	orgsdomain "example.com/invoicing/internal/modules/orgs/domain"
)

const selectInvitationByTokenHashSQL = `SELECT ` + invitationColumns + ` FROM org_invitations WHERE token_hash = $1`

// SelectInvitationByTokenHash returns an invitation by its token hash, or
// ErrInvitationNotFound. lock locks it until the transaction ends.
func (s *Store) SelectInvitationByTokenHash(ctx context.Context, tokenHash []byte, lock bool) (orgsdomain.Invitation, error) {
	sql := selectInvitationByTokenHashSQL
	if lock {
		sql += forUpdate
	}
	rows, err := s.db.Query(ctx, sql, tokenHash)
	if err != nil {
		return orgsdomain.Invitation{}, err
	}
	inv, err := pgx.CollectExactlyOneRow(rows, scanInvitation)
	if postgres.IsNoRows(err) {
		return orgsdomain.Invitation{}, orgsdomain.ErrInvitationNotFound
	}
	return inv, err
}
