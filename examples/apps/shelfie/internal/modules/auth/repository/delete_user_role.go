package repository

import "context"

const deleteUserRoleSQL = `DELETE FROM auth_user_roles WHERE user_id = $1 AND role = $2 AND org_id IS NULL`

// DeleteUserRole removes a platform role and reports whether the user had it.
func (s *Store) DeleteUserRole(ctx context.Context, userID, role string) (bool, error) {
	tag, err := s.db.Exec(ctx, deleteUserRoleSQL, userID, role)
	return tag.RowsAffected() == 1, err
}
