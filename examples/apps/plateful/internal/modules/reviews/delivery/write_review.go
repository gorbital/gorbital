package delivery

import (
	"context"

	"example.com/plateful/internal/modules/reviews/usecase"
)

// docs:start write-review-handler

type writeReviewInput struct {
	OrderID string `path:"orderId" maxLength:"64" example:"ord_mfrggzdfmztwq2lkmfrggzdfmy"`
	Body    struct {
		Rating  int    `json:"rating" minimum:"1" maximum:"5" doc:"1 to 5 stars"`
		Comment string `json:"comment,omitempty" maxLength:"2000" doc:"What the diner wants other diners to know"`
	}
}

// writeReview is the whole handler: read the request, call the use case,
// shape the answer.
//
// What it conspicuously does not do is decide whether this caller may review
// this order. It has the order ID from the path and the account from the
// request's actor, and it could compare them — but the rule is "the order
// names you as its customer", which is a fact about a row in the database,
// not about the request. It lives in the use case, in the same transaction
// as the write, where a job or a command would reach it too and where it
// can't be lost by adding a second handler.
func (h handlers) writeReview(ctx context.Context, in *writeReviewInput) (*reviewOutput, error) {
	r, err := h.svc.WriteReview(ctx, in.OrderID, usecase.WriteReviewInput{
		Rating:  in.Body.Rating,
		Comment: in.Body.Comment,
	})
	if err != nil {
		return nil, fieldErrors(err, "body")
	}
	return &reviewOutput{Body: toResponse(r)}, nil
}

// docs:end write-review-handler
