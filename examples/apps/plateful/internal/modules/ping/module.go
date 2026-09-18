// Package ping is an example module: a public endpoint whose reply is a
// runtime setting and whose extra field a feature flag turns on, in the
// layers every module uses (domain, usecase, delivery) with one file per
// operation. A module without a table has no repository. Copy it to start a
// module by hand, or generate one with orb gen module.
package ping

import (
	"errors"
	"net/http"
	"strings"

	"gorbital.dev/gorbital"
	"gorbital.dev/httpx"
	"gorbital.dev/modules/flags"
	"gorbital.dev/modules/settings"

	"example.com/plateful/internal/modules/ping/delivery"
	"example.com/plateful/internal/modules/ping/domain"
	"example.com/plateful/internal/modules/ping/usecase"
)

// Module returns the ping module. main.go adds it with every other module
// through modules.All. Error codes, setting keys and flag keys are public
// API: add new ones, never change existing ones.
func Module() gorbital.Module {
	// Declared in Settings and Flags, before the stores exist, and used in
	// Routes: no lookup by key.
	var (
		message    *settings.Setting[string]
		serverTime *flags.Flag
	)
	return gorbital.Module{
		Name: "ping",
		Errors: []httpx.Mapping{
			{Err: domain.ErrMessageRequired, Status: http.StatusUnprocessableEntity, Code: "message_required"},
		},
		Settings: func(r *settings.Registry) {
			message = settings.String(r, "example.ping_message", "pong",
				settings.Describe("Reply of GET /v1/ping. An example runtime setting: change it with PUT /ops/settings/example.ping_message."),
				settings.MaxLen(100),
				settings.Validate(notBlank),
			)
		},
		Flags: func(r *flags.Registry) {
			serverTime = flags.Bool(r, "example.ping_time",
				flags.Describe("Adds the server's time to GET /v1/ping replies. An example feature flag: turn it on with PUT /ops/flags/example.ping_time."),
				flags.Client(), // listed by GET /v1/flags
			)
		},
		Routes: func(r *gorbital.Router, _ gorbital.Deps) {
			delivery.Register(r, usecase.NewService(message, serverTime))
		},
	}
}

// notBlank refuses a blank ping reply.
func notBlank(s string) error {
	if strings.TrimSpace(s) == "" {
		return errors.New("must not be blank")
	}
	return nil
}
