package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/modules/postgres"

	projectsdomain "example.com/acme-api/internal/modules/projects/domain"
)

const (
	selectProjectSQL = `SELECT ` + projectColumns + ` FROM projects WHERE id = $1 AND owner_id = $2`
	forUpdate        = ` FOR UPDATE`
)

// SelectProject returns one of ownerID's projects, or
// ErrProjectNotFound. lock locks the row until the transaction ends.
func (s *Store) SelectProject(ctx context.Context, ownerID, id string, lock bool) (projectsdomain.Project, error) {
	sql := selectProjectSQL
	if lock {
		sql += forUpdate
	}
	rows, err := s.db.Query(ctx, sql, id, ownerID)
	if err != nil {
		return projectsdomain.Project{}, err
	}
	p, err := pgx.CollectExactlyOneRow(rows, scanProject)
	if postgres.IsNoRows(err) {
		return projectsdomain.Project{}, projectsdomain.ErrProjectNotFound
	}
	return p, err
}
