package repository

import (
	"context"

	projectsdomain "example.com/acme-api/internal/modules/projects/domain"
)

const deleteProjectSQL = `DELETE FROM projects WHERE id = $1 AND owner_id = $2`

// DeleteProject removes one of ownerID's projects, or returns
// ErrProjectNotFound.
func (s *Store) DeleteProject(ctx context.Context, ownerID, id string) error {
	tag, err := s.db.Exec(ctx, deleteProjectSQL, id, ownerID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return projectsdomain.ErrProjectNotFound
	}
	return nil
}
