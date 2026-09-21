package delivery

import (
	"context"

	"example.com/shelfie/internal/modules/books/domain"
)

type listBooksInput struct {
	Status string `query:"status" enum:"want_to_read,reading,read" doc:"Only books with this status"`
	Limit  int    `query:"limit" minimum:"1" maximum:"100" default:"20" doc:"How many books to return, newest first"`
}

type bookListOutput struct {
	Body struct {
		Items []BookResponse `json:"items"`
	}
}

func (h handlers) listBooks(ctx context.Context, in *listBooksInput) (*bookListOutput, error) {
	books, err := h.svc.ListBooks(ctx, domain.Status(in.Status), in.Limit)
	if err != nil {
		return nil, err
	}
	out := &bookListOutput{}
	out.Body.Items = make([]BookResponse, 0, len(books))
	for _, b := range books {
		out.Body.Items = append(out.Body.Items, toResponse(b))
	}
	return out, nil
}
