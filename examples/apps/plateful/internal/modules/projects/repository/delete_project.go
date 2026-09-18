package repository

import (
	"context"

	"example.com/plateful/internal/modules/projects/domain"
)

const deleteProjectSQL = `DELETE FROM projects WHERE id = $1 AND org_id = $2`

// DeleteProject removes one of orgID's projects, or returns
// ErrProjectNotFound.
func (s *Store) DeleteProject(ctx context.Context, orgID, id string) error {
	tag, err := s.db.Exec(ctx, deleteProjectSQL, id, orgID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrProjectNotFound
	}
	return nil
}
