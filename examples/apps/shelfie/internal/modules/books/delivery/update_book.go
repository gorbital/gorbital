package delivery

import (
	"context"

	"example.com/shelfie/internal/modules/books/domain"
)

type updateBookInput struct {
	ID   string `path:"id" maxLength:"64" example:"bok_mfrggzdfmztwq2lkmfrggzdfmy"`
	Body struct {
		Title  *string `json:"title,omitempty" minLength:"1" maxLength:"300"`
		Author *string `json:"author,omitempty" maxLength:"200"`
		ISBN   *string `json:"isbn,omitempty" maxLength:"17" doc:"Empty removes the ISBN"`
		Status *string `json:"status,omitempty" enum:"want_to_read,reading,read"`
	}
}

func (h handlers) updateBook(ctx context.Context, in *updateBookInput) (*bookOutput, error) {
	c := domain.Changes{Title: in.Body.Title, Author: in.Body.Author, ISBN: in.Body.ISBN}
	if in.Body.Status != nil {
		status := domain.Status(*in.Body.Status)
		c.Status = &status
	}
	book, err := h.svc.UpdateBook(ctx, in.ID, c)
	if err != nil {
		return nil, err
	}
	return &bookOutput{Body: toResponse(book)}, nil
}
