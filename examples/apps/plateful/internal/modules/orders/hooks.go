package orders

import (
	"context"
	"errors"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"

	"gorbital.dev/gorbital/authhttp"

	"example.com/plateful/internal/modules/orders/domain"
	"example.com/plateful/internal/modules/orders/repository"
)

// docs:start registration-fields

// RegistrationFields are what POST /v1/auth/register takes beside an email
// address and a password (authhttp.RegisterFields, wired in
// cmd/api/signin.go). Huma checks the tags when the request arrives, for
// every request, before any account exists, and the OpenAPI document shows
// them.
//
// Asking for the delivery address at registration is the whole reason
// Plateful customises sign-in: an account with nowhere to deliver to is an
// account that has to be interrupted the first time it orders.
type RegistrationFields struct {
	DisplayName string `json:"display_name" minLength:"1" maxLength:"80" doc:"The name couriers and restaurants see"`
	Address     string `json:"address,omitempty" maxLength:"200" doc:"Where you usually want an order taken; you can change it per order"`
}

// Resolve applies the profile's own rules when the request arrives, so a
// display name of three spaces is answered with 422 validation_failed
// whether or not the address already has an account — a registration must
// answer the same either way, or it tells strangers who has an account here.
func (f *RegistrationFields) Resolve(huma.Context) []error {
	if _, err := (domain.CustomerFields{DisplayName: f.DisplayName, Address: f.Address}).Clean(); err != nil {
		var invalid *domain.ValidationError
		if errors.As(err, &invalid) && len(invalid.Errors) > 0 {
			return []error{&huma.ErrorDetail{
				Location: "body." + invalid.Errors[0].Field,
				Message:  invalid.Errors[0].Message,
				Value:    f.DisplayName,
			}}
		}
	}
	return nil
}

// SaveRegistration writes the new customer's profile in tx, the transaction
// that creates their account: the account and the profile exist together or
// not at all.
//
// It runs only for a registration by email. An account created by a first
// Google, Apple or GitHub sign-in, or by an operator, never sends these
// fields — so nothing downstream may assume a profile exists, and placing an
// order falls back to the address in the request when there is none.
func SaveRegistration(ctx context.Context, tx pgx.Tx, a authhttp.NewAccount, f RegistrationFields) error {
	fields, err := domain.CustomerFields{DisplayName: f.DisplayName, Address: f.Address}.Clean()
	if err != nil {
		return err
	}
	now := time.Now().UTC().Truncate(time.Microsecond) // PostgreSQL's precision
	return repository.NewStoreOn(tx).UpsertCustomer(ctx,
		domain.Customer{UserID: a.User.ID, CustomerFields: fields}, now)
}

// docs:end registration-fields
