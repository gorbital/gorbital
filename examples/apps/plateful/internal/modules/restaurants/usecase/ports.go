package usecase

import (
	"context"

	"gorbital.dev/page"

	"example.com/plateful/internal/modules/restaurants/domain"
)

// BrowseQuery selects one page of the platform's restaurants.
type BrowseQuery struct {
	// Statuses keeps restaurants with one of these statuses. Customers
	// browse open ones; platform staff pass none and see them all.
	Statuses []domain.Status
	// Cuisine keeps restaurants of this cuisine, ignoring case; empty keeps
	// all.
	Cuisine string
	// Sort is one of the sortable fields: name or created_at.
	Sort page.SortField
	// After, when set, starts the page after this position.
	After *Position
	Limit int
}

// Position is where a page ended: the last restaurant's sort value and ID.
type Position struct {
	Time  string // RFC 3339 when sorting by created_at
	Text  string // when sorting by a text field
	ID    string
	IsSet bool
}

// docs:start restaurant-store-port

// Store reads and writes restaurants; repository.Store implements it with
// SQL.
type Store interface {
	// InsertRestaurant stores a new restaurant, or returns
	// ErrRestaurantNameTaken.
	InsertRestaurant(ctx context.Context, r domain.Restaurant) (domain.Restaurant, error)
	// SelectRestaurantByOrg returns the organisation's restaurant, or
	// ErrRestaurantNotFound. lock locks the row until the transaction ends.
	SelectRestaurantByOrg(ctx context.Context, orgID string, lock bool) (domain.Restaurant, error)
	// SelectRestaurant returns one restaurant by ID, whichever organisation
	// it belongs to, or ErrRestaurantNotFound. Customers and platform staff
	// reach restaurants this way; a restaurant's own staff go by
	// organisation.
	SelectRestaurant(ctx context.Context, id string, lock bool) (domain.Restaurant, error)
	// SelectRestaurants returns up to q.Limit restaurants in q.Sort order,
	// with the ID breaking ties.
	SelectRestaurants(ctx context.Context, q BrowseQuery) ([]domain.Restaurant, error)
	// UpdateRestaurant saves r when the stored version is still r.Version
	// and increments the version. It returns ErrRestaurantVersionConflict
	// when the version changed or the restaurant is gone, and
	// ErrRestaurantNameTaken.
	UpdateRestaurant(ctx context.Context, r domain.Restaurant) (domain.Restaurant, error)
	// InTx runs fn in one transaction: it commits when fn returns nil and
	// rolls back otherwise.
	InTx(ctx context.Context, fn func(tx Store) error) error
}

// docs:end restaurant-store-port
