// Package payments is the billing API's payments module: the payment
// provider's webhooks, recorded once each, in four layers (domain, usecase,
// repository, delivery) with one file per operation in each.
package payments

import (
	"net/http"
	"time"

	"gorbital.dev/config"
	"gorbital.dev/gorbital"
	"gorbital.dev/httpx"
	"gorbital.dev/modules/jobs"

	"example.com/payments/internal/modules/payments/delivery"
	"example.com/payments/internal/modules/payments/domain"
	"example.com/payments/internal/modules/payments/repository"
	"example.com/payments/internal/modules/payments/usecase"
)

// docs:start module

// Module returns the payments module. main.go adds it with every other
// module through modules.All.
func Module() gorbital.Module {
	return gorbital.Module{
		Name: "payments",
		// docs:start errors
		Errors: []httpx.Mapping{
			{Err: domain.ErrPaymentIDRequired, Status: http.StatusUnprocessableEntity, Code: "payment_id_required", Detail: "the event must name the payment it is about"},
			{Err: domain.ErrInvalidAmount, Status: http.StatusUnprocessableEntity, Code: "invalid_amount", Detail: "the amount must be a positive number of minor units"},
			{Err: domain.ErrInvalidCurrency, Status: http.StatusUnprocessableEntity, Code: "invalid_currency", Detail: "the currency must be a three-letter code, such as GBP"},
			{Err: domain.ErrPaymentNotFound, Status: http.StatusNotFound, Code: "payment_not_found", Detail: "no payment with that ID is recorded"},
			{Err: domain.ErrDeliveryInProgress, Status: http.StatusConflict, Code: "delivery_in_progress", Detail: "this delivery is being recorded; send it again"},
		},
		// docs:end errors
		Permissions: []gorbital.Permission{
			{Name: usecase.PermRead, Description: "Read a payment recorded from the provider's webhooks", Roles: []string{"platform_admin"}},
		},
		// docs:start jobs
		Jobs: func(defs *jobs.Definitions, d gorbital.Deps) {
			jobs.Define(defs, jobs.Definition[usecase.ReceiptArgs]{
				Name:        usecase.ReceiptJob,
				Description: "Sends the receipt for a payment recorded from the provider's webhook. Enqueued by the request, in the transaction that recorded the payment.",
				Worker:      usecase.NewReceiptWorker(repository.NewStore(d.DB), d.Logger),
				NewArgs:     func() usecase.ReceiptArgs { return usecase.ReceiptArgs{} },
				Enabled:     true, // no Schedule: the app enqueues it, nothing runs it on a clock
				Timeout:     30 * time.Second,
				MaxAttempts: 10,
			})
		},
		// docs:end jobs
		Routes: func(r *gorbital.Router, d gorbital.Deps) {
			// d is zero while the OpenAPI document is exported: the service
			// is built, but no use case runs.
			verifier, err := newVerifier(config.OS)
			if err != nil && d.Logger != nil {
				d.Logger.Warn(err.Error())
			}
			svc := usecase.NewService(
				repository.NewStore(d.DB),
				repository.NewTxManager(d.DB, d.Jobs),
				d.Logger,
			)
			delivery.Register(r, svc, verifier)
		},
	}
}

// docs:end module
