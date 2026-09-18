package usecase

import (
	"context"
	"time"

	"gorbital.dev/page"

	"example.com/plateful/internal/modules/orders/domain"
)

// ListQuery selects one page of orders.
type ListQuery struct {
	// OrgID limits the page to one restaurant's orders; CustomerID and
	// CourierID to one person's. Exactly one of them is set by the use case
	// that builds the query, and it is always set: it is the filter that
	// decides which rows exist for this caller.
	OrgID      string
	CustomerID string
	CourierID  string
	// Status keeps orders with this status; empty keeps all.
	Status domain.Status
	// PlacedFrom and PlacedBefore bound the date range; zero means open.
	PlacedFrom   time.Time
	PlacedBefore time.Time
	// Sort is one of the sortable fields: placed_at or total_minor.
	Sort page.SortField
	// After, when set, starts the page after this position.
	After *Position
	Limit int
}

// Position is where a page ended: the last order's sort value and ID.
type Position struct {
	Time  time.Time // when sorting by placed_at
	Total int64     // when sorting by total_minor
	ID    string
}

// MenuItem is what the menus module's table says about a dish when an order
// is priced. The orders module reads it with SQL rather than importing that
// module's layers.
type MenuItem struct {
	ID        string
	Name      string
	PriceMin  int64
	Currency  string
	Available bool
	// Stock is how many are left, or nil when the dish is unlimited.
	Stock *int
}

// DailySummary is one restaurant's day, counted by the database.
type DailySummary struct {
	From, To time.Time
	// ByStatus counts the day's orders under each status.
	ByStatus map[domain.Status]int
	Orders   int
	// RevenueMinor is what the delivered orders came to, in minor units.
	RevenueMinor int64
	Currency     string
	// AveragePreparation is how long, on average, the kitchen took between
	// accepting an order and having it ready. Zero when none was.
	AveragePreparation time.Duration
	// BusiestHour is the UTC hour (0 to 23) that took the most orders, and
	// how many; BusiestOrders is zero when the day had none.
	BusiestHour   int
	BusiestOrders int
}

// AvailableCourier is a courier the couriers module says is free, read with
// SQL from its platform-scoped table.
type AvailableCourier struct {
	ID          string
	DisplayName string
}

// Store reads orders and the few rows of other modules the rules need.
type Store interface {
	// SelectOrder returns one order by ID, whichever organisation it belongs
	// to, or ErrOrderNotFound. Customers and couriers reach an order this
	// way, because they have no organisation to scope it by; the use case
	// then checks the order is theirs. lock locks the row until the
	// transaction ends.
	SelectOrder(ctx context.Context, id string, lock bool) (domain.Order, error)
	// SelectOrgOrder returns one of the organisation's orders, or
	// ErrOrderNotFound. The organisation is part of the query, not checked
	// afterwards.
	SelectOrgOrder(ctx context.Context, orgID, id string, lock bool) (domain.Order, error)
	// SelectOrders returns up to q.Limit orders in q.Sort order, with the ID
	// breaking ties.
	SelectOrders(ctx context.Context, q ListQuery) ([]domain.Order, error)
	// SelectLines returns the lines of the given orders, keyed by order ID.
	SelectLines(ctx context.Context, orderIDs []string) (map[string][]domain.Line, error)
	// UpdateOrder saves the order's status, courier and timestamps, and
	// increments the version.
	UpdateOrder(ctx context.Context, o domain.Order) (domain.Order, error)
	// CountOpenOrders counts one restaurant's orders that haven't finished.
	CountOpenOrders(ctx context.Context, orgID string) (int, error)

	// SelectRestaurant returns the organisation and status of a restaurant,
	// from the restaurants module's table. It is read with SQL because a
	// module never imports another module's layers.
	SelectRestaurant(ctx context.Context, restaurantID string) (orgID string, accepting bool, err error)
	// SelectMenuItems returns the menu items of orgID with these IDs, from
	// the menus module's table. lock locks the rows so two orders can't take
	// the last portion of the same dish.
	SelectMenuItems(ctx context.Context, orgID string, ids []string, lock bool) (map[string]MenuItem, error)
	// PaymentStatus returns the status of an order's payment from the
	// payments module's table, or "" when there is none.
	PaymentStatus(ctx context.Context, orgID, orderID string) (string, error)
	// SelectAvailableCouriers returns couriers who are available and not
	// carrying anything, from the couriers module's platform-scoped table.
	SelectAvailableCouriers(ctx context.Context, limit int) ([]AvailableCourier, error)
	// CourierOfUser returns the courier profile of a user account, or
	// ErrNotACourier.
	CourierOfUser(ctx context.Context, userID string) (string, error)
	// SelectCustomerAddress returns where a customer usually wants an order
	// taken, or "" when they never said and when they have no profile at
	// all.
	SelectCustomerAddress(ctx context.Context, userID string) (string, error)

	// SelectDailySummary counts one restaurant's orders between two times in
	// one query.
	SelectDailySummary(ctx context.Context, orgID string, from, to time.Time) (DailySummary, error)
	// SelectLateOrders returns orders every restaurant accepted before the
	// given time and hasn't finished. It is the only method that crosses
	// organisations: the orders_late_sweep job runs for the whole platform.
	SelectLateOrders(ctx context.Context, acceptedBefore time.Time, limit int) ([]domain.Order, error)
}

// docs:start orders-transaction-port

// A TxManager runs several writes as one. repository.NewTxManager returns
// one on the pool and the job client.
type TxManager interface {
	// InTx runs fn in one transaction: it commits when fn returns nil and
	// rolls back otherwise.
	InTx(ctx context.Context, fn func(tx Tx) error) error
}

// A Tx is one transaction. Placing an order writes the order, its lines and
// the stock the dishes take, and enqueues the job that tells the restaurant
// — all through this, so all of it commits or none of it does. The job is a
// row written by jobs.Client.InsertTx in the same transaction, which is what
// stops a worker announcing an order that was rolled back.
type Tx interface {
	Store
	// InsertOrder stores the order and its lines.
	InsertOrder(ctx context.Context, o domain.Order) (domain.Order, error)
	// TakeStock reduces a limited dish's stock, or returns
	// ErrItemOutOfStock. A dish with unlimited stock is left alone.
	TakeStock(ctx context.Context, orgID, itemID string, quantity int) error
	// SetCourierOrder writes the courier's current assignment in the
	// couriers module's table: '' frees them.
	SetCourierOrder(ctx context.Context, courierID, orderID string) error
	// Notify enqueues a notification for the restaurant's organisation. The
	// job runs only if the transaction commits.
	Notify(ctx context.Context, orgID, title string, lines []string) error
}

// docs:end orders-transaction-port
