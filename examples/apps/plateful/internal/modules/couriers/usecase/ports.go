package usecase

import (
	"context"

	"example.com/plateful/internal/modules/couriers/domain"
)

// AvailableQuery selects the couriers a restaurant may dispatch right now.
// It carries no organisation: there is none on a courier to filter by, and
// that is the whole point of this query.
type AvailableQuery struct {
	// Limit is how many couriers to return at most.
	Limit int
}

// Store reads and writes couriers; repository.Store implements it with SQL.
type Store interface {
	// InsertCourier stores a new courier profile, or returns
	// ErrCourierAlreadyRegistered when the account already has one.
	InsertCourier(ctx context.Context, c domain.Courier) (domain.Courier, error)
	// SelectCourierByUser returns the account's own courier profile, or
	// ErrCourierNotFound. lock locks the row until the transaction ends.
	SelectCourierByUser(ctx context.Context, userID string, lock bool) (domain.Courier, error)
	// SelectAvailableCouriers returns up to q.Limit couriers who are
	// available and carrying nothing, oldest ID first.
	SelectAvailableCouriers(ctx context.Context, q AvailableQuery) ([]domain.Courier, error)
	// UpdateCourier saves c when the stored version is still c.Version and
	// increments the version. It returns ErrCourierVersionConflict when the
	// version changed or the courier is gone.
	UpdateCourier(ctx context.Context, c domain.Courier) (domain.Courier, error)
	// InTx runs fn in one transaction: it commits when fn returns nil and
	// rolls back otherwise.
	InTx(ctx context.Context, fn func(tx Store) error) error
}
