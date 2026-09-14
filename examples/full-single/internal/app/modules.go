package app

import (
	"github.com/danielgtaylor/huma/v2"

	"apistock.dev/config"
	"apistock.dev/httpx"

	opsusecase "example.com/acme-api/internal/modules/ops/usecase"
)

// services are what business modules need from the composition root. When
// exporting the OpenAPI document, only values the routes need are set.
type services struct {
	pingMessage config.Value[string]
	settings    opsusecase.SettingsStore
	jobs        opsusecase.JobsManager
	audit       opsusecase.AuditLog
}

// registerModules wires every business module: its HTTP operations and its
// error mappings. Each module has its own module_<name>.go file.
func registerModules(api huma.API, mapper *httpx.Mapper, svc services) error {
	//aps:anchor modules
	if err := registerPing(api, mapper, svc.pingMessage); err != nil {
		return err
	}
	if err := registerOps(api, mapper, svc.settings, svc.jobs, svc.audit); err != nil {
		return err
	}
	return nil
}
