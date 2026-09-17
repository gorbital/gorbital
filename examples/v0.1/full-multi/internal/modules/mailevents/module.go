// Package mailevents is the mail events module: it receives the email
// provider's signed webhooks and puts addresses that bounced permanently or
// complained on the suppression list, which the mail worker checks before
// every send (ADR-0062). Operators list and remove suppressions through the
// ops module.
package mailevents

import (
	"github.com/danielgtaylor/huma/v2"

	maileventsdelivery "example.com/acme-api/internal/modules/mailevents/delivery"
	maileventsusecase "example.com/acme-api/internal/modules/mailevents/usecase"
)

// Module is the mail events module.
type Module struct {
	svc *maileventsusecase.Service
}

// New builds the module on its dependencies.
func New(deps maileventsusecase.Deps) *Module {
	return &Module{svc: maileventsusecase.NewService(deps)}
}

// Register adds the module's HTTP operations to api.
func (m *Module) Register(api huma.API) {
	maileventsdelivery.Register(api, m.svc)
}
