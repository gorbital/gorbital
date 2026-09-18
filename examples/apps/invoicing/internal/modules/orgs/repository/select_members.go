package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	orgslib "gorbital.dev/modules/orgs"

	orgsdomain "example.com/invoicing/internal/modules/orgs/domain"
)

const selectMembersSQL = `
	SELECT ` + memberColumns + `
	FROM org_members m JOIN auth_users u ON u.id = m.user_id
	WHERE m.org_id = $1
	ORDER BY (m.role = 'owner') DESC, m.joined_at, m.user_id`

// SelectMembers returns an organisation's members, owners first, then in the
// order they joined.
func (s *Store) SelectMembers(ctx context.Context, orgID orgslib.ID) ([]orgsdomain.Member, error) {
	rows, err := s.db.Query(ctx, selectMembersSQL, orgID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanMember)
}
