package delivery

import "context"

type listEndpointsInput struct {
	OrgID string `path:"orgId" maxLength:"64" example:"org_mfrggzdfmztwq2lkmfrggzdfmy"`
}

// EndpointList is the restaurant's notification endpoints.
type EndpointList struct {
	Items []EndpointResponse `json:"items"`
}

type endpointListOutput struct {
	Body EndpointList
}

// listEndpoints returns every endpoint of the organisation. There is no
// cursor: a restaurant has a handful of channels, not a feed.
func (h handlers) listEndpoints(ctx context.Context, in *listEndpointsInput) (*endpointListOutput, error) {
	endpoints, err := h.svc.ListEndpoints(ctx, in.OrgID)
	if err != nil {
		return nil, err
	}
	out := &endpointListOutput{Body: EndpointList{Items: make([]EndpointResponse, len(endpoints))}}
	for i, e := range endpoints {
		out.Body.Items[i] = toResponse(e)
	}
	return out, nil
}
