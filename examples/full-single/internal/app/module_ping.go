package app

import (
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"apistock.dev/config"
	"apistock.dev/httpx"

	"example.com/acme-api/internal/modules/ping"
	pingdomain "example.com/acme-api/internal/modules/ping/domain"
)

// registerPing wires the ping example module. Error codes are public API:
// add new ones, never change existing ones.
func registerPing(api huma.API, mapper *httpx.Mapper, message config.Value[string]) error {
	err := mapper.Add(
		httpx.Mapping{Err: pingdomain.ErrMessageRequired, Status: http.StatusUnprocessableEntity, Code: "message_required"},
	)
	if err != nil {
		return fmt.Errorf("ping module: %w", err)
	}
	ping.New(message).Register(api)
	return nil
}
