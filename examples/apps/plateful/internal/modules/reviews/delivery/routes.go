// Package delivery is the reviews module's HTTP adapter: the route table in
// this file, and one file per operation with its input, output and handler.
package delivery

import (
	"net/http"
	"time"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/guard"

	"example.com/plateful/internal/modules/reviews/usecase"
)

// handlers holds what every operation's handler uses.
type handlers struct {
	svc *usecase.Service
}

// docs:start review-routes

// Register adds the reviews routes to r. Four routes, and no two of them
// answer to the same kind of caller.
//
//   - POST /v1/orders/{orderId}/review is the diner who placed the order.
//     guard.Permission(PermWrite) is a platform permission the "user" role
//     holds, which is every signed-in account: the customer belongs to no
//     organisation, so guard.OrgMember could never let them in, and the
//     guard by itself authorises nothing. The check that matters — this
//     order is yours — is a fact about the row, so it is in the use case.
//   - PATCH /v1/reviews/{id} is the same diner, within a day of writing it.
//   - GET /v1/restaurants/{restaurantId}/reviews is guard.Public(), and it
//     is the only route in the whole of Plateful that is. Everything else
//     here needs a signed-in caller; this one does not, because the person
//     reading a restaurant's reviews is choosing where to eat and has not
//     signed in yet. Asking them to would mean nobody read the reviews. It
//     is the app's one unauthenticated endpoint, so it is also the only one
//     an anonymous stranger can spend the platform's database on, which is
//     why it carries the module's one rate limit, keyed by IP because there
//     is no user to key it by.
//   - POST /v1/platform/reviews/{id}/hide is platform staff. The path
//     carries no organisation because the operation isn't a tenant's — and
//     there is deliberately no organisation-scoped route beside it: a
//     restaurant may not hide its own bad reviews, at all, by any route.
//     See usecase.HideReview for why that absence is the product decision.
func Register(r *gorbital.Router, svc *usecase.Service) {
	h := handlers{svc: svc}

	orders := r.Group("/v1/orders/{orderId}/review", gorbital.Tags("Reviews"))
	gorbital.Post(orders, "", h.writeReview, gorbital.OperationID("reviews-write"),
		gorbital.Summary("Review a delivered order"),
		gorbital.Description("Rate the order 1 to 5 and say why. One review per order, and only once the order has been delivered to you."),
		gorbital.Status(http.StatusCreated),
		gorbital.Errors(http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity),
		guard.Permission(usecase.PermWrite))

	mine := r.Group("/v1/reviews", gorbital.Tags("Reviews"))
	gorbital.Patch(mine, "/{id}", h.updateReview, gorbital.OperationID("reviews-update"),
		gorbital.Summary("Change your review"),
		gorbital.Description("Within 24 hours of writing it; after that the review is history. Send the `version` you read."),
		gorbital.Errors(http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity),
		guard.Permission(usecase.PermWrite))

	public := r.Group("/v1/restaurants/{restaurantId}/reviews", gorbital.Tags("Reviews"))
	gorbital.Get(public, "", h.listReviews, gorbital.OperationID("reviews-list"),
		gorbital.Summary("Read a restaurant's reviews"),
		gorbital.Description("Newest first, with the restaurant's review count and average rating. Paginate with `cursor`. No sign-in: a diner choosing where to eat has no account yet."),
		gorbital.Errors(http.StatusBadRequest),
		guard.Public(),
		// Keyed by IP, since an anonymous caller has no user to key by. The
		// limiter is named so every instance of this route shares one
		// budget, but it is not declared in Module.RateLimiters: gorbital
		// refuses a module-declared limiter whose name a guard.RateLimit
		// also uses, because their budgets would mix. Declared limiters are
		// for budgets a module spends itself, not for a guard's.
		guard.RateLimit(120, time.Minute, guard.ByIP(), guard.Named("reviews_public_list")))

	platform := r.Group("/v1/platform/reviews", gorbital.Tags("Platform"))
	gorbital.Post(platform, "/{id}/hide", h.hideReview, gorbital.OperationID("reviews-hide"),
		gorbital.Summary("Hide an abusive review"),
		gorbital.Description("Takes the review out of the public list and out of the restaurant's rating, and records why. Platform staff only: a restaurant cannot hide its own reviews."),
		gorbital.Errors(http.StatusNotFound, http.StatusUnprocessableEntity),
		guard.Permission(usecase.PermModerate))
}

// docs:end review-routes
