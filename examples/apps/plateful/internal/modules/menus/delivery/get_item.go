package delivery

import "context"

// itemIDInput names one of an organisation's menu items.
type itemIDInput struct {
	OrgID string `path:"orgId" maxLength:"64" example:"org_mfrggzdfmztwq2lkmfrggzdfmy"`
	ID    string `path:"id" maxLength:"64" example:"mnu_mfrggzdfmztwq2lkmfrggzdfmy"`
}

func (h handlers) getItem(ctx context.Context, in *itemIDInput) (*menuItemOutput, error) {
	item, err := h.svc.GetItem(ctx, in.OrgID, in.ID)
	if err != nil {
		return nil, err
	}
	return &menuItemOutput{Body: toResponse(item)}, nil
}
