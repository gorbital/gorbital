package payments

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"gorbital.dev/config"
	"gorbital.dev/webhook"
)

// WebhookSecretVar names the environment variable holding the payment
// provider's signing secret: "whsec_" and base64, as a provider's dashboard
// shows it, or several separated by commas while the secret is rotated —
// add the new one, deploy, switch the provider over, remove the old one.
// PAYMENTS_WEBHOOK_SECRET_FILE reads it from a mounted file instead, like
// every other gorbital secret.
const WebhookSecretVar = "PAYMENTS_WEBHOOK_SECRET"

// newVerifier returns the verifier for the secrets in WebhookSecretVar, and
// the reason it couldn't be built. Standard Webhooks signs
// "<webhook-id>.<webhook-timestamp>.<body>", so the whole body — including
// the event ID this module applies events by — is covered.
//
// Without a usable secret it returns a verifier that accepts nothing, for
// two reasons: the OpenAPI document exports without any configuration, and
// a deployment that forgot the variable refuses every delivery instead of
// marking orders paid on the word of anyone who can reach the route. The
// provider retries, so the deliveries arrive once the secret is set.
func newVerifier(src config.Source) (webhook.Verifier, error) {
	secret, err := src.Secret(WebhookSecretVar)
	switch {
	case err != nil:
		return refuseEverything{}, fmt.Errorf("payments: %s: %w", WebhookSecretVar, err)
	case secret.IsZero():
		return refuseEverything{}, fmt.Errorf("payments: %s isn't set: every delivery is refused", WebhookSecretVar)
	}
	v, err := webhook.NewStandard(webhook.StandardConfig{Secrets: strings.Split(secret.Reveal(), ",")})
	if err != nil {
		return refuseEverything{}, fmt.Errorf("payments: %s: %w", WebhookSecretVar, err) // never quotes the secret
	}
	return v, nil
}

// refuseEverything is the verifier used when there is no usable secret. Its
// answer is the one a wrong signature gets: 401 invalid_webhook_signature.
type refuseEverything struct{}

func (refuseEverything) Verify(context.Context, http.Header, []byte) error {
	return webhook.ErrInvalidSignature
}
