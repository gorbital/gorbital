package usecase

import (
	"context"
	"time"

	"gorbital.dev/page"

	"example.com/app/internal/modules/records/domain"
)

// ListQuery selects one page of an organisation's records.
type ListQuery struct {
	OrgID string
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
// Every method is limited to one organisation's records.
type Store interface {
	// InsertRecord stores a new record, or returns ErrRecordTitleTaken.
	InsertRecord(ctx context.Context, record domain.Record) (domain.Record, error)
	// SelectRecord returns one of orgID's records, or ErrRecordNotFound.
	// lock locks the row until the transaction ends.
	SelectRecord(ctx context.Context, orgID, id string, lock bool) (domain.Record, error)
	// SelectRecords returns up to q.Limit records in q.Sort order, with the
	// ID breaking ties.
	SelectRecords(ctx context.Context, q ListQuery) ([]domain.Record, error)
	// UpdateRecord saves record when the stored version is still record.Version and
	// increments the version. It returns ErrRecordVersionConflict when the
	// version changed or the record is gone, and ErrRecordTitleTaken.
	UpdateRecord(ctx context.Context, record domain.Record) (domain.Record, error)
	// DeleteRecord removes one of orgID's records, or returns
	// ErrRecordNotFound.
	DeleteRecord(ctx context.Context, orgID, id string) error
	// InTx runs fn in one transaction: it commits when fn returns nil and
	// rolls back otherwise.
	InTx(ctx context.Context, fn func(tx Store) error) error
}
