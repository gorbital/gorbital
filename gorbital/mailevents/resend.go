package mailevents

import (
	"fmt"
	"net/http"
	"time"

	"gorbital.dev/config"
	"gorbital.dev/modules/mail/resend"

	maileventsdomain "gorbital.dev/gorbital/mailevents/internal/domain"
)

// providerResend is the provider the webhook path names.
const providerResend = "resend"

// resendWebhooks reads Resend's signed webhooks, as a v0.1 app's
// infra_mail.go does.
type resendWebhooks struct {
	secret config.Secret
}

func (resendWebhooks) Provider() string         { return providerResend }
func (resendWebhooks) Tolerance() time.Duration { return resend.WebhookTolerance }

func (w resendWebhooks) Read(r maileventsdomain.WebhookRequest, now time.Time) (maileventsdomain.Webhook, error) {
	header := http.Header{}
	header.Set(resend.HeaderWebhookID, r.ID)
	header.Set(resend.HeaderWebhookTimestamp, r.Timestamp)
	header.Set(resend.HeaderWebhookSignature, r.Signature)
	if err := resend.VerifyWebhook(w.secret, header, r.Body, now); err != nil {
		return maileventsdomain.Webhook{}, fmt.Errorf("%w: %w", maileventsdomain.ErrInvalidSignature, err)
	}
	event, err := resend.ParseWebhookEvent(r.Body)
	if err != nil {
		return maileventsdomain.Webhook{}, fmt.Errorf("%w: %w", maileventsdomain.ErrInvalidPayload, err)
	}
	e := maileventsdomain.Event{Kind: maileventsdomain.KindOther, Recipients: event.To}
	switch event.Type {
	case resend.EventEmailBounced:
		e.Kind, e.Permanent = maileventsdomain.KindBounce, event.Bounce.Permanent()
		e.Detail = event.Bounce.Type + "/" + event.Bounce.SubType
	case resend.EventEmailComplained:
		e.Kind = maileventsdomain.KindComplaint
	}
	return maileventsdomain.Webhook{ID: r.ID, Events: []maileventsdomain.Event{e}}, nil
}
