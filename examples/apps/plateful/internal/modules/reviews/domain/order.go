package domain

// OrderStatusDelivered is the only status of an order that can be reviewed.
// The orders module owns the status values; this module compares against one
// of them and nothing more.
const OrderStatusDelivered = "delivered"

// docs:start order-facts

// OrderFacts is everything the reviews module needs to know about an order:
// whose it is, whose restaurant it came from, and whether it arrived.
//
// It is deliberately not the orders module's own order type. A module never
// imports another module's layers, so the repository reads these four
// columns out of the orders table with SQL and fills this in. The reviews
// module therefore depends on four column names rather than on another
// module's Go API, and the coupling is visible in one query.
type OrderFacts struct {
	OrgID        string
	RestaurantID string
	CustomerID   string
	Status       string
}

// Reviewable reports whether customerID may review this order: it is theirs
// and it has been delivered. The two refusals are separate errors, and the
// caller must check ownership first — see usecase.WriteReview.
func (o OrderFacts) Reviewable(customerID string) error {
	if o.CustomerID == "" || o.CustomerID != customerID {
		return ErrOrderNotFound
	}
	if o.Status != OrderStatusDelivered {
		return ErrOrderNotDelivered
	}
	return nil
}

// docs:end order-facts
