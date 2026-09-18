package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	orgslib "gorbital.dev/modules/orgs"
	"gorbital.dev/modules/postgres"

	orgsdomain "example.com/plateful/internal/modules/orgs/domain"
)

const selectMemberSQL = `
	SELECT ` + memberColumns + `
	FROM org_members m JOIN auth_users u ON u.id = m.user_id
	WHERE m.org_id = $1 AND m.user_id = $2`

// SelectMember returns a member of an organisation, deleted or not, or
// ErrMemberNotFound.
func (s *Store) SelectMember(ctx context.Context, orgID orgslib.ID, userID string) (orgsdomain.Member, error) {
	rows, err := s.db.Query(ctx, selectMemberSQL, orgID, userID)
	if err != nil {
		return orgsdomain.Member{}, err
	}
	m, err := pgx.CollectExactlyOneRow(rows, scanMember)
	if postgres.IsNoRows(err) {
		return orgsdomain.Member{}, orgsdomain.ErrMemberNotFound
	}
	return m, err
}
