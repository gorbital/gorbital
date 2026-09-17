package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	projectsdomain "example.com/acme-api/internal/modules/projects/domain"
)

const insertProjectSQL = `
	INSERT INTO projects (id, owner_id, name, description, status, version, created_at, updated_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	RETURNING ` + projectColumns

// InsertProject creates a project, or returns ErrProjectNameTaken
// when the owner already uses the value, ignoring case.
func (s *Store) InsertProject(ctx context.Context, p projectsdomain.Project) (projectsdomain.Project, error) {
	rows, err := s.db.Query(ctx, insertProjectSQL,
		p.ID, p.OwnerID, p.Name, p.Description, p.Status, p.Version, p.CreatedAt, p.UpdatedAt)
	if err == nil {
		p, err = pgx.CollectExactlyOneRow(rows, scanProject)
	}
	return p, constraintError(err)
}
