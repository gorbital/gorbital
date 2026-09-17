package delivery

import (
	"context"

	"example.com/shelfie/internal/modules/shelves/domain"
	"example.com/shelfie/internal/modules/shelves/usecase"
)

type updateShelfInput struct {
	ID   string `path:"id" maxLength:"64" example:"shl_mfrggzdfmztwq2lkmfrggzdfmy"`
	Body struct {
		Version     int64   `json:"version" minimum:"1" doc:"The version you read. If the shelf changed since, the update fails with shelf_version_conflict."`
		Name        *string `json:"name,omitempty" minLength:"1" maxLength:"100"`
		Description *string `json:"description,omitempty" maxLength:"2000"`
		Visibility  *string `json:"visibility,omitempty" enum:"private,shared"`
	}
}

func (h handlers) updateShelf(ctx context.Context, in *updateShelfInput) (*shelfOutput, error) {
	changes := domain.Changes{
		Name:        in.Body.Name,
		Description: in.Body.Description,
	}
	if in.Body.Visibility != nil {
		value := domain.Visibility(*in.Body.Visibility)
		changes.Visibility = &value
	}
	shelf, err := h.svc.UpdateShelf(ctx, in.ID, usecase.UpdateShelfInput{Version: in.Body.Version, Changes: changes})
	if err != nil {
		return nil, fieldErrors(err, "body")
	}
	return &shelfOutput{Body: toResponse(shelf)}, nil
}
