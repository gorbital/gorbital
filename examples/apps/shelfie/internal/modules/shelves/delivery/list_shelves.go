package delivery

import (
	"context"

	"gorbital.dev/page"

	"example.com/shelfie/internal/modules/shelves/domain"
	"example.com/shelfie/internal/modules/shelves/usecase"
)

type listShelvesInput struct {
	page.Params
	Visibility string `query:"visibility" enum:"private,shared" doc:"Only shelves with this visibility"`
}

// ShelfPage is a page of shelves.
type ShelfPage struct {
	Items      []ShelfResponse `json:"items"`
	NextCursor string          `json:"next_cursor,omitempty" doc:"Pass as cursor to get the next page; absent on the last page"`
}

type shelfPageOutput struct {
	Body ShelfPage
}

func (h handlers) listShelves(ctx context.Context, in *listShelvesInput) (*shelfPageOutput, error) {
	res, err := h.svc.ListShelves(ctx, usecase.ListShelvesInput{
		Page:       in.Params,
		Visibility: domain.Visibility(in.Visibility),
	})
	if err != nil {
		return nil, fieldErrors(err, "query")
	}
	out := &shelfPageOutput{Body: ShelfPage{Items: make([]ShelfResponse, len(res.Items)), NextCursor: res.NextCursor}}
	for i, shelf := range res.Items {
		out.Body.Items[i] = toResponse(shelf)
	}
	return out, nil
}
