// Package projects wires the projects module layers.
package projects

import (
	"time"

	"github.com/danielgtaylor/huma/v2"

	projectdelivery "apistock.dev/spikes/openapi/internal/modules/projects/delivery"
	projectusecase "apistock.dev/spikes/openapi/internal/modules/projects/usecase"
)

// Module is the projects bounded context.
type Module struct {
	svc *projectusecase.Service
}

// New builds the module around a storage adapter.
func New(repo projectusecase.ProjectRepository, newID func() string, now func() time.Time) *Module {
	return &Module{svc: projectusecase.NewService(repo, newID, now)}
}

// Register adds the module's HTTP operations to api.
func (m *Module) Register(api huma.API, access func(permission string) huma.Middlewares) {
	projectdelivery.Register(api, m.svc, access)
}
