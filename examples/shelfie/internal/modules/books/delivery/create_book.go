package delivery

import (
	"context"

	"example.com/shelfie/internal/modules/books/domain"
)

// docs:start create-book

type createBookInput struct {
	Body struct {
		Title  string `json:"title" minLength:"1" maxLength:"300"`
		Author string `json:"author,omitempty" maxLength:"200"`
		ISBN   string `json:"isbn,omitempty" maxLength:"17" doc:"ISBN-10 or ISBN-13; hyphens are ignored"`
		Status string `json:"status,omitempty" enum:"want_to_read,reading,read" default:"want_to_read"`
	}
}

func (h handlers) createBook(ctx context.Context, in *createBookInput) (*bookOutput, error) {
	book, err := h.svc.CreateBook(ctx, domain.Fields{
		Title: in.Body.Title, Author: in.Body.Author, ISBN: in.Body.ISBN, Status: domain.Status(in.Body.Status),
	})
	if err != nil {
		return nil, err
	}
	return &bookOutput{Body: toResponse(book)}, nil
}

// docs:end create-book
