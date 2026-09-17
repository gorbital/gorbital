// Package delivery is the HTTP adapter of the feature flags module.
package delivery

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/operation"
	"gorbital.dev/modules/openapi"

	flagsusecase "gorbital.dev/gorbital/flagshttp/internal/usecase"
)

// ClientFlagsResponse is whether each client flag is on for the caller.
type ClientFlagsResponse struct {
	Flags map[string]bool `json:"flags" doc:"Whether each flag is on for you, by key" example:"{\"example.ping_time\":false}"`
}

type clientFlagsOutput struct {
	CacheControl string `header:"Cache-Control"`
	Body         ClientFlagsResponse
}

type handler struct {
	svc *flagsusecase.Service
}

// Register adds the feature flag operations to api.
func Register(r *gorbital.Router, svc *flagsusecase.Service) {
	h := &handler{svc: svc}
	operation.Register(r, huma.Operation{
		OperationID: "flags-list",
		Method:      http.MethodGet,
		Path:        "/v1/flags",
		Summary:     "List your feature flags",
		Description: "Whether each feature flag clients may read is on for you. Answers depend on who asks, so responses aren't cached. Unknown keys may appear as flags are added: ignore them.",
		Tags:        []string{"Feature flags"},
		Security:    openapi.Bearer,
		Errors:      []int{http.StatusUnauthorized, http.StatusForbidden},
	}, h.list)
}

func (h *handler) list(ctx context.Context, _ *struct{}) (*clientFlagsOutput, error) {
	flags, err := h.svc.ClientFlags(ctx)
	if err != nil {
		return nil, err
	}
	return &clientFlagsOutput{CacheControl: "private, no-store", Body: ClientFlagsResponse{Flags: flags}}, nil
}
