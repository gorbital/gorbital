package usecase

import (
	"context"

	"example.com/plateful/internal/modules/orders/domain"
)

// docs:start restaurant-accepting

// RestaurantAccepting returns nil when the restaurant exists and is taking
// orders, ErrRestaurantNotFound when it doesn't, and
// ErrRestaurantNotAccepting when it is onboarding, paused, suspended or
// outside its opening hours.
//
// The module's own guard calls it before the handler runs
// (delivery/routes.go), which is why it takes a restaurant ID and nothing
// else: a guard sees the path, the headers and the query, and never the
// body. That is the constraint that shaped this API — placing an order is
// POST /v1/restaurants/{restaurantId}/orders rather than POST /v1/orders
// with the restaurant in the body, because only the first can be guarded.
//
// PlaceOrder checks the same thing again inside its transaction, which is
// where the rule is actually enforced; this is the cheap refusal that keeps
// a suspended restaurant's traffic out of the handler, and it is what makes
// a platform suspension stop new orders the moment it commits.
func (s *Service) RestaurantAccepting(ctx context.Context, restaurantID string) error {
	if _, err := callerID(ctx); err != nil {
		return err
	}
	accepting, err := s.store.SelectRestaurant(ctx, restaurantID)
	switch {
	case err != nil:
		return storeError("restaurant", err)
	case !accepting:
		return domain.ErrRestaurantNotAccepting
	}
	return nil
}

// docs:end restaurant-accepting
