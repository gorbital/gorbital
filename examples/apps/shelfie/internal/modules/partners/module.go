// Package partners is Shelfie's partners module: the books a partner
// bookshop reports a reader bought, received as a signed webhook, in four
// layers with one file per operation.
package partners

import (
	"context"
	"fmt"
	"net/http"

	"gorbital.dev/gorbital"
	"gorbital.dev/httpx"
	"gorbital.dev/webhook"

	"example.com/shelfie/internal/modules/partners/delivery"
	"example.com/shelfie/internal/modules/partners/domain"
	"example.com/shelfie/internal/modules/partners/repository"
	"example.com/shelfie/internal/modules/partners/usecase"
)

// docs:start module

// Module returns the partners module. secrets are the partner's webhook
// signing secrets, which main.go reads from the environment: the module
// takes what it needs instead of reading the environment itself.
func Module(partner string, secrets []string) gorbital.Module {
	v, err := newVerifier(secrets)
	return gorbital.Module{
		Name: "partners",
		// A secret the library won't take stops the app before it listens,
		// with the other configuration errors.
		Platform: func(*gorbital.Platform) error { return err },
		Errors: []httpx.Mapping{
			{Err: domain.ErrUnauthenticated, Status: http.StatusUnauthorized, Code: "unauthenticated", Detail: "authentication is required"},
			{Err: domain.ErrInvalidEventID, Status: http.StatusUnprocessableEntity, Code: "invalid_event_id", Detail: "an event ID is 1 to 100 characters of letters, digits, '.', ':', '-' or '_'"},
			{Err: domain.ErrInvalidReader, Status: http.StatusUnprocessableEntity, Code: "invalid_reader", Detail: "a purchase names the reader who bought the book"},
			{Err: domain.ErrInvalidISBN, Status: http.StatusUnprocessableEntity, Code: "invalid_isbn", Detail: "the ISBN must be an ISBN-13"},
			{Err: domain.ErrInvalidTitle, Status: http.StatusUnprocessableEntity, Code: "invalid_title", Detail: "a title is 1 to 300 characters"},
			{Err: domain.ErrInvalidTime, Status: http.StatusUnprocessableEntity, Code: "invalid_purchased_at", Detail: "purchased_at is required and can't be in the future"},
		},
		Permissions: []gorbital.Permission{
			{Name: usecase.PermRead, Description: "See the books you bought through a partner", Roles: []string{"user"}},
		},
		Routes: func(r *gorbital.Router, d gorbital.Deps) {
			// d is zero while the OpenAPI document is exported: the service
			// is built, but no use case runs.
			svc := usecase.NewService(repository.NewStore(d.DB), d.Audit, d.Logger)
			delivery.Register(r, svc, partner, v)
		},
	}
}

// newVerifier returns the verifier for the partner's deliveries. Without a
// secret it returns one that refuses everything, so an unconfigured app
// answers 401 instead of trusting the sender, and the route is in the
// OpenAPI document either way.
func newVerifier(secrets []string) (webhook.Verifier, error) {
	if len(secrets) == 0 {
		return refuseEverything{}, nil
	}
	v, err := webhook.NewStandard(webhook.StandardConfig{Secrets: secrets})
	if err != nil {
		return refuseEverything{}, fmt.Errorf("PARTNER_WEBHOOK_SECRET: %w", err)
	}
	return v, nil
}

type refuseEverything struct{}

func (refuseEverything) Verify(context.Context, http.Header, []byte) error {
	return webhook.ErrInvalidSignature
}

// docs:end module
