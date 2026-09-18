package usecase

import (
	"context"

	"example.com/plateful/internal/modules/images/domain"
)

// Store reads and writes image rows; repository.Store implements it with
// SQL. Every method takes the organisation as well as the ID, so a lookup
// can only ever reach that organisation's rows: the guard proved membership
// of the organisation in the path, and this proves the row belongs to it.
type Store interface {
	// InsertImage stores a new pending image.
	InsertImage(ctx context.Context, i domain.Image) (domain.Image, error)
	// SelectImage returns the organisation's image, or ErrImageNotFound.
	// lock locks the row until the transaction ends.
	SelectImage(ctx context.Context, orgID, id string, lock bool) (domain.Image, error)
	// UpdateImage saves the measured size, media type and status of an
	// image the caller read in this transaction.
	UpdateImage(ctx context.Context, i domain.Image) (domain.Image, error)
	// DeleteImage removes the organisation's image row, or returns
	// ErrImageNotFound.
	DeleteImage(ctx context.Context, orgID, id string) error
	// InTx runs fn in one transaction: it commits when fn returns nil and
	// rolls back otherwise.
	InTx(ctx context.Context, fn func(tx Store) error) error
}
