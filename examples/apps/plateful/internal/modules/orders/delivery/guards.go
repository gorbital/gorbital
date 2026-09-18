package delivery

import (
	"context"
	"net/http"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/guard"

	"example.com/plateful/internal/modules/orders/usecase"
)

// RateLimitPlace names the limiter on placing an order, for
// /ops/auth/rate-limits where an operator resets a key's budget.
const RateLimitPlace = "orders_place"

// docs:start accepting-guard

// acceptingRestaurant is the module's own guard: a restaurant has to be open
// before it is given an order. It is written with guard.New, so its refusal
// is mapped like any other of the module's errors (module.go) and the
// OpenAPI document lists the statuses it can answer.
//
// What a guard can see is the whole design constraint. guard.Request exposes
// the path, the headers and the query — never the body — so the restaurant
// has to be in the path for this to exist at all, which is why placing an
// order is POST /v1/restaurants/{restaurantId}/orders. A guard also can't
// see the handler's input struct, so the check is a second read of the
// restaurant row: cheap, indexed, and worth it to keep a suspended
// restaurant's traffic out of the handler.
//
// It is a refusal, not the rule. PlaceOrder asks the same question again
// inside its transaction, where the answer can't change under it. A guard
// that is the only place a rule lives is a rule that a job, a command or a
// second route will quietly skip.
func acceptingRestaurant(svc *usecase.Service) gorbital.RouteOption {
	return guard.New(guard.Spec{
		Name:     "restaurant_accepting",
		Statuses: []int{http.StatusNotFound, http.StatusConflict},
		Check: func(ctx context.Context, req guard.Request) error {
			return svc.RestaurantAccepting(ctx, req.PathParam("restaurantId"))
		},
	})
}

// docs:end accepting-guard
