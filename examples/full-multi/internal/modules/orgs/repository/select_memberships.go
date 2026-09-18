package repository

import (
	"context"

	orgsdomain "example.com/acme-api/internal/modules/orgs/domain"
	"github.com/jackc/pgx/v5"
)

const selectMembershipsSQL = `
	SELECT ` + orgColumns + `, m.role
	FROM org_members m JOIN orgs o ON o.id = m.org_id
	WHERE m.user_id = $1 AND o.deleted_at IS NULL
	ORDER BY o.personal DESC, lower(o.name) COLLATE "C", o.id`

// SelectMemberships returns userID's live organisations: the personal
// workspace first, then by name.
func (s *Store) SelectMemberships(ctx context.Context, userID string) ([]orgsdomain.Membership, error) {
	rows, err := s.db.Query(ctx, selectMembershipsSQL, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanMembership)
}
