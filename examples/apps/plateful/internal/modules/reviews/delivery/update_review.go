package delivery

import (
	"context"

	"example.com/plateful/internal/modules/reviews/usecase"
)

type updateReviewInput struct {
	ID   string `path:"id" maxLength:"64" example:"rev_mfrggzdfmztwq2lkmfrggzdfmy"`
	Body struct {
		Version int64  `json:"version" minimum:"1" doc:"The version you read"`
		Rating  int    `json:"rating" minimum:"1" maximum:"5" doc:"1 to 5 stars"`
		Comment string `json:"comment,omitempty" maxLength:"2000"`
	}
}

func (h handlers) updateReview(ctx context.Context, in *updateReviewInput) (*reviewOutput, error) {
	r, err := h.svc.UpdateReview(ctx, in.ID, usecase.UpdateReviewInput{
		Version: in.Body.Version,
		Rating:  in.Body.Rating,
		Comment: in.Body.Comment,
	})
	if err != nil {
		return nil, fieldErrors(err, "body")
	}
	return &reviewOutput{Body: toResponse(r)}, nil
}
