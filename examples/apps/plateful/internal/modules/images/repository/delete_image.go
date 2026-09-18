package repository

import (
	"context"

	"example.com/plateful/internal/modules/images/domain"
)

const deleteImageSQL = `DELETE FROM images WHERE org_id = $1 AND id = $2`

// DeleteImage removes the organisation's image row, or returns
// ErrImageNotFound when it has none with that ID.
func (s *Store) DeleteImage(ctx context.Context, orgID, id string) error {
	tag, err := s.db.Exec(ctx, deleteImageSQL, orgID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrImageNotFound
	}
	return nil
}
