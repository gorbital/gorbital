package delivery

import "context"

type hideReviewInput struct {
	ID   string `path:"id" maxLength:"64" example:"rev_mfrggzdfmztwq2lkmfrggzdfmy"`
	Body struct {
		Reason string `json:"reason" minLength:"1" maxLength:"500" doc:"Why the review is hidden; the audit log keeps it"`
	}
}

// hideReview is reached only by platform staff. The restaurant the review is
// about has no route to this handler, on purpose.
func (h handlers) hideReview(ctx context.Context, in *hideReviewInput) (*reviewOutput, error) {
	r, err := h.svc.HideReview(ctx, in.ID, in.Body.Reason)
	if err != nil {
		return nil, fieldErrors(err, "body")
	}
	return &reviewOutput{Body: toResponse(r)}, nil
}
