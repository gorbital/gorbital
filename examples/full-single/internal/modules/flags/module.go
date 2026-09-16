// Package flags is the feature flags module: it tells signed-in clients
// which features they get, from the flags declared flags.Client() in
// internal/app/flags.go (ADR-0057). Operators change flags through the ops
// module.
package flags

import (
	"github.com/danielgtaylor/huma/v2"

	flagsdelivery "example.com/acme-api/internal/modules/flags/delivery"
	flagsusecase "example.com/acme-api/internal/modules/flags/usecase"
)

// Module is the feature flags module.
type Module struct {
	svc *flagsusecase.Service
}

// New builds the module on store.
func New(store flagsusecase.FlagsStore) *Module {
	return &Module{svc: flagsusecase.NewService(store)}
}

// Register adds the module's HTTP operations to api.
func (m *Module) Register(api huma.API) {
	flagsdelivery.Register(api, m.svc)
}
