// Package delivery is the payments module's HTTP adapter: the route table
// in this file, and one file per operation with its input, output and
// handler.
package delivery

import (
	"net/http"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/guard"
	"gorbital.dev/webhook"

	"example.com/payments/internal/modules/payments/usecase"
)

// handlers holds what every operation's handler uses.
type handlers struct {
	svc *usecase.Service
}

// MaxDeliveryBytes is the largest webhook body accepted. Payment events are
// small; a bigger one is refused with 413 before it is verified.
const MaxDeliveryBytes = 64 << 10

// docs:start routes

// Register adds the payments routes to r: the provider posts deliveries,
// which carry no session and are trusted only because they are signed;
// staff read a payment with a permission.
func Register(r *gorbital.Router, svc *usecase.Service, verifier webhook.Verifier) {
	h := handlers{svc: svc}
	payments := r.Group("/v1", gorbital.Tags("Payments"))

	gorbital.Post(payments, "/webhooks/payments", h.paymentEvent,
		gorbital.Summary("Receive a payment event"),
		gorbital.Description("The payment provider's webhook. Signed with the Standard Webhooks scheme; deliveries are recorded once, however often they arrive."),
		gorbital.Errors(http.StatusConflict, http.StatusUnprocessableEntity),
		guard.Public(), // the provider has no session
		guard.Webhook(verifier, guard.WebhookBodyLimit(MaxDeliveryBytes)))

	gorbital.Get(payments, "/payments/{id}", h.getPayment,
		gorbital.Summary("Get a recorded payment"),
		gorbital.Errors(http.StatusNotFound),
		guard.Permission(usecase.PermRead))
}

// docs:end routes
