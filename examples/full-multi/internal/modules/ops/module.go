// Package ops is the operations module: admin APIs for runtime settings,
// background jobs, the audit log, releases, email and system health, composed
// from the apistock settings, jobs, auditpg, releases and mail modules
// (ADR-0026, ADR-0031, ADR-0033, ADR-0036, ADR-0037, ADR-0040, ADR-0051).
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

// New builds the module on its dependencies.
func New(deps opsusecase.Deps) *Module {
	return &Module{svc: opsusecase.NewService(deps)}
}

// Register adds the module's HTTP operations to api.
func (m *Module) Register(api huma.API) {
	opsdelivery.RegisterSettings(api, m.svc)
	opsdelivery.RegisterJobs(api, m.svc)
	opsdelivery.RegisterAudit(api, m.svc)
	opsdelivery.RegisterReleases(api, m.svc)
	opsdelivery.RegisterMail(api, m.svc)
	opsdelivery.RegisterAuth(api, m.svc)
	opsdelivery.RegisterSystem(api, m.svc)
	opsdelivery.RegisterAuditStats(api, m.svc)
	opsdelivery.RegisterJobsOverview(api, m.svc)
	opsdelivery.RegisterRetention(api, m.svc)
}
