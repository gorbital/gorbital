package app

import (
	"errors"
	"log/slog"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5/pgxpool"

	"apistock.dev/audit"
	"apistock.dev/config"
	"apistock.dev/httpx"
	"apistock.dev/ratelimit"

	authmodule "example.com/acme-api/internal/modules/auth"
	opsusecase "example.com/acme-api/internal/modules/ops/usecase"
)

// services are what business modules need from the composition root. When
// exporting the OpenAPI document, only values the routes need are set and db
// is nil.
type services struct {
	db          *pgxpool.Pool
	recorder    audit.Recorder
	logger      *slog.Logger
	pingMessage config.Value[string]
	ops         opsusecase.Deps
	auth        *authmodule.Module
	// ipLimiter limits /v1/auth/ requests per client IP (rate_limits.go).
	ipLimiter ratelimit.Taker
}

// registerModules wires every business module: its HTTP operations and its
// error mappings. Each module has its own module_<name>.go file, and
// aps gen resource adds a line after the anchor.
func registerModules(api huma.API, mapper *httpx.Mapper, svc services) error {
	return errors.Join(
		//aps:anchor modules
		registerProjects(api, mapper, svc),
		registerPing(api, mapper, svc.pingMessage),
		registerOps(api, mapper, svc.ops),
		registerAuth(api, mapper, svc.auth),
	)
}
