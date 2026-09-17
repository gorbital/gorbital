// Package delivery is the HTTP adapter of the mail events module.
package delivery

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/mailevents/internal/domain"
	maileventsusecase "gorbital.dev/gorbital/mailevents/internal/usecase"
	"gorbital.dev/gorbital/operation"
)

// MaxWebhookBytes bounds a webhook body. Resend's events are a few
// kilobytes; the app-wide APP_MAX_BODY_BYTES still applies first.
const MaxWebhookBytes = 256 << 10

type webhookInput struct {
	ID        string `header:"svix-id" maxLength:"255" doc:"The delivery's ID, the same for every retry"`
	Timestamp string `header:"svix-timestamp" maxLength:"20" doc:"When the attempt was signed, in Unix seconds"`
	Signature string `header:"svix-signature" maxLength:"4096" doc:"Space-separated v1,<base64 HMAC-SHA256> signatures"`
	RawBody   []byte
}

type handler struct {
	svc *maileventsusecase.Service
}

// Register adds the webhook operations to api.
func Register(r *gorbital.Router, svc *maileventsusecase.Service) {
	h := &handler{svc: svc}
	operation.Register(r, huma.Operation{
		OperationID: "mail-resend-webhook", Method: http.MethodPost, Path: "/v1/webhooks/resend",
		Summary: "Resend's email events",
		Description: "Register this URL as a webhook in Resend for `email.bounced` and `email.complained`, and set `RESEND_WEBHOOK_SECRET` to its signing secret. " +
			"Requests are signed (Svix) and refused when the signature doesn't match or is more than 5 minutes old. Hard bounces and complaints put the " +
			"recipients on the suppression list (`GET /ops/mail/suppressions`); soft bounces and other events are accepted and ignored. " +
			"404 when the app doesn't send with Resend or the secret isn't set.",
		Tags:         []string{"Webhooks"},
		MaxBodyBytes: MaxWebhookBytes,
		// The signature covers the raw body; ParseWebhookEvent reads it.
		SkipValidateBody: true,
		DefaultStatus:    http.StatusNoContent,
		Errors:           []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusNotFound},
	}, h.resend)
}

func (h *handler) resend(ctx context.Context, in *webhookInput) (*struct{}, error) {
	return nil, h.svc.ReceiveWebhook(ctx, "resend", domain.WebhookRequest{
		ID: in.ID, Timestamp: in.Timestamp, Signature: in.Signature, Body: in.RawBody,
	})
}
