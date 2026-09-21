package delivery

import "context"

func (h handlers) deleteClubBook(ctx context.Context, in *clubBookIDInput) (*struct{}, error) {
	return nil, h.svc.DeleteClubBook(ctx, in.OrgID, in.ID)
}
