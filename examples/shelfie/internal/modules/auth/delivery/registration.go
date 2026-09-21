package delivery

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"

	"github.com/danielgtaylor/huma/v2"
)

// A Registration registers POST /v1/auth/register.
type Registration func(r *routes, h *handler)

// registerOperation is v0.1's POST /v1/auth/register.
func registerOperation() huma.Operation {
	return huma.Operation{
		OperationID: "auth-register", Method: http.MethodPost, Path: "/v1/auth/register",
		Summary: "Create an account",
		Description: "Emails a 6-digit verification code. The response, and how long it takes, are the same whether or not the address already has an account. " +
			"Registering again before verifying keeps the password only when it is the same; otherwise the account is left without one, and the owner sets it with `POST /v1/auth/password/forgot` after verifying.",
		Tags: []string{"Auth"}, DefaultStatus: http.StatusAccepted,
		Errors: []int{http.StatusUnprocessableEntity, http.StatusTooManyRequests, http.StatusServiceUnavailable},
	}
}

// accepted is POST /v1/auth/register's response.
func accepted() *acceptedOutput {
	return &acceptedOutput{Body: AcceptedResponse{Status: "check_your_email", Message: "Check your email for a 6-digit code to verify your address."}}
}

type registerInput struct {
	Body struct {
		_        struct{} `json:"-" additionalProperties:"true"`
		Email    string   `json:"email" maxLength:"254" example:"ada@example.com"`
		Password string   `json:"password" maxLength:"512" doc:"At least 12 characters"`
	}
}

// DefaultRegistration registers v0.1's POST /v1/auth/register.
func DefaultRegistration(r *routes, h *handler) {
	passwordRoute(r, registerOperation(), func(ctx context.Context, in *registerInput) (*acceptedOutput, error) {
		if err := h.svc.Register(ctx, in.Body.Email, in.Body.Password); err != nil {
			return nil, authError(err)
		}
		return accepted(), nil
	}, passwordField{"password", passwordDocFormat})
}

// RegistrationWithFields registers POST /v1/auth/register with the app's
// extra fields T beside email and password (authhttp's RegisterFields).
func RegistrationWithFields[T any]() Registration {
	return func(r *routes, h *handler) {
		passwordRoute(r, registerOperation(), func(ctx context.Context, in *registerWithFieldsInput[T]) (*acceptedOutput, error) {
			if err := h.svc.RegisterWithFields(ctx, in.Body.Email, in.Body.Password, in.Body.Fields); err != nil {
				return nil, authError(err)
			}
			return accepted(), nil
		}, passwordField{"password", passwordDocFormat})
	}
}

type registerWithFieldsInput[T any] struct {
	Body RegisterBody[T]
}

// RegisterBody is POST /v1/auth/register's body with an app's fields T: one
// JSON object holding email, password and T's fields side by side. Its
// OpenAPI schema is v0.1's with T's properties and required fields added,
// so Huma validates T's tags before the handler runs; other properties
// are still allowed, as in v0.1.
type RegisterBody[T any] struct {
	_        struct{} `json:"-" additionalProperties:"true"`
	Email    string   `json:"email" maxLength:"254" example:"ada@example.com"`
	Password string   `json:"password" maxLength:"512" doc:"At least 12 characters"`
	// Fields are decoded from the same object.
	Fields T `json:"-"`
}

// registerBase is RegisterBody without its fields or methods.
type registerBase struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// UnmarshalJSON decodes email, password and T's fields from one object.
func (b *RegisterBody[T]) UnmarshalJSON(data []byte) error {
	var base registerBase
	if err := json.Unmarshal(data, &base); err != nil {
		return err
	}
	if err := json.Unmarshal(data, &b.Fields); err != nil {
		return err
	}
	b.Email, b.Password = base.Email, base.Password
	return nil
}

// TransformSchema adds T's properties and required fields to the body's
// schema.
func (b *RegisterBody[T]) TransformSchema(r huma.Registry, s *huma.Schema) *huma.Schema {
	fields := huma.SchemaFromType(r, reflect.TypeFor[T]())
	if fields.Ref != "" {
		fields = r.SchemaFromRef(fields.Ref)
	}
	for name, p := range fields.Properties {
		if _, taken := s.Properties[name]; !taken {
			s.Properties[name] = p
		}
	}
	for _, name := range fields.Required {
		if name != "email" && name != "password" {
			s.Required = append(s.Required, name)
		}
	}
	return s
}
