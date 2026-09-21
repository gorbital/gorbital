package repository

import (
	"context"

	"example.com/app/internal/modules/records/domain"
)

const deleteRecordSQL = `DELETE FROM records WHERE id = $1`

// DeleteRecord removes the record with this ID, or returns
// ErrRecordNotFound.
func (s *Store) DeleteRecord(ctx context.Context, id string) error {
	tag, err := s.db.Exec(ctx, deleteRecordSQL, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrRecordNotFound
	}
	return nil
}
