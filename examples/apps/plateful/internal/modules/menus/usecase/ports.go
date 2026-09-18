package usecase

import (
	"context"
	"time"

	"gorbital.dev/page"

	"example.com/plateful/internal/modules/menus/domain"
)

// ListQuery selects one page of an organisation's menu items, as its staff
// see them.
type ListQuery struct {
	OrgID string
	// Section keeps items printed under this heading, ignoring case; empty
	// keeps all.
	Section string
	// Available, when set, keeps items that are or aren't available. Staff
	// see both unless they ask.
	Available *bool
	// Sort is one of the sortable fields: name, price_minor or created_at.
	Sort page.SortField
	// After, when set, starts the page after this position.
	After *Position
	Limit int
}

// Position is where a page ended: the last item's sort value and ID. Only
// the field the sort uses is read.
type Position struct {
	Time   time.Time // when sorting by created_at
	Text   string    // when sorting by a text field
	Number int64     // when sorting by price_minor
	ID     string
}

// Store reads and writes menu items; repository.Store implements it with
// SQL.
type Store interface {
	// InsertItem stores a new item, or returns ErrItemNameTaken.
	InsertItem(ctx context.Context, item domain.Item) (domain.Item, error)
	// SelectItem returns one of the organisation's items, or
	// ErrItemNotFound. lock locks the row until the transaction ends.
	SelectItem(ctx context.Context, orgID, id string, lock bool) (domain.Item, error)
	// SelectItems returns up to q.Limit of the organisation's items in
	// q.Sort order, with the ID breaking ties.
	SelectItems(ctx context.Context, q ListQuery) ([]domain.Item, error)
	// UpdateItem saves item when the stored version is still item.Version
	// and increments the version. It returns ErrItemVersionConflict when the
	// version changed or the item is gone, and ErrItemNameTaken.
	UpdateItem(ctx context.Context, item domain.Item) (domain.Item, error)
	// DeleteItem removes one of the organisation's items, or returns
	// ErrItemNotFound.
	DeleteItem(ctx context.Context, orgID, id string) error
	// SelectMenu returns the organisation's available items in the order a
	// menu reads: section, then position, then ID.
	SelectMenu(ctx context.Context, orgID string) ([]domain.Item, error)
	// SelectOpenRestaurantOrg returns the organisation of the restaurant
	// restaurantID when it is open, or ErrRestaurantNotFound.
	SelectOpenRestaurantOrg(ctx context.Context, restaurantID string) (string, error)
	// InTx runs fn in one transaction: it commits when fn returns nil and
	// rolls back otherwise.
	InTx(ctx context.Context, fn func(tx Store) error) error
}
