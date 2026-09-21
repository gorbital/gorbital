package delivery

import "context"

func (h handlers) getBook(ctx context.Context, in *bookIDInput) (*bookOutput, error) {
	book, err := h.svc.GetBook(ctx, in.ID)
	if err != nil {
		return nil, err
	}
	return &bookOutput{Body: toResponse(book)}, nil
}
