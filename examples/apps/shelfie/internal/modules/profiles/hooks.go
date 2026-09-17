package profiles

import (
	"context"
	"errors"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"

	"gorbital.dev/gorbital/authhttp"

	"example.com/shelfie/internal/modules/profiles/domain"
	"example.com/shelfie/internal/modules/profiles/repository"
	"example.com/shelfie/internal/modules/profiles/usecase"
)

// docs:start registration-fields

// RegistrationFields are what POST /v1/auth/register takes beside email and
// password (authhttp.RegisterFields in cmd/api/signin.go). Huma checks the
// tags when the request arrives, for every request, before any account
// exists, and the OpenAPI document shows them.
type RegistrationFields struct {
	DisplayName string `json:"display_name" minLength:"1" maxLength:"50" doc:"How other readers see you"`
	Country     string `json:"country,omitempty" pattern:"^[A-Z]{2}$" doc:"ISO 3166-1 alpha-2 code, such as GB"`
}

// Resolve applies the profile's own rules, such as a display name that
// isn't only spaces, when the request arrives: a mistake answers 422
// validation_failed whether or not the address has an account.
func (f *RegistrationFields) Resolve(huma.Context) []error {
	_, err := domain.Fields{DisplayName: f.DisplayName, Country: f.Country}.Clean()
	switch {
	case errors.Is(err, domain.ErrInvalidCountry):
		return []error{&huma.ErrorDetail{Location: "body.country", Message: err.Error(), Value: f.Country}}
	case err != nil:
		return []error{&huma.ErrorDetail{Location: "body.display_name", Message: err.Error(), Value: f.DisplayName}}
	}
	return nil
}

// SaveRegistration creates the new reader's profile in tx, the transaction
// that creates their account.
func SaveRegistration(ctx context.Context, tx pgx.Tx, a authhttp.NewAccount, f RegistrationFields) error {
	svc := usecase.NewService(repository.NewStore(tx))
	return svc.SaveRegistration(ctx, a.User.ID, domain.Fields{DisplayName: f.DisplayName, Country: f.Country})
}

// docs:end registration-fields

// docs:start before-login

// ErrReaderSuspended is the refusal a suspended reader's sign-in gets: 403
// reader_suspended. Refuse checks the code when the program starts.
var ErrReaderSuspended = authhttp.Refuse("reader_suspended", "your account is suspended; write to help@shelfie.example")

// RefuseSuspended is an authhttp.BeforeLogin hook. It runs once the reader's
// password (or passkey, Google account, phone code) and second factor are
// verified, so only the account's owner ever sees the refusal.
func RefuseSuspended(ctx context.Context, tx pgx.Tx, a authhttp.LoginAttempt) error {
	suspended, err := usecase.NewService(repository.NewStore(tx)).Suspended(ctx, a.User.ID)
	switch {
	case err != nil:
		return err // sign-in fails closed with 500
	case suspended:
		return ErrReaderSuspended
	}
	return nil
}

// docs:end before-login
