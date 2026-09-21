package delivery

import (
	"context"

	"example.com/shelfie/internal/modules/shelves/domain"
)

type createShelfInput struct {
	Body struct {
		Name        string `json:"name" minLength:"1" maxLength:"100" doc:"Unique among your shelves, ignoring case"`
		Description string `json:"description,omitempty" maxLength:"2000"`
		Visibility  string `json:"visibility,omitempty" enum:"private,shared" default:"private"`
	}
}

func (h handlers) createShelf(ctx context.Context, in *createShelfInput) (*shelfOutput, error) {
	shelf, err := h.svc.CreateShelf(ctx, domain.ShelfFields{
		Name:        in.Body.Name,
		Description: in.Body.Description,
		Visibility:  domain.Visibility(in.Body.Visibility),
	})
	if err != nil {
		return nil, fieldErrors(err, "body")
	}
	return &shelfOutput{Body: toResponse(shelf)}, nil
}
