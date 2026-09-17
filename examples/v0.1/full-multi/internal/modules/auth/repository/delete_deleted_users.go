package repository

import (
	"context"
	"time"
)

const deleteDeletedUsersSQL = `DELETE FROM auth_users WHERE deleted_at < $1`

// DeleteDeletedUsers removes accounts deleted before before; their sessions,
// codes and roles go with them (ON DELETE CASCADE).
func (s *Store) DeleteDeletedUsers(ctx context.Context, before time.Time) (int64, error) {
	tag, err := s.db.Exec(ctx, deleteDeletedUsersSQL, before)
	return tag.RowsAffected(), err
}
