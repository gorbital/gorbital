package repository

import (
	"context"

	"example.com/app/internal/modules/records/domain"
)

const deleteRecordSQL = `DELETE FROM records WHERE id = $1 AND org_id = $2`

// DeleteRecord removes one of orgID's records, or returns
// ErrRecordNotFound.
func (s *Store) DeleteRecord(ctx context.Context, orgID, id string) error {
	tag, err := s.db.Exec(ctx, deleteRecordSQL, id, orgID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrRecordNotFound
	}
	return nil
}
