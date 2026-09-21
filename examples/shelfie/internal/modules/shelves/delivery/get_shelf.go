package delivery

import "context"

func (h handlers) getShelf(ctx context.Context, in *shelfIDInput) (*shelfOutput, error) {
	shelf, err := h.svc.GetShelf(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	return &shelfOutput{Body: toResponse(shelf)}, nil
}
