package usecase

import (
	"context"
	"time"

	"gorbital.dev/page"

	"example.com/app/internal/modules/records/domain"
)

// ListQuery selects one page of the records.
type ListQuery struct {
	// State keeps records with this state; empty keeps all.
	State domain.State
	// Sort is one of the sortable fields: created_at, updated_at or title.
	Sort page.SortField
	// After, when set, starts the page after this position.
	After *Position
	Limit int
}

// Position is where a page ended: the last record's sort value and ID.
type Position struct {
	Time time.Time // when sorting by created_at or updated_at
	Text string    // when sorting by a text field
	ID   string
}

// Store reads and writes records; repository.Store implements it with SQL.
// It carries no access rule of its own: the rows a list returns are the
// ones Policy.Filter leaves, and every other operation is checked by the
// service before it is called.
type Store interface {
	// InsertRecord stores a new record, or returns ErrRecordTitleTaken.
	InsertRecord(ctx context.Context, record domain.Record) (domain.Record, error)
	// SelectRecord returns the record with this ID, or ErrRecordNotFound.
	// lock locks the row until the transaction ends.
	SelectRecord(ctx context.Context, id string, lock bool) (domain.Record, error)
	// SelectRecords returns up to q.Limit records in q.Sort order, with the
	// ID breaking ties.
	SelectRecords(ctx context.Context, q ListQuery) ([]domain.Record, error)
	// UpdateRecord saves record when the stored version is still record.Version and
	// increments the version. It returns ErrRecordVersionConflict when the
	// version changed or the record is gone, and ErrRecordTitleTaken.
	UpdateRecord(ctx context.Context, record domain.Record) (domain.Record, error)
	// DeleteRecord removes the record with this ID, or returns
	// ErrRecordNotFound.
	DeleteRecord(ctx context.Context, id string) error
	// InTx runs fn in one transaction: it commits when fn returns nil and
	// rolls back otherwise.
	InTx(ctx context.Context, fn func(tx Store) error) error
}

// Policy decides who may see and change one record. The module's policy.go
// implements it, and nothing else in this package knows the rule.
//
// Both methods return nil to allow, domain.ErrUnauthenticated or
// domain.ErrRecordNotFound to refuse — a record somebody may not see must
// not exist for them — or gorbital.ErrNotImplemented while the rule is
// unwritten, which the module answers with 501.
type Policy interface {
	// CanRead reports whether the actor in ctx may read record.
	CanRead(ctx context.Context, record domain.Record) error
	// CanWrite reports whether the actor in ctx may create, update or
	// delete record.
	CanWrite(ctx context.Context, record domain.Record) error
}
