package delivery

import "context"

type endpointIDInput struct {
	OrgID string `path:"orgId" maxLength:"64" example:"org_mfrggzdfmztwq2lkmfrggzdfmy"`
	ID    string `path:"id" maxLength:"64" example:"nte_mfrggzdfmztwq2lkmfrggzdfmy"`
}

func (h handlers) removeEndpoint(ctx context.Context, in *endpointIDInput) (*struct{}, error) {
	return nil, h.svc.RemoveEndpoint(ctx, in.OrgID, in.ID)
}
