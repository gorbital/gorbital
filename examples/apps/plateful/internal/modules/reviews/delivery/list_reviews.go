package delivery

import (
	"context"

	"gorbital.dev/page"

	"example.com/plateful/internal/modules/reviews/usecase"
)

type listReviewsInput struct {
	page.Params
	RestaurantID string `path:"restaurantId" maxLength:"64" example:"rst_mfrggzdfmztwq2lkmfrggzdfmy"`
}

// listReviews answers the app's one unauthenticated route. It never reads an
// actor, because on this route there may not be one.
func (h handlers) listReviews(ctx context.Context, in *listReviewsInput) (*reviewPageOutput, error) {
	res, err := h.svc.ListReviews(ctx, usecase.ListInput{RestaurantID: in.RestaurantID, Page: in.Params})
	if err != nil {
		return nil, fieldErrors(err, "query")
	}
	out := &reviewPageOutput{Body: ReviewPage{
		Items:         make([]ReviewResponse, len(res.Page.Items)),
		NextCursor:    res.Page.NextCursor,
		ReviewCount:   res.Rating.Count,
		AverageRating: averageForWire(res.Rating),
	}}
	for i, r := range res.Page.Items {
		out.Body.Items[i] = toResponse(r)
	}
	return out, nil
}
