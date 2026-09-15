package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	orgslib "apistock.dev/modules/orgs"

	orgsusecase "example.com/acme-api/internal/modules/orgs/usecase"
)

const selectUserOrgsSQL = `
	SELECT o.id, o.personal, o.deleted_at IS NOT NULL, m.role,
	       (SELECT count(*) FROM org_members x WHERE x.org_id = o.id AND x.role = 'owner'),
	       (SELECT count(*) FROM org_members x WHERE x.org_id = o.id)
	FROM org_members m JOIN orgs o ON o.id = m.org_id
	WHERE m.user_id = $1
	ORDER BY o.id`

// SelectUserOrgs returns every organisation userID belongs to, deleted or
// not, with the user's role and the numbers of owners and members.
func (s *Store) SelectUserOrgs(ctx context.Context, userID string) ([]orgsusecase.UserOrg, error) {
	rows, err := s.db.Query(ctx, selectUserOrgsSQL, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (orgsusecase.UserOrg, error) {
		var (
			o  orgsusecase.UserOrg
			id string
		)
		err := row.Scan(&id, &o.Personal, &o.Deleted, &o.Role, &o.Owners, &o.Members)
		o.OrgID = orgslib.ID(id)
		return o, err
	})
}
