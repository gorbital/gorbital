// Package mailevents is the mail events module: POST /v1/webhooks/resend
// receives Resend's signed bounce and complaint webhooks and puts the
// addresses that bounced permanently or complained on the suppression list,
// which the mail worker checks before every send (ADR-0062). Operators list
// and remove suppressions through the operations API
// (gorbital.dev/gorbital/opshttp).
//
// The webhook is on when RESEND_WEBHOOK_SECRET is set, and answers 404
// webhook_not_found otherwise. Its path, operation ID, headers, error codes
// and audit action (mail.suppression.added) are those of the module a v0.1
// app generated, and are public API (ADR-0015).
//
// Stability: experimental until v0.2.0 (ADR-0015, ADR-0081).
package mailevents

import (
	"fmt"
	"net/http"

	"gorbital.dev/gorbital"
	"gorbital.dev/httpx"
	"gorbital.dev/modules/mail/resend"
	"gorbital.dev/modules/mail/suppressionpg"

	"gorbital.dev/gorbital/mailevents/internal/delivery"
	maileventsdomain "gorbital.dev/gorbital/mailevents/internal/domain"
	maileventsusecase "gorbital.dev/gorbital/mailevents/internal/usecase"
)

// Module returns the mail events module. Its name is "mailevents". gorbital.New
// fails with a configuration error when RESEND_WEBHOOK_SECRET is set but isn't
// a Resend signing secret (whsec_…).
func Module() gorbital.Module {
	var platform *gorbital.Platform
	return gorbital.Module{
		Name: "mailevents",
		// Error codes are public API: add new ones, never change existing
		// ones.
		Errors: []httpx.Mapping{
			{Err: maileventsdomain.ErrWebhookNotConfigured, Status: http.StatusNotFound, Code: "webhook_not_found", Detail: "this webhook isn't configured"},
			{Err: maileventsdomain.ErrInvalidSignature, Status: http.StatusUnauthorized, Code: "invalid_webhook_signature", Detail: "the webhook signature is missing, invalid or too old"},
			{Err: maileventsdomain.ErrInvalidPayload, Status: http.StatusBadRequest, Code: "invalid_webhook_payload", Detail: "the webhook body is not an event"},
		},
		Platform: func(p *gorbital.Platform) error {
			if secret := p.Config.Mail.ResendWebhookSecret; !secret.IsZero() {
				if err := resend.CheckWebhookSecret(secret); err != nil {
					return fmt.Errorf("RESEND_WEBHOOK_SECRET: %w", err)
				}
			}
			platform = p
			return nil
		},
		Routes: func(r *gorbital.Router, d gorbital.Deps) {
			deps := maileventsusecase.Deps{Logger: d.Logger.With("source", "mail")}
			if platform != nil && d.DB != nil {
				suppressions, err := suppressionpg.NewStore(d.DB)
				if err != nil {
					panic(err) // reported by gorbital.New as an error naming the module
				}
				deps.Suppressions, deps.Recorder = suppressions, d.Audit
				if secret := platform.Config.Mail.ResendWebhookSecret; !secret.IsZero() {
					deps.Reader = resendWebhooks{secret: secret}
				}
			}
			delivery.Register(r, maileventsusecase.NewService(deps))
		},
	}
}
