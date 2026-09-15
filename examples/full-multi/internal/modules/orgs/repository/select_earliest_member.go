package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	orgslib "apistock.dev/modules/orgs"
	"apistock.dev/modules/postgres"

	orgsdomain "example.com/acme-api/internal/modules/orgs/domain"
)

const selectEarliestMemberSQL = `
	SELECT ` + memberColumns + `
	FROM org_members m JOIN auth_users u ON u.id = m.user_id
	WHERE m.org_id = $1 AND ($2::text = '' OR m.role = $2) AND m.user_id <> $3
	ORDER BY m.joined_at, m.user_id
	LIMIT 1`

// SelectEarliestMember returns the member other than excludeUserID who
// joined first, among those with role (any role when empty), or
// ErrMemberNotFound.
func (s *Store) SelectEarliestMember(ctx context.Context, orgID orgslib.ID, role, excludeUserID string) (orgsdomain.Member, error) {
	rows, err := s.db.Query(ctx, selectEarliestMemberSQL, orgID, role, excludeUserID)
	if err != nil {
		return orgsdomain.Member{}, err
	}
	m, err := pgx.CollectExactlyOneRow(rows, scanMember)
	if postgres.IsNoRows(err) {
		return orgsdomain.Member{}, orgsdomain.ErrMemberNotFound
	}
	return m, err
}
