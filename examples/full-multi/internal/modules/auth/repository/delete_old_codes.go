package repository

import (
	"context"
	"time"
)

const deleteOldCodesSQL = `DELETE FROM auth_codes WHERE created_at < $1`

// DeleteOldCodes removes codes issued before before.
func (s *Store) DeleteOldCodes(ctx context.Context, before time.Time) (int64, error) {
	tag, err := s.db.Exec(ctx, deleteOldCodesSQL, before)
	return tag.RowsAffected(), err
}
