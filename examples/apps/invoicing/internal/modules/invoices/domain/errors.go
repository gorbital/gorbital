package domain

import (
	"errors"
	"strings"
)

// Errors of the invoices use cases. module.go maps each to an HTTP status
// and a problem code, which are public API.
var (
	// ErrUnauthenticated reports an operation without a member acting in
	// the organisation: a route without guard.OrgMember. An organisation the
	// caller isn't a member of never reaches the use cases; the guard
	// answers org_not_found.
	ErrUnauthenticated = errors.New("invoices: a member of the organisation is required")

	// ErrInvalidInvoice reports invalid fields. The error is a
	// *ValidationError.
	ErrInvalidInvoice = errors.New("invoices: invalid invoice")

	// ErrInvoiceNotFound reports an invoice that doesn't exist in the
	// organisation, such as another organisation's. The two aren't told
	// apart, so IDs can't be probed.
	ErrInvoiceNotFound = errors.New("invoices: invoice not found")

	// ErrInvoiceNumberTaken reports a number the organisation already uses,
	// ignoring case.
	ErrInvoiceNumberTaken = errors.New("invoices: invoice number is already taken")

	// ErrInvoiceVersionConflict reports an update to a version that is no
	// longer current.
	ErrInvoiceVersionConflict = errors.New("invoices: invoice was changed since it was read")
)

// FieldError is one invalid field.
type FieldError struct {
	Field   string
	Message string
}

// ValidationError lists every invalid field of an invoice.
type ValidationError struct {
	Errors []FieldError
}

func (e *ValidationError) Error() string {
	parts := make([]string, len(e.Errors))
	for i, f := range e.Errors {
		parts[i] = f.Field + " " + f.Message
	}
	return "invoices: invalid invoice: " + strings.Join(parts, "; ")
}

// Unwrap returns ErrInvalidInvoice.
func (e *ValidationError) Unwrap() error { return ErrInvalidInvoice }
