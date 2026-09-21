package delivery

import (
	"context"

	"example.com/shelfie/internal/modules/clubbooks/domain"
)

type createClubBookInput struct {
	OrgID string `path:"orgId" maxLength:"64" example:"org_mfrggzdfmztwq2lkmfrggzdfmy"`
	Body  struct {
		Title  string `json:"title" minLength:"1" maxLength:"100" doc:"Unique in the organisation, ignoring case"`
		Author string `json:"author,omitempty" maxLength:"100"`
		Status string `json:"status,omitempty" enum:"proposed,reading,finished" default:"proposed"`
		Note   string `json:"note,omitempty" maxLength:"2000"`
	}
}

func (h handlers) createClubBook(ctx context.Context, in *createClubBookInput) (*clubBookOutput, error) {
	clubBook, err := h.svc.CreateClubBook(ctx, in.OrgID, domain.ClubBookFields{
		Title:  in.Body.Title,
		Author: in.Body.Author,
		Status: domain.Status(in.Body.Status),
		Note:   in.Body.Note,
	})
	if err != nil {
		return nil, fieldErrors(err, "body")
	}
	return &clubBookOutput{Body: toResponse(clubBook)}, nil
}
