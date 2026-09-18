// Package repository stores the projects module's projects in PostgreSQL
// with hand-written SQL, one file per operation. The table comes from
// db/migrations.
package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"gorbital.dev/modules/postgres"

	"example.com/plateful/internal/modules/projects/domain"
	"example.com/plateful/internal/modules/projects/usecase"
)

// Store implements usecase.Store. It runs on the pool, or on a transaction
// inside InTx.
type Store struct {
	db   postgres.DBTX
	pool *pgxpool.Pool // nil inside a transaction
}

var _ usecase.Store = (*Store)(nil)

// NewStore returns a store on pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{db: pool, pool: pool}
}

// InTx runs fn with a store bound to one transaction. Inside a transaction,
// fn joins it.
func (s *Store) InTx(ctx context.Context, fn func(tx usecase.Store) error) error {
	if s.pool == nil {
		return fn(s)
	}
	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		return fn(&Store{db: tx})
	})
}

// projectColumns are the columns scanProject reads, in its order.
const projectColumns = `id, org_id, created_by, name, description, status, version, created_at, updated_at`

func scanProject(row pgx.CollectableRow) (domain.Project, error) {
	var project domain.Project
	err := row.Scan(&project.ID, &project.OrgID, &project.CreatedBy, &project.Name, &project.Description, &project.Status, &project.Version, &project.CreatedAt, &project.UpdatedAt)
	project.CreatedAt, project.UpdatedAt = project.CreatedAt.UTC(), project.UpdatedAt.UTC()
	return project, err
}

// constraintError turns the constraint violations the use cases handle into
// domain errors.
func constraintError(err error) error {
	if constraint, ok := postgres.UniqueViolation(err); ok {
		switch constraint {
		case "projects_org_name":
			return domain.ErrProjectNameTaken
		}
	}
	return err
}
