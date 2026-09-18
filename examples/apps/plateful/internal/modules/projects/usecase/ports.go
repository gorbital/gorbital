package usecase

import (
	"context"
	"time"

	"gorbital.dev/page"

	"example.com/plateful/internal/modules/projects/domain"
)

// ListQuery selects one page of an organisation's projects.
type ListQuery struct {
	OrgID string
	// Status keeps projects with this status; empty keeps all.
	Status domain.Status
	// Sort is one of the sortable fields: created_at, updated_at or name.
	Sort page.SortField
	// After, when set, starts the page after this position.
	After *Position
	Limit int
}

// Position is where a page ended: the last project's sort value and ID.
type Position struct {
	Time time.Time // when sorting by created_at or updated_at
	Text string    // when sorting by a text field
	ID   string
}

// Store reads and writes projects; repository.Store implements it with SQL.
// Every method is limited to one organisation's projects.
type Store interface {
	// InsertProject stores a new project, or returns ErrProjectNameTaken.
	InsertProject(ctx context.Context, project domain.Project) (domain.Project, error)
	// SelectProject returns one of orgID's projects, or ErrProjectNotFound.
	// lock locks the row until the transaction ends.
	SelectProject(ctx context.Context, orgID, id string, lock bool) (domain.Project, error)
	// SelectProjects returns up to q.Limit projects in q.Sort order, with the
	// ID breaking ties.
	SelectProjects(ctx context.Context, q ListQuery) ([]domain.Project, error)
	// UpdateProject saves project when the stored version is still project.Version and
	// increments the version. It returns ErrProjectVersionConflict when the
	// version changed or the project is gone, and ErrProjectNameTaken.
	UpdateProject(ctx context.Context, project domain.Project) (domain.Project, error)
	// DeleteProject removes one of orgID's projects, or returns
	// ErrProjectNotFound.
	DeleteProject(ctx context.Context, orgID, id string) error
	// InTx runs fn in one transaction: it commits when fn returns nil and
	// rolls back otherwise.
	InTx(ctx context.Context, fn func(tx Store) error) error
}
