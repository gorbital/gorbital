package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	orgslib "gorbital.dev/modules/orgs"

	orgsusecase "example.com/shelfie/internal/modules/orgs/usecase"
)

const selectUserOrgsSQL = `
	SELECT o.id, o.personal, o.deleted_at IS NOT NULL, m.role,
	       (SELECT count(*) FROM org_members x JOIN auth_users u ON u.id = x.user_id
	        WHERE x.org_id = o.id AND x.role = 'owner' AND u.deleted_at IS NULL),
	       (SELECT count(*) FROM org_members x JOIN auth_users u ON u.id = x.user_id
	        WHERE x.org_id = o.id AND u.deleted_at IS NULL)
	FROM org_members m JOIN orgs o ON o.id = m.org_id
	WHERE m.user_id = $1
	ORDER BY o.id`

// SelectUserOrgs returns every organisation userID belongs to, deleted or
// not, with the user's role and the numbers of owners and members with live
// accounts.
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
