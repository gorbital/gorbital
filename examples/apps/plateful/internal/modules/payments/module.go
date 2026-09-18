// Package payments is the payments module: paying for an order, in four
// layers (domain, usecase, repository, delivery) with one file per
// operation in each.
//
// # The provider is generic
//
// No payment provider's API is described anywhere here. A pay call creates
// a pending payment and gets back a reference; the provider confirms it
// later by delivering a signed webhook. A real integration replaces one
// adapter, provider.go, and nothing else — the status machine, the routes,
// the idempotency and the audit trail are written against usecase.Provider
// and the webhook body, not against a vendor.
//
// # The rule that spans two modules
//
// A restaurant may not accept an order until its payment is authorised.
// Half of that rule is here, in the order_payments table and the status
// machine that fills in its status column. The other half is in the orders
// module: internal/modules/orders refuses to accept an order whose payment
// isn't 'authorised', by reading order_payments.status with SQL of its own,
// because a module never imports another module's layers.
//
// So the two halves are joined by column names and nothing else. The names
// order_payments.order_id and order_payments.status are public API of this
// module in exactly the way an error code is: renaming either of them
// breaks a module that this one cannot see, and no compiler, test or type
// in either module will say so. If you change this table, grep the orders
// module for it first.
package payments

import (
	"net/http"

	"gorbital.dev/config"
	"gorbital.dev/gorbital"
	"gorbital.dev/httpx"

	"example.com/plateful/internal/modules/payments/delivery"
	"example.com/plateful/internal/modules/payments/domain"
	"example.com/plateful/internal/modules/payments/repository"
	"example.com/plateful/internal/modules/payments/usecase"
)

// Module returns the payments module. main.go adds it with every other
// module through modules.All. Error codes and permission names are public
// API: add new ones, never change existing ones.
func Module() gorbital.Module {
	return gorbital.Module{
		Name: "payments",
		// docs:start payment-errors
		// invalid_webhook_signature isn't declared here: guard.Webhook
		// answers it itself, before any handler of this module runs.
		//
		// order_not_payable covers two refusals that are one answer to the
		// customer — an order that isn't waiting to be paid, and a payment
		// whose status has already moved on — while payment_already_final
		// is the webhook's: an event about money that has finished moving.
		Errors: []httpx.Mapping{
			{Err: domain.ErrUnauthenticated, Status: http.StatusUnauthorized, Code: "unauthenticated", Detail: "authentication is required"},
			{Err: domain.ErrInvalidPayment, Status: http.StatusUnprocessableEntity, Code: "validation_failed", Detail: "the payment is not valid"},
			{Err: domain.ErrPaymentNotFound, Status: http.StatusNotFound, Code: "payment_not_found", Detail: "no payment of yours has this ID"},
			{Err: domain.ErrOrderNotFound, Status: http.StatusNotFound, Code: "order_not_found", Detail: "no order of yours has this ID"},
			{Err: domain.ErrPaymentNotPayable, Status: http.StatusConflict, Code: "order_not_payable", Detail: "this order can't be paid in its current state"},
			{Err: domain.ErrPaymentAlreadyFinal, Status: http.StatusConflict, Code: "payment_already_final", Detail: "the payment has already been refunded or has already failed"},
			{Err: domain.ErrPaymentNotRefundable, Status: http.StatusConflict, Code: "payment_not_refundable", Detail: "the payment took no money, or it has already been given back"},
		},
		// docs:end payment-errors
		// docs:start payment-permissions
		// Platform permissions, not organisation ones. A customer belongs
		// to no organisation, so there is no membership to hang paying on:
		// the "user" role, which every signed-in account has, holds
		// payments.payment.pay, and the check that the order is the
		// caller's is in the use case. Refunding is the platform's own
		// staff's, on a route that names no organisation at all.
		Permissions: []gorbital.Permission{
			{Name: usecase.PermPay, Description: "Pay for your own order and read its payment", Roles: []string{"user"}},
			{Name: usecase.PermRefund, Description: "Refund a customer's payment", Roles: []string{"platform_admin"}},
		},
		// docs:end payment-permissions
		Routes: func(r *gorbital.Router, d gorbital.Deps) {
			// d is zero while the OpenAPI document is exported: the service
			// is built, but no use case runs.
			verifier, err := newVerifier(config.OS)
			if err != nil && d.Logger != nil {
				d.Logger.Warn(err.Error())
			}
			svc := usecase.NewService(repository.NewStore(d.DB), newProvider(), d.Audit, d.Logger)
			delivery.Register(r, svc, verifier)
		},
	}
}
