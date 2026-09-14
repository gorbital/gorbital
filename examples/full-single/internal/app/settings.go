package app

import (
	"errors"
	"strings"

	"apistock.dev/modules/settings"
)

const defaultPingMessage = "pong"

// appSettings are the runtime settings: non-secret values operators change
// without a redeploy through /ops/settings (ADR-0031). Secrets and
// infrastructure stay in config.go.
type appSettings struct {
	pingMessage *settings.Setting[string]
}

// declareSettings declares every runtime setting.
func declareSettings(reg *settings.Registry) appSettings {
	return appSettings{
		pingMessage: settings.String(reg, "example.ping_message", defaultPingMessage,
			settings.Describe("Reply of GET /v1/ping. An example runtime setting: change it with PUT /ops/settings/example.ping_message."),
			settings.MaxLen(100),
			settings.Validate(func(s string) error {
				if strings.TrimSpace(s) == "" {
					return errors.New("must not be blank")
				}
				return nil
			}),
		),
	}
}
