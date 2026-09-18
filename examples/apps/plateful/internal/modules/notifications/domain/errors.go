package domain

import (
	"errors"
	"strings"
)

// Errors of the notifications use cases. module.go maps each to an HTTP
// status and a problem code, which are public API.
var (
	// ErrUnauthenticated reports an operation without a member acting in the
	// organisation: a route without guard.OrgMember. An organisation the
	// caller isn't a member of never reaches the use cases; the guard answers
	// org_not_found.
	ErrUnauthenticated = errors.New("notifications: a member of the organisation is required")

	// ErrInvalidEndpoint reports invalid fields. The error is a
	// *ValidationError.
	ErrInvalidEndpoint = errors.New("notifications: invalid notification endpoint")

	// ErrInvalidURL reports a URL this deployment will not deliver to. It is
	// its own error rather than a field of ErrInvalidEndpoint because it has
	// its own problem code (invalid_endpoint_url): "the URL is not
	// acceptable" and "the label is too long" are different conversations,
	// and only the first one is about what this server is willing to dial.
	ErrInvalidURL = errors.New("notifications: invalid endpoint URL")

	// ErrEndpointNotFound reports an endpoint that doesn't exist, or that
	// belongs to another organisation. The cases aren't told apart, so IDs
	// can't be probed.
	ErrEndpointNotFound = errors.New("notifications: endpoint not found")

	// ErrEndpointLabelTaken reports a label the organisation already uses,
	// ignoring case.
	ErrEndpointLabelTaken = errors.New("notifications: endpoint label is already taken")

	// ErrDeliveryFailed reports a delivery that may work next time: the
	// connection failed, or the endpoint answered 408, 429 or a 5xx. The
	// delivery worker returns it as an ordinary error, which is how the job
	// system is told to retry.
	ErrDeliveryFailed = errors.New("notifications: delivery failed")

	// ErrDeliveryRejected reports a delivery that will never work: the
	// endpoint answered a 4xx that isn't 408 or 429, redirected, or is not a
	// target this deployment may dial. The worker turns it into
	// river.JobCancel, so the attempts stop.
	ErrDeliveryRejected = errors.New("notifications: delivery rejected")

	// ErrForbiddenTarget reports an address the policy refuses to connect to.
	// The sender's dialler returns it from Control, so it arrives wrapped in
	// the transport's own errors; errors.Is finds it through them.
	ErrForbiddenTarget = errors.New("notifications: the address is not a permitted delivery target")
)

// FieldError is one invalid field.
type FieldError struct {
	Field   string
	Message string
}

// ValidationError lists every invalid field of an endpoint.
type ValidationError struct {
	Errors []FieldError
}

func (e *ValidationError) Error() string {
	parts := make([]string, len(e.Errors))
	for i, f := range e.Errors {
		parts[i] = f.Field + " " + f.Message
	}
	return "notifications: invalid notification endpoint: " + strings.Join(parts, "; ")
}

// Unwrap returns ErrInvalidEndpoint.
func (e *ValidationError) Unwrap() error { return ErrInvalidEndpoint }
