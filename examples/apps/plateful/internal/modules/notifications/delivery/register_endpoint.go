package delivery

import (
	"context"

	"example.com/plateful/internal/modules/notifications/usecase"
)

// docs:start register-endpoint-handler

type registerEndpointInput struct {
	OrgID string `path:"orgId" maxLength:"64" example:"org_mfrggzdfmztwq2lkmfrggzdfmy"`
	Body  struct {
		Label string `json:"label" minLength:"1" maxLength:"60" example:"Kitchen" doc:"What you call this channel"`
		// URL is a secret in a request body, so it is marked writeOnly: the
		// generated clients and the documentation then say what the API
		// already does, which is that this value goes in and never comes back
		// out. format is left off deliberately — OpenAPI's "uri" would accept
		// every scheme, and the real rules are the module's.
		URL string `json:"url" minLength:"1" maxLength:"2000" writeOnly:"true" example:"https://hooks.example.com/services/T000/B000/XXXX" doc:"The incoming webhook to post to. Stored, never returned, never logged."`
	}
}

// registerEndpoint is the whole handler: read the request, call the use
// case, shape the answer. Which URLs this deployment is willing to deliver
// to is the use case's decision and the domain's rule, not the handler's,
// because the delivery job has to make the same judgement and it has no
// request to make it from.
//
// Note what is *not* here: the handler never reads in.Body.URL for anything
// but passing it on. It is not logged, not echoed in an error and not put
// into the 201, which is why the response type has no field for it.
func (h handlers) registerEndpoint(ctx context.Context, in *registerEndpointInput) (*endpointOutput, error) {
	e, err := h.svc.RegisterEndpoint(ctx, in.OrgID, usecase.RegisterEndpointInput{
		Label: in.Body.Label,
		URL:   in.Body.URL,
	})
	if err != nil {
		return nil, fieldErrors(err, "body")
	}
	return &endpointOutput{Body: toResponse(e)}, nil
}

// docs:end register-endpoint-handler
