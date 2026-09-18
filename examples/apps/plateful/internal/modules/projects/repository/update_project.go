package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/modules/postgres"

	"example.com/plateful/internal/modules/projects/domain"
)

const updateProjectSQL = `
	UPDATE projects
	SET name = $3, description = $4, status = $5, updated_at = $6, version = version + 1
	WHERE id = $1 AND org_id = $2 AND version = $7
	RETURNING ` + projectColumns

// UpdateProject saves project when the stored version is still project.Version and
// returns it with the next version. It returns ErrProjectVersionConflict when
// no row has that version (changed, deleted or not the organisation's), and
// ErrProjectNameTaken.
func (s *Store) UpdateProject(ctx context.Context, project domain.Project) (domain.Project, error) {
	rows, err := s.db.Query(ctx, updateProjectSQL,
		project.ID, project.OrgID, project.Name, project.Description, project.Status, project.UpdatedAt, project.Version)
	if err != nil {
		return domain.Project{}, constraintError(err)
	}
	updated, err := pgx.CollectExactlyOneRow(rows, scanProject)
	if postgres.IsNoRows(err) {
		return domain.Project{}, domain.ErrProjectVersionConflict
	}
	return updated, constraintError(err)
}
