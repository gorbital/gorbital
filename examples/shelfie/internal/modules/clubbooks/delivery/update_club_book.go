package delivery

import (
	"context"

	"example.com/shelfie/internal/modules/clubbooks/domain"
	"example.com/shelfie/internal/modules/clubbooks/usecase"
)

type updateClubBookInput struct {
	OrgID string `path:"orgId" maxLength:"64" example:"org_mfrggzdfmztwq2lkmfrggzdfmy"`
	ID    string `path:"id" maxLength:"64" example:"clb_mfrggzdfmztwq2lkmfrggzdfmy"`
	Body  struct {
		Version int64   `json:"version" minimum:"1" doc:"The version you read. If the club book changed since, the update fails with club_book_version_conflict."`
		Title   *string `json:"title,omitempty" minLength:"1" maxLength:"100"`
		Author  *string `json:"author,omitempty" maxLength:"100"`
		Status  *string `json:"status,omitempty" enum:"proposed,reading,finished"`
		Note    *string `json:"note,omitempty" maxLength:"2000"`
	}
}

func (h handlers) updateClubBook(ctx context.Context, in *updateClubBookInput) (*clubBookOutput, error) {
	changes := domain.Changes{
		Title:  in.Body.Title,
		Author: in.Body.Author,
		Note:   in.Body.Note,
	}
	if in.Body.Status != nil {
		value := domain.Status(*in.Body.Status)
		changes.Status = &value
	}
	clubBook, err := h.svc.UpdateClubBook(ctx, in.OrgID, in.ID, usecase.UpdateClubBookInput{Version: in.Body.Version, Changes: changes})
	if err != nil {
		return nil, fieldErrors(err, "body")
	}
	return &clubBookOutput{Body: toResponse(clubBook)}, nil
}
