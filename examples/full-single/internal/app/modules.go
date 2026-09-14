package app

import (
	"github.com/danielgtaylor/huma/v2"

	"apistock.dev/config"
	"apistock.dev/httpx"

	authmodule "example.com/acme-api/internal/modules/auth"
	opsusecase "example.com/acme-api/internal/modules/ops/usecase"
	projectsmodule "example.com/acme-api/internal/modules/projects"
)

// services are what business modules need from the composition root. When
// exporting the OpenAPI document, only values the routes need are set.
type services struct {
	pingMessage config.Value[string]
	ops         opsusecase.Deps
	auth        *authmodule.Module
	projects    *projectsmodule.Module
}

// registerModules wires every business module: its HTTP operations and its
// error mappings. Each module has its own module_<name>.go file.
func registerModules(api huma.API, mapper *httpx.Mapper, svc services) error {
	//aps:anchor modules
	if err := registerPing(api, mapper, svc.pingMessage); err != nil {
		return err
	}
	if err := registerOps(api, mapper, svc.ops); err != nil {
		return err
	}
	if err := registerAuth(api, mapper, svc.auth); err != nil {
		return err
	}
	if err := registerProjects(api, mapper, svc.projects); err != nil {
		return err
	}
	return nil
}
