package usecase

import (
	"context"
	"time"

	"apistock.dev/page"

	projectsdomain "example.com/acme-api/internal/modules/projects/domain"
)

// The use cases own these ports; internal/modules/projects/repository
// implements them with SQL.

// ListQuery selects one page of an owner's projects.
type ListQuery struct {
	OwnerID string
	// Status keeps projects with this status; empty keeps every status.
	Status projectsdomain.Status
	// Sort is one of the sortable fields: created_at, updated_at or name.
	Sort page.SortField
	// After, when set, starts the page after this position.
	After *Position
	Limit int
}

// Position is where a page ended: the last project's sort value and ID.
type Position struct {
	Time time.Time // when sorting by created_at or updated_at
	Name string    // when sorting by name
	ID   string
}

// Store reads and writes projects. Every method is limited to one owner's
// projects.
type Store interface {
	// InsertProject creates a project, or returns ErrProjectNameTaken.
	InsertProject(ctx context.Context, p projectsdomain.Project) (projectsdomain.Project, error)
	// SelectProject returns one of ownerID's projects, or
	// ErrProjectNotFound. lock locks the row until the transaction ends.
	SelectProject(ctx context.Context, ownerID, id string, lock bool) (projectsdomain.Project, error)
	// SelectProjects returns up to q.Limit projects in q.Sort order, with
	// the ID breaking ties.
	SelectProjects(ctx context.Context, q ListQuery) ([]projectsdomain.Project, error)
	// UpdateProject saves p when the stored version is still p.Version and
	// increments the version. It returns ErrProjectVersionConflict when the
	// version changed or the project is gone, and ErrProjectNameTaken.
	UpdateProject(ctx context.Context, p projectsdomain.Project) (projectsdomain.Project, error)
	// DeleteProject removes one of ownerID's projects, or returns
	// ErrProjectNotFound.
	DeleteProject(ctx context.Context, ownerID, id string) error
	// InTx runs fn in one transaction: it commits when fn returns nil and
	// rolls back otherwise.
	InTx(ctx context.Context, fn func(tx Store) error) error
}
