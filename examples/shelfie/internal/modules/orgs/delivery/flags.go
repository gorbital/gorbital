package delivery

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"gorbital.dev/gorbital"
	orgslib "gorbital.dev/modules/orgs"

	"gorbital.dev/gorbital/operation"
)

// OrgFlagsResponse is whether each client feature flag is on in the
// organisation.
type OrgFlagsResponse struct {
	Flags map[string]bool `json:"flags" doc:"Whether each flag is on for you in this organisation, by key" example:"{\"example.ping_time\":false}"`
}

type orgFlagsOutput struct {
	CacheControl string `header:"Cache-Control"`
	Body         OrgFlagsResponse
}

// registerFlags adds the operation listing feature flags in an organisation
// (ADR-0057). Every member reads them.
func registerFlags(r *gorbital.Router, h *handler, inOrg func(huma.Operation) huma.Operation) {
	operation.Register(r, inOrg(huma.Operation{
		OperationID: "orgs-flags-list", Method: http.MethodGet, Path: "/v1/orgs/{orgId}/flags",
		Summary:     "List your feature flags in the organisation",
		Description: "Like GET /v1/flags, as a member acting in the organisation: its allow and deny lists apply, and percentage rollouts use the organisation, so members get the same answers unless a user list names them.",
	}), h.flags)
}

func (h *handler) flags(ctx context.Context, in *orgInput) (*orgFlagsOutput, error) {
	flags, err := h.svc.Flags(ctx, orgslib.ID(in.OrgID))
	if err != nil {
		return nil, err
	}
	return &orgFlagsOutput{CacheControl: "private, no-store", Body: OrgFlagsResponse{Flags: flags}}, nil
}
