package repository

import (
	"context"

	"example.com/app/internal/modules/records/domain"
)

const deleteRecordSQL = `DELETE FROM records WHERE id = $1 AND merchant_id = $2`

// DeleteRecord removes one of merchantID's records, or returns
// ErrRecordNotFound.
func (s *Store) DeleteRecord(ctx context.Context, merchantID, id string) error {
	tag, err := s.db.Exec(ctx, deleteRecordSQL, id, merchantID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrRecordNotFound
	}
	return nil
}
