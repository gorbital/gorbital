// Package ops is the operations module: admin APIs for runtime settings,
// background jobs and the audit log, composed from the apistock settings,
// jobs and auditpg modules (ADR-0026, ADR-0031, ADR-0033, ADR-0036).
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

// New builds the module on the settings store, jobs manager and audit log.
func New(store opsusecase.SettingsStore, manager opsusecase.JobsManager, auditLog opsusecase.AuditLog) *Module {
	return &Module{svc: opsusecase.NewService(store, manager, auditLog)}
}

// Register adds the module's HTTP operations to api.
func (m *Module) Register(api huma.API) {
	opsdelivery.RegisterSettings(api, m.svc)
	opsdelivery.RegisterJobs(api, m.svc)
	opsdelivery.RegisterAudit(api, m.svc)
}
