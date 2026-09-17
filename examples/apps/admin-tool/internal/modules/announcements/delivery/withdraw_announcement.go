package delivery

import "context"

type withdrawAnnouncementInput struct {
	ID   string `path:"id" example:"ann_mfrggzdfmztwq2lkmfrggzdfmy"`
	Body struct {
		Reason string `json:"reason" minLength:"3" maxLength:"200" doc:"Why customers stop seeing it; kept in the audit log"`
	}
}

func (h handlers) withdrawAnnouncement(ctx context.Context, in *withdrawAnnouncementInput) (*struct{}, error) {
	return nil, h.svc.WithdrawAnnouncement(ctx, in.ID, in.Body.Reason)
}
