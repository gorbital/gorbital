// Package reviews is the reviews module: a diner rates an order that was
// delivered to them, in four layers (domain, usecase, repository, delivery)
// with one file per operation in each.
//
// It is the third shape of ownership in Plateful, and the one that fits the
// framework least comfortably. The restaurants module's rows belong to an
// organisation and are reached by its members; the orders module's rows are
// reached by the restaurant, the customer and the courier. A review is
// written by somebody who is in no organisation, about a row that belongs to
// one, and read by somebody who isn't signed in at all.
package reviews

import (
	"net/http"

	"gorbital.dev/gorbital"
	"gorbital.dev/httpx"

	"example.com/plateful/internal/modules/reviews/delivery"
	"example.com/plateful/internal/modules/reviews/domain"
	"example.com/plateful/internal/modules/reviews/repository"
	"example.com/plateful/internal/modules/reviews/usecase"
)

// Module returns the reviews module. main.go adds it with every other module
// through modules.All. Error codes and permission names are public API: add
// new ones, never change existing ones.
func Module() gorbital.Module {
	return gorbital.Module{
		Name: "reviews",
		// docs:start review-errors
		// Two pairs of these are the same status for different reasons, and
		// two more are deliberately the same answer as each other.
		// order_not_found is what somebody else's order returns as well as
		// one that doesn't exist, and review_not_found likewise, so neither
		// kind of ID can be probed. order_not_delivered and review_exists
		// are both 409 because both say the request was right about who it
		// is and wrong about when: come back after it arrives, or you have
		// already had your say.
		Errors: []httpx.Mapping{
			{Err: domain.ErrUnauthenticated, Status: http.StatusUnauthorized, Code: "unauthenticated", Detail: "authentication is required"},
			{Err: domain.ErrInvalidReview, Status: http.StatusUnprocessableEntity, Code: "validation_failed", Detail: "the review is not valid"},
			{Err: domain.ErrReviewNotFound, Status: http.StatusNotFound, Code: "review_not_found", Detail: "you have no review with this ID"},
			{Err: domain.ErrReviewExists, Status: http.StatusConflict, Code: "review_exists", Detail: "you have already reviewed this order"},
			{Err: domain.ErrOrderNotFound, Status: http.StatusNotFound, Code: "order_not_found", Detail: "you have no order with this ID"},
			{Err: domain.ErrOrderNotDelivered, Status: http.StatusConflict, Code: "order_not_delivered", Detail: "the order can be reviewed once it has been delivered"},
			{Err: domain.ErrReviewWindowClosed, Status: http.StatusConflict, Code: "review_window_closed", Detail: "a review can only be changed within 24 hours of writing it"},
			{Err: domain.ErrReviewVersionConflict, Status: http.StatusConflict, Code: "review_version_conflict", Detail: "the review changed since you read it; get it again and retry"},
		},
		// docs:end review-errors
		// docs:start review-permissions
		// Platform permissions, both of them, held through a platform role
		// rather than through membership of an organisation. Neither has
		// OrgRoles, and that is the module in one line: nobody acting inside
		// the restaurant's organisation may write, change or hide a review
		// of it. "user" is every signed-in account, so PermWrite only gets a
		// customer as far as the handler — usecase.WriteReview decides
		// whether the order is theirs.
		Permissions: []gorbital.Permission{
			{Name: usecase.PermWrite, Description: "Review an order delivered to you", Roles: []string{"user"}},
			{Name: usecase.PermModerate, Description: "Hide an abusive review", Roles: []string{"platform_admin"}},
		},
		// docs:end review-permissions
		Routes: func(r *gorbital.Router, d gorbital.Deps) {
			// d is zero while the OpenAPI document is exported: the service
			// is built, but no use case runs.
			svc := usecase.NewService(repository.NewStore(d.DB), d.Audit, d.Logger)
			delivery.Register(r, svc)
		},
	}
}
