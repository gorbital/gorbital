package delivery

import "context"

func (h handlers) deleteShelf(ctx context.Context, in *shelfIDInput) (*struct{}, error) {
	return nil, h.svc.DeleteShelf(ctx, in.ID)
}
