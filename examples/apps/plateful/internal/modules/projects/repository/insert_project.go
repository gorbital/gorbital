package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"example.com/plateful/internal/modules/projects/domain"
)

const insertProjectSQL = `
	INSERT INTO projects (id, org_id, created_by, name, description, status, version, created_at, updated_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	RETURNING ` + projectColumns

// InsertProject stores a new project, or returns ErrProjectNameTaken when the
// organisation already uses the value, ignoring case.
func (s *Store) InsertProject(ctx context.Context, project domain.Project) (domain.Project, error) {
	rows, err := s.db.Query(ctx, insertProjectSQL,
		project.ID, project.OrgID, project.CreatedBy, project.Name, project.Description, project.Status, project.Version, project.CreatedAt, project.UpdatedAt)
	if err == nil {
		project, err = pgx.CollectExactlyOneRow(rows, scanProject)
	}
	return project, constraintError(err)
}
