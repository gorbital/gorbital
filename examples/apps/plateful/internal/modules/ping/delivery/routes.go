// Package delivery is the ping module's HTTP adapter: the route table in
// this file, and one file per operation with its input, output and handler.
package delivery

import (
	"net/http"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/guard"

	"example.com/plateful/internal/modules/ping/usecase"
)

// handlers holds what every operation's handler uses.
type handlers struct {
	svc *usecase.Service
}

// Register adds the ping routes to r. Both are public: every other route
// requires sign-in unless it says guard.Public().
func Register(r *gorbital.Router, svc *usecase.Service) {
	h := handlers{svc: svc}
	example := r.Group("/v1", gorbital.Tags("Example"))

	gorbital.Get(example, "/ping", h.ping, gorbital.OperationID("ping"),
		gorbital.Summary("Check the API is reachable"),
		guard.Public())
	gorbital.Post(example, "/echo", h.echo, gorbital.OperationID("echo"),
		gorbital.Summary("Echo a message"),
		gorbital.Description("Returns the trimmed message. Blank messages are rejected with the `message_required` error code."),
		gorbital.Errors(http.StatusUnprocessableEntity),
		guard.Public())
}
