package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/modules/postgres"

	"example.com/plateful/internal/modules/projects/domain"
)

const (
	selectProjectSQL = `SELECT ` + projectColumns + ` FROM projects WHERE id = $1 AND org_id = $2`
	forUpdate        = ` FOR UPDATE`
)

// SelectProject returns one of orgID's projects, or ErrProjectNotFound.
// lock locks the row until the transaction ends.
func (s *Store) SelectProject(ctx context.Context, orgID, id string, lock bool) (domain.Project, error) {
	sql := selectProjectSQL
	if lock {
		sql += forUpdate
	}
	rows, err := s.db.Query(ctx, sql, id, orgID)
	if err != nil {
		return domain.Project{}, err
	}
	project, err := pgx.CollectExactlyOneRow(rows, scanProject)
	if postgres.IsNoRows(err) {
		return domain.Project{}, domain.ErrProjectNotFound
	}
	return project, err
}
