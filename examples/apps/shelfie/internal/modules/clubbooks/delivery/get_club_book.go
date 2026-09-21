package delivery

import "context"

func (h handlers) getClubBook(ctx context.Context, in *clubBookIDInput) (*clubBookOutput, error) {
	clubBook, err := h.svc.GetClubBook(ctx, in.OrgID, in.ID)
	if err != nil {
		return nil, err
	}
	return &clubBookOutput{Body: toResponse(clubBook)}, nil
}
