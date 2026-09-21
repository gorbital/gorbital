package delivery

import (
	"context"

	"gorbital.dev/page"

	"example.com/shelfie/internal/modules/clubbooks/domain"
	"example.com/shelfie/internal/modules/clubbooks/usecase"
)

type listClubBooksInput struct {
	OrgID string `path:"orgId" maxLength:"64" example:"org_mfrggzdfmztwq2lkmfrggzdfmy"`
	page.Params
	Status string `query:"status" enum:"proposed,reading,finished" doc:"Only club books with this status"`
}

// ClubBookPage is a page of club books.
type ClubBookPage struct {
	Items      []ClubBookResponse `json:"items"`
	NextCursor string             `json:"next_cursor,omitempty" doc:"Pass as cursor to get the next page; absent on the last page"`
}

type clubBookPageOutput struct {
	Body ClubBookPage
}

func (h handlers) listClubBooks(ctx context.Context, in *listClubBooksInput) (*clubBookPageOutput, error) {
	res, err := h.svc.ListClubBooks(ctx, in.OrgID, usecase.ListClubBooksInput{
		Page:   in.Params,
		Status: domain.Status(in.Status),
	})
	if err != nil {
		return nil, fieldErrors(err, "query")
	}
	out := &clubBookPageOutput{Body: ClubBookPage{Items: make([]ClubBookResponse, len(res.Items)), NextCursor: res.NextCursor}}
	for i, clubBook := range res.Items {
		out.Body.Items[i] = toResponse(clubBook)
	}
	return out, nil
}
