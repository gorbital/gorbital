package delivery

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

type handler struct{}

func Register(api huma.API) {
	h := &handler{}
	signedIn := func(op huma.Operation) huma.Operation { return op }
	huma.Register(api, signedIn(huma.Operation{
		OperationID: "projects-create", Method: http.MethodPost, Path: "/v1/projects",
	}), h.create)
	huma.Register(api, huma.Operation{OperationID: "projects-list", Method: "get", Path: "/v1/projects"}, h.list)
}

func (h *handler) create(context.Context, *struct{}) (*struct{}, error) { return nil, nil }
func (h *handler) list(context.Context, *struct{}) (*struct{}, error)   { return nil, nil }
