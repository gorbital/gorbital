// Package ping is an example module showing the layered structure: domain
// rules, use cases, and an HTTP delivery adapter. Copy it to start a module.
package ping

import (
	"github.com/danielgtaylor/huma/v2"

	"gorbital.dev/config"

	pingdelivery "example.com/acme-api/internal/modules/ping/delivery"
	pingusecase "example.com/acme-api/internal/modules/ping/usecase"
)

// Module is the ping example module.
type Module struct {
	svc *pingusecase.Service
}

// New builds the module. message is the ping reply, usually a runtime
// setting.
func New(message config.Value[string]) *Module {
	return &Module{svc: pingusecase.NewService(message)}
}

// Register adds the module's HTTP operations to api.
func (m *Module) Register(api huma.API) {
	pingdelivery.Register(api, m.svc)
}
