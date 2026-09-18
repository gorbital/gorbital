// Package delivery is the payments module's HTTP adapter: the route table
// in this file, and one file per operation with its input, output and
// handler.
package delivery

import (
	"net/http"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/guard"
	"gorbital.dev/webhook"

	"example.com/plateful/internal/modules/payments/usecase"
)

// handlers holds what every operation's handler uses.
type handlers struct {
	svc *usecase.Service
}

// MaxDeliveryBytes is the largest webhook body accepted. Payment events are
// small; a bigger one is refused with 413 before it is verified.
const MaxDeliveryBytes = 64 << 10

// docs:start payment-routes

// Register adds the payments routes to r. Three kinds of caller, three
// guards, and no two of them look alike.
//
//   - /v1/orders/{orderId}/pay and its GET are the customer's. A customer
//     belongs to no organisation, so guard.OrgMember is the wrong guard
//     however tempting the path looks; guard.Permission(PermPay) checks a
//     platform permission the "user" role holds, and the use case checks
//     that the order is theirs, because a guard cannot read a row.
//   - /v1/webhooks/payments is the provider's. It has no session at all, so
//     it is guard.Public() plus guard.Webhook(verifier): the delivery is
//     trusted because it is signed, and applied once because of its event
//     ID.
//   - /v1/platform/payments/{id}/refund is the platform's own staff. The
//     path carries no organisation because the operation isn't a tenant's,
//     and guard.RecentReauth() means an idle stolen session can't move
//     money.
func Register(r *gorbital.Router, svc *usecase.Service, verifier webhook.Verifier) {
	h := handlers{svc: svc}

	orders := r.Group("/v1/orders/{orderId}", gorbital.Tags("Payments"))
	gorbital.Post(orders, "/pay", h.payOrder, gorbital.OperationID("payments-pay"),
		gorbital.Summary("Pay for your order"),
		gorbital.Description("Creates a pending payment with the provider and returns its reference; the provider confirms it later by webhook, and the restaurant may not accept the order until it does.\n\nThe call is idempotent twice over. An order that already has a pending payment gets that payment back rather than a second one. And, like every POST in this API, sending an `Idempotency-Key` header makes a retry with the same key return the first response, with `Idempotent-Replayed: true`."),
		gorbital.Status(http.StatusCreated),
		gorbital.Errors(http.StatusNotFound, http.StatusConflict),
		guard.Permission(usecase.PermPay))
	gorbital.Get(orders, "/payment", h.getOrderPayment, gorbital.OperationID("payments-get"),
		gorbital.Summary("Get your order's payment"),
		gorbital.Errors(http.StatusNotFound),
		guard.Permission(usecase.PermPay))

	hooks := r.Group("/v1/webhooks", gorbital.Tags("Payments"))
	gorbital.Post(hooks, "/payments", h.paymentEvent, gorbital.OperationID("payments-event"),
		gorbital.Summary("Receive a payment event"),
		gorbital.Description("The payment provider's webhook, signed with the Standard Webhooks scheme. An event is applied once however often it is delivered, and an event type this app doesn't act on is accepted and ignored so the provider stops retrying it."),
		gorbital.Errors(http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity),
		guard.Public(), // the provider has no session
		guard.Webhook(verifier, guard.WebhookBodyLimit(MaxDeliveryBytes)))

	platform := r.Group("/v1/platform/payments", gorbital.Tags("Platform"))
	gorbital.Post(platform, "/{id}/refund", h.refundPayment, gorbital.OperationID("payments-refund"),
		gorbital.Summary("Refund a payment"),
		gorbital.Description("Gives the customer their money back and records why. Platform staff only, and only for a payment that took any: a payment already refunded is refused."),
		gorbital.Errors(http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity),
		guard.Permission(usecase.PermRefund), guard.RecentReauth())
}

// docs:end payment-routes
