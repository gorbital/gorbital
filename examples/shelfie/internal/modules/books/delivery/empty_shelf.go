package delivery

import "context"

type emptyShelfOutput struct {
	Body struct {
		// Removed is how many books were on the shelf.
		Removed int `json:"removed" example:"42"`
	}
}

func (h handlers) emptyShelf(ctx context.Context, _ *struct{}) (*emptyShelfOutput, error) {
	removed, err := h.svc.EmptyShelf(ctx)
	if err != nil {
		return nil, err
	}
	out := &emptyShelfOutput{}
	out.Body.Removed = removed
	return out, nil
}
