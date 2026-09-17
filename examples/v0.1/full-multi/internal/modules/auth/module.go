// Package auth is the authentication module: sign-up with email
// verification, sign-in with a session cookie (browsers) or bearer token
// (native clients), sessions, password reset and change, account deletion
// and platform roles (ADR-0024, ADR-0038). The flows, tables and SQL are
// here; the gorbital auth module supplies the security building blocks.
package auth

import (
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5/pgxpool"

	authlib "gorbital.dev/modules/auth"

	authdelivery "example.com/acme-api/internal/modules/auth/delivery"
	authrepository "example.com/acme-api/internal/modules/auth/repository"
	authusecase "example.com/acme-api/internal/modules/auth/usecase"
)

// Module is the authentication module.
type Module struct {
	svc    *authusecase.Service
	cookie string
}

// New builds the module's layers on pool. cfg.Store is set to the module's
// repository.
func New(pool *pgxpool.Pool, cfg authusecase.Config) (*Module, error) {
	cfg.Store = authrepository.NewStore(pool)
	svc, err := authusecase.NewService(cfg)
	if err != nil {
		return nil, err
	}
	return &Module{svc: svc, cookie: authlib.DefaultCookieName}, nil
}

// Service returns the use cases, for other wiring (commands, jobs, tests).
func (m *Module) Service() *authusecase.Service { return m.svc }

// Middleware authenticates requests with the module's sessions, and bearer
// API keys (ADR-0058).
func (m *Module) Middleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return authlib.Middleware(m.svc, authlib.WithCookieName(m.cookie), authlib.WithLogger(logger), authlib.WithAPIKeys(m.svc))
}

// Register adds the module's HTTP operations to api. A nil module registers
// the operations without handlers' dependencies, for exporting the OpenAPI
// document.
func (m *Module) Register(api huma.API) {
	var svc *authusecase.Service
	cookie := authlib.DefaultCookieName
	if m != nil {
		svc, cookie = m.svc, m.cookie
	}
	authdelivery.Register(api, svc, cookie)
}

// RegisterOrgServiceAccounts adds organisations' service accounts, for
// multi-tenant apps (ADR-0058). A nil module registers the operations
// without their dependencies.
func (m *Module) RegisterOrgServiceAccounts(api huma.API) {
	var svc *authusecase.Service
	if m != nil {
		svc = m.svc
	}
	authdelivery.RegisterOrgServiceAccounts(api, svc)
}
