package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
)

const selectUserRolesSQL = `SELECT role FROM auth_user_roles WHERE user_id = $1 AND org_id IS NULL ORDER BY role`

// SelectUserRoles returns a user's platform roles, sorted.
func (s *Store) SelectUserRoles(ctx context.Context, userID string) ([]string, error) {
	rows, err := s.db.Query(ctx, selectUserRolesSQL, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}
