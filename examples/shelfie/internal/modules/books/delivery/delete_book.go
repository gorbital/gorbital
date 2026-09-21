package delivery

import "context"

func (h handlers) deleteBook(ctx context.Context, in *bookIDInput) (*struct{}, error) {
	return nil, h.svc.DeleteBook(ctx, in.ID)
}
