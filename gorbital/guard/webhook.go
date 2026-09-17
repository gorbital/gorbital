package guard

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/internal/route"
	"gorbital.dev/httpx"
	"gorbital.dev/webhook"
)

// DefaultWebhookBodyLimit is the largest webhook body [Webhook] reads unless
// [WebhookBodyLimit] sets another: 1 MiB, Huma's default body limit.
const DefaultWebhookBodyLimit = 1 << 20

// The refusals of [Webhook]. invalid_webhook_signature is the code v0.1's
// Resend webhook returns.
var (
	errInvalidWebhookSignature = httpx.NewProblem(http.StatusUnauthorized, "invalid_webhook_signature", "the webhook signature is missing, invalid or too old")
	errUnreadableBody          = httpx.NewProblem(http.StatusBadRequest, "bad_request", "the request body couldn't be read")
)

// A WebhookOption configures [Webhook].
type WebhookOption func(*webhookGuard)

type webhookGuard struct {
	limit int64
}

// WebhookBodyLimit sets the largest body [Webhook] reads, in bytes. Larger
// requests are refused with 413 request_too_large before they are verified.
// Default: [DefaultWebhookBodyLimit].
func WebhookBodyLimit(n int64) WebhookOption {
	return func(g *webhookGuard) { g.limit = n }
}

// Webhook refuses requests whose signature v doesn't accept, with 401
// invalid_webhook_signature, before the route's input is parsed. It reads
// the raw body once, up to the body limit (413 request_too_large beyond
// it), passes it with the headers to v, and gives the same bytes to the
// handler. A verifier error that doesn't wrap webhook.ErrInvalidSignature,
// such as a key server that is down, is a 500, mapped like any guard error.
//
// Senders have no session: combine it with [Public].
//
//	payments, err := webhook.NewStandard(webhook.StandardConfig{Secrets: []string{cfg.PaymentsWebhookSecret.Reveal()}})
//	gorbital.Post(r, "/v1/webhooks/payments", h.paymentEvent, guard.Public(), guard.Webhook(payments))
//
// A verified request can still arrive twice: senders retry, and a captured
// request can be replayed within the verifier's tolerance. Make the handler
// idempotent, for example by storing the delivery ID with the change it
// makes.
func Webhook(v webhook.Verifier, opts ...WebhookOption) gorbital.RouteOption {
	w := webhookGuard{limit: DefaultWebhookBodyLimit}
	for _, o := range opts {
		o(&w)
	}
	g := route.Guard{
		Name:     "webhook",
		Statuses: []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusRequestEntityTooLarge},
		Check:    w.check(v),
	}
	switch {
	case v == nil:
		g.Err = errors.New("guard.Webhook: the verifier is nil")
	case w.limit < 1:
		g.Err = fmt.Errorf("guard.Webhook: body limit %d must be positive", w.limit)
	}
	return addGuard(g)
}

func (w webhookGuard) check(v webhook.Verifier) func(hctx huma.Context) error {
	tooLarge := httpx.NewProblem(http.StatusRequestEntityTooLarge, "request_too_large", fmt.Sprintf("request body must not exceed %d bytes", w.limit))
	return func(hctx huma.Context) error {
		r, _ := humago.Unwrap(hctx)
		if r.ContentLength > w.limit {
			return tooLarge
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, w.limit+1))
		var maxBytes *http.MaxBytesError
		switch {
		case errors.As(err, &maxBytes):
			return tooLarge
		case err != nil:
			return errUnreadableBody
		case int64(len(body)) > w.limit:
			return tooLarge
		}
		// The handler reads the bytes that were verified.
		_ = r.Body.Close()
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
		if err := v.Verify(hctx.Context(), r.Header, body); err != nil {
			if errors.Is(err, webhook.ErrInvalidSignature) {
				return errInvalidWebhookSignature
			}
			return err
		}
		return nil
	}
}
