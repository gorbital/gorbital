// Package ops is the operations module: admin APIs for runtime settings and
// background jobs, composed from the apistock settings and jobs modules
// (ADR-0026, ADR-0031, ADR-0033).
package ops

import (
	"github.com/danielgtaylor/huma/v2"

	opsdelivery "example.com/acme-api/internal/modules/ops/delivery"
	opsusecase "example.com/acme-api/internal/modules/ops/usecase"
)

// Module is the operations module.
type Module struct {
	svc *opsusecase.Service
}

// New builds the module on the settings store and jobs manager.
func New(store opsusecase.SettingsStore, manager opsusecase.JobsManager) *Module {
	return &Module{svc: opsusecase.NewService(store, manager)}
}

// Register adds the module's HTTP operations to api.
func (m *Module) Register(api huma.API) {
	opsdelivery.RegisterSettings(api, m.svc)
	opsdelivery.RegisterJobs(api, m.svc)
}
