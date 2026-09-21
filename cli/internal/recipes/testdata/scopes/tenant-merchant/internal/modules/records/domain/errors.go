package domain

import (
	"errors"
	"strings"
)

// Errors of the records use cases. module.go maps each to an HTTP status
// and a problem code, which are public API.
var (
	// ErrUnauthenticated reports an operation without a member acting in
	// the organisation: a route without guard.OrgMember. An organisation the
	// caller isn't a member of never reaches the use cases; the guard
	// answers org_not_found.
	ErrUnauthenticated = errors.New("records: a member of the organisation is required")

	// ErrInvalidRecord reports invalid fields. The error is a
	// *ValidationError.
	ErrInvalidRecord = errors.New("records: invalid record")

	// ErrRecordNotFound reports a record that doesn't exist in the
	// organisation, such as another organisation's. The two aren't told
	// apart, so IDs can't be probed.
	ErrRecordNotFound = errors.New("records: record not found")

	// ErrRecordTitleTaken reports a title the organisation already uses,
	// ignoring case.
	ErrRecordTitleTaken = errors.New("records: record title is already taken")

	// ErrRecordVersionConflict reports an update to a version that is no
	// longer current.
	ErrRecordVersionConflict = errors.New("records: record was changed since it was read")
)

// FieldError is one invalid field.
type FieldError struct {
	Field   string
	Message string
}

// ValidationError lists every invalid field of a record.
type ValidationError struct {
	Errors []FieldError
}

func (e *ValidationError) Error() string {
	parts := make([]string, len(e.Errors))
	for i, f := range e.Errors {
		parts[i] = f.Field + " " + f.Message
	}
	return "records: invalid record: " + strings.Join(parts, "; ")
}

// Unwrap returns ErrInvalidRecord.
func (e *ValidationError) Unwrap() error { return ErrInvalidRecord }
