package usecase

import (
	"context"

	"example.com/mobile-backend/internal/modules/trips/domain"
)

// docs:start store

// Store reads and writes trips; repository.Store implements it with SQL.
// Every method takes the owner whose trips it may touch: there is no
// operation on a trip that isn't scoped to one traveller.
type Store interface {
	// InsertTrip stores a new trip.
	InsertTrip(ctx context.Context, t domain.Trip) (domain.Trip, error)
	// SelectTrips returns up to limit of owner's trips, newest first.
	SelectTrips(ctx context.Context, owner string, limit int) ([]domain.Trip, error)
	// SelectTrip returns owner's trip id, or domain.ErrTripNotFound.
	SelectTrip(ctx context.Context, owner, id string) (domain.Trip, error)
	// DeleteTrip removes owner's trip id, or returns
	// domain.ErrTripNotFound.
	DeleteTrip(ctx context.Context, owner, id string) error
}

// docs:end store
