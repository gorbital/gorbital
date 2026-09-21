package repository

import (
	"context"
	"time"
)

const insertUserRoleSQL = `
	INSERT INTO auth_user_roles (user_id, role, granted_at, granted_by)
	VALUES ($1, $2, $3, $4)
	ON CONFLICT (user_id, role) WHERE org_id IS NULL DO NOTHING`

// InsertUserRole grants a platform role and reports whether it was new.
func (s *Store) InsertUserRole(ctx context.Context, userID, role, grantedBy string, now time.Time) (bool, error) {
	tag, err := s.db.Exec(ctx, insertUserRoleSQL, userID, role, now, grantedBy)
	return tag.RowsAffected() == 1, err
}
