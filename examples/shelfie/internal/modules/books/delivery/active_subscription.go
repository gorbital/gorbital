package delivery

import (
	"context"
	"errors"
	"net/http"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/guard"

	"example.com/shelfie/internal/modules/books/usecase"
)

// docs:start active-subscription

// ErrActiveSubscriptionRefused is the error ActiveSubscription refuses a
// request with. module.go maps it, so the refusal has a status and a stable
// code:
//
//	{Err: delivery.ErrActiveSubscriptionRefused, Status: http.StatusForbidden, Code: "active_subscription_refused", Detail: "…"},
var ErrActiveSubscriptionRefused = errors.New("books: active subscription refused the request")

// ActiveSubscription is a guard for the books routes: routes.go adds it to a
// route next to guard.Permission. It runs after the sign-in check and before
// the request body is read, and x-gorbital-guards lists it as
// "active_subscription".
func ActiveSubscription(subs *usecase.Subscriptions) gorbital.RouteOption {
	return guard.New(guard.Spec{
		Name:     "active_subscription",
		Statuses: []int{http.StatusForbidden},
		Check:    checkActiveSubscription(subs),
	})
}

// checkActiveSubscription is the guard's rule. It returns nil to let a
// request through, or the error above to refuse it. req has the path
// parameters, query and headers, not the body; ctx has the caller, which
// Subscriptions.Active reads with actor.From. Any other error is a 500,
// logged once.
func checkActiveSubscription(subs *usecase.Subscriptions) func(context.Context, guard.Request) error {
	return func(ctx context.Context, _ guard.Request) error {
		active, err := subs.Active(ctx)
		switch {
		case err != nil:
			return err
		case !active:
			return ErrActiveSubscriptionRefused
		default:
			return nil
		}
	}
}

// docs:end active-subscription
