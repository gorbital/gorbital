package usecase

import (
	"context"
	"time"

	"gorbital.dev/page"

	"example.com/plateful/internal/modules/restaurants/domain"
)

// ListQuery selects one page of an organisation's restaurants.
type ListQuery struct {
	OrgID string
	// Status keeps restaurants with this status; empty keeps all.
	Status domain.Status
	// Sort is one of the sortable fields: created_at, updated_at, name, address or cuisine.
	Sort page.SortField
	// After, when set, starts the page after this position.
	After *Position
	Limit int
}

// Position is where a page ended: the last restaurant's sort value and ID.
type Position struct {
	Time time.Time // when sorting by created_at or updated_at
	Text string    // when sorting by a text field
	ID   string
}

// Store reads and writes restaurants; repository.Store implements it with SQL.
// Every method is limited to one organisation's restaurants.
type Store interface {
	// InsertRestaurant stores a new restaurant, or returns ErrRestaurantNameTaken.
	InsertRestaurant(ctx context.Context, restaurant domain.Restaurant) (domain.Restaurant, error)
	// SelectRestaurant returns one of orgID's restaurants, or ErrRestaurantNotFound.
	// lock locks the row until the transaction ends.
	SelectRestaurant(ctx context.Context, orgID, id string, lock bool) (domain.Restaurant, error)
	// SelectRestaurants returns up to q.Limit restaurants in q.Sort order, with the
	// ID breaking ties.
	SelectRestaurants(ctx context.Context, q ListQuery) ([]domain.Restaurant, error)
	// UpdateRestaurant saves restaurant when the stored version is still restaurant.Version and
	// increments the version. It returns ErrRestaurantVersionConflict when the
	// version changed or the restaurant is gone, and ErrRestaurantNameTaken.
	UpdateRestaurant(ctx context.Context, restaurant domain.Restaurant) (domain.Restaurant, error)
	// DeleteRestaurant removes one of orgID's restaurants, or returns
	// ErrRestaurantNotFound.
	DeleteRestaurant(ctx context.Context, orgID, id string) error
	// InTx runs fn in one transaction: it commits when fn returns nil and
	// rolls back otherwise.
	InTx(ctx context.Context, fn func(tx Store) error) error
}
